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
	session  string
	finished chan struct{}
	cancel   context.CancelFunc
	mu       sync.Mutex
	data     []any
	err      error
	progress chan []any
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
	Data        []any  `json:"data"`
	FnIndex     *int   `json:"fn_index,omitempty"`
	SessionHash string `json:"session_hash,omitempty"`
}

// classifyFn maps native frontend dependency indices to handler operations.
// Indices 0 and 1 remain Submit and Clear for backwards compatibility.
func (a *App) classifyFn(index int) (autosave bool, inputIndex int, valid bool) {
	if index == 0 || index == 1 {
		return false, 0, true
	}
	reactiveCount := 0
	if a.Config.Load {
		reactiveCount++
	}
	if a.Config.Live {
		reactiveCount += 2 * len(a.Config.Inputs)
	}
	if a.Config.EverySeconds > 0 {
		reactiveCount++
	}
	if index >= 2 && index < 2+reactiveCount {
		return false, 0, true
	}
	base := 2 + reactiveCount
	if a.security != nil && a.security.cfg.PersistInputs && index >= base && index < base+2*len(a.Config.Inputs) {
		return true, (index - base) / 2, true
	}
	if a.security != nil && a.security.cfg.PersistInputs {
		base += 2 * len(a.Config.Inputs)
	}
	if index >= base && index < base+len(a.Config.Events) {
		return false, 0, true
	}
	return false, 0, false
}
func (a *App) gradioPredict(w http.ResponseWriter, r *http.Request) {
	raw, fnIndex, err := a.parseGradioRequest(w, r)
	if err != nil {
		writePredictionError(w, err)
		return
	}
	if fnIndex == 1 {
		id, _ := IdentityFromContext(r.Context())
		if err := a.deleteStoredState(r.Context(), id); err != nil {
			http.Error(w, "Error limpiando estado", 500)
			return
		}
		a.gradioReset(w, r)
		return
	}
	if saved, idx, _ := a.classifyFn(fnIndex); saved {
		a.persistHandler(w, r, idx, raw[0])
		return
	}
	ctx := withGradioSession(r.Context(), r.Header.Get("X-Goradio-Session"))
	var data []any
	if _, _, custom := a.customEventForIndex(fnIndex); custom {
		data, err = a.executeCustomEvent(ctx, fnIndex, raw)
	} else {
		data, err = a.executeGradio(ctx, raw)
	}
	if err != nil {
		writePredictionError(w, err)
		return
	}
	id, _ := IdentityFromContext(r.Context())
	if _, _, custom := a.customEventForIndex(fnIndex); !custom {
		if err := a.storeResults(r.Context(), id, raw, data); err != nil {
			log.Printf("persistencia de predicción: %v", err)
		}
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
	fnIndex := 0
	if req.FnIndex != nil {
		fnIndex = *req.FnIndex
	}
	if req.SessionHash != "" {
		if !gradioSessionHash.MatchString(req.SessionHash) {
			return nil, 0, failPrediction(400, "SESION", "Identificador de sesión inválido")
		}
		r.Header.Set("X-Goradio-Session", req.SessionHash)
	}
	saved, _, valid := a.classifyFn(fnIndex)
	if !valid {
		return nil, 0, failPrediction(404, "EVENTO", "Evento desconocido")
	}
	if fnIndex == 1 {
		return nil, 1, nil
	}
	if saved {
		if len(req.Data) != 1 {
			return nil, 0, failPrediction(400, "ENTRADAS", "Guardado requiere una entrada")
		}
		return req.Data, fnIndex, nil
	}
	inputs := a.Config.Inputs
	if spec, _, custom := a.customEventForIndex(fnIndex); custom {
		inputs = a.customEventInputComponents(spec)
	}
	if len(req.Data) != len(inputs) {
		return nil, 0, failPrediction(400, "ENTRADAS", "Cantidad incorrecta de entradas")
	}
	data := make([]any, len(req.Data))
	for i, v := range req.Data {
		if !isMedia(inputs[i].Type) {
			data[i] = v
			continue
		}
		if v == nil {
			data[i] = nil
			continue
		}
		// Gradio VideoData usa {video: FileData, subtitles: FileData?}.
		if inputs[i].Type == "video" {
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
	return data, fnIndex, nil
}

// Gradio 6 Chatbot expects content to be an array of typed message parts,
// even when the handler returns plain text. Preserve metadata and roles.
func normalizeGradioChatMessages(value any) ([]any, error) {
	if value == nil {
		return []any{}, nil
	}
	var raw []any
	switch typed := value.(type) {
	case []any:
		raw = typed
	case []map[string]any:
		raw = make([]any, len(typed))
		for i, item := range typed {
			raw[i] = item
		}
	default:
		return nil, errors.New("Chatbot necesita una lista de mensajes")
	}
	if len(raw) > 500 {
		return nil, errors.New("Chatbot excede el límite de mensajes")
	}
	messages := make([]any, 0, len(raw))
	for _, element := range raw {
		message, ok := element.(map[string]any)
		if !ok {
			return nil, errors.New("Mensaje de Chatbot inválido")
		}
		role, ok := message["role"].(string)
		if !ok || (role != "user" && role != "assistant" && role != "system") {
			return nil, errors.New("Rol de Chatbot inválido")
		}
		copyMessage := make(map[string]any, len(message))
		for k, v := range message {
			copyMessage[k] = v
		}
		switch content := message["content"].(type) {
		case string:
			copyMessage["content"] = []any{map[string]any{"type": "text", "text": content}}
		case []any:
			for _, item := range content {
				part, ok := item.(map[string]any)
				if !ok || part["type"] == nil {
					return nil, errors.New("Bloque de Chatbot inválido")
				}
			}
		default:
			return nil, errors.New("Contenido de Chatbot inválido")
		}
		messages = append(messages, copyMessage)
	}
	return messages, nil
}

func (a *App) executeGradio(ctx context.Context, raw []any) ([]any, error) {
	results, err := a.predictValues(ctx, raw)
	if err != nil {
		return nil, err
	}
	return a.prepareGradioOutputs(ctx, results)
}

// prepareGradioOutputs applies the same media, gallery and chatbot conversion
// to final and intermediate streaming messages.
func (a *App) prepareGradioOutputs(ctx context.Context, results []any) ([]any, error) {
	if len(results) != len(a.Config.Outputs) {
		return nil, failPrediction(500, "SALIDAS", "Número de salidas incorrecto")
	}
	for i, c := range a.Config.Outputs {
		if patch, ok := results[i].(map[string]any); ok && patch["__type__"] == "update" {
			continue
		}
		if c.Type == "chatbot" {
			messages, err := normalizeGradioChatMessages(results[i])
			if err != nil {
				return nil, failPrediction(400, "CHATBOT", err.Error())
			}
			results[i] = messages
			continue
		}
		if c.Type == "gallery" && results[i] != nil {
			items, ok := results[i].([]string)
			if !ok || len(items) > 40 {
				return nil, failPrediction(400, "GALERIA", "La galería admite hasta 40 rutas locales registradas")
			}
			collection := make([]any, 0, len(items))
			for _, source := range items {
				base, err := filepath.Abs(a.MediaDir)
				if err != nil {
					return nil, err
				}
				path, err := filepath.Abs(filepath.Clean(source))
				if err != nil {
					return nil, err
				}
				rel, err := filepath.Rel(base, path)
				if err != nil || rel == "." || strings.HasPrefix(rel, "..") ||
					strings.Contains(rel, string(filepath.Separator)) || !fileID.MatchString(rel) {
					return nil, failPrediction(400, "GALERIA", "Archivo fuera del directorio multimedia")
				}
				info, err := os.Stat(path)
				if err != nil || info.IsDir() {
					return nil, failPrediction(400, "GALERIA", "Imagen de galería no encontrada")
				}
				if err = a.recordFile(ctx, rel); err != nil {
					return nil, err
				}
				file := map[string]any{
					"path": rel, "url": "/gradio_api/file=" + rel,
					"orig_name": rel, "size": info.Size(), "mime_type": mime.TypeByExtension(filepath.Ext(rel)),
					"meta": map[string]any{"_type": "gradio.FileData"},
				}
				collection = append(collection, map[string]any{"image": file, "caption": nil})
			}
			results[i] = collection
			continue
		}
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
		if err := a.recordFile(ctx, id); err != nil {
			return nil, err
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
	ctx, cancel := context.WithTimeout(context.WithValue(withGradioSession(context.Background(), r.Header.Get("X-Goradio-Session")), ctxIdentityKey{}, func() Identity { id, _ := IdentityFromContext(r.Context()); return id }()), 90*time.Second)
	job := &gradioJob{created: time.Now(), session: r.Header.Get("X-Goradio-Session"), finished: make(chan struct{}), progress: make(chan []any, 16), cancel: cancel}
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
	jobIdentity, _ := IdentityFromContext(r.Context())
	go func() {
		defer cancel()
		var results []any
		var runErr error
		idUser := jobIdentity
		if fnIndex == 1 {
			_ = a.deleteStoredState(ctx, idUser)
			results = a.resetValues()
		} else if saved, idx, _ := a.classifyFn(fnIndex); saved {
			runErr = a.storeInput(ctx, idUser, idx, data[0])
			results = []any{}
		} else {
			if spec, stream, streaming := a.customStreamForIndex(fnIndex); streaming {
				if stream == nil {
					runErr = failPrediction(503, "EVENTO", "Generador de evento no vinculado")
				} else {
					var normalized []any
					normalized, runErr = a.normalizeCustomEventInputs(fnIndex, data)
					if runErr == nil {
						last := []any(nil)
						runErr = stream(ctx, normalized, func(snapshot []any) error {
							if err := ctx.Err(); err != nil {
								return err
							}
							prepared, err := a.prepareCustomEventOutputs(ctx, spec, snapshot)
							if err != nil {
								return err
							}
							last = prepared
							// Bounded queue: keep final state even if partial frames
							// must be discarded for a slow/disconnected browser.
							select {
							case job.progress <- prepared:
							default:
								select {
								case <-job.progress:
								default:
								}
								select {
								case job.progress <- prepared:
								default:
								}
							}
							return nil
						})
						if runErr == nil && last == nil {
							runErr = failPrediction(500, "EVENTO", "El generador no produjo resultados")
						}
						results = last
					}
				}
			} else if _, _, custom := a.customEventForIndex(fnIndex); custom {
				results, runErr = a.executeCustomEvent(ctx, fnIndex, data)
			} else if a.Stream != nil {
				normalized := make([]any, len(data))
				for i, c := range a.Config.Inputs {
					normalized[i], runErr = a.validate(c, data[i])
					if runErr != nil {
						runErr = failPrediction(400, "VALIDACION", fmt.Sprintf("Entrada %d: %v", i+1, runErr))
						break
					}
				}
				if runErr == nil && a.Validator != nil {
					runErr = a.Validator(ctx, normalized)
				}
				if runErr == nil {
					last := []any(nil)
					lastRaw := []any(nil)
					runErr = a.Stream(ctx, normalized, func(partial []any) error {
						lastRaw = partial
						if err := ctx.Err(); err != nil {
							return err
						}
						prepared, err := a.prepareGradioOutputs(ctx, partial)
						if err != nil {
							return err
						}
						last = prepared
						// Bound memory usage even if the browser is slow.
						select {
						case job.progress <- prepared:
						default:
							select {
							case <-job.progress:
							default:
							}
							select {
							case job.progress <- prepared:
							default:
							}
						}
						return nil
					})
					if runErr == nil && last == nil {
						runErr = errors.New("La función de streaming no produjo ningún resultado")
					}
					if runErr == nil {
						_, runErr = a.runThen(ctx, lastRaw, func(values []any) error {
							prepared, err := a.prepareGradioOutputs(ctx, values)
							if err != nil {
								return err
							}
							last = prepared
							select {
							case job.progress <- prepared:
							default:
								select {
								case <-job.progress:
								default:
								}
								select {
								case job.progress <- prepared:
								default:
								}
							}
							return nil
						})
					}
					results = last
				}
			} else {
				results, runErr = a.executeGradio(ctx, data)
			}
			if runErr == nil {
				if _, _, custom := a.customEventForIndex(fnIndex); !custom {
					_ = a.storeResults(ctx, idUser, data, results)
				}
			}
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
	defer func() { a.jobsMu.Lock(); delete(a.jobs, id); a.jobsMu.Unlock() }()
	flush, ok := w.(http.Flusher)
	if !ok {
		return
	}
	send := func(event string, payload any) {
		raw, _ := json.Marshal(payload)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
		flush.Flush()
	}
	// Stream complete output snapshots; each "generating" SSE frame is a real
	// update from the Go handler, not an animated placeholder.
	for {
		select {
		case data := <-job.progress:
			send("generating", map[string]any{"data": data, "is_generating": true})
		case <-job.finished:
			// Forward any queued partial updates before the final frame.
			for {
				select {
				case data := <-job.progress:
					send("generating", map[string]any{"data": data, "is_generating": true})
				default:
					goto completed
				}
			}
		completed:
			data, err := job.result()
			if err != nil {
				log.Printf("evento Goradio: %v", err)
				send("error", map[string]any{"error": "La función produjo un error"})
			}
			if err == nil {
				send("complete", map[string]any{"data": data, "is_generating": false})
			}
			return
		case <-r.Context().Done():
			// User left the SSE endpoint: terminate the worker promptly.
			job.cancel()
			return
		}
	}
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
		if err := a.recordFile(r.Context(), name); err != nil {
			os.Remove(filepath.Join(a.MediaDir, name))
			apiError(w, 500, "ARCHIVO", "Error de almacenamiento")
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
	if !fileID.MatchString(id) || !a.canAccessFile(r.Context(), id) {
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
	id, _ := IdentityFromContext(r.Context())
	_ = a.deleteStoredState(r.Context(), id)

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
