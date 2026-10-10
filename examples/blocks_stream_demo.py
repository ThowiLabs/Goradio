"""Native Gradio Blocks with two independent callbacks, one progressive."""
import goradio as gr
import time

def base(prompt):
    return "Sin procesar", "Esperando"

def progress(prompt):
    for i in range(1,4):
        yield f"**Etapa {i}/3:** {prompt}"
        if i<3:
            time.sleep(.075)

demo=gr.Blocks(
    fn=base,
    title="Goradio · Eventos Blocks independientes",
    inputs=[gr.Textbox(key="prompt",label="Petición",value="prueba")],
    outputs=[
        gr.Markdown(key="progress_text",label="Avance"),
        gr.Textbox(key="instant_text",label="Resultado inmediato"),
    ],
    layout=gr.Tabs(
        gr.Tab("Entrada",gr.Column(
            gr.Ref("prompt"),
            gr.Row(gr.Button("stream","Procesar progresivo"),gr.Button("instant","Procesar inmediato"))
        )),
        gr.Tab("Resultados",gr.Column(gr.Ref("progress_text"),gr.Ref("instant_text")))
    ),
)
demo.on("stream","click",progress,inputs=["prompt"],outputs=["progress_text"])
demo.on("instant","click",lambda prompt:prompt.upper(),inputs=["prompt"],outputs=["instant_text"])

if __name__=="__main__":
    demo.launch(server_port=7994)
