# Goradio

**Build interactive web interfaces in Go or Python using the Gradio frontend.**

Goradio is a Go-powered framework for exposing functions through browser-based interfaces. It provides a Python API inspired by Gradio, a native Go API, multimedia components, optional authentication, persistent user state, and shareable links.

The project embeds the compiled **Gradio 6.30.0 frontend** and implements a compatible subset of its server API in Go. Goradio is an independent project and is **not affiliated with or endorsed by Gradio**.

> **Project status:** Experimental. The API and compatibility surface are evolving. Review the [limitations](#compatibility-and-limitations) before deploying publicly.

## Features

- **Go-first runtime** — HTTP server, routing, request validation, and execution backed by Go.
- **Python integration** — Define interfaces with familiar `Interface(fn, inputs, outputs)` semantics.
- **Gradio frontend** — Reuses bundled Gradio 6.30.0 UI assets rather than imitating the interface.
- **Multipage navigation** — Collapsible sidebar inspired by SB Admin, with the original Gradio interface and theme unchanged; independent routes and callbacks, shared login and optional per-page saved state. The sidebar inherits Gradio's theme and light/dark mode; the developer configures its primary and secondary colors in Go or Python, without exposing an editor to visitors.
- **Interactive components** — Text, numbers, sliders, checkboxes, dropdowns, JSON, images, audio, video, files, Dataframe, Plot, Gallery and Chatbot (experimental).
- **Reactive events and streaming** — Native `input`, `change`, page `load`, periodic Timer, SSE v3 streaming, output-property updates, sequential callbacks and bounded per-session state.
- **Native public sharing** — Generate `*.gradio.live` links using a Go implementation of the tunnel protocol; no separate `frpc` executable is required.
- **Authentication and persistence** — Optional SQLite accounts, sessions, and saved form inputs and outputs.
- **Alternative tunnels** — Integrates with Cloudflare Tunnel, localhost.run, Pinggy, and Localtunnel when their corresponding tools are available.

## Requirements

| Runtime | Requirement |
| --- | --- |
| Go | 1.24 or later |
| Python (optional) | 3.10 or later |
| Gradio frontend | Bundled; no separate Gradio installation required |
| Public sharing | Internet connection; external providers may require additional tools |

## Quick start

### Python

Clone the repository and install the local Python package:

```bash
git clone https://github.com/ThowiLabs/Goradio.git
cd Goradio
go build -o bin/goradio ./cmd/goradio
python -m pip install -e ./python
```

Create an interface:

```python
import goradio as gr

def greet(name):
    return f"Hello, {name}!"

demo = gr.Interface(
    fn=greet,
    inputs=gr.Textbox(label="Name", required=True),
    outputs=gr.Textbox(label="Greeting"),
    title="Hello from Goradio",
)

demo.launch(server_port=7860)
```

Visit **http://127.0.0.1:7860** in your browser.

To share the same app using the native Go client:

```python
demo.launch(share=["gradio"])
```

The resulting `gradio.live` URL is temporary and depends on the availability of Gradio's sharing service. See [native sharing](docs/GRADIO_NATIVE.md).

### Go

Run the included example:

```bash
go run ./examples/hello-go
```

Or create an application directly with the Go API:

```go
package main

import (
    "context"
    "log"

    "goradio"
)

func main() {
    app, err := goradio.New(
        goradio.Config{
            Title: "Hello from Go",
            Inputs: []goradio.Component{
                {Type: "textbox", Label: "Name", Required: true},
            },
            Outputs: []goradio.Component{
                {Type: "textbox", Label: "Greeting"},
            },
        },
        func(_ context.Context, values []any) ([]any, error) {
            return []any{"Hello, " + values[0].(string) + "!"}, nil
        },
        "",
    )
    if err != nil {
        log.Fatal(err)
    }
    if err := app.Serve(context.Background(), "127.0.0.1", 7860, nil); err != nil {
        log.Fatal(err)
    }
}
```

The Go module currently uses the module path `goradio`. To import it into a separate Go project, use a local `replace` directive or update the module path for your deployment. See [the external-module example](examples/external-go-app).

## Multipage dashboard

Build multipage workspaces with independent Python callbacks and the bundled Gradio frontend:

```python
dashboard = gr.Dashboard(title="My workspace")
dashboard.route("Home", "/", gr.Interface(
    fn=lambda name: "Hello " + name,
    inputs=gr.Textbox(label="Name"),
    outputs=gr.Textbox(label="Greeting"),
), icon="dashboard")
dashboard.route("Tools", "/tools", gr.Interface(
    fn=lambda text: text.upper(),
    inputs=gr.Textbox(label="Text"),
    outputs=gr.Textbox(label="Uppercase"),
), icon="file")
dashboard.launch()
```

For Go configuration, state isolation, routing and authentication, see the [dashboard guide](docs/DASHBOARD.md).

## Components

| Component | Python helper | Typical value |
| --- | --- | --- |
| Text | `gr.Textbox()` | `str` |
| Number | `gr.Number()` | Number |
| Slider | `gr.Slider()` | Number |
| Checkbox | `gr.Checkbox()` | Boolean |
| Dropdown | `gr.Dropdown([...])` | Selected value |
| JSON | `gr.JSON()` | JSON-compatible value |
| Image | `gr.Image()` | Local uploaded file path |
| Audio | `gr.Audio()` | Local uploaded file path |
| Video | `gr.Video()` | Local uploaded file path |
| File | `gr.File()` | Local uploaded file path |
| Dataframe | `gr.Dataframe()` | Table data with headers and rows |
| Plot | `gr.Plot()` | Plotly JSON-compatible value |
| Gallery | `gr.Gallery()` | Registered local image paths |
| Chatbot | `gr.Chatbot()` | Chat message history |

For multimedia outputs, use `gr.media(path)` to register a **trusted** local file. Never pass an arbitrary user-provided path to `media()`.

## Authentication and persistent state

Goradio supports optional SQLite-backed user accounts, sessions, and form-state persistence. Applications can also use HTTP Basic authentication for simpler setups.

```python
demo.launch(
    security={
        "db_path": "./data/app.sqlite",
        "app_id": "my-app",
        "require_login": True,
        "allow_signup": False,
        "persist": True,
        "bootstrap_user": "admin",
    }
)
```

Provide the initial administrator password securely, as described in the [security and persistence guide](docs/SECURITY_AND_PERSISTENCE.md). Do not use example credentials for publicly accessible applications.

## Sharing

| Provider | Identifier | Dependencies |
| --- | --- | --- |
| Gradio Live | `gradio` | Built into Go; no external FRP executable |
| Cloudflare Quick Tunnel | `cloudflare` | `cloudflared` |
| localhost.run | `localhostrun` | SSH client |
| Pinggy | `pinggy` | SSH client |
| Localtunnel | `localtunnel` | Localtunnel CLI |

You can start multiple providers at the same time. Check `/api/tunnels` for connection status and verified URLs. Public links expose your application's HTTP endpoints; enable authentication before handling private data.

## Architecture

```text
Python callbacks / Go handlers
             │
             ▼
       Goradio (Go)
   ├── HTTP and API routing
   ├── Component validation
   ├── Sessions and SQLite
   ├── File and media handling
   └── Native tunnel client
             │
             ▼
   Gradio frontend 6.30.0
             │
             ▼
        Web browser
```

The compiled frontend is bundled under `third_party/gradio/`, with provenance and license details documented in [SOURCE.md](third_party/gradio/SOURCE.md).

## Compatibility and limitations

Goradio implements a **subset** of Gradio's server protocol. It does not claim drop-in compatibility with all Gradio applications, including arbitrary `Blocks` layouts, full upstream queue semantics, complex dependency graphs, or every component configuration. The supported event triggers and progressive streaming are documented separately.

The framework has not undergone a production security audit. Before production deployment, configure authentication, persistent storage, HTTPS, sensible resource limits, and monitoring. Public sharing endpoints are intended primarily for development and demonstrations.

## Documentation

- [Security, authentication, and state persistence](docs/SECURITY_AND_PERSISTENCE.md)
- [Native Gradio Live sharing](docs/GRADIO_NATIVE.md)
- [Multipage navigation](docs/DASHBOARD.md)
- [Reactive events, streaming, tables and state](docs/REACTIVE.md)
- [Examples and showcase](examples/SHOWCASE.md)
- [Release history](CHANGELOG.md)

## Development

```bash
go test ./...
go test -race ./...
go vet ./...
python -m unittest discover -s python/tests -v
```

If you plan to contribute, open an issue or pull request describing the behavior, expected compatibility, and tests.

## Third-party software and licensing

Goradio includes third-party frontend assets and protocol-related code with their original notices and licenses. See [the bundled Gradio license](third_party/gradio/LICENSE) and [FRP protocol notices](internal/frpmsg/NOTICE.md).

**A project-wide distribution license has not yet been declared.** Do not assume that the licenses of bundled third-party components automatically apply to the Goradio source code.
