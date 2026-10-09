# Cambios

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
