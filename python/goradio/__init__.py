"""Interfaces interactivas respaldadas por un servidor Go."""

from __future__ import annotations

import base64
import contextvars
import re
import hmac
import inspect
import hashlib
import json
import os
import secrets
import shutil
import subprocess
import tempfile
import threading
import time
from dataclasses import asdict, dataclass, field
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any, Callable
from urllib.error import URLError
from urllib.request import Request, urlopen

__all__ = [
    "Interface", "Dashboard", "Component", "Textbox", "Number", "Slider", "Checkbox",
    "Dropdown", "Image", "Audio", "Video", "File", "JSON", "Theme", "Markdown", "Dataframe", "Chatbot", "Plot", "Gallery", "update", "state_get", "state_set", "media",
]

_ACTIVE_SESSION: contextvars.ContextVar = contextvars.ContextVar("goradio_active_session", default=None)
_SESSION_PATTERN = re.compile(r"^[A-Za-z0-9_-]{8,128}$")


def state_get(key: str, default: Any = None) -> Any:
    """Retrieve a JSON-serializable item scoped to the current browser session."""
    current = _ACTIVE_SESSION.get()
    if current is None:
        raise RuntimeError("state_get requiere una llamada activa de Gradio")
    app, session = current
    if not key or len(key) > 128:
        raise ValueError("Clave de estado inválida")
    with app._session_lock:
        entry = app._session_state.get(session)
        if entry is None or time.monotonic() - entry["updated"] > 1800:
            return default
        raw = entry["items"].get(key)
    return json.loads(raw) if raw is not None else default


def state_set(key: str, value: Any) -> None:
    """Store a value for the current session (64 KiB/item, 30-minute TTL)."""
    current = _ACTIVE_SESSION.get()
    if current is None:
        raise RuntimeError("state_set requiere una llamada activa de Gradio")
    app, session = current
    if not key or len(key) > 128:
        raise ValueError("Clave de estado inválida")
    raw = json.dumps(value, ensure_ascii=False)
    if len(raw.encode("utf-8")) > 65536:
        raise ValueError("Estado excede 64 KiB")
    now = time.monotonic()
    with app._session_lock:
        for name, entry in list(app._session_state.items()):
            if now - entry["updated"] > 1800:
                del app._session_state[name]
        if session not in app._session_state:
            if len(app._session_state) >= 1024:
                raise RuntimeError("Demasiadas sesiones")
            app._session_state[session] = {"updated": now, "items": {}}
        entry = app._session_state[session]
        if key not in entry["items"] and len(entry["items"]) >= 64:
            raise ValueError("Demasiadas claves de estado")
        entry["items"][key] = raw
        entry["updated"] = now


_MEDIA_ROOT: Path | None = None
_TYPES = {"textbox", "number", "slider", "checkbox", "dropdown",
          "image", "audio", "video", "file", "json", "markdown", "dataframe", "chatbot", "plot", "gallery"}


@dataclass
class Theme:
    """Colores CSS del tema original de Gradio (sin editor en la interfaz).

    Uso: gr.Theme(primary_color="#f97316", secondary_color="#3b82f6")
    """

    primary_color: str = ""
    secondary_color: str = ""

    def __post_init__(self):
        import re
        for name in ("primary_color", "secondary_color"):
            value = getattr(self, name)
            if value and not re.fullmatch(r"#[0-9a-fA-F]{6}", value):
                raise ValueError(f"theme.{name} debe tener formato #RRGGBB")

@dataclass
class Component:
    type: str
    label: str = ""
    section: str = ""
    placeholder: str = ""
    required: bool = False
    min: float | None = None
    max: float | None = None
    step: float | None = None
    value: Any = None
    choices: list[str] = field(default_factory=list)
    pattern: str = ""
    min_length: int | None = None
    max_length: int | None = None
    headers: list[str] = field(default_factory=list)
    datatype: list[str] = field(default_factory=list)

    def __post_init__(self) -> None:
        if self.type not in _TYPES:
            raise ValueError(f"Componente desconocido: {self.type}")
        if self.min is not None and self.max is not None and self.min > self.max:
            raise ValueError("min no puede superar max")
        if (self.min_length is not None and self.min_length < 0 or
            self.max_length is not None and self.max_length < 0 or
            self.min_length is not None and self.max_length is not None and self.min_length > self.max_length):
            raise ValueError("Longitud de texto inválida")
        if self.pattern:
            import re
            if len(self.pattern) > 256:
                raise ValueError("Patrón demasiado largo")
            try:
                re.compile(self.pattern)
            except re.error as exc:
                raise ValueError("Patrón inválido") from exc

    def config(self) -> dict:
        return {k: v for k, v in asdict(self).items() if v is not None}


def Textbox(label="Texto", **kwargs): return Component("textbox", label, **kwargs)
def Number(label="Número", **kwargs): return Component("number", label, **kwargs)
def Slider(label="Control", minimum=0, maximum=100, step=1, value=None, **kwargs):
    return Component("slider", label, min=minimum, max=maximum, step=step,
                     value=minimum if value is None else value, **kwargs)
def Checkbox(label="Selección", **kwargs): return Component("checkbox", label, **kwargs)
def Dropdown(choices, label="Opciones", **kwargs):
    return Component("dropdown", label, choices=list(choices), **kwargs)
def Image(label="Imagen", **kwargs): return Component("image", label, **kwargs)
def Audio(label="Audio", **kwargs): return Component("audio", label, **kwargs)
def Video(label="Video", **kwargs): return Component("video", label, **kwargs)
def File(label="Archivo", **kwargs): return Component("file", label, **kwargs)
def JSON(label="JSON", **kwargs): return Component("json", label, **kwargs)
def Markdown(value=None, label="Markdown", **kwargs): return Component("markdown", label, value=value, **kwargs)
def Dataframe(value=None, label="Tabla", headers=None, datatype=None, **kwargs):
    return Component("dataframe",label,value=value,headers=list(headers or []),datatype=list(datatype or []),**kwargs)
def Chatbot(value=None, label="Conversación", **kwargs): return Component("chatbot", label, value=value, **kwargs)
def Plot(value=None, label="Gráfico", **kwargs): return Component("plot", label, value=value, **kwargs)
def Gallery(value=None, label="Galería", **kwargs): return Component("gallery", label, value=value, **kwargs)


_UNSET = object()

def update(value: Any = _UNSET, *, visible: bool | None = None,
           interactive: bool | None = None) -> dict:
    """Actualiza propiedades de un componente, como gr.update()."""
    patch = {"__type__": "update"}
    if value is not _UNSET:
        patch["value"] = value
    if visible is not None:
        patch["visible"] = bool(visible)
    if interactive is not None:
        patch["interactive"] = bool(interactive)
    return patch


def media(path: str | Path) -> str:
    """Registra explícitamente un archivo de salida para servirlo desde Goradio.

    Solamente llamar con rutas de confianza. Nunca con rutas arbitrarias enviadas
    directamente por usuarios remotos.
    """
    if _MEDIA_ROOT is None:
        raise RuntimeError("Inicia una Interface antes de llamar media()")
    src = Path(path).resolve(strict=True)
    if not src.is_file():
        raise ValueError("Se requiere un archivo")
    suffix = src.suffix.lower()
    if len(suffix) > 11 or not suffix[1:].isalnum():
        raise ValueError("Extensión de archivo no permitida")
    dest = _MEDIA_ROOT / (secrets.token_hex(16) + suffix)
    shutil.copyfile(src, dest)
    return str(dest)


def _components(value: Any) -> list[Component]:
    if isinstance(value, (str, Component)):
        value = [value]
    if not isinstance(value, (list, tuple)):
        raise TypeError("inputs/outputs deben ser componentes o listas")
    result = []
    for item in value:
        result.append(Component(item) if isinstance(item, str) else item)
    if any(not isinstance(item, Component) for item in result):
        raise TypeError("Componente inválido")
    return result


def _normalize_auth(auth):
    """Valida HTTP Basic: sin credenciales no se habilita; una sola es error."""
    if auth is None:
        return None
    if not isinstance(auth, (tuple, list)) or len(auth) != 2 or any(not isinstance(v, str) for v in auth):
        raise ValueError("auth requiere (usuario, contraseña)")
    user, password = auth
    if not user.strip() and not password.strip():
        return None
    if not user.strip() or not password.strip():
        raise ValueError("usuario y contraseña deben estar completos; usa auth=None para no aplicar login")
    if len(password) < 4:
        raise ValueError("la contraseña debe contener al menos 4 caracteres")
    return (user, password)


class Interface:
    def __init__(self, fn: Callable, inputs: Any, outputs: Any,
                 title: str = "Goradio", description: str = "", theme: Theme | None = None,
                 live: bool = False, every: float = 0.0, load: bool = False,
                 validate: Callable | None = None) -> None:
        if not callable(fn):
            raise TypeError("fn debe ser invocable")
        self.fn = fn
        self.inputs = _components(inputs)
        self.outputs = _components(outputs)
        if not self.outputs:
            raise ValueError("Se requiere una salida")
        self.title = title
        self.description = description
        if theme is not None and not isinstance(theme, Theme):
            raise TypeError("theme debe ser gr.Theme o None")
        self.theme = theme
        import math
        if every != 0 and (not math.isfinite(every) or not .25 <= every <= 3600):
            raise ValueError("every debe estar entre 0.25 y 3600 segundos")
        self.live = bool(live)
        self.load = bool(load)
        self.every = float(every)
        if validate is not None and not callable(validate):
            raise TypeError("validate debe ser una función")
        self.validator = validate
        self._chains: list[Callable] = []
        self.streaming = inspect.isgeneratorfunction(fn)
        self._session_lock = threading.Lock()
        self._session_state: dict = {}
        self._process: subprocess.Popen | None = None
        self._server: ThreadingHTTPServer | None = None
        self._temporary: tempfile.TemporaryDirectory | None = None
        self.local_url: str | None = None
        self._dashboard_pages: dict | None = None
        self._dashboard_definitions: list[dict] = []

    def then(self, fn: Callable) -> "Interface":
        """Ejecuta un callback tras el anterior, usando sus salidas como entradas."""
        if not callable(fn):
            raise TypeError("then requiere una función")
        if len(self._chains) >= 16:
            raise ValueError("Máximo 16 funciones encadenadas")
        self._chains.append(fn)
        return self

    def launch(self, share: bool | list[str] | tuple[str, ...] = False,
               server_name: str = "127.0.0.1", server_port: int = 7860,
               block: bool = True, auth: tuple[str, str] | None = None,
               security: dict | None = None) -> "Interface":
        """Inicia el backend Go y su callback Python.

        share=True selecciona Cloudflare, localhost.run y Pinggy.
        share=["cloudflare", "localhostrun", "pinggy"] personaliza proveedores.
        """
        global _MEDIA_ROOT
        auth = _normalize_auth(auth)
        if self._process is not None:
            raise RuntimeError("Interface ya iniciada")
        if not (0 < server_port <= 65535):
            raise ValueError("server_port fuera de rango")
        providers = ["cloudflare", "localhostrun", "pinggy"] if share is True else (
            list(share) if share else [])
        root = Path(__file__).resolve().parents[2]
        custom_binary = os.getenv("GORADIO_BINARY", "").strip()
        binary = (Path(custom_binary).expanduser().resolve() if custom_binary
                  else root / "bin" / ("goradio.exe" if os.name == "nt" else "goradio"))
        if custom_binary and not binary.is_file():
            raise FileNotFoundError(f"El binario GORADIO_BINARY no existe: {binary}")
        if not binary.exists() and not shutil.which("go"):
            raise FileNotFoundError("Instala Go o compila bin/goradio.exe")
        self._temporary = tempfile.TemporaryDirectory(prefix="goradio-")
        workdir = Path(self._temporary.name)
        if security is not None:
            if auth is not None:
                raise ValueError("No combines HTTP Basic auth con login persistente")
            db_path = Path(security.get("db_path", "")).expanduser()
            if not str(security.get("db_path", "")).strip():
                raise ValueError("security.db_path debe apuntar a un archivo SQLite permanente")
            db_path = db_path.resolve()
            db_path.parent.mkdir(parents=True, exist_ok=True)
            # Los archivos siguen existiendo tras cerrar el proceso de Python.
            app_id = security.get("app_id") or ("app_" + hashlib.sha256(self.title.encode("utf-8")).hexdigest()[:16])
            media_dir = db_path.parent / ("goradio-media-" + app_id)
        else:
            media_dir = workdir / "media"
        media_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
        _MEDIA_ROOT = media_dir

        token = secrets.token_urlsafe(32)
        owner = self

        class Callback(BaseHTTPRequestHandler):
            def do_POST(self):
                streaming = self.path.endswith("/stream")
                base_path = self.path[:-7] if streaming else self.path
                valid_path = base_path == "/callback" or (
                    owner._dashboard_pages is not None
                    and base_path.startswith("/callback/")
                )
                if not valid_path or not hmac.compare_digest(
                    self.headers.get("Authorization", ""), "Bearer " + token
                ):
                    self.send_error(403)
                    return
                try:
                    length = int(self.headers.get("Content-Length", "0"))
                    if not 0 < length <= 1048576:
                        raise ValueError("Tamaño de solicitud inválido")
                    request = json.loads(self.rfile.read(length))
                    data = request["data"]
                    target = owner
                    if owner._dashboard_pages is not None:
                        slug = base_path.removeprefix("/callback/")
                        target = owner._dashboard_pages.get(slug)
                        if target is None:
                            raise ValueError("Página desconocida")
                    if not isinstance(data, list) or len(data) != len(target.inputs):
                        raise ValueError("Entradas incorrectas")
                    session = self.headers.get("X-Goradio-Session", "")
                    if session and not _SESSION_PATTERN.fullmatch(session):
                        raise ValueError("Identificador de sesión inválido")
                    token_ctx = _ACTIVE_SESSION.set((target, session) if session else None)
                    try:
                        self._execute_callback(target, data, streaming)
                    finally:
                        _ACTIVE_SESSION.reset(token_ctx)
                    return
                except Exception as exc:
                    encoded = json.dumps({"error": f"{type(exc).__name__}: {exc}"}, ensure_ascii=False).encode("utf-8")
                    self.send_response(200)
                    self.send_header("Content-Type", "application/json; charset=utf-8")
                    self.send_header("Content-Length", str(len(encoded)))
                    self.end_headers()
                    self.wfile.write(encoded)

            def _execute_callback(self, target, data, streaming):
                if target.validator is not None:
                    target.validator(*data)
                if streaming:
                    if not target.streaming:
                        raise ValueError("Función no es generadora")
                    self.send_response(200)
                    self.send_header("Content-Type", "application/x-ndjson; charset=utf-8")
                    self.send_header("Cache-Control", "no-cache")
                    self.send_header("Connection", "close")
                    self.end_headers()
                    try:
                        last_values = None
                        for result in target.fn(*data):
                            values = list(result) if len(target.outputs) > 1 and isinstance(result, (tuple, list)) else [result]
                            if len(values) != len(target.outputs):
                                raise ValueError("Número incorrecto de salidas")
                            line = json.dumps({"data": values}, ensure_ascii=False).encode("utf-8") + b"\n"
                            if len(line) > 1048576:
                                raise ValueError("Respuesta de streaming demasiado grande")
                            self.wfile.write(line)
                            self.wfile.flush()
                            last_values = values
                        if last_values is None:
                            raise ValueError("Generador no produjo resultados")
                        for callback in target._chains:
                            next_result = callback(*last_values)
                            last_values = list(next_result) if len(target.outputs) > 1 and isinstance(next_result, (tuple, list)) else [next_result]
                            if len(last_values) != len(target.outputs):
                                raise ValueError("Función encadenada devolvió salidas incorrectas")
                            self.wfile.write(json.dumps({"data": last_values}, ensure_ascii=False).encode("utf-8") + b"\n")
                            self.wfile.flush()
                    except (BrokenPipeError, ConnectionResetError):
                        return
                    except Exception:
                        self.wfile.write(json.dumps({"error": "Función generadora falló"}).encode("utf-8") + b"\n")
                        self.wfile.flush()
                    return
                result = target.fn(*data)
                values = list(result) if len(target.outputs) > 1 and isinstance(result, (tuple, list)) else [result]
                if len(values) != len(target.outputs):
                    raise ValueError("Número incorrecto de salidas")
                for callback in target._chains:
                    next_result = callback(*values)
                    values = list(next_result) if len(target.outputs) > 1 and isinstance(next_result, (tuple, list)) else [next_result]
                    if len(values) != len(target.outputs):
                        raise ValueError("Función encadenada devolvió salidas incorrectas")
                payload = json.dumps({"data": values}, ensure_ascii=False).encode("utf-8")
                self.send_response(200)
                self.send_header("Content-Type", "application/json; charset=utf-8")
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)

            def log_message(self, format, *args):
                pass

        try:
            class CallbackServer(ThreadingHTTPServer):
                # ThreadingHTTPServer hereda una cola TCP muy pequeña (5).
                # Varias peticiones Gradio paralelas pueden causar WSAECONNREFUSED.
                request_queue_size = 128
                daemon_threads = True

            self._server = CallbackServer(("127.0.0.1", 0), Callback)
            threading.Thread(target=self._server.serve_forever, daemon=True).start()
            cfg = {"title": self.title, "description": self.description,
                   "theme": asdict(self.theme) if self.theme is not None else None,
                   "live": self.live, "load": self.load, "every_seconds": self.every,
                   "streaming": self.streaming,
                   "inputs": [c.config() for c in self.inputs],
                   "outputs": [c.config() for c in self.outputs]}
            if self._dashboard_pages is not None:
                if not self._dashboard_pages:
                    raise ValueError("Dashboard requiere al menos una ruta")
                cfg["pages"] = [
                    {"title": page_data["title"], "path": page_data["path"],
                     "icon": page_data["icon"], "group": page_data["group"],
                     "description": page_data["app"].description,
                     "live": page_data["app"].live, "load": page_data["app"].load, "every_seconds": page_data["app"].every,
                     "streaming": page_data["app"].streaming,
                     "inputs": [c.config() for c in page_data["app"].inputs],
                     "outputs": [c.config() for c in page_data["app"].outputs]}
                    for page_data in self._dashboard_definitions
                ]
            config_path = workdir / "app.json"
            config_path.write_text(json.dumps(cfg, ensure_ascii=False), encoding="utf-8")
            base = [str(binary)] if binary.exists() else ["go", "run", "./cmd/goradio"]
            command = base + ["--config", str(config_path),
                              "--host", server_name, "--port", str(server_port),
                              "--media-dir", str(media_dir),
                              "--callback-url", f"http://127.0.0.1:{self._server.server_port}/callback",
                              "--share", ",".join(providers)]
            if security is not None:
                command += ["--security-db", str(db_path),
                            "--app-id", app_id]
                if security.get("require_login", False):
                    command.append("--require-login")
                if security.get("allow_signup", False):
                    command.append("--allow-signup")
                if security.get("persist", True):
                    command.append("--persist")
                if security.get("bootstrap_user"):
                    command.extend(["--bootstrap-user", security["bootstrap_user"]])
            env = os.environ.copy()
            env["GORADIO_PYTHON"] = os.sys.executable
            env["GORADIO_CALLBACK_TOKEN"] = token
            if auth:
                env["GORADIO_AUTH_USER"], env["GORADIO_AUTH_PASSWORD"] = auth
            self._process = subprocess.Popen(command, cwd=root, env=env)
            self.local_url = f"http://127.0.0.1:{server_port}"
            for _ in range(120):
                if self._process.poll() is not None:
                    raise RuntimeError("El proceso Go terminó antes de iniciar")
                try:
                    request = Request(self.local_url + "/api/health")
                    if auth:
                        encoded = base64.b64encode(f"{auth[0]}:{auth[1]}".encode()).decode()
                        request.add_header("Authorization", f"Basic {encoded}")
                    with urlopen(request, timeout=0.4) as response:
                        if response.status == 200:
                            break
                except (URLError, OSError):
                    time.sleep(0.15)
            else:
                raise RuntimeError("El servidor Go no respondió")
            print(f"Goradio local: {self.local_url}", flush=True)
            if providers:
                print("Enlaces públicos: consulta la sección Compartir aplicación", flush=True)
            if block:
                try:
                    self._process.wait()
                except KeyboardInterrupt:
                    pass
                finally:
                    self.close()
            return self
        except BaseException:
            self.close()
            raise

    def close(self) -> None:
        global _MEDIA_ROOT
        if self._process is not None:
            if self._process.poll() is None:
                self._process.terminate()
                try:
                    self._process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    self._process.kill()
                    self._process.wait()
            self._process = None
        if self._server is not None:
            self._server.shutdown()
            self._server.server_close()
            self._server = None
        if self._temporary is not None:
            self._temporary.cleanup()
            self._temporary = None
        _MEDIA_ROOT = None


class Dashboard(Interface):
    """Panel multipágina con frontend Gradio y navegación lateral responsive.

    API sugerida, inspirada en Blocks.route de Gradio 6::

        dash = gr.Dashboard(title="Mi panel")
        dash.route("Inicio", "/", gr.Interface(...), icon="dashboard")
        dash.route("Imágenes", "/imagenes", gr.Interface(...), icon="image")
        dash.launch(share=["gradio", "cloudflare"])
    """

    def __init__(self, title: str = "Goradio", description: str = "",
                 theme: Theme | None = None) -> None:
        super().__init__(lambda: "", [], Textbox(label="Salida"),
                         title=title, description=description, theme=theme)
        self._dashboard_pages = {}

    def route(self, title: str, path: str, app: Interface | None = None,
              *, icon: str = "file", group: str = "") -> "Dashboard":
        """Registra una ventana con una función y componentes independientes."""
        if app is None:
            raise ValueError("route requiere un gr.Interface para definir la página")
        if not isinstance(app, Interface) or app is self:
            raise TypeError("app debe ser un gr.Interface diferente del Dashboard")
        import re
        route = "/" if path in ("", "/") else "/" + path.strip("/")
        if route != "/" and re.fullmatch(r"/[a-z][a-z0-9-]{0,47}", route) is None:
            raise ValueError("Usa rutas como /imagenes o /mis-modelos")
        if route in {"/api", "/gradio_api", "/auth", "/login", "/config", "/assets", "/static"}:
            raise ValueError("Ruta reservada")
        key = "home" if route == "/" else route.removeprefix("/")
        if key in self._dashboard_pages:
            raise ValueError(f"Ruta duplicada: {route}")
        if not self._dashboard_pages:
            self.inputs, self.outputs, self.fn = app.inputs, app.outputs, app.fn
        self._dashboard_pages[key] = app
        self._dashboard_definitions.append({
            "path": route, "title": title or app.title, "icon": icon,
            "group": group, "app": app,
        })
        return self
