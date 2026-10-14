(()=>{
 function enhance(root=document){
  root.querySelectorAll('a[data-column-details]').forEach(link=>{
   const button=document.createElement('button');
   for(const attribute of link.attributes)if(attribute.name!=='href')button.setAttribute(attribute.name,attribute.value);
   button.type='button';button.append(...link.childNodes);link.replaceWith(button);
   button.closest('tr').classList.toggle('is-selected',button.getAttribute('aria-expanded')==='true');
  });
 }
 document.addEventListener('click',event=>{
  const button=event.target.closest('button[data-column-details]');if(!button)return;
  const table=button.closest('.insights-table'),row=table?.querySelector('#'+CSS.escape(button.dataset.columnDetails));if(!row)return;
  const open=row.hidden;
  table.querySelectorAll('.column-detail-row').forEach(item=>item.hidden=true);
  table.querySelectorAll('button[data-column-details]').forEach(item=>{item.setAttribute('aria-expanded','false');item.closest('tr').classList.remove('is-selected');});
  row.hidden=!open;button.setAttribute('aria-expanded',String(open));button.closest('tr').classList.toggle('is-selected',open);
 });
 document.addEventListener('fluxport:tool-mounted',()=>enhance());
 document.addEventListener('htmx:afterSwap',()=>enhance());
 enhance();
})();
