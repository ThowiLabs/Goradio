package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"goradio"
)

func main() {
	app, err := goradio.New(goradio.Config{
		Title:       "Goradio · Go nativo",
		Description: "Crea interfaces declarativas con funciones Go.",
		Inputs:      []goradio.Component{{Type: "textbox", Label: "Nombre", Required: true}},
		Outputs:     []goradio.Component{{Type: "textbox", Label: "Saludo"}},
	}, func(_ context.Context, inputs []any) ([]any, error) {
		return []any{"¡Hola, " + inputs[0].(string) + "! Esta función corre en Go."}, nil
	}, "")
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := app.Serve(ctx, "127.0.0.1", 7860, []string{}); err != nil {
		log.Fatal(err)
	}
}
