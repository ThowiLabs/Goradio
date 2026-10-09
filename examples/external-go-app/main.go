package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"goradio"
)

func newDemo(mediaDir string) (*goradio.App, error) {
	return goradio.New(
		goradio.Config{
			Title:       "Goradio desde otro módulo Go",
			Description: "Esta aplicación usa Goradio como biblioteca externa, sin copiar su código.",
			Inputs: []goradio.Component{
				{Type: "textbox", Label: "Tu nombre", Required: true},
				{Type: "number", Label: "Un número"},
			},
			Outputs: []goradio.Component{
				{Type: "textbox", Label: "Saludo"},
				{Type: "number", Label: "Doble"},
			},
		},
		func(ctx context.Context, values []any) ([]any, error) {
			nombre := values[0].(string)
			numero, ok := values[1].(float64)
			if !ok {
				numero = 0
			}
			return []any{fmt.Sprintf("¡Hola, %s!", nombre), numero * 2}, nil
		},
		mediaDir,
	)
}

func main() {
	app, err := newDemo("")
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := app.Serve(ctx, "127.0.0.1", 7899, nil); err != nil {
		log.Fatal(err)
	}
}
