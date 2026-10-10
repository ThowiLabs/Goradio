"""Radio and CheckboxGroup from the original bundled Gradio frontend."""
import goradio as gr

demo=gr.Interface(
    fn=lambda color, languages: f"Color: {color}; lenguajes: {', '.join(languages)}",
    inputs=[
        gr.Radio(["Rojo","Azul","Verde"],label="Color",value="Azul"),
        gr.CheckboxGroup(["Go","Python","Rust"],label="Lenguajes",value=["Go"],required=True),
    ],
    outputs=gr.Textbox(label="Selección"),
    title="Goradio · Selecciones nativas",
)

if __name__=="__main__":
    demo.launch(server_port=7995)
