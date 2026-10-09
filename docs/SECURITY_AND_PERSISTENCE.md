# Goradio 0.3.1 — Login y persistencia

## Acceso rápido a demo local (solo pruebas)

En `http://127.0.0.1:7916/login` se creó la cuenta temporal `admin` / `admin` para una prueba exclusivamente local. Para volver a configurarla ejecuta desde la raíz del proyecto `go run ./scripts/dev/seed_local_admin.go` con el servidor de la demo detenido, y luego inicia `python examples/secure_demo.py`. **No utilices esas credenciales con túneles o despliegues públicos.** El requisito de contraseñas seguras de la API Go permanece intacto.


Goradio es una biblioteca Go que sirve el frontend original Gradio y permite usar funciones tanto en **Go** como **Python**.

## Ejemplo Python con login

En PowerShell:

```powershell
cd C:\Users\Admin\Documents\GitHub\Goradio
conda activate 311
$env:GORADIO_ADMIN_PASSWORD = Read-Host "Contraseña nueva (mínimo 12 caracteres)"
python examples/secure_demo.py
```

Abrir http://127.0.0.1:7916/ . Usuario: `admin`, contraseña: la que configuraste. En el segundo inicio la base de datos conserva las cuentas y ya no es necesaria la variable de bootstrap.

**Advertencia**: para trabajar en producción no introduzcas secretos en scripts ni los publiques en Git. Nunca conectes un túnel público a una aplicación sin autenticación y medidas de seguridad apropiadas.

La base SQLite queda en `data/secure-demo.sqlite`; los archivos multimedia en `data/goradio-media-secure-demo/`. Esas carpetas se ignoran en Git.

Para iniciar una aplicación con tu propia función:

```python
import goradio as gr

app = gr.Interface(
    fn=lambda nombre: f"Hola {nombre}",
    inputs=gr.Textbox(label="Nombre"),
    outputs=gr.Textbox(label="Resultado"),
)
app.launch(
    security={
        "db_path": "./data/miapp.sqlite",
        "app_id": "miapp",
        "require_login": True,
        "allow_signup": False,
        "persist": True,
        "bootstrap_user": "admin",
    },
)
```

## Uso desde otro proyecto Go

Con el proyecto local como dependencia en otro módulo:

```go
// go.mod
module miempresa.com/miapp
go 1.24.0
require goradio v0.3.0
replace goradio => ../Goradio
```

Y desde código Go:

```go
app, err := goradio.New(cfg, handler, "./media")
if err != nil { /* tratar error */ }
err = app.EnableSecurity(goradio.SecurityConfig{
    DBPath:"./data/app.sqlite",
    AppID:"miapp",
    RequireLogin:true,
    PersistInputs:true,
    PersistOutputs:true,
})
if err != nil { /* tratar error */ }
// app.CreateUser(ctx, "admin", secret, "admin") una sola vez.
// app.Routes() permite montarlo en un servidor HTTP propio.
// app.Serve(ctx, "127.0.0.1", 7860, nil) arranca el servidor.
```

Consulta el ejemplo completo `examples/secure-go/main.go`. Se puede ejecutar desde el directorio raíz con `go run ./examples/secure-go`.

## Comportamiento de persistencia

- Entradas de texto, números, selectores y deslizadores se guardan por sesión mediante eventos nativos Gradio, sin Submit.
- La configuración que Go sirve al recargar ya incluye los últimos valores autorizados.
- Submit guarda también resultados; Clear borra el estado persistido.
- Archivos multimedia tienen propietario, dentro del almacenamiento privado. Las URL solo son accesibles con la sesión propietaria.
- El estado y las cuentas sobreviven reinicios del servidor al reutilizar el mismo archivo SQLite.
- El navegador mantiene una cookie de sesión HttpOnly; la contraseña nunca se guarda como texto plano ni en localStorage.
- La pestaña de invitado conserva su estado en el mismo navegador si `RequireLogin=false` y la persistencia está habilitada.

## Limitaciones de esta versión

- El módulo Go usa la ruta local `module goradio`: publicar para `go get` requiere confirmar primero la ruta de repositorio remoto y versionarlo bajo esa ruta.
- El sistema incluye login, registro opcional (configuración explícita) y logout, pero faltan recuperación de contraseña, administración visual de cuentas y 2FA.
- El almacenamiento SQLite requiere copias de seguridad, cuotas, mantenimiento y auditoría antes de usar información sensible o grandes volúmenes.
- La protección de archivos es por propietario; faltan controles avanzados de cuotas por usuario y retención.
- Para despliegues reales se requiere HTTPS, proxy de confianza, monitorización y revisión de seguridad adicional.
- La capa visual y parte de la API usan el frontend original Gradio 6.30.0; no se ha completado toda la compatibilidad Blocks.
