/* Small enhancements. Go owns data and navigation; HTMX owns fragment updates. */
(() => {
  'use strict';
  if (window.htmx) Object.assign(htmx.config, {allowEval:false, allowScriptTags:false, includeIndicatorStyles:false});
  const $ = id => document.getElementById(id);
  try { const theme=localStorage.getItem('fluxport-theme'); if(theme==='light'||theme==='dark') document.documentElement.dataset.theme=theme; } catch (_) {}
  let toastTimer;
  function closeMenu() { $('sidebar').classList.remove('is-open'); $('menu-toggle').setAttribute('aria-expanded','false'); }
  document.addEventListener('keydown',event=>{if(event.key==='Escape'&&$('sidebar').classList.contains('is-open')){closeMenu();$('menu-toggle').focus();}});
  document.addEventListener('click',event=>{if($('sidebar').classList.contains('is-open')&&!$('sidebar').contains(event.target)&&!event.target.closest('#menu-toggle'))closeMenu();});
  function toast(text) { const el=$('toast'); el.textContent=text; el.classList.add('visible');clearTimeout(toastTimer);toastTimer=setTimeout(()=>el.classList.remove('visible'),2400); }
  async function copy(text) { try { await navigator.clipboard.writeText(text); toast('Copied to clipboard'); } catch (_) { toast('Clipboard unavailable. Select and copy the text.'); } }
  document.addEventListener('click', event => {
    const button=event.target.closest('button'); if(!button) return;
    if(button.id==='theme-toggle') {const theme=document.documentElement.dataset.theme==='dark'?'light':'dark';document.documentElement.dataset.theme=theme;try{localStorage.setItem('fluxport-theme',theme)}catch(_){} }
    if(button.id==='menu-toggle') {const open=$('sidebar').classList.toggle('is-open');button.setAttribute('aria-expanded',String(open));}
    if(button.id==='menu-close') {closeMenu();$('menu-toggle').focus();}
    if(button.dataset.copy) { const el=$(button.dataset.copy); if(el) copy('value' in el?el.value:el.textContent); }
    if(button.id==='download-output') {const el=$('convert-output');if(el?.dataset.ready!=='true')return;const types={json:'application/json',csv:'text/csv',yaml:'application/yaml',xml:'application/xml',ndjson:'application/x-ndjson',toml:'application/toml'};const url=URL.createObjectURL(new Blob([el.value],{type:(types[el.dataset.format]||'text/plain')+';charset=utf-8'}));const link=document.createElement('a');link.href=url;link.download='converted.'+(el.dataset.format||'txt');link.click();setTimeout(()=>URL.revokeObjectURL(url),1000);}
  });
  document.body.addEventListener('htmx:configRequest', event => {event.detail.headers['X-CSRF-Token']=document.querySelector('meta[name="csrf-token"]').content;});
  document.body.addEventListener('htmx:beforeSwap', event => {
    if(event.detail.xhr.status>=400 && event.detail.xhr.status<600) {event.detail.shouldSwap=true;event.detail.isError=false;event.detail.target=$('feedback');if(event.detail.elt?.closest('#converter-form')){const status=document.querySelector('[data-output-status]');if(status)status.textContent='Previous result · conversion failed';}}
  });
  document.body.addEventListener('htmx:sendError', () => toast('Connection failed. Check that FluxPort is running.'));
  document.body.addEventListener('htmx:afterSwap', event => {
 if(event.detail.target?.id==='conversion-result'&&event.detail.xhr?.status<400)$('feedback').replaceChildren();
    const body=document.querySelector('[data-active-tab]');if(body)document.querySelectorAll('.tabs a').forEach(a=>a.classList.toggle('selected',new URL(a.href).searchParams.get('tab')===body.dataset.activeTab));
  });
  document.body.addEventListener('htmx:xhr:progress', event => {const el=$('upload-progress');if(el&&event.detail.total){el.hidden=false;el.value=event.detail.loaded/event.detail.total*100;}});
  document.body.addEventListener('htmx:afterRequest', () => {const el=$('upload-progress');if(el)el.hidden=true;});
  $('converter-file')?.addEventListener('change', async event => {const file=event.target.files[0];if(!file)return;const ext=file.name.split('.').pop().toLowerCase();const format={yml:'yaml',jsonl:'ndjson'}[ext]||ext; if([...$('source-format').options].some(o=>o.value===format))$('source-format').value=format;$('input-size').textContent=file.name;$('convert-input').value='';$('input-tree')?.replaceChildren();try {const text=await file.slice(0,256*1024).text();if(event.target.files[0]!==file)return;$('convert-input').value=text;if(file.size>256*1024)$('input-size').textContent=file.name+' · preview first 256 KiB; full file will be converted';}catch{$('input-size').textContent=file.name+' · preview unavailable; full file will be converted';}});
  $('convert-input')?.addEventListener('input',()=>{const el=$('converter-file');if(el)el.value='';$('input-tree')?.replaceChildren(); const status=document.querySelector('[data-output-status]');if(status)status.textContent='Previous result · input changed';});
  $('converter-form')?.addEventListener('change',()=>{const status=document.querySelector('[data-output-status]');if(status)status.textContent='Previous result · options changed';});
  $('endpoint-selector')?.addEventListener('change',event=>{$('request-path').value=event.target.value;});
  if($('endpoint-selector')) {const id=new URLSearchParams(location.search).get('endpoint');if(id&&/^[a-f0-9]{24}$/.test(id)){$('endpoint-selector').value='/api/collections/'+id;$('request-path').value='/api/collections/'+id;}}
  function request() {
    const url=new URL($('request-path').value,location.origin);if(url.origin!==location.origin||!url.pathname.startsWith('/api/')||url.username||url.password)throw Error('Choose a same-origin /api/ endpoint.');
    for(const line of $('request-query').value.split('\n')) {if(!line.trim())continue;const i=line.indexOf('=');if(i<1)throw Error('Query parameters must use key=value.');url.searchParams.append(line.slice(0,i).trim(),line.slice(i+1).trim());}
    const headers=new Headers();for(const line of $('request-headers').value.split('\n')) {if(!line.trim())continue;const i=line.indexOf(':');if(i<1)throw Error('Headers must use Name: value.');headers.append(line.slice(0,i).trim(),line.slice(i+1).trim());}
    if($('api-key').value)headers.set('Authorization','Bearer '+$('api-key').value);
    const method=$('request-method').value;const body=method==='GET'?undefined:$('request-body').value||undefined;if(body&&!headers.has('Content-Type'))headers.set('Content-Type','application/json');return {url,method,headers,body};
  }
  const shellQuote=s=>"'"+s.replaceAll("'","'\\''")+"'";
  $('copy-curl')?.addEventListener('click',()=>{try{const r=request();let cmd='curl -X '+r.method+' '+shellQuote(r.url.href);r.headers.forEach((v,k)=>{cmd+=' -H '+shellQuote(k+': '+v)});if(r.body)cmd+=' --data-raw '+shellQuote(r.body);copy(cmd)}catch(error){toast(error.message)}});
  $('explorer-form')?.addEventListener('submit',async event=>{
    event.preventDefault();const button=$('request-submit');button.disabled=true;const start=performance.now();
    try {const r=request();if(r.method!=='GET'&&!confirm('Send '+r.method+'? This may change source data.'))return;const response=await fetch(r.url,{method:r.method,headers:r.headers,body:r.body,credentials:'omit',redirect:'error',signal:AbortSignal.timeout(30000)});$('response-status').textContent=response.status+' '+response.statusText;
      const reader=response.body?.getReader();const chunks=[];let total=0;if(reader){for(;;){const {done,value}=await reader.read();if(done)break;total+=value.byteLength;if(total>1024*1024){await reader.cancel();throw Error('Response preview exceeds 1 MB. Download it with your HTTP client.');}chunks.push(value);}}
      const bytes=new Uint8Array(total);let offset=0;for(const chunk of chunks){bytes.set(chunk,offset);offset+=chunk.length;}let text=new TextDecoder().decode(bytes);if(response.headers.get('Content-Type')?.includes('application/json')){try{text=JSON.stringify(JSON.parse(text),null,2)}catch(_){}}$('response-body').textContent=text||'(empty response)';$('response-time').textContent=Math.round(performance.now()-start)+' ms · '+total+' bytes';
    }catch(error){$('response-status').textContent='ERROR';$('response-body').textContent=error.message;}finally{button.disabled=false;}
  });
})();

/* Push updates: no periodic polling. */
(() => {
 const activity=document.getElementById('job-activity');
 if(activity && window.EventSource) {
  const source=new EventSource('/events'); let pending;
  let refreshDetail=false;source.addEventListener('progress',event=>{try{const message=JSON.parse(event.data);if(message.resource==='*'||(message.resource && message.resource===location.pathname.split('/')[2]))refreshDetail=true;}catch(_){if(document.querySelector('#detail-content .notice'))refreshDetail=true;}if(pending)return;pending=setTimeout(()=>{pending=null;
   if(window.htmx){htmx.ajax('GET','/jobs',{target:'#job-activity',swap:'innerHTML'});htmx.trigger(document.body,'collectionsChanged');
    const detail=document.getElementById('detail-content');if(detail && refreshDetail && !detail.contains(document.activeElement)){const tab=detail.querySelector('[data-active-tab]')?.dataset.activeTab;if(tab!=='configuration'){const url=new URL(location.href);url.searchParams.set('tab',tab||'preview');url.searchParams.set('page',detail.querySelector('[data-page]')?.dataset.page||'1');htmx.ajax('GET',url.pathname+url.search,{target:detail,swap:'innerHTML'});}}refreshDetail=false;
   }
  },250)});
  window.addEventListener('pagehide',()=>source.close());
 }
 const input=document.getElementById('import-file'), zone=input?.closest('.drop-zone');
 const names=()=>{const list=document.getElementById('upload-files');if(list){list.replaceChildren(...[...input.files].map(f=>{const e=document.createElement('p');e.textContent=f.name+' · selected';return e}));}};
 input?.addEventListener('change',names);
 zone?.addEventListener('dragover',e=>{e.preventDefault();zone.classList.add('drag-active')});
 zone?.addEventListener('dragleave',()=>zone.classList.remove('drag-active'));
 zone?.addEventListener('drop',e=>{e.preventDefault();zone.classList.remove('drag-active');if(e.dataTransfer?.files.length){input.files=e.dataTransfer.files;names()}});
 zone?.addEventListener('submit',()=>{if(activity?.closest('details'))activity.closest('details').open=true;document.querySelectorAll('#upload-files p').forEach(e=>e.textContent=e.textContent.replace('selected','uploading'))});

 if(location.hash==='#import'){const el=document.getElementById('import');if(el)el.open=true;}
})();

document.body.addEventListener('htmx:afterRequest',event=>{if(event.detail.elt?.closest('.drop-zone'))document.querySelectorAll('#upload-files p').forEach(e=>e.textContent=e.textContent.replace('uploading','uploaded — see file activity'));});

document.getElementById('cancel-upload')?.addEventListener('click',()=>{const form=document.getElementById('import-file')?.form;if(form&&window.htmx)htmx.trigger(form,'htmx:abort');document.querySelectorAll('#upload-files p').forEach(e=>e.textContent=e.textContent.replace(/uploading|selected/,'cancelled'));});

for(const name of ['jobQueued','htmx:beforeRequest'])document.body.addEventListener(name,e=>{const form=e.detail?.elt;if(name==='jobQueued'||form?.getAttribute('hx-target')==='#job-activity'){const panel=document.getElementById('job-activity')?.closest('details');if(panel)panel.open=true;}});
if(location.hash==='#activity'){const panel=document.getElementById('activity');if(panel)panel.open=true;}
