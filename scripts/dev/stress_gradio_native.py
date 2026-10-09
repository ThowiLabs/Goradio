"""HTTP end-to-end smoke/stress against a running Goradio native share.

Usage:
  python scripts/dev/stress_gradio_native.py https://xxxxx.gradio.live
"""
import concurrent.futures
import json
import sys
import time
import subprocess

root=sys.argv[1].rstrip("/")
mode=sys.argv[2] if len(sys.argv)>2 else "python"
expected=24

def check(i):
    name=f"native-parallel-{i:02d}"
    body=({"data":[name]} if mode=="go" else {"fn_index":0,"data":[name,i]})
    data=json.dumps(body).encode()
    start=time.perf_counter()
    try:
        command=["curl.exe","-sS","--max-time","20",
                 "-H","Content-Type: application/json",
                 "--data-binary","@-",
                 root+("/api/predict" if mode=="go" else "/gradio_api/run/predict")]
        proc=subprocess.run(command,input=data,capture_output=True,timeout=24)
        if proc.returncode!=0:
            return i,False,proc.stderr.decode(errors="replace"),round(time.perf_counter()-start,2)
        try:
            result=json.loads(proc.stdout)
        except json.JSONDecodeError:
            return i,False,"non-JSON: "+repr(proc.stdout[:250]),round(time.perf_counter()-start,2)
        status=200
    except Exception as exc:
        return i,False,str(exc),round(time.perf_counter()-start,2)
    expected_data=([name.upper()] if mode=="go" else [f"Bienvenido, {name}. Tu panel está en Goradio.",i*2])
    valid=(status==200 and result.get("data")==expected_data and
           (mode=="go" or result.get("success") is True))
    return i,valid,result if not valid else "ok",round(time.perf_counter()-start,2)

with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
    result=list(pool.map(check,range(expected)))
good=[x for x in result if x[1]]
bad=[x for x in result if not x[1]]
print(f"Concurrent HTTPS responses: {len(good)}/{expected}")
print("Latencies sec (min,max):",min(x[3] for x in result),max(x[3] for x in result))
for x in bad:
    print("FAILED",x)
if bad:sys.exit(1)
print("PASS: 24 requests over native FRP, mode:",mode)
