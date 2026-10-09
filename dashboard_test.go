package goradio

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func demoDashboard(t *testing.T) *Dashboard {
	t.Helper()
	app, err := NewDashboard("Mi Panel",
		Window{Path: "/", Title: "Inicio", Icon: "dashboard", Config: Config{
			Inputs:  []Component{{Type: "textbox", Label: "Texto"}},
			Outputs: []Component{{Type: "textbox", Label: "Resultado"}},
		}, Handler: func(_ context.Context, items []any) ([]any, error) {
			return []any{items[0]}, nil
		}},
		Window{Path: "/imagenes", Title: "Imágenes", Icon: "image", Group: "Herramientas", Config: Config{
			Inputs:  []Component{{Type: "textbox", Label: "Imagen"}},
			Outputs: []Component{{Type: "textbox", Label: "Salida"}},
		}, Handler: func(_ context.Context, items []any) ([]any, error) {
			return []any{strings.ToUpper(items[0].(string))}, nil
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return app
}
func TestDashboardSidebarActiveAndPageRouting(t *testing.T) {
	t.Setenv("GORADIO_AUTH_USER", "")
	t.Setenv("GORADIO_AUTH_PASSWORD", "")
	d := demoDashboard(t)
	handler := d.Routes()
	for _, tc := range []struct{ path, active string }{
		{"/", "Inicio"}, {"/imagenes", "Imágenes"},
	} {
		req := httptest.NewRequest("GET", tc.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s %d %s", tc.path, rec.Code, rec.Body.String()[:min(150, rec.Body.Len())])
		}
		html := rec.Body.String()
		if !strings.Contains(html, "gd-sidebar") || !strings.Contains(html, "gd-menu-toggle") ||
			!strings.Contains(html, "aria-current=\"page\"") {
			t.Fatal("Falta sidebar o elemento activo")
		}
		if !strings.Contains(html, tc.active) {
			t.Fatalf("Missing window %s", tc.active)
		}
	}
	unknown := httptest.NewRequest("GET", "/does-not-exist", nil)
	unknown.Header.Set("Accept", "text/html")
	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, unknown)
	if missing.Code != 404 {
		t.Fatalf("Unknown path should 404: %d", missing.Code)
	}
}
func TestDashboardRelativeGradioAPIAndIndependentHandlers(t *testing.T) {
	d := demoDashboard(t)
	handler := d.Routes()
	requests := []string{"/imagenes/gradio_api/info", "/gradio_api/info"}
	for _, path := range requests {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Referer", "http://example.com/imagenes")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != 200 {
			t.Fatalf("%s => %d: %s", path, res.Code, res.Body.String())
		}
	}
	body, _ := json.Marshal(map[string]any{"fn_index": 0, "data": []any{"gradio image"}})
	for _, path := range []string{"/imagenes/gradio_api/run/predict", "/gradio_api/run/predict"} {
		req := httptest.NewRequest("POST", path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Goradio-Page", "/imagenes")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != 200 || !strings.Contains(res.Body.String(), "GRADIO IMAGE") {
			t.Fatalf("No routing on %s: %d %s", path, res.Code, res.Body.String())
		}
	}
	wrong := httptest.NewRequest("GET", "/imagenes/gradio_api/info", nil)
	wrong.Header.Set("X-Goradio-Page", "/")
	bad := httptest.NewRecorder()
	handler.ServeHTTP(bad, wrong)
	if bad.Code != 400 {
		t.Fatalf("Conflicting page must reject: %d", bad.Code)
	}
}

func dashboardRequest(t *testing.T, h http.Handler, method, path string, body any, cookie *http.Cookie, csrf, page, referer string) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrf != "" {
		req.Header.Set("X-Goradio-CSRF", csrf)
	}
	if page != "" {
		req.Header.Set("X-Goradio-Page", page)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
func TestDashboardSharedLoginAndIndependentPersistence(t *testing.T) {
	t.Setenv("GORADIO_AUTH_USER", "")
	t.Setenv("GORADIO_AUTH_PASSWORD", "")
	d := demoDashboard(t)
	db := filepath.Join(t.TempDir(), "dashboard.sqlite")
	if err := d.EnableSecurity(SecurityConfig{DBPath: db, AppID: "dash-test", RequireLogin: true, PersistInputs: true, PersistOutputs: true}); err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.CreateUser(context.Background(), "dashboard-user", "strong-password-2026", "admin"); err != nil {
		t.Fatal(err)
	}
	h := d.Routes()
	for _, path := range []string{"/", "/imagenes"} {
		result := dashboardRequest(t, h, "GET", path, nil, nil, "", "", "")
		if path == "/" {
			if result.Code != 303 || result.Header().Get("Location") != "/login" {
				t.Fatalf("root login redirect: %d %s", result.Code, result.Header().Get("Location"))
			}
		} else if result.Code != 303 || result.Header().Get("Location") != "/login" {
			t.Fatalf("protected page %s was not redirected: %d %s", path, result.Code, result.Header().Get("Location"))
		}
	}
	login := dashboardRequest(t, h, "POST", "/auth/login", map[string]string{"username": "dashboard-user", "password": "strong-password-2026"}, nil, "", "", "")
	if login.Code != 200 {
		t.Fatalf("login %d %s", login.Code, login.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range login.Result().Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("login session cookie missing")
	}
	me := dashboardRequest(t, h, "GET", "/auth/me", nil, cookie, "", "/imagenes", "")
	if me.Code != 200 {
		t.Fatalf("shared /auth/me: %d %s", me.Code, me.Body.String())
	}
	var token struct {
		CSRF string `json:"csrf"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &token); err != nil {
		t.Fatal(err)
	}
	if token.CSRF == "" {
		t.Fatal("missing CSRF token")
	}
	for _, page := range []string{"/", "/imagenes"} {
		pageResp := dashboardRequest(t, h, "GET", page, nil, cookie, "", "", "")
		if pageResp.Code != 200 {
			t.Fatalf("page %s authenticated %d", page, pageResp.Code)
		}
		if pageResp.Header().Get("Referrer-Policy") != "same-origin" {
			t.Fatalf("page %s referrer policy: %s", page, pageResp.Header().Get("Referrer-Policy"))
		}
	}
	first := dashboardRequest(t, h, "PUT", "/api/state", stateSnapshot{Inputs: []any{"personal home"}, Outputs: []any{"output home"}}, cookie, token.CSRF, "/", "")
	if first.Code != 200 {
		t.Fatalf("save home %d %s", first.Code, first.Body.String())
	}
	image := dashboardRequest(t, h, "PUT", "/imagenes/api/state", stateSnapshot{Inputs: []any{"personal image"}, Outputs: []any{"output image"}}, cookie, token.CSRF, "", "http://example.com/imagenes")
	if image.Code != 200 {
		t.Fatalf("save image %d %s", image.Code, image.Body.String())
	}
	home := dashboardRequest(t, h, "GET", "/api/state", nil, cookie, "", "/", "")
	if !strings.Contains(home.Body.String(), "personal home") || strings.Contains(home.Body.String(), "personal image") {
		t.Fatalf("home state mixed: %s", home.Body.String())
	}
	other := dashboardRequest(t, h, "GET", "/api/state", nil, cookie, "", "/imagenes", "")
	if !strings.Contains(other.Body.String(), "personal image") || strings.Contains(other.Body.String(), "personal home") {
		t.Fatalf("other page state mixed: %s", other.Body.String())
	}
	// A valid cookie must remain usable on the other window and after a restart.
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	reloaded := demoDashboard(t)
	if err := reloaded.EnableSecurity(SecurityConfig{DBPath: db, AppID: "dash-test", RequireLogin: true, PersistInputs: true, PersistOutputs: true}); err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	read := dashboardRequest(t, reloaded.Routes(), "GET", "/api/state", nil, cookie, "", "/imagenes", "")
	if read.Code != 200 || !strings.Contains(read.Body.String(), "personal image") {
		t.Fatalf("dashboard state lost after restart: %d %s", read.Code, read.Body.String())
	}
	logout := dashboardRequest(t, reloaded.Routes(), "POST", "/auth/logout", nil, cookie, token.CSRF, "/", "")
	if logout.Code != 200 {
		t.Fatalf("logout %d %s", logout.Code, logout.Body.String())
	}
	blocked := dashboardRequest(t, reloaded.Routes(), "GET", "/imagenes", nil, cookie, "", "", "")
	if blocked.Code != 303 {
		t.Fatalf("logout did not invalidate all pages: %d", blocked.Code)
	}
}
func TestDashboardRouteValidationAndPageIsolation(t *testing.T) {
	d := demoDashboard(t)
	empty := Window{Path: "/extra", Title: "Extra", Config: Config{Outputs: []Component{{Type: "textbox"}}}, Handler: func(_ context.Context, _ []any) ([]any, error) { return []any{"x"}, nil }}
	if err := d.Add(empty); err != nil {
		t.Fatal(err)
	}
	if err := d.Add(empty); err == nil {
		t.Fatal("duplicate page was accepted")
	}
	for _, path := range []string{"/auth", "/api", "/assets", "/favicon.ico", "/BadPath", "/x/second"} {
		x := empty
		x.Path = path
		if err := d.Add(x); err == nil {
			t.Errorf("invalid/reserved route %q accepted", path)
		}
	}
	d2 := demoDashboard(t)
	if err := d2.EnableSecurity(SecurityConfig{DBPath: filepath.Join(t.TempDir(), "another.db"), AppID: strings.Repeat("a", 80)}); err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	for _, a := range d2.apps {
		if len(a.security.cfg.AppID) > 80 {
			t.Fatalf("long state identifier: %s", a.security.cfg.AppID)
		}
	}
}
func TestDashboardSSEUsesRefererAndRejectsCrossPageLookup(t *testing.T) {
	d := demoDashboard(t)
	h := d.Routes()
	// EventSource cannot send X-Goradio-Page. The browser uses Referer
	// same-origin, which must direct each event ID to the correct app.
	reqBody := map[string]any{"fn_index": 0, "data": []any{"sse image"}}
	submit := dashboardRequest(t, h, "POST", "/gradio_api/call/predict", reqBody, nil, "", "/imagenes", "")
	if submit.Code != 200 {
		t.Fatalf("call %d %s", submit.Code, submit.Body.String())
	}
	var job map[string]string
	if err := json.Unmarshal(submit.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	eventID := job["event_id"]
	if eventID == "" {
		t.Fatal("missing job id")
	}
	bad := dashboardRequest(t, h, "GET", "/gradio_api/call/predict/"+eventID, nil, nil, "", "/", "")
	if bad.Code != 404 {
		t.Fatalf("other page accessed event ID: %d", bad.Code)
	}
	good := dashboardRequest(t, h, "GET", "/gradio_api/call/predict/"+eventID, nil, nil, "", "", "http://example.com/imagenes")
	if good.Code != 200 || !strings.Contains(good.Body.String(), "SSE IMAGE") {
		t.Fatalf("SSE not routed by referer: %d %s", good.Code, good.Body.String())
	}
}

func TestDashboardNestedGradioFontAssets(t *testing.T) {
	d := demoDashboard(t)
	for _, path := range []string{
		"/static/fonts/SourceSansPro/SourceSansPro-Regular.woff2",
		"/imagenes/static/fonts/SourceSansPro/SourceSansPro-Regular.woff2",
	} {
		result := dashboardRequest(t, d.Routes(), "GET", path, nil, nil, "", "", "")
		if result.Code != http.StatusOK || result.Body.Len() < 500 {
			t.Fatalf("Font %s failed: HTTP %d len %d %s", path, result.Code, result.Body.Len(), result.Body.String()[:min(60, result.Body.Len())])
		}
	}
}

func TestDashboardPublicAuthProbeDoesNotReturn404(t *testing.T) {
	d := demoDashboard(t)
	r := dashboardRequest(t, d.Routes(), "GET", "/auth/me", nil, nil, "", "", "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"authenticated":false`) {
		t.Fatalf("anonymous /auth/me should return 200: %d %s", r.Code, r.Body.String())
	}
}

func TestDashboardPreservesOriginalGradioFrontend(t *testing.T) {
	d := demoDashboard(t)
	h := d.Routes()
	for _, path := range []string{"/", "/imagenes"} {
		res := dashboardRequest(t, h, "GET", path, nil, nil, "", "", "")
		if res.Code != http.StatusOK {
			t.Fatalf("%s status %d", path, res.Code)
		}
		page := res.Body.String()
		for _, marker := range []string{
			"<gradio-app", "/assets/index-D88pgWqG.js", "/assets/index-DuOipanW.css",
			`aria-label="Ventanas de Goradio"`, `id="gd-sidebar-collapse"`,
			`id="gd-menu-toggle"`, `id="gd-theme-color"`, `id="gd-theme-secondary"`,
			`id="gd-theme-reset"`, `<svg aria-hidden="true" viewBox="0 0 24 24"><path d="m15 5-7 7 7 7"></path></svg>`,
			"goradio-dashboard.css", `aria-current="page"`,
		} {
			if !strings.Contains(page, marker) {
				t.Errorf("Missing Gradio/sidebar marker %q on %s", marker, path)
			}
		}
		for _, forbidden := range []string{"gd-page-header", "gd-topbar", "gd-content", "gd-status"} {
			if strings.Contains(page, forbidden) {
				t.Errorf("Unexpected administrative element %s on %s", forbidden, path)
			}
		}
	}
	css := dashboardRequest(t, h, "GET", "/goradio-dashboard.css", nil, nil, "", "", "")
	if css.Code != 200 {
		t.Fatalf("stylesheet HTTP %d", css.Code)
	}
	style := css.Body.String()
	if strings.Contains(style, ".gradio-container") || strings.Contains(style, "gradio-app .") {
		t.Fatal("Sidebar CSS must not override Gradio's theme or inner container")
	}
	for _, token := range []string{
		"var(--background-fill-primary", "var(--body-text-color", "var(--color-accent)",
		"var(--border-color-primary)", "var(--font,", ".gd-sidebar-collapse svg",
		"display:grid;place-items:center",
	} {
		if !strings.Contains(style, token) {
			t.Errorf("Sidebar must inherit original Gradio token or centered SVG: %s", token)
		}
	}
	if strings.Contains(style, "background: #18253b") {
		t.Fatal("Sidebar must not have a hard-coded color scheme")
	}
	js := dashboardRequest(t, h, "GET", "/goradio-dashboard.js", nil, nil, "", "", "")
	if js.Code != 200 {
		t.Fatalf("Sidebar JS status %d", js.Code)
	}
	for _, marker := range []string{
		"goradio.sidebar.collapsed.v1", "goradio.theme.", "primary",
		"secondary", "localStorage.setItem", "localStorage.removeItem",
		"applyPalette", "restorePalette",
	} {
		if !strings.Contains(js.Body.String(), marker) {
			t.Errorf("Missing theme persistence and restoration marker %q", marker)
		}
	}
}
