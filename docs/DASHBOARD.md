# Multipage Dashboard

Goradio 0.4.0 introduces a responsive SB Admin–inspired navigation shell around the bundled Gradio 6.30.0 frontend. Each page has independent components and callbacks. This API is inspired by Gradio's routing concepts but is **not** a complete replacement for Gradio Blocks.

## Python

```python
import goradio as gr

home = gr.Interface(
    fn=lambda name: "Hello, " + name,
    inputs=gr.Textbox(label="Your name"),
    outputs=gr.Textbox(label="Greeting"),
)
tools = gr.Interface(
    fn=lambda text: text.upper(),
    inputs=gr.Textbox(label="Text"),
    outputs=gr.Textbox(label="Uppercase"),
)

dashboard = gr.Dashboard(title="My workspace")
dashboard.route("Home", "/", home, icon="dashboard", group="General")
dashboard.route("Tools", "/tools", tools, icon="file", group="General")
dashboard.launch(server_port=7860, share=False)
```

The Python API is **Dashboard.route(title, path, app, *, icon="file", group="")**. Each app must be a separate Interface instance. Routes must be unique, single-segment, lowercase URL paths, for example /tools or /images.

Run the multimedia example with **python examples/dashboard_demo.py**. Image conversion in this example requires Pillow.

## Go

```go
dashboard, err := goradio.NewDashboard("My workspace")
if err != nil { log.Fatal(err) }

err = dashboard.Add(goradio.Window{
    Path: "/", Title: "Home", Icon: "dashboard", Group: "General",
    Config: goradio.Config{
        Inputs: []goradio.Component{{Type: "textbox", Label: "Name"}},
        Outputs: []goradio.Component{{Type: "textbox", Label: "Greeting"}},
    },
    Handler: func(_ context.Context, values []any) ([]any, error) {
        return []any{"Hello, " + values[0].(string)}, nil
    },
})
if err != nil { log.Fatal(err) }
if err := dashboard.Serve(context.Background(), "127.0.0.1", 7860, nil); err != nil {
    log.Fatal(err)
}
```

Complete native Go example: **examples/dashboard-go/main.go**.

### Window configuration

| Field | Description |
| --- | --- |
| Path | URL route, e.g. / or /settings |
| Title | Sidebar text and page title |
| Icon | dashboard, home, image, music, settings, file, chart, users, folder, terminal |
| Group | Optional navigation group |
| Config | Independent Gradio component configuration |
| Handler | Independent callback implemented in Go |

The current menu link receives aria-current="page". At narrow viewport widths the sidebar becomes an off-canvas panel with an accessible toggle, backdrop and Escape-to-close behavior.

## Login and per-page persistence

Configure all pages before enabling security:

```go
err := dashboard.EnableSecurity(goradio.SecurityConfig{
    DBPath: "./data/dashboard.sqlite",
    AppID: "workspace",
    RequireLogin: true,
    PersistInputs: true,
    PersistOutputs: true,
})
if err != nil { log.Fatal(err) }
defer dashboard.Close()
```

All pages share the same SQLite user accounts, login sessions and cookie. Each page stores its form state under a distinct identifier. For Python use the existing **security={...}** argument to Dashboard.launch.

## Browser and API routing

Browser pages use /, /tools and other defined paths. The native Gradio frontend sends the X-Goradio-Page header for fetch requests, while EventSource and media loads use a same-origin Referer. Relative endpoints such as /tools/gradio_api/run/predict and /tools/static/fonts/... are normalized to their page.

External API clients may send X-Goradio-Page: /tools on requests to shared root endpoints. Arbitrary Blocks layouts, streaming and full upstream queue support are not yet implemented.

## Public sharing

```python
dashboard.launch(share=["gradio"])
```

Gradio Live uses the native Go tunnel client, without downloading or executing an external FRP executable. Public URLs are temporary; see [Native sharing](GRADIO_NATIVE.md).

## Testing

```bash
go test ./...
go test -race ./...
python -m unittest discover -s python/tests -v
```

Optional end-to-end Python/Go callback testing: set GORADIO_RUN_INTEGRATION=1 and GORADIO_BINARY to the compiled executable, then run python -m unittest discover -s python/tests -p test_dashboard_integration.py -v. Chrome dashboard tests live in scripts/dev/dashboard_browser_smoke.mjs.
