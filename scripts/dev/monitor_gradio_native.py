"""Monitor Gradio's native FRP tunnel and the local Go server over time.

Run with a running native instance, e.g.:
  python scripts/dev/monitor_gradio_native.py --port 7993 --seconds 240

Checks local /api/health, tunnel state and remote /api/health; optionally
makes predict requests. A link rotation is recorded, not counted as a Go crash.
"""
import argparse
import json
import pathlib
import subprocess
import sys
import time
import urllib.error
import urllib.request

parser=argparse.ArgumentParser()
parser.add_argument("--port", type=int, default=7993)
parser.add_argument("--seconds", type=int, default=240)
parser.add_argument("--interval", type=float, default=12)
parser.add_argument("--output", type=pathlib.Path)
options=parser.parse_args()
local=f"http://127.0.0.1:{options.port}"
results=[]
old_url=""
started=time.monotonic()
consecutive_remote_fails=0

def get_local(path):
    with urllib.request.urlopen(local+path,timeout=4) as r:
        return r.status,r.read().decode()

def check_remote(url,path="/api/health",body=None):
    args=["curl.exe","-sS","-L","--max-time","12","-w","\\nCURL_STATUS:%{http_code}"]
    if body is not None:
        args+=["-X","POST","-H","Content-Type: application/json",
               "--data-binary",body]
    args.append(url+path)
    proc=subprocess.run(args,capture_output=True,text=True,timeout=17)
    raw,_,code=proc.stdout.rpartition("\nCURL_STATUS:")
    return int(code or 0),raw[:250],proc.stderr[:250],proc.returncode

deadline=started+options.seconds
while time.monotonic() < deadline:
    result={"elapsed_seconds":round(time.monotonic()-started,1)}
    try:
        status,body=get_local("/api/health")
        result["go_alive"]=status==200 and json.loads(body).get("status")=="ok"
        _,body=get_local("/api/tunnels")
        state=next((p for p in json.loads(body) if p["provider"]=="gradio"),{})
        result["state"]=state.get("status","unknown")
        link=state.get("url","")
        if link and link!=old_url:
            result["url_changed_from"]=old_url
            old_url=link
        if link:
            code,body,err,rc=check_remote(link)
            result["public_http"]=code
            result["public_healthy"]=code==200 and '"status":"ok"' in body
            if not result["public_healthy"]:result["public_error"]=err or body
            consecutive_remote_fails=0 if result["public_healthy"] else consecutive_remote_fails+1
            if result["public_healthy"] and len(results)%3==0:
                code,body,err,rc=check_remote(link,"/api/predict",'{"data":["native-duration"]}')
                result["predict_http"]=code
                result["predict_ok"]=code==200 and "native-duration" in body
                if not result["predict_ok"]:result["predict_error"]=err or body
        else:result["public_healthy"]=False
    except Exception as exc:
        result["go_alive"]=False
        result["error"]=str(exc)
    results.append(result)
    if options.output:
        options.output.parent.mkdir(parents=True,exist_ok=True)
        options.output.write_text(json.dumps(results,indent=2,ensure_ascii=False),encoding="utf-8")
    print(json.dumps(result,ensure_ascii=False),flush=True)
    time.sleep(min(options.interval,max(0,deadline-time.monotonic())))
n=len(results)
print("SUMMARY",json.dumps({
    "samples":n,"local_alive":sum(bool(x.get("go_alive")) for x in results),
    "public_healthy":sum(bool(x.get("public_healthy")) for x in results),
    "public_link_rotations":sum(bool(x.get("url_changed_from")) and x["url_changed_from"]!="" for x in results),
    "predict_success":sum(bool(x.get("predict_ok")) for x in results),
    "predict_fail":sum(x.get("predict_ok") is False for x in results),
},ensure_ascii=False),flush=True)
sys.exit(1 if any(x.get("go_alive") is False for x in results) or any(x.get("predict_ok") is False for x in results) else 0)
