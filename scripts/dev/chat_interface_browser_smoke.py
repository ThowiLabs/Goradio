"""Test original Gradio Chatbot, Python generator and session history."""
from pathlib import Path
import sys, os, json, subprocess
ROOT=Path(__file__).resolve().parents[2]
sys.path[:0]=[str(ROOT/"python"),str(ROOT/"examples")]
os.environ["GORADIO_BINARY"]=str(ROOT/"bin"/"goradio-blocks-cli.exe")
from chat_interface import chat

app=chat.launch(block=False,server_port=7990)
try:
    run=subprocess.run(["node","scripts/dev/cdp-audit.mjs",
       "http://127.0.0.1:7990/","--interact"],
       cwd=ROOT,capture_output=True,text=True,timeout=54)
    pos=run.stdout.find('{\n  "page":')
    assert run.returncode==0 and pos>=0,run.stderr[-850:]+run.stdout[-1200:]
    audited=json.loads(run.stdout[pos:])
    page=json.loads(audited["page"]["result"]["value"])
    errors=[e for e in audited["events"] if e.get("status",0)>=400
       or e.get("kind")=="Runtime.exceptionThrown"
       or "Unexpected client exception" in e.get("text","")]
    assert not errors,errors
    assert "Eco: hola prueba" in page["body"],page["body"][:1300]
    print(json.dumps({"result":"PASS","chatbot":"Gradio original",
                      "streaming":"SSE v3","errors":len(errors)},ensure_ascii=True))
finally:
    app.close()
