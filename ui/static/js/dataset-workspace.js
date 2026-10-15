/* Friendly controls submit ordinary forms; cleanup itself uses the pipeline service. */
(() => {
 function initialize(){
 const form=document.querySelector('[data-cleanup-form]');if(!form||form.dataset.initialized)return;
 form.dataset.initialized='true';
 const action=form.elements.cleanup,field=form.elements.field;
 function controls(){
  form.querySelectorAll('[data-cleanup-for]').forEach(el=>el.hidden=el.dataset.cleanupFor!==action.value);
  if(field.tagName==='SELECT')field.options[0].textContent=action.value==='deduplicate'?'All columns':'Choose a column';
  field.required=action.value!=='deduplicate';
  const editable=document.querySelector('.sheet-data')?.dataset.editable==='true';form.querySelector('[data-cleanup-help]').textContent=(action.value==='deduplicate'?'All columns compares complete rows. ':'')+(editable?'Saved to this file. Use Undo above to restore.':'This file is read-only. Download changes to save them.');
 }
 action.addEventListener('change',controls);controls();
 const key='fluxport-cleanup-'+form.dataset.collection;
 const values=()=>Array.from(form.querySelectorAll('[name=steps]')).map(el=>el.value);
 function remember(){try{const steps=values();if(steps.length)localStorage.setItem(key,JSON.stringify({steps,name:form.elements.name.value,revision:form.elements.revision.value,saved:form.dataset.saved,inputQuery:form.elements.input_query?.value,scope:form.elements.scope?.value,generation:form.elements.source_generation?.value,validation:form.elements.validation.value}));else localStorage.removeItem(key);}catch{}}
 let draft;try{draft=JSON.parse(localStorage.getItem(key)||'null');}catch{}
 const fresh=new URL(location.href).searchParams.has('new');
 if(fresh){try{localStorage.removeItem(key);}catch{}draft=null;}
 if(draft&&Array.isArray(draft.steps)&&draft.steps.length<=20&&draft.steps.every(s=>typeof s==='string'&&s.length<=8192)){
  const stale=!!draft.generation&&draft.generation!==form.elements.source_generation?.value;
  const same=JSON.stringify(draft.steps)===JSON.stringify(values())&&draft.name===form.elements.name.value&&(!draft.inputQuery||draft.inputQuery===form.elements.input_query?.value);
  if(!same&&!form.dataset.posted){
   const notice=document.querySelector('[data-draft-notice]');notice.className='notice';notice.append(document.createTextNode(stale?'The source changed. Restore your steps, then update the input. ':'You have unsaved changes. '));
   const restore=document.createElement('button');restore.type='button';restore.className='button small';restore.textContent='Restore changes';
   restore.addEventListener('click',()=>{
    form.querySelectorAll('[name=steps]').forEach(el=>el.remove());
    draft.steps.forEach(value=>{const input=document.createElement('input');input.type='hidden';input.name='steps';input.value=value;form.append(input);});
    form.elements.name.value=String(draft.name||'').slice(0,100);form.elements.revision.value=String(draft.revision||'0');
    if(/^[a-f0-9]{24}$/.test(draft.saved||''))form.setAttribute('action','/collections/'+form.dataset.collection+'/changes?saved='+draft.saved);
    if(form.elements.input_query&&typeof draft.inputQuery==='string')form.elements.input_query.value=draft.inputQuery;
    if(form.elements.scope&&['matching','entire'].includes(draft.scope))form.elements.scope.value=draft.scope;
    if(form.elements.source_generation&&draft.generation)form.elements.source_generation.value=draft.generation;
    if(typeof draft.validation==='string')form.elements.validation.value=draft.validation;
    field.required=false;form.requestSubmit(form.querySelector('[value=preview]'));
   });notice.append(restore);
   try{const session=JSON.parse(sessionStorage.getItem('fluxport-workspace-'+form.dataset.collection)||'{}');if(session.active==='clean'&&!stale)queueMicrotask(()=>restore.click());}catch{}
  }
 }
 if(form.dataset.posted)remember();
 form.elements.name.addEventListener('input',remember);
 form.elements.name.addEventListener('keydown',event=>{if(event.key==='Enter'){event.preventDefault();field.required=false;form.requestSubmit(form.querySelector('[value=save]'));}});
 form.addEventListener('submit',e=>{
  if(e.submitter?.value!=='add')field.required=false;
  remember();
 });
 // Validation must be updated before the browser validates a non-add submit.
 form.querySelectorAll('button[name=action]').forEach(button=>button.addEventListener('click',()=>{field.required=['add','save_change'].includes(button.value)&&action.value!=='deduplicate';}));
 const joinDataset=form.querySelector('[data-join-dataset]'),joinColumn=form.querySelector('[data-join-column]');
 if(joinDataset&&joinColumn){
  const choices=[...joinColumn.options].map(o=>({dataset:o.dataset.dataset,value:o.value,label:o.textContent}));
  const update=()=>{const previous=joinColumn.value;joinColumn.replaceChildren();for(const choice of choices.filter(c=>c.dataset===joinDataset.value)){const option=new Option(choice.value,choice.value);joinColumn.add(option)}if([...joinColumn.options].some(o=>o.value===previous))joinColumn.value=previous;};
  joinDataset.addEventListener('change',update);update();
 }
 }
 initialize();document.addEventListener('fluxport:tool-mounted',initialize);
})();
