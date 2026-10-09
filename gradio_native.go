package goradio

// Native implementation of Gradio's FRP 0.44-compatible wire protocol.
// No external process or FRP executable. This intentionally does not import
// the upstream frpc process runtime, whose server Kill handler calls os.Exit.
// Control/data streams use messages from Hugging Face's FRP fork under Apache 2.
import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	frpcrypto "github.com/fatedier/golib/crypto"
	frpio "github.com/fatedier/golib/io"
	"github.com/hashicorp/yamux"
	msg "goradio/internal/frpmsg"
)

// The Gradio FRP server uses the default empty frps token; -n is a unique
// PROXY NAME, not an auth token. The data/control cipher key is empty here.
const officialGradioAPI = "https://api.gradio.app/v3/tunnel-request"

// Solo se solicita información del servidor oficial; nunca se descarga FRP.
type officialShareTarget struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	RootCA string `json:"root_ca"`
}

var officialShareClient = &http.Client{Timeout: 35 * time.Second}

func requestOfficialTarget(ctx context.Context) (officialShareTarget, error) {
	var target officialShareTarget
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, officialGradioAPI, nil)
	if err != nil {
		return target, err
	}
	resp, err := officialShareClient.Do(req)
	if err != nil {
		return target, fmt.Errorf("Gradio tunnel-request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return target, fmt.Errorf("Gradio API respondió HTTP %d", resp.StatusCode)
	}
	var arr []officialShareTarget
	err = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&arr)
	if err != nil || len(arr) == 0 {
		return target, errors.New("respuesta inválida del servidor de Gradio")
	}
	target = arr[0]
	if target.Host == "" || target.Port <= 0 || target.Port > 65535 || !strings.Contains(target.RootCA, "BEGIN CERTIFICATE") {
		return target, errors.New("servidor de Gradio devolvió un destino TLS incompleto")
	}
	return target, nil
}

const gradioFRPToken = ""
const gradioFRPVersion = "0.44.0"
const gradioNativePoolSize = 5 // servidor FRP limita PoolCount a MaxPoolCount
const gradioFRPTLSHeadByte = 0x17

var gradioOfficialURL = regexp.MustCompile(`^https://[a-zA-Z0-9-]+\.gradio\.live/?$`)

func init() {
	// The upstream frpc client sets precisely this salt.
	frpcrypto.DefaultSalt = "frp"
}
func nativeAuthKey(timestamp int64) string {
	h := md5.Sum([]byte(fmt.Sprintf("%s%d", gradioFRPToken, timestamp)))
	return hex.EncodeToString(h[:])
}
func nativeTLSConfig(target officialShareTarget) (*tls.Config, error) {
	if target.Host == "" || target.Port < 1 || target.Port > 65535 {
		return nil, errors.New("servidor FRP inválido")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(target.RootCA)) {
		return nil, errors.New("certificado CA del servidor FRP inválido")
	}
	return &tls.Config{RootCAs: pool, ServerName: target.Host, MinVersion: tls.VersionTLS12}, nil
}
func dialNativeFRP(ctx context.Context, target officialShareTarget) (*yamux.Session, error) {
	tlsConfig, err := nativeTLSConfig(target)
	if err != nil {
		return nil, err
	}
	conn, err := (&net.Dialer{Timeout: 12 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(target.Host, fmt.Sprint(target.Port)))
	if err != nil {
		return nil, err
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			conn.Close()
		}
	}()
	// Custom first byte required by the Gradio FRP TLS listener.
	if _, err = conn.Write([]byte{gradioFRPTLSHeadByte}); err != nil {
		return nil, err
	}
	tlsConn := tls.Client(conn, tlsConfig)
	handshakeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err = tlsConn.HandshakeContext(handshakeCtx); err != nil {
		return nil, fmt.Errorf("handshake TLS FRP: %w", err)
	}
	settings := yamux.DefaultConfig()
	settings.LogOutput = io.Discard
	settings.KeepAliveInterval = 60 * time.Second
	mux, err := yamux.Client(tlsConn, settings)
	if err != nil {
		return nil, err
	}
	closeOnError = false
	return mux, nil
}
func randomProxyName() (string, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	return hex.EncodeToString(token), nil
}
func safeGradioLink(link string) bool {
	return gradioOfficialURL.MatchString(link)
}

// runNativeFRPSession handles control messages, heartbeats, public URL and data
// channels until cancellation or protocol error. It does not spawn processes.
func runNativeFRPSession(ctx context.Context, target officialShareTarget, localPort int, proxyName string, onURL func(string)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if localPort < 1 || localPort > 65535 {
		return errors.New("puerto local inválido")
	}
	mux, err := dialNativeFRP(ctx, target)
	if err != nil {
		return err
	}
	defer mux.Close()
	// A disconnect interrupts OpenStream / message reads.
	go func() {
		select {
		case <-ctx.Done():
			mux.Close()
		case <-mux.CloseChan():
		}
	}()
	control, err := mux.OpenStream()
	if err != nil {
		return err
	}
	defer control.Close()
	timestamp := time.Now().Unix()
	login := &msg.Login{Version: gradioFRPVersion, Os: runtime.GOOS, Arch: runtime.GOARCH,
		PoolCount: gradioNativePoolSize, Timestamp: timestamp, PrivilegeKey: nativeAuthKey(timestamp)}
	if err := msg.WriteMsg(control, login); err != nil {
		return fmt.Errorf("login FRP: %w", err)
	}
	_ = control.SetReadDeadline(time.Now().Add(12 * time.Second))
	var response msg.LoginResp
	if err := msg.ReadMsgInto(control, &response); err != nil {
		return fmt.Errorf("respuesta login FRP: %w", err)
	}
	_ = control.SetReadDeadline(time.Time{})
	if response.Error != "" {
		return fmt.Errorf("servidor FRP rechazó login: %s", response.Error)
	}
	if response.RunID == "" {
		return errors.New("servidor FRP no devolvió RunID")
	}
	// Standard FRP control channel is encrypted with the default empty token.
	reader := frpcrypto.NewReader(control, []byte(gradioFRPToken))
	writer, err := frpcrypto.NewWriter(control, []byte(gradioFRPToken))
	if err != nil {
		return err
	}
	messages := make(chan any, 16)
	fault := make(chan error, 1)
	var once sync.Once
	stop := func(err error) { once.Do(func() { fault <- err; control.Close() }) }
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case outgoing, ok := <-messages:
				if !ok {
					return
				}
				if err := msg.WriteMsg(writer, outgoing); err != nil {
					stop(fmt.Errorf("escritura FRP: %w", err))
					return
				}
			}
		}
	}()
	select {
	case messages <- &msg.NewProxy{
		ProxyName: proxyName, ProxyType: "http", UseEncryption: true, UseCompression: true,
		CustomDomains: []string{""}, SubDomain: "random",
	}:
	case <-ctx.Done():
		return ctx.Err()
	}
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()
	pongLast := time.Now()
	received := make(chan msg.Message, 8)
	go func() {
		for {
			incoming, err := msg.ReadMsg(reader)
			if err != nil {
				stop(fmt.Errorf("lectura FRP: %w", err))
				return
			}
			select {
			case received <- incoming:
			case <-ctx.Done():
				return
			}
		}
	}()
	ready := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-fault:
			return err
		case <-heartbeat.C:
			if time.Since(pongLast) > 90*time.Second {
				return errors.New("FRP: heartbeat perdido")
			}
			select {
			case messages <- &msg.Ping{}:
			case <-ctx.Done():
				return ctx.Err()
			}
		case packet := <-received:
			switch v := packet.(type) {
			case *msg.NewProxyResp:
				if v.Error != "" {
					return fmt.Errorf("FRP NewProxyResp: %s", v.Error)
				}
				if digest := sha256.Sum256([]byte(proxyName)); v.ProxyName != hex.EncodeToString(digest[:])[:18] {
					return errors.New("respuesta FRP de proxy incorrecto")
				}
				if !safeGradioLink(v.RemoteAddr) {
					return fmt.Errorf("servidor FRP devolvió URL inesperada: %s", v.RemoteAddr)
				}
				if !ready {
					ready = true
					onURL(v.RemoteAddr)
				}
			case *msg.ReqWorkConn:
				nativeDebug("ReqWorkConn received")
				go handleNativeWorkConn(ctx, mux, response.RunID, localPort)
			case *msg.Pong:
				if v.Error != "" {
					return fmt.Errorf("FRP heartbeat rechazado: %s", v.Error)
				}
				pongLast = time.Now()
			case *msg.Kill:
				// CRITICAL: unlike upstream's client, NEVER call os.Exit.
				return errors.New("el servidor FRP pidió cerrar el túnel")
			}
		}
	}
}
func nativeDebug(format string, args ...any) {
	if os.Getenv("GORADIO_NATIVE_DEBUG") == "1" {
		log.Printf("FRP native debug: "+format, args...)
	}
}
func handleNativeWorkConn(ctx context.Context, mux *yamux.Session, runID string, localPort int) {
	stream, err := mux.OpenStream()
	if err != nil {
		nativeDebug("OpenStream: %v", err)
		return
	}
	defer stream.Close()
	nativeDebug("OpenStream OK")
	if err := msg.WriteMsg(stream, &msg.NewWorkConn{RunID: runID}); err != nil {
		nativeDebug("NewWorkConn: %v", err)
		return
	}
	// Work connections can remain parked in the server-side pool until the
	// next user request (minutes/hours). Never expire them after a few seconds.
	// Cancellation/disconnection closes the parent yamux.Session instead.
	var started msg.StartWorkConn
	if err = msg.ReadMsgInto(stream, &started); err != nil {
		nativeDebug("StartWorkConn: %v", err)
		return
	}
	if started.Error != "" {
		nativeDebug("StartWorkConn error: %s", started.Error)
		return
	}
	local, err := (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(localPort)))
	if err != nil {
		nativeDebug("Local dial: %v", err)
		return
	}
	remote, err := frpio.WithEncryption(stream, []byte(gradioFRPToken))
	if err != nil {
		local.Close()
		nativeDebug("Encryption: %v", err)
		return
	}
	remote = frpio.WithCompression(remote)
	inbound, outbound := frpio.Join(local, remote)
	nativeDebug("WorkConn finished bytes in=%d out=%d", inbound, outbound)
}

// nativeHealthCheck does not publish an outdated URL if a previous native
// FRP session has already disconnected or been replaced.
func gradioPublicHealth(ctx context.Context, link string) bool {
	client := &http.Client{Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link+"/api/health", nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized &&
		strings.Contains(resp.Header.Get("WWW-Authenticate"), "Basic realm=\"Goradio\"") {
		return true
	}
	if resp.StatusCode != http.StatusOK {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	return err == nil && strings.Contains(string(body), `"status":"ok"`)
}
func (m *TunnelManager) verifyGradioNative(ctx context.Context, provider, link string) bool {
	for attempt := 0; attempt < 5; attempt++ {
		if ctx.Err() != nil {
			return false
		}
		if gradioPublicHealth(ctx, link) {
			if ctx.Err() != nil {
				return false
			}
			m.set(TunnelInfo{Provider: provider, Status: "online", URL: link})
			log.Printf("Goradio público verificado (%s): %s", provider, link)
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(1500 * time.Millisecond):
		}
	}
	return false
}

// Watch public health, not just the encrypted FRP control channel. Some remote
// relay instances return an HTML 404 while the TCP tunnel remains connected.
// 3 consecutive failures trigger a fresh public URL. Cancelled attempts cannot
// publish stale addresses.
func (m *TunnelManager) watchGradioNative(ctx context.Context, provider, link string, cancel context.CancelFunc, interval time.Duration) {
	if interval <= 0 {
		interval = 20 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			if gradioPublicHealth(ctx, link) {
				failures = 0
				continue
			}
			failures++
			nativeDebug("Public Gradio health failed %d/3", failures)
			if failures >= 3 {
				if ctx.Err() == nil {
					log.Printf("Gradio nativo: enlace público inactivo, solicitando nuevo túnel")
					m.set(TunnelInfo{Provider: provider, Status: "reconnecting", Error: "Enlace público no disponible"})
					cancel()
				}
				return
			}
		}
	}
}
func (m *TunnelManager) runGradioNative(ctx context.Context, provider string, port int) {
	defer m.set(TunnelInfo{Provider: provider, Status: "closed"})
	retry := time.Second
	for ctx.Err() == nil {
		target, err := requestOfficialTarget(ctx)
		if err == nil {
			var proxy string
			proxy, err = randomProxyName()
			if err == nil {
				m.set(TunnelInfo{Provider: provider, Status: "starting"})
				attemptCtx, cancel := context.WithCancel(ctx)
				established := false
				err = runNativeFRPSession(attemptCtx, target, port, proxy, func(link string) {
					established = true
					go func() {
						if m.verifyGradioNative(attemptCtx, provider, link) {
							m.watchGradioNative(attemptCtx, provider, link, cancel, 20*time.Second)
						}
					}()
				})
				cancel()
				if established {
					retry = time.Second
				}
			}
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("Gradio nativo: %v", err)
		}
		m.set(TunnelInfo{Provider: provider, Status: "reconnecting", Error: fmt.Sprint(err)})
		select {
		case <-ctx.Done():
			return
		case <-time.After(retry):
		}
		if retry < 16*time.Second {
			retry *= 2
		}
	}
}
