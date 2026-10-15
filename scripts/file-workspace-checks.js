async (page, artifactDir) => {
 const applyChange=async()=>{
  const response=page.waitForResponse(r=>r.request().method()==='POST'&&new URL(r.url()).pathname.endsWith('/changes'));
  await page.getByRole('button',{name:'Apply change',exact:true}).click();
  const result=await response;if(!result.ok())throw Error('Cleanup request failed: '+result.status());
  await page.waitForFunction(()=>!document.querySelector('#file-workspace-tool[aria-busy]')&&document.querySelector('[data-file-edit-status]')?.textContent==='Saved'&&!document.querySelector('[data-file-job]'));
 };
 const base=new URL(page.url()).origin,name='focused-workspace-'+Date.now()+'-Unicode-山田-long-file-name.json';let id;
 const pane=page.locator('#file-workspace-tool'),result=page.locator('#workspace-result');
 const settled=()=>page.waitForFunction(()=>!document.querySelector('#file-workspace-tool[aria-busy]'));
 const calmLayout=async label=>{
  const problems=await page.evaluate(()=>{
   const visible=e=>e.checkVisibility({checkOpacity:true,checkVisibilityCSS:true})&&!e.closest('details:not([open])')&&!e.closest('.sheet-table');
   const target=innerWidth<=767?44:40,issues=[];
   for(const el of document.querySelectorAll('.field-control,.button,.icon-button')){
    if(!visible(el)||el.disabled||el.getAttribute('aria-disabled')==='true')continue;
    if(el.getBoundingClientRect().height<target-.75)issues.push('Short control '+(el.getAttribute('aria-label')||el.name||el.textContent.trim()).slice(0,40));
   }
   for(const el of document.querySelectorAll('.download-chooser,.workspace-clean-advanced,.column-summary-body')){
    if(!visible(el))continue;const padding=parseFloat(getComputedStyle(el).paddingLeft);
    if(padding<16||padding>24)issues.push('Unexpected content padding '+el.className+': '+padding);
   }
   for(const group of document.querySelectorAll('.cleanup-controls,.sql-actions,.insights-filter,.download-fields')){
    if(!visible(group))continue;
    const boxes=[...group.querySelectorAll('input:not([type=hidden]),select,button')].filter(visible).map(e=>e.getBoundingClientRect());
    for(let i=0;i<boxes.length;i++)for(let j=i+1;j<boxes.length;j++)if(Math.abs(boxes[i].top-boxes[j].top)<4&&Math.abs(boxes[i].bottom-boxes[j].bottom)>2)issues.push('Uneven controls in '+group.className);
   }
   const data=document.querySelector('.workspace-main[data-active-view=data] .detail-body');if(data&&visible(data)&&parseFloat(getComputedStyle(data).paddingLeft)!==0)issues.push('Double padding around Data');
   if(document.querySelector('#file-workspace-tool [data-close-workspace-tool]'))issues.push('Duplicate Back to data action');
   return [...new Set(issues)];
  });if(problems.length)throw Error(label+': '+problems.join('; '));
 };
 const toolbarLayout=async (scope,label)=>{
  const rowProblems=await scope.locator('.sheet-table').evaluateAll(tables=>tables.flatMap(table=>{
   if(!table.checkVisibility())return [];
   const issues=[];
   for(const cell of table.querySelectorAll('.sheet-select')){const box=cell.getBoundingClientRect(),input=cell.querySelector('input'),check=input?.getBoundingClientRect();if(check&&(check.width>20||check.left<box.left||check.right>box.right||check.top<box.top||check.bottom>box.bottom))issues.push('Clipped row checkbox');}
   const first=table.tBodies[0]?.rows[0],select=first?.querySelector('.sheet-select'),index=first?.querySelector('.sheet-index');if(select&&index&&index.getBoundingClientRect().left<select.getBoundingClientRect().right-1)issues.push('Row number overlaps selection column');
   return issues;
  }));if(rowProblems.length)throw Error(label+': '+rowProblems.join('; '));
  const problems=await scope.locator('.sheet-query-bar').evaluateAll(groups=>{
   const issues=[];
   for(const group of groups){
    if(!group.checkVisibility())continue;
    const controls=[...group.querySelectorAll('.sheet-search input,.toolbar-disclosure>summary,.sheet-density select,.sheet-search-submit')].filter(e=>e.checkVisibility()).map(e=>({name:e.getAttribute('aria-label')||e.tagName,box:e.getBoundingClientRect()}));
    for(let i=0;i<controls.length;i++)for(let j=i+1;j<controls.length;j++){
     const a=controls[i],b=controls[j],ac=(a.box.top+a.box.bottom)/2,bc=(b.box.top+b.box.bottom)/2;
     if(Math.abs(ac-bc)<20&&(Math.abs(a.box.top-b.box.top)>1||Math.abs(a.box.bottom-b.box.bottom)>1))issues.push('Misaligned '+a.name+' / '+b.name);
    }
   }return issues;
  });if(problems.length)throw Error(label+': '+problems.join('; '));
 };
 const checkToolbar=async (scope,label)=>{
  for(const width of [320,390,768,1024,1440,2560]){
   await page.setViewportSize({width,height:1000});
   for(const theme of ['light','dark']){
    await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);
    for(const density of ['compact','comfortable']){
     await scope.locator('[data-sheet-density]').selectOption(density);
     await toolbarLayout(scope,label+' '+width+' '+theme+' '+density);
    }
   }
   for(const name of ['Filter and sort','Columns and table options']){
    const summary=scope.getByLabel(name,{exact:true});await summary.click();
    const fits=await summary.evaluate(el=>{const details=el.parentElement,bar=details.closest('.sheet-query-bar');return details.getBoundingClientRect().width>=bar.getBoundingClientRect().width-1&&getComputedStyle(details).marginTop==='0px';});
    if(!fits)throw Error(label+' expanded '+name+' has cramped width or extra margin at '+width);
    if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error(label+' expanded '+name+' overflows at '+width);
    if(name==='Columns and table options'){
     const bad=await scope.locator('.column-options input,.table-preferences input[type=checkbox]').evaluateAll(es=>es.filter(e=>e.checkVisibility()).some(e=>e.getBoundingClientRect().width>24||getComputedStyle(e.parentElement).display!=='flex'));
     if(bad)throw Error(label+' column checkbox labels lose their inline layout at '+width);
    }
    await summary.click();
   }
  }
  await page.setViewportSize({width:1440,height:1000});
 };
 const open=async key=>{
  if(['insights','quality','schema','raw'].includes(key)){await page.locator('.workspace-primary-views [data-workspace-target=insights]').click();await settled();if(key!=='insights'){await page.locator('[data-workspace-group=inspect] [data-workspace-target='+key+']').click();await settled();}}
  else if(key==='settings'){if(await page.locator('.workspace-file-action').isVisible())await page.locator('.workspace-file-action').click();else{await page.locator('.workspace-more>summary').click();await page.locator('.workspace-extra-tools [data-workspace-target=settings]').click();}await settled();}
  else {await page.locator((key==='export'?'.workspace-header-actions ':'.workspace-primary-views ')+'[data-workspace-target='+key+']').click();await settled();}
 };
 const fit=async label=>{if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error(label+' overflows the page');if(await pane.isVisible()){const box=await pane.boundingBox(),main=await page.locator('.workspace-main').boundingBox();if(box.width<main.width-2)throw Error(label+' is still in a narrow tool host');}const clipped=await page.locator('.workspace-toolbar a:visible,.workspace-header-actions a:visible').evaluateAll(items=>items.filter(el=>{const r=el.getBoundingClientRect();return r.left<0||r.right>innerWidth+1;}).map(el=>el.textContent.trim()));if(clipped.length)throw Error(label+' clipped actions: '+clipped.join(', '));};
 try{
  await page.setViewportSize({width:1440,height:1000});await page.goto(base+'/collections');await page.locator('#import>summary').click();
  const input=JSON.stringify(Array.from({length:60},(_,i)=>({id:i+1,name:i===0?' Ada ':'Person '+i,'a.b/x':'山田 '+i,nested:{enabled:true},empty:'',nil:null}))).replace('"id":1,','"id":1,"large":9007199254740993,');
  await page.locator('#import-file').setInputFiles({name,mimeType:'application/json',buffer:Buffer.from(input)});await page.getByRole('button',{name:'Open selected files',exact:true}).click();
  const row=page.locator('#collection-table tr').filter({hasText:name});await row.filter({hasText:'ready'}).waitFor();id=(await row.getByRole('link',{name,exact:true}).getAttribute('href')).split('/')[2];await page.goto(base+'/collections/'+id);
  if(await page.locator('link[rel=stylesheet]').count()!==3)throw Error('Legacy stylesheet layers still load');
  await page.locator('#detail-content .sheet-data').waitFor();
  for(const width of [320,390,768,1024,1440,2560]){await page.setViewportSize({width,height:1000});await fit('Data '+width);}
  const table=page.locator('#detail-content');
  await checkToolbar(table,'File toolbar');
  if(await page.locator('[data-workspace-view],#workspace-original').count())throw Error('Competing file versions remain');
  const fieldIndex=await table.locator('[data-sheet-columns] input').evaluateAll(es=>es.find(e=>e.value==='name')?.dataset.sheetColumn).catch(()=>null);
  const cell=table.locator('td[data-sheet-cell="'+(fieldIndex||'1')+'"]').first();
  const previous=await cell.locator('[data-cell-raw]').inputValue();
  await cell.dblclick();await cell.locator('input').fill('Edited Unicode 山田');await cell.locator('input').press('Enter');
  await page.waitForFunction(()=>document.querySelector('[data-file-edit-status]')?.textContent==='Saved'&&document.querySelector('#detail-content')?.textContent.includes('Edited Unicode 山田'));
  await page.locator('[data-file-undo]').click();await page.waitForFunction(()=>document.querySelector('[data-file-edit-status]')?.textContent==='Saved'&&!document.querySelector('#detail-content')?.textContent.includes('Edited Unicode 山田'));
  if((await(await page.request.get(base+'/collections/'+id+'/export?original=1')).text())!==input)throw Error('Undo did not restore exact bytes');
  await page.locator('[data-file-redo]').click();await page.waitForFunction(()=>document.querySelector('[data-file-edit-status]')?.textContent==='Saved'&&document.querySelector('#detail-content')?.textContent.includes('Edited Unicode 山田'));
  await page.locator('[data-file-undo]').click();await page.waitForFunction(()=>document.querySelector('[data-file-edit-status]')?.textContent==='Saved'&&!document.querySelector('#detail-content')?.textContent.includes('Edited Unicode 山田'));
  await open('clean');
  await pane.getByLabel('Change',{exact:true}).selectOption('trim');await pane.getByLabel('Column',{exact:true}).selectOption('name');await applyChange();
  await page.waitForFunction(()=>document.querySelector('[data-file-edit-status]')?.textContent==='Saved'&&!document.querySelector('[data-file-job]'));
  await checkToolbar(table,'Edited file toolbar');
  if(await result.locator('.sheet-data').count())throw Error('Cleanup duplicates the file table');
  if(!await table.isVisible())throw Error('File table missing under cleanup');
  await table.getByRole('link',{name:'Page 2',exact:true}).click();await table.getByText('Page 2 of 3',{exact:true}).waitFor();
  await open('sql');await pane.locator('#sql-editor').fill('SELECT COUNT(*) AS total FROM "c_'+id+'"');await pane.getByRole('button',{name:'Run query',exact:true}).click();
  const sqlResult=page.locator('#workspace-sql-result');await sqlResult.getByRole('cell',{name:'60',exact:true}).waitFor();if(await pane.locator('table').count())throw Error('SQL results remain inside editor controls');
  if(await table.isVisible())throw Error('SQL shows an unrelated file table');
  await pane.locator('#sql-editor').fill('SELECT id FROM "c_'+id+'" LIMIT 3');await open('clean');await table.getByText('Page 2 of 3',{exact:true}).waitFor();await open('sql');if(await pane.locator('#sql-editor').inputValue()!=='SELECT id FROM "c_'+id+'" LIMIT 3')throw Error('SQL draft lost on view switch');
  await pane.locator('#sql-editor').fill('SELECT invalid_column FROM "c_'+id+'"');await pane.getByRole('button',{name:'Run query',exact:true}).click();await pane.getByRole('alert').waitFor();if(await sqlResult.isVisible())throw Error('Failed SQL shows stale output');
  let failedRead=false;await page.route('**/collections/'+id+'/quality',async route=>{if(!failedRead){failedRead=true;await route.abort('failed');}else await route.continue();});
  for(const key of ['clean','sql','insights','quality','schema','raw','settings','export']){
   await open(key);
   if(key==='clean')await table.locator('.sheet-data').waitFor();
   for(const width of [320,390,768,1024,1440,2560]){await page.setViewportSize({width,height:1000});for(const theme of ['light','dark']){await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);await fit(key+' '+width+' '+theme);if([390,1440].includes(width)){await page.evaluate(()=>scrollTo(0,0));await page.screenshot({path:artifactDir+'/focused-'+key+'-'+width+'-'+theme+'.png'});}}}
   await page.setViewportSize({width:1440,height:1000});
   await calmLayout(key+' desktop');await page.setViewportSize({width:390,height:1000});await calmLayout(key+' mobile');await page.setViewportSize({width:1440,height:1000});
   await page.evaluate(()=>document.documentElement.style.zoom='2');await fit(key+' 200 percent zoom');await page.evaluate(()=>document.documentElement.style.zoom='');
   if(key==='insights'){
    for(const width of [320,390,768,1024,1440,2560]){
     await page.setViewportSize({width,height:1000});
     const table=pane.locator('.insights-table'),buttons=table.locator('button[data-column-details]'),first=buttons.first();
     if(await table.locator('.column-summary-row').count()<5||await pane.locator('.column-summary-grid').count())throw Error('Column summaries are not one table');
     const id=await first.getAttribute('aria-controls'),details=table.locator('#'+id);
     await first.focus();await page.keyboard.press('Enter');
     if(await first.getAttribute('aria-expanded')!=='true'||!await details.isVisible())throw Error('Column detail keyboard expansion failed');
     if(await details.locator('td[colspan="6"]').count()!==1)throw Error('Column details do not span the summary table');
     await fit('Expanded column details '+width);
     const body=details.locator('.column-summary-body'),available=(await pane.locator('.insights-table-scroll').boundingBox()).width-2;if((await body.boundingBox()).width<Math.min(960,available)-2)throw Error('Column details still use a narrow card at '+width);
     if([390,1440].includes(width))await page.screenshot({path:artifactDir+'/summary-expanded-'+width+'.png'});
     await buttons.nth(1).click();
     if(await details.isVisible()||await table.locator('.column-detail-row:not([hidden])').count()!==1)throw Error('Opening another column leaves competing detail panels');
     await buttons.nth(1).click();if(await table.locator('.column-detail-row:not([hidden])').count())throw Error('Column details do not close');
    }
    await page.setViewportSize({width:1440,height:1000});
   }
  }
  await open('clean');await pane.getByText('Advanced',{exact:true}).click();await fit('Expanded advanced cleanup');
  await page.evaluate(()=>document.documentElement.style.zoom='2');await fit('200 percent zoom');await page.evaluate(()=>document.documentElement.style.zoom='');
  await page.getByRole('link',{name:'Data',exact:true}).click();await open('export');if(await pane.locator('[data-download-source]').inputValue()!=='original')throw Error('Download lost current file');
  const download=page.waitForEvent('download');await pane.locator('[data-download-submit]').click();const file=await download,chunks=[];for await(const chunk of await file.createReadStream())chunks.push(chunk);const text=Buffer.concat(chunks).toString();if(JSON.parse(text).length!==60||!text.includes('9007199254740993'))throw Error('Cleaned export changed rows or large integers');
  const savedFile=await(await page.request.get(base+'/collections/'+id+'/export?original=1')).text();if(!savedFile.includes('9007199254740993')||JSON.parse(savedFile)[0].name!=='Ada')throw Error('Cleanup did not save exact values to the file');
  await open('sql');const draft='SELECT id FROM "c_'+id+'" LIMIT 2';await pane.locator('#sql-editor').fill(draft);await page.reload();await pane.locator('#sql-editor').waitFor();if(await pane.locator('#sql-editor').inputValue()!==draft||await sqlResult.locator('table').count())throw Error('Restored SQL lost draft or executed automatically');
  await page.keyboard.press('Escape');if(await pane.isVisible())throw Error('Escape does not return to Data');
  const native=await page.context().browser().newContext({javaScriptEnabled:false});try{const p=await native.newPage();await p.goto(base+'/collections/'+id);await p.setViewportSize({width:1440,height:1000});await toolbarLayout(p.locator('#detail-content'),'Native toolbar');await p.setViewportSize({width:390,height:1000});for(const name of ['Filter and sort','Columns and table options']){const summary=p.getByLabel(name,{exact:true});await summary.click();if(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error('Native expanded '+name+' overflows');await summary.click();}await p.goto(base+'/collections/'+id+'/changes');if(await p.locator('#workspace-original').count())throw Error('Native file view still has a comparison selector');await p.locator('#detail-content tbody tr').first().waitFor();await p.goto(base+'/collections/'+id+'/sql');await p.locator('#sql-editor').fill('SELECT COUNT(*) AS total FROM "c_'+id+'"');await p.getByRole('button',{name:'Run query',exact:true}).click();await p.locator('#workspace-sql-result').getByRole('cell',{name:'60',exact:true}).waitFor();await p.goto(base+'/collections/'+id+'/insights');await p.locator('a[data-column-details]').first().click();await p.locator('.column-detail-row:not([hidden])').waitFor();if(await p.locator('.insights-table').count()!==1)throw Error('Native summaries are not a table');await p.getByRole('link',{name:'Convert / Download',exact:true}).click();await p.locator('#collection-download-format').waitFor();}finally{await native.close();}
  console.log('Focused workspace passed: single-file editing, Undo/Redo, saved cleanup, all full-width views, preserved drafts/pagination, errors, exact exports, six widths, both themes, zoom and no-JavaScript views.');
 }catch(error){await page.screenshot({path:artifactDir+'/focused-failure.png',fullPage:true});console.log('Focused failure: '+page.url()+' '+(await pane.textContent()).slice(-1400));throw error;}
 finally{if(id){await page.goto(base+'/collections');const csrf=await page.locator('#import input[name=csrf]').inputValue();await page.request.post(base+'/collections/'+id+'/delete',{form:{csrf,confirm:'delete'}});}}
}
