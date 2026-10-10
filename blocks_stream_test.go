package goradio

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBlocksSeparateStreamAndOrdinaryEvents(t *testing.T) {
	layout := Column(Ref("query"), Row(Button("progress", "Progreso"), Button("instant", "Inmediato")), Row(Ref("stream_out"), Ref("instant_out")))
	app, err := New(Config{Title: "Blocks streaming", Layout: &layout,
		Inputs:  []Component{{Type: "textbox", Key: "query", Label: "Consulta"}},
		Outputs: []Component{{Type: "markdown", Key: "stream_out", Label: "Progreso"}, {Type: "textbox", Key: "instant_out", Label: "Resultado"}},
	}, func(context.Context, []any) ([]any, error) { return []any{"inicial", "inicial"}, nil }, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = app.OnStream(EventSpec{Target: "progress", Trigger: "click", Inputs: []string{"query"}, Outputs: []string{"stream_out"}},
		func(ctx context.Context, inputs []any, emit func([]any) error) error {
			for i := 1; i <= 3; i++ {
				if err := emit([]any{fmt.Sprintf("Paso %d: %s", i, inputs[0])}); err != nil {
					return err
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(14 * time.Millisecond):
				}
			}
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	err = app.On(EventSpec{Target: "instant", Trigger: "click", Inputs: []string{"query"}, Outputs: []string{"instant_out"}},
		func(_ context.Context, values []any) ([]any, error) { return []any{"OK " + values[0].(string)}, nil })
	if err != nil {
		t.Fatal(err)
	}
	cfg := app.gradioConfig()
	deps := cfg["dependencies"].([]any)
	if len(deps) != 4 {
		t.Fatalf("expected submit/clear/stream/click: %d", len(deps))
	}
	streamDep := deps[2].(map[string]any)
	if streamDep["queue"] != true || streamDep["types"].(map[string]bool)["generator"] != true {
		t.Fatalf("stream dep not generator: %#v", streamDep)
	}
	normalDep := deps[3].(map[string]any)
	if normalDep["queue"] != false || normalDep["types"].(map[string]bool)["generator"] != false {
		t.Fatalf("normal dep changed: %#v", normalDep)
	}
	if cfg["enable_queue"] != true {
		t.Fatal("no Gradio queue for standalone stream event")
	}
	normal := httptest.NewRecorder()
	app.Routes().ServeHTTP(normal, httptest.NewRequest("POST", "/gradio_api/run/predict", strings.NewReader("{\"fn_index\":3,\"data\":[\"hola\"]}")))
	if normal.Code != 200 || !strings.Contains(normal.Body.String(), "OK hola") {
		t.Fatalf("normal callback %d %s", normal.Code, normal.Body.String())
	}
	server := httptest.NewServer(app.Routes())
	defer server.Close()
	session := "abcdefgh12345678"
	payload := []byte("{\"data\":[\"hola\"],\"fn_index\":2,\"session_hash\":\"abcdefgh12345678\"}")
	join, err := http.Post(server.URL+"/gradio_api/queue/join", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer join.Body.Close()
	if join.StatusCode != 200 {
		t.Fatalf("queue join: %d", join.StatusCode)
	}
	var started map[string]any
	if err := json.NewDecoder(join.Body).Decode(&started); err != nil || started["event_id"] == nil {
		t.Fatalf("event ID %v %v", err, started)
	}
	eventID := started["event_id"].(string)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", server.URL+"/gradio_api/queue/data?session_hash="+session, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	generated := 0
	completed := false
	for scanner.Scan() {
		str := scanner.Text()
		if !strings.HasPrefix(str, "data: ") {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(str, "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		if event["event_id"] != eventID {
			t.Fatalf("foreign event: %#v", event)
		}
		switch event["msg"] {
		case "process_generating":
			generated++
			output := event["output"].(map[string]any)["data"].([]any)
			if len(output) != 1 {
				t.Fatalf("wrong outputs: %#v", output)
			}
			if generated == 1 && output[0] != "Paso 1: hola" {
				t.Fatalf("wrong first snapshot: %#v", output)
			}
			if generated == 2 {
				changes := output[0].([]any)
				if len(changes) != 1 || changes[0].([]any)[0] != "replace" {
					t.Fatalf("missing SSE v3 patches %#v", output)
				}
			}
		case "process_completed":
			completed = true
			last := event["output"].(map[string]any)["data"].([]any)
			if len(last) != 1 || last[0] != "Paso 3: hola" {
				t.Fatalf("wrong last event output %#v", last)
			}
		}
		if completed {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if !completed || generated != 3 {
		t.Fatalf("stream incomplete: generating %d completed %v", generated, completed)
	}
}
func TestBlocksRejectsInvalidStreamRegistration(t *testing.T) {
	app, err := New(testBlocksConfig(), func(context.Context, []any) ([]any, error) { return []any{"a", nil}, nil }, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.OnStream(EventSpec{Target: "analyze", Trigger: "change", Outputs: []string{"result"}}, func(context.Context, []any, func([]any) error) error { return nil }); err == nil {
		t.Fatal("allowed stream on invalid button trigger")
	}
	if err := app.OnStream(EventSpec{Target: "analyze", Trigger: "click", Outputs: []string{"result"}}, nil); err == nil {
		t.Fatal("allowed nil stream")
	}
	if err := app.BindStreamEvent(12, func(context.Context, []any, func([]any) error) error { return nil }); err == nil {
		t.Fatal("allowed unknown stream index")
	}
}

func TestBlocksStreamRejectsBadOutputAndCancellation(t *testing.T) {
	layout := Row(Ref("query"), Button("stream", "Stream"), Ref("result"))
	app, err := New(Config{Layout: &layout, Inputs: []Component{{Type: "textbox", Key: "query"}}, Outputs: []Component{{Type: "markdown", Key: "result"}}},
		func(context.Context, []any) ([]any, error) { return []any{"idle"}, nil }, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = app.OnStream(EventSpec{Target: "stream", Trigger: "click", Inputs: []string{"query"}, Outputs: []string{"result"}},
		func(ctx context.Context, v []any, emit func([]any) error) error {
			if v[0] == "cancel" {
				<-ctx.Done()
				return ctx.Err()
			}
			return emit([]any{"wrong", "extra"})
		})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRecorder()
	app.Routes().ServeHTTP(req, httptest.NewRequest("POST", "/gradio_api/call/predict", strings.NewReader("{\"fn_index\":2,\"data\":[\"x\"]}")))
	if req.Code != 200 {
		t.Fatalf("call: %d %s", req.Code, req.Body.String())
	}
	var response map[string]string
	if err := json.Unmarshal(req.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	eventID := response["event_id"]
	poll := httptest.NewRecorder()
	app.Routes().ServeHTTP(poll, httptest.NewRequest("GET", "/gradio_api/call/predict/"+eventID, nil))
	if !strings.Contains(poll.Body.String(), "event: error") {
		t.Fatalf("invalid stream output not rejected: %s", poll.Body.String())
	}
	// Cancel only the event belonging to the supplied browser session.
	call := httptest.NewRecorder()
	app.Routes().ServeHTTP(call, httptest.NewRequest("POST", "/gradio_api/call/predict", strings.NewReader("{\"fn_index\":2,\"data\":[\"cancel\"],\"session_hash\":\"abcddcba12345678\"}")))
	if call.Code != 200 {
		t.Fatalf("stream start: %d", call.Code)
	}
	if err := json.Unmarshal(call.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	id := response["event_id"]
	wrong := httptest.NewRecorder()
	app.Routes().ServeHTTP(wrong, httptest.NewRequest("POST", "/gradio_api/queue/cancel", strings.NewReader(fmt.Sprintf("{\"event_id\":%q,\"session_hash\":\"other12345678\"}", id))))
	if wrong.Code != 404 {
		t.Fatalf("cross-session cancel allowed: %d", wrong.Code)
	}
	right := httptest.NewRecorder()
	app.Routes().ServeHTTP(right, httptest.NewRequest("POST", "/gradio_api/queue/cancel", strings.NewReader(fmt.Sprintf("{\"event_id\":%q,\"session_hash\":\"abcddcba12345678\"}", id))))
	if right.Code != 200 {
		t.Fatalf("legitimate cancel failed: %d %s", right.Code, right.Body.String())
	}
	poll = httptest.NewRecorder()
	app.Routes().ServeHTTP(poll, httptest.NewRequest("GET", "/gradio_api/call/predict/"+id, nil))
	if !strings.Contains(poll.Body.String(), "event: error") {
		t.Fatalf("cancel not surfaced: %s", poll.Body.String())
	}
}
