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
 const sourceArea=workspace.querySelector('.file-workspace-data'),sourceDisclosure=document.getElementById('workspace-original'),main=workspace.querySelector('.workspace-main');
 const inspectViews=['insights','quality','schema','raw'],fileViews=['settings','api','views','history'];
 const sessionKey='fluxport-workspace-'+id;let viewState={};try{const saved=JSON.parse(sessionStorage.getItem(sessionKey)||'{}');if(saved&&typeof saved==='object'&&!Array.isArray(saved)&&(!saved.version||saved.version===2))viewState=saved;}catch{}viewState.version=2;
 if(!viewState.scroll||typeof viewState.scroll!=='object'||Array.isArray(viewState.scroll))viewState.scroll={};
 if(!viewState.expandedOriginal||typeof viewState.expandedOriginal!=='object')viewState.expandedOriginal={};
 if(!/^[a-f0-9]{24}$/.test(viewState.resultID||''))delete viewState.resultID;
 function storedURL(value,fallback){try{const url=new URL(value,location.href);if(url.origin===location.origin&&[base,base+'/workbench'].includes(url.pathname))return url.href;}catch{}return fallback;}
 let originalURL=storedURL(viewState.originalURL,base),resultURL=storedURL(viewState.resultURL,''),preparedID='',preparedRequest;
 function tableRoot(view=workspace.dataset.dataView){return view==='result'?result:document.getElementById('detail-content');}
 function tableQuery(view){return tableRoot(view)?.querySelector('[data-sheet-query]')?.elements.query.value||'{}';}
 function sourceGeneration(){return document.getElementById('detail-content').querySelector('.sheet-data')?.dataset.sourceGeneration||workspace.dataset.sourceGeneration||'';}
 function rememberWorkspace(captureScroll=true){
  const sql=host.querySelector('#sql-editor');if(sql){viewState.sql=sql.value;viewState.engine=sql.form.elements.engine?.value||'auto';}
  viewState.scroll ||= {};if(captureScroll)viewState.scroll[active||'data']=window.scrollY;
  Object.assign(viewState,{version:2,active,dataView:workspace.dataset.dataView||'original',originalURL,resultURL,resultID:preparedID});
  try{sessionStorage.setItem(sessionKey,JSON.stringify(viewState));}catch{}
 }
 function rememberSQL(node){const sql=node.querySelector('#sql-editor');if(!sql)return;sql.addEventListener('input',rememberWorkspace);sql.form.elements.engine?.addEventListener('change',rememberWorkspace);}
 function restoreSQL(node){const sql=node.querySelector('#sql-editor');if(!sql)return;if(!node.dataset.keepSQL&&typeof viewState.sql==='string'){sql.value=viewState.sql;if(sql.form.elements.engine&&viewState.engine)sql.form.elements.engine.value=viewState.engine;sql.dispatchEvent(new Event('input',{bubbles:true}));}rememberSQL(node);}
 function syncCleanedColumns(){
  const field=cache.get('clean')?.node?.querySelector('[name=field]'),choices=result.querySelector('datalist');if(!field||!choices)return;
  const previous=field.value,first=field.options[0].cloneNode(true);field.replaceChildren(first);
  for(const choice of choices.options)field.add(new Option(choice.value,choice.value));
  if([...field.options].some(option=>option.value===previous))field.value=previous;
 }
 let preparingRefresh=false,prepareAgain=false;
 async function refreshPrepared(){
  const requested=preparedID;if(!requested)return;if(preparingRefresh){prepareAgain=true;return;}preparingRefresh=true;
  preparedRequest?.abort();const controller=new AbortController();preparedRequest=controller;
  try{
   const url=new URL(base+'/cleaned-results/'+requested,location.href),view=new URL(resultURL||base,location.href);
   for(const key of ['query','q','page','limit'])if(view.searchParams.has(key))url.searchParams.set(key,view.searchParams.get(key));
   const response=await fetch(url,{headers:{'HX-Request':'true'},signal:controller.signal});
   const doc=new DOMParser().parseFromString(await response.text(),'text/html'),node=doc.querySelector('[data-cleaned-dataset]');
   if(requested!==preparedID)return;
    if(!response.ok||!node){result.replaceChildren();const warning=document.createElement('div');warning.className='notice';warning.textContent='This cleaned result is unavailable. Open Clean to prepare it again.';const retry=document.createElement('button');retry.type='button';retry.className='button small';retry.textContent='Prepare again';retry.addEventListener('click',()=>{preparedID='';resultURL='';open('clean',base+'/changes',retry,true);});warning.append(' ',retry);result.append(warning);layout();return;}
   if(node.querySelector('.sheet-data')){
    const viewing=result.dataset.restoreView||workspace.dataset.dataView;delete result.dataset.restoreView;result.replaceChildren(node);syncCleanedColumns();showData(viewing);
   }else{
    const next=node.querySelector('[data-prepared-result]'),current=result.querySelector('[data-prepared-result]');
    if(next){if(current)current.replaceWith(next);else result.prepend(next);}
   }
  }catch(error){if(error.name!=='AbortError'){const status=result.querySelector('[data-prepared-result]');if(status)status.setAttribute('data-refresh-error','true');}}
  finally{preparingRefresh=false;if(prepareAgain){prepareAgain=false;queueMicrotask(refreshPrepared);}}
 }

 const sqlResult=document.getElementById('workspace-sql-result');
 const result=document.getElementById('workspace-result'),caption=document.getElementById('workspace-data-caption');
 let exportChoice,downloadIntent=null;const downloaded=new Set();
 function layout(){
  const view=active||'data',clean=active==='clean',sql=active==='sql',data=!active;
  main.dataset.activeView=view;
  sourceArea.hidden=!(clean||sql||(data&&workspace.dataset.dataView!=='result'));
  if(data)sourceDisclosure.open=true;
  result.hidden=!result.firstElementChild||!(clean||(data&&workspace.dataset.dataView==='result'));
  sqlResult.hidden=!sql||!sqlResult.firstElementChild;
  workspace.querySelector('.workspace-data-heading').hidden=!data||!result.firstElementChild;
  workspace.querySelectorAll('[data-workspace-group]').forEach(nav=>nav.hidden=nav.dataset.workspaceGroup==='inspect'?!inspectViews.includes(view):!fileViews.includes(view));
 }
 sourceDisclosure.addEventListener('toggle',()=>{if(active==='clean'||active==='sql'){viewState.expandedOriginal ||= {};viewState.expandedOriginal[active]=sourceDisclosure.open;rememberWorkspace();}});
 function showData(view){
  if(view==='result'&&!result.firstElementChild)view='original';workspace.dataset.dataView=view;
  document.getElementById('detail-content').hidden=false;
  layout();
  workspace.querySelectorAll('[data-workspace-view]').forEach(button=>{button.setAttribute('aria-pressed',String(button.dataset.workspaceView===view));if(button.dataset.workspaceView==='result')button.value=preparedID;});
  caption.textContent=view==='result'?'Cleaned data':'Original data';
  rememberWorkspace();document.dispatchEvent(new CustomEvent('fluxport:data-mounted'));
 }
 function cleanDraft(){
  const form=cache.get('clean')?.node?.querySelector('[data-cleanup-form]');if(!form)return '';
  const steps=[...form.querySelectorAll('[name=steps]')].map(input=>input.value);if(!steps.length)return '';
  return '{"version":1,"source":'+JSON.stringify(id)+',"input_query":'+(form.elements.scope.value==='entire'?'null':form.elements.input_query.value||'{}')+',"steps":['+steps.join(',')+'],"validation":'+(form.elements.validation.value||'[]')+'}';
 }
 function syncClean(node){
  const preview=node.workspacePreview,hasSteps=!!node.querySelector('[name=steps]');
  const status=preview?.querySelector('[data-prepared-result]')||node.workspaceStatus,nextID=hasSteps?(status?.dataset.preparedResult||''):'';
  if(nextID&&nextID===preparedID&&result.querySelector('.sheet-data')){syncCleanedColumns();showData('result');return;}
  delete result.dataset.restoreView;preparedID=nextID;resultURL=nextID?base+'?result_id='+encodeURIComponent(nextID):'';preparedRequest?.abort();
  preview?.querySelectorAll('[data-preview-kind=json]').forEach(cell=>{
   if(cell.querySelector('button'))return;const value=cell.textContent,button=document.createElement('button');button.type='button';button.className='cell-json';button.textContent='JSON';button.setAttribute('aria-label','Inspect cleaned JSON value');button.addEventListener('click',()=>inspectCleanedValue(value));cell.replaceChildren(button);cell.removeAttribute('tabindex');
  });
  result.replaceChildren(...(preview?[preview]:[]));if(status&&!preview?.contains(status))result.prepend(status);
  workspace.querySelector('.workspace-data-heading').hidden=!hasSteps;workspace.querySelector('[data-workspace-view=result]').hidden=!hasSteps;
  showData(hasSteps&&result.firstElementChild?'result':'original');refreshPrepared();
 }
 function inspectCleanedValue(value){
  let dialog=workspace.querySelector('.workspace-value-dialog');
  if(!dialog){dialog=document.createElement('dialog');dialog.className='workspace-value-dialog';dialog.setAttribute('aria-label','Cleaned JSON value');dialog.innerHTML='<form method="dialog"><button class="button small" aria-label="Close cleaned value">Close</button></form><h2>JSON value</h2><textarea class="mono" rows="12" readonly aria-label="Complete cleaned JSON value"></textarea><button class="button" type="button" data-copy-cleaned>Copy value</button><span role="status"></span>';workspace.append(dialog);dialog.querySelector('[data-copy-cleaned]').addEventListener('click',async()=>{const input=dialog.querySelector('textarea');try{await navigator.clipboard.writeText(input.value);dialog.querySelector('[role=status]').textContent='Copied';}catch{input.focus();input.select();dialog.querySelector('[role=status]').textContent='Press Ctrl+C or Command+C to copy.';}});}
  dialog.querySelector('textarea').value=value;dialog.querySelector('[role=status]').textContent='';dialog.showModal();
 }
 function syncExport(){
  const form=host.querySelector('[data-collection-download]');if(!form)return;
  const source=form.querySelector('[data-download-source]'),draft=cleanDraft();
  if(draft||preparedID){if(!source.querySelector('[value=current]')){const option=new Option('Current cleaned data','current');source.options.add(option,1);}form.elements.draft.value=draft;}
  if(exportChoice){source.value=exportChoice==='current'&&source.querySelector('[value=current]')?'current':'original';exportChoice=undefined;}
  const current=source.value==='current';
  form.elements.result_id.value=current?preparedID:'';
  form.elements.query.value=current?tableQuery('result'):source.value==='original'?tableQuery('original'):'{}';
  form.elements.generation.value=sourceGeneration();
  const sheet=tableRoot(current?'result':'original')?.querySelector('.sheet-data'),count=form.querySelector('[data-download-count]');
  if(count)count.textContent=sheet?(sheet.dataset.matchRows+' matching rows · '+sheet.dataset.datasetRows+' total'):(current?'Waiting for complete cleaned data':'Original data');
  source.closest('.download-source-options').hidden=form.elements.format.value==='original';
  if(!form.dataset.workspaceSynced){form.dataset.workspaceSynced='true';form.elements.format.value=current?'json':workspace.querySelector('.format-tag')?.dataset.format||'json';}
  updateFormats();
 }
 const more=workspace.querySelector('.workspace-more'),moreSummary=more.querySelector('summary');
 workspace.classList.add('workspace-ready');
 const iconSource=workspace.querySelector('.workspace-toolbar use')?.getAttribute('href').split('#')[0]||'/static/icons/icons.svg';
 const theme=document.getElementById('theme-toggle');if(theme){theme.classList.add('workspace-theme');theme.title='Switch color theme';theme.innerHTML='<svg class="workspace-action-icon" aria-hidden="true"><use href="/static/icons/icons.svg#sun"/></svg><span class="workspace-tool-label">Switch theme</span>';more.querySelector('[data-workspace-theme]').append(theme);}
 const signout=document.querySelector('.topbar-actions form');if(signout)more.querySelector('[data-workspace-theme]').append(signout);
 theme?.querySelector('use').setAttribute('href',iconSource+'#sun');
 const mobileTools=matchMedia('(max-width:767px)'),extraTools=more.querySelector('.workspace-extra-tools');
 function arrangeTools(){more.open=false;const target=mobileTools.matches?more.querySelector('[data-workspace-theme]'):workspace.querySelector('[data-workspace-theme-desktop]');if(theme)target.append(theme);if(signout)target.append(signout);}
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
  workspace.querySelectorAll('.workspace-toolbar a,.workspace-subnav a,.workspace-header-actions a').forEach(link=>{const selected=key?link.dataset.workspaceTarget===key||(link.dataset.workspaceTarget==='insights'&&inspectViews.includes(key))||(link.dataset.workspaceTarget==='settings'&&fileViews.includes(key)):link.hasAttribute('data-workspace-data');link.classList.toggle('selected',selected);if(selected)link.setAttribute('aria-current','page');else link.removeAttribute('aria-current');});
  moreSummary.toggleAttribute('data-active',!!extraTools.querySelector('a[aria-current]'));
 }
 let dataRequest;
 async function loadData(url,remember=true,keepView=false){
  dataRequest?.abort();const request=new AbortController();dataRequest=request;
  const next=new URL(url,location.href),isResult=!!next.searchParams.get('result_id'),detail=isResult?result:document.getElementById('detail-content'),feedback=document.getElementById('feedback');detail.setAttribute('aria-busy','true');
  try{
   const response=await fetch(url,{credentials:'same-origin',signal:request.signal,headers:{'HX-Request':'true'}});
   const doc=new DOMParser().parseFromString(await response.text(),'text/html'),body=doc.querySelector('#detail-content')||doc.body;
   if(!response.ok||!body)throw Error(doc.querySelector('[role=alert]')?.textContent||'Unable to update the table. Try again.');
   const scroll=detail.querySelector('.record-scroll')?.scrollLeft||0;
   detail.replaceChildren(...body.childNodes);
   const types=detail.querySelector('[data-show-types]');if(types)types.checked=workspace.classList.contains('show-column-types');
   if(isResult){preparedID=next.searchParams.get('result_id');resultURL=base+next.search;syncCleanedColumns();workspace.querySelector('.workspace-data-heading').hidden=false;workspace.querySelector('[data-workspace-view=result]').hidden=false;}
   else{originalURL=base+next.search;workspace.dataset.sourceGeneration=detail.querySelector('.sheet-data')?.dataset.sourceGeneration||workspace.dataset.sourceGeneration;cache.delete('views');}
   if(active==='views'){const node=staticTool('views');if(node){cache.set('views',{node});mount({node});}}
   const metadata=doc.querySelector('#collection-metadata');if(metadata&&!isResult)document.getElementById('collection-metadata').replaceWith(metadata);
   window.htmx?.process(detail);if(!keepView)showData(isResult?'result':'original');else document.dispatchEvent(new CustomEvent('fluxport:data-mounted'));
   const table=detail.querySelector('.record-scroll');if(table)table.scrollLeft=scroll;
   if(remember)history.pushState(history.state,'',base+next.search+(active?'#tool='+active:''));
   rememberWorkspace();feedback.textContent='';
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
  if(active!==key){rememberWorkspace();saveCurrent();sourceDisclosure.open=!!viewState.expandedOriginal?.[key];}active=key;pane.hidden=false;workspace.classList.add('tool-open');heading.textContent=labels[key];
  markTool(key);
  history.replaceState(history.state,'',location.pathname+location.search+'#tool='+key);
  more.open=false;layout();rememberWorkspace(false);
 }
 function mount(entry){
  host.replaceChildren(entry.node);message('');
  if(active==='clean')syncClean(entry.node);
  if(active==='export')syncExport();
  if(active==='sql'){
   restoreSQL(entry.node);sqlResult.replaceChildren(...(entry.node.workspaceSQLResult?[entry.node.workspaceSQLResult]:[]));sqlResult.hidden=!sqlResult.firstElementChild;
   if(sqlResult.firstElementChild)caption.textContent='Original data';
  }else sqlResult.hidden=true;
  window.htmx?.process(entry.node);
  document.dispatchEvent(new CustomEvent('fluxport:tool-mounted',{detail:{tool:active}}));
  layout();updateFormats();deliverDownload();renderDownload();refreshDownload();
 }
 function close(){
  saveCurrent();active='';pane.hidden=true;workspace.classList.remove('tool-open');
  markTool('');layout();rememberWorkspace();
  history.replaceState(history.state,'',location.pathname+location.search);message('');if(lastTrigger?.isConnected)(mobileTools.matches&&extraTools.contains(lastTrigger)?moreSummary:lastTrigger).focus();
 }
 function extract(html,sourceURL=location.href){
  const direct=html instanceof Element,doc=direct?null:new DOMParser().parseFromString(html,'text/html');
  const fragment=direct?html:doc.querySelector('[data-workspace-fragment]');
  if(!fragment&&!doc.querySelector('[data-download-job],.format-review,.notice,[role=alert]'))throw Error('Unable to load the workspace view. Reload and try again.');
  const node=fragment||document.createElement('div');node.className='workspace-tool-body';if(new URL(sourceURL,location.href).searchParams.has('saved'))node.dataset.keepSQL='true';
  if(!fragment)node.append(...doc.body.childNodes);
  node.querySelectorAll('a[href],form').forEach(el=>{
   const attribute=el.tagName==='FORM'?'action':'href',value=el.getAttribute(attribute)||'';
   if(!value.startsWith('#'))el.setAttribute(attribute,new URL(value||sourceURL,sourceURL).href);
  });
  // Native tool forms are enhanced by this workspace, including error responses.
  node.querySelectorAll('[hx-get],[hx-post],[hx-push-url],[hx-trigger],[hx-target],[hx-swap-oob]').forEach(el=>{
   for(const attr of [...el.attributes])if(attr.name.startsWith('hx-'))el.removeAttribute(attr.name);
  });
  const detail=node.querySelector('#detail-content');if(detail)detail.id='tool-detail-content';
  const queryResult=node.querySelector('[data-sql-results]');if(queryResult){node.workspaceSQLResult=queryResult;queryResult.remove();}
  const cleanup=node.querySelector('[data-cleanup-form]');
  if(cleanup){
   const reuse=node.querySelector('.reuse-steps');reuse?.remove();
   node.querySelector('.workspace-shortcuts')?.remove();
   const preview=node.querySelector('[data-cleanup-preview]');if(preview){preview.querySelector('.cleanup-original-sample')?.remove();node.workspacePreview=preview;preview.remove();}
   const status=node.querySelector('[data-prepared-result]');if(status){node.workspaceStatus=status;status.remove();}
   const advanced=document.createElement('details'),summary=document.createElement('summary');summary.textContent='Advanced';advanced.className='workspace-clean-advanced';advanced.append(summary);cleanup.append(advanced);
   if(reuse)advanced.append(reuse);
   const save=cleanup.querySelector('.cleanup-save');if(save){advanced.append(save);const previewButton=save.querySelector('[value=preview]');if(previewButton)previewButton.hidden=true;}
   const reset=cleanup.querySelector('[value=reset]');if(reset)advanced.append(reset);
   for(const selector of ['.saved-changes','.join-builder','.dataset-advanced']){const item=node.querySelector(selector);if(item)advanced.append(item);}
   const steps=cleanup.querySelector('.cleanup-steps');if(!steps.querySelector('[name=steps]'))steps.hidden=true;
   cleanup.querySelector('.cleanup-download')?.remove();
   if(node.workspacePreview){const complete=node.workspacePreview.querySelector('.cleanup-complete-sample');if(complete)advanced.append(complete);const joinNotice=node.workspacePreview.querySelector('.notice'),join=node.querySelector('.join-builder');if(joinNotice&&join)join.append(joinNotice);}
   cleanup.querySelector('[data-cleanup-help]').hidden=true;
  }
  return node;
 }
 async function fetchView(url,options){
  try{return await fetch(url,options);}catch(error){
   if(error.name==='TypeError'&&!options.signal.aborted&&(!options.method||options.method==='GET'))return fetch(url,{...options,cache:'no-store'});
   throw error;
  }
 }
 async function load(key,url,options={}){
  const entry=cache.get(key)||{};entry.controller?.abort();entry.controller=new AbortController();entry.method=options.method||'GET';entry.request=(entry.request||0)+1;const request=entry.request;cache.set(key,entry);
  if(active===key){message(options.method==='POST'?'Working…':'Opening '+labels[key].toLowerCase()+'…');pane.setAttribute('aria-busy','true');}
  try{
   const response=await fetchView(url,{credentials:'same-origin',...options,signal:entry.controller.signal,headers:{...options.headers,'X-Fluxport-Workspace':'1','X-Fluxport-View':'workspace'}});
   if(response.headers.get('Content-Disposition')?.includes('attachment')){
    // SQL previews are bounded by the server. Original and job downloads bypass fetch.
    const blob=await response.blob(),link=document.createElement('a'),object=URL.createObjectURL(blob);
    link.href=object;link.download=response.headers.get('Content-Disposition').match(/filename="?([^";]+)/)?.[1]||'query-results';link.click();setTimeout(()=>URL.revokeObjectURL(object),1000);message('Download started.');return;
   }
   const html=await response.text();if(request!==entry.request)return;
   let node=extract(html,response.url);
   if(key==='clean'&&entry.node){for(const selector of ['.workspace-clean-advanced','.join-builder','.saved-changes','.reuse-steps']){const previous=entry.node.querySelector(selector),next=node.querySelector(selector);if(previous?.open&&next)next.open=true;}}
   if(key==='export'&&node.querySelector('[data-download-job],.notice.error')&&!node.querySelector('#collection-download-status')&&!node.querySelector('.format-review')){
    const previous=entry.node?.querySelector('#collection-download-status');
    if(previous){previous.replaceChildren(...node.childNodes);node=entry.node;}
    else{const status=document.createElement('div');status.id='collection-download-status';status.setAttribute('role','status');status.append(...node.childNodes);node.append(status);const again=document.createElement('a');again.className='button small ghost';again.href=routes.export;again.dataset.workspaceTarget='export';again.textContent='Another format';node.append(again);}
   }

   if(key==='api'&&!node.querySelector('form')&&entry.node?.querySelector('#config-result')){entry.node.querySelector('#config-result').replaceChildren(...node.childNodes);node=entry.node;}
   if(!node.textContent.trim())throw Error('The tool returned an empty response. Please try again.');
   if(new URL(response.url).pathname==='/login'){location.assign(response.url);return;}
   if(!response.ok&&!node.querySelector('form')&&entry.node){if(active===key)message(node.textContent.trim(),true);return;}
   const resultKey=node.dataset.workspaceFragment&&node.dataset.workspaceFragment!=='data'?node.dataset.workspaceFragment:toolFor(new URL(response.url))||key;
   const result={node,url:response.url,request:entry.request};cache.set(resultKey,result);
   if(resultKey==='export'&&active!==key){const status=node.querySelector('#collection-download-status');deliverDownload(status);renderDownload(status);refreshDownload();}
   if(active===key){select(resultKey);mount(result);if(!response.ok)message('Check the highlighted error and try again.',true);else if(options.method==='POST'&&((resultKey==='clean'&&result.node.workspacePreview)||(resultKey==='sql'&&result.node.workspaceSQLResult)))(resultKey==='sql'?sqlResult:document.getElementById('workspace-result')).scrollIntoView({block:'start',behavior:'smooth'});}
  }catch(error){if(error.name==='AbortError')return;if(active===key){message(error.message||'Unable to load this tool. Try again.',true);const retry=document.createElement('button');retry.type='button';retry.className='button small';retry.textContent='Try again';retry.addEventListener('click',()=>load(key,url,options));notice.append(' ',retry);}}
  finally{if(active===key&&request===entry.request)pane.removeAttribute('aria-busy');}
 }
 let restoringNavigation=false;
 function open(key,url,trigger,force=false){
  const savedScroll=viewState.scroll?.[key],targetScroll=Number.isFinite(savedScroll)&&savedScroll>=0?savedScroll:0;
  if(active!==key){const previous=cache.get(active);if(previous?.method==='GET')previous.controller?.abort();if(!restoringNavigation)history.pushState(history.state,'',location.pathname+location.search+'#tool='+key);}
  if(key==='export')exportChoice=workspace.dataset.dataView==='result'?'current':'original';
  if(key==='sql'||key==='quality'||key==='insights'||key==='views'){delete result.dataset.restoreView;showData('original');}
  if(url&&key==='sql'){const scoped=new URL(url,location.href);scoped.pathname=base+'/sql';url=scoped.href;}
  lastTrigger=trigger||lastTrigger;select(key);
  if(['settings','views','history'].includes(key)){const node=staticTool(key);if(node)cache.set(key,{node});}
  if(key==='raw'){
   const sample=cache.get('clean')?.node?.querySelector('.cleanup-complete-sample pre');
   if(workspace.dataset.dataView==='result'&&sample){const node=document.createElement('div'),label=document.createElement('p');label.className='muted small-text';label.textContent='Cleaned preview · '+(result.querySelector('[data-preview-count]')?.dataset.previewCount||0)+' rows';node.className='workspace-tool-body';node.append(label,sample.cloneNode(true));cache.set('raw',{node});force=false;}
   else {force=true;if(workspace.dataset.dataView==='result'&&preparedID){const target=new URL(base,location.href);target.searchParams.set('tab','raw');target.searchParams.set('result_id',preparedID);target.searchParams.set('query',tableQuery('result'));url=target.href;}}
  }
  const entry=cache.get(key);
  if(entry?.node&&!force){mount(entry);}else{host.replaceChildren();const target=new URL(url||routes[key],location.href);if(key==='clean'&&!target.searchParams.has('saved')&&!target.searchParams.has('reuse')){if(preparedID)target.searchParams.set('result_id',preparedID);else target.searchParams.set('input_query',tableQuery('original'));}if(key==='export'){target.searchParams.set('query',tableQuery(workspace.dataset.dataView));if(workspace.dataset.dataView==='result'&&preparedID)target.searchParams.set('result_id',preparedID);}load(key,target.href);}
  heading.focus({preventScroll:true});
  window.scrollTo({top:targetScroll});viewState.scroll ||= {};viewState.scroll[key]=targetScroll;rememberWorkspace(false);
 }
 document.addEventListener('click',event=>{
  if(more.open&&!more.contains(event.target))more.open=false;
  workspace.querySelectorAll('.toolbar-disclosure[open]').forEach(disclosure=>{if(!disclosure.contains(event.target))disclosure.open=false;});
  if(event.button!==0||event.ctrlKey||event.metaKey||event.shiftKey||event.altKey)return;
  const link=event.target.closest('a');
  if(event.target.closest('[data-workspace-edit-clean]')){open('clean',routes.clean,event.target);return;}
  const view=event.target.closest('[data-workspace-view]');if(view){event.preventDefault();showData(view.dataset.workspaceView);(view.dataset.workspaceView==='result'?result:document.getElementById('detail-content')).scrollIntoView({block:'start',behavior:'smooth'});return;}
  if(event.target.closest('[data-workspace-download-clean]')){showData('result');open('export',routes.export,event.target);return;}
  if(event.target.closest('[data-close-workspace-tool]')){close();return;}
  if(event.target.closest('[data-refresh-prepared],[data-browse-prepared]')){event.preventDefault();refreshPrepared();return;}
  if(!link)return;
  if(link.hasAttribute('data-workspace-data')){event.preventDefault();if(active)history.pushState(history.state,'',location.pathname+location.search);close();window.scrollTo({top:viewState.scroll?.data||0});return;}
  const url=new URL(link.href,location.href);let key=link.dataset.workspaceTarget||toolFor(url);
   if(key==='export'&&active==='sql'){key='sql';const disclosure=host.querySelector('details');if(disclosure){disclosure.open=true;disclosure.scrollIntoView({block:'center'});}event.preventDefault();return;}
  if(key&&(link.closest('[data-file-workspace]')||pane.contains(link))){event.preventDefault();event.stopImmediatePropagation();open(key,url.href,link,pane.contains(link)||(!link.dataset.workspaceTarget));}
  else if(pane.contains(link)&&url.origin===location.origin&&url.pathname===base&&!url.search){event.preventDefault();close();}
  else if((link.closest('#detail-content,#workspace-result')||(active==='views'&&pane.contains(link)))&&url.origin===location.origin&&[base,base+'/workbench'].includes(url.pathname)){event.preventDefault();event.stopImmediatePropagation();loadData(url.href);}
 },true);
 workspace.addEventListener('toggle',event=>{
  const disclosure=event.target;if(!disclosure.matches('.workspace-more,.toolbar-disclosure')||!disclosure.open)return;
  [more,...workspace.querySelectorAll('.toolbar-disclosure')].forEach(other=>{if(other!==disclosure)other.open=false;});
 },true);
 pane.addEventListener('submit',async event=>{
  const form=event.target,button=event.submitter;
  if(event.defaultPrevented||!(form instanceof HTMLFormElement))return;
  if(active==='settings')return;
  event.preventDefault();
  if(form.matches('[data-cleanup-form]')&&button?.value==='recapture'){form.elements.input_query.value=tableQuery('original');try{const latest=await fetch(base+'/workbench/status').then(r=>r.json());if(latest.status!=='ready'){message('The source is processing. Try updating the input when it is ready.',true);return;}workspace.dataset.sourceGeneration=latest.generation;form.elements.source_generation.value=latest.generation;}catch{message('Unable to refresh the input. Try again.',true);return;}}
  const data=new FormData(form,button),url=new URL(button?.getAttribute('formaction')||form.getAttribute('action')||location.href,location.href);
  const method=(button?.getAttribute('formmethod')||form.getAttribute('method')||'GET').toUpperCase();
  if(data.get('format')==='original'&&data.get('action')!=='review'&&url.pathname===base+'/export'){
   downloadIntent=null;location.assign(base+'/export?original=1');return;
  }
  if(url.pathname.endsWith('/cancel'))downloadIntent=null;
  else if(data.get('action')!=='review'&&(url.pathname===base+'/export'||url.pathname===base+'/workbench/export'||url.pathname.endsWith('/retry'))){downloadIntent={job:null};host.querySelector('#collection-download-status')?.replaceChildren();host.querySelector('.download-chooser')?.removeAttribute('data-editing');}
  const params=new URLSearchParams();for(const [key,value] of data){if(typeof value==='string')params.append(key,value);}
  if(method==='GET'){url.search=params.toString();load(active,url.href);}
  else{
   const buttons=[...form.querySelectorAll('button[type=submit],button:not([type])')];buttons.forEach(b=>b.disabled=true);
   load(active,url.href,{method,body:params,headers:{'HX-Request':'true'}}).finally(()=>buttons.forEach(b=>b.disabled=false));
  }
 });
 // Exporting a filtered table also belongs to this file's workspace.
 workspace.addEventListener('submit',event=>{
  const form=event.target;
  if(event.defaultPrevented)return;
  if(form.closest('[data-prepared-result]')){event.preventDefault();const body=new URLSearchParams(new FormData(form,event.submitter));fetch(form.getAttribute('action'),{method:'POST',body,headers:{'HX-Request':'true'}}).then(r=>r.text()).then(html=>{const doc=new DOMParser().parseFromString(html,'text/html'),status=doc.querySelector('[data-prepared-result]');if(status){preparedID=status.dataset.preparedResult;resultURL=base+'?result_id='+preparedID;result.querySelector('[data-prepared-result]')?.replaceWith(status);rememberWorkspace();refreshPrepared();}});return;}
  if(form.matches('[data-sheet-query]')){event.preventDefault();const url=new URL(base,location.href),params=new URLSearchParams();for(const [key,value] of new FormData(form,event.submitter))if(typeof value==='string')params.append(key,value);url.search=params.toString();loadData(url.href);return;}
  if(!(form instanceof HTMLFormElement)||pane.contains(form)||form.getAttribute('action')!==base+'/workbench/export')return;
  event.preventDefault();const body=new URLSearchParams();for(const [key,value] of new FormData(form,event.submitter))if(typeof value==='string')body.append(key,value);
  downloadIntent={job:null};select('export');host.replaceChildren();load('export',base+'/workbench/export',{method:'POST',body});
 });
 function updateFormats(){host.querySelectorAll('[data-export-format],#collection-download-format').forEach(select=>{
  const form=select.closest('form'),source=form?.querySelector('[data-download-source]');
  if(source){
   const original=select.value==='original',button=form.querySelector('[data-download-submit]');
   if(button){button.textContent=original?'Download original file':'Convert & download';button.disabled=!select.selectedOptions[0]||select.selectedOptions[0].disabled;}
   const rowScope=form.querySelector('.download-row-scope');if(rowScope)rowScope.hidden=original;
   const sourceOptions=form.querySelector('.download-source-options');if(sourceOptions)sourceOptions.hidden=original;
   const scope=form.querySelector('[data-download-scope]');if(scope)scope.textContent=original?'Exact source bytes.':form.elements.scope.value==='entire'?'All rows in this dataset.':'Matching rows, in table order.';
   const context=form.querySelector('[data-download-context]');if(context)context.textContent=(source.value==='original'?'Original data':source.value==='current'?'Cleaned data':source.selectedOptions[0].textContent)+' · Change';
  }
  const target=form?.querySelector('[data-download-note]');if(target){target.textContent=select.value==='original'?'Exact source file · all rows':select.selectedOptions[0]?.dataset.formatNote||'';target.hidden=!target.textContent;}
 });}
 pane.addEventListener('change',event=>{
  if(event.target.matches('[data-download-source]'))syncExport();
  if(event.target.matches('[data-cleanup-scope]')){const form=event.target.form;form.elements.field.required=false;form.requestSubmit(form.querySelector('[data-recapture-input]'));}
  rememberWorkspace();updateFormats();
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
 window.addEventListener('fluxport:progress',event=>{const prep=result.querySelector('[data-prepared-result]');if(prep&&(event.detail.connected||event.detail.id===preparedID||event.detail.resource==='*'))refreshPrepared();const job=host.querySelector('[data-download-job]')||cache.get('export')?.node?.querySelector('[data-download-job]');if(job&&(event.detail.connected||event.detail.id===job.dataset.downloadJob))refreshDownload();});
 document.addEventListener('keydown',event=>{
  if(event.key!=='Escape'||event.target.closest('dialog[open]'))return;
  const disclosure=[more,...workspace.querySelectorAll('.toolbar-disclosure')].find(el=>el.open);
  if(disclosure){event.preventDefault();disclosure.open=false;disclosure.querySelector('summary').focus();return;}
  if(active){event.preventDefault();close();}
 });
 window.addEventListener('popstate',()=>{restoringNavigation=true;const key=location.hash.match(/^#tool=(\w+)$/)?.[1];if(key&&routes[key])open(key,routes[key]);else{close();loadData(base+location.search,false);}restoringNavigation=false;});
 const requestedResult=new URL(location.href).searchParams.get('result_id')||viewState.resultID;
 if(requestedResult){result.dataset.restoreView=new URL(location.href).searchParams.get('result_id')?'result':viewState.dataView||'original';preparedID=requestedResult;resultURL=viewState.resultURL||base+'?result_id='+requestedResult;const existing=document.getElementById('detail-content').querySelector('[data-cleaned-dataset]');if(existing)result.replaceChildren(existing);if(!document.getElementById('detail-content').querySelector('.sheet-data')||existing)loadData(originalURL,false,true);workspace.querySelector('.workspace-data-heading').hidden=false;workspace.querySelector('[data-workspace-view=result]').hidden=false;showData('result');refreshPrepared();}
 else if(viewState.originalURL&&!new URL(location.href).search){loadData(originalURL,false);}
 const seeded=host.querySelector('[data-workspace-fragment]'),serverView=workspace.dataset.initialView;
 if(seeded){const node=extract(seeded);if(serverView==='clean'){node.workspacePreview=result.querySelector('[data-cleanup-preview]');node.workspaceStatus=result.querySelector('[data-prepared-result]');}if(serverView==='sql')node.workspaceSQLResult=sqlResult.querySelector('[data-sql-results]');cache.set(serverView,{node});}
 const initial=location.hash.match(/^#tool=(\w+)$/)?.[1]||(serverView!=='data'?serverView:viewState.active);restoringNavigation=true;if(initial&&routes[initial])open(initial,routes[initial]);else{layout();markTool('');}restoringNavigation=false;
})();
