import goradio as gr


def saludar(nombre, intensidad, emocion):
    saludo = f"Hola, {nombre.strip() or 'mundo'}!"
    return (saludo.upper() if intensidad > 5 else saludo) + f" {emocion}"


demo = gr.Interface(
    fn=saludar,
    inputs=[
        gr.Textbox(label="Tu nombre", placeholder="Escribe tu nombre", required=True),
        gr.Slider(label="Intensidad", minimum=1, maximum=10, value=3),
        gr.Dropdown(["🙂", "🎉", "🚀"], label="Emoción"),
    ],
    outputs=gr.Textbox(label="Respuesta"),
    title="Goradio · Demo Python",
    description="La interfaz y sus validaciones corren en Go; la función corre en Python.",
)

if __name__ == "__main__":
    demo.launch(share=["cloudflare", "localhostrun", "pinggy"])
