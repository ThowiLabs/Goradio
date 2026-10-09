# Goradio Studio: compresor y laboratorio

Archivo principal: `examples/showcase.py`. Se ejecuta con Python 3.11 del entorno Conda `311` y Pillow. El framework Goradio (instalado con `pip install -e python`) arranca el servidor compilado en Go y sirve el frontend oficial de Gradio 6.30.0.

## Abrir desde la computadora Windows

```powershell
cd C:\Users\Admin\Documents\GitHub\Goradio
conda activate 311
python examples/showcase.py
```

URL local por defecto: http://127.0.0.1:7898/ . Los enlaces públicos se muestran en los logs y en «◈ Compartir» cuando se validan. Pueden caducar y no se guardan de forma permanente.

Para desactivar los enlaces públicos:

```powershell
$env:GORADIO_SHARE = ""
python examples/showcase.py
```

En la demo: sube una imagen y escoge calidad, formato y tamaño. Pulsa Submit para descargar/ver la imagen comprimida y el reporte de tamaños. Puedes subir WAV/MP3, MP4 y un archivo para ver cómo regresan. Prueba además la transformación de texto, conteo de palabras, duplicación de números y salida JSON.

## Código y estructura

```text
Goradio/
├── goradio.go                 servidor y validaciones Go
├── gradio_frontend.go         configuración para el frontend Gradio 6.30.0
├── gradio_api.go              API compatible, eventos y archivos
├── tunnels.go                 Cloudflare, localhost.run, Pinggy, etc.
├── python/
│   └── goradio/__init__.py    API pública para Python (Interface y componentes)
├── examples/
│   ├── showcase.py            demo principal y compresor Pillow
│   ├── media_demo.py          ejemplo de imagen/audio/video
│   ├── demo.py                ejemplo simple
│   └── hello-go/main.go       app escrita directamente en Go
└── third_party/gradio/        frontend oficial con atribución Apache 2.0
```

La función `studio(...)` de `showcase.py` define la lógica que se ejecuta cuando la interfaz recibe los datos. `gr.Interface(fn=studio, inputs=[...], outputs=[...])` declara controles y resultados. `demo.launch(...)` arranca el servidor Go y crea los enlaces configurados.

**Nota:** el compresor es una demo, no un servicio diseñado para producción ni archivos confidenciales. Aunque el frontend visual es el de Gradio, el contrato completo de Gradio todavía está en desarrollo.
