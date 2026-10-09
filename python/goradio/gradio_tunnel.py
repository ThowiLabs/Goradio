"""Puente experimental con el túnel oficial de Gradio.

Requiere instalar gradio en el mismo entorno Python.
Esta integración usa una API interna de Gradio que puede cambiar.
"""
from __future__ import annotations

import argparse
import secrets
import sys


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, required=True)
    args = parser.parse_args()

    try:
        from gradio import networking
        from gradio.tunneling import CURRENT_TUNNELS
    except ImportError as exc:
        raise SystemExit("Para share=gradio instala 'gradio' en este entorno Python") from exc

    try:
        url = networking.setup_tunnel(
            local_host="127.0.0.1",
            local_port=args.port,
            share_token=secrets.token_urlsafe(32),
            share_server_address=None,
        )
        print(url, flush=True)
        sys.stdin.buffer.read()
    finally:
        for tunnel in list(CURRENT_TUNNELS):
            tunnel.kill()


if __name__ == "__main__":
    main()
