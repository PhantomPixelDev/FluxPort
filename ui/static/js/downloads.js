/* One source download or one background export; updates arrive through SSE. */
(() => {
 // The focused workspace owns its download forms and SSE lifecycle.
 if(document.querySelector('[data-file-workspace]'))return;
 const chooser=document.getElementById('download');
 const status=document.getElementById('collection-download-status');
 const form=document.querySelector('[data-collection-download]');
 let pending;
 let requested=false;const delivered=new Set();
 function deliver(){
  const job=status?.querySelector('[data-download-job]'),link=job?.querySelector('a[href]');
  if(requested&&job?.dataset.downloadState==='ready'&&link&&!delivered.has(job.dataset.downloadJob)){delivered.add(job.dataset.downloadJob);requested=false;link.click();}
 }
 function refresh(){
  const job=status?.querySelector('[data-download-job]');
  if(job&&window.htmx)htmx.ajax('GET','/jobs?download_view=compact&id='+encodeURIComponent(job.dataset.downloadJob),{target:status,swap:'innerHTML'});
 }
 function note(select){
  const target=select.closest('form')?.querySelector('[data-download-note]');
  if(target){target.textContent=select.selectedOptions[0]?.dataset.formatNote||'';target.hidden=!target.textContent;}
 }
 function availability(){
  const state=document.querySelector('#collection-metadata [data-state]')?.dataset.state;
  if(state&&form){
   form.querySelectorAll('option:not([value=original])').forEach(o=>o.disabled=state!=='ready');
   const notice=chooser?.querySelector('[data-download-pending]');
   if(notice)notice.hidden=state==='ready';
  }
 }
 document.querySelectorAll('[data-export-format],#collection-download-format').forEach(select=>{note(select);select.addEventListener('change',()=>note(select));});
 const steps=form?.querySelector('[data-download-steps]');
 function selectedSteps(){
  if(!steps)return;
  const select=form.elements.format,original=select.querySelector('[value=original]');
  original.disabled=steps.value!=='original';
  if(original.disabled&&select.value==='original'){select.value='json';note(select);}
  const button=form.querySelector('[data-download-submit]');if(button)button.textContent=select.value==='original'?'Download original file':'Download '+select.selectedOptions[0].textContent.split(' (')[0];
 }
 steps?.addEventListener('change',selectedSteps);selectedSteps();
 form?.elements.format.addEventListener('change',selectedSteps);
 document.addEventListener('keydown',e=>{if(e.key==='Escape')document.querySelectorAll('.collection-actions>details[open]').forEach(d=>{d.open=false;d.querySelector('summary')?.focus();});});
 document.addEventListener('click',e=>{document.querySelectorAll('.collection-actions>details[open]').forEach(d=>{if(!d.contains(e.target))d.open=false;});});
 document.querySelectorAll('.collection-actions>details').forEach(d=>d.addEventListener('toggle',()=>{if(d.open)document.querySelectorAll('.collection-actions>details').forEach(other=>{if(other!==d)other.open=false;});}));
 document.body.addEventListener('htmx:beforeRequest',e=>{
  if(e.detail.elt===form && form.elements.format.value==='original' && e.detail.requestConfig?.parameters?.action!=='review'){
   e.preventDefault();window.location.assign(form.getAttribute('action')+'?original=1');
  }
  else if(e.detail.elt===form&&e.detail.requestConfig?.parameters?.action!=='review'){requested=true;status.replaceChildren();}
 });
 document.body.addEventListener('htmx:afterSwap',e=>{
  availability();
  if(e.detail.target===status){if(chooser)chooser.open=true;deliver();if(e.detail.requestConfig?.verb?.toLowerCase()==='post')refresh();}
  const tab=document.querySelector('#detail-content [data-active-tab]')?.dataset.activeTab;
  if(tab==='preview'||tab==='raw')document.querySelectorAll('.collection-preview .tabs>span').forEach(el=>el.remove());
 });
 document.body.addEventListener('htmx:afterRequest',e=>{
  if(e.detail.elt===form&&!e.detail.successful){status.textContent='Unable to prepare download. Please try again.';chooser.open=true;}
 });
 window.addEventListener('fluxport:progress',e=>{
  const job=status?.querySelector('[data-download-job]');
  if(job&&(e.detail.connected||e.detail.id===job.dataset.downloadJob)&&!pending){pending=setTimeout(()=>{pending=null;refresh();},150);}
  availability();
 });
 if(location.hash==='#download'&&chooser)chooser.open=true;
})();
