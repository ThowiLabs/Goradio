package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"goradio"
)

func main() {
	d, err := goradio.NewDashboard("Mi panel Go")
	if err != nil {
		log.Fatal(err)
	}
	err = d.Add(goradio.Window{
		Path: "/", Title: "Inicio", Icon: "dashboard", Group: "Principal",
		Config:  goradio.Config{Title: "Inicio Go", Inputs: []goradio.Component{{Type: "textbox", Label: "Nombre"}}, Outputs: []goradio.Component{{Type: "textbox", Label: "Respuesta"}}},
		Handler: func(_ context.Context, v []any) ([]any, error) { return []any{"Hola desde Go, " + v[0].(string)}, nil },
	})
	if err != nil {
		log.Fatal(err)
	}
	err = d.Add(goradio.Window{
		Path: "/configuracion", Title: "Configuración", Icon: "settings", Group: "Sistema",
		Config:  goradio.Config{Title: "Configuración de Go", Inputs: []goradio.Component{{Type: "textbox", Label: "Comando"}}, Outputs: []goradio.Component{{Type: "textbox", Label: "Salida"}}},
		Handler: func(_ context.Context, v []any) ([]any, error) { return []any{"Recibido: " + v[0].(string)}, nil },
	})
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := d.Serve(ctx, "127.0.0.1", 7933, []string{}); err != nil {
		log.Fatal(err)
	}
}
