async (page, artifactDir) => {
 const base=new URL(page.url()).origin,name='row-controls-'+Date.now()+'.json';let file;
 try{
  await page.goto(base+'/collections');await page.locator('#import>summary').click();
  await page.locator('#import-file').setInputFiles({name,mimeType:'application/json',buffer:Buffer.from(JSON.stringify(Array.from({length:30},(_,i)=>({id:i+1,name:'Row '+i}))))});
  await page.getByRole('button',{name:'Open selected files',exact:true}).click();const row=page.locator('#collection-table tr').filter({hasText:name});await row.filter({hasText:'Done'}).waitFor({timeout:120000});file=base+await row.getByRole('link',{name,exact:true}).getAttribute('href');await page.goto(file);await page.locator('[data-workspace-data]').click();
  const assert=async()=>{
   await page.evaluate(()=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))));
   const issues=await page.locator('.sheet-table').evaluate(table=>{
    const errors=[];
    for(const row of table.rows){const select=row.querySelector('.sheet-select'),index=row.querySelector('.sheet-index');if(!select||!index)continue;const s=select.getBoundingClientRect(),n=index.getBoundingClientRect(),c=select.querySelector('input').getBoundingClientRect();if(parseFloat(getComputedStyle(select.querySelector('input')).width)>20||c.left<s.left||c.right>s.right||c.top<s.top||c.bottom>s.bottom)errors.push('checkbox clipped');if(n.left<s.right-1)errors.push('sticky columns overlap');}
    const header=table.querySelector('thead .sheet-index').getBoundingClientRect(),body=table.querySelector('tbody .sheet-index').getBoundingClientRect();if(Math.abs(header.left-body.left)>1||Math.abs(header.width-body.width)>1)errors.push('header/body misaligned');
    return [...new Set(errors)];
   });if(issues.length)throw Error(issues.join('; '));
  };
  for(const width of [320,390,768,1024,1440,2560])for(const theme of ['light','dark'])for(const density of ['compact','comfortable']){
   await page.setViewportSize({width,height:900});await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);await page.locator('[data-sheet-density]').selectOption(density);
   await page.locator('.record-scroll').evaluate(el=>{el.scrollLeft=0;el.scrollTop=0;});await assert();await page.locator('.record-scroll').evaluate(el=>{el.scrollLeft=250;el.scrollTop=250;});await assert();
  }
  await page.evaluate(()=>document.documentElement.style.zoom='2');await assert();await page.evaluate(()=>document.documentElement.style.zoom='');
  await page.setViewportSize({width:390,height:844});await page.screenshot({path:artifactDir+'/row-controls-mobile.png'});
  const checkbox=page.locator('tbody [name=record_id]').first();await page.locator('.record-scroll').evaluate(el=>el.scrollTop=0);await checkbox.check();if(!await checkbox.isChecked())throw Error('Row selection stopped working');await checkbox.uncheck();
  await page.locator('[data-query-search]').fill('Row 2');await page.locator('[data-query-search]').press('Enter');await page.locator('.sheet-query-footer').getByText('11 matching records',{exact:true}).waitFor();await assert();
  let injected=false;
  await page.evaluate(()=>{
   window.__savedPendingTable=false;
   window.__rowObserver=new MutationObserver(()=>{if(document.querySelector('[data-file-edit-status]')?.textContent==='Saved'&&document.querySelector('#detail-content .sheet-data')?.dataset.sourceStatus!=='ready')window.__savedPendingTable=true;});
   window.__rowObserver.observe(document.querySelector('[data-file-workspace]'),{subtree:true,childList:true,characterData:true});
  });
  const refreshRoute=async route=>{
   const request=route.request();
   if(!injected&&request.method()==='GET'&&new URL(request.url()).pathname===new URL(file).pathname){
    injected=true;const response=await route.fetch(),html=await response.text();
    await route.fulfill({response,body:html.replaceAll('data-source-status="ready"','data-source-status="indexing"')});
    await page.evaluate(resource=>setTimeout(()=>window.dispatchEvent(new CustomEvent('fluxport:progress',{detail:{resource,state:'ready'}})),200),new URL(file).pathname.split('/')[2]);
   }else await route.continue();
  };
  await page.route(file+'*',refreshRoute);
  const column=await page.locator('[data-sheet-column]').evaluateAll(es=>es.find(e=>e.value==='name').dataset.sheetColumn),cell=page.locator('tbody td[data-sheet-cell="'+column+'"]').first();
  await cell.dblclick();await cell.locator('input').fill('Row 2 edited');await cell.locator('input').press('Enter');
  await page.waitForFunction(()=>document.querySelector('[data-file-edit-status]')?.textContent==='Saved'&&document.querySelector('#detail-content .sheet-data')?.dataset.sourceStatus==='ready'&&document.querySelector('#detail-content')?.textContent.includes('Row 2 edited'));
  if(!injected||await page.evaluate(()=>window.__savedPendingTable))throw Error('Save completed with an unready table');
  await page.evaluate(()=>window.__rowObserver.disconnect());await page.unroute(file+'*',refreshRoute);
  console.log('Row controls passed: checkbox bounds, sticky alignment, horizontal/vertical scrolling, both densities/themes, six widths, zoom, selection and table replacement.');
 }finally{if(file){await page.goto(base+'/collections');const csrf=await page.locator('meta[name=csrf-token]').getAttribute('content');await page.request.post(file+'/delete',{form:{csrf,confirm:'delete'}});}}
}
