"""End-to-end streaming smoke test: Python generator -> Go queue -> Gradio 6.30 UI."""
import json
import os
import pathlib
import subprocess
import sys
import time
from urllib.request import urlopen

ROOT = pathlib.Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "python"))
import goradio as gr

def generate(text):
    count = gr.state_get("calls", 0) + 1
    gr.state_set("calls", count)
    for i in range(1, 4):
        yield f"### Paso {i}/3\n\n{str(text)} · Visita {count}"
        if i < 3:
            time.sleep(.1)

def main():
    os.environ["GORADIO_BINARY"] = str(ROOT / "bin" / "goradio-reactive-cli.exe")
    app = gr.Interface(
        fn=generate,
        inputs=gr.Textbox(label="Solicitud", value="monitoreo"),
        outputs=gr.Markdown(label="Avance"),
        title="Python + Go streaming original",
        load=True,
        every=5,
    )
    app.then(lambda result: result + " · Cadena completa")
    app.launch(block=False, server_port=7991)
    try:
        with urlopen("http://127.0.0.1:7991/api/health", timeout=5) as response:
            assert json.load(response)["status"] == "ok"
        result = subprocess.run(
            ["node", "scripts/dev/cdp-audit.mjs", "http://127.0.0.1:7991/"],
            cwd=ROOT, capture_output=True, text=True, timeout=45,
        )
        output = result.stdout
        start = output.find('{\n  "page":')
        assert result.returncode == 0 and start >= 0, (result.stderr, output[-1500:])
        data = json.loads(output[start:])
        page = json.loads(data["page"]["result"]["value"])
        problems = [e for e in data["events"]
                    if e.get("status", 0) >= 400 or e["kind"] in ("Runtime.exceptionThrown",)
                    or ("Unexpected client exception" in e.get("text", ""))]
        assert not problems, problems
        assert "Paso 3/3" in page["body"] and "Cadena completa" in page["body"], page["body"]
        assert "Visita" in page["body"], page["body"]
        print(json.dumps({"status": "PASS", "body": page["body"][:600],
                          "browser_issues": problems}, ensure_ascii=False))
    finally:
        app.close()

if __name__ == "__main__":
    main()
