"""Verifica únicamente la demo local 127.0.0.1:7916 con admin/admin."""
from http.cookiejar import CookieJar
from urllib.request import HTTPCookieProcessor,Request,build_opener
import json

origin="http://127.0.0.1:7916"
browser=build_opener(HTTPCookieProcessor(CookieJar()))
payload=json.dumps({"username":"admin","password":"admin"}).encode()
req=Request(origin+"/auth/login",payload,{"Content-Type":"application/json"},method="POST")
with browser.open(req,timeout=6) as response:
    assert response.status==200, response.status
with browser.open(origin+"/config",timeout=6) as response:
    assert response.status==200, response.status
print("LOGIN OK: admin/admin autenticado y aplicación accesible en "+origin)
