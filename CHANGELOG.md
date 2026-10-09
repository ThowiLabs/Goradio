# Cambios

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
