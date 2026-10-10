"""Verify native Gradio Radio / CheckboxGroup rendering and server validation."""
import json
import os
from pathlib import Path
import subprocess
import sys
import socket
from urllib.request import Request,urlopen
ROOT=Path(__file__).resolve().parents[2]
CLI=ROOT/"bin"/"goradio-blocks-cli.exe"
os.environ["GORADIO_BINARY"]=str(CLI)
sys.path.insert(0,str(ROOT/"python"))
sys.path.insert(0,str(ROOT/"examples"))
from selection_demo import demo

with socket.socket() as listener:
    listener.bind(("127.0.0.1",0))
    port=listener.getsockname()[1]
app=demo.launch(block=False,server_port=port)
try:
    assert app._process is None or app._process.poll() is None, "Go process not alive"
    origin=f"http://127.0.0.1:{port}"
    with urlopen(origin+"/gradio_api/config",timeout=6) as response:
        cfg=json.load(response)
    kinds={item["type"] for item in cfg["components"]}
    assert "radio" in kinds and "checkboxgroup" in kinds,kinds
    payload=json.dumps({"data":["Verde",["Go","Python"]]}).encode()
    request=Request(origin+"/gradio_api/run/predict",data=payload,headers={"Content-Type":"application/json"})
    with urlopen(request,timeout=8) as response:
        data=json.load(response)["data"]
    assert data==["Color: Verde; lenguajes: Go, Python"],data
    cmd=["node","scripts/dev/cdp-audit.mjs",origin+"/"]
    check=subprocess.run(cmd,cwd=ROOT,capture_output=True,text=True,timeout=55)
    assert check.returncode==0,(check.stderr,check.stdout[-1200:])
    start=check.stdout.find('{\n  "page":')
    assert start>=0,check.stdout[-1200:]
    audit=json.loads(check.stdout[start:])
    pg=json.loads(audit["page"]["result"]["value"])
    errors=[e for e in audit["events"] if e.get("status",0)>=400
      or e.get("kind")=="Runtime.exceptionThrown"
      or "Unexpected client exception" in e.get("text","")]
    assert not errors,errors
    for text in ["Rojo","Azul","Verde","Go","Python","Rust"]:
        assert text in pg["body"],pg["body"]
    print(json.dumps({"result":"PASS","controls":["Radio","CheckboxGroup"],
                      "prediction":data[0],"browser_errors":len(errors)},ensure_ascii=False))
finally:
    app.close()
