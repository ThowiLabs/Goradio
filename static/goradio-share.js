"use strict";
(() => {
 const root=document.createElement("aside");
 root.className="goradio-share";
 const toggle=document.createElement("button");
 toggle.className="goradio-share-toggle";
 toggle.type="button";
 toggle.textContent="◈  Compartir";
 toggle.setAttribute("aria-label","Mostrar enlaces Goradio");
 toggle.setAttribute("aria-expanded","false");
 const panel=document.createElement("section");
 panel.className="goradio-share-panel";
 panel.hidden=true;
 panel.setAttribute("aria-label","Enlaces públicos de Goradio");
 const title=document.createElement("strong"); title.textContent="Enlaces de Goradio";
 const hint=document.createElement("small");hint.textContent="Los enlaces públicos son temporales y pueden caducar.";
 const list=document.createElement("div");list.className="goradio-share-list";
 panel.append(title,hint,list);
 root.append(toggle,panel);
 toggle.addEventListener("click",()=>{
  panel.hidden=!panel.hidden;
  toggle.setAttribute("aria-expanded",String(!panel.hidden));
 });
 const row=(name,url,status)=>{
  const item=document.createElement("div"); item.className="goradio-link-row";
  const text=document.createElement("span");text.textContent=name;
  item.append(text);
  if(url&&/^https?:\/\//i.test(url)){
   const a=document.createElement("a");a.href=url;a.target="_blank";a.rel="noopener noreferrer";
   a.textContent="Abrir ↗";a.title=url;item.append(a);
  }else{const state=document.createElement("small");state.textContent=status||"Esperando";item.append(state)}
  return item;
 };
 async function update(){
  try{
   const res=await fetch("/api/tunnels",{cache:"no-store"});
   if(!res.ok)return;
   const entries=await res.json();
   list.replaceChildren(row("Local",location.origin,""));
   for(const entry of entries)list.append(row(entry.provider,entry.url,entry.status==="error"?(entry.error||"Error"):entry.status));
  }catch{ /* el servidor pudo detenerse */ }
 }
 document.addEventListener("DOMContentLoaded",()=>{
   document.body.append(root);
   update();
   const interval=setInterval(()=>{if(document.visibilityState==="visible")update()},3000);
   window.addEventListener("pagehide",()=>clearInterval(interval),{once:true});
 });
})();