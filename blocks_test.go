package goradio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testBlocksConfig() Config {
	layout := Tabs(Tab("Datos", Row(Ref("query"), Button("analyze", "Analizar"))), Tab("Resultado", Column(Ref("result"), Ref("table"))))
	return Config{
		Title: "Blocks", Layout: &layout,
		Inputs:  []Component{{Type: "textbox", Key: "query", Label: "Consulta"}},
		Outputs: []Component{{Type: "markdown", Key: "result"}, {Type: "dataframe", Key: "table"}},
	}
}
func TestBlocksNativeContainersAndDirectedEvent(t *testing.T) {
	cfg := testBlocksConfig()
	app, err := New(cfg, func(_ context.Context, v []any) ([]any, error) {
		return []any{"full", map[string]any{"headers": []string{"Campo"}, "data": [][]any{{"A"}}}}, nil
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = app.On(EventSpec{Target: "analyze", Trigger: "click", Inputs: []string{"query"}, Outputs: []string{"result"}},
		func(_ context.Context, v []any) ([]any, error) { return []any{"target: " + v[0].(string)}, nil })
	if err != nil {
		t.Fatal(err)
	}
	frontend := app.gradioConfig()
	components := frontend["components"].([]any)
	seen := map[string]bool{}
	for _, item := range components {
		c := item.(map[string]any)
		seen[c["type"].(string)] = true
	}
	for _, kind := range []string{"tabs", "tabitem", "row", "column", "button", "textbox", "markdown", "dataframe"} {
		if !seen[kind] {
			t.Errorf("Blocks missing native Gradio component %s", kind)
		}
	}
	deps := frontend["dependencies"].([]any)
	if len(deps) != 3 {
		t.Fatalf("expected 3 dependencies: %d", len(deps))
	}
	dep := deps[2].(map[string]any)
	if len(dep["inputs"].([]int)) != 1 || len(dep["outputs"].([]int)) != 1 {
		t.Fatalf("not a selective event: %#v", dep)
	}
	if _, _, valid := app.classifyFn(2); !valid {
		t.Fatal("custom event not recognized")
	}
	req := httptest.NewRequest(http.MethodPost, "/gradio_api/run/predict", strings.NewReader(`{"fn_index":2,"data":["hola"]}`))
	res := httptest.NewRecorder()
	app.Routes().ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatalf("HTTP %d: %s", res.Code, res.Body.String())
	}
	var result struct {
		Data []any `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Data) != 1 || result.Data[0] != "target: hola" {
		t.Fatalf("wrong output selection: %#v", result)
	}
	for _, payload := range []string{
		`{"fn_index":2,"data":[]}`, `{"fn_index":2,"data":["ok","extra"]}`, `{"fn_index":99,"data":["ok"]}`,
	} {
		rr := httptest.NewRecorder()
		app.Routes().ServeHTTP(rr, httptest.NewRequest("POST", "/gradio_api/run/predict", strings.NewReader(payload)))
		if rr.Code < 400 {
			t.Errorf("unexpected HTTP %d for %s", rr.Code, payload)
		}
	}
}
func TestBlocksRejectsInvalidLayoutsAndEvents(t *testing.T) {
	badLayouts := []LayoutNode{
		Row(Ref("query"), Ref("query"), Ref("result"), Ref("table")),
		Row(Ref("query"), Ref("result")),
		Tabs(Ref("query")),
		Row(Tab("Fuera", Ref("query")), Ref("result"), Ref("table")),
		Row(Ref("query"), Button("query", "reused"), Ref("result"), Ref("table")),
		Row(Ref("query"), Ref("result"), Ref("table"), Button("x", ""), Button("x", "Duplicado")),
	}
	for _, layout := range badLayouts {
		cfg := testBlocksConfig()
		cfg.Layout = &layout
		_, err := New(cfg, func(context.Context, []any) ([]any, error) { return nil, nil }, t.TempDir())
		if err == nil {
			t.Errorf("accepted invalid layout: %#v", layout)
		}
	}
	app, err := New(testBlocksConfig(), func(context.Context, []any) ([]any, error) { return []any{"A", nil}, nil }, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	invalid := []EventSpec{
		{Target: "analyze", Trigger: "change", Outputs: []string{"result"}},
		{Target: "does_not_exist", Trigger: "click", Outputs: []string{"result"}},
		{Target: "analyze", Trigger: "click", Inputs: []string{"missing"}, Outputs: []string{"result"}},
		{Target: "analyze", Trigger: "click", Inputs: []string{"query"}, Outputs: []string{"missing"}},
		{Target: "analyze", Trigger: "click", Outputs: []string{"result", "result"}},
	}
	for _, spec := range invalid {
		if err := app.On(spec, func(context.Context, []any) ([]any, error) { return nil, nil }); err == nil {
			t.Errorf("accepted invalid event: %#v", spec)
		}
	}
	valid := EventSpec{Target: "analyze", Trigger: "click", Inputs: []string{"query"}, Outputs: []string{"result"}}
	handler := func(context.Context, []any) ([]any, error) { return []any{"ok"}, nil }
	if err := app.On(valid, handler); err != nil {
		t.Fatal(err)
	}
	if err := app.On(valid, handler); err == nil {
		t.Fatal("accepted duplicated event")
	}
}

func TestBlocksDataframeOptionsAndCodeEditor(t *testing.T) {
	show := false
	cfg := Config{
		Inputs: []Component{
			{Type: "code", Label: "Script", Language: "python", Lines: 12},
			{Type: "dataframe", Label: "Sheet", Headers: []string{"Nombre", "Edad"}, Datatype: []string{"str", "number"},
				RowCount: 4, ColumnCount: 2, FixedColumns: true, ShowSearch: &show},
		},
		Outputs: []Component{{Type: "code", Label: "Result", Language: "python"}},
	}
	app, err := New(cfg, func(_ context.Context, v []any) ([]any, error) { return []any{v[0]}, nil }, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	components := app.gradioConfig()["components"].([]any)
	foundCode, foundTable := false, false
	for _, raw := range components {
		c := raw.(map[string]any)
		props := c["props"].(map[string]any)
		switch c["type"] {
		case "code":
			if props["interactive"] == true {
				foundCode = true
				if props["language"] != "python" || props["lines"] != 12 {
					t.Fatalf("Invalid Code props: %#v", props)
				}
			}
		case "dataframe":
			foundTable = true
			if props["interactive"] != true || props["show_search"] != nil {
				t.Fatalf("Invalid table props: %#v", props)
			}
			rows := props["row_count"].([]any)
			cols := props["column_count"].([]any)
			if rows[0] != 4 || rows[1] != "dynamic" || cols[0] != 2 || cols[1] != "fixed" {
				t.Fatalf("Invalid rows/cols: %#v %#v", rows, cols)
			}
		}
	}
	if !foundCode || !foundTable {
		t.Fatalf("Components missing: code=%v table=%v", foundCode, foundTable)
	}
	_, err = app.predictValues(context.Background(), []any{"print('ok')", map[string]any{
		"headers": []any{"Nombre", "Edad"}, "data": []any{[]any{"Ana", float64(23)}}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.predictValues(context.Background(), []any{42, map[string]any{
		"headers": []any{"Nombre", "Edad"}, "data": []any{}}})
	if err == nil {
		t.Fatal("Code accepted non-string")
	}
}

func TestBlocksEventsRequireLayout(t *testing.T) {
	cfg := Config{Inputs: []Component{{Type: "textbox"}}, Outputs: []Component{{Type: "textbox"}},
		Events: []EventSpec{{Target: "button", Trigger: "click", Outputs: []string{"result"}}}}
	_, err := New(cfg, func(context.Context, []any) ([]any, error) { return []any{""}, nil }, t.TempDir())
	if err == nil {
		t.Fatal("Accepted event without Blocks layout")
	}
}
