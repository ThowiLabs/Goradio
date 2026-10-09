"""Prueba de integración Python→Go con login SQLite. No deja base de datos."""
from __future__ import annotations
import json
import os
import tempfile
from pathlib import Path
from http.cookiejar import CookieJar
from urllib.error import HTTPError
from urllib.request import HTTPCookieProcessor,Request,build_opener,urlopen
import goradio as gr

with tempfile.TemporaryDirectory(prefix="goradio-e2e-") as folder:
    app=gr.Interface(lambda text:text.upper(),inputs=gr.Textbox(label="Texto"),
                     outputs=gr.Textbox(label="Resultado"),title="Goradio smoke auth")
    try:
        app.launch(block=False,server_port=7916,security={
            "db_path":str(Path(folder)/"secure.sqlite"),"app_id":"python-e2e",
            "require_login":True,"persist":True,"bootstrap_user":"admin",
        })
        origin="http://127.0.0.1:7916"
        with urlopen(origin+"/api/health",timeout=8) as r:
            assert r.status==200
        try:
            urlopen(origin+"/config",timeout=8)
            raise AssertionError("El acceso sin login no debe permitir config")
        except HTTPError as e:
            assert e.code==401, e.code
        opener=build_opener(HTTPCookieProcessor(CookieJar()))
        req=Request(origin+"/auth/login",json.dumps({
             "username":"admin","password":os.environ["GORADIO_ADMIN_PASSWORD"]
        }).encode(),{"Content-Type":"application/json"},method="POST")
        with opener.open(req,timeout=8) as r:
            assert r.status==200
        with opener.open(origin+"/config",timeout=8) as r:
            assert r.status==200
        with opener.open(origin+"/auth/me",timeout=8) as r:
            assert json.load(r)["authenticated"] is True
        print("OK: Python import + servidor Go + SQLite + login + sesión HTTP")
    finally:
        app.close()
