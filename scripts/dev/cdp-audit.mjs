import {spawn} from "node:child_process";
import {mkdtempSync,writeFileSync} from "node:fs";
import {tmpdir} from "node:os";
import {join} from "node:path";
const sleep=(ms)=>new Promise(r=>setTimeout(r,ms));
const url=process.argv[2]??"http://127.0.0.1:7860/";
const chrome="C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe";
const profile=mkdtempSync(join(tmpdir(),"goradio-cdp-"));
const port=9357;
const child=spawn(chrome,["--headless=new","--no-first-run","--disable-gpu","--disable-background-networking","--disable-extensions","--remote-debugging-port="+port,"--user-data-dir="+profile,"about:blank"],{stdio:"ignore"});
let ws,sequence=1;
const events=[];const pending=new Map();
const send=(method,params={})=>new Promise((resolve,reject)=>{const id=sequence++;pending.set(id,{resolve,reject});ws.send(JSON.stringify({id,method,params}));setTimeout(()=>{if(pending.has(id)){pending.delete(id);reject(new Error("CDP timeout: "+method))}},8000)});
try {
 let target;
 for(let i=0;i<50;i++){try{const pages=await (await fetch("http://127.0.0.1:"+port+"/json/list")).json();target=pages.find(x=>x.type==="page"&&x.webSocketDebuggerUrl);if(target)break;}catch{}await sleep(200)}
 if(!target)throw Error("Chrome CDP no disponible");
 ws=new WebSocket(target.webSocketDebuggerUrl);
 await new Promise((resolve,reject)=>{ws.onopen=resolve;ws.onerror=reject});
 ws.onmessage=(event)=>{let m=JSON.parse(event.data);if(m.id&&pending.has(m.id)){const q=pending.get(m.id);pending.delete(m.id);q.resolve(m.result??m.error);return;}if(m.method&&/^(Runtime\.exceptionThrown|Runtime\.consoleAPICalled|Network\.loadingFailed|Network\.responseReceived|Network\.requestWillBeSent|Log\.entryAdded)$/.test(m.method)){
 if(m.method==="Network.responseReceived" && m.params?.response?.status<400 && !m.params?.response?.url?.includes("/gradio_api/run/"))return; if(m.method==="Network.requestWillBeSent" && !(/gradio_api|config|theme/.test(m.params?.request?.url??"")))return;
 events.push({kind:m.method,url:m.params?.response?.url||m.params?.request?.url||m.params?.requestId,status:m.params?.response?.status,stack:m.params?.stackTrace?.callFrames?.slice(0,3).map(f=>({url:f.url,line:f.lineNumber,fn:f.functionName})),text:(m.params?.entry?.text||m.params?.exceptionDetails?.exception?.description||m.params?.exceptionDetails?.text||m.params?.args?.map(a=>a.value||a.description).join(" ")||m.params?.errorText||"").slice(0,450)});}};
 for(const method of ["Runtime.enable","Page.enable","Network.enable","Log.enable"])await send(method);
 if(process.argv.includes("--mobile"))await send("Emulation.setDeviceMetricsOverride",{width:390,height:844,deviceScaleFactor:1,mobile:true});
 await send("Page.navigate",{url});
 await sleep(10000);
 if(process.argv.includes("--mobile")){
   const before=await send("Runtime.evaluate",{expression:"(()=>({open:document.querySelector('.gd-layout')?.classList.contains('gd-menu-open'),visible:getComputedStyle(document.querySelector('.gd-menu-toggle')).display,expanded:document.querySelector('#gd-menu-toggle')?.getAttribute('aria-expanded')}))()",returnByValue:true});
   const opened=await send("Runtime.evaluate",{expression:"(()=>{document.querySelector('#gd-menu-toggle')?.click();return {open:document.querySelector('.gd-layout')?.classList.contains('gd-menu-open'),expanded:document.querySelector('#gd-menu-toggle')?.getAttribute('aria-expanded')}})()",returnByValue:true});
   const closed=await send("Runtime.evaluate",{expression:"(()=>{document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));return {open:document.querySelector('.gd-layout')?.classList.contains('gd-menu-open'),expanded:document.querySelector('#gd-menu-toggle')?.getAttribute('aria-expanded')}})()",returnByValue:true});
   console.log("MOBILE",JSON.stringify({before,opened,closed}));
 }

 if(process.argv.includes("--upload-image")||process.argv.includes("--upload-audio")||process.argv.includes("--upload-video")){
   const kind=process.argv.includes("--upload-video")?"video":process.argv.includes("--upload-audio")?"audio":"image";
   const filename=kind==="video"?join(process.cwd(),"scripts","dev","_fixtures","tiny.mp4"):kind==="audio"?join(process.cwd(),"scripts","dev","_fixtures","tone.wav"):join(profile,"tiny.png");
   if(kind==="image")writeFileSync(filename,Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+9+RUAAAAASUVORK5CYII=","base64"));
   const doc=await send("DOM.getDocument",{depth:-1,pierce:true});
   const foundAll=await send("DOM.querySelectorAll",{nodeId:doc.root.nodeId,selector:"input[type=file]"});
   const ids=foundAll.nodeIds||[];
   const selected=kind==="video"?ids[ids.length-1]:kind==="audio"?ids[Math.min(1,ids.length-1)]:ids[0];
   const found={nodeId:selected??0,total:ids.length,kind};
   console.log("INPUT FILE",JSON.stringify(found));
   if(found.nodeId){
    await send("DOM.setFileInputFiles",{files:[filename],nodeId:found.nodeId});
    await sleep(6000);
    const submit=await send("Runtime.evaluate",{expression:"(()=>{const button=[...document.querySelectorAll('button')].find(x=>x.textContent.trim().includes('Submit'));if(!button)return 'missing Submit';button.click();return 'submit clicked'})()",returnByValue:true});
    console.log("SUBMIT IMAGE",JSON.stringify(submit));
    await sleep(7000);
   }
 }
 if(process.argv.includes("--draft")){
   const typed=await send("Runtime.evaluate",{expression:"(()=>{const x=document.querySelector('textarea');if(!x)return {missing:'textarea'};const setter=Object.getOwnPropertyDescriptor(Object.getPrototypeOf(x),'value')?.set;if(setter)setter.call(x,'Borrador persistente sin Submit');else x.value='Borrador persistente sin Submit';x.dispatchEvent(new Event('input',{bubbles:true}));x.dispatchEvent(new Event('change',{bubbles:true}));return {value:x.value}})()",returnByValue:true});
   console.log("TYPED",JSON.stringify(typed));
   await sleep(3500);
   const saved=await send("Runtime.evaluate",{expression:"fetch('/api/state').then(r=>r.json()).then(x=>JSON.stringify(x))",returnByValue:true,awaitPromise:true});
   console.log("PERSISTED",JSON.stringify(saved));
   await send("Page.reload",{ignoreCache:true});
   await sleep(9000);
   const restored=await send("Runtime.evaluate",{expression:"JSON.stringify({value:document.querySelector('textarea')?.value,body:document.body.innerText.slice(0,160)})",returnByValue:true});
   console.log("RESTORED",JSON.stringify(restored));
 }
 if (process.argv.includes("--click-blocks")) {
   const clicked=await send("Runtime.evaluate",{expression:"(()=>{const btn=[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='Resumir');if(!btn)return 'missing Resumir';btn.click();return 'clicked'})()",returnByValue:true});
   console.log("BLOCKS CLICK",JSON.stringify(clicked));
   await sleep(4000);
 }
 const activeTabArg=process.argv.find(arg=>arg.startsWith("--open-tab="));
 if (process.argv.includes("--open-results") || activeTabArg) {
   const label=activeTabArg?activeTabArg.slice("--open-tab=".length):"Resultados";
   const opened=await send("Runtime.evaluate",{expression:"(()=>{const label="+JSON.stringify(label)+";const node=[...document.querySelectorAll('[role=tab]')].find(el=>el.textContent.trim()===label);if(!node)return {found:false};node.click();return {found:true,label}})()",returnByValue:true});
   console.log("TAB",JSON.stringify(opened));
   await sleep(2400);
 }
 if (process.argv.includes("--interact")) {
  const expression="(()=>{const input=document.querySelector('textarea')||document.querySelector('input[type=text]');if(!input)return {status:'missing input'};const set=Object.getOwnPropertyDescriptor(Object.getPrototypeOf(input),'value')?.set; if(set)set.call(input,'hola prueba');else input.value='hola prueba';input.dispatchEvent(new Event('input',{bubbles:true}));input.dispatchEvent(new Event('change',{bubbles:true}));const numeric=document.querySelector('input[type=number]');if(numeric){const setter=Object.getOwnPropertyDescriptor(Object.getPrototypeOf(numeric),'value')?.set;if(setter)setter.call(numeric,'3');else numeric.value='3';numeric.dispatchEvent(new Event('input',{bubbles:true}));numeric.dispatchEvent(new Event('change',{bubbles:true}));}const button=[...document.querySelectorAll('button')].find(b=>b.textContent.trim().includes('Submit'));if(!button)return {status:'missing button',element:input.outerHTML};button.click();return {status:'clicked',input:input.value,buttons:document.querySelectorAll('button').length}})()";
  const clicked=await send("Runtime.evaluate",{expression,returnByValue:true});
  console.log("INTERACTION",JSON.stringify(clicked));
  await sleep(6000);
  if(process.argv.includes("--clear")){
   const reset=await send("Runtime.evaluate",{expression:"(()=>{const b=[...document.querySelectorAll('button')].find(x=>x.textContent.trim().includes('Clear'));if(!b)return 'no clear';b.click();return 'clear clicked'})()",returnByValue:true});
   console.log("CLEAR",JSON.stringify(reset));
   await sleep(2000);
  }
 }
 const res=await send("Runtime.evaluate",{expression:'JSON.stringify({title:document.title,body:document.body.innerText.slice(0,1200),html:document.querySelector("gradio-app")?.innerHTML.slice(0,800),forms:document.querySelectorAll("button").length,images:[...document.querySelectorAll("img")].map(x=>x.src).slice(0,8),videos:[...document.querySelectorAll("video")].map(x=>({src:x.getAttribute("src"),current:x.currentSrc,ready:x.readyState,error:x.error?.message||null})),waveforms:[...document.querySelectorAll("[data-testid^=waveform-]")].map(x=>({label:x.getAttribute("data-testid"),visible:getComputedStyle(x).display,canvas:x.querySelectorAll("canvas").length,html:x.innerHTML.slice(0,140)})),nativeAudios:[...document.querySelectorAll("audio.standard-player")].map(x=>({className:x.className,display:getComputedStyle(x).display,src:x.currentSrc})),values:[...document.querySelectorAll("textarea,input")].map(x=>({type:x.tagName,value:x.value,placeholder:x.placeholder})).slice(0,16)})',returnByValue:true,awaitPromise:true});
 console.log(JSON.stringify({page:res,events:events.slice(0,80),totalEvents:events.length},null,2));
}catch(e){console.error(e.message);process.exitCode=1}
finally{try{ws?.close()}catch{}try{child.kill()}catch{}}
