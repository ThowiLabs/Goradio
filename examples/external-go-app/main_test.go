package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOtherGoModuleCanUseGoradio(t *testing.T) {
	app, err := newDemo(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/predict", strings.NewReader(`{"data":["Ana",7]}`))
	request = request.WithContext(context.Background())
	response := httptest.NewRecorder()
	app.Routes().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("Status %d: %s", response.Code, response.Body.String())
	}
	var result struct {
		Data []any `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Data) != 2 || result.Data[0] != "¡Hola, Ana!" || result.Data[1] != float64(14) {
		t.Fatalf("Resultado: %#v", result.Data)
	}
}
