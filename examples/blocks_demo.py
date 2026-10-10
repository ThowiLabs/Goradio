"""Original Gradio UI with native Tabs, Row, and separate event callback."""
import goradio as gr

def basic(text):
    return [f"## Resultado completo\n\n{text}",
            {"headers":["Campo","Valor"],"data":[["Texto",text]],"metadata":None}]

demo = gr.Blocks(
    fn=basic,
    title="Goradio Blocks · Python",
    inputs=[gr.Textbox(key="query",label="Consulta",value="Hola")],
    outputs=[gr.Markdown(key="result",label="Resultado"),
             gr.Dataframe(key="table",label="Tabla",headers=["Campo","Valor"])],
    layout=gr.Tabs(
        gr.Tab("Consulta",gr.Row(gr.Ref("query"),gr.Button("summarize","Resumir"))),
        gr.Tab("Resultados",gr.Column(gr.Ref("result"),gr.Ref("table"))),
    ),
)
demo.on("summarize","click",lambda text: f"### Resumen\n\n**{text}**",
        inputs=["query"],outputs=["result"])

if __name__=="__main__":
    demo.launch(server_port=7992)
