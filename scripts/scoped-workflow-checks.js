async (page, artifactDir) => {
 const applyChange=async()=>{
  const response=page.waitForResponse(r=>r.request().method()==='POST'&&new URL(r.url()).pathname.endsWith('/changes'));
  await page.getByRole('button',{name:'Apply change',exact:true}).click();
  const result=await response;if(!result.ok())throw Error('Cleanup request failed: '+result.status());
  await page.waitForFunction(()=>!document.querySelector('#file-workspace-tool[aria-busy]')&&document.querySelector('[data-file-edit-status]')?.textContent==='Saved'&&!document.querySelector('[data-file-job]'));
 };
 const base=new URL(page.url()).origin,name='scoped-file-'+Date.now()+'.ndjson';let id;
 const errors=[],onError=e=>errors.push(e.message);page.on('pageerror',onError);
 const input=Array.from({length:240},(_,i)=>'{"id":'+i+',"group":'+(i%80)+',"name":" Person '+i+' ","a.b/x":9007199254740993,"nested":{"city":"東京","flag":true},"empty":"","null":null}').join('\n')+'\n';
 const saved=()=>page.waitForFunction(()=>document.querySelector('[data-file-edit-status]')?.textContent==='Saved'&&!document.querySelector('[data-file-job]'));
 const apply=async()=>{await applyChange();await saved();};
 try{
  await page.setViewportSize({width:1440,height:1000});await page.goto(base+'/collections');await page.locator('#import>summary').click();
  await page.locator('#import-file').setInputFiles({name,mimeType:'application/x-ndjson',buffer:Buffer.from(input)});await page.getByRole('button',{name:'Open selected files',exact:true}).click();
  const row=page.locator('#collection-table tr').filter({hasText:name});await row.filter({hasText:'ready'}).waitFor();id=(await row.getByRole('link',{name,exact:true}).getAttribute('href')).split('/')[2];
  const file=base+'/collections/'+id,query='{"filters":[{"field":"id","op":"gte","value":80}],"sort":[{"field":"id","desc":true}],"page":4,"limit":20}';
  await page.goto(file+'?query='+encodeURIComponent(query));const pane=page.locator('#file-workspace-tool'),table=page.locator('#detail-content');
  await page.locator('[data-workspace-target=clean]').click();await pane.locator('[name=field]').selectOption('group');await apply();
  const source=await(await page.request.get(file+'/export?original=1')).text(),rows=source.trim().split('\n').map(JSON.parse);
  if(rows.length!==160||rows.filter(r=>r.id<80).length!==80||rows.filter(r=>r.id>=80).length!==80)throw Error('Scoped cleanup lost unmatched rows or processed only one page');
  await table.getByText('80 matching records',{exact:true}).waitFor();
  const captured=await pane.locator('[name=input_query]').inputValue();if(JSON.parse(captured).page||JSON.parse(captured).limit||JSON.parse(captured).filters[0].value!==80)throw Error('Captured pagination or lost scope');
  await table.getByRole('link',{name:'Page 3',exact:true}).click();await table.getByText('Page 3 of 4',{exact:true}).waitFor();
  await table.locator('[data-query-search]').fill('Person 239');await table.locator('[data-query-search]').press('Enter');await table.locator('tbody tr').filter({hasText:'Person 239'}).waitFor();if(await table.locator('tbody tr').count()!==1)throw Error('Search did not apply');
  await pane.locator('[name=cleanup]').selectOption('trim');await pane.locator('[name=field]').selectOption('name');await apply();
  if(await pane.locator('[name=input_query]').inputValue()!==captured)throw Error('Browsing changed captured cleanup input');
  const changed=(await(await page.request.get(file+'/export?original=1')).text()).trim().split('\n').map(JSON.parse);
  if(changed.some(r=>r.id<80&&!r.name.startsWith(' '))||changed.some(r=>r.id>=80&&r.name.startsWith(' ')))throw Error('Cleanup touched wrong scope');
  await pane.locator('[name=cleanup]').selectOption('rename');await pane.locator('[name=field]').selectOption('a.b/x');await pane.locator('[name=new_name]').fill('renamed.part/x');await apply();
  await table.getByRole('link',{name:'Sort renamed.part/x',exact:true}).waitFor();const choices=await pane.locator('[name=field] option').evaluateAll(es=>es.map(e=>e.value));if(!choices.includes('renamed.part/x'))throw Error('Column choices did not follow saved schema');
  await page.evaluate(()=>{navigator.clipboard.writeText=async value=>{window.__copied=value;};});await table.getByRole('button',{name:'Copy renamed.part/x value',exact:true}).first().click();if(await page.evaluate(()=>window.__copied)!=='9007199254740993')throw Error('Large integer copy rounded');
  await page.locator('[data-workspace-target=export]').click();await pane.locator('#collection-download-format').selectOption('json');const waiting=page.waitForEvent('download');await pane.locator('[data-download-submit]').click();const download=await waiting,chunks=[];for await(const b of await download.createReadStream())chunks.push(b);const text=Buffer.concat(chunks).toString(),out=JSON.parse(text);if(out.length!==1||out[0].id!==239||!text.includes('9007199254740993')||!download.suggestedFilename().endsWith('-filtered.json'))throw Error('Filtered download lost saved values, scope or filename');
  await page.locator('[data-workspace-target=sql]').click();const draft='SELECT COUNT(*) FROM "c_'+id+'"';await pane.locator('#sql-editor').fill(draft);await pane.locator('[name=engine]').selectOption('duckdb');await page.reload();await pane.locator('#sql-editor').waitFor();if(await pane.locator('#sql-editor').inputValue()!==draft||await pane.locator('[name=engine]').inputValue()!=='duckdb'||await page.locator('#workspace-sql-result table').count())throw Error('SQL restore lost draft or executed it');
  await page.getByRole('link',{name:'Data',exact:true}).click();await table.locator('tbody tr').first().waitFor();const nested=table.getByRole('button',{name:'Inspect JSON in nested',exact:true}).first();await nested.click();await page.locator('dialog[open] [data-record-values]').fill('{"city":"京都","n":18446744073709551617}');await page.locator('dialog[open] [data-use-record]').click();await saved();const edited=await(await page.request.get(file+'/export?original=1')).text();if(!edited.includes('京都')||!edited.includes('18446744073709551617'))throw Error('Nested cell edit lost exact data');
  for(const width of [1440,390])for(const theme of ['light','dark']){await page.setViewportSize({width,height:1000});await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error('Scoped file overflow '+width);await page.screenshot({path:artifactDir+'/scoped-workflow-'+width+'-'+theme+'.png'});}
  const native=await page.context().browser().newContext({javaScriptEnabled:false});try{const p=await native.newPage();await p.goto(file+'/file-history');await p.locator('.file-history-table').waitFor();await p.getByRole('button',{name:'↶ Undo',exact:true}).click();await p.goto(file);await p.locator('#detail-content tbody tr').first().waitFor();}finally{await native.close();}
  if(errors.length)throw Error(errors.join('; '));console.log('Scoped file workflow passed: all-page cleanup, unmatched rows, exact Undo history, transformed schema, independent search, nested cell editing, matching downloads, SQL drafts and native forms.');
 }catch(e){await page.screenshot({path:artifactDir+'/scoped-workflow-failure.png'});throw e;}
 finally{page.off('pageerror',onError);if(id){await page.goto(base+'/collections');const csrf=await page.locator('#import input[name=csrf]').inputValue();await page.request.post(base+'/collections/'+id+'/delete',{form:{csrf,confirm:'delete'}});}}
}
