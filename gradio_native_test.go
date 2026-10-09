package goradio

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	frpcrypto "github.com/fatedier/golib/crypto"
	frpio "github.com/fatedier/golib/io"
	"github.com/hashicorp/yamux"
	msg "goradio/internal/frpmsg"
)

func TestNativeFRPAuthKeyAndURLValidation(t *testing.T) {
	if nativeAuthKey(123) != "202cb962ac59075b964b07152d234b70" {
		// md5("123"), equivalent to the empty FRP token + timestamp.
		t.Fatalf("auth mismatch: %s", nativeAuthKey(123))
	}
	for _, pair := range []struct {
		url     string
		allowed bool
	}{
		{"https://a1b2c3.gradio.live", true},
		{"https://12345.gradio.live", true},
		{"https://gradio.live", false},
		{"http://test.gradio.live", false},
		{"https://test.gradio.live.evil.org", false},
		{"https://abc.gradio.live:8080", false},
		{"https://abc.gradio.live/path", false},
		{"javascript:alert(1)", false},
	} {
		if actual := safeGradioLink(pair.url); actual != pair.allowed {
			t.Fatalf("%q => %v", pair.url, actual)
		}
	}
	if _, err := nativeTLSConfig(officialShareTarget{Host: "localhost", Port: 8000, RootCA: "garbage"}); err == nil {
		t.Fatal("invalid CA accepted")
	}
	if _, err := nativeTLSConfig(officialShareTarget{Host: "", Port: 1}); err == nil {
		t.Fatal("empty host accepted")
	}
}

type mockFRP struct {
	target officialShareTarget
	close  func()
	events chan string
	errors chan error
}

func newMockFRP(t *testing.T, mode string, localPort int) *mockFRP {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	template := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	target := officialShareTarget{
		Host: "127.0.0.1", Port: l.Addr().(*net.TCPAddr).Port,
		RootCA: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
	}
	m := &mockFRP{target: target, close: func() { _ = l.Close() }, events: make(chan string, 16), errors: make(chan error, 8)}
	t.Cleanup(m.close)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		deadline := 12 * time.Second
		if mode == "delayed-transfer" {
			deadline = 25 * time.Second
		}
		_ = c.SetDeadline(time.Now().Add(deadline))
		var first [1]byte
		if _, err := io.ReadFull(c, first[:]); err != nil {
			m.errors <- err
			return
		}
		if first[0] != gradioFRPTLSHeadByte {
			m.errors <- fmt.Errorf("wrong custom TLS head byte: %x", first)
			return
		}
		tlsConn := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			m.errors <- err
			return
		}
		mux, err := yamux.Server(tlsConn, nil)
		if err != nil {
			m.errors <- err
			return
		}
		defer mux.Close()
		ctrl, err := mux.AcceptStream()
		if err != nil {
			m.errors <- err
			return
		}
		defer ctrl.Close()
		var login msg.Login
		if err := msg.ReadMsgInto(ctrl, &login); err != nil {
			m.errors <- err
			return
		}
		if login.Version != gradioFRPVersion || login.PrivilegeKey != nativeAuthKey(login.Timestamp) {
			m.errors <- errors.New("wrong login")
			return
		}
		m.events <- "authenticated"
		if err := msg.WriteMsg(ctrl, &msg.LoginResp{RunID: "runid-test"}); err != nil {
			m.errors <- err
			return
		}
		reader := frpcrypto.NewReader(ctrl, []byte(gradioFRPToken))
		writer, err := frpcrypto.NewWriter(ctrl, []byte(gradioFRPToken))
		if err != nil {
			m.errors <- err
			return
		}
		var proxy msg.NewProxy
		if err := msg.ReadMsgInto(reader, &proxy); err != nil {
			m.errors <- err
			return
		}
		if proxy.ProxyType != "http" || !proxy.UseEncryption || !proxy.UseCompression || proxy.SubDomain != "random" {
			m.errors <- fmt.Errorf("invalid proxy config: %#v", proxy)
			return
		}
		m.events <- "proxy"
		nameDigest := sha256.Sum256([]byte(proxy.ProxyName))
		answer := msg.NewProxyResp{ProxyName: hex.EncodeToString(nameDigest[:])[:18],
			RemoteAddr: "https://mock.gradio.live"}
		switch mode {
		case "badproxy":
			answer.ProxyName = "wrong"
		case "badurl":
			answer.RemoteAddr = "http://untrusted.test"
		case "denied":
			answer.Error = "proxy denied"
		}
		if err := msg.WriteMsg(writer, &answer); err != nil {
			m.errors <- err
			return
		}
		if mode == "kill" {
			if err := msg.WriteMsg(writer, &msg.Kill{}); err != nil {
				m.errors <- err
			}
			select {
			case <-time.After(1 * time.Second):
			}
			return
		}
		if mode == "transfer" || mode == "delayed-transfer" {
			if err := msg.WriteMsg(writer, &msg.ReqWorkConn{}); err != nil {
				m.errors <- err
				return
			}
			stream, err := mux.AcceptStream()
			if err != nil {
				m.errors <- err
				return
			}
			defer stream.Close()
			var newWork msg.NewWorkConn
			if err := msg.ReadMsgInto(stream, &newWork); err != nil {
				m.errors <- err
				return
			}
			if newWork.RunID != "runid-test" {
				m.errors <- errors.New("wrong runID")
				return
			}
			if mode == "delayed-transfer" {
				time.Sleep(16 * time.Second)
			}
			if err := msg.WriteMsg(stream, &msg.StartWorkConn{ProxyName: proxy.ProxyName}); err != nil {
				m.errors <- err
				return
			}
			remote, err := frpio.WithEncryption(stream, []byte(gradioFRPToken))
			if err != nil {
				m.errors <- err
				return
			}
			remote = frpio.WithCompression(remote)
			if _, err := remote.Write([]byte("GET /api/health HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")); err != nil {
				m.errors <- err
				return
			}
			reply, err := http.ReadResponse(bufio.NewReader(remote), nil)
			if err != nil {
				m.errors <- fmt.Errorf("respuesta vía FRP: %w", err)
				return
			}
			data, err := io.ReadAll(io.LimitReader(reply.Body, 4096))
			reply.Body.Close()
			if err != nil {
				m.errors <- err
				return
			}
			if reply.StatusCode != http.StatusOK || !strings.Contains(string(data), "native-mock-ok") {
				m.errors <- fmt.Errorf("respuesta incorrecta: HTTP %d %q", reply.StatusCode, data)
				return
			}
			m.events <- "work-roundtrip"
			_ = remote.Close()
			return
		}
		_ = ctrl.SetReadDeadline(time.Now().Add(2 * time.Second))
		var incoming msg.Message
		incoming, err = msg.ReadMsg(reader)
		if err == nil {
			m.events <- fmt.Sprintf("message %T", incoming)
		}
	}()
	return m
}
func TestNativeFRPTLSAndProxyHandshake(t *testing.T) {
	cases := []struct {
		mode    string
		wantErr string
	}{
		{"kill", "servidor FRP pidió cerrar"},
		{"badproxy", "proxy incorrecto"},
		{"badurl", "URL inesperada"},
		{"denied", "proxy denied"},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			mock := newMockFRP(t, tc.mode, 0)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			called := make(chan string, 1)
			err := runNativeFRPSession(ctx, mock.target, 7878, "proxy-unit-test", func(url string) { called <- url })
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("unexpected error %v want %q", err, tc.wantErr)
			}
			select {
			case <-mock.errors:
				t.Fatal("mock protocol error")
			default:
			}
			if tc.mode == "kill" {
				select {
				case u := <-called:
					if u != "https://mock.gradio.live" {
						t.Fatal(u)
					}
				default:
					t.Fatal("URL not announced")
				}
			}
		})
	}
}
func TestNativeFRPRejectsUntrustedCertificate(t *testing.T) {
	mock := newMockFRP(t, "kill", 0)
	mock.target.RootCA = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not-a-certificate")}))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := runNativeFRPSession(ctx, mock.target, 7878, "proxy-test", func(string) {}); err == nil {
		t.Fatal("invalid certificate was trusted")
	}
}
func TestNativeHealthVerificationHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()
	manager := NewTunnelManager()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	manager.verifyGradioNative(ctx, "gradio_native", server.URL)
	if len(manager.Snapshot()) != 0 {
		t.Fatal("cancelled verifier changed provider status")
	}
}
func TestNativeHealthRejectsWrongPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"not-gradio"}`))
	}))
	defer server.Close()
	manager := NewTunnelManager()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	manager.verifyGradioNative(ctx, "gradio_native", server.URL)
	if len(manager.Snapshot()) != 0 {
		t.Fatal("invalid health response published URL")
	}
}
func checkNativeEncryptedHTTPRoundTrip(t *testing.T, mode string, limit time.Duration) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("native-mock-ok"))
	}))
	defer backend.Close()
	local := strings.TrimPrefix(backend.URL, "http://")
	_, p, err := net.SplitHostPort(local)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		t.Fatal(err)
	}
	mock := newMockFRP(t, mode, port)
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		finished <- runNativeFRPSession(ctx, mock.target, port, "unit-native-roundtrip", func(string) {})
	}()
	for {
		select {
		case event := <-mock.events:
			if event == "work-roundtrip" {
				cancel()
				select {
				case <-finished:
				case <-time.After(time.Second):
					t.Fatal("session didn't close")
				}
				return
			}
		case e := <-mock.errors:
			t.Fatalf("protocolo FRP/HTTP: %v", e)
		case e := <-finished:
			t.Fatalf("connection exited before HTTP roundtrip: %v", e)
		case <-ctx.Done():
			t.Fatal("encrypted stream roundtrip timed out")
		}
	}
}

func TestNativeFRPEncryptedSnappyHTTPRoundTrip(t *testing.T) {
	checkNativeEncryptedHTTPRoundTrip(t, "transfer", 7*time.Second)
}
func TestNativeFRPIdleConnectionSurvives16Seconds(t *testing.T) {
	// Regression: FRP parks work connections before delivering StartWorkConn.
	// Closing parked streams after 15 seconds makes gradio.live stop routing.
	checkNativeEncryptedHTTPRoundTrip(t, "delayed-transfer", 22*time.Second)
}
func TestGradioNativeProviderDoesNotRequireFrpcBinary(t *testing.T) {
	for _, provider := range []string{"gradio", "gradio_native"} {
		command, args, err := tunnelCommand(provider, 7945)
		if err != nil {
			t.Fatal(err)
		}
		if command != "native-go" || len(args) != 1 || args[0] != "7945" {
			t.Fatalf("%s unexpectedly requires executable: %s %#v", provider, command, args)
		}
	}
	if _, _, err := tunnelCommand("gradio_frpc", 7945); err == nil {
		t.Fatal("El modo externo debe quedar eliminado")
	}
	for _, provider := range []string{"gradio", "gradio_native"} {
		if url := parseTunnelURL(provider, "https://token.gradio.live"); url != "https://token.gradio.live" {
			t.Fatalf("%s did not accept official hostname: %s", provider, url)
		}
	}
}

func TestGradioNativeWatchReplacesDeadLink(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "No interface is running", http.StatusNotFound)
	}))
	defer backend.Close()
	m := NewTunnelManager()
	m.set(TunnelInfo{Provider: "gradio", Status: "online", URL: backend.URL})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { m.watchGradioNative(ctx, "gradio", backend.URL, cancel, 10*time.Millisecond); close(done) }()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("watcher did not detect dead URL")
	}
	<-done
	state := m.entry("gradio")
	if state.Status != "reconnecting" || state.URL != "" {
		t.Fatalf("stale public URL still online: %+v", state)
	}
}
func TestGradioNativeWatchKeepsHealthyLink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":"ok"}`)
	}))
	defer server.Close()
	m := NewTunnelManager()
	m.set(TunnelInfo{Provider: "gradio_native", Status: "online", URL: server.URL})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.watchGradioNative(ctx, "gradio_native", server.URL, cancel, 8*time.Millisecond); close(done) }()
	time.Sleep(75 * time.Millisecond)
	if ctx.Err() != nil {
		t.Fatal("live URL was incorrectly disconnected")
	}
	state := m.entry("gradio_native")
	if state.URL != server.URL || state.Status != "online" {
		t.Fatalf("unexpected status: %+v", state)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("monitor didn't stop")
	}
}

func TestGradioPublicHealthBasicAuth(t *testing.T) {
	for _, tc := range []struct {
		realm string
		valid bool
	}{
		{`Basic realm="Goradio"`, true},
		{`Basic realm="Otro servidor"`, false},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("WWW-Authenticate", tc.realm)
			http.Error(w, "authentication required", 401)
		}))
		result := gradioPublicHealth(context.Background(), server.URL)
		server.Close()
		if result != tc.valid {
			t.Errorf("realm %q => %v expected %v", tc.realm, result, tc.valid)
		}
	}
}
