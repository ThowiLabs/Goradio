
import goradio as gr

def compute(name, value):
    return "Hola, " + name + "! Total: " + str(value * 2)

app = gr.Interface(compute, inputs=[gr.Textbox(label="Nombre"), gr.Number(label="Cantidad")],
                   outputs=gr.Textbox(label="Resultado"), title="Prueba Python+Go")
app.launch(server_port=7880, share=False, block=True)
