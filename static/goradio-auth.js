"use strict";
(() => {
 let auth;
 async function fetchAuth() {
   try {
    const response=await fetch("/auth/me",{credentials:"same-origin",cache:"no-store"});
    if(!response.ok)return;
    auth=await response.json();
    if(!auth.authenticated)return;
    const panel=document.createElement("div");
    panel.className="goradio-auth-panel";
    const user=document.createElement("span");
    user.textContent="◉ "+auth.identity.username;
    const logout=document.createElement("button");
    logout.textContent="Cerrar sesión";
    logout.type="button";
    logout.addEventListener("click",async()=>{
      logout.disabled=true;
      try {
       const res=await fetch("/auth/logout",{method:"POST",credentials:"same-origin",headers:{"X-Goradio-CSRF":auth.csrf}});
       if(!res.ok)throw new Error("No se pudo cerrar la sesión");
       location.replace("/login");
      }catch(error){logout.disabled=false;alert(error.message)}
    });
    panel.append(user,logout);
    document.body.append(panel);
   }catch { /* el modo público no necesita autenticación */ }
 }
 document.addEventListener("DOMContentLoaded", fetchAuth);
})();