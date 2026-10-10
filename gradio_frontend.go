package goradio

import (
	"archive/zip"
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

//go:embed third_party/gradio/frontend.zip
var upstreamFrontend embed.FS

var (
	frontendOnce sync.Once
	frontendFS   *zip.Reader
	frontendErr  error
)

func loadFrontend() (*zip.Reader, error) {
	frontendOnce.Do(func() {
		data, err := upstreamFrontend.ReadFile("third_party/gradio/frontend.zip")
		if err != nil {
			frontendErr = err
			return
		}
		frontendFS, frontendErr = zip.NewReader(bytes.NewReader(data), int64(len(data)))
	})
	return frontendFS, frontendErr
}

var assetNames = regexp.MustCompile(`^(assets|static)/[a-zA-Z0-9_./-]+$`)

func (a *App) gradioAsset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if !assetNames.MatchString(name) || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	archive, err := loadFrontend()
	if err != nil {
		http.Error(w, "frontend no disponible", 500)
		return
	}
	f, err := archive.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "error leyendo asset", 500)
		return
	}
	typ := mime.TypeByExtension(path.Ext(name))
	if typ == "" {
		typ = "application/octet-stream"
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, err = w.Write(data)
	if err != nil {
		log.Printf("asset %s: %v", name, err)
	}
}
func (a *App) gradioIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	config := a.gradioConfigForRequest(r)
	extraHead, shellOpen, shellClose := "", "", ""
	if ctx, ok := r.Context().Value(dashboardCtxKey{}).(dashboardPageContext); ok {
		// Las peticiones EventSource y elementos multimedia no pueden incluir
		// X-Goradio-Page: su Referer same-origin identifica la ventana activa.
		w.Header().Set("Referrer-Policy", "same-origin")
		extraHead, shellOpen, shellClose = dashboardShell(ctx)
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		http.Error(w, "Error de configuración", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// La interfaz, CSS y todos sus módulos son los del frontend oficial 6.30.0.
	// El documento hospedador solo aporta la configuración nativa generada por Go.
	fmt.Fprintf(w, `<!doctype html><html lang="es"><head>
 <meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
 <meta name="referrer" content="same-origin"><base href="/">%s
 <link rel="stylesheet" href="/goradio-share.css">
 <link rel="stylesheet" href="/goradio-auth.css">
 <title>Goradio</title>
 <script>window.gradio_config=%s;window.__gradio_mode__="app";window.iFrameResizer={autoResize:false,sizeWidth:false,onReady:()=>{}};</script>
 <script type="module" crossorigin src="/assets/index-D88pgWqG.js"></script>
 <script defer src="/goradio-share.js"></script>
 <script defer src="/goradio-auth.js"></script>
 <link rel="stylesheet" crossorigin href="/assets/index-DuOipanW.css">
 </head><body style="margin:0;min-height:100vh;display:flex;flex-direction:column">
 %s<gradio-app control_page_title="true" embed="false" eager="true" style="display:flex;flex-direction:column;flex-grow:1"></gradio-app>%s
 </body></html>`, extraHead, encoded, shellOpen, shellClose)
}

type node map[string]any

func branch(id int, children ...node) node { return node{"id": id, "children": children} }
func leaf(id int) node                     { return node{"id": id} }
func propsComponent(id int, kind string, props map[string]any) map[string]any {
	return map[string]any{"id": id, "type": kind, "props": props, "skip_api": false, "component_class_id": id, "key": nil}
}
func (a *App) gradioConfig() map[string]any {
	next := 0
	id := func() int { next++; return next }
	root := id()
	// Organizar componentes como Gradio Interface: cabecera y dos columnas.
	header := id()
	columns := id()
	left := id()
	right := id()
	inputs := []int{}
	outputs := []int{}
	all := []any{
		propsComponent(root, "column", map[string]any{"visible": true, "variant": "default", "scale": 1, "min_width": 320}),
		propsComponent(header, "markdown", map[string]any{"value": "# " + a.Config.Title + "\n\n" + a.Config.Description, "visible": true, "sanitize_html": true, "line_breaks": true, "header_links": false}),
		propsComponent(columns, "row", map[string]any{"visible": true, "equal_height": false}),
		propsComponent(left, "column", map[string]any{"visible": true, "scale": 1, "min_width": 320}),
		propsComponent(right, "column", map[string]any{"visible": true, "scale": 1, "min_width": 320}),
	}
	// Los componentes ordinarios se mantienen como nodos. Los que declaran
	// section se agrupan dentro de un Accordion real de Gradio.
	type grouped struct {
		id      int
		section string
	}
	leftItems := []grouped{}
	rightItems := []grouped{}
	for _, c := range a.Config.Inputs {
		n := id()
		inputs = append(inputs, n)
		all = append(all, propsComponent(n, gradioComponentName(c.Type), gradioComponentProps(c, true)))
		leftItems = append(leftItems, grouped{id: n, section: c.Section})
	}
	for _, c := range a.Config.Outputs {
		n := id()
		outputs = append(outputs, n)
		all = append(all, propsComponent(n, gradioComponentName(c.Type), gradioComponentProps(c, false)))
		rightItems = append(rightItems, grouped{id: n, section: c.Section})
	}
	wrapSections := func(items []grouped) []node {
		children := []node{}
		last := ""
		for _, item := range items {
			label := strings.TrimSpace(item.section)
			if label == "" {
				children = append(children, leaf(item.id))
				last = ""
				continue
			}
			if last == label && len(children) > 0 {
				previous := &children[len(children)-1]
				(*previous)["children"] = append((*previous)["children"].([]node), leaf(item.id))
			} else {
				groupID := id()
				all = append(all, propsComponent(groupID, "accordion", map[string]any{
					"label": label, "open": false, "visible": true, "elem_id": nil,
					"elem_classes": []string{},
				}))
				children = append(children, branch(groupID, leaf(item.id)))
			}
			last = label
		}
		return children
	}
	leftChildren := wrapSections(leftItems)
	rightChildren := wrapSections(rightItems)
	submit := id()
	all = append(all, propsComponent(submit, "button", map[string]any{"value": "Submit", "variant": "primary", "visible": true, "size": "lg", "interactive": true}))
	leftChildren = append(leftChildren, leaf(submit))
	var timerID int
	if a.Config.EverySeconds > 0 {
		timerID = id()
		all = append(all, propsComponent(timerID, "timer", map[string]any{"value": a.Config.EverySeconds, "active": true, "render": true}))
		leftChildren = append(leftChildren, leaf(timerID))
	}
	clear := id()
	all = append(all, propsComponent(clear, "button", map[string]any{"value": "Clear", "variant": "secondary", "visible": true, "size": "sm", "interactive": true}))
	rightChildren = append(rightChildren, leaf(clear))
	deps := []any{
		map[string]any{
			"id": 0, "targets": []any{[]any{submit, "click"}}, "inputs": inputs, "outputs": outputs, "backend_fn": true,
			"queue": a.Stream != nil, "api_name": "predict", "api_description": nil, "scroll_to_output": true,
			"show_progress": "full", "show_progress_on": nil, "batch": false, "max_batch_size": 4,
			"cancels": []any{}, "types": map[string]bool{"generator": a.Stream != nil, "cancel": false},
			"collects_event_data": false, "trigger_after": nil, "trigger_only_on_success": false,
			"trigger_only_on_failure": false, "trigger_mode": "once", "api_visibility": "public",
			"rendered_in": nil, "render_id": nil, "connection": "sse", "time_limit": nil,
			"stream_every": 0.5, "event_specific_args": nil, "component_prop_inputs": []int{},
		},
		map[string]any{
			"id": 1, "targets": []any{[]any{clear, "click"}}, "inputs": []int{}, "outputs": append(append([]int{}, inputs...), outputs...), "backend_fn": true,
			"js": nil, "queue": false, "api_name": "clear", "show_progress": "hidden",
			"batch": false, "max_batch_size": 4, "cancels": []any{}, "types": map[string]bool{"generator": false, "cancel": false},
			"collects_event_data": false, "trigger_mode": "once", "api_visibility": "private", "connection": "sse",
			"event_specific_args": nil, "component_prop_inputs": []int{},
		},
	}
	// The original Gradio load event runs once when a page is ready.
	if a.Config.Load {
		deps = append(deps, map[string]any{
			"id": len(deps), "targets": []any{[]any{root, "load"}},
			"inputs": inputs, "outputs": outputs, "backend_fn": true, "queue": a.Stream != nil,
			"api_name": nil, "show_progress": "minimal", "scroll_to_output": false,
			"cancels": []any{}, "types": map[string]bool{"generator": a.Stream != nil, "cancel": false},
			"trigger_mode": "once", "api_visibility": "private", "connection": "sse",
			"stream_every": 0.5, "event_specific_args": nil,
		})
	}
	// Native change, input, and timer events run the existing prediction
	// handler, without replacing Gradio widgets with custom HTML.
	if a.Config.Live {
		for _, inputID := range inputs {
			for _, trigger := range []string{"change", "input"} {
				deps = append(deps, map[string]any{
					"id": len(deps), "targets": []any{[]any{inputID, trigger}},
					"inputs": inputs, "outputs": outputs, "backend_fn": true, "queue": a.Stream != nil,
					"api_name": nil, "show_progress": "minimal", "scroll_to_output": false,
					"cancels": []any{}, "types": map[string]bool{"generator": a.Stream != nil, "cancel": false},
					"trigger_mode": "always_last", "api_visibility": "private", "connection": "sse",
					"stream_every": 0.5, "event_specific_args": nil,
				})
			}
		}
	}
	if timerID > 0 {
		deps = append(deps, map[string]any{
			"id": len(deps), "targets": []any{[]any{timerID, "tick"}},
			"inputs": inputs, "outputs": outputs, "backend_fn": true, "queue": a.Stream != nil,
			"api_name": nil, "show_progress": "hidden", "scroll_to_output": false,
			"cancels": []any{}, "types": map[string]bool{"generator": a.Stream != nil, "cancel": false},
			"trigger_mode": "always_last", "api_visibility": "private", "connection": "sse",
			"stream_every": 0.5, "event_specific_args": nil,
		})
	}
	// Eventos reactivos nativos Gradio. Cada input se guarda sin pulsar Submit.
	// input captura escritura y slider en tiempo real; change cubre selects y otros.
	if a.security != nil && a.security.cfg.PersistInputs {
		for i, inputID := range inputs {
			for _, trigger := range []string{"input", "change"} {
				index := len(deps)
				deps = append(deps, map[string]any{
					"id": index, "targets": []any{[]any{inputID, trigger}},
					"inputs": []int{inputID}, "outputs": []int{},
					"backend_fn": true, "queue": false, "api_name": fmt.Sprintf("autosave_%d_%s", i, trigger),
					"api_description": nil, "scroll_to_output": false, "show_progress": "hidden",
					"batch": false, "max_batch_size": 4, "cancels": []any{},
					"types":               map[string]bool{"generator": false, "cancel": false},
					"collects_event_data": false, "trigger_after": nil, "trigger_only_on_success": false,
					"trigger_only_on_failure": false, "trigger_mode": "always_last", "api_visibility": "private",
					"rendered_in": nil, "render_id": nil, "connection": "sse", "time_limit": nil,
					"stream_every": 0.5, "event_specific_args": nil, "component_prop_inputs": []int{},
				})
			}
		}
	}
	layout := branch(root, leaf(header), branch(columns, branch(left, leftChildren...), branch(right, rightChildren...)))
	if a.Config.Layout != nil {
		refs := map[string]int{}
		targets := map[string]int{}
		for i, c := range a.Config.Inputs {
			refs[c.Key] = inputs[i]
			targets[c.Key] = inputs[i]
		}
		for i, c := range a.Config.Outputs {
			refs[c.Key] = outputs[i]
			targets[c.Key] = outputs[i]
		}
		var create func(LayoutNode) node
		create = func(spec LayoutNode) node {
			if spec.Type == "component" {
				return leaf(refs[spec.Component])
			}
			if spec.Type == "button" {
				n := id()
				targets[spec.Key] = n
				all = append(all, propsComponent(n, "button", map[string]any{
					"value": spec.Label, "variant": "secondary", "visible": true, "size": "md", "interactive": true,
					"elem_id": nil, "elem_classes": []string{},
				}))
				return leaf(n)
			}
			n := id()
			kind := spec.Type
			props := map[string]any{"visible": true, "elem_id": nil, "elem_classes": []string{}}
			switch kind {
			case "row":
				props["equal_height"] = false
			case "column":
				props["scale"] = 1
				props["min_width"] = 320
				props["variant"] = "default"
			case "accordion":
				props["label"] = spec.Label
				props["open"] = spec.Open
			case "tabs":
				props["selected"] = nil
			case "tab":
				kind = "tabitem"
				props["label"] = spec.Label
				props["id"] = nil
				props["interactive"] = true
			}
			all = append(all, propsComponent(n, kind, props))
			children := make([]node, 0, len(spec.Children))
			for _, child := range spec.Children {
				children = append(children, create(child))
			}
			return branch(n, children...)
		}
		actions := id()
		all = append(all, propsComponent(actions, "row", map[string]any{"visible": true, "equal_height": false}))
		layout = branch(root, leaf(header), create(*a.Config.Layout), branch(actions, leaf(submit), leaf(clear)))
		for _, ev := range a.Config.Events {
			eventInputs := make([]int, 0, len(ev.Inputs))
			eventOutputs := make([]int, 0, len(ev.Outputs))
			for _, key := range ev.Inputs {
				eventInputs = append(eventInputs, refs[key])
			}
			for _, key := range ev.Outputs {
				eventOutputs = append(eventOutputs, refs[key])
			}
			deps = append(deps, map[string]any{
				"id": len(deps), "targets": []any{[]any{targets[ev.Target], ev.Trigger}},
				"inputs": eventInputs, "outputs": eventOutputs,
				"backend_fn": true, "queue": false, "api_name": nil, "show_progress": "minimal",
				"scroll_to_output": false, "batch": false, "cancels": []any{},
				"types":        map[string]bool{"generator": false, "cancel": false},
				"trigger_mode": "once", "api_visibility": "private", "connection": "sse",
				"stream_every": 0.5, "event_specific_args": nil,
			})
		}
	}
	ids := make([]int, 0, next)
	for n := 1; n <= next; n++ {
		ids = append(ids, n)
	}
	cfg := map[string]any{
		"version": "6.30.0", "api_prefix": "/gradio_api", "mode": "interface", "app_id": "goradio",
		"dev_mode": false, "vibe_mode": false, "analytics_enabled": false, "components": all,
		"css": nil, "connect_heartbeat": false, "js": nil, "head": nil, "title": a.Config.Title,
		"space_id": nil, "enable_queue": a.Stream != nil, "show_error": true, "footer_links": []any{},
		"run_history": false, "is_colab": false, "max_file_size": a.MaxUploadBytes,
		"stylesheets": []any{}, "theme": "default", "protocol": "sse_v3",
		"body_css":    map[string]string{"body_background_fill": "white", "body_text_color": "#1f2937", "body_background_fill_dark": "#0b0f19", "body_text_color_dark": "#f3f4f6"},
		"fill_height": false, "fill_width": false, "theme_hash": 0, "pwa": false,
		"pages": []any{[]any{"", "", true}}, "current_page": "", "root": "",
		"page": map[string]any{"": map[string]any{"layout": layout, "components": ids, "dependencies": func() []int {
			v := make([]int, len(deps))
			for i := range v {
				v[i] = i
			}
			return v
		}()}},
		"username": nil, "dependencies": deps, "layout": layout,
	}
	return cfg
}
func gradioComponentName(k string) string {
	switch k {
	case "json":
		return "json"
	case "checkbox":
		return "checkbox"
	default:
		return k
	}
}
func gradioComponentProps(c Component, input bool) map[string]any {
	p := map[string]any{"label": c.Label, "visible": true, "interactive": input, "elem_id": nil, "elem_classes": []string{}, "show_label": true, "container": true, "scale": nil, "min_width": 160, "key": nil}
	if c.Label == "" {
		p["label"] = strings.ToUpper(c.Type[:1]) + c.Type[1:]
	}
	if c.Placeholder != "" {
		p["placeholder"] = c.Placeholder
	}
	if c.Value != nil {
		p["value"] = c.Value
	}
	if c.Min != nil {
		p["minimum"] = *c.Min
	}
	if c.Max != nil {
		p["maximum"] = *c.Max
	}
	if c.Step != nil {
		p["step"] = *c.Step
	}
	switch c.Type {
	case "textbox":
		p["lines"] = 1
		p["max_lines"] = 20
		p["type"] = "text"
		p["submit_btn"] = false
		p["stop_btn"] = false
		if c.MinLength != nil {
			p["min_length"] = *c.MinLength
		}
		if c.MaxLength != nil {
			p["max_length"] = *c.MaxLength
		}
		if c.Pattern != "" {
			p["pattern"] = c.Pattern
		}
	case "slider":
		if c.Min == nil {
			p["minimum"] = 0
		}
		if c.Max == nil {
			p["maximum"] = 100
		}
		if c.Step == nil {
			p["step"] = 1
		}
		if c.Value == nil {
			p["value"] = p["minimum"]
		}
	case "number":
		p["precision"] = nil
	case "dropdown":
		options := []any{}
		for _, v := range c.Choices {
			options = append(options, []string{v, v})
		}
		p["choices"] = options
		p["allow_custom_value"] = false
		p["multiselect"] = false
		p["filterable"] = true
	case "image":
		p["sources"] = []string{"upload", "clipboard"}
		p["type"] = "filepath"
		p["image_mode"] = "RGB"
		p["buttons"] = []string{"fullscreen", "download"}
		p["streaming"] = false
		p["format"] = "webp"
	case "audio":
		p["sources"] = []string{"upload", "microphone"}
		p["type"] = "filepath"
		p["format"] = "wav"
		p["waveform_options"] = map[string]any{
			"waveform_color":          "#9ca3af",
			"waveform_progress_color": "#f97316",
			"trim_region_color":       "#f9731660",
			"show_recording_waveform": true,
			"skip_length":             5,
			"sample_rate":             44100,
			"show_controls":           false,
		}
		p["editable"] = input
		p["buttons"] = []string{"download"}
		p["loop"] = false
		p["minimal"] = false
		p["playback_position"] = 0
		p["streaming"] = false
		p["autoplay"] = false
	case "video":
		p["sources"] = []string{"upload"}
		p["format"] = "mp4"
		p["webcam_options"] = map[string]any{"mirror": false, "constraints": map[string]any{}}
		p["include_audio"] = true
		p["autoplay"] = false
		p["loop"] = false
		p["show_download_button"] = true
	case "file":
		p["file_count"] = "single"
		p["file_types"] = nil
	case "json":
		if c.Value == nil {
			p["value"] = map[string]any{}
		}
	case "markdown":
		p["sanitize_html"] = true
		p["line_breaks"] = true
		p["header_links"] = false
		p["show_copy_button"] = false
		if c.Value == nil {
			p["value"] = ""
		}
	case "code":
		p["language"] = "python"
		if c.Language != "" {
			p["language"] = c.Language
		}
		p["lines"] = 5
		if c.Lines > 0 {
			p["lines"] = c.Lines
		}
		p["max_lines"] = 25
		p["wrap_lines"] = true
		p["show_copy_button"] = true
		p["show_fullscreen_button"] = true
		p["show_line_numbers"] = true
		if c.Value == nil {
			p["value"] = ""
		}
	case "dataframe":
		p["datatype"] = "auto"
		if len(c.Datatype) > 0 {
			p["datatype"] = c.Datatype
		}
		if len(c.Headers) > 0 && c.Value == nil {
			p["value"] = map[string]any{"headers": c.Headers, "data": []any{}, "metadata": nil}
		}
		p["type"] = "array"
		p["wrap"] = true
		p["show_row_numbers"] = true
		p["show_fullscreen_button"] = true
		p["show_copy_button"] = true
		if c.ShowCopyButton != nil {
			p["show_copy_button"] = *c.ShowCopyButton
		}
		p["show_search"] = "filter"
		if c.ShowSearch != nil && !*c.ShowSearch {
			p["show_search"] = nil
		}
		rows := 3
		if c.RowCount > 0 {
			rows = c.RowCount
		}
		cols := 3
		if c.ColumnCount > 0 {
			cols = c.ColumnCount
		}
		if len(c.Headers) > 0 {
			cols = len(c.Headers)
		}
		rowMode := "dynamic"
		colMode := "dynamic"
		if c.FixedRows {
			rowMode = "fixed"
		}
		if c.FixedColumns {
			colMode = "fixed"
		}
		p["row_count"] = []any{rows, rowMode}
		p["col_count"] = []any{cols, colMode}
		p["column_count"] = []any{cols, colMode}
		if c.Value == nil && len(c.Headers) == 0 {
			p["value"] = map[string]any{"headers": []any{}, "data": []any{}, "metadata": nil}
		}
	case "chatbot":
		p["type"] = "messages"
		p["layout"] = "bubble"
		p["render_markdown"] = true
		p["sanitize_html"] = true
		p["line_breaks"] = true
		p["show_copy_button"] = true
		p["show_copy_all_button"] = true
		p["show_share_button"] = false
		p["height"] = 460
		p["bubble_full_width"] = false
		p["avatar_images"] = []any{nil, nil}
		p["feedback_options"] = []any{}
		if c.Value == nil {
			p["value"] = []any{}
		} else if messages, err := normalizeGradioChatMessages(c.Value); err == nil {
			p["value"] = messages
		} else {
			p["value"] = []any{}
		}
	case "gallery":
		p["sources"] = []string{"upload", "clipboard"}
		p["file_types"] = []string{"image"}
		p["columns"] = 3
		p["rows"] = 2
		p["height"] = 420
		p["object_fit"] = "cover"
		p["allow_preview"] = true
		p["preview"] = false
		p["show_fullscreen_button"] = true
		p["show_download_button"] = true
		p["show_share_button"] = false
		p["buttons"] = []string{"download", "fullscreen"}
		if c.Value == nil {
			p["value"] = []any{}
		}
	case "plot":
		p["show_label"] = true
		p["show_actions_button"] = true
		p["format"] = "webp"
		p["show_fullscreen_button"] = true
		if c.Value == nil {
			p["value"] = nil
		}
	}
	return p
}
func (a *App) gradioConfigResponse(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.gradioConfigForRequest(r))
}
func (a *App) gradioTheme(w http.ResponseWriter, r *http.Request) {
	theme, err := assets.ReadFile("static/gradio-default-theme.css")
	if err != nil {
		http.Error(w, "No se encontró el tema original de Gradio", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if a.Config.Theme != nil {
		theme = append(theme, []byte(themeOverrides(a.Config.Theme))...)
	}
	if _, err := w.Write(theme); err != nil {
		log.Printf("Error enviando CSS de Gradio: %v", err)
	}
}
func (a *App) gradioInfo(w http.ResponseWriter, r *http.Request) {
	// Contrato mínimo legible por @gradio/client; evolucionará con el soporte Blocks.
	p := []any{}
	ret := []any{}
	for i, c := range a.Config.Inputs {
		p = append(p, map[string]any{"label": c.Label, "parameter_name": "param_" + strconv.Itoa(i), "component": gradioComponentName(c.Type), "type": map[string]any{"type": "string"}, "python_type": map[string]string{"type": "str", "description": ""}, "example_input": "", "parameter_has_default": false})
	}
	for _, c := range a.Config.Outputs {
		ret = append(ret, map[string]any{"label": c.Label, "component": gradioComponentName(c.Type), "type": map[string]any{"type": "string"}, "python_type": map[string]string{"type": "str", "description": ""}})
	}
	writeJSON(w, 200, map[string]any{"named_endpoints": map[string]any{"/predict": map[string]any{"parameters": p, "returns": ret, "api_visibility": "public", "show_api": true}}, "unnamed_endpoints": map[string]any{}})
}

func (a *App) gradioFavicon(w http.ResponseWriter, r *http.Request) {
	archive, err := loadFrontend()
	if err != nil {
		http.Error(w, "Error de interfaz", 500)
		return
	}
	file, err := archive.Open("favicon.png")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if _, err = io.Copy(w, file); err != nil {
		log.Printf("favicon: %v", err)
	}
}
