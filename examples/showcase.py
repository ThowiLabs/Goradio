"""Goradio Studio: compresor de imágenes y laboratorio de componentes.

Iniciar:
    conda activate 311
    python examples/showcase.py

El servidor y el frontend son Go + Gradio 6.30.0; solamente las
funciones de usuario de este ejemplo se ejecutan en Python.
"""
from __future__ import annotations

import os
import re
import tempfile
from pathlib import Path

import goradio as gr
from PIL import Image, ImageOps, UnidentifiedImageError


MAX_PIXELS = 12_000_000
Image.MAX_IMAGE_PIXELS = MAX_PIXELS


def readable_size(value: int) -> str:
    if value < 1024:
        return f"{value} B"
    if value < 1024 * 1024:
        return f"{value / 1024:.1f} KB"
    return f"{value / (1024 * 1024):.2f} MB"


def compress_image(
    uploaded_path: str | None,
    quality: float,
    max_dimension: float,
    output_format: str,
    remove_metadata: bool,
) -> tuple[str | None, str]:
    """Comprime dentro de un directorio temporal y registra la salida.

    Gradio entrega a la función Python una ruta a su almacenamiento privado.
    Se devuelve la ruta registrada mediante gr.media(), nunca una ruta de
    sistema arbitraria ni contenido externo al directorio de Goradio.
    """
    if not uploaded_path:
        return None, "Sube una imagen para probar la compresión."

    quality = max(10, min(95, int(quality or 75)))
    max_dimension = max(320, min(2048, int(max_dimension or 1280)))
    ext = {"WebP": ".webp", "JPEG": ".jpg", "PNG": ".png"}.get(output_format)
    if not ext:
        return None, "Formato de salida no admitido."

    try:
        path = Path(uploaded_path)
        original_size = path.stat().st_size
        # El límite de píxeles evita imágenes comprimidas que agotan memoria.
        with Image.open(path) as source:
            width, height = source.size
            if width * height > MAX_PIXELS:
                return None, f"Imagen demasiado grande: máximo {MAX_PIXELS:,} píxeles."
            metadata = source.info.get("exif")
            image = ImageOps.exif_transpose(source)
            image.thumbnail((max_dimension, max_dimension), Image.Resampling.LANCZOS)
            if output_format == "JPEG":
                # JPEG no soporta transparencias: usar un fondo blanco.
                if image.mode in ("RGBA", "LA") or "transparency" in image.info:
                    rgba = image.convert("RGBA")
                    background = Image.new("RGB", rgba.size, (255, 255, 255))
                    background.paste(rgba, mask=rgba.getchannel("A"))
                    image = background
                else:
                    image = image.convert("RGB")

            settings: dict = {"optimize": True}
            if output_format in ("JPEG", "WebP"):
                settings["quality"] = quality
            if output_format == "WebP":
                settings["method"] = 4
            elif output_format == "PNG":
                settings["compress_level"] = 9
            if not remove_metadata and metadata and output_format != "PNG":
                settings["exif"] = metadata

            with tempfile.TemporaryDirectory(prefix="goradio-compress-") as folder:
                output_file = Path(folder) / ("optimizada" + ext)
                image.save(output_file, format=output_format, **settings)
                new_size = output_file.stat().st_size
                shared_path = gr.media(output_file)

            percent = (1 - new_size / max(original_size, 1)) * 100
            change = f"{percent:.1f}% de reducción" if percent >= 0 else f"{-percent:.1f}% más grande"
            summary = (
                f"Formato: {output_format} · Calidad: {quality}\n"
                f"Resolución: {width}×{height} → {image.width}×{image.height}\n"
                f"Antes: {readable_size(original_size)}\n"
                f"Después: {readable_size(new_size)}\n"
                f"Resultado: {change}"
            )
            if output_format == "PNG":
                summary += "\nNota: PNG usa compresión sin pérdida; el control de calidad no aplica."
            return shared_path, summary
    except (OSError, ValueError, UnidentifiedImageError) as exc:
        return None, f"No se pudo procesar la imagen ({type(exc).__name__})."


def studio(
    image_path: str | None,
    quality: float,
    max_size: float,
    fmt: str,
    remove_exif: bool,
    text: str,
    multiplier: float,
    audio_path: str | None,
    video_path: str | None,
    file_path: str | None,
):
    result_image, compression_report = compress_image(
        image_path, quality, max_size, fmt, remove_exif
    )
    text = str(text or "")
    cleaned = re.sub(r"\s+", " ", text).strip()
    text_preview = cleaned.upper() if cleaned else "Escribe algo en el campo de texto."
    count = len(cleaned.split()) if cleaned else 0
    return (
        result_image,
        compression_report,
        text_preview,
        count,
        float(multiplier or 0) * 2,
        gr.media(audio_path) if audio_path else None,
        gr.media(video_path) if video_path else None,
        gr.media(file_path) if file_path else None,
        {
            "motor": "Go",
            "frontend": "Gradio 6.30.0",
            "biblioteca_python": "goradio",
            "calidad": int(quality or 75),
            "formato": fmt,
            "palabras": count,
            "imagen_procesada": bool(result_image),
            "audio_recibido": bool(audio_path),
            "video_recibido": bool(video_path),
            "archivo_recibido": bool(file_path),
        },
    )


demo = gr.Interface(
    fn=studio,
    inputs=[
        gr.Image(label="🖼️ Imagen a comprimir"),
        gr.Slider(label="Calidad de imagen (JPEG/WebP)", minimum=10, maximum=95, step=5, value=75),
        gr.Slider(label="Tamaño máximo del lado (px)", minimum=320, maximum=2048, step=64, value=1280),
        gr.Dropdown(["WebP", "JPEG", "PNG"], label="Formato de salida", value="WebP"),
        gr.Checkbox(label="Eliminar metadatos EXIF", value=True),
        gr.Textbox(label="📝 Texto de prueba", section="Otras herramientas · texto, audio y archivos", placeholder="Escribe algo para pasarlo a mayúsculas"),
        gr.Number(label="🔢 Número que se duplicará", section="Otras herramientas · texto, audio y archivos", value=21),
        gr.Audio(label="🎵 Audio: subir o grabar", section="Otras herramientas · texto, audio y archivos"),
        gr.Video(label="🎬 Video: subir", section="Otras herramientas · texto, audio y archivos"),
        gr.File(label="📁 Archivo: subida/descarga", section="Otras herramientas · texto, audio y archivos"),
    ],
    outputs=[
        gr.Image(label="🖼️ Imagen comprimida"),
        gr.Textbox(label="Reporte de compresión"),
        gr.Textbox(label="Texto transformado", section="Resultados adicionales"),
        gr.Number(label="Palabras", section="Resultados adicionales"),
        gr.Number(label="Número × 2", section="Resultados adicionales"),
        gr.Audio(label="Audio devuelto", section="Resultados adicionales"),
        gr.Video(label="Video devuelto", section="Resultados adicionales"),
        gr.File(label="Archivo devuelto", section="Resultados adicionales"),
        gr.JSON(label="Datos de la ejecución", section="Resultados adicionales"),
    ],
    title="Goradio Studio · Compresor de imágenes",
    description=(
        "Comprime imágenes a WebP, JPEG o PNG y compara tamaños. "
        "Ajusta la calidad, la resolución y los metadatos. "
        "Abre «Otras herramientas» para probar texto, números, audio, video y archivos."
    ),
)


if __name__ == "__main__":
    port = int(os.environ.get("GORADIO_PORT", "7898"))
    providers = [
        provider.strip()
        for provider in os.environ.get("GORADIO_SHARE", "cloudflare,localhostrun").split(",")
        if provider.strip()
    ]
    demo.launch(server_port=port, share=providers, block=True)
