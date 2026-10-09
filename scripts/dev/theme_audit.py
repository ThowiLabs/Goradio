import zipfile,re,collections
from pathlib import Path
z=zipfile.ZipFile(Path(__file__).resolve().parents[2]/"third_party/gradio/frontend.zip")
cs=[x for x in z.namelist() if x.endswith(".css")]
print("CSS ASSETS",len(cs),cs[:22])
styles={k:v for k,v in ((p,z.read(p).decode("utf8")) for p in cs)}
print("index css",[(p,len(d)) for p,d in styles.items() if p.startswith("assets/index-")])
d=next(d for p,d in styles.items() if p.startswith("assets/index-"))
v=collections.Counter(re.findall(r'var\((--[\w-]+)',d))
print("TOP VARIABLE REFERENCES",v.most_common(80))
print("GLOBAL",d[:3500])
for s in ["--block-padding","--body-background-fill","--block-background-fill","--layout-gap","--spacing-lg","--text-lg","--font"]:
 print("VAR",s, "occurences",len(re.findall(s,d)), "first context",d[max(0,d.find(s)-120):d.find(s)+250])
