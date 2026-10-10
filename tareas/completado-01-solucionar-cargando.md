# Corregir carga permanente en Goradio
Estado: completado y verificado con Chrome (clic físico), go test y go vet
Fecha: 2026-10-10
Causa reproducida: status-tracker global de arranque permanece en `Cargando...` aunque los componentes estan montados; cubre toda la vista y captura clics.
Criterio: mantener el progreso de acciones, ocultar solo el aviso global obsoleto despues de montar componentes, pruebas Go y navegador con clic de raton, sin editar recursos del bundle.
