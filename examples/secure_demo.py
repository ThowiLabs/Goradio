"""Ejemplo de aplicación con login y formularios persistentes.

Primer arranque:
    $env:GORADIO_ADMIN_PASSWORD = Read-Host "Contraseña admin (mínimo 12 caracteres)"
    python examples/secure_demo.py

Elige una contraseña fuerte. El hash se guarda en SQLite, no en este archivo.
En reinicios posteriores no se requiere definir la contraseña de bootstrap
si el administrador ya existe.
"""
import os
from pathlib import Path
import goradio as gr

ROOT=Path(__file__).resolve().parents[1]
DB=ROOT/"data"/"secure-demo.sqlite"

def calcular(nombre,nota,numero):
    return f"Hola {nombre}: {nota}", float(numero or 0)*2

app=gr.Interface(
    fn=calcular,
    inputs=[
        gr.Textbox(label="Nombre", required=True),
        gr.Textbox(label="Mi borrador",placeholder="Se guarda incluso sin pulsar Submit"),
        gr.Number(label="Número",value=5),
    ],
    outputs=[
        gr.Textbox(label="Resultado"),
        gr.Number(label="Doble"),
    ],
    title="Goradio · Login y persistencia",
    description="Prueba el login, el autoguardado al escribir y la restauración al recargar o reiniciar.",
)

if __name__=="__main__":
    app.launch(
        server_port=int(os.getenv("GORADIO_PORT","7916")),
        share=False,
        security={
            "db_path":str(DB),
            "app_id":"secure-demo",
            "require_login":True,
            "allow_signup":False,
            "persist":True,
            "bootstrap_user":"admin",
        },
    )
