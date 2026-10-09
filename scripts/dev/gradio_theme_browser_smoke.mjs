import {spawn} from "node:child_process";
import {mkdtempSync} from "node:fs";
import {join} from "node:path";
import {tmpdir} from "node:os";
const port=Number(process.env.GORADIO_CDP_PORT||9579);
const chrome=process.env.GORADIO_CHROME||(process.platform==="win32"?"C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe":"google-chrome");
const origin=process.argv[2]||"http://127.0.0.1:7933";
const firstPage=process.argv[3]||"/";
const secondPage=process.argv[4]||"/configuracion";
const mobilePage=process.argv[5]||firstPage;
const proc=spawn(chrome,["--headless=new","--no-first-run","--disable-gpu","--disable-extensions",
 "--remote-debugging-port="+port,"--user-data-dir="+mkdtempSync(join(tmpdir(),"goradio-theme-test-")),"about:blank"],{stdio:"ignore",windowsHide:true});
const wait=ms=>new Promise(r=>setTimeout(r,ms));
let ws,seq=0;const pending=new Map(),issues=[];
function send(method,params={}){return new Promise((resolve,reject)=>{const id=++seq;pending.set(id,{resolve,reject});ws.send(JSON.stringify({id,method,params}));setTimeout(()=>{if(pending.has(id)){pending.delete(id);reject(new Error("CDP timeout "+method))}},13000)})}
async function evalJS(fn){
 const ans=await send("Runtime.evaluate",{expression:"("+fn.toString()+")()",returnByValue:true,awaitPromise:true});
 if(ans.exceptionDetails)throw Error(ans.exceptionDetails.exception?.description||"Evaluation failed");
 return ans.result.value;
}
function check(cond,msg){if(!cond)throw Error(msg)}
try{
 let target;for(let i=0;i<45;i++){try{const pages=await (await fetch("http://127.0.0.1:"+port+"/json/list")).json();target=pages.find(x=>x.type==="page"&&x.webSocketDebuggerUrl);if(target)break}catch{}await wait(200)}
 if(!target)throw Error("Chrome unavailable");
 ws=new WebSocket(target.webSocketDebuggerUrl);
 await new Promise((resolve,reject)=>{ws.onopen=resolve;ws.onerror=reject});
 ws.onmessage=e=>{const v=JSON.parse(e.data);if(v.id&&pending.has(v.id)){pending.get(v.id).resolve(v.result||v.error);pending.delete(v.id)}else if(v.method==="Runtime.exceptionThrown"){issues.push(v.params.exceptionDetails?.exception?.description||"JS exception")}else if(v.method==="Network.responseReceived"&&v.params.response.status>=400){issues.push(v.params.response.status+" "+v.params.response.url)}};
 await send("Page.enable");await send("Runtime.enable");await send("Network.enable");
 await send("Emulation.setDeviceMetricsOverride",{width:1366,height:900,deviceScaleFactor:1,mobile:false});
 await send("Page.navigate",{url:new URL(firstPage,origin).href});await wait(5200);
 const initial=await evalJS(function(){
  const app=document.querySelector("gradio-app"),bar=document.querySelector(".gd-sidebar"),btn=document.querySelector("#gd-sidebar-collapse"),glyph=btn.querySelector("svg");
  const rect=el=>{const r=el.getBoundingClientRect();return {x:r.x,y:r.y,w:r.width,h:r.height}};
  const b=rect(btn),g=rect(glyph);
  return {app:!!app,bodyDark:document.body.classList.contains("dark"),barBg:getComputedStyle(bar).backgroundColor,appBg:getComputedStyle(app).backgroundColor,accent:getComputedStyle(app).getPropertyValue("--primary-500").trim(),centerError:Math.max(Math.abs((b.x+b.w/2)-(g.x+g.w/2)),Math.abs((b.y+b.h/2)-(g.y+g.h/2)))};
 });
 check(initial.app,"Gradio app not loaded");
 check(initial.barBg===initial.appBg,"Sidebar doesn't inherit Gradio bg "+JSON.stringify(initial));
 check(initial.centerError<1.1,"Chevron not centered "+initial.centerError);
 check(initial.accent==="#f97316","Original Gradio accent differs "+initial.accent);
 const collapsed=await evalJS(function(){
  const absent=["gd-theme-color","gd-theme-secondary","gd-theme-reset"].every(id=>!document.getElementById(id));
  document.querySelector("#gd-sidebar-collapse").click();
  return {noEditor:absent,collapsed:document.querySelector(".gd-layout").classList.contains("gd-collapsed"),stored:localStorage.getItem("goradio.sidebar.collapsed.v1"),width:document.querySelector(".gd-sidebar").getBoundingClientRect().width};
 });
 check(collapsed.noEditor,"Color editor unexpectedly exposed in production");
 check(collapsed.collapsed&&collapsed.width<=72,"Sidebar collapse failed "+JSON.stringify(collapsed));
 await send("Page.navigate",{url:new URL(secondPage,origin).href});await wait(5200);
 const persisted=await evalJS(function(){
  const app=document.querySelector("gradio-app"),sidebar=document.querySelector(".gd-sidebar");
  const before=getComputedStyle(sidebar).backgroundColor;
  document.body.classList.toggle("dark",false);
  return {collapsed:document.querySelector(".gd-layout").classList.contains("gd-collapsed"),page:document.querySelector(".gd-nav-link.is-active")?.textContent.trim(),before,lightBg:getComputedStyle(sidebar).backgroundColor,lightAppBg:getComputedStyle(app).backgroundColor,hasEditor:!!document.querySelector("#gd-theme-color")};
 });
 check(persisted.collapsed&&!persisted.hasEditor,"Collapse not persisted or UI contains theme editor "+JSON.stringify(persisted));
 check(persisted.lightBg===persisted.lightAppBg,"Light mode sidebar doesn't match Gradio "+JSON.stringify(persisted));
 await send("Emulation.setDeviceMetricsOverride",{width:390,height:844,deviceScaleFactor:1,mobile:true});
 await send("Page.navigate",{url:new URL(mobilePage,origin).href});await wait(4800);
 const mobile=await evalJS(function(){
  const menu=document.querySelector("#gd-menu-toggle"),layout=document.querySelector(".gd-layout");
  menu.click();
  const rect=el=>{const r=el.getBoundingClientRect();return {x:r.x,w:r.width,h:r.height}};
  const side=document.querySelector("#gd-sidebar");
  return {width:window.innerWidth,open:layout.classList.contains("gd-menu-open"),side:rect(side),noEditor:!document.querySelector("#gd-theme-color"),barBg:getComputedStyle(side).backgroundColor,appBg:getComputedStyle(document.querySelector("gradio-app")).backgroundColor};
 });
 check(mobile.open,"Mobile menu failed to open "+JSON.stringify(mobile));
 check(mobile.side.x>=-1&&mobile.side.w<mobile.width&&mobile.noEditor,"Mobile sidebar issue "+JSON.stringify(mobile));
 check(mobile.barBg===mobile.appBg,"Mobile theme background not inherited "+JSON.stringify(mobile));
 check(issues.length===0,"Browser issues "+JSON.stringify(issues.slice(0,6)));
 console.log(JSON.stringify({result:"PASS",initial,collapsed,persisted,mobile,issues},null,2));
}catch(e){console.error("FAIL:",e.stack||e.message);process.exitCode=1}
finally{try{ws?.close()}catch{}try{proc.kill()}catch{}}
