/* Bounded grid interactions; server validates and commits complete commands. */
(() => {
 'use strict';
 const shell=document.querySelector('[data-file-workspace]');if(!shell)return;
 const owner=shell.dataset.fileWorkspace,base='/collections/'+owner;
 let state=null,columnContext=null,copyJob='',copyChecking=false,copyAgain=false,previewEpoch=0;
 const root=()=>shell.querySelector('#detail-content');
 const table=()=>root()?.querySelector('.sheet-table');
 const cells=()=>Array.from(table()?.querySelectorAll('tbody tr')||[]).map(row=>Array.from(row.querySelectorAll('td[data-sheet-cell]')).filter(c=>!c.hidden)).filter(row=>row.length);
 const columns=()=>Array.from(root()?.querySelectorAll('[data-sheet-column]')||[]).filter(c=>c.checked);
 const rowID=row=>row?.closest('tr').querySelector('[data-record-json]')?.dataset.recordJson;
 function range(){const grid=cells();if(!grid.length||!state)return null;const a=grid.findIndex(row=>rowID(row[0])===state.anchor.id),b=grid.findIndex(row=>rowID(row[0])===state.active.id),cols=columns(),x=cols.findIndex(c=>c.value===state.anchor.field),y=cols.findIndex(c=>c.value===state.active.field);if([a,b,x,y].some(n=>n<0))return null;return {grid,r0:Math.min(a,b),r1:Math.max(a,b),c0:Math.min(x,y),c1:Math.max(x,y)};}
 function point(cell){return {id:rowID(cell),field:columns().find(c=>c.dataset.sheetColumn===cell.dataset.sheetCell)?.value};}
 function paint(){table()?.querySelectorAll('[data-grid-selected],[data-grid-active]').forEach(c=>{delete c.dataset.gridSelected;delete c.dataset.gridActive;c.removeAttribute('aria-selected');});const selection=range();if(!selection)return;
  for(let r=selection.r0;r<=selection.r1;r++)for(let c=selection.c0;c<=selection.c1;c++){const cell=selection.grid[r][c];cell.dataset.gridSelected='true';cell.setAttribute('aria-selected','true');if(rowID(cell)===state.active.id&&point(cell).field===state.active.field)cell.dataset.gridActive='true';}
 }
 function activate(cell,extend){const p=point(cell);if(!p.id||p.field===undefined)return;state=extend&&state?{...state,active:p}:{anchor:p,active:p};paint();}
 function selectedIDs(){const checked=Array.from(root()?.querySelectorAll('[name=record_id]:checked')||[]).map(b=>b.value);if(checked.length)return checked;const r=range();return r?r.grid.slice(r.r0,r.r1+1).map(row=>rowID(row[0])):[];}
 function selectedFields(){if(columnContext!==null)return [columnContext];const r=range();return r?columns().slice(r.c0,r.c1+1).map(c=>c.value):[];}
 function error(message){const panel=root()?.querySelector('[data-editor-panel]'),el=panel?.querySelector('.editor-command-error');if(el){el.hidden=!message;el.textContent=message;}else{const feedback=document.getElementById('feedback');if(feedback)feedback.textContent=message;}}
 function groups(form){const action=form.elements.command_action.value;form.querySelectorAll('[data-editor-for]').forEach(group=>{const visible=group.dataset.editorFor.split(' ').includes(action);group.hidden=!visible;group.querySelectorAll('input,textarea,select').forEach(c=>c.disabled=!visible);});form.querySelector('[data-command-result]')?.replaceChildren();}
 function open(action,text){
  window.FluxPortWorkspace?.showFile?.();
  const panel=root()?.querySelector('[data-editor-panel]');if(!panel)return;panel.hidden=false;panel.open=true;const form=panel.querySelector('[data-command-preview]');previewEpoch++;form.hidden=false;panel.querySelector('[data-command-result]').replaceChildren();form.elements.command_action.value=action;groups(form);error('');
  const ids=selectedIDs(),fields=selectedFields();Array.from(form.elements.record_id?.options||[]).forEach(o=>o.selected=ids.includes(o.value));Array.from(form.elements.field?.options||[]).forEach(o=>o.selected=fields.includes(o.value));
  if(action==='rename_column')form.elements.column_name.value=fields[0]||'';
  if(text!==undefined){form.elements.paste_text.value=text;targetPaste(form);}
  shell.querySelectorAll('[data-editor-menu]').forEach(menu=>menu.open=false);panel.scrollIntoView({block:'start',behavior:'smooth'});
 }
 // Parse TSV dimensions without coercing values. Go's CSV reader is authoritative.
 function parseTSV(text){
  const rows=[];let row=[],value='',quoted=false,after=false;
  for(let i=0;i<text.length;i++){const c=text[i];if(quoted){if(c==='"'){if(text[i+1]==='"'){value+='"';i++;}else{quoted=false;after=true;}}else value+=c;continue;}
   if(c==='"'){if(value||after)throw Error('Unexpected quote in clipboard field');quoted=true;continue;}
   if(c==='\t'){row.push(value);value='';after=false;continue;}
   if(c==='\n'||c==='\r'&&text[i+1]==='\n'){if(c==='\r')i++;if(row.length||value||after){row.push(value);rows.push(row);}row=[];value='';after=false;continue;}
   if(after)throw Error('Unexpected text after a quoted cell');value+=c;
  }
  if(quoted)throw Error('Unclosed quoted cell');if(row.length||value||after){row.push(value);rows.push(row);}
  if(!rows.length||rows.some(r=>r.length!==rows[0].length))throw Error('Paste must contain a rectangular set of cells');
  if(rows.length>1000||rows.length*rows[0].length>5000)throw Error('Paste supports at most 1000 rows and 5000 cells');return rows;
 }
 function targetPaste(form){
  if(form.elements.append.checked)return;
  try{const data=parseTSV(form.elements.paste_text.value),r=range();if(!r)return;
   const grid=r.grid,cols=columns();if(r.r0+data.length>grid.length||r.c0+data[0].length>cols.length)throw Error('Paste exceeds this page. Choose a smaller block or explicitly append rows.');
   const ids=grid.slice(r.r0,r.r0+data.length).map(row=>rowID(row[0])),names=cols.slice(r.c0,r.c0+data[0].length).map(c=>c.value);
   Array.from(form.elements.record_id.options).forEach(o=>o.selected=ids.includes(o.value));Array.from(form.elements.field.options).forEach(o=>o.selected=names.includes(o.value));error('');
  }catch(e){error(e.message);}
 }
 function csrf(){return document.querySelector('meta[name=csrf-token]')?.content||'';}
 function preview(panel,data){
  panel.querySelector('[data-command-preview]').hidden=true;
  const box=panel.querySelector('[data-command-result]');box.replaceChildren();const section=document.createElement('section');section.className='editor-preview';
  const title=document.createElement('h3');title.textContent='Preview: '+data.preview.label;const count=document.createElement('p');count.textContent=data.preview.rows+' affected rows'+(data.preview.cells?' · '+data.preview.cells+' affected cells':'');
  const note=document.createElement('p');note.className='muted small-text';note.textContent='Sample only · first 20 affected rows';section.append(title,count,note);
  const scroll=document.createElement('div');scroll.className='table-scroll';const t=document.createElement('table');t.className='data-table';const head=t.createTHead().insertRow();data.preview.columns.forEach(name=>{const th=document.createElement('th');th.textContent=name;head.append(th);});const body=t.createTBody();data.preview.sample?.forEach(row=>{const tr=body.insertRow();row.forEach(value=>{tr.insertCell().textContent=value;});});scroll.append(t);section.append(scroll);
  const form=document.createElement('form');form.method='post';form.action=base+'/workbench/edit';form.dataset.fileCommand='';
  for(const [name,value]of Object.entries({csrf:csrf(),command:data.command,confirm:'delete'})){const input=document.createElement('input');input.type='hidden';input.name=name;input.value=value;form.append(input);}
  const command=JSON.parse(data.command);const button=document.createElement('button');button.className='button primary';button.textContent=command.action.startsWith('delete_')?'Delete and save':'Apply change';form.append(button);
  // Confirmation runs before the global save handler.
  button.addEventListener('click',e=>{if(command.action.startsWith('delete_')&&!confirm('Delete the previewed rows or columns? Undo is available.')){e.preventDefault();}},{once:false});
  const back=document.createElement('a');back.className='button';back.href=base+'?edit='+encodeURIComponent(command.action);back.dataset.commandBack='';back.textContent='Edit change';form.append(back);
  section.append(form);box.append(section);panel.scrollIntoView({block:'nearest'});button.focus({preventScroll:true});
 }
 async function copy(kind){
  const r=range();if(!r){error('Select cells in the table first.');return;}
  let text;if(kind==='json'){const ids=new Set(r.grid.slice(r.r0,r.r1+1).map(row=>rowID(row[0])));text='['+Array.from(table().querySelectorAll('[data-record-json]')).filter(n=>ids.has(n.dataset.recordJson)).map(n=>n.value).join(',')+']';}
  else{const quote=v=>/[\t\r\n"]/.test(v)?'"'+v.replaceAll('"','""')+'"':v;text=r.grid.slice(r.r0,r.r1+1).map(row=>row.slice(r.c0,r.c1+1).map(cell=>quote(cell.querySelector('[data-cell-raw]')?.value||'')).join('\t')).join('\n');}
  try{await navigator.clipboard.writeText(text);shell.querySelectorAll('[data-editor-menu]').forEach(menu=>menu.open=false);const feedback=document.getElementById('feedback');if(feedback)feedback.textContent='Selection copied.';}
  catch{const dialog=root().querySelector('[data-record-dialog]');dialog.querySelector('[data-record-values]').value=text;dialog.querySelector('[data-record-values]').readOnly=true;dialog.querySelector('[data-use-record]').hidden=true;dialog.showModal();dialog.querySelector('textarea').select();}
 }
 document.addEventListener('pointerdown',e=>{const cell=e.target.closest('td[data-sheet-cell]');if(!cell||!root()?.contains(cell)||e.target.closest('input,textarea,button,a'))return;columnContext=null;activate(cell,e.shiftKey);if(e.shiftKey)e.preventDefault();cell.focus({preventScroll:true});});
 document.addEventListener('pointerover',e=>{if(e.buttons!==1)return;const cell=e.target.closest('td[data-sheet-cell]');if(cell&&root()?.contains(cell)&&state&&!e.target.closest('input,textarea,button'))activate(cell,true);});
 document.addEventListener('keydown',e=>{
  const cell=e.target.closest('td[data-sheet-cell]');if(!cell||!root()?.contains(cell)||e.target.closest('input,textarea,button,a'))return;
  if((e.ctrlKey||e.metaKey)&&e.key.toLowerCase()==='c'){e.preventDefault();copy('tsv');return;}
  if(e.altKey||e.ctrlKey||e.metaKey)return;
  if(e.key==='Escape'){state=null;paint();return;}
  if(!['ArrowLeft','ArrowRight','ArrowUp','ArrowDown','Tab'].includes(e.key))return;
  const grid=cells(),row=grid.findIndex(r=>r.includes(cell));if(row<0)return;let x=grid[row].indexOf(cell),y=row;
  if(e.key==='ArrowLeft')x--;if(e.key==='ArrowRight')x++;if(e.key==='ArrowUp')y--;if(e.key==='ArrowDown')y++;
  if(e.key==='Tab'){x+=e.shiftKey?-1:1;if(x>=grid[row].length){x=0;y++;}if(x<0){y--;x=grid[row].length-1;}}
  if(!grid[y]?.[x])return;e.preventDefault();activate(grid[y][x],e.shiftKey&&e.key!=='Tab');grid[y][x].focus({preventScroll:true});grid[y][x].scrollIntoView({block:'nearest',inline:'nearest'});
 });
 document.addEventListener('paste',e=>{if(!e.target.matches('td[data-sheet-cell]')||!root()?.querySelector('[data-editor-panel]'))return;e.preventDefault();open('paste',e.clipboardData.getData('text/plain'));});
 document.addEventListener('click',e=>{
  const back=e.target.closest('[data-command-back]');if(back&&root()?.contains(back)){e.preventDefault();e.stopImmediatePropagation();const panel=back.closest('[data-editor-panel]');panel.querySelector('[data-command-preview]').hidden=false;panel.querySelector('[data-command-result]').replaceChildren();return;}
  const opener=e.target.closest('[data-editor-open]');if(opener&&shell.contains(opener)){e.preventDefault();e.stopImmediatePropagation();open(opener.dataset.editorOpen);return;}
  const column=e.target.closest('[data-column-edit]');if(column&&root()?.contains(column)){columnContext=columns().find(c=>c.dataset.sheetColumn===column.dataset.columnEdit)?.value;open('rename_column');return;}
  const copier=e.target.closest('[data-grid-copy]');if(copier&&root()?.contains(copier)){e.preventDefault();copy(copier.dataset.gridCopy);}
 },true);
 document.addEventListener('change',e=>{if(e.target.matches('[data-command-action]')){previewEpoch++;groups(e.target.form);root()?.querySelector('[data-command-result]')?.replaceChildren();}if(e.target.matches('[data-sheet-column]')){state=null;paint();}if(e.target.matches('[name=append]')&&e.target.form?.matches('[data-command-preview]')&&e.target.checked)Array.from(e.target.form.elements.record_id.options).forEach(o=>o.selected=false);});
 function buildAddRow(form){
 if(form.elements.command_action.value!=='add_row')return true;
 const fields=Array.from(form.querySelectorAll('[data-add-fields] [data-field-name]')).filter(c=>!c.disabled&&!c.hidden);
 const filled=fields.filter(c=>c.type==='checkbox'?c.checked:c.value.trim()!=='');
 if(!filled.length)return true;
 const row=Object.create(null);
 for(const control of fields){
  const name=control.dataset.fieldName;
  if(control.type==='checkbox'){if(control.checked)row[name]=true;continue;}
  const text=control.value.trim();if(text==='')continue;
  if(control.tagName==='TEXTAREA'){try{row[name]=JSON.parse(text);}catch(e){error('"'+name+'" needs valid JSON for nested values.');control.focus();return false;}continue;}
  if(control.type==='number'){const n=Number(text);if(!Number.isFinite(n)){error('"'+name+'" needs a number, or leave it blank.');control.focus();return false;}if(/^\d+$/.test(text)&&!Number.isSafeInteger(n)){error('"'+name+'" is too large to store exactly. Use a quoted value in the Advanced raw JSON field.');control.focus();return false;}row[name]=n;continue;}
  row[name]=text;
 }
 const raw=form.elements.command_value;raw.value=JSON.stringify(row);
 return true;
}
document.addEventListener('submit',async e=>{
  const form=e.target;if(!form.matches('[data-command-preview]'))return;e.preventDefault();e.stopImmediatePropagation();if(window.FluxPortEditor?.saving){error('Wait for the current save to finish.');return;}
  if(!buildAddRow(form))return;if(form.elements.command_action.value==='paste')targetPaste(form);
  const values=new URLSearchParams(new FormData(form));values.set('preview','1');
  if(new TextEncoder().encode(values.toString()).length>1<<20){error('Edit exceeds the 1 MiB request limit. Use a smaller block.');return;}
  const requestEpoch=++previewEpoch;form.setAttribute('aria-busy','true');error('');
  try{const response=await fetch(form.action,{method:'POST',headers:{Accept:'application/json'},body:values});const data=await response.json();if(!response.ok)throw Error(data.message||data.error||'Preview failed');if(requestEpoch!==previewEpoch)return;preview(form.closest('[data-editor-panel]'),data);}
  catch(err){if(requestEpoch===previewEpoch)error(err.message);}finally{form.removeAttribute('aria-busy');}
 },true);
 function initialize(){
  const panel=root()?.querySelector('[data-editor-panel]');if(panel){panel.hidden=!panel.open;panel.addEventListener('toggle',()=>{if(!panel.open)panel.hidden=true;},{once:true});const form=panel.querySelector('[data-command-preview]');groups(form);}
  const grid=cells();const signature=JSON.stringify([root()?.querySelector('[data-sheet-query] [name=query]')?.value,grid.map(row=>rowID(row[0]))]);
  if(shell.dataset.gridSignature!==signature){state=null;shell.dataset.gridSignature=signature;}paint();
  const marker=root()?.querySelector('[data-editable-copy-job]');if(marker){copyJob=marker.dataset.editableCopyJob;if(marker.dataset.state==='ready')finishCopy(marker.querySelector('[data-open-editable-copy]')?.getAttribute('href'));}
 }
 function finishCopy(href){if(!href)return;try{if(sessionStorage.getItem('fluxport-copy-'+owner)===copyJob){sessionStorage.removeItem('fluxport-copy-'+owner);location.assign(href);}}catch{}}
 async function refreshCopy(){
  if(!copyJob)return;if(copyChecking){copyAgain=true;return;}copyChecking=true;
  try{const response=await fetch(base+'/editable-copy/'+encodeURIComponent(copyJob),{headers:{Accept:'application/json'}});if(!response.ok)return;const j=await response.json(),box=root()?.querySelector('[data-copy-status]');if(!box)return;
   root().querySelectorAll('[data-editable-copy] button').forEach(b=>b.disabled=!['ready','failed','cancelled','interrupted'].includes(j.state));
   box.replaceChildren();const status=document.createElement('p');status.setAttribute('role','status');status.textContent=j.state==='ready'?'Editable copy ready.':j.error||('Copy '+j.state+' · '+j.records+' records');box.append(status);
   if(j.state==='ready'){const link=document.createElement('a');link.href='/collections/'+j.result;link.className='button primary';link.textContent='Open editable copy';box.append(link);finishCopy(link.getAttribute('href'));}
   else if(['failed','cancelled','interrupted'].includes(j.state)){const note=document.createElement('p');note.textContent='Use narrower filters, or use cleanup and download.';box.append(note);const link=document.createElement('a');link.href=base+'?copy_job='+copyJob;link.textContent='Recovery options';box.append(link);}
   else{const cancel=document.createElement('form');cancel.action='/jobs/'+copyJob+'/cancel';cancel.method='post';const token=document.createElement('input');token.type='hidden';token.name='csrf';token.value=csrf();const button=document.createElement('button');button.className='button';button.textContent='Cancel';button.name='redirect';button.value=base+'?copy_job='+copyJob;cancel.append(token,button);box.append(cancel);}
  }catch{}finally{copyChecking=false;if(copyAgain){copyAgain=false;queueMicrotask(refreshCopy);}}
 }
 document.addEventListener('submit',async e=>{
  const form=e.target;if(!form.matches('[data-editable-copy]'))return;e.preventDefault();e.stopImmediatePropagation();
  form.setAttribute('aria-busy','true');form.querySelector('button').disabled=true;
  try{const response=await fetch(form.action,{method:'POST',headers:{Accept:'application/json'},body:new URLSearchParams(new FormData(form))});const job=await response.json();if(!response.ok)throw Error(job.message||job.error||'Copy could not start');copyJob=job.id;try{sessionStorage.setItem('fluxport-copy-'+owner,copyJob);}catch{}await refreshCopy();}
  catch(err){const box=root().querySelector('[data-copy-status]');box.textContent=err.message;box.setAttribute('role','alert');form.querySelector('button').disabled=false;}finally{form.removeAttribute('aria-busy');}
 },true);
 window.addEventListener('fluxport:progress',e=>{if(e.detail?.connected||e.detail?.id===copyJob||e.detail?.resource==='*')refreshCopy();});
 for(const name of ['fluxport:file-reloaded','fluxport:tool-mounted','htmx:afterSwap'])document.addEventListener(name,initialize);
 initialize();
})();
