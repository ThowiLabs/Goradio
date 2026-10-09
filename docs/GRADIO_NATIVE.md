# Goradio — Gradio Live completamente nativo en Go

Fecha: 2026-10-09. Implementación en `gradio_native.go`, `tunnels.go`, `internal/frpmsg/`.

## Qué cambia

`gradio` y `gradio_native` usan un cliente FRP programado en Go. **No descargan ni ejecutan ningún binario frpc.** El protocolo sigue requiriendo el servicio externo oficial de Gradio y conexión a Internet, ya que `https://xxxxx.gradio.live` es una URL temporal.

El cliente nativo es el único modo de conexión de Gradio: no existe proveedor alternativo que descargue ni ejecute `frpc`.

**Supervisión:** cada 20 segundos se comprueba que el enlace público siga respondiendo, no solo que la conexión local FRP esté abierta. Tras cinco comprobaciones fallidas, se invalida el enlace y se solicita automáticamente otro. Durante la reconexión se puede consultar `/api/tunnels` para obtener la nueva dirección.

El cliente Go implementa las funciones esenciales del protocolo compatible con el fork FRP del servicio de Hugging Face: TLS con certificado del servidor de la API oficial, prefijo TLS FRP, yamux, login, hash de nombre de proxy, canal de control cifrado, proxy HTTP, conexión de trabajo, cifrado, compresión Snappy, ping/pong, interrupción segura y reintentos con backoff. El evento remoto `Kill` no ejecuta nunca `os.Exit`.

## Uso directo en Go

```go
app, err := goradio.New(config, handler, "./media")
if err != nil { panic(err) }
// Arranca HTTP local y publica automáticamente una URL gradio.live.
if err := app.Serve(ctx, "127.0.0.1", 7860, []string{"gradio"}); err != nil {
    panic(err)
}
```

En otro servidor HTTP no basado en App:

```go
manager := goradio.NewTunnelManager()
manager.Start(ctx, 7860, []string{"gradio"})
link := manager.WaitForURL("gradio", 45*time.Second)
if link == "" {
    // Revisar manager.Snapshot(), no inventar URL.
}
```

Hay que arrancar primero el servidor local. El contexto controla el ciclo de vida del enlace.

## Uso desde Python

```python
import goradio as gr
demo = gr.Interface(fn=lambda text: text.upper(),
                    inputs=gr.Textbox(),outputs=gr.Textbox())
demo.launch(share=["gradio"])
```

Para varios proveedores en paralelo: `share=["gradio","cloudflare","pinggy"]`. El alias `gradio_native` es equivalente a `gradio`.

CLI Go: `--share gradio` sin ningún cliente externo.

### Supervisión y cambios de URL

El cliente verifica el endpoint público antes de anunciar `online`. Si la verificación inicial falla, **cancela ese túnel e intenta obtener otro**, evitando sesiones abiertas pero inservibles. Posteriormente consulta la salud pública cada 20 segundos y tolera hasta cuatro errores consecutivos para no cambiar de dirección ante fallos transitorios. Después de cinco fallos seguidos renegocia un túnel.

La URL `gradio.live` **puede cambiar** al recuperar la conexión: el cliente no controla el hostname del proveedor y no puede mantener activa la URL anterior. Consulta `GET /api/tunnels` para obtener la dirección vigente con `status: online`. La instancia Go local puede continuar funcionando aunque el enlace externo haya expirado. Mantén vivo el proceso Go y el contexto de la llamada a `Serve` o `TunnelManager.Start`.

Para medir estabilidad:

```powershell
python scripts/dev/monitor_gradio_native.py --port 7860 --seconds 300 --interval 15 --output native-test.json
```

Este monitor registra la salud local, pública, cambios de URL y predicciones. Las pruebas de corta duración no prueban una disponibilidad continua de 72 horas. Para una URL estable de producción, utiliza alojamiento y dominio propios.

## Dependencias y derechos

La implementación FRP nativa es **código Go estáticamente compilado**:
- `github.com/hashicorp/yamux` para multiplexación de conexiones
- `github.com/fatedier/golib` para capas de cifrado y compresión
- `internal/frpmsg` conserva estructuras y serialización FRP necesarias, derivadas del fork de Hugging Face y del proyecto fatedier/frp. Copyright/licencia original Apache 2.0 conservados en los archivos y en `internal/frpmsg/LICENSE`.

No se requiere `go mod replace` para el fork FRP al importar Goradio desde otro módulo Go.

## Pruebas realizadas

1. Negociación real HTTPS/TLS con `api.gradio.app/v3/tunnel-request` y servidor FRP de Gradio.
2. Comprobación de enlace `gradio.live` antes de mostrar el estado `online`.
3. Peticiones reales `GET /api/health`, `GET /api/config`, páginas multipágina.
4. `POST /api/predict` y `POST /gradio_api/run/predict` correctamente procesados.
5. Subida y posterior descarga de archivo a través del túnel público.
6. Prueba de trabajo FRP cifrado y comprimido con servidor local simulado (TLS + yamux + Snappy + HTTP).
7. Pruebas negativas: URLs no oficiales, nombres de proxy inválidos, certificados CA inválidos, orden Kill, denegación de proxy, respuesta healthcheck falsa y cancelación.
8. `go test -race ./...`, `go vet ./...` y prueba desde módulo externo Go.
9. Comprobación de no creación de carpeta de ejecutables FRP al utilizar `share=["gradio"]`.
10. Pruebas de concurrencia: se observaron sesiones nativas con respuestas transitorias `No interface is running` en algunas rondas iniciales (17/24), y otras con cuatro rondas consecutivas correctas (96/96). Se añadieron más conexiones de trabajo y supervisión HTTP para renovación de enlaces.

Para repetir la prueba externa de concurrencia:
```powershell
python scripts/dev/stress_gradio_native.py https://TU-ENLACE.gradio.live
```

## Limitaciones en producción

Gradio Live es un **servicio externo temporal**, no un hosting permanente. Su disponibilidad y límites no están bajo control de Goradio y una URL puede dejar de funcionar cuando se desconecta el servidor o falla la conectividad. Goradio supervisa la conexión pública y solicita otra URL cuando detecta una caída prolongada. Para sitios de producción que requieran una dirección estable conviene utilizar hosting y dominio propios.

Cuando se comparte una aplicación se vuelve accesible por Internet. Activa autenticación y HTTPS para contenido privado. Una contraseña como `admin/admin` no es adecuada para una URL pública. Las pruebas descritas no representan una auditoría externa de seguridad ni una garantía de disponibilidad del servidor de Gradio.

El protocolo puede cambiar con futuras versiones de Gradio. Al actualizar, realizar pruebas reales y revisar compatibilidad antes de desplegar.
