package goradio

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/*
var assets embed.FS

func assetHandler() http.Handler {
	sub, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
