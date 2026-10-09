import {spawn,execFileSync} from "node:child_process";
import {writeFileSync,unlinkSync,existsSync} from "node:fs";
import {join} from "node:path";

const port=7958,cwd=process.cwd();
const configPath=join(cwd,"scripts","dev","_dashboard_browser_config.json");
const config={title:"Goradio Browser QA",pages:[
 {path:"/",title:"Inicio",icon:"dashboard",group:"Principal",mode:"uppercase",
  inputs:[{type:"textbox",label:"Entrada"}],outputs:[{type:"textbox",label:"Salida"}]},
 {path:"/imagenes",title:"Imágenes",icon:"image",group:"Herramientas",mode:"uppercase",
  inputs:[{type:"textbox",label:"Entrada 2"}],outputs:[{type:"textbox",label:"Salida 2"}]}
]};
writeFileSync(configPath,JSON.stringify(config),"utf8");
const binary=join(cwd,"bin","goradio-dashboard-v040.exe");
const args=["--config",configPath,"--host","127.0.0.1","--port",String(port)];
if(process.env.GORADIO_BROWSER_SHARE)args.push("--share",process.env.GORADIO_BROWSER_SHARE);
const child=spawn(binary,args,{cwd,stdio:"ignore",windowsHide:true});
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
try {
 let started=false;
 for(let i=0;i<70;i++){
  if(child.exitCode!==null)throw Error("Go exited "+child.exitCode);
  try{const response=await fetch("http://127.0.0.1:"+port+"/api/health",{signal:AbortSignal.timeout(600)});
      if(response.ok){started=true;break}}catch{}
  await sleep(140);
 }
 if(!started)throw Error("Cannot start dashboard Go server");
 console.log("Dashboard local port",port);
 for(const path of ["/","/imagenes"]){
   const flags=path==="/imagenes"?["--mobile","--interact"]:[];
   const output=execFileSync("node",["scripts/dev/cdp-audit.mjs","http://127.0.0.1:"+port+path,...flags],
     {cwd,encoding:"utf8",timeout:44000,maxBuffer:2*1024*1024});
   if(path==="/imagenes"){
    const match=output.match(/^MOBILE ([^\r\n]+)/m);
    if(!match)throw Error("Missing mobile report");
    const mobile=JSON.parse(match[1]);
    console.log("Mobile",JSON.stringify(mobile));
    if(mobile.before.result.value.visible==="none"||mobile.opened.result.value.open!==true||mobile.closed.result.value.open!==false)throw Error("Responsive menu failed");
   }
   const pos=output.indexOf('{\n  "page":');
   if(pos<0)throw Error("Missing audit output: "+output.slice(0,300));
   const report=JSON.parse(output.slice(pos));
   const page=JSON.parse(report.page.result.value);
   console.log("Chrome "+path+" "+JSON.stringify({
     title:page.title,body:page.body.slice(0,280),forms:page.forms,
     jsErrors:report.events.filter(x=>x.kind==="Runtime.exceptionThrown").map(x=>x.text),
     httpErrors:report.events.filter(x=>x.status>=400).map(x=>x.url).slice(0,8)
   }));
   if(!page.body.includes("Goradio Browser QA") ||
      !page.body.includes("Inicio") || !page.body.includes("Imágenes") ||
      page.forms<2)throw Error("Dashboard sidebar or controls missing on "+path);
   if(report.events.some(e=>e.kind==="Runtime.exceptionThrown"))throw Error("JS exception on "+path);
 }
 if(process.env.GORADIO_BROWSER_SHARE){
   let url="";
   for(let i=0;i<80;i++){
     try{
       const status=await (await fetch("http://127.0.0.1:"+port+"/api/tunnels")).json();
       const tunnel=status.find(item=>item.provider===process.env.GORADIO_BROWSER_SHARE);
       if(tunnel&&tunnel.status==="online"){url=tunnel.url;break}
     }catch{}
     await sleep(250);
   }
   if(!url)throw Error("Official Gradio tunnel never became online");
   for(const pagePath of ["/","/imagenes"]){
     const response=await fetch(url+pagePath,{signal:AbortSignal.timeout(15000)});
     const html=await response.text();
     console.log("Official tunnel "+pagePath+" HTTP "+response.status);
     if(response.status!==200 || !html.includes("Goradio Browser QA"))throw Error("Official Gradio page unavailable: "+pagePath);
   }
 }
 console.log("PASS dashboard Chromium desktop");
}finally{
 if(child.exitCode===null)child.kill();
 if(existsSync(configPath))unlinkSync(configPath);
}
