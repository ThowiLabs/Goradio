# Fecha
2026-10-09

# Objetivo
Migrar Goradio del frontend propio a componentes originales del frontend oficial de Gradio, conservando Go como servidor y Python como API opcional.

# Decisiones tomadas
- Frontend vendorizado desde el wheel oficial Gradio 6.30.0; fuente Git examinada en commit 2cdd7727442fa2a8560992e1d8e164e18e1527aa.
- Licencia Apache-2.0 conservada en third_party/gradio/LICENSE.
- 973 recursos del frontend empaquetados como un solo archivo third_party/gradio/frontend.zip (aproximadamente 17 MB) para reducir ruido en Git.
- Go los sirve con archive/zip y embed sin dependencias de frontend durante la compilación.
- HTML hospedador reducido genera window.gradio_config compatible y carga el JS/CSS original.
- / clásico de Gradio y /classic/ para la UI anterior. No modificar fuente vendorizado durante runtime.
- Adaptador REST nativo en Go sin instalar Gradio como dependencia de producción.
- Se añadió panel aislado para mostrar URLs de Cloudflare, localhost.run, Pinggy, localtunnel y Gradio.live experimental.
- Se implementó compatibilidad de eventos por fn_index=0 (Submit) y fn_index=1 (Clear), usando endpoint /gradio_api/run/predict.
- Archivo multimedia de usuario se restringe a directorio administrado, extensión permitida y nombre aleatorio.

# Arquitectura actual
- Go: goradio.go valida, registra endpoints, ejecuta funciones; callback.go conecta Go/Python.
- Go: gradio_frontend.go transforma Config a componentes, layout, dependencias y sirve recursos oficiales.
- Go: gradio_api.go traduce predicciones, uploads, FileData, eventos SSE y descarga.
- Navegador: UI Svelte original en zip; share mini-panel externo aislado.
- Python: paquete goradio define Interface y componentes; callback local protegido con token.

# Librerías usadas
Go 1.26.4 estándar. Python 3.11 en Conda 311. Frontend oficial Gradio 6.30.0 (Svelte compilado), sin compilación pnpm obligatoria. Node 26 para auditoría Chrome CDP (sin paquetes externos). Gradio Python no es dependencia de runtime.

# Archivos importantes modificados
gradio_frontend.go, gradio_api.go, gradio_compat_test.go, goradio.go, README.md, CHANGELOG.md, python/pyproject.toml, static/goradio-share.js, static/goradio-share.css, scripts/dev/stop-goradio.ps1, scripts/dev/cdp-audit.mjs, scripts/dev/smoke-browser.mjs, third_party/gradio/frontend.zip, third_party/gradio/LICENSE, third_party/gradio/SOURCE.md, examples/media_demo.py.

# Problemas encontrados
- Config no incluía IDs de componentes de página => frontend en pantalla 'Cargando...'; corregido.
- window.gradio_api_info={} inválido => cliente buscaba named_endpoints; omitido para que consulte /gradio_api/info.
- Propiedad Image.buttons ausente/nula y Audio.waveform_options ausente => errores JS; corregidas.
- Gradio Clear llama /gradio_api/run/predict con fn_index 1 => adaptador añadido.
- /gradio_api/upload_progress todavía no está implementado; frontend sigue subiendo archivos correctamente pero muestra 404/log y no hay seguimiento real de progreso.
- Parte del protocolo oficial no está implementada: streaming, queue, Blocks dinámico, estado/sesiones, autenticación equivalente a la oficial.
- El entorno 311 emite aviso preexistente de pip_system_certs.
- El enlace público de localhost.run anterior devolvió 403 y no se debe considerar operativo aunque aparezca en la salida.

# Soluciones implementadas
- Bundle original vendorizado con licencia; servidor de rutas /assets, /static, /config, /gradio_api/info.
- Adaptación de config, layout, componentes, evento y ejecución.
- Endpoints /gradio_api/run/predict, /gradio_api/call/predict, /gradio_api/call/predict/{id}, /gradio_api/upload, /gradio_api/file={id}.
- Asignación de FileData para resultados y validación de directorios y nombres.
- Botón de compartir URLs; script Windows para cerrar únicamente procesos Goradio dentro del proyecto.
- Validación automática en Chrome real mediante CDP (sin paquetes Node externos).

# Estado de tests
Go: go test ./... pasó, gradio_compat_test.go cubre frontend/config, predicción, evento, subida archivo y traversal, Clear.
Go: go vet ./... pasó.
Browser Chrome: UI original carga texto+número, Submit transforma "hola prueba" a "HOLA PRUEBA"; Clear resetea ambos; Python env 311 devuelve "Hola, hola prueba! Total: 6".
Browser Chrome: Imagen, Audio y Video originales se renderizan sin errores tras corregir props.
Browser Chrome: carga de PNG llama /gradio_api/upload y /gradio_api/run/predict con 200; verificar render final tras corregir botones.
Pendiente auditoría visual móvil/revisión de temas más exhaustiva.

# Estado de seguridad
Assets oficiales Apache-2.0. Upload con límite, nombres aleatorios, allowlist de extensiones, rutas confinadas. Scripts originales requieren seguimiento de actualizaciones y advisories. Exponer vía túneles abre ejecución remota de funciones de usuario: usar auth y no alojar datos sensibles. Falta rate limiting, cuotas y manejo avanzado.

# Pendientes
1. Completar progreso de archivos multipart de forma real.
2. Validar imagen/audio/video binario y regresiones visuales después de cada cambio.
3. Amplificar propiedades específicas de 40+ componentes, layouts, eventos y sesiones.
4. Tema oficial parametrizable y Gradio Blocks/Row/Column desde Go/Python.
5. Streaming/queue/cancelación de jobs, API cliente Gradio, audio/video en tiempo real.
6. Pruebas de túneles al habilitar proveedores, manejo de autenticación y HTTPS estable.
7. Auditar licencias de recursos transitivos del bundle y seguridad de dependencias de frontend.
8. Definir política de versiones/actualizaciones del bundle original.

# Próximos pasos
Cerrar esta versión con pruebas, commit en español y servidores de prueba detenidos. Planear implementación de Blocks y validaciones por componente.
