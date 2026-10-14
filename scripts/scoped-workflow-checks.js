async (page, artifactDir) => {
 const base=new URL(page.url()).origin,name='scoped-workflow-'+Date.now()+'.ndjson';let id;
 const errors=[];const onError=error=>errors.push(error.message);page.on('pageerror',onError);
 const input=Array.from({length:240},(_,i)=>'{"id":'+i+',"group":'+(i%80)+',"name":" Person '+i+' ","a.b/x":9007199254740993,"nested":{"city":"東京","flag":true},"empty":"","null":null}').join('\n')+'\n';
 try {
  await page.setViewportSize({width:1440,height:1000});
  await page.goto(base+'/collections');await page.locator('#import>summary').click();
  await page.locator('#import-file').setInputFiles({name,mimeType:'application/x-ndjson',buffer:Buffer.from(input)});
  await page.getByRole('button',{name:'Open selected files',exact:true}).click();
  const row=page.locator('#collection-table tr').filter({hasText:name});await row.filter({hasText:'ready'}).waitFor();
  id=(await row.getByRole('link',{name,exact:true}).getAttribute('href')).split('/')[2];const file=base+'/collections/'+id;
  const query='{"filters":[{"field":"id","op":"gte","value":80}],"sort":[{"field":"id","desc":true}],"page":4,"limit":20}';
  await page.goto(file+'?query='+encodeURIComponent(query));
  const pane=page.locator('#file-workspace-tool'),cleaned=page.locator('#workspace-result');
  await page.locator('[data-workspace-target=clean]').click();
  await pane.locator('[name=field]').selectOption('group');await pane.getByRole('button',{name:'Apply',exact:true}).click();
  await cleaned.locator('[data-cleaned-total="80"]').waitFor();
  if(await cleaned.locator('tbody tr').count()!==20||await cleaned.locator('.sheet-data').getAttribute('data-editable')!=='false')throw Error('Complete cleaned result did not use read-only pagination');
  const captured=await pane.locator('[name=input_query]').inputValue();
  if(JSON.parse(captured).page||JSON.parse(captured).limit||JSON.parse(captured).filters[0].value!==80)throw Error('Cleanup captured a page instead of the whole view');
  await cleaned.getByRole('link',{name:'Page 3',exact:true}).click();await cleaned.getByText('Page 3 of 4',{exact:true}).waitFor();
  await cleaned.locator('[data-query-search]').fill('Person 239');await cleaned.locator('[data-query-search]').press('Enter');
  await cleaned.locator('tbody tr').filter({hasText:'Person 239'}).waitFor();
  if(await cleaned.locator('tbody tr').count()!==1)throw Error('Output search did not apply independently');
  await pane.locator('[name=cleanup]').selectOption('trim');await pane.locator('[name=field]').selectOption('name');await pane.getByRole('button',{name:'Apply',exact:true}).click();
  await pane.locator('[name=steps]').nth(1).waitFor({state:'attached'});await cleaned.locator('[data-cleaned-total="80"]').waitFor();
  if(await pane.locator('[name=input_query]').inputValue()!==captured)throw Error('Output filtering changed subsequent cleanup input');
  await pane.locator('[name=cleanup]').selectOption('rename');await pane.locator('[name=field]').selectOption('a.b/x');await pane.locator('[name=new_name]').fill('renamed.part/x');await pane.getByRole('button',{name:'Apply',exact:true}).click();
  await cleaned.getByRole('link',{name:'Sort renamed.part/x',exact:true}).waitFor();
  const names=await pane.locator('[name=field] option').evaluateAll(options=>options.map(o=>o.value));if(!names.includes('renamed.part/x')||names.includes('a.b/x'))throw Error('Cleanup columns did not follow transformed schema');
  await cleaned.getByRole('link',{name:'Sort renamed.part/x',exact:true}).click();await cleaned.locator('th[aria-sort=ascending]').waitFor();
  await cleaned.getByLabel('Filter and sort',{exact:true}).click();await cleaned.getByText('Advanced query JSON',{exact:true}).click();
  await cleaned.locator('textarea[name=query]').fill('{"filters":[{"field":"id","op":"gte","value":230}],"sort":[{"field":"id"}],"limit":20}');await cleaned.locator('[data-raw-query]').click();
  await cleaned.getByText('10 matching records',{exact:true}).waitFor();
  await page.evaluate(()=>{navigator.clipboard.writeText=async value=>{window.__copied=value;};});
  await cleaned.getByRole('button',{name:'Copy renamed.part/x value',exact:true}).first().click();
  if(await page.evaluate(()=>window.__copied)!=='9007199254740993')throw Error('Large integer copy was rounded');
  await page.locator('[data-workspace-target=export]').click();
  if(await pane.locator('[name=result_id]').inputValue()===''||await pane.locator('[data-download-source]').inputValue()!=='current')throw Error('Download did not default to the displayed dataset');
  const downloading=page.waitForEvent('download');await pane.locator('[data-download-submit]').click();
  const download=await downloading,stream=await download.createReadStream(),chunks=[];for await(const chunk of stream)chunks.push(chunk);const text=Buffer.concat(chunks).toString(),rows=JSON.parse(text);
  if(rows.length!==10||rows[0].id!==230||rows[9].id!==239||!text.includes('9007199254740993')||download.suggestedFilename()!==name.replace('.ndjson','-cleaned-filtered.json'))throw Error('Matching export used the wrong result, scope, order or filename');
  if((await(await page.request.get(file+'/export?original=1')).text())!==input)throw Error('Cleanup altered original bytes');
  await page.locator('[data-workspace-target=sql]').click();const sql=pane.locator('#sql-editor'),draft='SELECT COUNT(*) FROM "c_'+id+'"';await sql.fill(draft);await pane.locator('[name=engine]').selectOption('duckdb');
  await page.reload();await pane.locator('#sql-editor').waitFor();
  if(await pane.locator('#sql-editor').inputValue()!==draft||await pane.locator('[name=engine]').inputValue()!=='duckdb'||await pane.getByRole('heading',{name:'Results',exact:true}).count())throw Error('SQL restoration lost a draft or executed it automatically');
  await page.locator('[data-workspace-target=clean]').click();await pane.locator('[name=steps]').nth(2).waitFor({state:'attached'});await cleaned.locator('.sheet-data').waitFor();
  if(await pane.locator('[name=input_query]').inputValue()!==captured)throw Error('Reload lost the captured cleanup scope');
  await page.locator('#workspace-original>summary').click();await page.locator('#detail-content [data-sheet-query]').waitFor();
  await page.locator('#detail-content [data-query-search]').fill('Person 239');await page.locator('#detail-content [data-query-search]').press('Enter');await page.locator('#detail-content tbody tr').filter({hasText:'Person 239'}).waitFor();
  await pane.locator('[name=scope]').selectOption('matching');await pane.locator('[data-recapture-input]').click();await cleaned.locator('[data-cleaned-total="1"]').waitFor();
  for(const width of [1440,390])for(const theme of ['light','dark']){await page.setViewportSize({width,height:1000});await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error('Scoped workspace overflow '+width);await page.screenshot({path:artifactDir+'/scoped-workflow-'+width+'-'+theme+'.png'});}
  const native=await page.context().browser().newContext({javaScriptEnabled:false});
  try{const p=await native.newPage();await p.goto(file+'?query='+encodeURIComponent(query));await p.getByRole('link',{name:'Clean',exact:true}).click();await p.getByLabel('Column',{exact:true}).selectOption('group');await p.getByRole('button',{name:'Apply',exact:true}).click();const resultID=await p.locator('[data-prepared-result]').getAttribute('data-prepared-result');await p.goto(file+'?result_id='+resultID);if(!await p.locator('.sheet-data').count()){await p.getByRole('link',{name:'Refresh status',exact:true}).click();}await p.getByText('Page 1 of 4',{exact:true}).waitFor();await p.getByRole('link',{name:'Page 2',exact:true}).click();await p.getByText('Page 2 of 4',{exact:true}).waitFor();await p.getByRole('link',{name:'Convert / Download',exact:true}).click();if(await p.locator('[data-collection-download] [name=result_id]').inputValue()!==resultID)throw Error('Native download lost cleaned dataset');}finally{await native.close();}
  if(errors.length)throw Error('Browser errors: '+errors.join('; '));
  console.log('Scoped workflow passed: full ordered cleanup, independent output filters, pagination, rename schema, exact copies, matching export, reload/SQL drafts, recapture, themes/mobile and no JavaScript.');
 }catch(error){await page.screenshot({path:artifactDir+'/scoped-workflow-failure.png'});console.log('Scoped failure: '+page.url()+' '+(await page.locator('#file-workspace-tool').textContent().catch(()=>'' )).slice(0,1400));throw error;}
 finally{page.off('pageerror',onError);if(id){await page.goto(base+'/collections');const csrf=await page.locator('#import input[name=csrf]').inputValue();await page.request.post(base+'/collections/'+id+'/delete',{form:{csrf,confirm:'delete'}});}}
}
