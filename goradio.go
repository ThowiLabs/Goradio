package goradio

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Component struct {
	Type        string   `json:"type"`
	Label       string   `json:"label,omitempty"`
	Section     string   `json:"section,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	Step        *float64 `json:"step,omitempty"`
	Value       any      `json:"value,omitempty"`
	Choices     []string `json:"choices,omitempty"`
}
type Config struct {
	Title       string      `json:"title"`
	Description string      `json:"description,omitempty"`
	Inputs      []Component `json:"inputs"`
	Outputs     []Component `json:"outputs"`
}
type Handler func(context.Context, []any) ([]any, error)
type App struct {
	Config         Config
	Handler        Handler
	MediaDir       string
	MaxUploadBytes int64
	tunnels        *TunnelManager
	jobsMu         sync.Mutex
	jobs           map[string]*gradioJob
	uploadsMu      sync.Mutex
	uploads        map[string]*uploadState
	security       *securityBackend
	stateMu        sync.Mutex
}

var supported = map[string]bool{"textbox": true, "number": true, "slider": true, "checkbox": true, "dropdown": true, "image": true, "audio": true, "video": true, "file": true, "json": true}
var fileID = regexp.MustCompile(`^[a-f0-9]{32}(\.[a-z0-9]{1,10})?$`)

func New(config Config, handler Handler, mediaDir string) (*App, error) {
	if handler == nil {
		return nil, errors.New("handler obligatorio")
	}
	if len(config.Outputs) == 0 {
		return nil, errors.New("se requiere una salida")
	}
	if len(config.Inputs) > 32 || len(config.Outputs) > 32 {
		return nil, errors.New("demasiados componentes")
	}
	for _, c := range append(append([]Component{}, config.Inputs...), config.Outputs...) {
		if !supported[c.Type] {
			return nil, fmt.Errorf("componente desconocido: %s", c.Type)
		}
		if c.Min != nil && c.Max != nil && *c.Min > *c.Max {
			return nil, errors.New("min mayor a max")
		}
	}
	if mediaDir == "" {
		mediaDir = filepath.Join(os.TempDir(), "goradio-media")
	}
	if err := os.MkdirAll(mediaDir, 0700); err != nil {
		return nil, err
	}
	if config.Title == "" {
		config.Title = "Goradio"
	}
	return &App{Config: config, Handler: handler, MediaDir: mediaDir, MaxUploadBytes: 40 << 20, tunnels: NewTunnelManager(), jobs: map[string]*gradioJob{}, uploads: map[string]*uploadState{}}, nil
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, a.Config) })
	mux.HandleFunc("GET /api/tunnels", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, a.tunnels.Snapshot()) })
	// La UI consulta /auth/me también en el modo público. Sin seguridad
	// se devuelve un estado anónimo explícito y no un 404 en consola.
	mux.HandleFunc("GET /auth/me", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false, "identity": Identity{Guest: true}, "csrf": "", "login_required": false})
	})
	mux.HandleFunc("POST /api/predict", a.predict)
	mux.HandleFunc("POST /api/upload", a.upload)
	mux.HandleFunc("GET /api/media/{file}", a.media)

	mux.HandleFunc("GET /config", a.gradioConfigResponse)
	mux.HandleFunc("GET /config/", a.gradioConfigResponse)
	mux.HandleFunc("GET /gradio_api/config", a.gradioConfigResponse)
	mux.HandleFunc("GET /gradio_api/info", a.gradioInfo)
	mux.HandleFunc("GET /gradio_api/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /theme.css", a.gradioTheme)
	mux.HandleFunc("GET /assets/", a.gradioAsset)
	mux.HandleFunc("GET /static/", a.gradioAsset)
	mux.HandleFunc("POST /gradio_api/run/predict", a.gradioPredict)
	mux.HandleFunc("POST /gradio_api/run/clear", a.gradioReset)
	mux.HandleFunc("POST /gradio_api/api/predict", a.gradioPredict)
	mux.HandleFunc("POST /gradio_api/call/predict", a.gradioCall)
	mux.HandleFunc("GET /gradio_api/call/predict/{id}", a.gradioEvent)
	mux.HandleFunc("POST /gradio_api/upload", a.gradioUpload)
	mux.HandleFunc("GET /gradio_api/upload_progress", a.gradioUploadProgress)
	mux.HandleFunc("GET /gradio_api/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/gradio_api/file=") {
			a.gradioFile(w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("GET /favicon.ico", a.gradioFavicon)
	mux.HandleFunc("GET /favicon.png", a.gradioFavicon)
	mux.Handle("GET /classic/", http.StripPrefix("/classic/", assetHandler()))
	mux.Handle("GET /app.js", assetHandler())
	mux.Handle("GET /goradio-share.js", assetHandler())
	mux.Handle("GET /goradio-share.css", assetHandler())
	mux.Handle("GET /goradio-auth.js", assetHandler())
	mux.Handle("GET /goradio-auth.css", assetHandler())
	mux.Handle("GET /goradio-dashboard.css", assetHandler())
	mux.Handle("GET /goradio-dashboard.js", assetHandler())
	mux.Handle("GET /app.css", assetHandler())
	mux.HandleFunc("GET /", a.gradioIndex)

	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		user, pass := os.Getenv("GORADIO_AUTH_USER"), os.Getenv("GORADIO_AUTH_PASSWORD")
		if a.security == nil && (user != "" || pass != "") {
			if strings.TrimSpace(user) == "" || strings.TrimSpace(pass) == "" || len([]rune(pass)) < 4 {
				http.Error(w, "Configuración de autenticación incompleta o contraseña demasiado corta", http.StatusServiceUnavailable)
				return
			}
			givenUser, givenPass, ok := r.BasicAuth()
			matchUser := subtle.ConstantTimeCompare([]byte(givenUser), []byte(user)) == 1
			matchPass := subtle.ConstantTimeCompare([]byte(givenPass), []byte(pass)) == 1
			if !ok || !matchUser || !matchPass {
				w.Header().Set("WWW-Authenticate", `Basic realm="Goradio"`)
				http.Error(w, "Se requiere autenticación", http.StatusUnauthorized)
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
	if a.security != nil {
		return a.secureRoutes(baseHandler)
	}
	return baseHandler
}
func (a *App) Serve(ctx context.Context, host string, port int, providers []string) error {
	if host == "" {
		host = "127.0.0.1"
	}
	if port < 0 || port > 65535 {
		return errors.New("puerto inválido")
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		return fmt.Errorf("abrir puerto: %w", err)
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)
	srv := &http.Server{Handler: a.Routes(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20}
	log.Printf("Goradio local: http://127.0.0.1:%d", addr.Port)
	tunnelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	a.tunnels.Start(tunnelCtx, addr.Port, providers)
	go func() {
		<-ctx.Done()
		shutdown, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		if err := srv.Shutdown(shutdown); err != nil {
			log.Printf("apagado: %v", err)
		}
	}()
	err = srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

type predictionFailure struct {
	status  int
	code    string
	message string
}

func (e *predictionFailure) Error() string { return e.message }
func failPrediction(status int, code, message string) error {
	return &predictionFailure{status, code, message}
}

func (a *App) predict(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()
	var req struct {
		Data []any `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apiError(w, 400, "JSON_INVALIDO", "Solicitud inválida")
		return
	}
	results, err := a.predictValues(r.Context(), req.Data)
	if err != nil {
		writePredictionError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": results})
}
func writePredictionError(w http.ResponseWriter, err error) {
	var problem *predictionFailure
	if errors.As(err, &problem) {
		apiError(w, problem.status, problem.code, problem.message)
		return
	}
	log.Printf("handler: %v", err)
	apiError(w, 500, "EJECUCION", "No se pudo ejecutar la función")
}
func (a *App) predictValues(ctx context.Context, data []any) ([]any, error) {
	if len(data) != len(a.Config.Inputs) {
		return nil, failPrediction(400, "CANTIDAD_INVALIDA", "Número de entradas incorrecto")
	}
	values := make([]any, len(data))
	for i, c := range a.Config.Inputs {
		v, err := a.validate(c, data[i])
		if err != nil {
			return nil, failPrediction(400, "VALIDACION", fmt.Sprintf("Entrada %d: %v", i+1, err))
		}
		values[i] = v
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	results, err := a.Handler(timeoutCtx, values)
	if err != nil {
		return nil, err
	}
	if len(results) != len(a.Config.Outputs) {
		return nil, failPrediction(500, "SALIDAS", "La función devolvió una cantidad incorrecta de salidas")
	}
	for i, c := range a.Config.Outputs {
		if !isMedia(c.Type) {
			continue
		}
		value, ok := results[i].(string)
		if !ok || value == "" {
			continue
		}
		base, err := filepath.Abs(a.MediaDir)
		if err != nil {
			return nil, failPrediction(500, "ARCHIVO", "Ruta inválida")
		}
		abs, err := filepath.Abs(filepath.Clean(value))
		if err != nil {
			return nil, failPrediction(500, "ARCHIVO", "Ruta inválida")
		}
		rel, err := filepath.Rel(base, abs)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") || strings.Contains(rel, string(filepath.Separator)) || !fileID.MatchString(rel) {
			return nil, failPrediction(400, "ARCHIVO", "La salida multimedia debe registrarse con goradio.media(ruta)")
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, failPrediction(400, "ARCHIVO", "Archivo de salida no encontrado")
		}
		results[i] = "/api/media/" + rel
	}
	return results, nil
}
func (a *App) validate(c Component, v any) (any, error) {
	if v == nil {
		if c.Required {
			return nil, errors.New("campo obligatorio")
		}
		return v, nil
	}
	if s, ok := v.(string); ok && s == "" {
		if c.Required {
			return nil, errors.New("campo obligatorio")
		}
		return v, nil
	}
	switch c.Type {
	case "textbox":
		if _, ok := v.(string); !ok {
			return nil, errors.New("debe ser texto")
		}
	case "json":
		return v, nil
	case "number", "slider":
		n, ok := v.(float64)
		if !ok || math.IsInf(n, 0) || math.IsNaN(n) {
			return nil, errors.New("debe ser número")
		}
		if c.Min != nil && n < *c.Min {
			return nil, fmt.Errorf("valor menor que %v", *c.Min)
		}
		if c.Max != nil && n > *c.Max {
			return nil, fmt.Errorf("valor mayor que %v", *c.Max)
		}
	case "checkbox":
		b, ok := v.(bool)
		if !ok {
			return nil, errors.New("debe ser booleano")
		}
		if c.Required && !b {
			return nil, errors.New("casilla obligatoria")
		}
	case "dropdown":
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("debe ser texto")
		}
		found := false
		for _, item := range c.Choices {
			if s == item {
				found = true
			}
		}
		if !found {
			return nil, errors.New("opción no permitida")
		}
	case "image", "audio", "video", "file":
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("archivo inválido")
		}
		id := strings.TrimPrefix(s, "/api/media/")
		if !strings.HasPrefix(s, "/api/media/") || !fileID.MatchString(id) {
			return nil, errors.New("archivo no registrado")
		}
		if _, err := os.Stat(filepath.Join(a.MediaDir, id)); err != nil {
			return nil, errors.New("archivo no disponible")
		}
		return filepath.Join(a.MediaDir, id), nil
	}
	return v, nil
}
func isMedia(k string) bool { return k == "file" || k == "image" || k == "audio" || k == "video" }
func (a *App) upload(w http.ResponseWriter, r *http.Request) {
	limit := a.MaxUploadBytes
	if limit <= 0 || limit > 200<<20 {
		limit = 40 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit+(1<<20))
	file, header, err := r.FormFile("file")
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if err != nil {
		apiError(w, 400, "SUBIDA", "Falta el archivo o supera el límite")
		return
	}
	defer file.Close()
	kind := r.FormValue("kind")
	if kind != "file" && !isMedia(kind) {
		apiError(w, 400, "TIPO", "Tipo no permitido")
		return
	}
	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		apiError(w, 400, "SUBIDA", "No se pudo leer archivo")
		return
	}
	mime := http.DetectContentType(head[:n])
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if len(ext) > 11 {
		apiError(w, 400, "EXTENSION", "Extensión inválida")
		return
	}
	if !safeExt[ext] {
		apiError(w, 400, "EXTENSION", "Extensión no permitida")
		return
	}
	if !validMediaType(kind, mime, ext) {
		apiError(w, 400, "TIPO", "El archivo no corresponde al tipo seleccionado")
		return
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		apiError(w, 500, "SUBIDA", "Error interno")
		return
	}
	id := hex.EncodeToString(token) + ext
	path := filepath.Join(a.MediaDir, id)
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		apiError(w, 500, "SUBIDA", "Error creando archivo")
		return
	}
	defer out.Close()
	if _, err := out.Write(head[:n]); err != nil {
		os.Remove(path)
		apiError(w, 500, "SUBIDA", "Error escribiendo archivo")
		return
	}
	copied, err := io.Copy(out, io.LimitReader(file, limit-int64(n)+1))
	if err != nil || copied > limit-int64(n) {
		os.Remove(path)
		apiError(w, 413, "LIMITE", "Archivo demasiado grande")
		return
	}
	if err := a.recordFile(r.Context(), id); err != nil {
		os.Remove(path)
		apiError(w, 500, "ARCHIVO", "No se pudo registrar el archivo")
		return
	}
	writeJSON(w, 200, map[string]string{"url": "/api/media/" + id, "name": header.Filename, "mime": mime})
}
func validMediaType(kind, mime, ext string) bool {
	switch kind {
	case "image":
		return strings.HasPrefix(mime, "image/") && (ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".webp")
	case "audio":
		return (strings.HasPrefix(mime, "audio/") || mime == "application/octet-stream") && (ext == ".mp3" || ext == ".wav" || ext == ".ogg" || ext == ".flac" || ext == ".m4a")
	case "video":
		return (strings.HasPrefix(mime, "video/") || mime == "application/octet-stream") && (ext == ".mp4" || ext == ".webm" || ext == ".mov")
	case "file":
		return true
	}
	return false
}
func (a *App) media(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("file")
	if !fileID.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	if !a.canAccessFile(r.Context(), id) {
		http.NotFound(w, r)
		return
	}
	setSafeMediaHeaders(w, id)
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeFile(w, r, filepath.Join(a.MediaDir, id))
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("codificando JSON: %v", err)
	}
}
func apiError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
