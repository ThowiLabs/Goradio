package goradio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type TunnelInfo struct {
	Provider string `json:"provider"`
	Status   string `json:"status"`
	URL      string `json:"url,omitempty"`
	Error    string `json:"error,omitempty"`
}
type TunnelManager struct {
	mu      sync.RWMutex
	entries map[string]TunnelInfo
}

func NewTunnelManager() *TunnelManager { return &TunnelManager{entries: map[string]TunnelInfo{}} }
func (m *TunnelManager) Snapshot() []TunnelInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]TunnelInfo, 0, len(m.entries))
	for _, v := range m.entries {
		result = append(result, v)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Provider < result[j].Provider })
	return result
}
func (m *TunnelManager) set(v TunnelInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[v.Provider] = v
}
func (m *TunnelManager) Start(ctx context.Context, port int, providers []string) {
	selected := map[string]bool{}
	for _, p := range providers {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || selected[p] {
			continue
		}
		selected[p] = true
		name, args, err := tunnelCommand(p, port)
		if err != nil {
			m.set(TunnelInfo{Provider: p, Status: "error", Error: err.Error()})
			continue
		}
		m.set(TunnelInfo{Provider: p, Status: "starting"})
		go m.run(ctx, p, name, args)
	}
}
func tunnelCommand(provider string, port int) (string, []string, error) {
	local := fmt.Sprintf("localhost:%d", port)
	switch provider {
	case "cloudflare":
		return "cloudflared", []string{"tunnel", "--url", "http://127.0.0.1:" + fmt.Sprint(port)}, nil
	case "localhostrun":
		return "ssh", []string{"-T", "-o", "BatchMode=yes", "-o", "ServerAliveInterval=30", "-o", "StrictHostKeyChecking=accept-new", "-R", "80:" + local, "nokey@localhost.run"}, nil
	case "pinggy":
		return "ssh", []string{"-T", "-p", "443", "-o", "StrictHostKeyChecking=accept-new", "-o", "ServerAliveInterval=30", "-R", "0:" + local, "free.pinggy.io"}, nil
	case "localtunnel":
		return "lt", []string{"--port", fmt.Sprint(port)}, nil
	case "gradio":
		python := os.Getenv("GORADIO_PYTHON")
		if python == "" {
			python = "python"
		}
		return python, []string{"-u", "-m", "goradio.gradio_tunnel", "--port", fmt.Sprint(port)}, nil
	default:
		return "", nil, fmt.Errorf("proveedor desconocido: %s", provider)
	}
}

var publicURL = regexp.MustCompile(`https://[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9](?::[0-9]+)?`)

func parseTunnelURL(provider, s string) string {
	// Terminales pueden producir secuencias ANSI y texto alrededor.
	s = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`).ReplaceAllString(s, "")
	for _, candidate := range publicURL.FindAllString(s, -1) {
		u, err := url.Parse(candidate)
		if err != nil {
			continue
		}
		h := strings.ToLower(u.Hostname())
		allowed := false
		switch provider {
		case "cloudflare":
			allowed = strings.HasSuffix(h, ".trycloudflare.com")
		case "localhostrun":
			allowed = strings.HasSuffix(h, ".localhost.run") || strings.HasSuffix(h, ".lhr.rocks")
		case "pinggy":
			allowed = strings.HasSuffix(h, ".pinggy.link") || strings.HasSuffix(h, ".pinggy.io") || strings.HasSuffix(h, ".pinggy.online")
		case "localtunnel":
			allowed = strings.HasSuffix(h, ".loca.lt") || strings.HasSuffix(h, ".localtunnel.me")
		case "gradio":
			allowed = strings.HasSuffix(h, ".gradio.live")
		}
		if allowed {
			return candidate
		}
	}
	return ""
}

type tunnelWriter struct {
	m        *TunnelManager
	provider string
	mu       sync.Mutex
	buffer   string
	seen     map[string]bool
	ready    chan struct{}
	once     sync.Once
}

func (t *tunnelWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buffer += string(p)
	if len(t.buffer) > 8192 {
		t.buffer = t.buffer[len(t.buffer)-4096:]
	}
	if found := parseTunnelURL(t.provider, t.buffer); found != "" {
		if t.seen == nil {
			t.seen = make(map[string]bool)
		}
		if !t.seen[found] {
			t.seen[found] = true
			go t.verify(found)
		}
	}
	return len(p), nil
}
func (t *tunnelWriter) verify(candidate string) {
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	for attempt := 0; attempt < 5; attempt++ {
		resp, err := client.Get(candidate + "/api/health")
		if err != nil {
			if attempt == 4 {
				log.Printf("Túnel %s no disponible: %v", t.provider, err)
				return
			}
			time.Sleep(2 * time.Second)
			continue
		}
		valid := false
		if resp.StatusCode == http.StatusOK {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 100))
			valid = readErr == nil && strings.Contains(string(body), `"status":"ok"`)
		} else if resp.StatusCode == http.StatusUnauthorized && strings.Contains(resp.Header.Get("WWW-Authenticate"), "Goradio") {
			valid = true
		}
		resp.Body.Close()
		if !valid {
			log.Printf("Túnel %s no verificado (HTTP %d): %s", t.provider, resp.StatusCode, candidate)
			return
		}
		t.m.set(TunnelInfo{Provider: t.provider, Status: "online", URL: candidate})
		log.Printf("Goradio público verificado (%s): %s", t.provider, candidate)
		t.once.Do(func() { close(t.ready) })
		return
	}
}
func (m *TunnelManager) entry(p string) TunnelInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.entries[p]
}
func (m *TunnelManager) run(ctx context.Context, provider, name string, args []string) {
	if _, err := exec.LookPath(name); err != nil {
		m.set(TunnelInfo{Provider: provider, Status: "error", Error: "No se encontró " + name})
		return
	}
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var cmd *exec.Cmd
	var childInput io.WriteCloser
	if provider == "gradio" {
		cmd = exec.Command(name, args...)
		input, err := cmd.StdinPipe()
		if err != nil {
			m.set(TunnelInfo{Provider: provider, Status: "error", Error: err.Error()})
			return
		}
		childInput = input
	} else {
		cmd = exec.CommandContext(childCtx, name, args...)
	}
	writer := &tunnelWriter{m: m, provider: provider, ready: make(chan struct{})}
	go func() {
		timer := time.NewTimer(35 * time.Second)
		defer timer.Stop()
		select {
		case <-writer.ready:
		case <-timer.C:
			cancel()
		case <-ctx.Done():
		}
	}()
	cmd.Stdout = writer
	cmd.Stderr = writer
	finished := make(chan struct{})
	if childInput != nil {
		go func() {
			<-childCtx.Done()
			childInput.Close()
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				if cmd.Process != nil {
					if err := cmd.Process.Kill(); err != nil {
						log.Printf("finalizar Gradio: %v", err)
					}
				}
			}
		}()
	}
	err := cmd.Run()
	close(finished)
	if ctx.Err() != nil {
		m.set(TunnelInfo{Provider: provider, Status: "closed"})
		return
	}
	if childCtx.Err() != nil && m.entry(provider).Status != "online" {
		m.set(TunnelInfo{Provider: provider, Status: "error", Error: "No se pudo verificar una URL pública en 35 segundos"})
		return
	}
	if err == nil {
		err = io.EOF
	}
	status := "error"
	if errors.Is(err, io.EOF) {
		status = "closed"
	}
	m.set(TunnelInfo{Provider: provider, Status: status, Error: err.Error()})
}
func (m *TunnelManager) WaitForURL(provider string, timeout time.Duration) string {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			info := m.entry(provider)
			if info.URL != "" {
				return info.URL
			}
			if info.Status == "error" {
				return ""
			}
		case <-deadline.C:
			return ""
		}
	}
}
