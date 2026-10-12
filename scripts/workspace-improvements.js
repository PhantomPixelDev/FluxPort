async (page,artifactDir,workbook) => {
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
  await page.goto(base+'/collections/'+left);await page.getByRole('link',{name:'Summary of name',exact:true}).click();
  await page.getByRole('heading',{name:'Understand your data',exact:true}).waitFor();
  if(!await page.getByText('1 duplicate rows found in checked data',{exact:true}).isVisible())throw Error('Missing duplicate suggestion');
  await page.getByRole('button',{name:'Preview trimming spaces',exact:true}).click();
  await page.getByRole('heading',{name:'Before and after',exact:true}).waitFor();
  await page.getByLabel('Save these steps as',{exact:true}).fill(prefix+' recipe');await page.getByRole('button',{name:'Save these steps',exact:true}).click();
  recipes.push(new URL(page.url()).searchParams.get('saved'));
  await page.goto(base+'/collections/'+next+'/changes');await page.locator('.reuse-steps>summary').click();await page.getByRole('link',{name:prefix+' recipe',exact:true}).click();
  await page.getByRole('button',{name:'Preview result',exact:true}).click();
  if(!(await page.locator('.change-comparison').textContent()).includes('Alan'))throw Error('Reused cleanup result missing');
  await page.goto(base+'/collections/'+left+'/changes?new=1');await page.locator('.join-builder>summary').click();
  await page.getByLabel('Match this column',{exact:true}).fill('key.x');await page.getByLabel('Other dataset',{exact:true}).selectOption(right);await page.getByLabel('Match their column',{exact:true}).selectOption('key/y');
  await page.getByRole('button',{name:'Preview join',exact:true}).click();
  await page.getByText('Sample of 3 input rows: 2 matched, 1 unmatched; 2 have multiple matches. These are sample counts, not dataset totals.',{exact:true}).waitFor();
  await page.screenshot({path:artifactDir+'/join-preview.png'});
  await page.setViewportSize({width:390,height:844});if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+2))throw Error('Mobile join overflow');
  await page.getByRole('button',{name:'Switch color theme',exact:true}).click();await page.screenshot({path:artifactDir+'/join-mobile.png'});await page.setViewportSize({width:1440,height:1000});
  await page.goto(base+'/collections/'+left+'?tab=downloads');await page.locator('#collection-download-format').selectOption('parquet-typed');await page.getByRole('button',{name:'Check format changes',exact:true}).click();
  await page.getByRole('heading',{name:'Before you download',exact:true}).waitFor();if(!(await page.locator('.dataset-workspace').textContent()).includes('nested values become JSON text'))throw Error('Nested export warning missing');
  await page.getByRole('button',{name:'Prepare download',exact:true}).click();const ready=page.locator('#collection-download-status').getByRole('link',{name:'Download file',exact:true});await ready.waitFor();
  const download=page.waitForEvent('download');await ready.click();if(await(await download).failure())throw Error('Typed Parquet download failed');
  await page.goto(base+'/collections/'+left+'?tab=downloads');await page.getByRole('button',{name:'Check format changes',exact:true}).click();await page.getByRole('heading',{name:'Before you download',exact:true}).waitFor();
  const original=await page.request.get(base+'/collections/'+left+'/export?original=1');if((await original.body()).toString()!==JSON.stringify(sources[0].rows))throw Error('Cleanup changed source');
  await page.goto(base+'/collections/'+xlsx);await page.getByLabel('Excel sheet',{exact:true}).selectOption('Second 山田');await page.getByRole('button',{name:'Open sheet',exact:true}).click();
  for(let attempt=0;attempt<30;attempt++){if(await page.locator('.sheet-table .cell-value').getByText('Second sheet',{exact:true}).count())break;await page.waitForTimeout(200);await page.reload()}
  await page.locator('.sheet-table .cell-value').getByText('Second sheet',{exact:true}).waitFor();await page.reload();if(await page.getByLabel('Excel sheet',{exact:true}).inputValue()!=='Second 山田')throw Error('Sheet selection not persisted');
  const native=await page.context().browser().newContext({javaScriptEnabled:false,acceptDownloads:true});
  try{const p=await native.newPage();await p.goto(base+'/collections/'+xlsx);await p.getByLabel('Excel sheet',{exact:true}).selectOption('Sheet1');await p.getByRole('button',{name:'Open sheet',exact:true}).click();await p.goto(base+'/collections/'+left+'/insights?field=name');await p.getByRole('button',{name:'Preview trimming spaces',exact:true}).click();await p.getByRole('heading',{name:'Before and after',exact:true}).waitFor();await p.goto(base+'/collections/'+left+'?tab=downloads');await p.locator('#collection-download-format').selectOption('parquet-typed');await p.getByRole('button',{name:'Check format changes',exact:true}).click();await p.getByRole('heading',{name:'Before you download',exact:true}).waitFor();}finally{await native.close()}
 } finally {
  await page.goto(base+'/collections');const csrf=await page.locator('#import input[name=csrf]').inputValue();
  for(const id of recipes.filter(Boolean))await page.request.post(base+'/pipelines/'+id+'/delete',{form:{csrf}});
  for(const id of imported)await page.request.post(base+'/collections/'+id+'/delete',{form:{csrf,confirm:'delete'}});
 }
}
