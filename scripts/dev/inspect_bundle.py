"""Inspección de contratos del bundle frontend vendorizado de Gradio (solo archivos del proyecto)."""
from pathlib import Path
import zipfile
import sys
import re

root = Path(__file__).resolve().parents[2]
zip_path = root / "third_party" / "gradio" / "frontend.zip"
term = sys.argv[1] if len(sys.argv) > 1 else "upload_progress"
filename_filter = sys.argv[2] if len(sys.argv)>2 else ""
with zipfile.ZipFile(zip_path) as z:
    matches = []
    for info in z.infolist():
        if not info.filename.endswith(".js") or info.file_size > 12_000_000 or (filename_filter and filename_filter not in info.filename):
            continue
        raw = z.read(info.filename).decode("utf-8", errors="ignore")
        for match in list(re.finditer(re.escape(term), raw))[:3]:
            start, end = match.start(), match.end()
            matches.append((info.filename, raw[max(start - 400, 0):min(end + 700,len(raw))]))
        if len(matches) >= 14:
            break
print("Termino:",term,"resultados:",len(matches))
for name, snippet in matches:
    print("\nFILE",name,"\n",snippet[:1000])
