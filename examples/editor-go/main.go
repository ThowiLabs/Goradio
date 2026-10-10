package main

import (
	"context"
	"flag"
	"goradio"
	"log"
	"os"
	"os/signal"
)

func main() {
	port := flag.Int("port", 7986, "port")
	flag.Parse()
	layout := goradio.Tabs(
		goradio.Tab("Editor de código", goradio.Row(goradio.Ref("source"), goradio.Ref("preview"))),
		goradio.Tab("Tabla editable", goradio.Column(goradio.Ref("sheet"), goradio.Ref("status"))),
	)
	app, err := goradio.New(goradio.Config{
		Title: "Gradio · componentes editables", Layout: &layout,
		Inputs: []goradio.Component{
			{Type: "code", Key: "source", Label: "Código", Language: "python", Value: "print('Hola desde Goradio')"},
			{Type: "dataframe", Key: "sheet", Label: "Tabla", Headers: []string{"Nombre", "Valor"}, Datatype: []string{"str", "number"},
				RowCount: 3, ColumnCount: 2, FixedColumns: true},
		},
		Outputs: []goradio.Component{
			{Type: "code", Key: "preview", Label: "Salida", Language: "python", Value: "# Vista previa"},
			{Type: "markdown", Key: "status", Label: "Estado", Value: "**Tabla lista para editar**"},
		},
	}, func(_ context.Context, v []any) ([]any, error) { return []any{v[0], "**Datos recibidos**"}, nil }, "")
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := app.Serve(ctx, "127.0.0.1", *port, nil); err != nil {
		log.Fatal(err)
	}
}
