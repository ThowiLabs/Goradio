"""Demo multipágina con navegación lateral, basada en la API de Gradio.

Inicio:
    conda activate 311
    python examples/dashboard_demo.py

La interfaz visual utiliza los componentes oficiales Gradio 6.30.0.
Las funciones se ejecutan en Python y el servidor/router/túneles en Go.
"""
from __future__ import annotations
from pathlib import Path
import tempfile
import os
import goradio as gr
from PIL import Image as PILImage, ImageOps


def welcome(name: str, number: float):
    return f"Bienvenido, {name or 'visitante'}. Tu panel está en Goradio.", (number or 0)*2


def process_image(path: str | None, format: str):
    if not path:
        return None, "Carga una imagen para convertir."
    ext = {"WebP": ".webp", "JPEG": ".jpg", "PNG": ".png"}[format]
    with PILImage.open(path) as source:
        image=ImageOps.exif_transpose(source)
        image.thumbnail((1400,1400))
        if format=="JPEG":image=image.convert("RGB")
        with tempfile.TemporaryDirectory(prefix="goradio-dashboard-") as folder:
            dest=Path(folder)/("imagen"+ext)
            image.save(dest,format=format,quality=80 if format!="PNG" else None)
            out=gr.media(dest)
    return out,f"Imagen convertida a {format} ({image.width} × {image.height})"


def passthrough_audio(audio: str | None):
    return gr.media(audio) if audio else None


def settings(theme: str, scale: float, active: bool):
    return {"tema":theme,"escala":scale,"habilitado":active}


dashboard=gr.Dashboard(title="Goradio Admin",description="Workspace multipágina · Go + Python")
dashboard.route(
    "Inicio", "/",
    gr.Interface(
        fn=welcome,
        inputs=[gr.Textbox(label="Tu nombre",placeholder="Escribe tu nombre"),
                gr.Number(label="Un número",value=5)],
        outputs=[gr.Textbox(label="Mensaje"),gr.Number(label="Resultado")],
        title="Panel principal",description="Prueba el envío y el estado de una página independiente.",
    ),
    icon="dashboard",group="Principal",
)
dashboard.route(
    "Imágenes", "/imagenes",
    gr.Interface(
        fn=process_image,
        inputs=[gr.Image(label="Imagen de entrada"),gr.Dropdown(["WebP","JPEG","PNG"],label="Formato",value="WebP")],
        outputs=[gr.Image(label="Imagen procesada"),gr.Textbox(label="Resumen")],
        title="Herramientas de imágenes",description="Conversión de formato con la librería Python Pillow.",
    ),
    icon="image",group="Herramientas",
)
dashboard.route(
    "Audio", "/audio",
    gr.Interface(
        fn=passthrough_audio,
        inputs=gr.Audio(label="Audio de entrada"),
        outputs=gr.Audio(label="Reproductor con forma de onda"),
        title="Estudio de audio",description="Carga WAV/MP3, procesa y visualiza con el reproductor oficial Gradio.",
    ),
    icon="music",group="Herramientas",
)
dashboard.route(
    "Configuración", "/configuracion",
    gr.Interface(
        fn=settings,
        inputs=[gr.Dropdown(["Claro","Oscuro","Sistema"],label="Tema",value="Sistema"),
                gr.Slider(label="Escala",minimum=1,maximum=10,value=5),
                gr.Checkbox(label="Habilitado",value=True)],
        outputs=gr.JSON(label="Preferencias"),
        title="Configuración",description="Página independiente con selectores, checkbox y respuesta JSON.",
    ),
    icon="settings",group="Sistema",
)
if __name__=="__main__":
    dashboard.launch(
        share=[x for x in os.getenv("GORADIO_SHARE","").split(",") if x],
        server_port=int(os.getenv("GORADIO_PORT","7932")),
    )
