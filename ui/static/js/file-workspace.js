/* Keep the source table in place. Tools reuse their existing server forms. */
(() => {
 'use strict';
 const workspace=document.querySelector('[data-file-workspace]');if(!workspace)return;
 const id=workspace.dataset.fileWorkspace,base='/collections/'+id;
 const pane=document.getElementById('file-workspace-tool'),host=document.getElementById('workspace-tool-content');
 const heading=document.getElementById('workspace-tool-title'),notice=document.getElementById('workspace-tool-notice');
 const labels={clean:'Clean data',sql:'SQL query',export:'Export',insights:'Column summaries',quality:'Validate data',raw:'Raw JSON',schema:'Schema',api:'API settings',advanced:'Advanced changes'};
 const routes={clean:base+'/changes',sql:base+'/sql',export:base+'?tab=downloads',insights:base+'/insights',quality:base+'/quality',raw:base+'?tab=raw',schema:base+'?tab=schema',api:base+'?tab=configuration',advanced:'/pipelines?collection='+id};
 const cache=new Map();let active='',lastTrigger=null;
 let dataRequest;
 async function loadData(url,remember=true){
  dataRequest?.abort();const request=new AbortController();dataRequest=request;
  const detail=document.getElementById('detail-content'),feedback=document.getElementById('feedback');detail.setAttribute('aria-busy','true');
  try{
   const response=await fetch(url,{credentials:'same-origin',signal:request.signal});
   const doc=new DOMParser().parseFromString(await response.text(),'text/html'),body=doc.querySelector('#detail-content');
   if(!response.ok||!body)throw Error(doc.querySelector('[role=alert]')?.textContent||'Unable to update the table. Try again.');
   const scroll=detail.querySelector('.record-scroll')?.scrollLeft||0;
   detail.replaceChildren(...body.childNodes);
   const metadata=doc.querySelector('#collection-metadata');if(metadata)document.getElementById('collection-metadata').replaceWith(metadata);
   window.htmx?.process(detail);document.dispatchEvent(new CustomEvent('fluxport:data-mounted'));
   const table=detail.querySelector('.record-scroll');if(table)table.scrollLeft=scroll;
   if(remember){const next=new URL(url,location.href);history.pushState(history.state,'',base+next.search+(active?'#tool='+active:''));}
   feedback.textContent='';
  }catch(error){if(error.name!=='AbortError')feedback.textContent=error.message;}
  finally{if(dataRequest===request)detail.removeAttribute('aria-busy');}
 }
 function toolFor(url){
  if(url.origin!==location.origin)return '';
  if(url.pathname===base){return {downloads:'export',raw:'raw',schema:'schema',configuration:'api'}[url.searchParams.get('tab')]||'';}
  if(url.pathname.startsWith(base+'/'))return {changes:'clean',sql:'sql',insights:'insights',quality:'quality','export-check':'export'}[url.pathname.slice(base.length+1)]||'';
  if(url.pathname==='/sql')return 'sql';
  if(url.pathname==='/pipelines')return 'advanced';
  return '';
 }
 function message(text,error=false){notice.textContent=text;notice.toggleAttribute('data-error',error);}
 function saveCurrent(){if(active&&host.firstElementChild){const entry=cache.get(active);if(entry)entry.node=host.firstElementChild;}}
 function select(key){
  if(active!==key)saveCurrent();active=key;pane.hidden=false;workspace.classList.add('tool-open');heading.textContent=labels[key];
  workspace.querySelectorAll('.workspace-toolbar>a').forEach(link=>{const selected=link.dataset.workspaceTarget===key;link.classList.toggle('selected',selected);if(selected)link.setAttribute('aria-current','true');else link.removeAttribute('aria-current');});
  history.replaceState(history.state,'',location.pathname+location.search+'#tool='+key);
  workspace.querySelector('.workspace-more').open=false;
 }
 function mount(entry){
  host.replaceChildren(entry.node);message('');
  window.htmx?.process(entry.node);
  document.dispatchEvent(new CustomEvent('fluxport:tool-mounted',{detail:{tool:active}}));
  updateFormats();refreshDownload();
 }
 function close(){
  saveCurrent();active='';pane.hidden=true;workspace.classList.remove('tool-open');
  workspace.querySelectorAll('.workspace-toolbar>a').forEach(link=>{const selected=link.hasAttribute('data-workspace-data');link.classList.toggle('selected',selected);if(selected)link.setAttribute('aria-current','page');else link.removeAttribute('aria-current');});
  history.replaceState(history.state,'',location.pathname+location.search);message('');lastTrigger?.focus();
 }
 function extract(html,sourceURL=location.href){
  const doc=new DOMParser().parseFromString(html,'text/html');
  const root=doc.querySelector('main')||doc.body;
  root.querySelectorAll('script,.page-heading,.dataset-tabs,#collection-metadata,.activity-panel,.collection-tools,#feedback:empty').forEach(el=>el.remove());
  const node=document.createElement('div');node.className='workspace-tool-body';
  const content=root.querySelector('.dataset-workspace,.quality-layout')||root;
  node.append(...content.childNodes);
  node.querySelectorAll('a[href],form').forEach(el=>{
   const attribute=el.tagName==='FORM'?'action':'href',value=el.getAttribute(attribute)||'';
   if(!value.startsWith('#'))el.setAttribute(attribute,new URL(value||sourceURL,sourceURL).href);
  });
  // Native tool forms are enhanced by this workspace, including error responses.
  node.querySelectorAll('[hx-get],[hx-post],[hx-push-url],[hx-trigger],[hx-target],[hx-swap-oob]').forEach(el=>{
   for(const attr of [...el.attributes])if(attr.name.startsWith('hx-'))el.removeAttribute(attr.name);
  });
  const detail=node.querySelector('#detail-content');if(detail)detail.id='tool-detail-content';
  const cleanup=node.querySelector('[data-cleanup-form]');
  if(cleanup){
   const save=cleanup.querySelector('.cleanup-save'),toolbar=cleanup.querySelector('.cleanup-steps .panel-toolbar');
   if(save){const details=document.createElement('details'),summary=document.createElement('summary');summary.textContent='Save steps for later';details.className='workspace-save-steps';details.append(summary);save.before(details);details.append(save);const preview=save.querySelector('[value=preview]');if(preview&&toolbar)toolbar.append(preview);}
   const join=cleanup.querySelector('.join-builder');if(join)cleanup.append(join);
  }
  return node;
 }
 async function load(key,url,options={}){
  const entry=cache.get(key)||{};entry.request=(entry.request||0)+1;const request=entry.request;cache.set(key,entry);
  if(active===key){message(options.method==='POST'?'Working…':'Opening '+labels[key].toLowerCase()+'…');pane.setAttribute('aria-busy','true');}
  try{
   const response=await fetch(url,{credentials:'same-origin',...options});
   if(response.headers.get('Content-Disposition')?.includes('attachment')){
    // SQL previews are bounded by the server. Original and job downloads bypass fetch.
    const blob=await response.blob(),link=document.createElement('a'),object=URL.createObjectURL(blob);
    link.href=object;link.download=response.headers.get('Content-Disposition').match(/filename="?([^";]+)/)?.[1]||'query-results';link.click();setTimeout(()=>URL.revokeObjectURL(object),1000);message('Download started.');return;
   }
   const html=await response.text();if(request!==entry.request)return;
   const node=extract(html,response.url);
   if(!node.textContent.trim())throw Error('The tool returned an empty response. Please try again.');
   if(new URL(response.url).pathname==='/login'){location.assign(response.url);return;}
   if(!response.ok&&!node.querySelector('form')&&entry.node){if(active===key)message(node.textContent.trim(),true);return;}
   const resultKey=toolFor(new URL(response.url))||key;
   const result={node,url:response.url,request:entry.request};cache.set(resultKey,result);
   if(active===key){select(resultKey);mount(result);if(!response.ok)message('Check the highlighted error and try again.',true);}
  }catch(error){if(active===key){message(error.message||'Unable to load this tool. Try again.',true);const retry=document.createElement('button');retry.type='button';retry.className='button small';retry.textContent='Try again';retry.addEventListener('click',()=>load(key,url,options));notice.append(' ',retry);}}
  finally{pane.removeAttribute('aria-busy');}
 }
 function open(key,url,trigger,force=false){
  if(url&&key==='sql'){const scoped=new URL(url,location.href);scoped.pathname=base+'/sql';url=scoped.href;}
  lastTrigger=trigger||lastTrigger;select(key);
  const entry=cache.get(key);
  if(entry?.node&&!force){mount(entry);}else{host.replaceChildren();load(key,url||routes[key]);}
  heading.focus({preventScroll:true});
  if(matchMedia('(max-width:1000px)').matches)pane.scrollIntoView({behavior:'smooth',block:'start'});
 }
 document.addEventListener('click',event=>{
  if(event.button!==0||event.ctrlKey||event.metaKey||event.shiftKey||event.altKey)return;
  const link=event.target.closest('a');
  if(event.target.closest('[data-close-workspace-tool]')){close();return;}
  if(!link)return;
  if(link.hasAttribute('data-workspace-data')){event.preventDefault();close();return;}
  const url=new URL(link.href,location.href),key=link.dataset.workspaceTarget||toolFor(url);
  if(key&&(link.closest('[data-file-workspace]')||pane.contains(link))){event.preventDefault();event.stopImmediatePropagation();open(key,url.href,link,pane.contains(link)||(!link.dataset.workspaceTarget));}
  else if(pane.contains(link)&&url.origin===location.origin&&url.pathname===base&&!url.search){event.preventDefault();close();}
  else if(link.closest('#detail-content')&&url.origin===location.origin&&[base,base+'/workbench'].includes(url.pathname)){event.preventDefault();event.stopImmediatePropagation();loadData(url.href);}
 },true);
 pane.addEventListener('submit',event=>{
  const form=event.target,button=event.submitter;
  if(event.defaultPrevented||!(form instanceof HTMLFormElement))return;
  event.preventDefault();
  const data=new FormData(form,button),url=new URL(button?.getAttribute('formaction')||form.getAttribute('action')||location.href,location.href);
  const method=(button?.getAttribute('formmethod')||form.getAttribute('method')||'GET').toUpperCase();
  if(data.get('format')==='original'&&!data.get('pipeline')&&data.get('action')!=='review'&&url.pathname===base+'/export'){
   location.assign(base+'/export?original=1');return;
  }
  const params=new URLSearchParams();for(const [key,value] of data){if(typeof value==='string')params.append(key,value);}
  if(method==='GET'){url.search=params.toString();load(active,url.href);}
  else{
   const buttons=[...form.querySelectorAll('button[type=submit],button:not([type])')];buttons.forEach(b=>b.disabled=true);
   load(active,url.href,{method,body:params}).finally(()=>buttons.forEach(b=>b.disabled=false));
  }
 });
 // Exporting a filtered table also belongs to this file's workspace.
 workspace.addEventListener('submit',event=>{
  const form=event.target;
  if(event.defaultPrevented)return;
  if(form.matches('[data-sheet-query]')){event.preventDefault();const url=new URL(base,location.href),params=new URLSearchParams();for(const [key,value] of new FormData(form,event.submitter))if(typeof value==='string')params.append(key,value);url.search=params.toString();loadData(url.href);return;}
  if(!(form instanceof HTMLFormElement)||pane.contains(form)||form.getAttribute('action')!==base+'/workbench/export')return;
  event.preventDefault();const body=new URLSearchParams();for(const [key,value] of new FormData(form,event.submitter))if(typeof value==='string')body.append(key,value);
  select('export');host.replaceChildren();load('export',base+'/workbench/export',{method:'POST',body});
 });
 function updateFormats(){host.querySelectorAll('[data-export-format],#collection-download-format').forEach(select=>{
  const target=select.closest('form')?.querySelector('[data-download-note]');if(target){target.textContent=select.selectedOptions[0]?.dataset.formatNote||'';target.hidden=!target.textContent;}
 });}
 pane.addEventListener('change',event=>{
  if(event.target.matches('[data-download-steps]')){const select=event.target.form.elements.format,original=select.querySelector('[value=original]');original.disabled=!!event.target.value;if(original.disabled&&select.value==='original')select.value='json';}
  updateFormats();
 });
 let refreshing=false,refreshAgain=false;
 async function refreshDownload(){
  const status=host.querySelector('#collection-download-status'),job=status?.querySelector('[data-download-job]');
  if(!job)return;if(refreshing){refreshAgain=true;return;}refreshing=true;
  try{const response=await fetch('/jobs?download_view=compact&id='+encodeURIComponent(job.dataset.downloadJob),{headers:{'HX-Request':'true'}});if(response.ok&&status.isConnected){const node=extract(await response.text());status.replaceChildren(...node.childNodes);}}
  catch{}finally{refreshing=false;if(refreshAgain){refreshAgain=false;queueMicrotask(refreshDownload);}}
 }
 window.addEventListener('fluxport:progress',event=>{const job=host.querySelector('[data-download-job]');if(job&&(event.detail.connected||event.detail.id===job.dataset.downloadJob))refreshDownload();});
 document.addEventListener('keydown',event=>{if(event.key==='Escape'&&active&&!event.target.closest('dialog[open]')){event.preventDefault();close();}});
 window.addEventListener('popstate',()=>{loadData(location.href,false);const key=location.hash.match(/^#tool=(\w+)$/)?.[1];if(key&&routes[key])open(key,routes[key]);else close();});
 const initial=location.hash.match(/^#tool=(\w+)$/)?.[1];if(initial&&routes[initial])open(initial,routes[initial]);
})();
