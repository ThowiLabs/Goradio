package goradio

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type gradioJob struct {
	created  time.Time
	finished chan struct{}
	cancel   context.CancelFunc
	mu       sync.Mutex
	data     []any
	err      error
}

func (j *gradioJob) complete(data []any, err error) {
	j.mu.Lock()
	j.data = data
	j.err = err
	j.mu.Unlock()
	close(j.finished)
}
func (j *gradioJob) result() ([]any, error) { j.mu.Lock(); defer j.mu.Unlock(); return j.data, j.err }

type gradioRequest struct {
	Data    []any `json:"data"`
	FnIndex *int  `json:"fn_index,omitempty"`
}

func (a *App) gradioPredict(w http.ResponseWriter, r *http.Request) {
	raw, fnIndex, err := a.parseGradioRequest(w, r)
	if err != nil {
		writePredictionError(w, err)
		return
	}
	if fnIndex == 1 {
		a.gradioReset(w, r)
		return
	}
	data, err := a.executeGradio(r.Context(), raw)
	if err != nil {
		writePredictionError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": data, "duration": 0.01, "average_duration": 0.01, "is_generating": false, "success": true})
}
func (a *App) parseGradioRequest(w http.ResponseWriter, r *http.Request) ([]any, int, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()
	var req gradioRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, 0, failPrediction(400, "JSON_INVALIDO", "Solicitud inválida")
	}
	if req.FnIndex != nil {
		if *req.FnIndex == 1 {
			return nil, 1, nil
		}
		if *req.FnIndex != 0 {
			return nil, 0, failPrediction(404, "EVENTO", "Evento desconocido")
		}
	}
	if len(req.Data) != len(a.Config.Inputs) {
		return nil, 0, failPrediction(400, "ENTRADAS", "Cantidad incorrecta de entradas")
	}
	data := make([]any, len(req.Data))
	for i, v := range req.Data {
		if !isMedia(a.Config.Inputs[i].Type) {
			data[i] = v
			continue
		}
		if v == nil {
			data[i] = nil
			continue
		}
		// Gradio VideoData usa {video: FileData, subtitles: FileData?}.
		if a.Config.Inputs[i].Type == "video" {
			if videoData, ok := v.(map[string]any); ok {
				if nested, exists := videoData["video"]; exists {
					if nested == nil {
						data[i] = nil
						continue
					}
					v = nested
				}
			}
		}
		var ref string
		switch value := v.(type) {
		case string:
			ref = value
		case map[string]any:
			if path, ok := value["path"].(string); ok {
				ref = path
			} else if u, ok := value["url"].(string); ok {
				ref = u
			}
		default:
			return nil, 0, failPrediction(400, "ARCHIVO", "Referencia multimedia incorrecta")
		}
		ref = strings.TrimPrefix(ref, "/gradio_api/file=")
		ref = strings.TrimPrefix(ref, "/api/media/")
		if strings.HasPrefix(ref, "http:") || strings.HasPrefix(ref, "https:") || !fileID.MatchString(ref) {
			return nil, 0, failPrediction(400, "ARCHIVO", "Referencia multimedia no permitida")
		}
		data[i] = "/api/media/" + ref
	}
	return data, 0, nil
}
func (a *App) executeGradio(ctx context.Context, raw []any) ([]any, error) {
	results, err := a.predictValues(ctx, raw)
	if err != nil {
		return nil, err
	}
	for i, c := range a.Config.Outputs {
		if !isMedia(c.Type) || results[i] == nil {
			continue
		}
		s, ok := results[i].(string)
		if !ok || s == "" {
			continue
		}
		id := strings.TrimPrefix(s, "/api/media/")
		if !fileID.MatchString(id) {
			return nil, failPrediction(500, "ARCHIVO", "Resultado multimedia inválido")
		}
		info, err := os.Stat(filepath.Join(a.MediaDir, id))
		if err != nil {
			return nil, err
		}
		fileData := map[string]any{
			"path": id, "url": "/gradio_api/file=" + id, "orig_name": id,
			"size": info.Size(), "mime_type": mime.TypeByExtension(filepath.Ext(id)),
			"meta": map[string]any{"_type": "gradio.FileData"},
		}
		if c.Type == "video" {
			results[i] = map[string]any{"video": fileData, "subtitles": nil}
		} else {
			results[i] = fileData
		}
	}
	return results, nil
}
func eventID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func (a *App) gradioCall(w http.ResponseWriter, r *http.Request) {
	data, fnIndex, err := a.parseGradioRequest(w, r)
	if err != nil {
		writePredictionError(w, err)
		return
	}
	id, err := eventID()
	if err != nil {
		apiError(w, 500, "INTERNO", "Fallo generando id")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	job := &gradioJob{created: time.Now(), finished: make(chan struct{}), cancel: cancel}
	a.jobsMu.Lock()
	if len(a.jobs) > 100 {
		for key, item := range a.jobs {
			if time.Since(item.created) > 2*time.Minute {
				delete(a.jobs, key)
			}
		}
	}
	if len(a.jobs) > 100 {
		a.jobsMu.Unlock()
		cancel()
		apiError(w, 429, "LIMITE", "Demasiadas ejecuciones pendientes")
		return
	}
	a.jobs[id] = job
	a.jobsMu.Unlock()
	go func() {
		defer cancel()
		var results []any
		var runErr error
		if fnIndex == 1 {
			results = a.resetValues()
		} else {
			results, runErr = a.executeGradio(ctx, data)
		}
		job.complete(results, runErr)
	}()
	writeJSON(w, 200, map[string]string{"event_id": id})
}
func (a *App) gradioEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if len(id) != 32 || !fileID.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	a.jobsMu.Lock()
	job, ok := a.jobs[id]
	a.jobsMu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	select {
	case <-job.finished:
	case <-r.Context().Done():
		return
	}
	data, err := job.result()
	event := "complete"
	payload := map[string]any{"data": data}
	if err != nil {
		event = "error"
		payload = map[string]any{"error": "La función produjo un error"}
		log.Printf("evento Goradio: %v", err)
	}
	raw, _ := json.Marshal(payload)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
	if flush, ok := w.(http.Flusher); ok {
		flush.Flush()
	}
	a.jobsMu.Lock()
	delete(a.jobs, id)
	a.jobsMu.Unlock()
}

// uploadState refleja los bytes realmente leídos del multipart, no solo una animación.
type uploadState struct {
	start   time.Time
	done    chan struct{}
	changed chan struct{}
	mu      sync.Mutex
	bytes   map[string]int64
}

var uploadIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

func (s *uploadState) add(name string, n int64) {
	if s == nil || n <= 0 {
		return
	}
	s.mu.Lock()
	s.bytes[name] += n
	s.mu.Unlock()
	select {
	case s.changed <- struct{}{}:
	default:
	}
}
func (s *uploadState) snapshot() map[string]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[string]int64, len(s.bytes))
	for name, n := range s.bytes {
		result[name] = n
	}
	return result
}
func (a *App) registerUpload(id string) (*uploadState, func(), error) {
	if id == "" {
		return nil, func() {}, nil
	}
	if !uploadIDPattern.MatchString(id) {
		return nil, nil, errors.New("upload_id inválido")
	}
	state := &uploadState{start: time.Now(), done: make(chan struct{}), changed: make(chan struct{}, 1), bytes: map[string]int64{}}
	a.uploadsMu.Lock()
	for key, old := range a.uploads {
		if time.Since(old.start) > 3*time.Minute {
			delete(a.uploads, key)
		}
	}
	if len(a.uploads) >= 100 {
		a.uploadsMu.Unlock()
		return nil, nil, errors.New("Demasiadas cargas simultáneas")
	}
	if _, exists := a.uploads[id]; exists {
		a.uploadsMu.Unlock()
		return nil, nil, errors.New("upload_id duplicado")
	}
	a.uploads[id] = state
	a.uploadsMu.Unlock()
	return state, func() { close(state.done) }, nil
}
func (a *App) gradioUploadProgress(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("upload_id")
	if !uploadIDPattern.MatchString(id) {
		http.Error(w, "upload_id inválido", 400)
		return
	}
	var state *uploadState
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
	for state == nil {
		a.uploadsMu.Lock()
		state = a.uploads[id]
		a.uploadsMu.Unlock()
		if state != nil {
			break
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			http.NotFound(w, r)
			return
		case <-r.Context().Done():
			return
		}
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	flush, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming no disponible", 500)
		return
	}
	fmt.Fprint(w, ": connected\n\n")
	flush.Flush()
	sent := map[string]int64{}
	broadcast := func() {
		for name, count := range state.snapshot() {
			diff := count - sent[name]
			if diff <= 0 {
				continue
			}
			event, _ := json.Marshal(map[string]any{"orig_name": name, "chunk_size": diff})
			fmt.Fprintf(w, "data: %s\n\n", event)
			sent[name] = count
		}
		flush.Flush()
	}
	timeout := time.NewTimer(95 * time.Second)
	defer timeout.Stop()
	cadence := time.NewTicker(150 * time.Millisecond)
	defer cadence.Stop()
	for {
		select {
		case <-state.changed:
			broadcast()
		case <-cadence.C:
			broadcast()
		case <-state.done:
			broadcast()
			fmt.Fprint(w, "data: {\"msg\":\"done\"}\n\n")
			flush.Flush()
			return
		case <-timeout.C:
			return
		case <-r.Context().Done():
			return
		}
	}
}

// progressReader registra los bytes recibidos durante la subida.
type progressReader struct {
	reader io.Reader
	onRead func(int)
}

func (p progressReader) Read(b []byte) (int, error) {
	n, err := p.reader.Read(b)
	if n > 0 && p.onRead != nil {
		p.onRead(n)
	}
	return n, err
}
func (a *App) gradioUpload(w http.ResponseWriter, r *http.Request) {
	state, finish, err := a.registerUpload(r.URL.Query().Get("upload_id"))
	if err != nil {
		apiError(w, 400, "SUBIDA", err.Error())
		return
	}
	defer finish()
	limit := a.MaxUploadBytes
	if limit <= 0 || limit > 200<<20 {
		limit = 40 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit+(1<<20))
	defer r.Body.Close()
	multipartReader, err := r.MultipartReader()
	if err != nil {
		apiError(w, 400, "SUBIDA", "Formulario multipart inválido")
		return
	}
	saved := []string{}
	success := false
	defer func() {
		if !success {
			for _, name := range saved {
				os.Remove(filepath.Join(a.MediaDir, name))
			}
		}
	}()
	for {
		part, err := multipartReader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			apiError(w, 400, "SUBIDA", "Error leyendo archivos")
			return
		}
		if part.FormName() != "files" || part.FileName() == "" {
			_, err = io.Copy(io.Discard, io.LimitReader(part, 1024))
			part.Close()
			if err != nil {
				apiError(w, 400, "SUBIDA", "Campo multipart inválido")
				return
			}
			continue
		}
		if len(saved) >= 10 {
			part.Close()
			apiError(w, 400, "SUBIDA", "Máximo 10 archivos")
			return
		}
		origName := filepath.Base(part.FileName())
		monitored := progressReader{reader: part, onRead: func(n int) {
			if state != nil {
				state.add(origName, int64(n))
			}
		}}
		// El límite total del request también está protegido por MaxBytesReader.
		name, err := a.storeGradioFile(monitored, origName, limit)
		part.Close()
		if err != nil {
			apiError(w, 400, "SUBIDA", err.Error())
			return
		}
		saved = append(saved, name)
	}
	if len(saved) == 0 {
		apiError(w, 400, "SUBIDA", "No se recibieron archivos")
		return
	}
	success = true
	writeJSON(w, 200, saved)
}

var safeExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".mp3": true, ".wav": true, ".ogg": true, ".flac": true, ".m4a": true, ".mp4": true, ".webm": true, ".mov": true, ".pdf": true, ".txt": true, ".csv": true, ".json": true, ".zip": true}

func (a *App) storeGradioFile(file io.Reader, filename string, max int64) (string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	if !safeExt[ext] {
		return "", errors.New("Extensión no permitida")
	}
	name, err := eventID()
	if err != nil {
		return "", err
	}
	name += ext
	full := filepath.Join(a.MediaDir, name)
	dest, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	defer dest.Close()
	n, err := io.Copy(dest, io.LimitReader(file, max+1))
	if err != nil || n > max {
		os.Remove(full)
		return "", errors.New("Archivo excede límite")
	}
	if n == 0 {
		os.Remove(full)
		return "", errors.New("Archivo vacío")
	}
	return name, nil
}
func (a *App) gradioFile(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/gradio_api/file=")
	if !fileID.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(a.MediaDir, id)
	if _, err := os.Stat(full); err != nil {
		http.NotFound(w, r)
		return
	}
	ext := strings.ToLower(filepath.Ext(id))
	if ext == ".svg" || ext == ".html" || ext == ".js" || ext == ".json" || ext == ".txt" || ext == ".csv" || ext == ".pdf" {
		w.Header().Set("Content-Disposition", "attachment")
	}
	setSafeMediaHeaders(w, id)
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeFile(w, r, full)
}

func (a *App) resetValues() []any {
	values := make([]any, 0, len(a.Config.Inputs)+len(a.Config.Outputs))
	for _, c := range append(append([]Component{}, a.Config.Inputs...), a.Config.Outputs...) {
		var value any
		switch c.Type {
		case "textbox":
			value = ""
		case "number", "slider":
			if c.Value != nil {
				value = c.Value
			} else if c.Min != nil {
				value = *c.Min
			} else {
				value = 0
			}
		case "checkbox":
			value = false
		case "json":
			value = map[string]any{}
		default:
			value = nil
		}
		values = append(values, value)
	}
	return values
}

func (a *App) gradioReset(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"data": a.resetValues(), "duration": 0.0, "average_duration": 0.0, "is_generating": false, "success": true})
}
func setSafeMediaHeaders(w http.ResponseWriter, id string) {
	ext := strings.ToLower(filepath.Ext(id))
	typ := mime.TypeByExtension(ext)
	if typ == "" {
		typ = "application/octet-stream"
	}
	w.Header().Set("Content-Type", typ)
	switch ext {
	case ".png", ".jpeg", ".jpg", ".gif", ".webp", ".wav", ".mp3", ".ogg", ".flac", ".m4a", ".mp4", ".webm", ".mov":
		// Media types can be previewed inline.
	default:
		w.Header().Set("Content-Disposition", "attachment")
	}
}
