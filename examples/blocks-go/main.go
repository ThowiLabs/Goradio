package main

import (
	"context"
	"flag"
	"fmt"
	"goradio"
	"log"
	"os"
	"os/signal"
)

func main() {
	port := flag.Int("port", 7994, "port")
	flag.Parse()
	layout := goradio.Column(
		goradio.Tabs(
			goradio.Tab("Datos", goradio.Row(goradio.Column(goradio.Ref("query"), goradio.Ref("format"), goradio.Button("summarize", "Resumir")), goradio.Column(goradio.Ref("result")))),
			goradio.Tab("Tabla", goradio.Group(goradio.Ref("table"))),
			goradio.Tab("Opciones", goradio.Accordion("Preferencias", true, goradio.Ref("active"))),
		),
	)
	app, err := goradio.New(goradio.Config{
		Title:       "Goradio Blocks · original Gradio 6.30",
		Description: "Pestañas y controles originales; no es una maqueta HTML.",
		Layout:      &layout, Live: true,
		Inputs: []goradio.Component{
			{Type: "textbox", Key: "query", Label: "Consulta", Value: "Hola"},
			{Type: "dropdown", Key: "format", Label: "Formato", Choices: []string{"Breve", "Detallado"}, Value: "Breve"},
			{Type: "checkbox", Key: "active", Label: "Activar procesamiento", Value: true},
		},
		Outputs: []goradio.Component{
			{Type: "markdown", Key: "result", Label: "Resultado", Value: "*Sin ejecutar*"},
			{Type: "dataframe", Key: "table", Label: "Datos", Headers: []string{"Campo", "Valor"}},
		},
	}, func(_ context.Context, v []any) ([]any, error) {
		return []any{fmt.Sprintf("### %v · %v\n\nActivado: %v", v[0], v[1], v[2]), map[string]any{"headers": []string{"Campo", "Valor"}, "data": [][]any{{"Consulta", v[0]}, {"Formato", v[1]}, {"Activado", fmt.Sprint(v[2])}}, "metadata": nil}}, nil
	}, "")
	if err != nil {
		log.Fatal(err)
	}
	if err := app.On(goradio.EventSpec{
		Target: "summarize", Trigger: "click",
		Inputs: []string{"query"}, Outputs: []string{"result"},
	}, func(_ context.Context, values []any) ([]any, error) {
		return []any{fmt.Sprintf("### Resumen de: %s\\n\\nActualización **solo del resultado**, sin modificar la tabla.", values[0])}, nil
	}); err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := app.Serve(ctx, "127.0.0.1", *port, nil); err != nil {
		log.Fatal(err)
	}
}
