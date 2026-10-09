package goradio

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testApp(t *testing.T, inputs []Component, outputs []Component, h Handler) *App {
	t.Helper()
	app, err := New(Config{Title: "Test", Inputs: inputs, Outputs: outputs}, h, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return app
}
func sendPredict(t *testing.T, app *App, data any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"data": data})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, httptest.NewRequest("POST", "/api/predict", bytes.NewReader(raw)))
	return rec
}
func TestPredict_ReturnsHandlerResult(t *testing.T) {
	app := testApp(t, []Component{{Type: "textbox", Required: true}}, []Component{{Type: "textbox"}}, BuiltinHandler("uppercase"))
	rec := sendPredict(t, app, []any{"hola"})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "HOLA") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
}
func TestPredict_RejectsInvalidInputWithoutPanic(t *testing.T) {
	cases := []struct {
		name  string
		input any
		c     Component
	}{
		{"entrada ausente", nil, Component{Type: "textbox", Required: true}},
		{"lista inesperada", []any{"foo"}, Component{Type: "textbox"}},
		{"dropdown falso", "No", Component{Type: "dropdown", Choices: []string{"Sí"}}},
		{"número textual", "tres", Component{Type: "number"}},
		{"archivo ajeno", "/api/media/../../secret", Component{Type: "image"}},
		{"casilla obligatoria", false, Component{Type: "checkbox", Required: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := testApp(t, []Component{tc.c}, []Component{{Type: "textbox"}}, BuiltinHandler("echo"))
			rec := sendPredict(t, app, []any{tc.input})
			if rec.Code != 400 {
				t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}
func TestUpload_ImageRoundTrip(t *testing.T) {
	app := testApp(t, []Component{{Type: "image", Required: true}}, []Component{{Type: "image"}}, func(_ context.Context, values []any) ([]any, error) { return values, nil })
	var body bytes.Buffer
	m := multipart.NewWriter(&body)
	part, err := m.CreateFormFile("file", "tiny.png")
	if err != nil {
		t.Fatal(err)
	}
	png := []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 0, 73, 72, 68, 82}
	if _, err := part.Write(png); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteField("kind", "image"); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/upload", &body)
	req.Header.Set("Content-Type", m.FormDataContentType())
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	var result map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(app.MediaDir, filepath.Base(result["url"]))); err != nil {
		t.Fatal(err)
	}
	predicted := sendPredict(t, app, []any{result["url"]})
	if predicted.Code != 200 {
		t.Fatalf("predict: %d %s", predicted.Code, predicted.Body.String())
	}
	if !strings.Contains(predicted.Body.String(), result["url"]) {
		t.Fatal("la imagen no se conservó")
	}
}
func TestUpload_RejectsWrongMediaKind(t *testing.T) {
	app := testApp(t, nil, []Component{{Type: "textbox"}}, BuiltinHandler("echo"))
	var body bytes.Buffer
	m := multipart.NewWriter(&body)
	f, err := m.CreateFormFile("file", "fake.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("no es una imagen")); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteField("kind", "image"); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/upload", &body)
	req.Header.Set("Content-Type", m.FormDataContentType())
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("status %d", rec.Code)
	}
}
func TestConfigSecurity_BasicAuth(t *testing.T) {
	t.Setenv("GORADIO_AUTH_USER", "demo")
	t.Setenv("GORADIO_AUTH_PASSWORD", "secreto")
	app := testApp(t, nil, []Component{{Type: "textbox"}}, BuiltinHandler("echo"))
	req := httptest.NewRequest("GET", "/api/config", nil)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatal(rec.Code)
	}
	req = httptest.NewRequest("GET", "/api/config", nil)
	req.SetBasicAuth("demo", "secreto")
	rec = httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal(rec.Code)
	}
}
func TestTunnelCommand_OnlySupportedProviders(t *testing.T) {
	for _, provider := range []string{"cloudflare", "localhostrun", "pinggy"} {
		_, args, err := tunnelCommand(provider, 7860)
		if err != nil || len(args) == 0 {
			t.Fatalf("provider=%s, err=%v", provider, err)
		}
	}
	if _, _, err := tunnelCommand("unknown", 7860); err == nil {
		t.Fatal("se esperaba error")
	}
}
func TestParseTunnelURL_RejectsWrongProvider(t *testing.T) {
	cases := []struct{ provider, raw, want string }{
		{"cloudflare", "route https://random.trycloudflare.com active", "https://random.trycloudflare.com"},
		{"localhostrun", "tunneled at https://bright.localhost.run", "https://bright.localhost.run"},
		{"pinggy", "https://example.a.pinggy.link", "https://example.a.pinggy.link"},
		{"cloudflare", "https://secret.localhost.run", ""},
		{"pinggy", "https://example.evil.com", ""},
	}
	for _, c := range cases {
		t.Run(c.provider+c.raw, func(t *testing.T) {
			if got := parseTunnelURL(c.provider, c.raw); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}
func TestAPIHealth(t *testing.T) {
	app := testApp(t, nil, []Component{{Type: "textbox"}}, BuiltinHandler("echo"))
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
}

func TestTunnelVerify_OnlineOnlyForGoradio(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		expected string
	}{
		{"servidor propio", 200, `{"status":"ok"}`, "online"},
		{"proxy externo", 403, "Prohibido", "starting"},
		{"otro JSON", 200, `{"status":"wrong"}`, "starting"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) }))
			defer remote.Close()
			manager := NewTunnelManager()
			manager.set(TunnelInfo{Provider: "cloudflare", Status: "starting"})
			writer := &tunnelWriter{m: manager, provider: "cloudflare", ready: make(chan struct{})}
			writer.verify(remote.URL)
			got := manager.entry("cloudflare")
			if got.Status != tc.expected {
				t.Fatalf("estado %q, esperado %q", got.Status, tc.expected)
			}
		})
	}
}
