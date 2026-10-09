"use strict";
let config;
const $ = (id) => document.getElementById(id);
const inputHandles=[];
const mediaTypes=new Set(["image","audio","video","file"]);
async function api(path,opts) {
 const res=await fetch(path,opts);
 const payload=await res.json();
 if(!res.ok)throw new Error(payload.error?.message || "Error HTTP "+res.status);
 return payload;
}
function el(tag,props={}) {
 const node=document.createElement(tag);
 for(const [key,value] of Object.entries(props)){
  if(key==="text")node.textContent=String(value);
  else if(key==="class")node.className=value;
  else node.setAttribute(key,String(value));
 }
 return node;
}
function component(c,index) {
 const wrap=el("div",{class:"field"});
 const label=el("label",{text:c.label || c.type});
 const name="input-"+index;label.htmlFor=name;
 let field;
 switch(c.type){
 case "textbox":field=el("textarea",{id:name,placeholder:c.placeholder||"Escribe aquí"});field.value=c.value??"";break;
 case "dropdown":
  field=el("select",{id:name});
  for(const choice of (c.choices||[])){const option=el("option",{value:choice,text:choice});field.append(option)}
  if(c.value!==undefined)field.value=c.value;
  break;
 case "number":case "slider":
  field=el("input",{id:name,type:c.type==="slider"?"range":"number"});
  if(c.min!==undefined)field.min=c.min;if(c.max!==undefined)field.max=c.max;
  if(c.step!==undefined)field.step=c.step;
  field.value=c.value??c.min??0;break;
 case "checkbox":field=el("input",{id:name,type:"checkbox"});field.checked=Boolean(c.value);break;
 default:
  field=el("input",{id:name,type:"file"});
  const accepts={image:"image/*",audio:"audio/*",video:"video/*",file:"*/*"};
  field.accept=accepts[c.type]??"*/*";
 }
 if(c.required)field.required=true;
 wrap.append(label,field);
 if(c.type==="slider"){const val=el("small",{text:field.value});field.addEventListener("input",()=>{val.textContent=field.value});wrap.append(val)}
 if(mediaTypes.has(c.type)){
  const preview=el("div",{class:"preview"});wrap.append(preview);
  field.addEventListener("change",()=>{preview.replaceChildren();const file=field.files?.[0];if(!file)return;
   const node=el("small",{text:file.name});preview.append(node);
  });
 }
 inputHandles.push({field,c});return wrap;
}
function outputBox(c,index) {
 const wrap=el("div",{class:"field"});
 wrap.append(el("label",{text:c.label||c.type}),el("div",{class:"result",id:"result-"+index}));
 return wrap;
}
function renderValue(node,c,value){
 node.replaceChildren();
 if(value==null){node.textContent="Sin resultado";return}
 if(mediaTypes.has(c.type)&&typeof value==="string"&&value.startsWith("/api/media/")){
  if(c.type==="image"){node.append(el("img",{src:value,alt:c.label||"Imagen de resultado"}));return}
  if(c.type==="audio"||c.type==="video"){const player=el(c.type,{src:value,controls:""});node.append(player);return}
  const link=el("a",{href:value,text:"Descargar archivo",download:""});node.append(link);return;
 }
 node.textContent=typeof value==="string"?value:JSON.stringify(value,null,2);
}
async function predict(event){
 event.preventDefault();
 const button=$("submit");button.disabled=true;$("status").textContent="Procesando…";
 try{
  const values=[];
  for(const {field,c} of inputHandles){
   if(mediaTypes.has(c.type)){
    if(!field.files?.length){values.push(null);continue}
    const fd=new FormData();fd.append("file",field.files[0]);fd.append("kind",c.type);
    const uploaded=await api("/api/upload",{method:"POST",body:fd});
    values.push(uploaded.url);
   } else if(c.type==="checkbox")values.push(field.checked);
   else if(c.type==="number"||c.type==="slider")values.push(field.value===""?null:Number(field.value));
   else values.push(field.value);
  }
  const data=await api("/api/predict",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({data:values})});
  data.data.forEach((value,i)=>renderValue($("result-"+i),config.outputs[i],value));
  $("empty").hidden=true;$("status").textContent="Ejecución completada";
 }catch(error){$("status").textContent=error.message}finally{button.disabled=false}
}
function renderTunnels(items){
 const links=$("links");
 links.querySelectorAll(".provider").forEach(n=>n.remove());
 for(const item of items){
  const row=el("div",{class:"link-row provider"});
  row.append(el("span",{text:item.provider}));
  if(item.url){const a=el("a",{href:item.url,target:"_blank",rel:"noopener noreferrer",text:item.url});row.append(a)}
  else row.append(el("code",{text:item.status==="error"?(item.error||"Error"):item.status}));
  links.append(row);
 }
}
async function refreshLinks(){try{renderTunnels(await api("/api/tunnels"))}catch(_error){/* server disconnected */}}
async function init(){
 try {
  config=await api("/api/config");
  document.title=config.title+" · Goradio";
  $("title").textContent=config.title;$("description").textContent=config.description||"Interfaz interactiva";
  config.inputs.forEach((c,i)=>$("inputs").append(component(c,i)));
  config.outputs.forEach((c,i)=>$("outputs").append(outputBox(c,i)));
  $("form").addEventListener("submit",predict);
  $("local-link").textContent=location.origin;$("local-link").href=location.origin;
  await refreshLinks();setInterval(refreshLinks,2500);
 }catch(e){$("status").textContent=e.message}
}
init();
