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
- Optional public sharing with supported tunnel providers.

## Public sharing

| Provider | Identifier | Requirement |
| --- | --- | --- |
| Cloudflare Quick Tunnel | `cloudflare` | `cloudflared` installed |
| localhost.run | `localhostrun` | SSH client installed |
| Pinggy | `pinggy` | SSH client installed |
| Localtunnel | `localtunnel` | Localtunnel CLI installed |

Launch multiple tunnels:

```python
demo.launch(share=["cloudflare", "pinggy"])
```

Sharing exposes application endpoints to the Internet. Enable
authentication and validate all user-provided values.

## Compatibility

The project's initial UI and API are inspired by Gradio. They
do not provide complete frontend or backend compatibility.

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
