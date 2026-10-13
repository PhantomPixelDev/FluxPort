/* Keep the source table in place. Tools reuse their existing server forms. */
(() => {
 'use strict';
 const workspace=document.querySelector('[data-file-workspace]');if(!workspace)return;
 const id=workspace.dataset.fileWorkspace,base='/collections/'+id;
 const pane=document.getElementById('file-workspace-tool'),host=document.getElementById('workspace-tool-content');
 const heading=document.getElementById('workspace-tool-title'),notice=document.getElementById('workspace-tool-notice');
 const labels={clean:'Clean',sql:'SQL query',export:'Convert / Download',insights:'Column summaries',quality:'Validate data',raw:'Inspect JSON',schema:'Schema',api:'API settings',advanced:'Advanced changes',settings:'File settings',views:'Saved views',history:'Processing history'};
 const routes={clean:base+'/changes',sql:base+'/sql',export:base+'?tab=downloads',insights:base+'/insights',quality:base+'/quality',raw:base+'?tab=raw',schema:base+'?tab=schema',api:base+'?tab=configuration',advanced:'/pipelines?collection='+id,settings:base+'#file-settings',views:base+'#saved-views',history:base+'#activity'};
 const cache=new Map();let active='',lastTrigger=null;
 const result=document.getElementById('workspace-result'),caption=document.getElementById('workspace-data-caption');
 let exportChoice,downloadIntent=null;const downloaded=new Set();
 function showData(view){
  if(view==='result'&&!result.firstElementChild)view='original';workspace.dataset.dataView=view;
  document.getElementById('detail-content').hidden=view==='result';result.hidden=view!=='result';
  workspace.querySelectorAll('[data-workspace-view]').forEach(button=>button.setAttribute('aria-pressed',String(button.dataset.workspaceView===view)));
  caption.textContent=view==='result'?'Preview · '+(result.querySelector('[data-preview-count]')?.dataset.previewCount||0)+' rows':'';
 }
 function cleanDraft(){
  const form=cache.get('clean')?.node?.querySelector('[data-cleanup-form]');if(!form)return '';
  const steps=[...form.querySelectorAll('[name=steps]')].map(input=>input.value);if(!steps.length)return '';
  return '{"version":1,"source":'+JSON.stringify(id)+',"steps":['+steps.join(',')+'],"validation":'+(form.elements.validation.value||'[]')+'}';
 }
 function syncClean(node){
  const preview=node.workspacePreview,hasSteps=!!node.querySelector('[name=steps]');
  preview?.querySelectorAll('[data-preview-kind=json]').forEach(cell=>{
   if(cell.querySelector('button'))return;const value=cell.textContent,button=document.createElement('button');button.type='button';button.className='cell-json';button.textContent='JSON';button.setAttribute('aria-label','Inspect cleaned JSON value');button.addEventListener('click',()=>inspectCleanedValue(value));cell.replaceChildren(button);cell.removeAttribute('tabindex');
  });
  result.replaceChildren(...(preview?[preview]:[]));workspace.querySelector('.workspace-data-heading').hidden=!preview||!hasSteps;workspace.querySelector('[data-workspace-view=result]').hidden=!preview||!hasSteps;
  showData(preview&&hasSteps?'result':'original');
 }
 function inspectCleanedValue(value){
  let dialog=workspace.querySelector('.workspace-value-dialog');
  if(!dialog){dialog=document.createElement('dialog');dialog.className='workspace-value-dialog';dialog.setAttribute('aria-label','Cleaned JSON value');dialog.innerHTML='<form method="dialog"><button class="button small" aria-label="Close cleaned value">Close</button></form><h2>JSON value</h2><textarea class="mono" rows="12" readonly aria-label="Complete cleaned JSON value"></textarea><button class="button" type="button" data-copy-cleaned>Copy value</button><span role="status"></span>';workspace.append(dialog);dialog.querySelector('[data-copy-cleaned]').addEventListener('click',async()=>{const input=dialog.querySelector('textarea');try{await navigator.clipboard.writeText(input.value);dialog.querySelector('[role=status]').textContent='Copied';}catch{input.focus();input.select();dialog.querySelector('[role=status]').textContent='Press Ctrl+C or Command+C to copy.';}});}
  dialog.querySelector('textarea').value=value;dialog.querySelector('[role=status]').textContent='';dialog.showModal();
 }
 function syncExport(){
  const form=host.querySelector('[data-collection-download]');if(!form)return;
  const source=form.querySelector('[data-download-source]'),draft=cleanDraft();
  if(draft){if(!source.querySelector('[value=current]')){const option=new Option('Current cleaned data','current');source.options.add(option,1);}form.elements.draft.value=draft;}
  if(exportChoice){source.value=exportChoice==='current'&&source.querySelector('[value=current]')?'current':'original';exportChoice=undefined;}
  source.closest('.download-source-options').hidden=source.options.length===1;
  updateFormats();
 }
 const more=workspace.querySelector('.workspace-more'),moreSummary=more.querySelector('summary');
 workspace.classList.add('workspace-ready');
 const iconSource=workspace.querySelector('.workspace-toolbar use')?.getAttribute('href').split('#')[0]||'/static/icons/icons.svg';
 const theme=document.getElementById('theme-toggle');if(theme){theme.classList.add('workspace-theme');theme.title='Switch color theme';theme.innerHTML='<svg class="workspace-action-icon" aria-hidden="true"><use href="/static/icons/icons.svg#sun"/></svg><span class="workspace-tool-label">Switch theme</span>';more.querySelector('[data-workspace-theme]').append(theme);}
 const signout=document.querySelector('.topbar-actions form');if(signout)more.querySelector('[data-workspace-theme]').append(signout);
 theme?.querySelector('use').setAttribute('href',iconSource+'#sun');
 const mobileTools=matchMedia('(max-width:767px)'),extraTools=more.querySelector('.workspace-extra-tools'),desktopTools=workspace.querySelector('.workspace-desktop-tools');
 function arrangeTools(){more.open=false;(mobileTools.matches?more:desktopTools).append(extraTools);}
 mobileTools.addEventListener('change',arrangeTools);arrangeTools();
 new ResizeObserver(entries=>workspace.style.setProperty('--workspace-header-height',(entries[0].target.offsetHeight+12)+'px')).observe(workspace.querySelector('.workspace-header'));
 function staticTool(key){
  const node=document.createElement('div');node.className='workspace-tool-body';
  const selectors={settings:'.collection-tools',views:'#detail-content .sheet-tools',history:'.activity-panel'};
  const source=workspace.querySelector(selectors[key]);if(!source)return cache.get(key)?.node;
  source.querySelector(':scope>summary')?.remove();node.append(...source.childNodes);source.remove();
  if(key==='settings'){
   const editing=workspace.querySelector('#detail-content .source-editing');if(editing)node.prepend(editing);
   node.querySelectorAll('[hx-post]').forEach(form=>{for(const attr of [...form.attributes])if(attr.name.startsWith('hx-'))form.removeAttribute(attr.name);});
   const refresh=node.querySelector('form[action$="/refresh"]');if(refresh){const input=document.createElement('input');input.type='hidden';input.name='return_collection';input.value='1';refresh.append(input);}
  }
  return node;
 }
 function markTool(key){
  workspace.querySelectorAll('.workspace-toolbar a').forEach(link=>{const selected=key?link.dataset.workspaceTarget===key:link.hasAttribute('data-workspace-data');link.classList.toggle('selected',selected);if(selected)link.setAttribute('aria-current','page');else link.removeAttribute('aria-current');});
  moreSummary.toggleAttribute('data-active',!!extraTools.querySelector('a[aria-current]'));
 }
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
   const types=detail.querySelector('[data-show-types]');if(types)types.checked=workspace.classList.contains('show-column-types');
   cache.delete('views');
   if(active==='views'){const node=staticTool('views');if(node){cache.set('views',{node});mount({node});}}
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
  markTool(key);
  history.replaceState(history.state,'',location.pathname+location.search+'#tool='+key);
  more.open=false;
 }
 function mount(entry){
  host.replaceChildren(entry.node);message('');
  if(active==='clean')syncClean(entry.node);
  if(active==='export')syncExport();
  window.htmx?.process(entry.node);
  document.dispatchEvent(new CustomEvent('fluxport:tool-mounted',{detail:{tool:active}}));
  updateFormats();deliverDownload();renderDownload();refreshDownload();
 }
 function close(){
  saveCurrent();active='';pane.hidden=true;workspace.classList.remove('tool-open');
  markTool('');
  history.replaceState(history.state,'',location.pathname+location.search);message('');if(lastTrigger?.isConnected)(mobileTools.matches&&extraTools.contains(lastTrigger)?moreSummary:lastTrigger).focus();
 }
 function extract(html,sourceURL=location.href){
  const doc=new DOMParser().parseFromString(html,'text/html');
  const root=doc.querySelector('main')||doc.body;
  root.querySelectorAll('script,.workspace-header,.page-heading,.dataset-tabs,#collection-metadata,.activity-panel,.collection-tools,#feedback:empty').forEach(el=>el.remove());
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
   const reuse=node.querySelector('.reuse-steps');reuse?.remove();
   node.querySelector('.workspace-shortcuts')?.remove();
   const preview=cleanup.querySelector('[data-cleanup-preview]');if(preview){preview.querySelector('.cleanup-original-sample')?.remove();node.workspacePreview=preview;preview.remove();}
   const advanced=document.createElement('details'),summary=document.createElement('summary');summary.textContent='Advanced';advanced.className='workspace-clean-advanced';advanced.append(summary);cleanup.append(advanced);
   if(reuse)advanced.append(reuse);
   const save=cleanup.querySelector('.cleanup-save');if(save){advanced.append(save);const previewButton=save.querySelector('[value=preview]');if(previewButton)previewButton.hidden=true;}
   const reset=cleanup.querySelector('[value=reset]');if(reset)advanced.append(reset);
   for(const selector of ['.saved-changes','.join-builder','.dataset-advanced']){const item=node.querySelector(selector);if(item)advanced.append(item);}
   const steps=cleanup.querySelector('.cleanup-steps');if(!steps.querySelector('[name=steps]'))steps.hidden=true;
   cleanup.querySelector('.cleanup-download')?.remove();
   if(node.workspacePreview){const complete=node.workspacePreview.querySelector('.cleanup-complete-sample');if(complete)advanced.append(complete);}
   cleanup.querySelector('[data-cleanup-help]').hidden=true;
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
   if(resultKey==='export'&&active!==key){const status=node.querySelector('#collection-download-status');deliverDownload(status);renderDownload(status);refreshDownload();}
   if(active===key){select(resultKey);mount(result);if(!response.ok)message('Check the highlighted error and try again.',true);else if(options.method==='POST'&&resultKey==='clean'&&result.node.workspacePreview&&matchMedia('(max-width:1000px)').matches)document.getElementById('workspace-result').scrollIntoView({block:'start',behavior:'smooth'});}
  }catch(error){if(active===key){message(error.message||'Unable to load this tool. Try again.',true);const retry=document.createElement('button');retry.type='button';retry.className='button small';retry.textContent='Try again';retry.addEventListener('click',()=>load(key,url,options));notice.append(' ',retry);}}
  finally{pane.removeAttribute('aria-busy');}
 }
 function open(key,url,trigger,force=false){
  if(key==='export')exportChoice=workspace.dataset.dataView==='result'?'current':'original';
  if(key==='sql'||key==='quality'||key==='insights')showData('original');
  if(url&&key==='sql'){const scoped=new URL(url,location.href);scoped.pathname=base+'/sql';url=scoped.href;}
  lastTrigger=trigger||lastTrigger;select(key);
  if(['settings','views','history'].includes(key)){const node=staticTool(key);if(node)cache.set(key,{node});}
  if(key==='raw'){
   const sample=cache.get('clean')?.node?.querySelector('.cleanup-complete-sample pre');
   if(workspace.dataset.dataView==='result'&&sample){const node=document.createElement('div'),label=document.createElement('p');label.className='muted small-text';label.textContent='Cleaned preview · '+(result.querySelector('[data-preview-count]')?.dataset.previewCount||0)+' rows';node.className='workspace-tool-body';node.append(label,sample.cloneNode(true));cache.set('raw',{node});force=false;}
   else force=true;
  }
  const entry=cache.get(key);
  if(entry?.node&&!force){mount(entry);}else{host.replaceChildren();load(key,url||routes[key]);}
  heading.focus({preventScroll:true});
  if(matchMedia('(max-width:1000px)').matches)pane.scrollIntoView({behavior:'smooth',block:'start'});
 }
 document.addEventListener('click',event=>{
  if(more.open&&!more.contains(event.target))more.open=false;
  workspace.querySelectorAll('.toolbar-disclosure[open]').forEach(disclosure=>{if(!disclosure.contains(event.target))disclosure.open=false;});
  if(event.button!==0||event.ctrlKey||event.metaKey||event.shiftKey||event.altKey)return;
  const link=event.target.closest('a');
  if(event.target.closest('[data-workspace-edit-clean]')){open('clean',routes.clean,event.target);return;}
  const view=event.target.closest('[data-workspace-view]');if(view){showData(view.dataset.workspaceView);return;}
  if(event.target.closest('[data-workspace-download-clean]')){showData('result');open('export',routes.export,event.target);return;}
  if(event.target.closest('[data-close-workspace-tool]')){close();return;}
  if(!link)return;
  if(link.hasAttribute('data-workspace-data')){event.preventDefault();close();return;}
  const url=new URL(link.href,location.href),key=link.dataset.workspaceTarget||toolFor(url);
  if(key&&(link.closest('[data-file-workspace]')||pane.contains(link))){event.preventDefault();event.stopImmediatePropagation();open(key,url.href,link,pane.contains(link)||(!link.dataset.workspaceTarget));}
  else if(pane.contains(link)&&url.origin===location.origin&&url.pathname===base&&!url.search){event.preventDefault();close();}
  else if((link.closest('#detail-content')||(active==='views'&&pane.contains(link)))&&url.origin===location.origin&&[base,base+'/workbench'].includes(url.pathname)){event.preventDefault();event.stopImmediatePropagation();loadData(url.href);}
 },true);
 workspace.addEventListener('toggle',event=>{
  const disclosure=event.target;if(!disclosure.matches('.workspace-more,.toolbar-disclosure')||!disclosure.open)return;
  [more,...workspace.querySelectorAll('.toolbar-disclosure')].forEach(other=>{if(other!==disclosure)other.open=false;});
 },true);
 pane.addEventListener('submit',event=>{
  const form=event.target,button=event.submitter;
  if(event.defaultPrevented||!(form instanceof HTMLFormElement))return;
  if(active==='settings')return;
  event.preventDefault();
  const data=new FormData(form,button),url=new URL(button?.getAttribute('formaction')||form.getAttribute('action')||location.href,location.href);
  const method=(button?.getAttribute('formmethod')||form.getAttribute('method')||'GET').toUpperCase();
  if(data.get('format')==='original'&&(!data.get('dataset')||data.get('dataset')==='original')&&!data.get('pipeline')&&data.get('action')!=='review'&&url.pathname===base+'/export'){
   downloadIntent=null;location.assign(base+'/export?original=1');return;
  }
  if(url.pathname.endsWith('/cancel'))downloadIntent=null;
  else if(data.get('action')!=='review'&&(url.pathname===base+'/export'||url.pathname===base+'/workbench/export'||url.pathname.endsWith('/retry'))){downloadIntent={job:null};host.querySelector('#collection-download-status')?.replaceChildren();host.querySelector('.download-chooser')?.removeAttribute('data-editing');}
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
  downloadIntent={job:null};select('export');host.replaceChildren();load('export',base+'/workbench/export',{method:'POST',body});
 });
 function updateFormats(){host.querySelectorAll('[data-export-format],#collection-download-format').forEach(select=>{
  const form=select.closest('form'),source=form?.querySelector('[data-download-source]');
  if(source){const original=select.querySelector('[value=original]');original.disabled=source.value!=='original';if(original.disabled&&select.value==='original')select.value='json';const button=form.querySelector('[data-download-submit]');if(button){button.textContent=select.value==='original'?'Download':'Convert & download';button.disabled=!select.selectedOptions[0]||select.selectedOptions[0].disabled;}const scope=form.querySelector('[data-download-scope]');if(scope)scope.textContent=source.value==='original'?'Downloads all rows.':'Downloads all cleaned rows. Original file stays unchanged.';const context=form.querySelector('[data-download-context]');if(context)context.textContent=(source.value==='original'?'Original data':source.value==='current'?'Cleaned data':source.selectedOptions[0].textContent)+' · Change';}
  const target=select.closest('form')?.querySelector('[data-download-note]');if(target){target.textContent=select.selectedOptions[0]?.dataset.formatNote||'';target.hidden=!target.textContent;}
 });}
 pane.addEventListener('change',event=>{
  updateFormats();
 });
 function deliverDownload(status=host.querySelector('#collection-download-status')){
  const job=status?.querySelector('[data-download-job]');if(!job||!downloadIntent)return;
  if(!downloadIntent.job)downloadIntent.job=job.dataset.downloadJob;
  if(downloadIntent.job!==job.dataset.downloadJob)return;
  if(['failed','cancelled','interrupted'].includes(job.dataset.downloadState)){downloadIntent=null;return;}
  const link=job.querySelector('a[href]');if(job.dataset.downloadState==='ready'&&link&&!downloaded.has(job.dataset.downloadJob)){downloaded.add(job.dataset.downloadJob);downloadIntent=null;link.click();}
  renderDownload(status);
 }
 function renderDownload(status=host.querySelector('#collection-download-status')){
  const chooser=status?.closest('.download-chooser'),job=status?.querySelector('[data-download-job]');if(!chooser||!job)return;
  const form=chooser.querySelector('[data-collection-download]');if(!form||chooser.hasAttribute('data-editing'))return;
  if(downloaded.has(job.dataset.downloadJob)){
   job.querySelector('strong').textContent='Download started';const link=job.querySelector('a[href]');if(link)link.textContent='Download again';
  }
  form.hidden=true;status.hidden=false;
  let change=chooser.querySelector('[data-download-options]');
  if(!change){change=document.createElement('button');change.type='button';change.className='button small ghost';change.dataset.downloadOptions='';change.textContent='Another format';change.addEventListener('click',()=>{chooser.dataset.editing='';form.hidden=false;status.hidden=true;change.hidden=true;});chooser.append(change);}
  change.hidden=['queued','running','staged'].includes(job.dataset.downloadState);
 }
 let refreshing=false,refreshAgain=false;
 async function refreshDownload(){
  const status=host.querySelector('#collection-download-status')||cache.get('export')?.node?.querySelector('#collection-download-status'),job=status?.querySelector('[data-download-job]');
  if(!job)return;if(refreshing){refreshAgain=true;return;}refreshing=true;
  try{const response=await fetch('/jobs?download_view=compact&id='+encodeURIComponent(job.dataset.downloadJob),{headers:{'HX-Request':'true'}});if(response.ok&&(status.isConnected||cache.get('export')?.node?.contains(status))){const node=extract(await response.text());status.replaceChildren(...node.childNodes);deliverDownload(status);renderDownload(status);}}
  catch{}finally{refreshing=false;if(refreshAgain){refreshAgain=false;queueMicrotask(refreshDownload);}}
 }
 window.addEventListener('fluxport:progress',event=>{const job=host.querySelector('[data-download-job]')||cache.get('export')?.node?.querySelector('[data-download-job]');if(job&&(event.detail.connected||event.detail.id===job.dataset.downloadJob))refreshDownload();});
 document.addEventListener('keydown',event=>{
  if(event.key!=='Escape'||event.target.closest('dialog[open]'))return;
  const disclosure=[more,...workspace.querySelectorAll('.toolbar-disclosure')].find(el=>el.open);
  if(disclosure){event.preventDefault();disclosure.open=false;disclosure.querySelector('summary').focus();return;}
  if(active){event.preventDefault();close();}
 });
 window.addEventListener('popstate',()=>{loadData(location.href,false);const key=location.hash.match(/^#tool=(\w+)$/)?.[1];if(key&&routes[key])open(key,routes[key]);else close();});
 const initial=location.hash.match(/^#tool=(\w+)$/)?.[1];if(initial&&routes[initial])open(initial,routes[initial]);
})();
