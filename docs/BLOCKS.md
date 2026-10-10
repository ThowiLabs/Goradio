# Native Blocks layouts (experimental)

Goradio embeds the original **Gradio 6.30.0 frontend**. This release adds a
**validated subset of Blocks layouts and targeted events** on top of the existing
`Interface`, `Dashboard`, live input events and SSE streaming. It does **not**
replace the frontend with an HTML template or add a separate admin dashboard.

## Containers and event bindings

| Gradio container/control | Go helper | Python helper |
| --- | --- | --- |
| Row | `goradio.Row` | `gr.Row` |
| Column | `goradio.Column` | `gr.Column` |
| Group | `goradio.Group` | `gr.Group` |
| Accordion | `goradio.Accordion` | `gr.Accordion` |
| Tabs and Tab | `goradio.Tabs`, `goradio.Tab` | `gr.Tabs`, `gr.Tab` |
| Component reference | `goradio.Ref("key")` | `gr.Ref("key")` |
| Button | `goradio.Button("key","Label")` | `gr.Button("key","Label")` |
| Event | `app.On(EventSpec,...)` | `demo.on(...)` |

Every configured input and output must have a **unique `key`** and occur
**exactly once** in the layout. Container nesting, button keys and references
are checked before serving. Maximum: 128 layout nodes, 12 levels, 64
additional events. A `Tabs` container contains `Tab` children.

The standard Interface `Submit`/`Clear`, `Load`, `Live`, `Timer`,
session state, and streaming remain supported. **Targeted `on` callbacks**
are currently **non-streaming** and receive only the named input values.
Their results update only the named outputs. Register all Go events before
starting the server.

### Go

```go
layout := goradio.Tabs(
    goradio.Tab("Consulta", goradio.Row(
        goradio.Ref("text"),
        goradio.Button("summarize", "Resumir"),
    )),
    goradio.Tab("Resultado", goradio.Ref("result")),
)
app, err := goradio.New(goradio.Config{
    Title: "Mi herramienta",
    Layout: &layout,
    Inputs: []goradio.Component{
        {Type:"textbox", Key:"text", Label:"Consulta"},
    },
    Outputs: []goradio.Component{
        {Type:"markdown", Key:"result", Label:"Respuesta"},
    },
}, func(_ context.Context, values []any)([]any,error){
    return []any{"Completo: " + values[0].(string)},nil
}, "")
if err != nil { log.Fatal(err) }

err = app.On(goradio.EventSpec{
    Target:"summarize", Trigger:"click",
    Inputs:[]string{"text"}, Outputs:[]string{"result"},
},func(_ context.Context, values []any)([]any,error){
    return []any{"Resumen: " + values[0].(string)},nil
})
if err != nil { log.Fatal(err) }
```

Run `go run ./examples/blocks-go` for the complete Go example.

### Python

```python
import goradio as gr

demo = gr.Blocks(
    fn=lambda text: text.upper(),
    inputs=[gr.Textbox(key="text", label="Texto")],
    outputs=[gr.Markdown(key="result", label="Resultado")],
    layout=gr.Tabs(
        gr.Tab("Entrada", gr.Row(
            gr.Ref("text"), gr.Button("run","Procesar")
        )),
        gr.Tab("Resultado", gr.Ref("result")),
    ),
)
demo.on(
    "run", "click", lambda text: f"**{text}**",
    inputs=["text"], outputs=["result"],
)
demo.launch()
```

This is an **explicit layout tree**, not Gradio's context-manager syntax
(`with gr.Blocks(): ...`). The Gradio components, tabs and event rendering
are real, but the Go backend supports only the declared subset.

Try `python examples/blocks_demo.py` or include a Blocks instance in an
existing multipage `Dashboard.route(...)`.

## Native editable Dataframe and Code

`gr.Dataframe(...)` supports headers, datatypes, row and column counts,
fixed/dynamic sizing, search, and copy controls using the actual Gradio table.
As an **input**, cells can be edited and the table can import CSV/TSV in the
frontend. Input payloads are validated in Go; unknown column names, malformed
row lengths, oversized tables, and invalid numeric cells are rejected.

```python
table = gr.Dataframe(
    key="sheet", label="Datos",
    headers=["Nombre", "Edad"], datatype=["str", "number"],
    row_count=4, column_count=2, fixed_columns=True,
    show_search=True,
)
script = gr.Code(key="script", language="python", lines=12)
```

The code editor is Gradio's own syntax-highlighting component. It does
**not execute uploaded code**. Table import/search/editing comes from the
upstream widget; server-side per-cell event graphs, advanced formulas,
collaborative editing, pandas transformations and export workflows remain
future work.

To see the native components, run `go run ./examples/editor-go`.

## ChatInterface: connect a model

`gr.ChatInterface` uses the original Gradio `Chatbot` with text input, SSE
streaming and bounded per-browser-session history. It accepts a user-provided
`fn(message, history)` that returns a string or yields progressive **full**
assistant responses; the app developer can connect their own local model
or hosted inference client. Goradio deliberately does not pretend a
demonstration reply is a real AI model.

```python
import goradio as gr

def infer(message, history):
    # Replace with your actual model's inference function.
    yield "Procesando..."
    yield f"Respuesta de prueba a: {message}"

chat = gr.ChatInterface(fn=infer, title="Mi asistente")
chat.launch()
```

Conversation history is in-memory per browser session, capped and evicted
to fit the 64 KiB state limit. It expires after inactivity or server restart.
There is no automatic database-backed conversation archive, model selector,
tool-calling, attachments in chat, or production-ready RAG in this subset.

See `examples/chat_interface.py` and
`scripts/dev/chat_interface_browser_smoke.py`.

## Verification

```bash
go test -race -count=1 ./...
go vet ./...
python -m unittest discover -s python/tests -v
python scripts/dev/blocks_python_browser_smoke.py
python scripts/dev/chat_interface_browser_smoke.py
```

The two browser smoke scripts use the local Go CLI and a headless Chrome.
They confirm real native Tabs, a custom button event, an SSE streaming chat,
and no browser console/network errors.

## Still outstanding

Goradio does not yet implement all upstream Blocks features: arbitrary
nested dependency graphs, multiple independent streaming functions, every
event type and cancellation policy, user-defined UI render functions,
component-specific select/edit event payloads, dynamic component insertion,
all multimedia editors, Model3D, FileExplorer, full queue concurrency control,
or full upstream theme/component parameters. Do not call it 100% Gradio API
compatible.
