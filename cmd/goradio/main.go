package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"goradio"
)

func main() {
	path := flag.String("config", "", "Ruta de configuración JSON")
	host := flag.String("host", "127.0.0.1", "Dirección local de escucha")
	port := flag.Int("port", 7860, "Puerto de escucha (0 para automático)")
	share := flag.String("share", "", "Proveedores de túneles separados por comas")
	media := flag.String("media-dir", "", "Directorio de multimedia")
	callback := flag.String("callback-url", "", "Endpoint interno Python")
	flag.Parse()
	if *path == "" {
		log.Fatal("obligatorio: --config")
	}
	raw, err := os.ReadFile(*path)
	if err != nil {
		log.Fatal(err)
	}
	var cfg goradio.Config
	if err = json.Unmarshal(raw, &cfg); err != nil {
		log.Fatal(err)
	}
	handler := goradio.BuiltinHandler("echo")
	if *callback != "" {
		handler = goradio.PythonHandler(*callback, os.Getenv("GORADIO_CALLBACK_TOKEN"))
	} else {
		var doc struct{ Mode string }
		if err = json.Unmarshal(raw, &doc); err == nil && doc.Mode != "" {
			handler = goradio.BuiltinHandler(doc.Mode)
		}
	}
	if *media == "" {
		dir, err := os.MkdirTemp("", "goradio-media-")
		if err != nil {
			log.Fatal(err)
		}
		defer os.RemoveAll(dir)
		*media = dir
	}
	app, err := goradio.New(cfg, handler, *media)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	providers := []string{}
	if strings.TrimSpace(*share) != "" {
		providers = strings.Split(*share, ",")
	}
	if err := app.Serve(ctx, *host, *port, providers); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
