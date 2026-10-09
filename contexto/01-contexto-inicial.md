# Fecha
2026-10-09

# Objetivo
Construir Goradio, un framework ligero escrito en Go inspirado en Gradio, con API Python y capacidad de compartir la misma interfaz mediante múltiples URLs temporales. Priorizar metodología Ponytail, seguridad, pruebas y dependencias mínimas.

# Decisiones tomadas
- Go standard library + HTML/CSS/JS nativo.
- Python usa una capa de definición declarativa y un callback HTTP en loopback, con token aleatorio.
- UI desacoplada de las funciones.
- Los proveedores de URLs no son parte del handler ni de las validaciones.
- Los túneles requieren verificación HTTP de salud de Goradio.
- Gradio.live: integración opcional y experimental a través de una API interna de Gradio, sin compromiso de compatibilidad.
- Las rutas multimedia de salida fuera del directorio administrado están bloqueadas: usar goradio.media().
- La subida de archivos exige nombre impredecible, límite y revisión básica de MIME.
- Autenticación básica opcional usando variables de entorno, evitando secretos en parámetros CLI.

# Arquitectura actual
- goradio.go: configuración, componentes, HTTP, validaciones, multimedia.
- callback.go: handlers de ejemplo y puente Go → callback Python.
- tunnels.go: proveedores independientes, procesos y validación de URL.
- assets.go y static/: UI incorporada en binario.
- cmd/goradio/: CLI para ejecutar configuraciones JSON.
- python/goradio/: API Python y puente opcional con share de Gradio.
- examples/: muestras Go, JSON y Python.
- python/tests/, goradio_test.go: pruebas automáticas.

# Librerías usadas
Go standard library. Python standard library. Dependencia externa opcional: gradio para túnel .gradio.live. Dependencias externas de CLI opcionales: cloudflared, OpenSSH, localtunnel.

# Archivos importantes modificados
go.mod, .gitignore, goradio.go, callback.go, tunnels.go, assets.go, cmd/goradio/main.go, static/index.html, static/app.css, static/app.js, python/pyproject.toml, python/goradio/__init__.py, python/goradio/gradio_tunnel.py, examples/*, goradio_test.go, python/tests/test_goradio.py, README.md.

# Problemas encontrados
- El entorno Conda 311 contiene un problema anterior con pip_system_certs (ASN1 NOT_ENOUGH_DATA).
- Instalación pip editable requirió SETUPTOOLS_USE_DISTUTILS=stdlib por choque distutils/setuptools.
- Una URL impresa por localhost.run respondió HTTP 403; no se debe asumir que una URL capturada es operativa.
- Pinggy estuvo en estado starting durante una comprobación inicial; debe mostrar error por timeout si no se verifica.
- No se probó enlace .gradio.live sin instalar la librería externa Gradio.
- No se realizó validación visual con navegador; sí verificación HTTP y validación sintáctica JS.

# Soluciones implementadas
- Detección de dependencias externas por proveedor.
- Validación de salud remota y estado "online" solo tras prueba.
- Tests de validación, subida multimedia, auth, captura URL y servidor de salud.
- Lógica Python↔Go confirmada por petición real al endpoint predict.

# Estado de tests
Go: go test ./... OK; go test -race ./... OK; go vet ./... OK.
Python: 5 tests unittest OK.
Frontend: node --check static/app.js OK.
E2E: GET /api/config y POST /api/predict con función Python pasaron.
Cloudflare: GET remoto respondió 200.
Pendiente repetir verificación de otros proveedores con nueva lógica.

# Estado de seguridad
Autenticación básica opcional; body limitado en predict/upload; token para loopback; rutas multimedia identificadas aleatoriamente; evita directorios fuera del directorio de medios.
Pendientes: cuota total de disco, rate limiting, auditoría de uploads y autenticación más completa para producción.

# Pendientes
- Mejorar integridad de componentes y mostrar errores por campo.
- Implementar Blocks/Rows/Columns, eventos y sesiones.
- Añadir previews y captura de audio/video de cámara/micro.
- Streaming, procesamiento asíncrono, cancelación y cola de tareas.
- Túneles estables opcionales y políticas de autenticación.
- Limpiar almacenamiento temporal en Go nativo, límites por sesión.
- Verificación visual real en navegador.
- Reprobar Gradio.live con dependencia externa controlada.

# Próximos pasos
Cerrar versión 0.1.x con prueba de túneles, documentar interfaces y planear primer Blocks simplificado.
