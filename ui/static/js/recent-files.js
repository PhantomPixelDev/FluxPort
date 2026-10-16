(() => {
 'use strict';
 const key='fluxport-recent-files';const shell=document.querySelector('[data-file-workspace]');
 const load=()=>{try{const a=JSON.parse(localStorage.getItem(key)||'[]');return Array.isArray(a)?a.filter(v=>v&&/^[a-f0-9]{24}$/.test(v.id)&&typeof v.name==='string').slice(0,8):[];}catch{return [];}};
 function remember(){if(!shell)return;const name=shell.querySelector('h1')?.textContent.trim();if(!name)return;try{localStorage.setItem(key,JSON.stringify([{id:shell.dataset.fileWorkspace,name},...load().filter(v=>v.id!==shell.dataset.fileWorkspace)].slice(0,8)));}catch{}}
 async function recent(){
  const target=document.querySelector('[data-recent-files]');if(!target)return;target.replaceChildren();target.hidden=true;const entries=load();if(!entries.length)return;
  const valid=[];let next=0;await Promise.all(Array.from({length:Math.min(4,entries.length)},async()=>{while(next<entries.length){const i=next++,item=entries[i];try{const r=await fetch('/collections/'+item.id+'/workbench/status');if(r.ok)valid[i]=item;}catch{valid[i]=item;}}}));
  const remaining=valid.filter(Boolean);if(!remaining.length)return;target.hidden=false;const label=document.createElement('span');label.className='muted small-text';label.textContent='Recently opened';target.append(label);
  remaining.slice(0,5).forEach(item=>{const link=document.createElement('a');link.href='/collections/'+item.id;link.textContent=item.name;link.setAttribute('aria-label','Reopen '+item.name);link.title=item.name;target.append(link);});
 }
 document.addEventListener('collectionsChanged',recent);
 document.addEventListener('htmx:afterSwap',e=>{const target=e.detail?.target;if(target&&(target.id==='collection-table'||target.querySelector?.('#collection-table')))recent();});
 document.addEventListener('fluxport:file-reloaded',remember);remember();recent();
})();
