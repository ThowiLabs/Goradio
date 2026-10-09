"""Genera el CSS Default de Gradio 6.30.0 desde su código fuente oficial.

Uso: python scripts/dev/export_official_theme.py RUTA_CLON_GRADIO
No instala Gradio ni modifica el entorno Python; importa solamente
módulos de themes y crea stubs para las funciones de publicación a Hub.
"""
from pathlib import Path
from subprocess import check_output
from types import ModuleType
from importlib.machinery import ModuleSpec
import importlib.resources
import sys

root=Path(__file__).resolve().parents[2]
upstream=Path(sys.argv[1]) if len(sys.argv)>1 else None
if not upstream or not upstream.is_dir():
    raise SystemExit("Se requiere la ruta del repositorio oficial de Gradio clonado")

def original(path:str)->str:
    return check_output(["git","show",f"HEAD:{path}"],cwd=upstream).decode("utf8")

def fake(name:str, package:bool=False)->ModuleType:
    mod=ModuleType(name)
    if package:
        mod.__path__=[str(root/"third_party"/"gradio")]
        mod.__spec__=ModuleSpec(name,loader=None,is_package=True)
    sys.modules[name]=mod
    return mod

gradio=fake("gradio",True)
themes=fake("gradio.themes",True)
utils=fake("gradio.themes.utils",True)
hub=fake("huggingface_hub")
hub.CommitOperationAdd=type("CommitOperationAdd",(),{})
fake("semantic_version")
fake("gradio_client",True)
docs=fake("gradio_client.documentation")
docs.document=lambda *a,**kw: lambda cls:cls
readme=fake("gradio.themes.utils.readme_content")
readme.README_CONTENT=""

for mod_name,source in (
    ("colors","gradio/themes/utils/colors.py"),
    ("sizes","gradio/themes/utils/sizes.py"),
    ("fonts","gradio/themes/utils/fonts.py"),
):
    mod=fake(f"gradio.themes.utils.{mod_name}")
    setattr(utils,mod_name,mod)
    if mod_name=="fonts":
        previous=importlib.resources.files
        importlib.resources.files=lambda package: root/"third_party"/"gradio"
    try:
        exec(compile(original(source),source,"exec"),mod.__dict__)
    finally:
        if mod_name=="fonts":
            importlib.resources.files=previous

utils.get_matching_version=lambda *a,**kw:None
utils.get_theme_assets=lambda *a,**kw:None
base=fake("gradio.themes.base")
exec(compile(original("gradio/themes/base.py"),"gradio/themes/base.py","exec"),base.__dict__)
default=fake("gradio.themes.default")
exec(compile(original("gradio/themes/default.py"),"gradio/themes/default.py","exec"),default.__dict__)
theme=default.Default()
css=theme._get_theme_css()
path=root/"static"/"gradio-default-theme.css"
path.write_text("/* Gradio 6.30.0 Default theme. Apache-2.0. See third_party/gradio/LICENSE */\n"+css+"\n",encoding="utf8")
print("Generated",path,len(css),"bytes",css.count("--"),"variables")
