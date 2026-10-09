# Cambios

## 0.3.3 — 2026-10-09
- Túnel oficial `gradio.live` integrado nativamente en Go: `share=gradio` ya no descarga ni ejecuta `frpc`.
- Conexión TLS/FRP, yamux, cifrado, compresión, control y conexiones de trabajo sin proceso externo.
- Supervisión HTTP del enlace público, reconexión automática ante caída o expiración y URL siempre verificada.
- Alias `gradio_native` y `gradio` compatibles con la conexión nativa; se elimina el cliente descargable y su proveedor anterior.
- Dependencia FRP eliminada: estructuras de protocolo internas con licencia Apache 2.0, importación Go externa comprobada.
- Pruebas de protocolo simulado, TLS, errores, Kill seguro, subida/descarga, concurrencia, `go test -race` y librería Python.


## 0.3.2 — 2026-10-09
- Contraseñas de cuentas SQLite: mínimo 4 caracteres, incluyendo PIN de 4 dígitos; se rechazan vacías o solo espacios.
- Inicio de sesión/registro: validación de usuario y contraseña no vacíos, sin eludir login habilitado.
- Python HTTP Basic: `auth=None` o `auth=("", "")` desactiva Basic; configuración parcial rechazada; clave configurada mínimo 4.
- Go HTTP Basic: credenciales incompletas o clave menor a 4 devuelven error de configuración.
- Nuevas pruebas Go/Python y documentación de la regla.
- Corregido el reinicio de la biblioteca Python cuando la carpeta multimedia persistente ya existe.
- Selección opcional del ejecutable de Go mediante `GORADIO_BINARY`, para probar nuevas versiones sin cerrar otros servidores.


## 0.3.1 — 2026-10-09
- Corregida la demo de login: servidor local en `127.0.0.1:7916` iniciado y verificado.
- Añadido script local para configurar `admin/admin` únicamente en la demo de desarrollo, sin rebajar el mínimo de contraseñas de la librería.
- Añadida verificación HTTP automatizada del login local.


## 0.3.0 — 2026-10-09
- Middleware de login y logout; altas administrativas y registro opcional.
- Sesiones duraderas SQLite con contraseñas Argon2id, cookies HttpOnly, CSRF y rate limit básico.
- Autoguardado de entradas al escribir, persistencia de resultados y restauración al recargar.
- Control de acceso a archivos multimedia por propietario.
- Configuración de seguridad desde Go, Python y CLI; ejemplos independientes y pruebas de reinicio/multiusuario.


## 0.2.0 — 2026-10-09
- Integración del frontend original compilado de Gradio 6.30.0 con conservación de su licencia.
- Adaptador Go para configuración, predicción directa, eventos, subida y visualización multimedia.
- Panel de enlaces públicos independiente, interfaz anterior accesible por /classic/.
- Compatibilidad probada con navegador real y funciones Go/Python.
- Pruebas de seguridad y regresión para subida de archivos y eventos.
- Restaurado el reproductor de audio de Gradio con forma de onda (WaveSurfer) en entradas y salidas; añadido test de regresión para evitar volver al reproductor HTML nativo.

## 0.1.0 — 2026-10-09
- Proyecto Go y Python inicial, UI declarativa, componentes, validación y multimedia.
- Múltiples túneles independientes con estado y verificación remota.
- Bridge Python → servidor Go y componentes inspirados en Gradio.
- Autenticación HTTP básica opcional y pruebas automatizadas.
