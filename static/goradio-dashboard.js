"use strict";
(function(){
 const svg=(paths)=>'<svg viewBox="0 0 24 24" aria-hidden="true">'+paths+'</svg>';
 const paths={
  home:'<rect x="3" y="9" width="18" height="12" rx="2"/><path d="M3 10 12 3l9 7M9 21v-8h6v8"/>',
  dashboard:'<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/>',
  image:'<rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="8.5" cy="8.5" r="1.5"/><path d="m3 17 6-6 4 4 3-3 5 5"/>',
  music:'<path d="M9 18V5l12-2v13M9 5l12-2"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/>',
  settings:'<circle cx="12" cy="12" r="3"/><path d="m10 2 1-1h2l1 1 1 2 2 1 2-1 2 2-1 2 1 2 2 1v3l-2 1-1 2 1 2-2 2-2-1-2 1-1 2h-3l-1-2-2-1-2 1-2-2 1-2-1-2-2-1v-3l2-1 1-2-1-2 2-2 2 1 2-1z"/>',
  file:'<path d="M6 2h8l5 5v15H6a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2z"/><path d="M14 2v6h5M8 13h8M8 17h8"/>',
  chart:'<path d="M4 19h16M6 15l4-5 4 2 4-8"/><circle cx="6" cy="15" r="1"/><circle cx="10" cy="10" r="1"/><circle cx="14" cy="12" r="1"/><circle cx="18" cy="4" r="1"/>',
  users:'<circle cx="9" cy="7" r="4"/><path d="M2 21v-2a7 7 0 0 1 14 0v2H2M17 4a4 4 0 0 1 0 8m0 2a6 6 0 0 1 5 6"/>',
  folder:'<path d="M3 6h7l2 2h9v11H3z"/>',
  terminal:'<rect x="2" y="3" width="20" height="18" rx="2"/><path d="m6 8 4 4-4 4m6 0h6"/>'
 };
 document.addEventListener("DOMContentLoaded",()=>{
  const layout=document.querySelector(".gd-layout"),btn=document.querySelector("#gd-menu-toggle");
  const side=document.querySelector("#gd-sidebar"),backdrop=document.querySelector("#gd-backdrop");
  if(!layout||!btn||!side)return;
  const setOpen=(value)=>{
   layout.classList.toggle("gd-menu-open",value);
   btn.setAttribute("aria-expanded",String(value));
   btn.setAttribute("aria-label",value?"Cerrar menú":"Abrir menú");
  };
  btn.addEventListener("click",()=>setOpen(!layout.classList.contains("gd-menu-open")));
  backdrop?.addEventListener("click",()=>setOpen(false));
  document.addEventListener("keydown",e=>{if(e.key==="Escape")setOpen(false)});
  window.addEventListener("resize",()=>{if(innerWidth>790)setOpen(false)});
  for(const link of side.querySelectorAll(".gd-nav-link")){
    const icon=link.dataset.icon;
    link.querySelector(".gd-nav-icon").innerHTML=svg(paths[icon]||paths.file);
    link.addEventListener("click",()=>setOpen(false));
  }
 });
})();