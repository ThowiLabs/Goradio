package goradio

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeRadioAndCheckboxGroup(t *testing.T) {
	app, err := New(Config{
		Inputs: []Component{
			{Type: "radio", Label: "Color", Choices: []string{"Rojo", "Azul"}, Value: "Azul"},
			{Type: "checkboxgroup", Label: "Idiomas", Choices: []string{"Go", "Python", "Rust"}, Required: true},
		},
		Outputs: []Component{{Type: "textbox", Label: "Resumen"}},
	}, func(_ context.Context, v []any) ([]any, error) {
		return []any{v[0].(string) + ": " + v[1].([]any)[0].(string)}, nil
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := app.gradioConfig()
	found := map[string]bool{}
	for _, raw := range cfg["components"].([]any) {
		c := raw.(map[string]any)
		kind := c["type"].(string)
		if kind == "radio" || kind == "checkboxgroup" {
			props := c["props"].(map[string]any)
			choices, ok := props["choices"].([]any)
			if !ok || len(choices) < 2 {
				t.Fatalf("missing original Gradio options: %#v", props)
			}
			if props["interactive"] != true {
				t.Fatalf("input must be interactive: %#v", props)
			}
			found[kind] = true
		}
	}
	if !found["radio"] || !found["checkboxgroup"] {
		t.Fatalf("missing input widgets: %#v", found)
	}
	for _, tc := range []struct {
		data   string
		status int
	}{
		{`{"data":["Rojo",["Go","Python"]]}`, 200},
		{`{"data":["Verde",["Go"]]}`, 400},
		{`{"data":["Rojo",["Go","Go"]]}`, 400},
		{`{"data":["Rojo",["C++"]]}`, 400},
		{`{"data":["Rojo",[]]}`, 400},
	} {
		req := httptest.NewRequest("POST", "/gradio_api/run/predict", strings.NewReader(tc.data))
		res := httptest.NewRecorder()
		app.Routes().ServeHTTP(res, req)
		if res.Code != tc.status {
			t.Errorf("payload %s got %d not %d: %s", tc.data, res.Code, tc.status, res.Body.String())
		}
	}
}
