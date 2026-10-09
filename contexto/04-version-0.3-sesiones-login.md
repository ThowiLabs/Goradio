# Goradio 0.3.0 — Implementación efectiva

Fecha: 2026-10-09

## Estado entregado

Esta versión añade autenticación y persistencia a la aplicación Go y a su envoltorio Python.

- SQLite mediante modernc.org/sqlite, WAL y migración de tablas al habilitar seguridad
- Usuarios con hash de contraseñas Argon2id y roles admin/user
- Login, logout, registro opcional, cookies HttpOnly y SameSite Lax, vencimiento configurable
- Protección de operaciones privadas y rechazo de acceso anónimo cuando RequireLogin=true
- Protección CSRF del API de estado y logout, revisión Origin y limitador de intentos básico
- Estado persistente por app_id e identidad; borrador se guarda mediante eventos input/change originales Gradio
- Restauración desde la configuración de componentes tras recargar
- Resultado de Submit, Clear y eventos de auto guardado actualizan el estado
- Acceso a archivos controlado por identidad con registro SQLite de propietarios; multimedia en almacenamiento durable si DBPath se usa desde Python
- API Go: EnableSecurity, CreateUser, UserExists, Routes, Serve, IdentityFromContext
- API Python: Interface.launch(security={db_path, app_id, require_login, allow_signup, persist, bootstrap_user})
- Ejemplo Go externo: examples/external-go-app (módulo autónomo con replace a la fuente local)
- Ejemplos nuevos de login: examples/secure_demo.py y examples/secure-go
- Verificación Chrome: texto autoguardado sin Submit, recarga y recuperación exacta
- Test Python→Go con login SQLite: scripts/dev/smoke_security_python.py
- Versión Go/Python 0.3.0 y etiqueta Git correspondiente

## Límites

- NO está implementada la paridad completa con la API oficial Gradio, Blocks avanzado ni 2FA.
- El módulo local se llama goradio. Falta definir la URL canónica del repositorio y publicar versiones descargables en go get/pip.
- Faltan UI de administración de usuarios, recuperación de contraseña, cuotas de archivos, gestión de backups y auditoría de seguridad formal.
- Persistencia del estado no sustituye almacenamiento duradero ni migración de formularios cuando cambia el esquema.
- El sistema es una base experimental, no una certificación de seguridad para datos sensibles ni un SaaS listo para producción.
- Login HTTP Basic legado sigue disponible cuando no se activa EnableSecurity, por compatibilidad.
- No crear usuarios con contraseñas en texto plano dentro del repositorio.

## Comandos de validación

```powershell
go test ./...
go vet ./...
cd examples/external-go-app; go test ./...
cd ../..
C:\Users\Admin\.conda\envs\311\python.exe -m unittest discover -s python/tests -v
```

Una guía de uso detallada se encuentra en docs/SECURITY_AND_PERSISTENCE.md.

## Continuación

0.3.1: control de cuotas, límite de sesión y mejor gestión de archivos, login configurable.
0.4: componentes tipados, Blocks avanzado, sistemas de roles y administración de cuentas.
0.5: streaming, colas de tareas, cancelación, autenticación avanzada.
1.0: release de Go con import canónico y distribución binaria/ruedas Python por plataforma.
