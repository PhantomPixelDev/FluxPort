/* File writes use native CSRF-protected forms. SSE refreshes the table after indexing. */
(() => {
 'use strict';
 const shell=document.querySelector('[data-file-workspace]');if(!shell)return;
 const id=shell.dataset.fileWorkspace,base='/collections/'+id;
 const api=window.FluxPortEditor={saving:false};let pending=false,checking=false,checkAgain=false,jobID='',refreshing=false,epoch=0;
 const toolbar=()=>shell.querySelector('[data-file-editor]');
 function message(text,error=false){const el=shell.querySelector('[data-file-edit-status]');if(el){el.textContent=text;el.classList.toggle('error',error);}shell.querySelectorAll('[data-file-undo],[data-file-redo]').forEach(b=>{if(api.saving)b.disabled=true;});}
 function update(state){
  const bar=toolbar();if(!bar)return;
  bar.dataset.generation=state.generation;bar.dataset.currentRevision=state.history.current;bar.dataset.fileStatus=state.status;
  shell.querySelectorAll('[data-file-history-action]').forEach(form=>{form.elements.generation.value=state.generation;form.elements.current_revision.value=state.history.current;});
  const editable=state.editable&&state.status==='ready'&&!state.history.stale&&!api.saving;
  const undo=bar.querySelector('[data-file-undo]'),redo=bar.querySelector('[data-file-redo]');
  if(undo)undo.disabled=!editable||!state.history.previous;
  if(redo)redo.disabled=!editable||!state.history.next;
 }
 async function check(){
  if(checking){checkAgain=true;return;}checking=true;const requestEpoch=epoch;
  try{
   const r=await fetch(base+'/file-history?format=json'+(jobID?'&job_id='+encodeURIComponent(jobID):''),{headers:{Accept:'application/json'}});if(!r.ok)throw Error('Unable to read file status');
   const state=await r.json();if(requestEpoch!==epoch)return;
   if(state.job_state==='ready')pending=true;
   if(['failed','cancelled'].includes(state.job_state)){api.saving=false;pending=false;update(state);message(state.job_error||'Change cancelled.',true);return;}
   update(state);
   if(pending&&state.status==='ready'&&!refreshing){
    refreshing=true;
    try{
     await window.FluxPortWorkspace?.refreshFile();
     const sheet=shell.querySelector('#detail-content .sheet-data'),generation=sheet?.querySelector('[data-sheet-edit]')?.elements.generation.value;
     // A watcher may start another index while the table request is in flight.
     // Keep the pending save until SSE supplies a queryable generation.
     if(sheet?.dataset.sourceStatus!=='ready'||generation!==state.generation){message('Saved · updating table…');return;}
     pending=false;api.saving=false;jobID='';update(state);message('Saved');
    }
    finally{refreshing=false;}
   }else if(pending&&state.status==='failed'){api.saving=false;message('File saved; table needs refresh: '+(state.error||'Indexing failed'),true);}
   else if(!api.saving)message(state.status==='ready'?(state.editable?'Saved':'Read-only file'):'Updating table…');
  }catch(e){message(e.message,true);}finally{checking=false;if(checkAgain){checkAgain=false;queueMicrotask(check);}}
 }
 async function submit(form,submitter){
  if(api.saving)return;
  epoch++;api.saving=true;message(form.matches('[data-file-history-action]')?'Restoring file…':'Saving…');
  const values=new FormData(form);if(submitter?.name)values.set(submitter.name,submitter.value);
  form.setAttribute('aria-busy','true');
  try{
   const r=await fetch(form.getAttribute('action'),{method:'POST',headers:{Accept:'application/json'},body:new URLSearchParams(values)});
   let state;try{state=await r.json();}catch{throw Error('Save failed. Reload to check the file.');}
   if(!r.ok)throw Error(state.message||state.error||'Changes were not saved');
   pending=true;update({...state,editable:true});message('Saved · updating table…');
   try{localStorage.removeItem('fluxport-cleanup-'+id);}catch{}
   await check();
  }catch(e){api.saving=false;await check();message(e.message,true);}
  finally{form.removeAttribute('aria-busy');}
 }
 document.addEventListener('submit',e=>{
  const form=e.target;
  if(api.saving&&form.matches('[data-cleanup-form]')){e.preventDefault();e.stopImmediatePropagation();return;}
  if(!form.matches('[data-file-history-action],[data-file-command]')&&!(form.matches('[data-sheet-edit]')&&form.elements.file_mode?.value==='1'))return;
  if(form.matches('[data-sheet-edit]')&&!form.querySelector('[name=record_id]:checked'))return;
  if(form.elements.method?.value==='DELETE'&&!confirm('Delete selected rows from this file? You can Undo this change.')){e.preventDefault();e.stopImmediatePropagation();return;}
  e.preventDefault();e.stopImmediatePropagation();submit(form,e.submitter);
 },true);
 api.submit=submit;
 function job(marker){
  if(!marker)return;
  epoch++;jobID=marker.dataset.fileJob;
  const state=marker.dataset.state;
  if(state==='ready'){pending=true;api.saving=true;message('Saved · updating table…');check();}
  else if(state==='queued'||state==='running'){api.saving=true;message('Saving changes…');check();}
 }
 document.addEventListener('fluxport:file-job',e=>job(e.detail?.node||shell.querySelector('[data-file-job]')));
 window.addEventListener('fluxport:progress',async e=>{
  const m=e.detail||{};
  if(jobID&&m.id===jobID){
   if(m.state==='ready'){pending=true;message('Saved · updating table…');}
   if(m.state==='failed'||m.state==='cancelled'){api.saving=false;pending=false;message(m.state==='failed'?'Change failed. Open Clean to retry.':'Change cancelled.',true);window.FluxPortWorkspace?.refreshFile();}
  }
  if(m.connected||(m.resource===id&&['ready','failed','cancelled','changed'].includes(m.state)))await check();
 });
 document.addEventListener('keydown',e=>{
  if(!(e.ctrlKey||e.metaKey)||e.altKey||e.target.closest('input,textarea,[contenteditable]'))return;
  const redo=(e.key.toLowerCase()==='z'&&e.shiftKey)||e.key.toLowerCase()==='y';
  if(!redo&&e.key.toLowerCase()!=='z')return;
  const button=toolbar()?.querySelector(redo?'[data-file-redo]':'[data-file-undo]');
  if(button&&!button.disabled){e.preventDefault();button.click();}
 });
 function dates(){shell.querySelectorAll('[data-file-time]').forEach(t=>{const d=new Date(t.dateTime);if(!Number.isNaN(d.valueOf()))t.textContent=d.toLocaleString();});}
 document.addEventListener('fluxport:file-reloaded',dates);
 document.addEventListener('htmx:afterSwap',dates);
 document.addEventListener('fluxport:tool-mounted',dates);
 dates();job(shell.querySelector('[data-file-job]'));queueMicrotask(check);
})();

