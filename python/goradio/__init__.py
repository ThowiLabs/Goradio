"""Interfaces interactivas respaldadas por un servidor Go."""

from __future__ import annotations

import base64
import hmac
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
    "Interface", "Component", "Textbox", "Number", "Slider", "Checkbox",
    "Dropdown", "Image", "Audio", "Video", "File", "JSON", "media",
]

_MEDIA_ROOT: Path | None = None
_TYPES = {"textbox", "number", "slider", "checkbox", "dropdown",
          "image", "audio", "video", "file", "json"}


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

    def __post_init__(self) -> None:
        if self.type not in _TYPES:
            raise ValueError(f"Componente desconocido: {self.type}")
        if self.min is not None and self.max is not None and self.min > self.max:
            raise ValueError("min no puede superar max")

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
                 title: str = "Goradio", description: str = "") -> None:
        if not callable(fn):
            raise TypeError("fn debe ser invocable")
        self.fn = fn
        self.inputs = _components(inputs)
        self.outputs = _components(outputs)
        if not self.outputs:
            raise ValueError("Se requiere una salida")
        self.title = title
        self.description = description
        self._process: subprocess.Popen | None = None
        self._server: ThreadingHTTPServer | None = None
        self._temporary: tempfile.TemporaryDirectory | None = None
        self.local_url: str | None = None

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
                if self.path != "/callback" or not hmac.compare_digest(
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
                    if not isinstance(data, list) or len(data) != len(owner.inputs):
                        raise ValueError("Entradas incorrectas")
                    result = owner.fn(*data)
                    values = list(result) if len(owner.outputs) > 1 and isinstance(result, (tuple, list)) else [result]
                    if len(values) != len(owner.outputs):
                        raise ValueError("Número incorrecto de salidas")
                    payload = {"data": values}
                except Exception as exc:
                    payload = {"error": f"{type(exc).__name__}: {exc}"}
                encoded = json.dumps(payload, ensure_ascii=False).encode("utf-8")
                self.send_response(200)
                self.send_header("Content-Type", "application/json; charset=utf-8")
                self.send_header("Content-Length", str(len(encoded)))
                self.end_headers()
                self.wfile.write(encoded)

            def log_message(self, format, *args):
                pass

        try:
            self._server = ThreadingHTTPServer(("127.0.0.1", 0), Callback)
            threading.Thread(target=self._server.serve_forever, daemon=True).start()
            cfg = {"title": self.title, "description": self.description,
                   "inputs": [c.config() for c in self.inputs],
                   "outputs": [c.config() for c in self.outputs]}
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
