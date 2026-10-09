package goradio

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
	_ "modernc.org/sqlite"
)

// SecurityConfig configura las cuentas y el almacenamiento persistente.
// SQLite se guarda en disco. Nunca utilices un directorio temporal para DBPath.
type SecurityConfig struct {
	DBPath         string
	AppID          string
	RequireLogin   bool
	AllowSignup    bool
	SessionTTL     time.Duration
	PersistInputs  bool
	PersistOutputs bool
}
type Identity struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Guest    bool   `json:"guest"`
}
type ctxIdentityKey struct{}

func IdentityFromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxIdentityKey{}).(Identity)
	return id, ok
}

type securityBackend struct {
	cfg      SecurityConfig
	db       *sql.DB
	authMu   sync.Mutex
	failures map[string][]time.Time
}
type stateSnapshot struct {
	Inputs    []any `json:"inputs"`
	Outputs   []any `json:"outputs"`
	UpdatedAt int64 `json:"updated_at"`
}

var validUsername = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,40}$`)
var validAppID = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,80}$`)

const sessionCookie = "goradio_session"
const guestCookie = "goradio_guest"

func (a *App) EnableSecurity(cfg SecurityConfig) error {
	if a.security != nil {
		return errors.New("seguridad ya configurada")
	}
	if cfg.DBPath == "" {
		return errors.New("SecurityConfig.DBPath debe apuntar a un archivo persistente")
	}
	if cfg.AppID == "" {
		sum := sha256.Sum256([]byte(a.Config.Title))
		cfg.AppID = "app_" + hex.EncodeToString(sum[:8])
	}
	if !validAppID.MatchString(cfg.AppID) {
		return errors.New("AppID inválido")
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 30 * 24 * time.Hour
	}
	if cfg.SessionTTL > 365*24*time.Hour {
		return errors.New("SessionTTL demasiado largo")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0700); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, query := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		`CREATE TABLE IF NOT EXISTS goradio_users (
    id TEXT PRIMARY KEY, username TEXT UNIQUE COLLATE NOCASE NOT NULL,
    password_hash TEXT NOT NULL, role TEXT NOT NULL, created_at INTEGER NOT NULL
  )`,
		`CREATE TABLE IF NOT EXISTS goradio_sessions (
    token_hash TEXT PRIMARY KEY, user_id TEXT,
    csrf TEXT NOT NULL, expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL,
    FOREIGN KEY (user_id) REFERENCES goradio_users(id)
  )`,
		`CREATE TABLE IF NOT EXISTS goradio_guest_sessions (
    token_hash TEXT PRIMARY KEY, csrf TEXT NOT NULL, expires_at INTEGER NOT NULL
  )`,
		`CREATE TABLE IF NOT EXISTS goradio_form_state (
    app_id TEXT NOT NULL, identity_id TEXT NOT NULL, schema_hash TEXT NOT NULL,
    inputs_json TEXT NOT NULL DEFAULT '[]', outputs_json TEXT NOT NULL DEFAULT '[]',
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (app_id, identity_id)
  )`,
		`CREATE TABLE IF NOT EXISTS goradio_files (
    file_id TEXT PRIMARY KEY, app_id TEXT NOT NULL, identity_id TEXT NOT NULL,
    created_at INTEGER NOT NULL
  )`,
	} {
		if _, err = db.ExecContext(ctx, query); err != nil {
			db.Close()
			return fmt.Errorf("inicializar SQLite: %w", err)
		}
	}
	a.security = &securityBackend{cfg: cfg, db: db, failures: map[string][]time.Time{}}
	return nil
}
func (a *App) Close() error {
	if a.security == nil {
		return nil
	}
	err := a.security.db.Close()
	a.security = nil
	return err
}
func passwordHash(password string) (string, error) {
	// Permite claves simples como un PIN de cuatro dígitos; el almacenamiento
	// sigue protegido con sal y Argon2id.
	// El campo vacío nunca genera una contraseña válida.
	if strings.TrimSpace(password) == "" {
		return "", errors.New("la contraseña no puede estar vacía")
	}
	if len([]rune(password)) < 4 || len(password) > 1024 {
		return "", errors.New("la contraseña debe contener al menos 4 caracteres (máximo 1024 bytes)")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	digest := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return fmt.Sprintf("argon2id$v=19$m=65536,t=3,p=2$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(digest)), nil
}
func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" || parts[1] != "v=19" || parts[2] != "m=65536,t=3,p=2" {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(salt) != 16 {
		return false
	}
	expect, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(expect) != 32 {
		return false
	}
	result := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(result, expect) == 1
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
func (a *App) CreateUser(ctx context.Context, username, password, role string) error {
	if a.security == nil {
		return errors.New("habilita seguridad primero")
	}
	if !validUsername.MatchString(username) {
		return errors.New("usuario: usa 3-40 caracteres alfanuméricos, ._-")
	}
	if role != "admin" && role != "user" {
		return errors.New("rol inválido")
	}
	hashed, err := passwordHash(password)
	if err != nil {
		return err
	}
	id, err := randomToken()
	if err != nil {
		return err
	}
	_, err = a.security.db.ExecContext(ctx, "INSERT INTO goradio_users(id,username,password_hash,role,created_at) VALUES(?,?,?,?,?)", id, username, hashed, role, time.Now().Unix())
	return err
}
func (a *App) UserExists(ctx context.Context, username string) (bool, error) {
	if a.security == nil {
		return false, errors.New("habilita seguridad primero")
	}
	var count int
	err := a.security.db.QueryRowContext(ctx, "SELECT count(*) FROM goradio_users WHERE username=?", username).Scan(&count)
	return count > 0, err
}
func (a *App) userLogin(ctx context.Context, username, password string) (Identity, error) {
	var id, hash, role string
	err := a.security.db.QueryRowContext(ctx, "SELECT id,password_hash,role FROM goradio_users WHERE username=?", username).Scan(&id, &hash, &role)
	if err != nil {
		return Identity{}, errors.New("credenciales incorrectas")
	}
	if !verifyPassword(hash, password) {
		return Identity{}, errors.New("credenciales incorrectas")
	}
	return Identity{ID: "user:" + id, Username: username, Role: role}, nil
}
func (a *App) createSession(w http.ResponseWriter, r *http.Request, user Identity) error {
	token, err := randomToken()
	if err != nil {
		return err
	}
	csrf, err := randomToken()
	if err != nil {
		return err
	}
	uid := strings.TrimPrefix(user.ID, "user:")
	now := time.Now()
	_, err = a.security.db.ExecContext(r.Context(), "INSERT INTO goradio_sessions(token_hash,user_id,csrf,expires_at,created_at) VALUES(?,?,?,?,?)", tokenHash(token), uid, csrf, now.Add(a.security.cfg.SessionTTL).Unix(), now.Unix())
	if err != nil {
		return err
	}
	writeSessionCookie(w, r, sessionCookie, token, a.security.cfg.SessionTTL)
	return nil
}
func writeSessionCookie(w http.ResponseWriter, r *http.Request, name, value string, ttl time.Duration) {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(ttl), MaxAge: int(ttl.Seconds())})
}
func deleteSessionCookie(w http.ResponseWriter, r *http.Request, name string) {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}
func (a *App) identityForRequest(w http.ResponseWriter, r *http.Request) (Identity, string, error) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		var id, user, role, csrf string
		var expiry int64
		err = a.security.db.QueryRowContext(r.Context(), `SELECT u.id,u.username,u.role,s.csrf,s.expires_at
   FROM goradio_sessions s JOIN goradio_users u ON u.id=s.user_id WHERE s.token_hash=?`, tokenHash(cookie.Value)).Scan(&id, &user, &role, &csrf, &expiry)
		if err == nil && expiry > time.Now().Unix() {
			return Identity{ID: "user:" + id, Username: user, Role: role}, csrf, nil
		}
	}
	if a.security.cfg.RequireLogin {
		return Identity{}, "", nil
	}
	if cookie, err := r.Cookie(guestCookie); err == nil {
		var csrf string
		var expiry int64
		err = a.security.db.QueryRowContext(r.Context(), "SELECT csrf,expires_at FROM goradio_guest_sessions WHERE token_hash=?", tokenHash(cookie.Value)).Scan(&csrf, &expiry)
		if err == nil && expiry > time.Now().Unix() {
			return Identity{ID: "guest:" + tokenHash(cookie.Value), Role: "guest", Guest: true}, csrf, nil
		}
	}
	token, err := randomToken()
	if err != nil {
		return Identity{}, "", err
	}
	csrf, err := randomToken()
	if err != nil {
		return Identity{}, "", err
	}
	_, err = a.security.db.ExecContext(r.Context(), "INSERT INTO goradio_guest_sessions(token_hash,csrf,expires_at) VALUES(?,?,?)", tokenHash(token), csrf, time.Now().Add(a.security.cfg.SessionTTL).Unix())
	if err != nil {
		return Identity{}, "", err
	}
	writeSessionCookie(w, r, guestCookie, token, a.security.cfg.SessionTTL)
	return Identity{ID: "guest:" + tokenHash(token), Role: "guest", Guest: true}, csrf, nil
}
func csrfForRequest(r *http.Request) string {
	v, _ := r.Context().Value(ctxCSRFKey{}).(string)
	return v
}

type ctxCSRFKey struct{}

func (a *App) secureRoutes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		if r.URL.Path == "/api/health" || r.URL.Path == "/gradio_api/health" || strings.HasPrefix(r.URL.Path, "/assets/") || strings.HasPrefix(r.URL.Path, "/static/") || strings.HasPrefix(r.URL.Path, "/goradio-share") || r.URL.Path == "/theme.css" || r.URL.Path == "/favicon.ico" || r.URL.Path == "/favicon.png" {
			next.ServeHTTP(w, r)
			return
		}
		identity, csrf, err := a.identityForRequest(w, r)
		if err != nil {
			http.Error(w, "No se pudo leer la sesión", 500)
			return
		}
		ctx := context.WithValue(r.Context(), ctxIdentityKey{}, identity)
		ctx = context.WithValue(ctx, ctxCSRFKey{}, csrf)
		r = r.WithContext(ctx)
		if r.URL.Path == "/login" && r.Method == http.MethodGet {
			a.loginPage(w, r)
			return
		}
		if r.URL.Path == "/auth/login" && r.Method == http.MethodPost {
			a.loginHandler(w, r)
			return
		}
		if r.URL.Path == "/auth/signup" && r.Method == http.MethodPost {
			a.signupHandler(w, r)
			return
		}
		if r.URL.Path == "/auth/logout" && r.Method == http.MethodPost {
			if !checkCSRF(r) {
				http.Error(w, "CSRF inválido", 403)
				return
			}
			a.logoutHandler(w, r)
			return
		}
		if r.URL.Path == "/auth/me" && r.Method == http.MethodGet {
			writeJSON(w, 200, map[string]any{"authenticated": !identity.Guest && identity.ID != "", "identity": identity, "csrf": csrf, "login_required": a.security.cfg.RequireLogin})
			return
		}
		if identity.ID == "" && a.security.cfg.RequireLogin {
			if r.URL.Path == "/" && r.Method == http.MethodGet {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			http.Error(w, "Inicia sesión para continuar", http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/api/state" {
			switch r.Method {
			case http.MethodGet:
				a.readState(w, r)
				return
			case http.MethodPut:
				if !checkCSRF(r) {
					http.Error(w, "CSRF inválido", 403)
					return
				}
				a.saveState(w, r)
				return
			case http.MethodDelete:
				if !checkCSRF(r) {
					http.Error(w, "CSRF inválido", 403)
					return
				}
				a.deleteState(w, r)
				return
			}
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions && r.URL.Path != "/auth/login" && r.URL.Path != "/auth/signup" {
			if !sameOrigin(r) {
				http.Error(w, "Origen no permitido", 403)
				return
			}
			// Los endpoints nativos de Gradio usan CSRF mediante SameSite y verificación Origin.
		}
		next.ServeHTTP(w, r)
	})
}
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return origin == "http://"+r.Host || origin == "https://"+r.Host
}
func checkCSRF(r *http.Request) bool {
	value := csrfForRequest(r)
	return value != "" && subtle.ConstantTimeCompare([]byte(value), []byte(r.Header.Get("X-Goradio-CSRF"))) == 1
}
func (a *App) loginPage(w http.ResponseWriter, r *http.Request) {
	if id, ok := IdentityFromContext(r.Context()); ok && !id.Guest && id.ID != "" {
		http.Redirect(w, r, "/", 303)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintf(w, `<!doctype html><html lang="es"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Acceso a Goradio</title>
 <style>body{background:#111827;color:#fafafa;font:16px system-ui;display:grid;place-items:center;min-height:100vh;margin:0;padding:20px}form{display:grid;gap:14px;background:#1f2937;border:1px solid #374151;border-radius:16px;padding:32px;width:min(100%%,390px);box-sizing:border-box}h1{margin:0;font-size:26px}p{margin:0;color:#cbd5e1}label{display:grid;gap:6px}input{padding:12px;border:1px solid #64748b;border-radius:7px;font:inherit;color:#fff;background:#111827}button{background:#f97316;border:0;border-radius:8px;padding:13px;font-size:16px;font-weight:700;cursor:pointer}#status{min-height:20px;color:#fca5a5}</style></head><body><form id="form"><h1>Goradio</h1><p>Inicia sesión para continuar.</p><label>Usuario<input name="username" autocomplete="username" required minlength="3"></label><label>Contraseña<input type="password" name="password" autocomplete="current-password" required></label><button id="action">Entrar</button><a id="register-link" href="#" style="color:#fdba74;display:none;text-align:center">Crear cuenta</a><span id="status"></span></form>
 <script>const signupAllowed=%t;let registering=false;const link=document.querySelector("#register-link");if(signupAllowed){link.style.display="block";link.onclick=e=>{e.preventDefault();registering=!registering;document.querySelector("#action").textContent=registering?"Crear cuenta":"Entrar";link.textContent=registering?"Ya tengo cuenta":"Crear cuenta";document.querySelector("#status").textContent=""}}document.querySelector("#form").addEventListener("submit",async e=>{e.preventDefault();const f=e.target;const v=Object.fromEntries(new FormData(f));const status=document.querySelector("#status");status.textContent="";try{const res=await fetch(registering?"/auth/signup":"/auth/login",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(v)});if(!res.ok)throw Error(registering?"No se pudo crear la cuenta. Verifica usuario y contraseña (mínimo 4 caracteres).":"Usuario o contraseña incorrectos");location.replace("/")}catch(err){status.textContent=err.message}})</script></body></html>`, a.security.cfg.AllowSignup)
}
func (a *App) limitedLogin(r *http.Request) bool {
	ip := r.RemoteAddr
	if host, _, err := strings.Cut(ip, ":"); err {
		ip = host
	}
	a.security.authMu.Lock()
	defer a.security.authMu.Unlock()
	now := time.Now()
	attempts := a.security.failures[ip]
	filtered := attempts[:0]
	for _, t := range attempts {
		if now.Sub(t) < 10*time.Minute {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) >= 10 {
		a.security.failures[ip] = filtered
		return true
	}
	a.security.failures[ip] = append(filtered, now)
	return false
}
func (a *App) loginHandler(w http.ResponseWriter, r *http.Request) {
	if a.limitedLogin(r) {
		http.Error(w, "Demasiados intentos", 429)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Solicitud inválida", 400)
		return
	}
	if strings.TrimSpace(req.Username) == "" || strings.TrimSpace(req.Password) == "" {
		http.Error(w, "Introduce usuario y contraseña", http.StatusBadRequest)
		return
	}
	id, err := a.userLogin(r.Context(), req.Username, req.Password)
	if err != nil {
		http.Error(w, "Credenciales incorrectas", 401)
		return
	}
	if err := a.createSession(w, r, id); err != nil {
		http.Error(w, "Error al crear sesión", 500)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "username": id.Username})
}
func (a *App) signupHandler(w http.ResponseWriter, r *http.Request) {
	if !a.security.cfg.AllowSignup {
		http.NotFound(w, r)
		return
	}
	if a.limitedLogin(r) {
		http.Error(w, "Demasiados intentos", 429)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Solicitud inválida", 400)
		return
	}
	if strings.TrimSpace(req.Username) == "" || strings.TrimSpace(req.Password) == "" {
		http.Error(w, "Introduce usuario y contraseña", http.StatusBadRequest)
		return
	}
	if err := a.CreateUser(r.Context(), req.Username, req.Password, "user"); err != nil {
		http.Error(w, "Registro no válido", 400)
		return
	}
	id, err := a.userLogin(r.Context(), req.Username, req.Password)
	if err != nil {
		http.Error(w, "Error interno", 500)
		return
	}
	if err := a.createSession(w, r, id); err != nil {
		http.Error(w, "Error de sesión", 500)
		return
	}
	writeJSON(w, 201, map[string]any{"ok": true, "username": id.Username})
}
func (a *App) logoutHandler(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		_, _ = a.security.db.ExecContext(r.Context(), "DELETE FROM goradio_sessions WHERE token_hash=?", tokenHash(cookie.Value))
	}
	deleteSessionCookie(w, r, sessionCookie)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) schemaHash() string {
	raw, _ := json.Marshal([]any{a.Config.Inputs, a.Config.Outputs})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:16])
}
func (a *App) getState(ctx context.Context, id Identity) (stateSnapshot, error) {
	empty := stateSnapshot{Inputs: []any{}, Outputs: []any{}}
	if a.security == nil || id.ID == "" {
		return empty, nil
	}
	var hash, in, out string
	var updated int64
	err := a.security.db.QueryRowContext(ctx, `SELECT schema_hash,inputs_json,outputs_json,updated_at FROM goradio_form_state
    WHERE app_id=? AND identity_id=?`, a.security.cfg.AppID, id.ID).Scan(&hash, &in, &out, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	if hash != a.schemaHash() {
		return empty, nil
	}
	var values stateSnapshot
	if json.Unmarshal([]byte(in), &values.Inputs) != nil || json.Unmarshal([]byte(out), &values.Outputs) != nil {
		return empty, nil
	}
	values.UpdatedAt = updated
	return values, nil
}
func (a *App) persistState(ctx context.Context, id Identity, state stateSnapshot) error {
	if a.security == nil || id.ID == "" {
		return nil
	}
	in, err := json.Marshal(state.Inputs)
	if err != nil {
		return err
	}
	out, err := json.Marshal(state.Outputs)
	if err != nil {
		return err
	}
	if len(in) > 1<<20 || len(out) > 1<<20 {
		return errors.New("formulario demasiado grande")
	}
	_, err = a.security.db.ExecContext(ctx, `INSERT INTO goradio_form_state(app_id,identity_id,schema_hash,inputs_json,outputs_json,updated_at)
 VALUES(?,?,?,?,?,?) ON CONFLICT(app_id,identity_id) DO UPDATE SET
 schema_hash=excluded.schema_hash,inputs_json=excluded.inputs_json,outputs_json=excluded.outputs_json,updated_at=excluded.updated_at`,
		a.security.cfg.AppID, id.ID, a.schemaHash(), string(in), string(out), time.Now().Unix())
	return err
}
func (a *App) readState(w http.ResponseWriter, r *http.Request) {
	id, _ := IdentityFromContext(r.Context())
	state, err := a.getState(r.Context(), id)
	if err != nil {
		http.Error(w, "Error de almacenamiento", 500)
		return
	}
	writeJSON(w, 200, state)
}
func (a *App) saveState(w http.ResponseWriter, r *http.Request) {
	id, _ := IdentityFromContext(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()
	var state stateSnapshot
	if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
		http.Error(w, "JSON inválido", 400)
		return
	}
	if len(state.Inputs) != len(a.Config.Inputs) || len(state.Outputs) != len(a.Config.Outputs) {
		http.Error(w, "Número incorrecto de componentes", 400)
		return
	}
	for i, c := range a.Config.Inputs {
		if isMedia(c.Type) && state.Inputs[i] != nil {
			file := a.mediaFileID(state.Inputs[i])
			if file == "" || !a.canAccessFile(r.Context(), file) {
				http.Error(w, "Archivo no autorizado", 403)
				return
			}
			continue
		}
		if _, err := a.validate(c, state.Inputs[i]); err != nil {
			http.Error(w, "Valor de entrada inválido", 400)
			return
		}
	}
	for i, c := range a.Config.Outputs {
		if isMedia(c.Type) && state.Outputs[i] != nil {
			file := a.mediaFileID(state.Outputs[i])
			if file == "" || !a.canAccessFile(r.Context(), file) {
				http.Error(w, "Archivo de salida no autorizado", 403)
				return
			}
		}
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if err := a.persistState(r.Context(), id, state); err != nil {
		http.Error(w, "Error guardando estado", 500)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) deleteStoredState(ctx context.Context, id Identity) error {
	if a.security == nil || id.ID == "" {
		return nil
	}
	_, err := a.security.db.ExecContext(ctx, "DELETE FROM goradio_form_state WHERE app_id=? AND identity_id=?", a.security.cfg.AppID, id.ID)
	return err
}
func (a *App) deleteState(w http.ResponseWriter, r *http.Request) {
	id, _ := IdentityFromContext(r.Context())
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if err := a.deleteStoredState(r.Context(), id); err != nil {
		http.Error(w, "Error de almacenamiento", 500)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) mediaFileID(value any) string {
	var path string
	switch x := value.(type) {
	case string:
		path = x
	case map[string]any:
		if nested, ok := x["video"]; ok {
			return a.mediaFileID(nested)
		}
		path, _ = x["path"].(string)
		if path == "" {
			path, _ = x["url"].(string)
		}
	}
	path = strings.TrimPrefix(path, "/gradio_api/file=")
	path = strings.TrimPrefix(path, "/api/media/")
	if fileID.MatchString(path) {
		return path
	}
	return ""
}
func (a *App) recordFile(ctx context.Context, file string) error {
	if a.security == nil {
		return nil
	}
	identity, ok := IdentityFromContext(ctx)
	if !ok || identity.ID == "" {
		return errors.New("no hay identidad para el archivo")
	}
	if !fileID.MatchString(file) {
		return errors.New("ID de archivo inválido")
	}
	_, err := a.security.db.ExecContext(ctx, `INSERT INTO goradio_files(file_id,app_id,identity_id,created_at)
 VALUES(?,?,?,?) ON CONFLICT(file_id) DO NOTHING`, file, a.security.cfg.AppID, identity.ID, time.Now().Unix())
	return err
}
func (a *App) canAccessFile(ctx context.Context, file string) bool {
	if a.security == nil {
		return true
	}
	identity, ok := IdentityFromContext(ctx)
	if !ok || identity.ID == "" || !fileID.MatchString(file) {
		return false
	}
	var count int
	err := a.security.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM goradio_files WHERE file_id=? AND app_id=? AND identity_id=?",
		file, a.security.cfg.AppID, identity.ID).Scan(&count)
	return err == nil && count > 0
}
func (a *App) storeInput(ctx context.Context, id Identity, index int, value any) error {
	if a.security == nil || !a.security.cfg.PersistInputs {
		return nil
	}
	if index < 0 || index >= len(a.Config.Inputs) {
		return errors.New("Índice inválido")
	}
	c := a.Config.Inputs[index]
	if isMedia(c.Type) && value != nil {
		file := a.mediaFileID(value)
		if file == "" || !a.canAccessFile(ctx, file) {
			return errors.New("archivo no autorizado")
		}
	} else if _, err := a.validate(c, value); err != nil {
		return err
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	state, err := a.getState(ctx, id)
	if err != nil {
		return err
	}
	if len(state.Inputs) != len(a.Config.Inputs) {
		state.Inputs = make([]any, len(a.Config.Inputs))
		for i, c := range a.Config.Inputs {
			state.Inputs[i] = c.Value
		}
	}
	if len(state.Outputs) != len(a.Config.Outputs) {
		state.Outputs = make([]any, len(a.Config.Outputs))
	}
	state.Inputs[index] = value
	return a.persistState(ctx, id, state)
}
func (a *App) storeResults(ctx context.Context, id Identity, inputs, outputs []any) error {
	if a.security == nil || (!a.security.cfg.PersistInputs && !a.security.cfg.PersistOutputs) {
		return nil
	}
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	state, err := a.getState(ctx, id)
	if err != nil {
		return err
	}
	if a.security.cfg.PersistInputs {
		values := make([]any, len(inputs))
		copy(values, inputs)
		for i, c := range a.Config.Inputs {
			if !isMedia(c.Type) || values[i] == nil {
				continue
			}
			file := a.mediaFileID(values[i])
			if file == "" || !a.canAccessFile(ctx, file) {
				values[i] = nil
				continue
			}
			values[i] = a.outputFileData(file, c.Type)
		}
		state.Inputs = values
	}
	if a.security.cfg.PersistOutputs {
		state.Outputs = make([]any, len(outputs))
		copy(state.Outputs, outputs)
		for i, c := range a.Config.Outputs {
			if !isMedia(c.Type) || state.Outputs[i] == nil {
				continue
			}
			file := a.mediaFileID(state.Outputs[i])
			if file == "" || !a.canAccessFile(ctx, file) {
				state.Outputs[i] = nil
			}
		}
	}
	return a.persistState(ctx, id, state)
}
func (a *App) outputFileData(file, kind string) any {
	info, err := os.Stat(filepath.Join(a.MediaDir, file))
	if err != nil {
		return nil
	}
	data := map[string]any{"path": file, "url": "/gradio_api/file=" + file, "orig_name": file,
		"size": info.Size(), "mime_type": mime.TypeByExtension(filepath.Ext(file)),
		"meta": map[string]any{"_type": "gradio.FileData"}}
	if kind == "video" {
		return map[string]any{"video": data, "subtitles": nil}
	}
	return data
}
func (a *App) persistHandler(w http.ResponseWriter, r *http.Request, index int, v any) {
	id, _ := IdentityFromContext(r.Context())
	if err := a.storeInput(r.Context(), id, index, v); err != nil {
		writePredictionError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": []any{}, "duration": 0, "average_duration": 0, "is_generating": false, "success": true})
}
func (a *App) gradioConfigForRequest(r *http.Request) map[string]any {
	cfg := a.gradioConfig()
	if a.security == nil {
		return cfg
	}
	id, _ := IdentityFromContext(r.Context())
	state, err := a.getState(r.Context(), id)
	if err != nil {
		return cfg
	}
	deps, ok := cfg["dependencies"].([]any)
	if !ok || len(deps) == 0 {
		return cfg
	}
	submit := deps[0].(map[string]any)
	inputs := submit["inputs"].([]int)
	outputs := submit["outputs"].([]int)
	components := cfg["components"].([]any)
	byID := map[int]map[string]any{}
	for _, v := range components {
		c := v.(map[string]any)
		byID[c["id"].(int)] = c["props"].(map[string]any)
	}
	if len(state.Inputs) == len(inputs) {
		for i, id := range inputs {
			if state.Inputs[i] != nil {
				byID[id]["value"] = state.Inputs[i]
			}
		}
	}
	if len(state.Outputs) == len(outputs) {
		for i, id := range outputs {
			if state.Outputs[i] != nil {
				byID[id]["value"] = state.Outputs[i]
			}
		}
	}
	return cfg
}
