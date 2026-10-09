# Goradio — Hoja de ruta: librería Go, usuarios y persistencia

Fecha: 2026-10-09
Proyecto: C:\Users\Admin\Documents\GitHub\Goradio

## Solicitud confirmada del usuario

Goradio debe ser un framework independiente, escrito principalmente en Go, con frontend original de Gradio, que pueda utilizarse desde Python y **también importarse desde cualquier otro proyecto Go**.

**Requisitos esenciales (no olvidar):**
1. Sistema de login real: usuarios, contraseñas seguras, logout y configuración de acceso público/privado.
2. Sesiones persistentes entre recargas, pestañas y reinicios del servidor según la configuración.
3. Persistencia de valores del formulario y resultados al recargar la página. Lo escrito o seleccionado debe restaurarse incluso si no se pulsó Submit.
4. Datos aislados por usuario, sesión y aplicación; jamás mostrar a otro usuario los archivos o resultados de una sesión ajena.
5. Conservar la opción de crear URL públicas, pero NO confundir una URL secreta con autenticación.
6. Configuración extensible: tanto Go como Python deben tener API declarativa e integrarse como Handler HTTP cuando sea necesario.

## Estado real comprobado

- Versión experimental actual 0.2.x; frontend original Gradio 6.30.0 incorporado.
- Go: paquete raiz `goradio` con `New(Config, Handler, mediaDir)`, `App.Routes() http.Handler` y `App.Serve(...)` para ejecutar de forma independiente. Un ejemplo en un **segundo módulo Go** está en `examples/external-go-app`.
- Go.mod actual: `module goradio`; NO tiene remoto Git configurado. Para importar con `go get` desde GitHub será necesario definir el nombre público correcto del módulo después de establecer la URL del repositorio y el propietario. No inventar namespace ni registrar un paquete que no existe.
- Python: `import goradio`, `Interface`, controles básicos.
- Auth actual: HTTP Basic con un solo usuario/clave mediante variables `GORADIO_AUTH_*`. No existe login HTML real, base de usuarios, roles ni sesiones persistentes.
- Estado: las entradas viven en frontend durante la sesión de navegación; actualmente NO se guardan en DB ni se restauran tras recargar.
- Multimedia: imágenes/audio/video y progreso parcial; archivos suelen ser temporales en las demos Python, lo que impide persistirlos a largo plazo sin nuevo almacenamiento.
- Se han realizado pruebas del frontend y del reproductor de audio original en Chrome.
- Puede haber cambios de otro agente; consultar Git antes de editar y limitar mutaciones al directorio Goradio.

## Diseño propuesto: responsabilidad y seguridad

### Paquete Go reutilizable (fase 0.3)

- Mantener `goradio` como biblioteca importable con `App.Routes() http.Handler` y `App.Serve()`; separar framework del ejecutable en `cmd/goradio`.
- Estabilizar constructores tipados, validación, contexto de petición y hooks de ejecución.
- Permitir montaje bajo subruta configurable (`BasePath`), ya que las URLs de assets del frontend actual son absolutas.
- Documentar un módulo anidado de consumidor independiente y un ejemplo de `http.ServeMux`.
- Para publicación: cuando exista un remoto confirmado, migrar `module goradio` a ruta canónica, ajustar imports, versionar y verificar `go get`.

### Usuarios y login (fase 0.4)

- Contraseñas con Argon2id (hash por usuario, nunca texto plano).
- SQLite por defecto para usuarios y sesiones; almacenamiento sustituible (p. ej. PostgreSQL).
- Cookies de sesión aleatorias con `HttpOnly`, `Secure` bajo HTTPS, `SameSite=Lax` o `Strict`; renovación/rotación, expiración e invalidación con logout.
- Formularios login/registro según configuración; roles visitante/usuario/admin, protecciones de fuerza bruta y CSRF.
- Cada operación `predict`, upload, descarga, estado y evento SSE debe evaluar identidad/permisos.
- Política explícita para invitados, registro abierto, recuperación de contraseña y administración de usuarios.
- No guardar contraseñas, sesiones o secretos en localStorage.

### Estado persistente del formulario (fase 0.5)

- Al cargar: resolver identidad + app_id y GET del último estado permitido.
- Guardado automático al escribir/cambiar valor con debounce (aprox. 500 ms), además de guardado tras Submit.
- Recuperar textbox, slider, checkbox, dropdown, number, estados de acordeones, y resultados de salida según política.
- Tabla lógica `form_state(app_id, identity_id, form_version, component_id, value_json, updated_at)`; un mismo usuario puede tener aplicaciones y estados distintos.
- Versionar esquema del formulario: si cambia, migrar o descartar solo campos incompatibles.
- Botón Clear debe borrar también el estado persistido si el usuario confirma/restaura.
- Persistencia configurable: `none`, `browser`, `session`, `user`. El modo `user` guarda datos en servidor para recuperarlos en otro dispositivo tras login.
- Proteger ante cambios simultáneos mediante versionado/updated_at, no mezclar valores de usuarios.
- Multimedia persistente requiere mover archivos a almacenamiento durable, cuota por usuario, comprobaciones de propiedad, expiración, borrado seguro y límites de espacio.
- Prueba de aceptación: escribir sin Submit → actualizar → valores iguales; cerrar navegador y volver → mismos valores según modalidad; reiniciar Go → sigue igual; login usuario B → NO ve valores de A.

### Funciones avanzadas (0.6+)

- Layouts `Blocks`, `Row`, `Column`, `Tab`, `Accordion` con API Go/Python, múltiples eventos y dependencias.
- 40+ componentes, cambios reactivos, validaciones por campo, transformaciones de inputs, errores UI.
- Cola de trabajos, cancelación, progreso real, streaming SSE/WebSocket, tareas largas y ejecutores.
- Datos binarios, cámara/micrófono en tiempo real, transcodificación, almacenamiento seguro de medios.
- Tunelado HTTPS estable, configuración por entorno, límites, logs, healthchecks, métricas y deploy.
- Documentación GoDoc/Python, tests de integración, tests de navegador/móvil, API cliente.

## Orden de implementación recomendado

1. Verificar ejemplo de segundo módulo Go y acordar ruta futura de publicación.
2. Modelar identidad/sesión/almacenamiento como interfaces inyectables; crear migraciones SQLite.
3. Implementar login+logout, middleware, cookies, roles y permisos por ruta.
4. Autoguardado/restauración de componentes con GET/PUT y tests de reload/restart/multiusuario.
5. Persistencia segura de archivos multimedia.
6. Continuar Blocks, componentes avanzados y streaming.

## Criterios para considerar "terminado"

- `go test ./...` y prueba del módulo independiente pasan.
- Autenticación, logout y sesiones persisten reinicio, no solo recarga; datos privados no se comparten.
- Formulario recupera entradas no enviadas y resultados según configuración.
- Al reiniciar Go (y Python opcional), usuarios y estados siguen accesibles según política.
- El usuario puede `import` Goradio desde un segundo proyecto sin copiarlo; `go get` público se verificará tras configurar remoto.
- Las URL compartidas no permiten eludir las políticas de autenticación.

**Importante:** este archivo registra los requisitos y la propuesta. No implica que login, DB o persistencia estén implementados todavía.
