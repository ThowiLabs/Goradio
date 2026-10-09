package goradio

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func secureApp(t *testing.T, dbPath string, requireLogin bool) *App {
	t.Helper()
	app := testApp(t, []Component{{Type: "textbox", Label: "Nota"}, {Type: "number", Label: "Cantidad"}}, []Component{{Type: "textbox", Label: "Resultado"}}, func(_ context.Context, v []any) ([]any, error) { return []any{v[0]}, nil })
	// t.TempDir del App es solo media; estado de SQLite se guarda fuera y se reabre.
	err := app.EnableSecurity(SecurityConfig{DBPath: dbPath, AppID: "session-test", RequireLogin: requireLogin, PersistInputs: true, PersistOutputs: true})
	if err != nil {
		t.Fatal(err)
	}
	return app
}
func serveSecure(app *App, method, path string, body any, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	var src []byte
	if body != nil {
		src, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(src))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrf != "" {
		req.Header.Set("X-Goradio-CSRF", csrf)
	}
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	return rec
}
func loginSecure(t *testing.T, app *App, user, password string) (*http.Cookie, string) {
	t.Helper()
	r := serveSecure(app, "POST", "/auth/login", map[string]string{"username": user, "password": password}, nil, "")
	if r.Code != 200 {
		t.Fatalf("login %s %d %s", user, r.Code, r.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range r.Result().Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("cookie de sesión ausente")
	}
	me := serveSecure(app, "GET", "/auth/me", nil, cookie, "")
	if me.Code != 200 {
		t.Fatal(me.Body.String())
	}
	var value struct {
		CSRF string `json:"csrf"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return cookie, value.CSRF
}
func TestSecurityAccountsIsolationAndSessionAfterRestart(t *testing.T) {
	db := filepath.Join(t.TempDir(), "goradio.sqlite")
	a := secureApp(t, db, true)
	if err := a.CreateUser(context.Background(), "alice", "password-of-alice-2026", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := a.CreateUser(context.Background(), "bob", "password-of-bob-2026", "user"); err != nil {
		t.Fatal(err)
	}
	unauthorized := serveSecure(a, "GET", "/config", nil, nil, "")
	if unauthorized.Code != 401 {
		t.Fatalf("sin login HTTP %d", unauthorized.Code)
	}
	alice, csrfAlice := loginSecure(t, a, "alice", "password-of-alice-2026")
	bob, _ := loginSecure(t, a, "bob", "password-of-bob-2026")
	save := serveSecure(a, "PUT", "/api/state", stateSnapshot{Inputs: []any{"nota secreta", 7}, Outputs: []any{"resultado secreto"}}, alice, csrfAlice)
	if save.Code != 200 {
		t.Fatalf("guardar estado: %d %s", save.Code, save.Body.String())
	}
	bobRead := serveSecure(a, "GET", "/api/state", nil, bob, "")
	if bobRead.Code != 200 || strings.Contains(bobRead.Body.String(), "nota secreta") {
		t.Fatalf("filtración entre usuarios: %d %s", bobRead.Code, bobRead.Body.String())
	}
	badCSRF := serveSecure(a, "DELETE", "/api/state", nil, alice, "CSRF-FALSO")
	if badCSRF.Code != 403 {
		t.Fatalf("CSRF aceptado %d", badCSRF.Code)
	}
	mustSave := serveSecure(a, "GET", "/config", nil, alice, "")
	if !strings.Contains(mustSave.Body.String(), "nota secreta") {
		t.Fatalf("Config no restaura valores: %s", mustSave.Body.String())
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := secureApp(t, db, true)
	defer restarted.Close()
	last := serveSecure(restarted, "GET", "/api/state", nil, alice, "")
	if last.Code != 200 || !strings.Contains(last.Body.String(), "nota secreta") {
		t.Fatalf("No persiste tras reinicio: %d %s", last.Code, last.Body.String())
	}
	logout := serveSecure(restarted, "POST", "/auth/logout", nil, alice, csrfAlice)
	if logout.Code != 200 {
		t.Fatal(logout.Body.String())
	}
	after := serveSecure(restarted, "GET", "/api/state", nil, alice, "")
	if after.Code != 401 {
		t.Fatalf("Logout no invalida sesión %d", after.Code)
	}
}
func TestSecurityGuestPersistsOnReloadAndRestarts(t *testing.T) {
	db := filepath.Join(t.TempDir(), "guest.sqlite")
	a := secureApp(t, db, false)
	root := serveSecure(a, "GET", "/", nil, nil, "")
	if root.Code != 200 {
		t.Fatalf("Frontend %d", root.Code)
	}
	var guest *http.Cookie
	for _, c := range root.Result().Cookies() {
		if c.Name == guestCookie {
			guest = c
		}
	}
	if guest == nil {
		t.Fatal("No hay cookie de invitado persistente")
	}
	who := serveSecure(a, "GET", "/auth/me", nil, guest, "")
	var me struct {
		CSRF string `json:"csrf"`
	}
	if err := json.Unmarshal(who.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	saved := serveSecure(a, "PUT", "/api/state", stateSnapshot{Inputs: []any{"borrador no enviado", 4}, Outputs: []any{nil}}, guest, me.CSRF)
	if saved.Code != 200 {
		t.Fatalf("Guardar invitado: %d %s", saved.Code, saved.Body.String())
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	b := secureApp(t, db, false)
	defer b.Close()
	loaded := serveSecure(b, "GET", "/config", nil, guest, "")
	if !strings.Contains(loaded.Body.String(), "borrador no enviado") {
		t.Fatal("El formulario no se restaura tras reinicio")
	}
	stranger := serveSecure(b, "GET", "/config", nil, nil, "")
	if strings.Contains(stranger.Body.String(), "borrador no enviado") {
		t.Fatal("Datos de invitado visibles a otro invitado")
	}
}
func TestNativeGradioAutoSaveNoSubmit(t *testing.T) {
	a := secureApp(t, filepath.Join(t.TempDir(), "auto.db"), false)
	defer a.Close()
	root := serveSecure(a, "GET", "/config", nil, nil, "")
	var guest *http.Cookie
	for _, c := range root.Result().Cookies() {
		if c.Name == guestCookie {
			guest = c
		}
	}
	if guest == nil {
		t.Fatal("Sin invitado")
	}
	cfg := map[string]any{}
	if err := json.Unmarshal(root.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	deps := cfg["dependencies"].([]any)
	if len(deps) < 4 {
		t.Fatalf("faltan eventos de autoguardado: %d", len(deps))
	}
	changed := serveSecure(a, "POST", "/gradio_api/run/predict", map[string]any{"fn_index": 2, "data": []any{"escribiendo sin submit"}}, guest, "")
	if changed.Code != 200 {
		t.Fatalf("Evento change: %d %s", changed.Code, changed.Body.String())
	}
	refreshed := serveSecure(a, "GET", "/config", nil, guest, "")
	if !strings.Contains(refreshed.Body.String(), "escribiendo sin submit") {
		t.Fatalf("Gradio no recupera: %s", refreshed.Body.String())
	}
	clear := serveSecure(a, "POST", "/gradio_api/run/predict", map[string]any{"fn_index": 1, "data": []any{}}, guest, "")
	if clear.Code != 200 {
		t.Fatalf("Clear HTTP %d", clear.Code)
	}
	empty := serveSecure(a, "GET", "/api/state", nil, guest, "")
	if strings.Contains(empty.Body.String(), "escribiendo sin submit") {
		t.Fatal("Clear no eliminó el estado durable")
	}
}

func TestSecurityMediaFilesBelongToTheirOwner(t *testing.T) {
	db := filepath.Join(t.TempDir(), "media.sqlite")
	app := secureApp(t, db, true)
	defer app.Close()
	for _, user := range []string{"ana", "beto"} {
		if err := app.CreateUser(context.Background(), user, "secure-password-"+user+"-2026", "user"); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := loginSecure(t, app, "ana", "secure-password-ana-2026")
	b, _ := loginSecure(t, app, "beto", "secure-password-beto-2026")
	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)
	part, err := multipartWriter.CreateFormFile("files", "example.txt")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("archivo confidencial"))
	multipartWriter.Close()
	request := httptest.NewRequest("POST", "/gradio_api/upload", &body)
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	request.AddCookie(a)
	uploaded := httptest.NewRecorder()
	app.Routes().ServeHTTP(uploaded, request)
	if uploaded.Code != 200 {
		t.Fatalf("Carga autorizada: %d %s", uploaded.Code, uploaded.Body.String())
	}
	var files []string
	if err := json.Unmarshal(uploaded.Body.Bytes(), &files); err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files=%v", files)
	}
	own := serveSecure(app, "GET", "/gradio_api/file="+files[0], nil, a, "")
	if own.Code != 200 {
		t.Fatalf("Owner: %d", own.Code)
	}
	stranger := serveSecure(app, "GET", "/gradio_api/file="+files[0], nil, b, "")
	if stranger.Code != 404 {
		t.Fatalf("Otro usuario puede leer el archivo: %d", stranger.Code)
	}
}

func TestPasswordMinimumFourAndNonEmpty(t *testing.T) {
	for _, v := range []string{"", " ", "   ", "123", "abc"} {
		if _, err := passwordHash(v); err == nil {
			t.Fatalf("Contraseña demasiado corta o vacía aceptada: %q", v)
		}
	}
	for _, v := range []string{"1234", "abcd", "abcd5678"} {
		encoded, err := passwordHash(v)
		if err != nil {
			t.Fatalf("Contraseña %q rechazada: %v", v, err)
		}
		if !verifyPassword(encoded, v) {
			t.Fatalf("La contraseña de 4+ caracteres no se pudo verificar: %q", v)
		}
	}
}
func TestSecurityFourDigitPINAndEmptyLogin(t *testing.T) {
	app := secureApp(t, filepath.Join(t.TempDir(), "pin.sqlite"), true)
	defer app.Close()
	if err := app.CreateUser(context.Background(), "pinuser", "1234", "user"); err != nil {
		t.Fatal(err)
	}
	if err := app.CreateUser(context.Background(), "nobody", "", "user"); err == nil {
		t.Fatal("Se creó cuenta con contraseña vacía")
	}
	for _, fields := range []map[string]string{
		{"username": "pinuser", "password": ""},
		{"username": "", "password": "1234"},
		{"username": "  ", "password": "   "},
	} {
		resp := serveSecure(app, http.MethodPost, "/auth/login", fields, nil, "")
		if resp.Code != http.StatusBadRequest {
			t.Fatalf("Se aceptaron campos vacíos: %d, %v", resp.Code, fields)
		}
	}
	valid := serveSecure(app, http.MethodPost, "/auth/login", map[string]string{"username": "pinuser", "password": "1234"}, nil, "")
	if valid.Code != http.StatusOK {
		t.Fatalf("PIN 1234 no autentica: %d %s", valid.Code, valid.Body.String())
	}
}
func TestLegacyBasicOptionalButNoPartialCredentials(t *testing.T) {
	app := testApp(t, nil, []Component{{Type: "textbox"}}, BuiltinHandler("echo"))
	t.Setenv("GORADIO_AUTH_USER", "")
	t.Setenv("GORADIO_AUTH_PASSWORD", "")
	ok := httptest.NewRecorder()
	app.Routes().ServeHTTP(ok, httptest.NewRequest("GET", "/api/health", nil))
	if ok.Code != 200 {
		t.Fatalf("Sin credenciales debería estar desactivado Basic: %d", ok.Code)
	}
	t.Setenv("GORADIO_AUTH_USER", "demo")
	incomplete := httptest.NewRecorder()
	app.Routes().ServeHTTP(incomplete, httptest.NewRequest("GET", "/api/health", nil))
	if incomplete.Code != http.StatusServiceUnavailable {
		t.Fatalf("Usuario sin contraseña no debe activar autenticación incompleta: %d", incomplete.Code)
	}
	t.Setenv("GORADIO_AUTH_PASSWORD", "123")
	tooShort := httptest.NewRecorder()
	app.Routes().ServeHTTP(tooShort, httptest.NewRequest("GET", "/api/health", nil))
	if tooShort.Code != http.StatusServiceUnavailable {
		t.Fatalf("Basic con contraseña menor a cuatro caracteres debe rechazarse: %d", tooShort.Code)
	}
	t.Setenv("GORADIO_AUTH_PASSWORD", "1234")
	missing := httptest.NewRecorder()
	app.Routes().ServeHTTP(missing, httptest.NewRequest("GET", "/api/health", nil))
	if missing.Code != http.StatusUnauthorized {
		t.Fatalf("Basic configurado sin credenciales enviadas: %d", missing.Code)
	}
	request := httptest.NewRequest("GET", "/api/health", nil)
	request.SetBasicAuth("demo", "1234")
	authorized := httptest.NewRecorder()
	app.Routes().ServeHTTP(authorized, request)
	if authorized.Code != http.StatusOK {
		t.Fatalf("Basic PIN de 4 caracteres no funciona: %d", authorized.Code)
	}
}
