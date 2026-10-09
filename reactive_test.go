package goradio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func makeReactiveApp(t *testing.T) *App {
	t.Helper()
	app, err := New(Config{
		Title: "Reactive", Live: true, EverySeconds: 1,
		Inputs:  []Component{{Type: "textbox", Label: "Text"}},
		Outputs: []Component{{Type: "textbox", Label: "Output"}},
	}, func(_ context.Context, args []any) ([]any, error) { return []any{args[0]}, nil }, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestReactiveGradioDependencies(t *testing.T) {
	a := makeReactiveApp(t)
	cfg := a.gradioConfig()
	deps := cfg["dependencies"].([]any)
	if len(deps) != 5 {
		t.Fatalf("expected submit, clear, change, input, timer, got %d", len(deps))
	}
	for i := 2; i < 5; i++ {
		dep := deps[i].(map[string]any)
		if dep["id"] != i {
			t.Errorf("id %v does not match index %d", dep["id"], i)
		}
		if reflect.DeepEqual(dep["outputs"], []int{}) {
			t.Error("reactive event has no outputs")
		}
	}
	components := cfg["components"].([]any)
	found := false
	for _, entry := range components {
		c := entry.(map[string]any)
		if c["type"] == "timer" {
			found = true
			props := c["props"].(map[string]any)
			if props["value"] != float64(1) || props["active"] != true {
				t.Fatal("Invalid timer props")
			}
		}
	}
	if !found {
		t.Fatal("No Gradio Timer component")
	}
	for i := 0; i < 5; i++ {
		saved, _, ok := a.classifyFn(i)
		if !ok || saved {
			t.Errorf("unexpected event classification %d", i)
		}
	}
	_, _, ok := a.classifyFn(5)
	if ok {
		t.Fatal("accepted unknown event")
	}
}
func TestReactiveFnIndexPredictions(t *testing.T) {
	a := makeReactiveApp(t)
	for _, index := range []int{0, 2, 3, 4} {
		req := httptest.NewRequest("POST", "/gradio_api/run/predict", strings.NewReader(fmt.Sprintf("{\"fn_index\":%d,\"data\":[\"hello\"]}", index)))
		rr := httptest.NewRecorder()
		a.Routes().ServeHTTP(rr, req)
		if rr.Code != 200 || !strings.Contains(rr.Body.String(), "hello") {
			t.Fatalf("fn_index %d returned %d: %s", index, rr.Code, rr.Body.String())
		}
	}
	req := httptest.NewRequest("POST", "/gradio_api/run/predict", strings.NewReader(`{"fn_index":5,"data":["bad"]}`))
	rr := httptest.NewRecorder()
	a.Routes().ServeHTTP(rr, req)
	if rr.Code != 404 {
		t.Fatalf("accepted unknown event: %d %s", rr.Code, rr.Body.String())
	}
}

func TestProgressiveStreamingEvents(t *testing.T) {
	a := makeReactiveApp(t)
	a.SetStreamHandler(func(ctx context.Context, args []any, emit func([]any) error) error {
		for _, value := range []string{"Preparando", "Procesando", "Completado"} {
			if err := emit([]any{value}); err != nil {
				return err
			}
		}
		return nil
	})
	deps := a.gradioConfig()["dependencies"].([]any)
	if deps[0].(map[string]any)["types"].(map[string]bool)["generator"] != true {
		t.Fatal("no generator flag")
	}
	r := httptest.NewRecorder()
	a.Routes().ServeHTTP(r, httptest.NewRequest("POST", "/gradio_api/call/predict", strings.NewReader(`{"data":["hello"],"fn_index":0}`)))
	if r.Code != 200 {
		t.Fatalf("call failed %d %s", r.Code, r.Body.String())
	}
	var started struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &started); err != nil || started.EventID == "" {
		t.Fatalf("no event ID: %v %s", err, r.Body.String())
	}
	poll := httptest.NewRecorder()
	a.Routes().ServeHTTP(poll, httptest.NewRequest("GET", "/gradio_api/call/predict/"+started.EventID, nil))
	if poll.Code != 200 {
		t.Fatalf("SSE code %d", poll.Code)
	}
	body := poll.Body.String()
	if strings.Count(body, "event: generating") != 3 || strings.Count(body, "event: complete") != 1 {
		t.Fatalf("stream lost frames: %s", body)
	}
	if !strings.Contains(body, "Preparando") || !strings.Contains(body, "Completado") {
		t.Fatalf("stream content missing: %s", body)
	}
	a.jobsMu.Lock()
	count := len(a.jobs)
	a.jobsMu.Unlock()
	if count != 0 {
		t.Fatal("completed job leaked")
	}
}

func TestReactiveAdvancedValidation(t *testing.T) {
	min, max := 2, 5
	a, err := New(Config{
		Inputs:  []Component{{Type: "textbox", Label: "Code", MinLength: &min, MaxLength: &max, Pattern: "^[a-z]+$"}},
		Outputs: []Component{{Type: "textbox"}},
	}, func(_ context.Context, data []any) ([]any, error) { return []any{data[0]}, nil }, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"a", "abcdef", "123"} {
		if _, err := a.predictValues(context.Background(), []any{input}); err == nil {
			t.Errorf("accepted invalid %q", input)
		}
	}
	if _, err := a.predictValues(context.Background(), []any{"abc"}); err != nil {
		t.Fatal(err)
	}
	tableApp, err := New(Config{
		Inputs:  []Component{{Type: "dataframe", Headers: []string{"Nombre", "Edad"}, Datatype: []string{"str", "number"}}},
		Outputs: []Component{{Type: "json"}},
	}, func(_ context.Context, values []any) ([]any, error) { return []any{values[0]}, nil }, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		v  any
		ok bool
	}{
		{map[string]any{"headers": []any{"Nombre", "Edad"}, "data": []any{[]any{"Juan", float64(32)}}}, true},
		{map[string]any{"headers": []any{"Nombre", "Edad"}, "data": []any{[]any{"Juan", "bad"}}}, false},
		{map[string]any{"headers": []any{"Wrong", "Edad"}, "data": []any{}}, false},
	} {
		_, err := tableApp.predictValues(context.Background(), []any{tc.v})
		if (err == nil) != tc.ok {
			t.Errorf("invalid dataframe result %v: %v", tc.v, err)
		}
	}
}
func TestReactiveConfigRejectsInvalidIntervals(t *testing.T) {
	for _, interval := range []float64{-1, .1, 3601} {
		_, err := New(Config{EverySeconds: interval, Outputs: []Component{{Type: "textbox"}}},
			func(context.Context, []any) ([]any, error) { return nil, errors.New("unused") }, t.TempDir())
		if err == nil {
			t.Errorf("accepted interval %v", interval)
		}
	}
}
