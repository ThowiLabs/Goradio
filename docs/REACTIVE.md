# Reactivity and streaming in Goradio

Goradio uses the **original Gradio 6.30.0 frontend**. Its Go backend now
implements the compatible subset below. These options do not add a theme
editor, admin dashboard, or custom replacement widgets.

## Available events

| Feature | Go | Python | Notes |
| --- | --- | --- | --- |
| Submit / Clear | Yes | Yes | Existing Gradio Interface controls |
| `input` and `change` | `Config.Live` | `live=True` | Re-runs the current handler using all inputs |
| Periodic timer | `Config.EverySeconds` | `every=5` | Accepts 0.25–3600 seconds; client page must be open |
| Page load | `Config.Load` | `load=True` | Runs the handler when Gradio initializes |
| Progressive output | `SetStreamHandler` | Generator (`yield`) | Native queue protocol and SSE v3 patches |
| Update output properties | `goradio.Update(...)` | `gr.update(...)` | Update value, visibility or interactivity |
| Sequential callbacks | `app.Then(handler)` | `demo.then(fn)` | Sequential processing; not a full Gradio Blocks event graph |
| Session state | `app.StateGet/Set` | `gr.state_get/set` | In-memory per-session values, 30-minute inactivity TTL |
| Custom validation | `app.SetValidator` | `validate=fn` | Runs before the handler; combined field validation |
| Cancel a running task | Queue cancel endpoint | Queue cancel endpoint | By Gradio session hash and event ID |
| Dataframe | `Component{Type:"dataframe"}` | `gr.Dataframe(...)` | Real Gradio Dataframe; header and numeric cell validation |
| Streaming Chatbot | `Component{Type:"chatbot"}` | `gr.Chatbot(...)` | Original Gradio component; plug in your own AI model |

`gr.update` and `Then` are compatible subsets, **not** a complete reimplementation
of arbitrary Gradio `Blocks`, `State`, event graphs, cancellation policies,
progress tracking, Dataframe editing callbacks, or every upstream property.

## Go: real-time monitoring

```go
app, err := goradio.New(goradio.Config{
    Title:        "Monitor de procesos",
    Live:         true, // run on input/change
    Load:         true, // run on page load
    EverySeconds: 5,    // timer ticks every 5 seconds
    Inputs: []goradio.Component{
        {Type: "textbox", Label: "Proceso", Value: "worker"},
    },
    Outputs: []goradio.Component{
        {Type: "markdown", Label: "Estado"},
        {Type: "dataframe", Label: "Tabla",
            Headers: []string{"Proceso", "Estado", "Porcentaje"}},
    },
}, callback, "")
if err != nil { log.Fatal(err) }

app.SetStreamHandler(func(ctx context.Context, values []any, emit func([]any) error) error {
    for percent := 0; percent <= 100; percent += 25 {
        if err := ctx.Err(); err != nil { return err }
        snapshot := []any{
            fmt.Sprintf("**%d%%**", percent),
            map[string]any{
                "headers": []string{"Proceso", "Estado", "Porcentaje"},
                "data": [][]any{{values[0], "Procesando", percent}},
                "metadata": nil,
            },
        }
        if err := emit(snapshot); err != nil { return err }
    }
    return nil
})
```

For a working example, run `go run ./examples/reactive-go`.
The sample changes real Gradio Markdown and Dataframe components. It uses
demonstration data rather than monitoring a real process.

## Python: live events and generators

```python
import goradio as gr
import time

def progress(task):
    for step in range(1, 4):
        yield f"**{task}: {step}/3**"
        time.sleep(.2)

demo = gr.Interface(
    fn=progress,
    inputs=gr.Textbox(label="Tarea", min_length=2, max_length=50),
    outputs=gr.Markdown(),
    title="Monitor",
    load=True,
    live=True,
    every=5,
)
demo.then(lambda final: final + " · Finalizado")
demo.launch()
```

Generators are detected automatically. Python sends bounded NDJSON results to
the local Go callback, and Go streams them through Gradio's own
`/gradio_api/queue/join` and `/gradio_api/queue/data` protocol.

For stateful callbacks:

```python
def increment(text):
    visits = gr.state_get("visits", 0) + 1
    gr.state_set("visits", visits)
    return f"{text}: visita {visits}"
```

Use `state_set` only from an active Gradio callback. Each page has its own
in-memory state store; use SQLite for durable application data. Do not store
passwords or other sensitive secrets in the transient session store.

To validate multiple fields, pass a `validate` callback to `gr.Interface`.
Raise `ValueError` to reject invalid input:

```python
def check(age, permission):
    if age < 18 and permission:
        raise ValueError("La selección requiere revisar la autorización")

demo = gr.Interface(
    fn=your_function,
    inputs=[gr.Number(), gr.Checkbox()],
    outputs=gr.Textbox(),
    validate=check,
)
```

## Chat streaming

`go run ./examples/chat-streaming-go` runs the original Gradio `Chatbot`
component backed by Go session state. Its replies are **scripted**, not a
real LLM. Connect an inference model through `StreamHandler` (Go) or
a Python generator to produce actual AI responses.

## Operations and limits

- `Timer` and `Live` depend on an open browser and reachable server.
  A timer does **not** run jobs in the background when the UI is closed.
- `StreamHandler` yields **complete output snapshots**, not token-only deltas.
  Goradio converts later snapshots to Gradio sse_v3 patches. The backend
  buffers up to 16 snapshots per job, dropping older intermediate frames
  if the browser cannot keep up, but retaining the final snapshot.
- Jobs have a 90-second deadline. If the browser disconnects, canceled jobs
  must honor their `context.Context`; external work should also be cancellable.
- At most 100 queued jobs are intended. State is bounded to 1024 active
  sessions, 64 keys/session, and 64 KiB/item. In-memory state expires after
  30 minutes of inactivity and is lost when the process exits.
- Dataframe input validation currently checks structure, column names,
  row length, and numeric typed cells. This is **not** full pandas integration.
- Public sharing URLs can rotate when `gradio.live` reconnects; it is
  independent of UI live-update behavior.

## Validation

```bash
go test -race -count=1 ./...
go vet ./...
python -m unittest discover -s python/tests -v
go run ./examples/reactive-go
go run ./examples/chat-streaming-go
python scripts/dev/reactive_python_smoke.py
```

The Python smoke test starts the CLI bridge and verifies a generator and
`.then` callback in headless Chrome. It expects the Go executable at
`bin/goradio-reactive-cli.exe` on Windows, built from `./cmd/goradio`,
and a Chrome installation.
