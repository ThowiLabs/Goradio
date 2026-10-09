package goradio

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGradio6ChatbotTypedMessageContract(t *testing.T) {
	input := []any{
		map[string]any{"role": "user", "content": "Hola"},
		map[string]any{"role": "assistant", "content": "Respuesta"},
	}
	result, err := normalizeGradioChatMessages(input)
	if err != nil {
		t.Fatal(err)
	}
	msg := result[1].(map[string]any)
	parts := msg["content"].([]any)
	part := parts[0].(map[string]any)
	if part["type"] != "text" || part["text"] != "Respuesta" {
		t.Fatalf("Incorrect Gradio 6 message: %#v", part)
	}
	props := gradioComponentProps(Component{Type: "chatbot", Value: input}, false)
	if _, ok := props["value"].([]any); !ok {
		t.Fatal("Chatbot initial value not normalized")
	}
	for _, invalid := range []any{
		"not a list",
		[]any{map[string]any{"role": "assistant", "content": 42}},
		[]any{map[string]any{"role": "other", "content": "hello"}},
	} {
		if _, err := normalizeGradioChatMessages(invalid); err == nil {
			t.Fatalf("Expected invalid chat message rejection: %#v", invalid)
		}
	}
}
func TestGradio6GalleryHasSourcesAndFileTypes(t *testing.T) {
	props := gradioComponentProps(Component{Type: "gallery"}, false)
	sources, ok := props["sources"].([]string)
	if !ok || len(sources) == 0 || sources[0] != "upload" {
		t.Fatalf("Gallery sources missing: %#v", props)
	}
	if files, ok := props["file_types"].([]string); !ok || len(files) == 0 {
		t.Fatal("Gallery file types missing")
	}
}
func TestGradio6ChatbotAndGalleryResults(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "1234567890abcdef1234567890abcdef.png")
	if err := os.WriteFile(file, []byte("fake asset"), 0600); err != nil {
		t.Fatal(err)
	}
	config := Config{Title: "Components", Inputs: []Component{{Type: "textbox", Label: "Prompt"}}, Outputs: []Component{{Type: "chatbot", Label: "Chat"}, {Type: "gallery", Label: "Images"}}}
	app, err := New(config, func(_ context.Context, _ []any) ([]any, error) {
		return []any{[]any{map[string]any{"role": "assistant", "content": "Hola desde Go"}}, []string{file}}, nil
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	result, err := app.executeGradio(context.Background(), []any{"x"})
	if err != nil {
		t.Fatal(err)
	}
	msg := result[0].([]any)[0].(map[string]any)
	if msg["content"].([]any)[0].(map[string]any)["text"] != "Hola desde Go" {
		t.Fatal("Missing typed message")
	}
	gallery := result[1].([]any)[0].(map[string]any)
	image := gallery["image"].(map[string]any)
	if !strings.HasPrefix(image["url"].(string), "/gradio_api/file=") {
		t.Fatalf("Missing Gallery FileData: %#v", image)
	}
	_, err = json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
}
