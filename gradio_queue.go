package goradio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"
)

// Gradio's original frontend uses one SSE connection per browser session.
// Queue events are associated with the unguessable event ID and the session
// hash provided by the official Gradio client.
var gradioSessionHash = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,128}$`)

func (a *App) gradioQueueJoin(w http.ResponseWriter, r *http.Request) {
	if a.Stream == nil && !a.hasStreamingEvents() {
		http.Error(w, "No streaming handler", http.StatusNotFound)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		apiError(w, 400, "COLA", "Solicitud demasiado grande")
		return
	}
	var request struct {
		SessionHash string `json:"session_hash"`
	}
	if err := json.Unmarshal(body, &request); err != nil || !gradioSessionHash.MatchString(request.SessionHash) {
		apiError(w, 400, "COLA", "session_hash inválido")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.Header.Set("X-Goradio-Session", request.SessionHash)
	a.gradioCall(w, r)
}

// The SSE connection remains open, so subsequent native Gradio events in the
// same browser session can reuse it. A bounded ticker avoids busy spinning;
// HTTP cancellation terminates immediately when the browser navigates away.
func (a *App) gradioQueueData(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session_hash")
	if !gradioSessionHash.MatchString(session) {
		apiError(w, 400, "COLA", "session_hash inválido")
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "SSE no disponible", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	flusher := w.(http.Flusher)
	send := func(msg map[string]any) bool {
		encoded, err := json.Marshal(msg)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", encoded); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if _, err := fmt.Fprint(w, ": connected\n\n"); err != nil {
		return
	}
	flusher.Flush()
	pending := map[string]bool{}
	generating := map[string]bool{}
	ticker := time.NewTicker(35 * time.Millisecond)
	heartbeat := time.NewTicker(12 * time.Second)
	defer ticker.Stop()
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			type queued struct {
				id  string
				job *gradioJob
			}
			jobs := []queued{}
			a.jobsMu.Lock()
			for id, job := range a.jobs {
				if job.session == session {
					jobs = append(jobs, queued{id, job})
				}
			}
			a.jobsMu.Unlock()
			for _, entry := range jobs {
				id, job := entry.id, entry.job
				if !pending[id] {
					if !send(map[string]any{"msg": "process_starts", "event_id": id, "eta": nil}) {
						return
					}
					pending[id] = true
				}
				for len(job.progress) > 0 {
					part := <-job.progress
					var output any = part
					if generating[id] {
						// Gradio sse_v3 needs patches after the first full frame.
						diffs := make([]any, len(part))
						for i, value := range part {
							diffs[i] = []any{[]any{"replace", []any{}, value}}
						}
						output = diffs
					}
					generating[id] = true
					if !send(map[string]any{"msg": "process_generating", "event_id": id,
						"output":  map[string]any{"data": output, "is_generating": true},
						"success": true}) {
						return
					}
				}
				select {
				case <-job.finished:
					data, err := job.result()
					status := err == nil
					output := map[string]any{"data": data, "is_generating": false}
					if err != nil {
						output = map[string]any{"error": "Error ejecutando la función"}
					}
					if !send(map[string]any{"msg": "process_completed", "event_id": id, "output": output, "success": status}) {
						return
					}
					a.jobsMu.Lock()
					if a.jobs[id] == job {
						delete(a.jobs, id)
					}
					a.jobsMu.Unlock()
					delete(pending, id)
					delete(generating, id)
				default:
				}
			}
		}
	}
}

func (a *App) gradioQueueCancel(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	defer r.Body.Close()
	var request struct {
		SessionHash string `json:"session_hash"`
		EventID     string `json:"event_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || !gradioSessionHash.MatchString(request.SessionHash) || len(request.EventID) != 32 || !fileID.MatchString(request.EventID) {
		apiError(w, 400, "COLA", "Datos de cancelación inválidos")
		return
	}
	a.jobsMu.Lock()
	job := a.jobs[request.EventID]
	a.jobsMu.Unlock()
	if job == nil || job.session != request.SessionHash {
		http.NotFound(w, r)
		return
	}
	job.cancel()
	writeJSON(w, 200, map[string]any{"success": true})
}
