// Chat streaming in the ORIGINAL Gradio 6.30.0 Chatbot component.
// This demo uses a scripted response: it does not connect to a real AI model.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"goradio"
)

func main() {
	port := flag.Int("port", 7987, "Puerto local")
	flag.Parse()
	var app *goradio.App
	min, max := 1, 2000
	cfg := goradio.Config{
		Title:       "Goradio · Chat streaming",
		Description: "Componente Chatbot original · Respuestas simuladas por streaming Go",
		Inputs:      []goradio.Component{{Type: "textbox", Label: "Mensaje", Value: "Hola", MinLength: &min, MaxLength: &max}},
		Outputs:     []goradio.Component{{Type: "chatbot", Label: "Conversación"}},
	}
	var err error
	app, err = goradio.New(cfg, func(_ context.Context, _ []any) ([]any, error) {
		return []any{[]any{}}, nil
	}, "")
	if err != nil {
		log.Fatal(err)
	}
	app.SetStreamHandler(func(ctx context.Context, inputs []any, emit func([]any) error) error {
		msg := strings.TrimSpace(inputs[0].(string))
		var history []map[string]any
		app.StateGet(ctx, "chat.history", &history)
		history = append(history, map[string]any{"role": "user", "content": msg})
		reply := "Esta respuesta es una demostración de streaming con el componente original Chatbot de Gradio. Para usar una IA real, conecta tu modelo al StreamHandler."
		words := strings.Fields(reply)
		for i := range words {
			partial := append([]map[string]any(nil), history...)
			partial = append(partial, map[string]any{"role": "assistant", "content": strings.Join(words[:i+1], " ")})
			if err := emit([]any{partial}); err != nil {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(22 * time.Millisecond):
			}
		}
		history = append(history, map[string]any{"role": "assistant", "content": reply})
		if err := app.StateSet(ctx, "chat.history", history); err != nil {
			return fmt.Errorf("guardar conversación: %w", err)
		}
		return nil
	})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("Chat real Gradio en http://127.0.0.1:%d", *port)
	if err := app.Serve(ctx, "127.0.0.1", *port, nil); err != nil {
		log.Fatal(err)
	}
}
