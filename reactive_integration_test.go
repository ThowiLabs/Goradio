package goradio

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReactiveStateIsolationAndLimits(t *testing.T) {
	a := makeReactiveApp(t)
	sessionA := withGradioSession(context.Background(), "aabbccdd12345678")
	sessionB := withGradioSession(context.Background(), "bbaaddcc87654321")
	if err := a.StateSet(sessionA, "counter", 10); err != nil {
		t.Fatal(err)
	}
	var counter int
	if a.StateGet(sessionB, "counter", &counter) {
		t.Fatal("state leaked between browser sessions")
	}
	if !a.StateGet(sessionA, "counter", &counter) || counter != 10 {
		t.Fatalf("incorrect state %d", counter)
	}
	if err := a.StateSet(context.Background(), "x", 1); err == nil {
		t.Fatal("accepted state without a session")
	}
	if err := a.StateSet(sessionA, "oversized", strings.Repeat("x", 65536)); err == nil {
		t.Fatal("oversized state accepted")
	}
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if err := a.StateSet(sessionA, fmt.Sprintf("k%d", n), n); err != nil {
				t.Error(err)
			}
		}(n)
	}
	wg.Wait()
	for n := 0; n < 20; n++ {
		var v int
		if !a.StateGet(sessionA, fmt.Sprintf("k%d", n), &v) || v != n {
			t.Fatalf("state key %d wrong", n)
		}
	}
}
func TestNativeGradioQueueStreamsPatches(t *testing.T) {
	a := makeReactiveApp(t)
	a.SetStreamHandler(func(ctx context.Context, data []any, emit func([]any) error) error {
		for i := 0; i < 3; i++ {
			if err := emit([]any{fmt.Sprintf("Etapa %d", i+1)}); err != nil {
				return err
			}
			time.Sleep(20 * time.Millisecond)
		}
		return nil
	})
	server := httptest.NewServer(a.Routes())
	defer server.Close()
	body := []byte(`{"data":["inicio"],"fn_index":0,"session_hash":"abcdef1234567890"}`)
	resp, err := http.Post(server.URL+"/gradio_api/queue/join", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var joined struct {
		EventID string `json:"event_id"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&joined); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || joined.EventID == "" {
		t.Fatalf("queue join %d: %+v", resp.StatusCode, joined)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/gradio_api/queue/data?session_hash=abcdef1234567890", nil)
	eventStream, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer eventStream.Body.Close()
	scanner := bufio.NewScanner(eventStream.Body)
	var frames []map[string]any
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		if event["event_id"] != joined.EventID {
			t.Fatalf("wrong event ID %#v", event)
		}
		frames = append(frames, event)
		if event["msg"] == "process_completed" {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range frames {
		if event["msg"] != "process_generating" {
			continue
		}
		count++
		output := event["output"].(map[string]any)["data"].([]any)
		if len(output) != 1 {
			t.Fatalf("invalid output %#v", output)
		}
		if count > 1 {
			patches, ok := output[0].([]any)
			if !ok || len(patches) != 1 {
				t.Fatalf("missing sse_v3 diff %#v", output)
			}
			patch := patches[0].([]any)
			if patch[0] != "replace" {
				t.Fatalf("invalid diff %#v", patch)
			}
		}
	}
	if count != 3 {
		t.Fatalf("expected 3 progressive frames, received %d: %#v", count, frames)
	}
	if frames[len(frames)-1]["msg"] != "process_completed" {
		t.Fatal("no final frame")
	}
}
func TestPythonGeneratorBridgeNDJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback/stream" || r.Header.Get("Authorization") != "Bearer demo" {
			http.Error(w, "Forbidden", 403)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		f := w.(http.Flusher)
		for _, label := range []string{"inicio", "mitad", "final"} {
			fmt.Fprintf(w, "{\"data\":[%q]}\n", label)
			f.Flush()
		}
	}))
	defer server.Close()
	var values []string
	handler := PythonStreamHandler(server.URL+"/callback", "demo")
	if err := handler(context.Background(), []any{"request"}, func(valuesPart []any) error {
		values = append(values, valuesPart[0].(string))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(values) != "[inicio mitad final]" {
		t.Fatal(values)
	}
	abort := errors.New("cancelled")
	err := handler(context.Background(), []any{"request"}, func([]any) error { return abort })
	if !errors.Is(err, abort) {
		t.Fatalf("emitter error ignored: %v", err)
	}
}
func TestGradioUpdateContract(t *testing.T) {
	yes, no := true, false
	out := Update("Hola", &yes, &no)
	if out["__type__"] != "update" || out["value"] != "Hola" || out["visible"] != true || out["interactive"] != false {
		t.Fatalf("%#v", out)
	}
	a := makeReactiveApp(t)
	result, err := a.prepareGradioOutputs(context.Background(), []any{out})
	if err != nil || result[0].(map[string]any)["__type__"] != "update" {
		t.Fatalf("%v %v", result, err)
	}
}

func TestGoThenWithStreamingAndValidation(t *testing.T) {
	app := makeReactiveApp(t)
	app.SetValidator(func(_ context.Context, data []any) error {
		if data[0] == "rejected" {
			return errors.New("No permitido")
		}
		return nil
	})
	if err := app.Then(func(_ context.Context, data []any) ([]any, error) {
		return []any{data[0].(string) + "-then"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	output, err := app.predictValues(context.Background(), []any{"hello"})
	if err != nil || output[0] != "hello-then" {
		t.Fatalf("Then not applied: %v %v", output, err)
	}
	if _, err := app.predictValues(context.Background(), []any{"rejected"}); err == nil {
		t.Fatal("validator bypassed")
	}
	app.SetStreamHandler(func(_ context.Context, _ []any, emit func([]any) error) error {
		return emit([]any{"stream"})
	})
	call := httptest.NewRecorder()
	app.Routes().ServeHTTP(call, httptest.NewRequest("POST", "/gradio_api/call/predict", strings.NewReader(`{"data":["ok"]}`)))
	var start struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(call.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	app.Routes().ServeHTTP(res, httptest.NewRequest("GET", "/gradio_api/call/predict/"+start.EventID, nil))
	body := res.Body.String()
	if !strings.Contains(body, "stream-then") || strings.Count(body, "event: generating") != 2 {
		t.Fatalf("chained stream missing: %s", body)
	}
}
func TestQueueCancelRequiresCorrectSession(t *testing.T) {
	app := makeReactiveApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id := "0123456789abcdef0123456789abcdef"
	job := &gradioJob{session: "aabbccdd12345678", cancel: cancel, finished: make(chan struct{}), progress: make(chan []any, 2)}
	app.jobsMu.Lock()
	app.jobs[id] = job
	app.jobsMu.Unlock()
	h := app.Routes()
	wrong := httptest.NewRecorder()
	h.ServeHTTP(wrong, httptest.NewRequest("POST", "/gradio_api/queue/cancel",
		strings.NewReader(`{"event_id":"0123456789abcdef0123456789abcdef","session_hash":"wrongsession1234"}`)))
	if wrong.Code != 404 || ctx.Err() != nil {
		t.Fatalf("Unauthorized cancellation accepted %d", wrong.Code)
	}
	right := httptest.NewRecorder()
	h.ServeHTTP(right, httptest.NewRequest("POST", "/gradio_api/queue/cancel",
		strings.NewReader(`{"event_id":"0123456789abcdef0123456789abcdef","session_hash":"aabbccdd12345678"}`)))
	if right.Code != 200 || ctx.Err() == nil {
		t.Fatalf("Cancellation failed %d", right.Code)
	}
}
func TestNativeLoadConfigAndIndex(t *testing.T) {
	app := makeReactiveApp(t)
	app.Config.Load = true
	deps := app.gradioConfig()["dependencies"].([]any)
	if len(deps) != 6 {
		t.Fatalf("expected load + change + input + timer: %d", len(deps))
	}
	load := deps[2].(map[string]any)
	if load["targets"].([]any)[0].([]any)[1] != "load" {
		t.Fatalf("missing native load %#v", load)
	}
	for i := 2; i < 6; i++ {
		if save, _, ok := app.classifyFn(i); !ok || save {
			t.Fatalf("event %d wrongly classified", i)
		}
	}
}
