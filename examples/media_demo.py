"""Ejemplo de controles multimedia con el frontend original de Gradio.

Para devolver archivos se usa goradio.media() y solo rutas de confianza.
"""
import os
import goradio as gr


def repetir(imagen, audio, video):
    return (
        gr.media(imagen) if imagen else None,
        gr.media(audio) if audio else None,
        gr.media(video) if video else None,
    )


if __name__ == "__main__":
    gr.Interface(
        repetir,
        inputs=[
            gr.Image(label="Imagen"),
            gr.Audio(label="Audio"),
            gr.Video(label="Video"),
        ],
        outputs=[
            gr.Image(label="Imagen procesada"),
            gr.Audio(label="Audio procesado"),
            gr.Video(label="Video procesado"),
        ],
        title="Goradio · Multimedia",
        description="Interfaz Gradio original y backend Go con callback Python.",
    ).launch(server_port=int(os.environ.get("GORADIO_PORT", "7881")), share=False)
