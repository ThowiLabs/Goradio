# Goradio

**Create interactive browser interfaces with Go or Python.**

Goradio is an experimental Go-powered web UI framework with a Python
API inspired by Gradio. It focuses on function invocation, component
validation, and optional public sharing.

> **Status:** Experimental. Not a drop-in replacement for Gradio.

## Requirements

- Go 1.24 or later.
- Python 3.10 or later for Python applications.
- Internet access only when opening public sharing tunnels.

## Quick start

Clone the repository and install the Python package:

```bash
git clone https://github.com/ThowiLabs/Goradio.git
cd Goradio
go build -o bin/goradio ./cmd/goradio
python -m pip install -e ./python
```

```python
import goradio as gr

def greet(name):
    return f"Hello, {name}!"

demo = gr.Interface(
    fn=greet,
    inputs=gr.Textbox(label="Name"),
    outputs=gr.Textbox(label="Greeting"),
)
demo.launch()
```

Run the native Go example:

```bash
go run ./examples/hello-go
```

The application is available at `http://127.0.0.1:7860`.

## Features

- HTTP server and function execution implemented in Go.
- Python interface API and typed input components.
- Text, number, slider, checkbox and dropdown support.
- Image, audio, video, file and JSON components.
- Bundled compiled Gradio 6.30.0 frontend.
- Basic Gradio-compatible API and multimedia uploads.
- Optional SQLite accounts, sessions and persisted form state.
- Optional public sharing with supported tunnel providers.

## Public sharing

| Provider | Identifier | Requirement |
| --- | --- | --- |
| Cloudflare Quick Tunnel | `cloudflare` | `cloudflared` installed |
| localhost.run | `localhostrun` | SSH client installed |
| Pinggy | `pinggy` | SSH client installed |
| Localtunnel | `localtunnel` | Localtunnel CLI installed |
| Gradio Live (experimental) | `gradio` | Python Gradio and compatible service |

Launch multiple tunnels:

```python
demo.launch(share=["cloudflare", "pinggy"])
```

Sharing exposes application endpoints to the Internet. Enable
authentication and validate all user-provided values.

## Authentication and persistence

Goradio supports optional SQLite-backed users and saved form state.

```python
demo.launch(security={
    "db_path": "./data/app.sqlite",
    "app_id": "my-app",
    "require_login": True,
    "persist": True,
    "bootstrap_user": "admin",
})
```

Set the initial password as described in the
[security guide](docs/SECURITY_AND_PERSISTENCE.md).

## Compatibility

The bundled Gradio frontend connects to a partial Go implementation
of its backend protocol. Arbitrary Blocks layouts, full event queues,
streaming and every component configuration are not supported.
Frontend license and attribution: `third_party/gradio/`.

## Testing

```bash
go test ./...
go vet ./...
python -m unittest discover -s python/tests -v
```

## Development and licensing

Report bugs or propose enhancements through GitHub issues. Respect the
licenses of bundled dependencies. No project-wide distribution license
has been declared for Goradio's source code.
