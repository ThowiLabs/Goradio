"""End-to-end Python Blocks + selected Go callbacks + native Gradio Tabs."""
import json
import os
from pathlib import Path
import sys
from urllib.request import Request,urlopen

ROOT=Path(__file__).resolve().parents[2]
CLI=ROOT/"bin"/"goradio-blocks-cli.exe"
if not CLI.is_file():
    raise SystemExit("Build: go build -o bin/goradio-blocks-cli.exe ./cmd/goradio")
os.environ["GORADIO_BINARY"]=str(CLI)
sys.path.insert(0,str(ROOT/"python"))
sys.path.insert(0,str(ROOT/"examples"))
from blocks_demo import demo

app=demo.launch(block=False,server_port=7992)
try:
    with urlopen("http://127.0.0.1:7992/gradio_api/config",timeout=5) as response:
        config=json.load(response)
    assert len(config["dependencies"])==3,config["dependencies"]
    request=Request("http://127.0.0.1:7992/gradio_api/run/predict",
        data=json.dumps({"fn_index":2,"data":["Python"]}).encode(),
        headers={"Content-Type":"application/json"})
    with urlopen(request,timeout=8) as response:
        direct=json.load(response)
    assert len(direct["data"]) == 1 and direct["data"][0].startswith("### Resumen") and "Python" in direct["data"][0],direct

    import subprocess
    checked=subprocess.run(["node","scripts/dev/cdp-audit.mjs",
         "http://127.0.0.1:7992/","--click-blocks","--open-results"],
         cwd=ROOT,encoding="utf-8",capture_output=True,timeout=55)
    if checked.returncode:
        raise AssertionError("Chrome failed: "+checked.stderr[-600:])
    pos=checked.stdout.find('{\n  "page":')
    assert pos>=0,checked.stdout[-700:]
    audit=json.loads(checked.stdout[pos:])
    page=json.loads(audit["page"]["result"]["value"])
    errors=[e for e in audit["events"] if e.get("status",0)>=400
        or e.get("kind")=="Runtime.exceptionThrown"
        or "Unexpected client exception" in e.get("text","")]
    assert not errors,errors
    assert "Resumen" in page["body"] and "Hola" in page["body"],page["body"]
    print(json.dumps({"result":"PASS","python_callback":direct["data"][0],
                      "tabs":"Resultados","browser_errors":len(errors)},ensure_ascii=True))
finally:
    app.close()
