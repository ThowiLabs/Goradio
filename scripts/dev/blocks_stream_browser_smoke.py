"""Smoke: generator on one Blocks button and non-streaming on another."""
import json
import os
from pathlib import Path
import subprocess
import sys
import socket
from urllib.request import Request,urlopen

ROOT=Path(__file__).resolve().parents[2]
CLI=ROOT/"bin"/"goradio-blocks-cli.exe"
if not CLI.exists():
    raise SystemExit("Build Go CLI before this test")
os.environ["GORADIO_BINARY"]=str(CLI)
sys.path.insert(0,str(ROOT/"python"))
sys.path.insert(0,str(ROOT/"examples"))
from blocks_stream_demo import demo

with socket.socket() as listener:
    listener.bind(("127.0.0.1",0))
    port=listener.getsockname()[1]
app=demo.launch(block=False,server_port=port)
try:
    assert app._process is None or app._process.poll() is None, "Go process not alive"
    base=f"http://127.0.0.1:{port}"
    with urlopen(base+"/gradio_api/config",timeout=6) as response:
        cfg=json.load(response)
    deps=cfg["dependencies"]
    assert len(deps)==4,deps
    assert deps[2]["queue"] is True and deps[2]["types"]["generator"] is True,deps[2]
    assert deps[3]["queue"] is False and deps[3]["types"]["generator"] is False,deps[3]
    assert cfg["enable_queue"] is True
    payload=json.dumps({"fn_index":3,"data":["normal"]}).encode()
    request=Request(base+"/gradio_api/run/predict",data=payload,
        headers={"Content-Type":"application/json"})
    with urlopen(request,timeout=8) as response:
        result=json.load(response)
    assert result["data"]==["NORMAL"],result
    cmd=["node","scripts/dev/cdp-audit.mjs",base+"/","--click-button=Procesar progresivo","--open-results"]
    run=subprocess.run(cmd,cwd=ROOT,capture_output=True,text=True,timeout=65)
    if run.returncode:raise AssertionError("Chrome failed: "+run.stderr[-1500:])
    start=run.stdout.find('{\n  "page":')
    assert start>=0,run.stdout[-1000:]
    report=json.loads(run.stdout[start:])
    page=json.loads(report["page"]["result"]["value"])
    problems=[e for e in report["events"] if e.get("status",0)>=400
      or e.get("kind")=="Runtime.exceptionThrown"
      or "Unexpected client exception" in e.get("text","")]
    assert not problems,problems
    assert "Etapa 3/3" in page["body"],page["body"]
    assert "prueba" in page["body"],page["body"]
    assert any("queue/join" in e.get("url","") for e in report["events"]),report["events"]
    print(json.dumps({"result":"PASS","frontend":"Gradio 6.30.0",
                      "stream":"Etapa 3/3","normal":result["data"][0],
                      "browser_errors":len(problems)},ensure_ascii=False))
finally:
    app.close()
