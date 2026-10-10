package goradio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGradioFrontend_OfficialAssetsAndContract(t *testing.T) {
	app := testApp(t, []Component{{Type: "textbox", Label: "Input"}}, []Component{{Type: "textbox", Label: "Output"}}, BuiltinHandler("uppercase"))
	html := httptest.NewRecorder()
	app.Routes().ServeHTTP(html, httptest.NewRequest("GET", "/", nil))
	if html.Code != 200 || !strings.Contains(html.Body.String(), "/assets/index-D88pgWqG.js") || !strings.Contains(html.Body.String(), "<gradio-app") {
		t.Fatalf("frontend no disponible %d", html.Code)
	}
	if !strings.Contains(html.Body.String(), `.gradio-container > .main:has(button) > [data-testid="status-tracker"]:has(p.loading)`) {
		t.Fatal("falta la protección para el indicador global de carga persistente")
	}
	css := httptest.NewRecorder()
	app.Routes().ServeHTTP(css, httptest.NewRequest("GET", "/assets/index-DuOipanW.css", nil))
	if css.Code != 200 || len(css.Body.Bytes()) < 500 {
		t.Fatal("CSS original no servido")
	}
	config := app.gradioConfig()
	components := config["components"].([]any)
	if len(components) < 7 {
		t.Fatalf("componentes: %d", len(components))
	}
	page := config["page"].(map[string]any)[""].(map[string]any)
	ids := page["components"].([]int)
	if len(ids) != len(components) {
		t.Fatalf("página incompleta: %d != %d", len(ids), len(components))
	}
}
func TestGradioAPI_RunPredict(t *testing.T) {
	app := testApp(t, []Component{{Type: "textbox"}}, []Component{{Type: "textbox"}}, BuiltinHandler("uppercase"))
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, httptest.NewRequest("POST", "/gradio_api/run/predict", strings.NewReader(`{"data":["hola"]}`)))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "HOLA") {
		t.Fatalf("predicción %d: %s", rec.Code, rec.Body.String())
	}
}
func TestGradioAPI_CallEvents(t *testing.T) {
	app := testApp(t, []Component{{Type: "textbox"}}, []Component{{Type: "textbox"}}, BuiltinHandler("uppercase"))
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, httptest.NewRequest("POST", "/gradio_api/call/predict", strings.NewReader(`{"data":["goradio"]}`)))
	if rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	var result map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	id := result["event_id"]
	if len(id) != 32 {
		t.Fatalf("event id: %q", id)
	}
	follow := httptest.NewRecorder()
	app.Routes().ServeHTTP(follow, httptest.NewRequest("GET", "/gradio_api/call/predict/"+id, nil))
	if follow.Code != 200 || !strings.Contains(follow.Body.String(), "GORADIO") || !strings.Contains(follow.Body.String(), "event: complete") {
		t.Fatalf("SSE: %s", follow.Body.String())
	}
}
func TestGradioAPI_UploadPreviewRoundtrip(t *testing.T) {
	app := testApp(t, []Component{{Type: "image"}}, []Component{{Type: "image"}}, func(_ context.Context, x []any) ([]any, error) { return x, nil })
	png := []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 0, 73, 72, 68, 82}
	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)
	f, err := multipartWriter.CreateFormFile("files", "sample.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(png); err != nil {
		t.Fatal(err)
	}
	if err := multipartWriter.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/gradio_api/upload", &body)
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, request)
	if rec.Code != 200 {
		t.Fatalf("upload %d %s", rec.Code, rec.Body.String())
	}
	var names []string
	if err := json.Unmarshal(rec.Body.Bytes(), &names); err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 {
		t.Fatal(names)
	}
	preview := httptest.NewRecorder()
	app.Routes().ServeHTTP(preview, httptest.NewRequest("GET", "/gradio_api/file="+names[0], nil))
	if preview.Code != 200 {
		t.Fatalf("preview %d %s", preview.Code, preview.Body.String())
	}
	file, err := io.ReadAll(preview.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(file, png) {
		t.Fatal("archivo cambiado")
	}
	predict := httptest.NewRecorder()
	obj := map[string]any{"data": []any{map[string]any{"path": names[0], "meta": map[string]any{"_type": "gradio.FileData"}}}}
	data, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	app.Routes().ServeHTTP(predict, httptest.NewRequest("POST", "/gradio_api/run/predict", bytes.NewReader(data)))
	if predict.Code != 200 || !strings.Contains(predict.Body.String(), "/gradio_api/file=") {
		t.Fatalf("predict %d %s", predict.Code, predict.Body.String())
	}
}
func TestGradioAPI_RejectsTraversal(t *testing.T) {
	app := testApp(t, []Component{{Type: "file"}}, []Component{{Type: "textbox"}}, BuiltinHandler("echo"))
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/gradio_api/run/predict", strings.NewReader(`{"data":[{"path":"../../Windows/win.ini"}]}`)))
	if rec.Code != 400 {
		t.Fatalf("traversal accepted: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGradioAPI_ClearResetsInputsAndOutputs(t *testing.T) {
	app := testApp(t, []Component{{Type: "textbox"}, {Type: "slider"}}, []Component{{Type: "textbox"}}, BuiltinHandler("echo"))
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, httptest.NewRequest("POST", "/gradio_api/run/clear", strings.NewReader(`{"data":[]}`)))
	if rec.Code != 200 {
		t.Fatalf("clear HTTP %d", rec.Code)
	}
	var response struct {
		Data []any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data) != 3 || response.Data[0] != "" || response.Data[2] != "" {
		t.Fatalf("clear values: %#v", response.Data)
	}
}
func TestGradioAPI_UploadRejectsHTMLPayload(t *testing.T) {
	app := testApp(t, nil, []Component{{Type: "file"}}, BuiltinHandler("echo"))
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	p, err := writer.CreateFormFile("files", "script.html")
	if err != nil {
		t.Fatal(err)
	}
	p.Write([]byte("<script>alert(1)</script>"))
	writer.Close()
	req := httptest.NewRequest("POST", "/gradio_api/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("HTML aceptado: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGradioAPI_ResetByFnIndex(t *testing.T) {
	app := testApp(t, []Component{{Type: "textbox"}}, []Component{{Type: "textbox"}}, BuiltinHandler("uppercase"))
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, httptest.NewRequest("POST", "/gradio_api/run/predict", strings.NewReader(`{"fn_index":1,"data":[]}`)))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"data":["",""]`) {
		t.Fatalf("clear fn_index result %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGradioUploadProgressSSE_ReportsTransferredBytes(t *testing.T) {
	app := testApp(t, nil, []Component{{Type: "file"}}, BuiltinHandler("echo"))
	contents := []byte("contenido de prueba de archivo")
	var body bytes.Buffer
	m := multipart.NewWriter(&body)
	file, err := m.CreateFormFile("files", "archivo.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/gradio_api/upload?upload_id=testfile123", &body)
	req.Header.Set("Content-Type", m.FormDataContentType())
	recorder := httptest.NewRecorder()
	app.Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("upload: %d: %s", recorder.Code, recorder.Body.String())
	}
	progress := httptest.NewRecorder()
	app.Routes().ServeHTTP(progress, httptest.NewRequest(http.MethodGet, "/gradio_api/upload_progress?upload_id=testfile123", nil))
	if progress.Code != http.StatusOK {
		t.Fatalf("SSE status: %d", progress.Code)
	}
	events := progress.Body.String()
	if !strings.Contains(events, "data: {\"msg\":\"done\"}\n\n") {
		t.Fatalf("No se recibió done SSE: %q", events)
	}
	if !strings.Contains(events, "\"orig_name\":\"archivo.txt\"") {
		t.Fatalf("No se recibió nombre de archivo: %q", events)
	}
	if !strings.Contains(events, fmt.Sprintf("\"chunk_size\":%d", len(contents))) {
		t.Fatalf("Progreso incorrecto: %q", events)
	}
	if strings.Contains(events, `\n`) {
		t.Fatalf("El SSE contiene escapes literales de nueva línea: %q", events)
	}
}
func TestGradioUploadProgressSSE_InvalidID(t *testing.T) {
	app := testApp(t, nil, []Component{{Type: "file"}}, BuiltinHandler("echo"))
	recorder := httptest.NewRecorder()
	app.Routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/gradio_api/upload_progress?upload_id=../../escape", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("ID inválido aceptado: %d", recorder.Code)
	}
}

func TestGradioVideoData_CompatConFrontend(t *testing.T) {
	app := testApp(t, []Component{{Type: "video"}}, []Component{{Type: "video"}}, func(_ context.Context, x []any) ([]any, error) { return x, nil })
	content := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 0, 0}
	var multipartBody bytes.Buffer
	writer := multipart.NewWriter(&multipartBody)
	f, err := writer.CreateFormFile("files", "sample.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(content); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	upload := httptest.NewRequest("POST", "/gradio_api/upload", &multipartBody)
	upload.Header.Set("Content-Type", writer.FormDataContentType())
	uploaded := httptest.NewRecorder()
	app.Routes().ServeHTTP(uploaded, upload)
	if uploaded.Code != 200 {
		t.Fatalf("upload: %d %s", uploaded.Code, uploaded.Body.String())
	}
	var ids []string
	if err = json.Unmarshal(uploaded.Body.Bytes(), &ids); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 {
		t.Fatal(ids)
	}
	request := map[string]any{"data": []any{map[string]any{"video": map[string]any{"path": ids[0]}, "subtitles": nil}}}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, httptest.NewRequest("POST", "/gradio_api/run/predict", bytes.NewReader(raw)))
	if rec.Code != 200 {
		t.Fatalf("video predict: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data []map[string]any `json:"data"`
	}
	if err = json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 {
		t.Fatal(body.Data)
	}
	video, ok := body.Data[0]["video"].(map[string]any)
	if !ok || video["path"] != ids[0] || body.Data[0]["subtitles"] != nil {
		t.Fatalf("Gradio.VideoData incorrecto: %#v", body.Data[0])
	}
}
func TestGradioUploadProgress_StartsBeforeUpload(t *testing.T) {
	app := testApp(t, nil, []Component{{Type: "file"}}, BuiltinHandler("echo"))
	server := httptest.NewServer(app.Routes())
	defer server.Close()
	client := &http.Client{Timeout: 4 * time.Second}
	result := make(chan string, 1)
	go func() {
		resp, err := client.Get(server.URL + "/gradio_api/upload_progress?upload_id=early123")
		if err != nil {
			result <- "error: " + err.Error()
			return
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if err != nil {
			result <- "error: " + err.Error()
			return
		}
		result <- string(raw)
	}()
	time.Sleep(70 * time.Millisecond)
	var form bytes.Buffer
	m := multipart.NewWriter(&form)
	part, err := m.CreateFormFile("files", "first.txt")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("1234567890"))
	m.Close()
	req, err := http.NewRequest(http.MethodPost, server.URL+"/gradio_api/upload?upload_id=early123", &form)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", m.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	select {
	case body := <-result:
		if !strings.Contains(body, "\"chunk_size\":10") || !strings.Contains(body, "\"msg\":\"done\"") {
			t.Fatalf("SSE temprano incorrecto: %q", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Se bloqueó el SSE de progreso")
	}
}

func TestGradioAudioWaveformOptions_UseOfficialPlayer(t *testing.T) {
	for _, input := range []bool{true, false} {
		props := gradioComponentProps(Component{Type: "audio", Label: "Muestra"}, input)
		waveform, ok := props["waveform_options"].(map[string]any)
		if !ok {
			t.Fatal("waveform_options ausente")
		}
		if waveform["show_recording_waveform"] != true {
			t.Fatalf("audio input=%v debe mostrar reproductor de ondas original", input)
		}
		if props["streaming"] != false {
			t.Fatalf("input=%v usa streaming inesperado", input)
		}
		if props["editable"] != input {
			t.Fatalf("input=%v editable incorrecto", input)
		}
	}
}
