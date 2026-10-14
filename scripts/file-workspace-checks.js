async (page, artifactDir) => {
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
  await checkToolbar(page.locator('#detail-content'),'Original toolbar');
  await open('clean');if(await page.locator('#workspace-original').getAttribute('open')!==null)throw Error('Original starts expanded in Clean');
  await pane.getByLabel('Change',{exact:true}).selectOption('trim');await pane.getByLabel('Column',{exact:true}).selectOption('name');await pane.getByRole('button',{name:'Apply change',exact:true}).click();
  await result.locator('.sheet-data').waitFor();await result.locator('[data-cleaned-total="60"]').waitFor();
  await checkToolbar(result,'Cleaned toolbar');
  if(await pane.locator('[data-cleanup-preview],table').count())throw Error('Cleanup still duplicates the sample inside the controls');
  if(await page.locator('#detail-content').isVisible())throw Error('Original table should be collapsed in Clean');
  await page.locator('#workspace-original>summary').click();await page.locator('#detail-content').waitFor({state:'visible'});
  const source=await page.locator('#detail-content').boundingBox(),cleaned=await result.boundingBox();if(cleaned.y<source.y+source.height)throw Error('Original is not above the cleanup result');
  await page.locator('#workspace-original>summary').click();await result.getByRole('link',{name:'Page 2',exact:true}).click();await result.getByText('Page 2 of 3',{exact:true}).waitFor();
  await open('sql');await pane.locator('#sql-editor').fill('SELECT COUNT(*) AS total FROM "c_'+id+'"');await pane.getByRole('button',{name:'Run query',exact:true}).click();
  const sqlResult=page.locator('#workspace-sql-result');await sqlResult.getByRole('cell',{name:'60',exact:true}).waitFor();if(await pane.locator('table').count())throw Error('SQL results remain inside editor controls');
  if(await page.locator('#workspace-original').getAttribute('open')!==null)throw Error('Original starts expanded in SQL');
  await pane.locator('#sql-editor').fill('SELECT id FROM "c_'+id+'" LIMIT 3');await open('clean');await result.getByText('Page 2 of 3',{exact:true}).waitFor();await open('sql');if(await pane.locator('#sql-editor').inputValue()!=='SELECT id FROM "c_'+id+'" LIMIT 3')throw Error('SQL draft lost on view switch');
  await pane.locator('#sql-editor').fill('SELECT invalid_column FROM "c_'+id+'"');await pane.getByRole('button',{name:'Run query',exact:true}).click();await pane.getByRole('alert').waitFor();if(await sqlResult.isVisible())throw Error('Failed SQL shows stale output');
  let failedRead=false;await page.route('**/collections/'+id+'/quality',async route=>{if(!failedRead){failedRead=true;await route.abort('failed');}else await route.continue();});
  for(const key of ['clean','sql','insights','quality','schema','raw','settings','export']){
   await open(key);
   if(key==='clean')await result.locator('.sheet-data').waitFor();
   for(const width of [320,390,768,1024,1440,2560]){await page.setViewportSize({width,height:1000});for(const theme of ['light','dark']){await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);await fit(key+' '+width+' '+theme);if([390,1440].includes(width)){await page.evaluate(()=>scrollTo(0,0));await page.screenshot({path:artifactDir+'/focused-'+key+'-'+width+'-'+theme+'.png'});}}}
   await page.setViewportSize({width:1440,height:1000});
   await calmLayout(key+' desktop');await page.setViewportSize({width:390,height:1000});await calmLayout(key+' mobile');await page.setViewportSize({width:1440,height:1000});
   await page.evaluate(()=>document.documentElement.style.zoom='2');await fit(key+' 200 percent zoom');await page.evaluate(()=>document.documentElement.style.zoom='');
   if(key==='insights'){
    for(const width of [320,390,768,1024,1440,2560]){
     await page.setViewportSize({width,height:1000});
     const card=pane.locator('.column-summary').first(),summary=card.locator('summary');
     const size=await summary.evaluate(el=>({height:el.getBoundingClientRect().height,padding:parseFloat(getComputedStyle(el).paddingLeft)}));
     if(size.height>56||size.height<40||size.padding<12)throw Error('Column summary needs compact padded controls at '+width);
     await summary.focus();await page.keyboard.press('Enter');
     if(await card.getAttribute('open')===null)throw Error('Summary keyboard expansion failed');
     await fit('Expanded summary '+width);
     if([390,1440].includes(width))await page.screenshot({path:artifactDir+'/summary-expanded-'+width+'.png'});
     await page.keyboard.press('Enter');
    }
    await page.setViewportSize({width:1440,height:1000});
   }
  }
  await open('clean');await pane.getByText('Advanced',{exact:true}).click();await fit('Expanded advanced cleanup');
  await page.evaluate(()=>document.documentElement.style.zoom='2');await fit('200 percent zoom');await page.evaluate(()=>document.documentElement.style.zoom='');
  await page.getByRole('link',{name:'Data',exact:true}).click();await page.getByRole('button',{name:'Cleaned',exact:true}).click();await open('export');if(await pane.locator('[data-download-source]').inputValue()!=='current')throw Error('Download lost selected cleaned dataset');
  const download=page.waitForEvent('download');await pane.locator('[data-download-submit]').click();const file=await download,chunks=[];for await(const chunk of await file.createReadStream())chunks.push(chunk);const text=Buffer.concat(chunks).toString();if(JSON.parse(text).length!==60||!text.includes('9007199254740993'))throw Error('Cleaned export changed rows or large integers');
  if((await(await page.request.get(base+'/collections/'+id+'/export?original=1')).text())!==input)throw Error('Original source bytes changed');
  await open('sql');const draft='SELECT id FROM "c_'+id+'" LIMIT 2';await pane.locator('#sql-editor').fill(draft);await page.reload();await pane.locator('#sql-editor').waitFor();if(await pane.locator('#sql-editor').inputValue()!==draft||await sqlResult.locator('table').count())throw Error('Restored SQL lost draft or executed automatically');
  await page.keyboard.press('Escape');if(await pane.isVisible())throw Error('Escape does not return to Data');
  const native=await page.context().browser().newContext({javaScriptEnabled:false});try{const p=await native.newPage();await p.goto(base+'/collections/'+id);await p.setViewportSize({width:1440,height:1000});await toolbarLayout(p.locator('#detail-content'),'Native toolbar');await p.setViewportSize({width:390,height:1000});for(const name of ['Filter and sort','Columns and table options']){const summary=p.getByLabel(name,{exact:true});await summary.click();if(await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error('Native expanded '+name+' overflows');await summary.click();}await p.goto(base+'/collections/'+id+'/changes');if(await p.locator('#workspace-original').getAttribute('open')!==null)throw Error('Native original starts expanded');await p.locator('#workspace-original>summary').click();await p.locator('#detail-content tbody tr').first().waitFor();await p.goto(base+'/collections/'+id+'/sql');await p.locator('#sql-editor').fill('SELECT COUNT(*) AS total FROM "c_'+id+'"');await p.getByRole('button',{name:'Run query',exact:true}).click();await p.locator('#workspace-sql-result').getByRole('cell',{name:'60',exact:true}).waitFor();await p.getByRole('link',{name:'Convert / Download',exact:true}).click();await p.locator('#collection-download-format').waitFor();}finally{await native.close();}
  console.log('Focused workspace passed: all full-width views, expandable original, preserved drafts/pagination, errors, exact exports, six widths, both themes, zoom and no-JavaScript views.');
 }catch(error){await page.screenshot({path:artifactDir+'/focused-failure.png',fullPage:true});console.log('Focused failure: '+page.url()+' '+(await pane.textContent()).slice(-1400));throw error;}
 finally{if(id){await page.goto(base+'/collections');const csrf=await page.locator('#import input[name=csrf]').inputValue();await page.request.post(base+'/collections/'+id+'/delete',{form:{csrf,confirm:'delete'}});}}
}
