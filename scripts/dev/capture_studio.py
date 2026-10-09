"""Captura página local para comparar el tema de Goradio visualmente."""
from pathlib import Path
from tempfile import TemporaryDirectory
import subprocess
import sys

url = sys.argv[1] if len(sys.argv)>1 else "http://127.0.0.1:7898/"
root=Path(__file__).resolve().parents[2]
outdir=root/"scripts"/"dev"/"_screenshots"
outdir.mkdir(parents=True,exist_ok=True)
out=outdir/"studio-after.png"
chrome=Path(r"C:\Program Files\Google\Chrome\Application\chrome.exe")
with TemporaryDirectory(prefix="goradio-chrome-") as profile:
    result=subprocess.run([str(chrome),"--headless=new","--no-first-run","--disable-gpu","--disable-extensions","--hide-scrollbars","--disable-background-networking","--window-size=1320,1000","--force-device-scale-factor=1","--virtual-time-budget=11000",f"--user-data-dir={profile}",f"--screenshot={out}",url],capture_output=True,text=True,timeout=60)
print("exit",result.returncode,"file",out,"bytes",out.stat().st_size if out.exists() else 0)
print("chrome message",result.stderr[-700:])
