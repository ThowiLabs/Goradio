# Fecha
2026-10-10

# Objetivo
Corregir la pantalla "Cargando..." de Goradio que permanece como overlay y bloquea los clics, observada en YuE2 Dataset Studio y también en el ejemplo nativo Blocks.

# Decisiones tomadas
- Corregir el framework común Goradio, no añadir un segundo frontend en YuE2.
- Mantener el frontend Gradio 6.30.0 vendorizado intacto.
- Selector CSS local: ocultar únicamente el indicador de progreso global cuando el formulario ya contiene botones y dicho indicador permanece en el estado p.loading.
- Mantener disponibles los indicadores específicos de componentes y la comunicación de resultados mediante el protocolo de predicción.
- No modificar archivos no relacionados ni el proyecto local no rastreado examples/gradio-studio/.

# Arquitectura actual
gradio_frontend.go sirve HTML hospedador con estilo de compatibilidad acotado al tracker global de arranque.
gradio_compat_test.go valida que la protección se emita en el HTML.
El frontend y protocolos HTTP originales no cambian.

# Librerías usadas
Sin dependencias adicionales. Go 1.24+, frontend oficial Gradio 6.30.0 integrado.

# Archivos importantes modificados
gradio_frontend.go, gradio_compat_test.go, contexto/05-carga-bloqueante.md y tareas/completado-01-solucionar-cargando.md.

# Problemas encontrados
El status-tracker global ocupa todo el contenedor, con z-index 30 y pointer-events auto, y continúa mostrando p.loading = "Cargando..." incluso cuando los campos y botones están renderizados.
Un usuario no puede clicar porque elementFromPoint devuelve el tracker. El fenómeno ocurre en ejemplos básicos de Goradio, no solo en YuE2. El primer selector basado en .form no encontró el layout y fue ajustado a .main:has(button).

# Soluciones implementadas
Se incluye CSS específico en gradio_frontend.go, condicionado a que la vista esté montada y a que el tracker global continúe en el estado "loading"; el CSS no modifica el estado interno ni los trackers de componentes.
Pruebas: go test ./..., go vet ./...; Chrome real en YuE2 Dataset Studio: texto "Cargando..." ya no aparece visible, elementFromPoint sobre "Comprobar repositorio" devuelve el botón, clic real del mouse cambia el estado al mensaje de validación.

# Pendientes
Revisar la lógica de estados upstream o actualizar bundle Gradio cuando exista una corrección que permita eliminar el CSS de compatibilidad. No se cambió el modelo musical ni el cliente de Hugging Face.

# Próximos pasos
Mantener una regresión de navegador que compruebe carga sin overlay y clic real en un botón, actualizar la solución si cambia el DOM del frontend.
