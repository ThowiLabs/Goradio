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
	Type           string   `json:"type"`
	Key            string   `json:"key,omitempty"`
	Label          string   `json:"label,omitempty"`
	Section        string   `json:"section,omitempty"`
	Placeholder    string   `json:"placeholder,omitempty"`
	Required       bool     `json:"required,omitempty"`
	Min            *float64 `json:"min,omitempty"`
	Max            *float64 `json:"max,omitempty"`
	Step           *float64 `json:"step,omitempty"`
	Value          any      `json:"value,omitempty"`
	Choices        []string `json:"choices,omitempty"`
	Pattern        string   `json:"pattern,omitempty"`
	MinLength      *int     `json:"min_length,omitempty"`
	MaxLength      *int     `json:"max_length,omitempty"`
	Headers        []string `json:"headers,omitempty"`
	Datatype       []string `json:"datatype,omitempty"`
	RowCount       int      `json:"row_count,omitempty"`
	ColumnCount    int      `json:"column_count,omitempty"`
	FixedRows      bool     `json:"fixed_rows,omitempty"`
	FixedColumns   bool     `json:"fixed_columns,omitempty"`
	ShowSearch     *bool    `json:"show_search,omitempty"`
	ShowCopyButton *bool    `json:"show_copy_button,omitempty"`
	Language       string   `json:"language,omitempty"`
	Lines          int      `json:"lines,omitempty"`
}
type Theme struct {
	PrimaryColor   string `json:"primary_color,omitempty"`
	SecondaryColor string `json:"secondary_color,omitempty"`
}

type Config struct {
	Theme        *Theme      `json:"theme,omitempty"`
	Title        string      `json:"title"`
	Description  string      `json:"description,omitempty"`
	Inputs       []Component `json:"inputs"`
	Outputs      []Component `json:"outputs"`
	Layout       *LayoutNode `json:"layout,omitempty"`
	Events       []EventSpec `json:"events,omitempty"`
	Live         bool        `json:"live,omitempty"`
	Load         bool        `json:"load,omitempty"`
	Streaming    bool        `json:"streaming,omitempty"`
	EverySeconds float64     `json:"every_seconds,omitempty"`
}
type Handler func(context.Context, []any) ([]any, error)

// StreamHandler sends successive complete output snapshots. Call emit until
// the operation finishes or the context is canceled.
type StreamHandler func(context.Context, []any, func([]any) error) error
type App struct {
	Config         Config
	Handler        Handler
	Stream         StreamHandler
	Validator      func(context.Context, []any) error
	pipeline       []Handler
	eventHandlers  map[int]Handler
	eventStreams   map[int]StreamHandler
	MediaDir       string
	MaxUploadBytes int64
	tunnels        *TunnelManager
	jobsMu         sync.Mutex
	jobs           map[string]*gradioJob
	uploadsMu      sync.Mutex
	uploads        map[string]*uploadState
	security       *securityBackend
	stateMu        sync.Mutex
	sessionMu      sync.Mutex
	sessions       map[string]*sessionBucket
}

var supported = map[string]bool{"textbox": true, "number": true, "slider": true, "checkbox": true, "dropdown": true, "radio": true, "checkboxgroup": true, "image": true, "audio": true, "video": true, "file": true, "json": true, "markdown": true, "code": true, "dataframe": true, "chatbot": true, "plot": true, "gallery": true}
var fileID = regexp.MustCompile(`^[a-f0-9]{32}(\.[a-z0-9]{1,10})?$`)

func New(config Config, handler Handler, mediaDir string) (*App, error) {
	if handler == nil {
		return nil, errors.New("handler obligatorio")
	}
	if err := validateBlocksLayout(config); err != nil {
		return nil, err
	}
	if err := validateTheme(config.Theme); err != nil {
		return nil, err
	}
	if config.EverySeconds != 0 && (config.EverySeconds < .25 || config.EverySeconds > 3600 || math.IsNaN(config.EverySeconds) || math.IsInf(config.EverySeconds, 0)) {
		return nil, errors.New("every_seconds debe estar entre 0.25 y 3600")
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
		if c.MinLength != nil && *c.MinLength < 0 || c.MaxLength != nil && *c.MaxLength < 0 || c.MinLength != nil && c.MaxLength != nil && *c.MinLength > *c.MaxLength {
			return nil, errors.New("longitud de texto inválida")
		}
		if c.Pattern != "" {
			if len(c.Pattern) > 256 {
				return nil, errors.New("patrón demasiado largo")
			}
			if _, err := regexp.Compile(c.Pattern); err != nil {
				return nil, fmt.Errorf("patrón inválido: %w", err)
			}
		}
		if c.RowCount < 0 || c.RowCount > 10000 || c.ColumnCount < 0 || c.ColumnCount > 64 || c.Lines < 0 || c.Lines > 200 || len(c.Language) > 40 {
			return nil, errors.New("límites inválidos de tabla o código")
		}
		if len(c.Headers) > 64 || len(c.Datatype) > 64 {
			return nil, errors.New("demasiadas columnas")
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
	return &App{Config: config, Handler: handler, MediaDir: mediaDir, MaxUploadBytes: 40 << 20, tunnels: NewTunnelManager(), jobs: map[string]*gradioJob{}, uploads: map[string]*uploadState{}, sessions: map[string]*sessionBucket{}, eventHandlers: map[int]Handler{}, eventStreams: map[int]StreamHandler{}}, nil
}

// BindEvent attaches a callback to an event already declared in Config.Events.
func (a *App) BindEvent(index int, handler Handler) error {
	if handler == nil || index < 0 || index >= len(a.Config.Events) {
		return errors.New("evento o handler inválido")
	}
	if a.Config.Layout == nil {
		return errors.New("eventos dirigidos requieren Blocks")
	}
	a.eventHandlers[index] = handler
	return nil
}

// On declares a new native Gradio event and binds its Go callback.
// Configure it before starting the server.
func (a *App) On(spec EventSpec, handler Handler) error {
	if a.Config.Layout == nil {
		return errors.New("On requiere layout Blocks")
	}
	if handler == nil {
		return errors.New("handler obligatorio")
	}
	if len(a.Config.Events) >= 64 {
		return errors.New("máximo 64 eventos")
	}
	copyCfg := a.Config
	copyCfg.Events = append(append([]EventSpec{}, copyCfg.Events...), spec)
	if err := validateBlocksLayout(copyCfg); err != nil {
		return err
	}
	for _, ev := range a.Config.Events {
		if ev.Target == spec.Target && ev.Trigger == spec.Trigger {
			return errors.New("evento duplicado")
		}
	}
	a.Config.Events = copyCfg.Events
	a.eventHandlers[len(a.Config.Events)-1] = handler
	return nil
}

// BindStreamEvent links a declared Blocks event to a generator. When an
// event is streaming, the original Gradio frontend uses its native SSE queue.
func (a *App) BindStreamEvent(index int, stream StreamHandler) error {
	if stream == nil || a.Config.Layout == nil || index < 0 || index >= len(a.Config.Events) {
		return errors.New("evento de streaming inválido")
	}
	a.eventStreams[index] = stream
	a.Config.Events[index].Streaming = true
	return nil
}

// OnStream declares an independent streaming event on a native Gradio Button
// or component. It does not replace the original frontend or theme.
func (a *App) OnStream(spec EventSpec, stream StreamHandler) error {
	if stream == nil {
		return errors.New("stream obligatorio")
	}
	spec.Streaming = true
	if err := a.On(spec, func(context.Context, []any) ([]any, error) {
		return nil, failPrediction(400, "EVENTO", "Este evento requiere SSE")
	}); err != nil {
		return err
	}
	return a.BindStreamEvent(len(a.Config.Events)-1, stream)
}

// SetStreamHandler enables progressive results over Gradio's SSE protocol.
// Configure it before serving HTTP requests.
func (a *App) SetStreamHandler(stream StreamHandler) { a.Stream = stream }

// SetValidator rejects invalid combinations of input fields before invoking
// application functions. Configure it before starting the server.
func (a *App) SetValidator(validate func(context.Context, []any) error) { a.Validator = validate }

// Then chains a second handler after the previous handler has produced all its
// outputs. Each stage receives the previous complete output list as inputs.
// It uses the existing Gradio components; no alternative frontend is added.
func (a *App) Then(next Handler) error {
	if next == nil {
		return errors.New("then requiere una función")
	}
	if len(a.pipeline) >= 16 {
		return errors.New("máximo 16 eventos encadenados")
	}
	a.pipeline = append(a.pipeline, next)
	return nil
}

func (a *App) runThen(ctx context.Context, values []any, afterEach func([]any) error) ([]any, error) {
	current := values
	for _, handler := range a.pipeline {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		next, err := handler(ctx, current)
		if err != nil {
			return nil, err
		}
		if len(next) != len(a.Config.Outputs) {
			return nil, failPrediction(500, "SALIDAS", "Etapa de cadena devolvió salidas incorrectas")
		}
		current = next
		if afterEach != nil {
			if err := afterEach(current); err != nil {
				return nil, err
			}
		}
	}
	return current, nil
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
	mux.HandleFunc("POST /gradio_api/queue/join", a.gradioQueueJoin)
	mux.HandleFunc("GET /gradio_api/queue/data", a.gradioQueueData)
	mux.HandleFunc("POST /gradio_api/queue/cancel", a.gradioQueueCancel)
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
	if a.Validator != nil {
		if err := a.Validator(timeoutCtx, values); err != nil {
			return nil, failPrediction(400, "VALIDACION", err.Error())
		}
	}
	results, err := a.Handler(timeoutCtx, values)
	if err != nil {
		return nil, err
	}
	results, err = a.runThen(timeoutCtx, results, nil)
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
	case "textbox", "code":
		txt, ok := v.(string)
		if !ok {
			return nil, errors.New("debe ser texto")
		}
		n := len([]rune(txt))
		if c.MinLength != nil && n < *c.MinLength {
			return nil, fmt.Errorf("mínimo %d caracteres", *c.MinLength)
		}
		if c.MaxLength != nil && n > *c.MaxLength {
			return nil, fmt.Errorf("máximo %d caracteres", *c.MaxLength)
		}
		if c.Pattern != "" && !regexp.MustCompile(c.Pattern).MatchString(txt) {
			return nil, errors.New("formato inválido")
		}
	case "json":
		return v, nil
	case "dataframe":
		frame, ok := v.(map[string]any)
		if !ok {
			return nil, errors.New("se requiere tabla con headers y data")
		}
		rows, ok := frame["data"].([]any)
		if !ok {
			return nil, errors.New("filas inválidas")
		}
		if len(rows) > 10000 {
			return nil, errors.New("máximo 10000 filas")
		}
		if len(c.Headers) > 0 {
			headers, ok := frame["headers"].([]any)
			if !ok || len(headers) != len(c.Headers) {
				return nil, errors.New("columnas inválidas")
			}
			for i, h := range headers {
				if h != c.Headers[i] {
					return nil, errors.New("nombres de columnas inválidos")
				}
			}
		}
		for _, row := range rows {
			cells, ok := row.([]any)
			if !ok || len(cells) > 64 || len(c.Headers) > 0 && len(cells) != len(c.Headers) {
				return nil, errors.New("fila inválida")
			}
			for i, x := range cells {
				if i < len(c.Datatype) && c.Datatype[i] == "number" && x != nil {
					v, ok := x.(float64)
					if !ok || math.IsNaN(v) || math.IsInf(v, 0) {
						return nil, errors.New("celda numérica inválida")
					}
				}
			}
		}
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
	case "dropdown", "radio":
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
	case "checkboxgroup":
		items, ok := v.([]any)
		if !ok {
			return nil, errors.New("selección múltiple debe ser lista")
		}
		if len(items) > len(c.Choices) {
			return nil, errors.New("selección múltiple demasiado grande")
		}
		allowed := make(map[string]bool, len(c.Choices))
		for _, choice := range c.Choices {
			allowed[choice] = true
		}
		seen := make(map[string]bool, len(items))
		for _, item := range items {
			value, ok := item.(string)
			if !ok || !allowed[value] || seen[value] {
				return nil, errors.New("opción de selección múltiple no permitida o duplicada")
			}
			seen[value] = true
		}
		if c.Required && len(items) == 0 {
			return nil, errors.New("selección obligatoria")
		}
		return v, nil
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
