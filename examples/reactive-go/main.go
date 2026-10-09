// Command reactive-go showcases the bundled Gradio 6.30.0 UI using
// real Go-backed timer updates, live inputs and progressive SSE outputs.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"goradio"
)

func main() {
	port := flag.Int("port", 7996, "Puerto de vista previa")
	flag.Parse()
	min, max := 3, 60
	app, err := goradio.New(goradio.Config{
		Title:       "Goradio · Reactividad nativa",
		Description: "Los componentes de esta pantalla son los originales de Gradio 6.30. Cambia el texto o espera el temporizador.",
		Live:        true, EverySeconds: 5,
		Inputs: []goradio.Component{
			{Type: "textbox", Label: "Consulta", Value: "monitoreo de estado", MinLength: &min, MaxLength: &max},
		},
		Outputs: []goradio.Component{
			{Type: "markdown", Label: "Estado", Value: "## Esperando actualización..."},
			{Type: "dataframe", Label: "Tabla de estado", Headers: []string{"Proceso", "Estado", "Avance"}},
		},
	}, func(_ context.Context, v []any) ([]any, error) {
		return []any{fmt.Sprintf("**%s**", v[0]), map[string]any{"headers": []string{"Proceso", "Estado", "Avance"}, "data": [][]any{{"Gradio", "activo", 100}}, "metadata": nil}}, nil
	}, "")
	if err != nil {
		log.Fatal(err)
	}
	app.SetStreamHandler(func(ctx context.Context, data []any, emit func([]any) error) error {
		for i := 1; i <= 3; i++ {
			partial := []any{
				fmt.Sprintf("### %s\n\n**Estado: %d/3** · %s", data[0], i, []string{"Iniciando", "Procesando", "Listo"}[i-1]),
				map[string]any{"headers": []string{"Proceso", "Estado", "Avance"}, "data": [][]any{{"Monitor", []string{"inicio", "trabajando", "completado"}[i-1], i * 33}}, "metadata": nil},
			}
			if err := emit(partial); err != nil {
				return err
			}
			if i < 3 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(180 * time.Millisecond):
				}
			}
		}
		return nil
	})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("Gradio reactive demo: http://127.0.0.1:%d", *port)
	if err := app.Serve(ctx, "127.0.0.1", *port, nil); err != nil {
		log.Fatal(err)
	}
}
