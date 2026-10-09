package goradio

// Páginas independientes en una sola aplicación con sidebar tipo SB Admin.
// Cada ruta mantiene su propio Handler y configuración Gradio.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

type Window struct {
	Path    string        `json:"path"`
	Title   string        `json:"title"`
	Icon    string        `json:"icon,omitempty"`
	Group   string        `json:"group,omitempty"`
	Config  Config        `json:"config"`
	Handler Handler       `json:"-"`
	Stream  StreamHandler `json:"-"`
}
type Dashboard struct {
	Title   string
	pages   []Window
	apps    map[string]*App
	tunnels *TunnelManager
	root    *App
}
type dashboardCtxKey struct{}
type dashboardPageContext struct {
	title   string
	current string
	pages   []Window
}

var dashboardPath = regexp.MustCompile(`^/[a-z][a-z0-9-]{0,47}$`)
var dashboardReserved = map[string]bool{"api": true, "gradio_api": true, "assets": true, "static": true, "login": true, "auth": true, "classic": true, "config": true, "theme.css": true, "favicon.ico": true, "favicon.png": true, "app.js": true, "app.css": true}

func normalizeWindowPath(path string) (string, error) {
	if path == "" || path == "/" {
		return "/", nil
	}
	path = "/" + strings.Trim(path, "/")
	if !dashboardPath.MatchString(path) {
		return "", fmt.Errorf("ruta de ventana inválida: %q", path)
	}
	if dashboardReserved[strings.TrimPrefix(path, "/")] {
		return "", errors.New("ruta de ventana reservada")
	}
	return path, nil
}
func NewDashboard(title string, windows ...Window) (*Dashboard, error) {
	dashboard := &Dashboard{Title: title, apps: map[string]*App{}, tunnels: NewTunnelManager()}
	for _, window := range windows {
		if err := dashboard.Add(window); err != nil {
			return nil, err
		}
	}
	return dashboard, nil
}
func (d *Dashboard) Add(window Window) error {
	if d.root != nil && d.root.security != nil {
		return errors.New("agrega páginas antes de activar seguridad")
	}
	p, err := normalizeWindowPath(window.Path)
	if err != nil {
		return err
	}
	if _, exists := d.apps[p]; exists {
		return fmt.Errorf("ruta de ventana duplicada: %s", p)
	}
	if window.Handler == nil {
		return fmt.Errorf("handler faltante en %s", p)
	}
	if window.Title == "" {
		window.Title = window.Config.Title
	}
	if window.Title == "" {
		window.Title = "Página"
	}
	window.Path = p
	if window.Config.Title == "" {
		window.Config.Title = window.Title
	}
	if d.Title == "" {
		d.Title = "Goradio"
	}
	mediaDir := ""
	if d.root != nil {
		mediaDir = d.root.MediaDir
	}
	app, err := New(window.Config, window.Handler, mediaDir)
	if err != nil {
		return err
	}
	app.tunnels = d.tunnels
	app.Stream = window.Stream
	if d.root == nil {
		d.root = app
	}
	d.pages = append(d.pages, window)
	d.apps[p] = app
	return nil
}

// SetTheme applies the same Gradio theme to every window in a dashboard.
// Configure it before serving requests; no theme editor is rendered to users.
func (d *Dashboard) SetTheme(theme Theme) error {
	if err := validateTheme(&theme); err != nil {
		return err
	}
	for _, app := range d.apps {
		copyTheme := theme
		app.Config.Theme = &copyTheme
	}
	return nil
}

func (d *Dashboard) SetMediaDir(dir string) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	for _, app := range d.apps {
		app.MediaDir = dir
	}
	return nil
}
func (d *Dashboard) Route(title, path string, config Config, handler Handler) error {
	return d.Add(Window{Path: path, Title: title, Config: config, Handler: handler})
}

// dashboardStateID limita el identificador persistente y separa los formularios
// de las ventanas, manteniendo las sesiones en la misma base SQLite.
func dashboardStateID(appID, path string) string {
	slug := strings.Trim(path, "/")
	if slug == "" {
		slug = "home"
	}
	combined := appID + "__" + slug
	if len(combined) <= 80 {
		return combined
	}
	sum := sha256.Sum256([]byte(combined))
	return appID[:min(40, len(appID))] + "__" + hex.EncodeToString(sum[:16])
}
func (d *Dashboard) EnableSecurity(cfg SecurityConfig) error {
	if d.root == nil {
		return errors.New("registra al menos una página antes de activar seguridad")
	}
	if cfg.AppID == "" {
		cfg.AppID = "dashboard"
	}
	if err := d.root.EnableSecurity(cfg); err != nil {
		return err
	}
	for p, a := range d.apps {
		if a == d.root {
			continue
		}
		childCfg := d.root.security.cfg
		childCfg.AppID = dashboardStateID(cfg.AppID, p)
		a.security = &securityBackend{
			cfg: childCfg, db: d.root.security.db, failures: map[string][]time.Time{},
		}
	}
	// La página inicial también usa su propio ID de estado.
	rootPath := d.pages[0].Path
	d.root.security.cfg.AppID = dashboardStateID(cfg.AppID, rootPath)
	return nil
}
func (d *Dashboard) CreateUser(ctx context.Context, username, password, role string) error {
	if d.root == nil {
		return errors.New("dashboard vacío")
	}
	return d.root.CreateUser(ctx, username, password, role)
}
func (d *Dashboard) UserExists(ctx context.Context, username string) (bool, error) {
	if d.root == nil {
		return false, errors.New("dashboard vacío")
	}
	return d.root.UserExists(ctx, username)
}
func (d *Dashboard) Close() error {
	if d.root != nil {
		return d.root.Close()
	}
	return nil
}
func (d *Dashboard) Routes() http.Handler {
	if d.root == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "Dashboard sin páginas", 500) })
	}
	handlers := map[string]http.Handler{}
	for path, app := range d.apps {
		handlers[path] = app.Routes()
	}
	initial := d.pages[0].Path
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		selected := ""
		// Peticiones de API: el cliente del frontend añade una cabecera de página.
		// Los EventSource/SSE que no pueden añadir cabeceras usan Referer same-origin.
		header := r.Header.Get("X-Goradio-Page")
		if header != "" {
			if _, ok := handlers[header]; ok {
				selected = header
			} else {
				http.Error(w, "Página desconocida", 400)
				return
			}
		}
		if selected == "" && r.Referer() != "" {
			ref, err := url.Parse(r.Referer())
			if err == nil && ref.Host == r.Host {
				if _, ok := handlers[ref.Path]; ok {
					selected = ref.Path
				}
			}
		}
		if selected == "" {
			if _, ok := handlers[r.URL.Path]; ok {
				selected = r.URL.Path
			} else {
				selected = initial
			}
		}

		// The Gradio frontend sometimes resolves API routes relative to the
		// current page (/imagenes/gradio_api/info). Normalize them without
		// losing which page owns the handler/callback.
		for _, page := range d.pages {
			if page.Path == "/" || !strings.HasPrefix(r.URL.Path, page.Path+"/") {
				continue
			}
			suffix := strings.TrimPrefix(r.URL.Path, page.Path)
			if strings.HasPrefix(suffix, "/gradio_api/") ||
				strings.HasPrefix(suffix, "/api/") ||
				strings.HasPrefix(suffix, "/static/") ||
				strings.HasPrefix(suffix, "/assets/") ||
				suffix == "/config" || suffix == "/config/" ||
				suffix == "/theme.css" || suffix == "/favicon.ico" ||
				suffix == "/favicon.png" {
				if header != "" && header != page.Path {
					http.Error(w, "Página y ruta no coinciden", 400)
					return
				}
				selected = page.Path
				request := r.Clone(r.Context())
				target := *r.URL
				target.Path = suffix
				target.RawPath = ""
				request.URL = &target
				r = request
				break
			}
		}
		handler, exists := handlers[selected]
		if !exists {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/" && selected != "/" {
			http.Redirect(w, r, selected, http.StatusSeeOther)
			return
		}
		isPage := r.URL.Path == selected
		if isPage {
			ctx := context.WithValue(r.Context(), dashboardCtxKey{}, dashboardPageContext{title: d.Title, current: selected, pages: d.pages})
			copyRequest := r.Clone(ctx)
			copyURL := *r.URL
			copyURL.Path = "/"
			copyRequest.URL = &copyURL
			handler.ServeHTTP(w, copyRequest)
			return
		}
		// Desconocida, pero con estructura de página (ej. /does-not-exist)
		if strings.HasPrefix(r.URL.Path, "/") && !strings.Contains(strings.TrimPrefix(r.URL.Path, "/"), "/") && r.URL.Path != "/" && strings.HasPrefix(r.Header.Get("Accept"), "text/html") {
			if _, found := handlers[r.URL.Path]; !found {
				http.NotFound(w, r)
				return
			}
		}
		handler.ServeHTTP(w, r)
	})
}
func (d *Dashboard) Serve(ctx context.Context, host string, port int, providers []string) error {
	if d.root == nil {
		return errors.New("dashboard vacío")
	}
	if host == "" {
		host = "127.0.0.1"
	}
	if port < 0 || port > 65535 {
		return errors.New("puerto inválido")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		return err
	}
	defer listener.Close()
	address := listener.Addr().(*net.TCPAddr)
	server := &http.Server{Handler: d.Routes(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 90 * time.Second}
	log.Printf("Goradio Dashboard: http://127.0.0.1:%d", address.Port)
	shareCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	d.tunnels.Start(shareCtx, address.Port, providers)
	go func() {
		<-ctx.Done()
		shutdown, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = server.Shutdown(shutdown)
	}()
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func dashboardShell(info dashboardPageContext) (string, string, string) {
	links := strings.Builder{}
	lastGroup := ""
	for _, p := range info.pages {
		if p.Group != "" && p.Group != lastGroup {
			links.WriteString(`<div class="gd-nav-caption">`)
			links.WriteString(html.EscapeString(p.Group))
			links.WriteString(`</div>`)
			lastGroup = p.Group
		}
		active := p.Path == info.current
		klass := "gd-nav-link"
		aria := ""
		if active {
			klass += " is-active"
			aria = ` aria-current="page"`
		}
		icon := p.Icon
		if icon == "" {
			icon = "file"
		}
		links.WriteString(fmt.Sprintf(`<a class="%s" href="%s" title="%s" data-icon="%s"%s><span class="gd-nav-icon" aria-hidden="true"></span><span class="gd-link-label">%s</span></a>`,
			klass, html.EscapeString(p.Path), html.EscapeString(p.Title), html.EscapeString(icon), aria, html.EscapeString(p.Title)))
	}
	title := info.title
	if title == "" {
		title = "Goradio"
	}
	// JSON para JS sin interpolación sin escape.
	pageSlug, _ := json.Marshal(info.current)
	extraHead := fmt.Sprintf(`<link rel="stylesheet" href="/goradio-dashboard.css">
 <script>window.__goradioPage=%s;(function(){const original=window.fetch.bind(window);window.fetch=function(input,init){const uri=typeof input==="string"?input:input instanceof URL?input.href:input.url;const target=new URL(uri,location.href);if(target.origin===location.origin&&(target.pathname.startsWith("/gradio_api/")||target.pathname.startsWith("/api/")||target.pathname==="/config")){const options=init?{...init}:{};const headers=new Headers(options.headers||(input instanceof Request?input.headers:undefined));headers.set("X-Goradio-Page",window.__goradioPage);options.headers=headers;return original(input,options)}return original(input,init)}})();</script>
 <script defer src="/goradio-dashboard.js"></script>`, string(pageSlug))
	opener := fmt.Sprintf(`<div class="gd-layout">
  <aside id="gd-sidebar" class="gd-sidebar" aria-label="Ventanas de Goradio">
    <div class="gd-brand-row">
      <a href="%s" class="gd-brand" title="Goradio"><span class="gd-brand-mark">G</span><span class="gd-brand-label">%s<span class="gd-brand-sub">GORADIO</span></span></a>
      <button id="gd-sidebar-collapse" class="gd-sidebar-collapse" type="button" title="Contraer barra lateral" aria-label="Contraer barra lateral" aria-controls="gd-sidebar" aria-expanded="true"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="m15 5-7 7 7 7"></path></svg></button>
    </div>
    <nav class="gd-side-scroll" aria-label="Páginas"><div class="gd-nav-caption">VENTANAS</div>%s</nav>
    <div class="gd-sidebar-footer"><span class="gd-sidebar-footer-label">Goradio</span></div>
  </aside>
  <div id="gd-backdrop" class="gd-backdrop" aria-hidden="true"></div>
  <main class="gd-main">
    <div class="gd-mobile-toolbar"><button id="gd-menu-toggle" class="gd-menu-toggle" type="button" aria-label="Abrir menú" aria-controls="gd-sidebar" aria-expanded="false"><span aria-hidden="true">☰</span></button><span class="gd-mobile-label">%s</span></div>`,
		html.EscapeString(info.pages[0].Path), html.EscapeString(title), links.String(), html.EscapeString(title))
	return extraHead, opener, `</main></div>`
}
