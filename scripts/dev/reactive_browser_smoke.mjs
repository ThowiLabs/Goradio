// Browser smoke checks two native Gradio 6.30.0 components and the
// SSE queue, preserving the real frontend rather than a replacement UI.
// Start: go run ./examples/reactive-go and go run ./examples/chat-streaming-go.
import {spawnSync} from "node:child_process";
import {fileURLToPath} from "node:url";
import {dirname,resolve} from "node:path";

const root=resolve(dirname(fileURLToPath(import.meta.url)),"../..");
const base=process.argv[2]||"http://127.0.0.1:7996";
const chatBase=process.argv[3]||"http://127.0.0.1:7987";
const script=resolve(root,"scripts/dev/cdp-audit.mjs");
const checks=[
  {name:"Gradio Dataframe + Timer",url:base,expected:["Estado: 3/3","Monitor","completado"],interact:false},
  {name:"Gradio Chatbot + SSE",url:chatBase,expected:["hola prueba","Esta respuesta es una demostración"],interact:true},
];
for(const check of checks){
 const argv=[script,check.url,...(check.interact?["--interact"]:[])];
 const res=spawnSync(process.execPath,argv,{cwd:root,encoding:"utf8",timeout:55000,maxBuffer:2<<20});
 if(res.error || res.status!==0)throw Error(check.name+": navegador falló: "+(res.error?.message||res.stderr||res.stdout.slice(-1500)));
 const start=res.stdout.indexOf('{\n  "page":');
 if(start<0)throw Error(check.name+": no hay auditoría JSON");
 const audit=JSON.parse(res.stdout.slice(start));
 const page=JSON.parse(audit.page.result.value);
 const failures=audit.events.filter(e=>e.status>=400||e.kind==="Runtime.exceptionThrown"
  ||e.kind==="Network.loadingFailed"
  ||/Unexpected client exception|Unexpected error|Could not parse server response/.test(e.text||""));
 if(failures.length)throw Error(check.name+": errores de navegador "+JSON.stringify(failures.slice(0,4)));
 for(const expected of check.expected){
  if(!page.body.includes(expected))throw Error(check.name+": faltó "+expected+": "+page.body.slice(0,1400));
 }
 if(!audit.events.some(e=>e.url?.includes("/gradio_api/queue/join"))
  ||!audit.events.some(e=>e.url?.includes("/gradio_api/queue/data"))){
  throw Error(check.name+": Gradio no consumió la cola SSE nativa");
 }
 console.log("PASS "+check.name+"; requests="+audit.totalEvents);
}
