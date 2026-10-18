(() => {
 document.addEventListener('change',event=>{if(event.target.matches('[data-show-types]'))document.querySelector('[data-file-workspace]')?.classList.toggle('show-column-types',event.target.checked);});
  'use strict';
	const dataListeners=new WeakMap();
	function initialize(){
	const sqlEditor=document.getElementById('sql-editor');
	if(sqlEditor&&!sqlEditor.dataset.initialized){
		sqlEditor.dataset.initialized='true';
		const preview=document.getElementById('sql-highlight');
		function highlight(){preview.replaceChildren();const tokens=sqlEditor.value.split(/(\b(?:SELECT|FROM|WHERE|WITH|JOIN|LEFT|RIGHT|INNER|OUTER|ON|GROUP|BY|ORDER|ASC|DESC|LIMIT|OFFSET|AS|AND|OR|NOT|NULL|SUM|COUNT|AVG|MIN|MAX|DISTINCT|HAVING|UNION|ALL)\b)/gi);tokens.forEach((text,i)=>{if(i%2){const span=document.createElement('span');span.className='sql-keyword';span.textContent=text;preview.append(span);}else preview.append(document.createTextNode(text));});}
		sqlEditor.addEventListener('input',highlight);highlight();
		sqlEditor.addEventListener('keydown',event=>{if((event.ctrlKey||event.metaKey)&&event.key==='Enter'){event.preventDefault();sqlEditor.form.requestSubmit(sqlEditor.form.querySelector('[value=run]'));}});
		sqlEditor.form.addEventListener('submit',event=>{if(event.submitter?.value==='save'&&!sqlEditor.form.elements.name.value.trim()){event.preventDefault();sqlEditor.form.elements.name.focus();sqlEditor.form.elements.name.setCustomValidity('Enter a name for this query.');sqlEditor.form.elements.name.reportValidity();}else sqlEditor.form.setAttribute('aria-busy','true');});
		sqlEditor.form.elements.name.addEventListener('input',()=>sqlEditor.form.elements.name.setCustomValidity(''));
	}
  const workspace=document.querySelector('[data-file-workspace]');
  const roots=workspace?[document.getElementById('detail-content'),document.getElementById('workspace-result')]:[document];
  roots.forEach(initializeTable);
 }
 function initializeTable(root){
  if(!root)return;
  const queryForm = root.querySelector('[data-sheet-query]');
  if (!queryForm||queryForm.dataset.initialized) return;
  queryForm.dataset.initialized='true';
  dataListeners.get(root)?.abort();const listeners=new AbortController();dataListeners.set(root,listeners);
  const filters = queryForm.querySelector('[data-filter-list]');
  const sorts = queryForm.querySelector('[data-sort-list]');
  const operators = [['eq','equals'],['neq','does not equal'],['contains','contains'],['starts','starts with'],['gt','greater than'],['gte','at least'],['lt','less than'],['lte','at most'],['null','missing / null'],['notnull','has a value']];
  function control(label, marker, options) {
    const wrapper = document.createElement('label'); wrapper.className='field-label'; wrapper.append(document.createTextNode(label));
    const element = document.createElement(options ? 'select' : 'input'); element.className='field-control'; element.setAttribute(marker, '');
    if (options) options.forEach(([value,text]) => { const o=document.createElement('option');o.value=value;o.textContent=text;element.append(o); });
    else if (marker.endsWith('field')) {element.setAttribute('list',root.querySelector('datalist').id);element.required=true;}
    wrapper.append(element);return wrapper;
  }
  function addRule(kind) {
    const list=kind==='filter'?filters:sorts;
    if(list.children.length >= (kind==='filter'?20:8)) return;
    const row=document.createElement('div');row.className=kind+'-row';
    row.append(control('Field','data-'+kind+'-field'));
    if(kind==='filter') {row.append(control('Match','data-filter-op',operators),control('Value','data-filter-value'));}
    else row.append(control('Direction','data-sort-direction',[['asc','Ascending'],['desc','Descending']]));
    const remove=document.createElement('button');remove.type='button';remove.className='button small';remove.dataset.removeRule='';remove.textContent='Remove';row.append(remove);list.append(row);row.querySelector('input').focus();
  }
  queryForm.addEventListener('click',event=>{if(event.target.closest('[data-add-filter]'))addRule('filter');if(event.target.closest('[data-add-sort]'))addRule('sort');const remove=event.target.closest('[data-remove-rule]');if(remove)remove.parentElement.remove();});
  // Keep explicit quoted strings and integers outside JS's exact range intact.
  function valueJSON(text) { const trimmed=text.trim(); if (/^-?\d+(\.\d+)?([eE][+-]?\d+)?$/.test(trimmed)||['true','false','null'].includes(trimmed)) return trimmed; if(trimmed.startsWith('"')){try{if(typeof JSON.parse(trimmed)==='string')return trimmed;}catch{}}return JSON.stringify(text); }
  queryForm.addEventListener('submit',event=>{
    if(event.submitter?.hasAttribute('data-raw-query')){queryForm.querySelector('[data-query-search]').removeAttribute('name');queryForm.querySelector('[data-query-limit]').removeAttribute('name');return;}

    const filterJSON=Array.from(filters.children).map(row=>'{"field":'+JSON.stringify(row.querySelector('[data-filter-field]').value)+',"op":'+JSON.stringify(row.querySelector('[data-filter-op]').value)+',"value":'+valueJSON(row.querySelector('[data-filter-value]').value)+'}');
    const sort=Array.from(sorts.children).map(row=>({field:row.querySelector('[data-sort-field]').value,desc:row.querySelector('[data-sort-direction]').value==='desc'}));
    queryForm.querySelector('[name=query]').value='{"search":'+JSON.stringify(queryForm.querySelector('[data-query-search]').value)+',"limit":'+Number(queryForm.querySelector('[data-query-limit]').value)+',"filters":['+filterJSON.join(',')+'],"sort":'+JSON.stringify(sort)+'}';
  });
  const sheet=root.querySelector('.sheet-data'),edit=sheet.querySelector('[data-sheet-edit]');
  const selection=sheet.querySelector('thead .sheet-select');
  if(selection){
   // Tables with few columns distribute spare width; pin after the actual column.
   const align=()=>{if(selection.getClientRects().length)sheet.style.setProperty('--sheet-select-width',getComputedStyle(selection).width);};
   align();const observer=new ResizeObserver(align);observer.observe(selection);
   listeners.signal.addEventListener('abort',()=>observer.disconnect(),{once:true});
  }
	if(window.EventSource){
		const initial=sheet.dataset.sourceGeneration||edit.elements.generation.value,initialState=sheet.dataset.sourceStatus;let checking=false;
		async function check(){if(checking||window.FluxPortEditor?.saving)return;checking=true;try{const response=await fetch('/collections/'+sheet.dataset.collection+'/workbench/status');if(!response.ok)return;const state=await response.json();if(state.status!==initialState&&initialState!=='ready'){location.reload();return;}if(state.status==='ready'&&state.generation!==initial){if(!initial){location.reload();return;}if(!document.querySelector('[data-source-changed]')){const notice=document.createElement('div');notice.className='notice';notice.dataset.sourceChanged='';notice.append(document.createTextNode('The file changed outside FluxPort. Reload to continue editing. '));const link=document.createElement('a');link.href=location.href;link.textContent='Reload records';notice.append(link);document.querySelector('main').prepend(notice);}}}catch{}finally{checking=false;}}
		window.addEventListener('fluxport:progress',event=>{const message=event.detail;if(message.connected){check();return;}if((message.resource===sheet.dataset.collection||message.resource==='*')&&['ready','failed','cancelled','changed'].includes(message.state))check();},{signal:listeners.signal});
	}
  const boxes=()=>Array.from(edit.querySelectorAll('[name=record_id]:not(:disabled)'));
  function count(){const n=boxes().filter(b=>b.checked).length;const text=edit.querySelector('[data-selection-count]');if(text)text.textContent=n+' records selected';}
  edit.addEventListener('change',event=>{if(event.target.matches('[data-select-page]'))boxes().forEach(b=>b.checked=event.target.checked);count();});
  edit.addEventListener('submit',event=>{const n=boxes().filter(b=>b.checked).length;if(!n){event.preventDefault();return;}if(edit.elements.method.value==='DELETE'&&!window.confirm('Permanently delete '+n+' selected records from the source?'))event.preventDefault();});
  const dialog=root.querySelector('[data-record-dialog]');let activeID='',activeField=null;
  const inspector=root.querySelector('[data-record-values]'),useRecord=dialog.querySelector('[data-use-record]');
  function inspect(value,title,record){inspector.value=value;inspector.readOnly=!record||sheet.dataset.editable!=='true';useRecord.hidden=!record||sheet.dataset.editable!=='true';dialog.querySelector('[data-inspector-title]').textContent=title;dialog.querySelector('[data-copy-feedback]').textContent='';dialog.showModal();}
  async function copy(value){try{await navigator.clipboard.writeText(value);const notice=document.getElementById('feedback');if(notice)notice.textContent='Value copied.';dialog.querySelector('[data-copy-feedback]').textContent='Copied';}catch{inspect(value,'Copy complete value',false);inspector.select();dialog.querySelector('[data-copy-feedback]').textContent='Press Ctrl+C or Command+C to copy.';}}
  edit.addEventListener('click',event=>{
   const cell=event.target.closest('td[data-sheet-cell]');
   if(event.target.closest('[data-copy-cell]')){copy(cell.querySelector('[data-cell-raw]').value);return;}
   if(event.target.closest('[data-inspect-cell]')){activeID=cell.closest('tr').querySelector('[name=record_id]').value;activeField=columns.find(c=>c.dataset.sheetColumn===cell.dataset.sheetCell)?.value;inspect(cell.querySelector('[data-cell-raw]').value,'JSON value',sheet.dataset.editable==='true');return;}
   const button=event.target.closest('[data-edit-record]');if(!button)return;activeField=null;activeID=button.dataset.editRecord;const source=Array.from(edit.querySelectorAll('[data-record-json]')).find(e=>e.dataset.recordJson===activeID);inspect(source.value,'Record values',true);
  });
  dialog.querySelector('[data-copy-inspector]').addEventListener('click',()=>copy(inspector.value));
  function submitValue(id,field,raw){
   try{JSON.parse(raw);}catch{inspector.setCustomValidity('Enter valid JSON.');inspector.reportValidity();return false;}
   boxes().forEach(b=>b.checked=b.value===id);
   if(field!==null){if(!edit.elements.method.querySelector('[value=FIELD]'))edit.elements.method.add(new Option('Edit one field','FIELD'));edit.elements.method.value='FIELD';edit.elements.value.value='{"field":'+JSON.stringify('/'+field.replaceAll('~','~0').replaceAll('/','~1'))+',"value":'+raw+'}';}
   else{edit.elements.method.value='PUT';edit.elements.value.value=raw;}
   count();edit.requestSubmit();return true;
  }
  inspector.addEventListener('input',()=>inspector.setCustomValidity(''));
  dialog.querySelector('[data-use-record]').addEventListener('click',()=>{if(submitValue(activeID,activeField,inspector.value))dialog.close();});
  const density=queryForm.querySelector('[data-sheet-density]');try{density.value=localStorage.getItem('fluxport-table-density')==='compact'?'compact':'comfortable';}catch{}sheet.dataset.density=density.value;density.addEventListener('change',()=>{sheet.dataset.density=density.value;try{localStorage.setItem('fluxport-table-density',density.value);}catch{}});
  const key='fluxport-sheet-'+sheet.dataset.collection;let prefs=Object.create(null);try{Object.assign(prefs,JSON.parse(localStorage.getItem(key)||'{}')||{});}catch{}
  const columns=Array.from(queryForm.querySelectorAll('[data-sheet-column]'));
if(sheet.dataset.editable==='true'){
  const mark=()=>{for(const cell of sheet.querySelectorAll('td[data-sheet-cell]:not([data-cell-editable])')){cell.dataset.cellEditable='';cell.title='Double-click or press Enter to edit';}};
  mark();
  const hintKey='fluxport-hint-cell-edit';let shown=false;try{shown=!!sessionStorage.getItem(hintKey);}catch{}
  if(!shown&&!sheet.parentElement.querySelector('.edit-hint')){
   const hint=document.createElement('div');hint.className='edit-hint';hint.setAttribute('role','status');
   hint.append(document.createTextNode('Tip: double-click a cell to edit. Enter confirms, Escape cancels.'));
   const close=document.createElement('button');close.type='button';close.className='edit-hint-close';close.textContent='\u00d7';close.setAttribute('aria-label','Dismiss editing hint');
   close.addEventListener('click',()=>{hint.remove();try{sessionStorage.setItem(hintKey,'1');}catch{}});
   hint.append(close);sheet.before(hint);
  }
 }
	let currentEdit=null;
	function closeEdit(save){
		const open=currentEdit;if(!open)return;currentEdit=null;
		if(!open.cell.isConnected)return;
		if(!save||open.input.value===open.original){open.cell.innerHTML=open.originalHTML;return;}
		let valid=true;try{JSON.parse(open.cell.dataset.valueKind==='string'?JSON.stringify(open.input.value):open.input.value);}catch{valid=false;}
		if(valid)submitCellEdit(open.cell,open.input,open.id);else open.cell.innerHTML=open.originalHTML;
	}
	function submitCellEdit(cell,input,id){
		const field=columns.find(c=>c.dataset.sheetColumn===cell.dataset.sheetCell)?.value;
		const raw=cell.dataset.valueKind==='string'?JSON.stringify(input.value):input.value;
		boxes().forEach(b=>b.checked=b.value===id);let option=edit.elements.method.querySelector('[value=FIELD]');if(!option){option=new Option('Edit one field','FIELD');edit.elements.method.add(option);}edit.elements.method.value='FIELD';edit.elements.value.value='{"field":'+JSON.stringify("/"+field.replaceAll("~","~0").replaceAll("/","~1"))+',"value":'+raw+'}';edit.requestSubmit();
	}
	function inlineEdit(cell){
		if(sheet.dataset.editable!=='true'||window.FluxPortEditor?.saving||cell.querySelector('input'))return;
		if(currentEdit)closeEdit(true);
		const row=cell.closest('tr'),id=row.querySelector('[name=record_id]').value;
		const original=cell.querySelector('[data-cell-raw]')?.value||'',originalHTML=cell.innerHTML;
		if(original.length>=512||cell.dataset.displayKind==='json'||sheet.dataset.scalar==='true'){activeID=id;activeField=sheet.dataset.scalar==='true'?null:columns.find(c=>c.dataset.sheetColumn===cell.dataset.sheetCell)?.value;const raw=cell.dataset.valueKind==='string'?JSON.stringify(original):original;inspect(raw||'null','Edit value',true);return;}
		const input=document.createElement('input');input.className='field-control';input.value=original;input.setAttribute('aria-label','New field value');cell.replaceChildren(input);input.focus();input.select();
		currentEdit={cell,input,id,original,originalHTML};
		input.addEventListener('blur',()=>{if(currentEdit?.input===input)closeEdit(true);});
		input.addEventListener('keydown',event=>{if(event.key==='Escape'){if(currentEdit?.input===input)currentEdit=null;cell.innerHTML=originalHTML;cell.focus();}if(event.key==='Enter'){event.preventDefault();try{JSON.parse(cell.dataset.valueKind==='string'?JSON.stringify(input.value):input.value);}catch{input.setCustomValidity('Enter a valid JSON value.');input.reportValidity();return;}if(currentEdit?.input===input)currentEdit=null;submitCellEdit(cell,input,id);}});
	}
	sheet.addEventListener('dblclick',event=>{const cell=event.target.closest('td[data-sheet-cell]');if(cell)inlineEdit(cell);});
	sheet.addEventListener('keydown',event=>{if(event.key==='Enter'&&event.target.matches('td[data-sheet-cell]')){event.preventDefault();inlineEdit(event.target);}});
  function store(){try{localStorage.setItem(key,JSON.stringify(prefs));}catch{}}
  function setColumn(box){const index=box.dataset.sheetColumn;sheet.querySelectorAll('[data-sheet-cell="'+index+'"]').forEach(e=>e.hidden=!box.checked);}
  let savedColumns=null;try{savedColumns=JSON.parse(sheet.dataset.savedColumns);}catch{}
  columns.forEach(box=>{if(Array.isArray(savedColumns)&&savedColumns.length){box.checked=savedColumns.includes(box.value);prefs[box.value]={...prefs[box.value],visible:box.checked};store();}else if(prefs[box.value]?.visible===false)box.checked=false;setColumn(box);box.addEventListener('change',()=>{prefs[box.value]={...prefs[box.value],visible:box.checked};setColumn(box);store();});});
  sheet.querySelectorAll('.column-resizer').forEach(handle=>{const heading=handle.parentElement,index=heading.dataset.sheetCell,name=columns.find(b=>b.dataset.sheetColumn===index)?.value;
    function resize(width){width=Math.max(70,Math.min(600,width));sheet.querySelectorAll('[data-sheet-cell="'+index+'"]').forEach(e=>e.style.width=width+'px');prefs[name]={...prefs[name],width};store();}
    if(prefs[name]?.width)resize(prefs[name].width);
    handle.addEventListener('pointerdown',event=>{event.preventDefault();const start=event.clientX,width=heading.getBoundingClientRect().width;handle.setPointerCapture(event.pointerId);const move=e=>resize(width+e.clientX-start);handle.addEventListener('pointermove',move);handle.addEventListener('pointerup',()=>handle.removeEventListener('pointermove',move),{once:true});});
    handle.addEventListener('keydown',event=>{if(event.key==='ArrowLeft'||event.key==='ArrowRight'){event.preventDefault();resize(heading.getBoundingClientRect().width+(event.key==='ArrowRight'?20:-20));}});
  });
  root.querySelector('[data-save-sheet-view]')?.addEventListener('submit',event=>{columns.filter(b=>b.checked).forEach(box=>{const input=document.createElement('input');input.type='hidden';input.name='columns';input.value=box.value;event.target.append(input);});});
	}
	initialize();document.addEventListener('fluxport:tool-mounted',initialize);document.addEventListener('fluxport:data-mounted',initialize);
})();
