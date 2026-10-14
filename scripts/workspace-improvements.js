async (page,artifactDir,workbook) => {
 const errors=[];const onError=error=>errors.push(error.message);page.on('pageerror',onError);
 const openMore=async()=>{const summary=page.locator('.workspace-more>summary');if(await summary.isVisible())await summary.click();};
 const base=new URL(page.url()).origin,prefix='workspace-'+Date.now();
 const sources=[{name:prefix+'-left.json',rows:[{'key.x':1,name:' Ada ',nested:{n:'9007199254740993'}},{'key.x':1,name:' Ada ',nested:{n:'9007199254740993'}},{'key.x':2,name:'Grace'}]},{name:prefix+'-right.json',rows:[{'key/y':1,value:'a'},{'key/y':1,value:'b'}]},{name:prefix+'-next.json',rows:[{name:' Alan '}]}];
 const imported=[],recipes=[];
 await page.setViewportSize({width:1440,height:1000});
 try {
  await page.goto(base+'/collections');await page.locator('#import>summary').click();
  await page.locator('#import-file').setInputFiles([...sources.map(s=>({name:s.name,mimeType:'application/json',buffer:Buffer.from(JSON.stringify(s.rows))})),{name:prefix+'.xlsx',mimeType:'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',buffer:workbook}]);
  await page.getByRole('button',{name:'Open selected files',exact:true}).click();
  for(const name of [...sources.map(s=>s.name),prefix+'.xlsx']){
   await page.locator('#collection-table tr').filter({hasText:name}).filter({hasText:'ready'}).waitFor();
   const href=await page.getByRole('link',{name,exact:true}).getAttribute('href');imported.push(href.split('/')[2]);
  }
  const [left,right,next,xlsx]=imported;
  await page.goto(base+'/collections/'+left);await page.getByLabel('Table options',{exact:true}).click();await page.locator('[data-show-types]').check();await page.keyboard.press('Escape');await page.getByRole('link',{name:'Summary of name',exact:true}).click();
  await page.getByRole('heading',{name:'Column summaries',exact:true}).waitFor();
  if(!await page.getByText('1 duplicate rows found in checked data',{exact:true}).isVisible())throw Error('Missing duplicate suggestion');
  await page.getByRole('button',{name:'Preview trimming spaces',exact:true}).click();
  await page.locator('#workspace-result:not([hidden]) tbody tr').first().waitFor();
  await page.getByText('Advanced',{exact:true}).click();
  await page.getByLabel('Save these steps as',{exact:true}).fill(prefix+' recipe');await page.getByRole('button',{name:'Save these steps',exact:true}).click();
  await page.waitForFunction(()=>!!document.querySelector('[data-cleanup-form]')?.dataset.saved);
  recipes.push(await page.locator('[data-cleanup-form]').getAttribute('data-saved'));
  await page.goto(base+'/collections/'+next+'/changes');await page.getByText('Advanced',{exact:true}).click();await page.locator('.reuse-steps>summary').click();await page.getByRole('link',{name:prefix+' recipe',exact:true}).click();
  await page.locator('#workspace-result .sheet-data').waitFor();
  await page.locator('#workspace-result .cell-value').getByText('Alan',{exact:true}).waitFor();
  await page.goto(base+'/collections/'+left+'/changes?new=1');await page.getByText('Advanced',{exact:true}).click();await page.locator('.join-builder>summary').click();
  await page.getByLabel('Match this column',{exact:true}).fill('key.x');await page.getByLabel('Other dataset',{exact:true}).selectOption(right);await page.getByLabel('Match their column',{exact:true}).selectOption('key/y');
  await page.getByRole('button',{name:'Preview join',exact:true}).click();
  await page.getByText('Sample of 3 input rows: 2 matched, 1 unmatched; 2 have multiple matches. These are sample counts, not dataset totals.',{exact:true}).waitFor();
  await page.screenshot({path:artifactDir+'/join-preview.png'});
  await page.setViewportSize({width:390,height:844});if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+2)){await page.screenshot({path:artifactDir+'/join-mobile-overflow.png'});console.log(await page.evaluate(()=>[...document.querySelectorAll('main *')].filter(e=>{const r=e.getBoundingClientRect();return r.width&&r.height&&r.right>innerWidth+2&&!e.closest('.record-scroll');}).slice(0,12).map(e=>({tag:e.tagName,cls:e.className,width:e.getBoundingClientRect().width,right:e.getBoundingClientRect().right}))));throw Error('Mobile join overflow');}
  await openMore();await page.getByRole('button',{name:'Switch color theme',exact:true}).click();await page.screenshot({path:artifactDir+'/join-mobile.png'});await page.setViewportSize({width:1440,height:1000});
  await page.goto(base+'/collections/'+left+'?tab=downloads');await page.locator('#collection-download-format').selectOption('parquet-typed');await page.getByText('Format details',{exact:true}).click();await page.getByRole('button',{name:'Check format changes',exact:true}).click();
  await page.getByRole('heading',{name:'Format compatibility',exact:true}).waitFor();if(!(await page.locator('.dataset-workspace').textContent()).includes('nested values become JSON text'))throw Error('Nested export warning missing');
  await page.getByRole('button',{name:'Download file',exact:true}).click();const ready=page.locator('#collection-download-status').locator('a[href*="/download"]');await ready.waitFor();
  const download=page.waitForEvent('download');await ready.click();if(await(await download).failure())throw Error('Typed Parquet download failed');
  await page.goto(base+'/collections/'+left+'?tab=downloads');await page.getByText('Format details',{exact:true}).click();await page.getByRole('button',{name:'Check format changes',exact:true}).click();await page.getByRole('heading',{name:'Format compatibility',exact:true}).waitFor();
  const original=await page.request.get(base+'/collections/'+left+'/export?original=1');if((await original.body()).toString()!==JSON.stringify(sources[0].rows))throw Error('Cleanup changed source');
  await page.goto(base+'/collections/'+xlsx);await page.getByLabel('Excel sheet',{exact:true}).selectOption('Second 山田');await page.getByRole('button',{name:'Open sheet',exact:true}).click();
  for(let attempt=0;attempt<30;attempt++){if(await page.locator('.sheet-table .cell-value').getByText('Second sheet',{exact:true}).count())break;await page.waitForTimeout(200);await page.reload()}
  await page.locator('.sheet-table .cell-value').getByText('Second sheet',{exact:true}).waitFor();await page.reload();if(await page.getByLabel('Excel sheet',{exact:true}).inputValue()!=='Second 山田')throw Error('Sheet selection not persisted');
  const native=await page.context().browser().newContext({javaScriptEnabled:false,acceptDownloads:true});
  try{const p=await native.newPage();await p.goto(base+'/collections/'+xlsx);await p.getByLabel('Excel sheet',{exact:true}).selectOption('Sheet1');await p.getByRole('button',{name:'Open sheet',exact:true}).click();await p.goto(base+'/collections/'+left+'/insights?field=name');await p.getByRole('button',{name:'Preview trimming spaces',exact:true}).click();await p.getByRole('heading',{name:'Cleaned data',exact:true}).waitFor();await p.goto(base+'/collections/'+left+'?tab=downloads');await p.locator('#collection-download-format').selectOption('parquet-typed');await p.getByText('Format details',{exact:true}).click();await p.getByRole('button',{name:'Check format changes',exact:true}).click();await p.getByRole('heading',{name:'Format compatibility',exact:true}).waitFor();}finally{await native.close()}
  if(errors.length)throw Error('Browser errors: '+errors.join('; '));
  console.log('Workspace tools passed: suggestions, saved/reused steps, joins, typed Parquet, Excel sheets and native forms.');
 } finally {
  page.off('pageerror',onError);
  await page.goto(base+'/collections');const csrf=await page.locator('#import input[name=csrf]').inputValue();
  for(const id of recipes.filter(Boolean))await page.request.post(base+'/pipelines/'+id+'/delete',{form:{csrf}});
  for(const id of imported)await page.request.post(base+'/collections/'+id+'/delete',{form:{csrf,confirm:'delete'}});
 }
}
