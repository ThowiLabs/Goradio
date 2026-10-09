"""Ejecuta predicción usando el mismo archivo almacenado por el frontend, solo en servidor local de prueba."""
import json
import sys
from urllib.request import Request, urlopen

id = sys.argv[1]
url = "http://127.0.0.1:7897/gradio_api/run/predict"
body = json.dumps({"data":[None,None,{"video":{"path":id,"meta":{"_type":"gradio.FileData"}},"subtitles":None}]}).encode()
req = Request(url, data=body, headers={"Content-Type":"application/json"}, method="POST")
with urlopen(req,timeout=8) as r:
    print("STATUS",r.status,"BODY",r.read().decode())
