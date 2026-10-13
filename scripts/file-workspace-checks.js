async (page, artifactDir) => {
 const base=new URL(page.url()).origin,name='single-workspace-'+Date.now()+'.json';
 let id;
 try {
  await page.goto(base+'/collections');await page.locator('#import>summary').click();
  await page.locator('#import-file').setInputFiles({name,mimeType:'application/json',buffer:Buffer.from(JSON.stringify(Array.from({length:60},(_,i)=>({id:i+1,name:i===0?' Ada ':'Person '+i,nested:{enabled:true}}))))});
  await page.getByRole('button',{name:'Open selected files',exact:true}).click();
  await page.locator('#collection-table tr').filter({hasText:name}).filter({hasText:'ready'}).waitFor();
  const href=await page.getByRole('link',{name,exact:true}).getAttribute('href');id=href.split('/')[2];
  await page.goto(base+href);
  const source=page.locator('#detail-content'),pane=page.locator('#file-workspace-tool');
  const more=page.locator('.workspace-more'),moreSummary=more.locator('summary');
  const visibleTargets=async locator=>{const bad=await locator.evaluateAll(links=>links.filter(el=>{const r=el.getBoundingClientRect();return r.left<0||r.right>innerWidth||r.top<0||r.bottom>innerHeight||!el.contains(document.elementFromPoint(r.x+r.width/2,r.y+r.height/2));}).map(el=>el.textContent.trim()));if(bad.length)throw Error('Clipped or covered controls: '+bad.join(', '));};
  for(const width of [1440,1100,768,390,320]){await page.setViewportSize({width,height:1000});for(const theme of ['light','dark']){
   await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);await moreSummary.focus();await page.keyboard.press('Enter');
   await visibleTargets(more.locator('a'));if(await more.evaluate(el=>{const nav=el.closest('nav');return ['auto','scroll'].includes(getComputedStyle(nav).overflowY)&&nav.scrollHeight>nav.clientHeight+1;}))throw Error('More menu creates a toolbar scrollbar');
   if([1440,320].includes(width))await page.screenshot({path:artifactDir+'/more-'+width+'-'+theme+'.png'});
   await page.keyboard.press('Escape');if(await more.getAttribute('open')!==null)throw Error('Escape did not close More');if(!await moreSummary.evaluate(el=>el===document.activeElement))throw Error('More did not restore keyboard focus');
   await moreSummary.click();await page.locator('.collection-title').click();if(await more.getAttribute('open')!==null)throw Error('More did not close on outside click');
   for(const name of ['Filter and sort','Columns']){const summary=source.locator('.toolbar-disclosure>summary').filter({hasText:name});await summary.click();if(await source.locator('.sheet-search').evaluate(el=>el.getBoundingClientRect().width<100))throw Error('Search field squeezed at '+width);const controls=source.locator('.toolbar-disclosure[open] input:visible,.toolbar-disclosure[open] select:visible,.toolbar-disclosure[open] button:visible');for(const control of await controls.all()){await control.scrollIntoViewIfNeeded();await visibleTargets(control);}if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error('Toolbar overflow at '+width);await page.keyboard.press('Escape');}
  }}
  await page.setViewportSize({width:1440,height:1000});
  for(const [key,label] of [['insights','Column summaries'],['quality','Validate data'],['raw','Raw JSON'],['schema','Schema'],['api','API settings']]){
   await moreSummary.click();await more.locator('[data-workspace-target='+key+']').click();await pane.locator('#workspace-tool-title').filter({hasText:label}).waitFor();await page.waitForFunction(()=>!document.querySelector('#file-workspace-tool').hasAttribute('aria-busy')&&document.querySelector('#workspace-tool-content').textContent.trim().length>0);
   if(await more.getAttribute('open')!==null||!await moreSummary.evaluate(el=>el.hasAttribute('data-active')))throw Error('More tool selection state missing');
   await moreSummary.click();if(!await more.locator('[data-workspace-target='+key+']').evaluate(el=>el.getAttribute('aria-current')==='page'))throw Error('More does not identify active tool');await page.keyboard.press('Escape');if(!await pane.isVisible())throw Error('Closing More also closed the active tool');await page.keyboard.press('Escape');if(!await moreSummary.evaluate(el=>el===document.activeElement))throw Error('Closing More tool focused an invisible link');
  }
  await page.evaluate(()=>{document.querySelector('#detail-content').dataset.persistenceTest='kept';});
  await page.locator('[data-workspace-target=clean]').click();
  await pane.getByLabel('Action',{exact:true}).selectOption('trim');
  await pane.getByLabel('Column',{exact:true}).fill('name');
  await pane.getByRole('button',{name:'Add change',exact:true}).click();
  await page.locator('#workspace-result').getByRole('heading',{name:'Cleaned data',exact:true}).waitFor();
  const aligned=await pane.locator('.cleanup-controls').first().evaluate(row=>{const controls=[...row.querySelectorAll('input,select,button')].filter(e=>e.getBoundingClientRect().height);return Math.max(...controls.map(e=>e.getBoundingClientRect().left))-Math.min(...controls.map(e=>e.getBoundingClientRect().left))<2;});
  if(!aligned)throw Error('Cleanup inputs and action button are not aligned');
  if(!await pane.getByText('Trim spaces in name',{exact:true}).count())throw Error('Cleanup not retained');
  const preview=page.locator('#workspace-result');
  if(await source.isVisible()||await preview.locator('tbody tr').count()!==20)throw Error('Cleanup must show one result sample table');
  await page.getByRole('button',{name:'Original file',exact:true}).click();if(!await source.isVisible()||await preview.isVisible())throw Error('Original/result switch unclear');
  await page.getByRole('button',{name:'After changes',exact:true}).click();
  await pane.getByRole('button',{name:'Download cleaned data',exact:true}).click();
  await pane.locator('[data-download-source]').selectOption('current');
  const fullResult=page.waitForEvent('download');await pane.locator('[data-download-submit]').click();
  const file=await fullResult,stream=await file.createReadStream(),chunks=[];for await(const chunk of stream)chunks.push(chunk);
  const output=JSON.parse(Buffer.concat(chunks).toString());if(output.length!==60||output[0].name!=='Ada')throw Error('Cleanup download used the sample or wrong data');
  const originalResponse=await page.request.get(base+href+'/export?original=1');if((await originalResponse.json())[0].name!==' Ada ')throw Error('Cleanup changed the original');
  await pane.getByRole('button',{name:'Download another format',exact:true}).click();await pane.locator('[data-download-source]').selectOption('original');await pane.locator('#collection-download-format').selectOption('original');
  const exactDownload=page.waitForEvent('download');await pane.locator('[data-download-submit]').click();if(await(await exactDownload).failure())throw Error('Original download failed after cleanup');
  await page.locator('[data-workspace-target=sql]').click();
  const sql=pane.locator('#sql-editor');await sql.fill('SELECT COUNT(*) AS total FROM "c_'+id+'"');
  await pane.getByRole('button',{name:'Run query',exact:true}).click();
  await pane.getByRole('heading',{name:'Results',exact:true}).waitFor();
  await pane.getByRole('cell',{name:'60',exact:true}).waitFor();
  await sql.fill('SELECT id FROM "c_'+id+'" LIMIT 3');
  await page.locator('[data-workspace-target=clean]').click();
  await page.locator('#workspace-result').getByRole('heading',{name:'Cleaned data',exact:true}).waitFor();
  await page.locator('[data-workspace-target=sql]').click();
  if(await sql.inputValue()!=='SELECT id FROM "c_'+id+'" LIMIT 3')throw Error('SQL draft lost');
  if(new URL(page.url()).pathname!==href||await source.getAttribute('data-persistence-test')!=='kept')throw Error('Workspace navigated or source table replaced');
  await source.getByRole('link',{name:'Page 2',exact:true}).click();await source.locator('.detail-body[data-page="2"]').waitFor();
  if(await sql.inputValue()!=='SELECT id FROM "c_'+id+'" LIMIT 3'||!await pane.isVisible())throw Error('Pagination lost the active SQL draft');
  await source.getByRole('link',{name:'Sort id',exact:true}).click();await source.locator('th[aria-sort=ascending]').waitFor();
  if(await sql.inputValue()!=='SELECT id FROM "c_'+id+'" LIMIT 3')throw Error('Sorting lost the SQL draft');
  await sql.fill('SELECT invalid_column FROM "c_'+id+'"');
  await pane.getByRole('button',{name:'Run query',exact:true}).click();
  await pane.getByRole('alert').waitFor();
  if(!(await sql.inputValue()).includes('invalid_column'))throw Error('SQL lost after error');
  await page.locator('[data-workspace-target=export]').click();await pane.locator('#collection-download-format').selectOption('json');
  const delayed='**/collections/'+id+'/export';await page.route(delayed,async route=>{const response=await route.fetch();await page.waitForTimeout(500);await route.fulfill({response});});
  const backgroundDownload=page.waitForEvent('download');await pane.locator('[data-download-submit]').click();await page.locator('[data-workspace-target=sql]').click();
  if(await(await backgroundDownload).failure()||!(await sql.inputValue()).includes('invalid_column'))throw Error('Download stopped or SQL draft changed while switching tools');await page.unroute(delayed);
  await page.locator('[data-workspace-target=export]').click();
  await pane.getByRole('button',{name:'Download another format',exact:true}).click();
  await pane.locator('#collection-download-format').selectOption('xlsx');
  await pane.getByText('Format details',{exact:true}).click();await pane.getByRole('button',{name:'Check format changes',exact:true}).click();
  await pane.getByRole('heading',{name:'Format compatibility',exact:true}).waitFor();
  const automatic=page.waitForEvent('download');await pane.getByRole('button',{name:'Download file',exact:true}).click();
  const ready=pane.locator('a[href*="/download"]');await ready.waitFor();
  if(await(await automatic).failure())throw Error('Automatic workspace export failed');
  await page.locator('[data-workspace-target=clean]').click();
  for(const width of [1440,390]){await page.setViewportSize({width,height:1000});if(width===390)await pane.evaluate(el=>el.scrollIntoView({block:'start'}));for(const theme of ['light','dark']){await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);await page.waitForTimeout(200);if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error('Workspace overflow '+width);const unreadable=await page.evaluate(()=>{const rgb=s=>s.match(/[\d.]+/g)?.map(Number),lum=rgb=>rgb.slice(0,3).map(v=>v/255).map(v=>v<=.04045?v/12.92:((v+.055)/1.055)**2.4).reduce((s,v,i)=>s+v*[.2126,.7152,.0722][i],0);return [...document.querySelectorAll('.button,.icon-button')].filter(el=>el.getBoundingClientRect().height&&el.textContent.trim()&&getComputedStyle(el).visibility!=='hidden').flatMap(el=>{const style=getComputedStyle(el);let current=el,bg;while(current){bg=rgb(getComputedStyle(current).backgroundColor);if(bg&&(bg.length<4||bg[3]===1))break;current=current.parentElement;}const fg=rgb(style.color);if(!bg||!fg)return [];const a=lum(fg),b=lum(bg),ratio=(Math.max(a,b)+.05)/(Math.min(a,b)+.05);return ratio<4.5?[el.textContent.trim()+': '+ratio.toFixed(2)]:[];});});if(unreadable.length)throw Error('Unreadable buttons in '+theme+': '+unreadable.join(', '));await page.screenshot({path:artifactDir+'/workspace-'+width+'-'+theme+'.png'});}}
  await page.keyboard.press('Escape');if(await pane.isVisible())throw Error('Escape did not close the tool');
  await page.locator('[data-workspace-target=sql]').click();if(!(await sql.inputValue()).includes('invalid_column'))throw Error('Draft lost after closing');
  const native=await page.context().browser().newContext({javaScriptEnabled:false});
  try{const p=await native.newPage();await p.goto(base+href);await p.locator('.workspace-more>summary').click();await p.getByRole('link',{name:'Schema',exact:true}).click();if(!new URL(p.url()).searchParams.get('tab'))throw Error('Native More link failed');await p.goto(base+href);await p.getByRole('link',{name:'SQL',exact:true}).click();await p.locator('#sql-editor').fill('SELECT COUNT(*) AS total FROM "c_'+id+'"');await p.getByRole('button',{name:'Run query',exact:true}).click();await p.getByRole('cell',{name:'60',exact:true}).waitFor();await p.goto(base+href);await p.getByRole('link',{name:'Download',exact:true}).click();await p.locator('#collection-download-format').waitFor();}finally{await native.close();}
  console.log('Single collection workspace passed: persistent source, cleanup/SQL drafts, inline errors, exports, themes/mobile, Escape and no-JavaScript forms.');
 } catch(error) {
  console.log('Workspace failure at '+page.url()+': '+(await page.locator('#file-workspace-tool').textContent()).slice(-2600));
  await page.screenshot({path:artifactDir+'/workspace-failure.png'});throw error;
 } finally {
  if(id){await page.goto(base+'/collections');const csrf=await page.locator('#import input[name=csrf]').inputValue();await page.request.post(base+'/collections/'+id+'/delete',{form:{csrf,confirm:'delete'}});}
 }
}
