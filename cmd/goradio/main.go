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
	securityDB := flag.String("security-db", "", "Ruta persistente SQLite para login y sesiones")
	appID := flag.String("app-id", "goradio", "Identificador aislado por aplicación")
	requireLogin := flag.Bool("require-login", false, "Solicitar login para todas las páginas y operaciones")
	allowSignup := flag.Bool("allow-signup", false, "Permitir auto registro público (usar con cuidado)")
	persist := flag.Bool("persist", false, "Guardar valores de inputs y outputs en SQLite")
	bootstrapUser := flag.String("bootstrap-user", "", "Usuario admin para crear una sola vez")
	bootstrapEnv := flag.String("bootstrap-password-env", "GORADIO_ADMIN_PASSWORD", "Variable de entorno con la contraseña inicial de admin")

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
	var app interface {
		EnableSecurity(goradio.SecurityConfig) error
		Close() error
		CreateUser(context.Context, string, string, string) error
		UserExists(context.Context, string) (bool, error)
		Serve(context.Context, string, int, []string) error
	}
	var pages struct {
		Pages []struct {
			Path        string              `json:"path"`
			Title       string              `json:"title"`
			Icon        string              `json:"icon"`
			Group       string              `json:"group"`
			Mode        string              `json:"mode"`
			Description string              `json:"description"`
			Inputs      []goradio.Component `json:"inputs"`
			Outputs     []goradio.Component `json:"outputs"`
		} `json:"pages"`
	}
	if err = json.Unmarshal(raw, &pages); err != nil {
		log.Fatal(err)
	}
	if len(pages.Pages) > 0 {
		dashboard, createErr := goradio.NewDashboard(cfg.Title)
		if createErr != nil {
			log.Fatal(createErr)
		}
		for _, page := range pages.Pages {
			pageHandler := goradio.BuiltinHandler(page.Mode)
			if *callback != "" {
				slug := strings.Trim(page.Path, "/")
				if slug == "" {
					slug = "home"
				}
				pageHandler = goradio.PythonHandler(strings.TrimSuffix(*callback, "/")+"/"+slug, os.Getenv("GORADIO_CALLBACK_TOKEN"))
			}
			pageCfg := goradio.Config{
				Title: page.Title, Description: page.Description, Inputs: page.Inputs, Outputs: page.Outputs,
			}
			if err := dashboard.Add(goradio.Window{
				Path: page.Path, Title: page.Title, Icon: page.Icon, Group: page.Group,
				Config: pageCfg, Handler: pageHandler,
			}); err != nil {
				log.Fatal(err)
			}
		}
		if err := dashboard.SetMediaDir(*media); err != nil {
			log.Fatal(err)
		}
		app = dashboard
	} else {
		single, createErr := goradio.New(cfg, handler, *media)
		if createErr != nil {
			log.Fatal(createErr)
		}
		app = single
	}

	if *securityDB != "" {
		sec := goradio.SecurityConfig{
			DBPath: *securityDB, AppID: *appID, RequireLogin: *requireLogin,
			AllowSignup: *allowSignup, PersistInputs: *persist, PersistOutputs: *persist,
		}
		if err := app.EnableSecurity(sec); err != nil {
			log.Fatal(err)
		}
		defer app.Close()
		if *bootstrapUser != "" {
			secret := os.Getenv(*bootstrapEnv)
			exists, err := app.UserExists(context.Background(), *bootstrapUser)
			if err != nil {
				log.Fatal(err)
			}
			if !exists {
				if secret == "" {
					log.Fatalf("para crear %s debes definir la variable %s", *bootstrapUser, *bootstrapEnv)
				}
				if err := app.CreateUser(context.Background(), *bootstrapUser, secret, "admin"); err != nil {
					log.Fatal(err)
				}
				log.Printf("Usuario administrador %s creado", *bootstrapUser)
			}
		}
		if *requireLogin {
			log.Printf("Login activo con SQLite: %s", *securityDB)
		}
	} else if *requireLogin || *allowSignup || *persist || *bootstrapUser != "" {
		log.Fatal("es necesario --security-db para estas opciones")
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
