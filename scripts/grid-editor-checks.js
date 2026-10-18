async (page,artifactDir,workbook)=>{
 const base=new URL(page.url()).origin,filename='grid-editor-'+Date.now()+'.json';
 let file,copy,binary;
 const data='[{"name":"Ada","a.b/x":"old","big":18446744073709551617,"nullable":null,"empty":""},{"name":"Grace","a.b/x":"next"},{"name":"Linus","a.b/x":"third"}]';
 async function saved(action){
  const response=page.waitForResponse(r=>r.request().method()==='POST'&&(r.url().includes('/workbench/edit')||r.url().includes('/file-history')));
  await action();const r=await response;if(!r.ok())throw Error('Save failed: '+await r.text());
  await page.waitForFunction(()=>document.querySelector('[data-file-edit-status]')?.textContent==='Saved'&&!window.FluxPortEditor?.saving);
 }
 async function preview(action){await page.getByRole('button',{name:'Preview change',exact:true}).click();await page.locator('.editor-preview h3').waitFor();if(action)await saved(action);}
 try{
  await page.goto(base+'/collections');await page.locator('#import>summary').click();await page.locator('#import-file').setInputFiles({name:filename,mimeType:'application/json',buffer:Buffer.from(data)});await page.getByRole('button',{name:'Open selected files',exact:true}).click();
  const row=page.locator('#collection-table tr').filter({hasText:filename});await row.filter({hasText:'ready'}).waitFor();file=base+await row.getByRole('link',{name:filename,exact:true}).getAttribute('href');await page.goto(file);
  const cell=name=>page.locator('tbody td[data-sheet-cell]').filter({has:page.locator('[data-cell-raw]')}); // select through actual column indices below
  const index=async name=>page.locator('[data-sheet-column]').evaluateAll((es,name)=>es.find(e=>e.value===name).dataset.sheetColumn,name);
  const nameIndex=await index('name'),literalIndex=await index('a.b/x');
  const first=page.locator('tbody td[data-sheet-cell="'+literalIndex+'"]').first();
  await first.click({position:{x:25,y:20}});await first.press('ArrowDown');if(await page.locator('[data-grid-active]').getAttribute('data-sheet-cell')!==literalIndex)throw Error('Keyboard navigation failed');
  await page.locator('[data-grid-active]').press('ArrowUp');await page.locator('[data-grid-active]').press('Shift+ArrowDown');if(await page.locator('[data-grid-selected]').count()!==2)throw Error('Range selection failed');
  await page.evaluate(()=>{navigator.clipboard.writeText=async text=>{window.__copied=text;};});
  await page.locator('[data-grid-active]').press('Control+c');if(!await page.evaluate(()=>window.__copied.includes('old')&&window.__copied.includes('next')))throw Error('TSV copy failed');
  await page.getByText('Edit',{exact:true}).first().click();await page.locator('[data-grid-copy=json]').click();if(!await page.evaluate(()=>window.__copied.includes('18446744073709551617')&&JSON.parse(window.__copied)[0].nullable===null))throw Error('JSON copy lost exact values');
  await page.getByText('Edit',{exact:true}).filter({hasNot:page.locator('input')}).first().click();await page.locator('[data-editor-open=paste]').click();
  const form=page.locator('[data-command-preview]');await form.locator('[name=paste_text]').fill('"00123"\n"山田\n太郎"');await preview();
  if(!await page.locator('.editor-preview').textContent().then(t=>t.includes('00123')&&t.includes('山田')))throw Error('Paste preview inaccurate');
  await saved(()=>page.locator('.editor-preview').getByRole('button',{name:'Apply change',exact:true}).click());
  const original=await page.request.get(file+'/export?original=1');const edited=await original.text();if(!edited.includes('18446744073709551617')||!edited.includes('"00123"'))throw Error('Paste corrupted values');
  await saved(()=>page.locator('[data-file-undo]').click());if(await (await page.request.get(file+'/export?original=1')).text()!==data)throw Error('Paste Undo not exact');
  await page.goto(file+'?edit=rename_column');const rename=page.locator('[data-command-preview]');await rename.locator('[name=field]').selectOption('a.b/x');await rename.locator('[name=column_name]').fill('renamed/key.dot');await preview(()=>page.locator('.editor-preview').getByRole('button',{name:'Apply change',exact:true}).click());if(!await page.locator('thead').textContent().then(t=>t.includes('renamed/key.dot')))throw Error('Literal rename failed');
  await page.goto(file+'?edit=add_row');await page.locator('details.editor-advanced-json[data-editor-for~=add_row]>summary').click();await page.locator('[name=command_value]').fill('{"name":"New row","big":18446744073709551618}');await preview(()=>page.locator('.editor-preview').getByRole('button',{name:'Apply change',exact:true}).click());if(!await page.locator('tbody').textContent().then(t=>t.includes('New row')))throw Error('Add row failed');
  await page.goto(file+'?edit=find_replace');const find=page.locator('[data-command-preview]');await find.locator('[name=field]').selectOption('name');await find.locator('[name=find]').fill('New');await find.locator('[name=replacement]').fill('Better');await preview(()=>page.locator('.editor-preview').getByRole('button',{name:'Apply change',exact:true}).click());if(!await page.locator('tbody').textContent().then(t=>t.includes('Better row')))throw Error('Find/replace failed');
  const beforeBatch=await (await page.request.get(file+'/export?original=1')).text();
  for(const action of ['fill_down','delete_rows']){
   await page.goto(file+'?edit='+action);const edit=page.locator('[data-command-preview]');const ids=await edit.locator('[name=record_id] option').evaluateAll(es=>es.slice(0,2).map(e=>e.value));await edit.locator('[name=record_id]').selectOption(ids);
   if(action==='fill_down')await edit.locator('[name=field]').selectOption('name');
   await preview();if(action==='delete_rows')page.once('dialog',dialog=>dialog.accept());
   await saved(()=>page.locator('.editor-preview').getByRole('button',{name:action==='delete_rows'?'Delete and save':'Apply change',exact:true}).click());
   await saved(()=>page.locator('[data-file-undo]').click());if(await (await page.request.get(file+'/export?original=1')).text()!==beforeBatch)throw Error(action+' did not undo atomically');
  }
  const nativeContext=await page.context().browser().newContext({javaScriptEnabled:false,viewport:{width:1024,height:900}});
  try{
   const native=await nativeContext.newPage();await native.goto(file+'?edit=add_column');await native.locator('[name=column_name]').fill('Native column');await native.locator('details.editor-advanced-json[data-editor-for~=add_column]>summary').click();await native.locator('[name=command_value]').fill('"plain text"');
   await native.getByRole('button',{name:'Preview change',exact:true}).click();await native.locator('.editor-preview input[type=checkbox]').check();await native.locator('.editor-preview').getByRole('button',{name:'Apply change',exact:true}).click();
   await native.locator('[data-file-undo]').click();const afterNative=await (await native.request.get(file+'/export?original=1')).text();if(afterNative!==beforeBatch){console.log('Native state',native.url(),(await native.locator('main').textContent()).slice(0,800),{before:beforeBatch.length,after:afterNative.length,hasNative:afterNative.includes('Native column')});throw Error('Native form Undo failed');}
  }finally{await nativeContext.close();}
  await page.goto(file+'?tab=downloads');await page.locator('[data-collection-download] [name=format]').selectOption('csv');await page.reload();if(await page.locator('[data-collection-download] [name=format]').inputValue()!=='csv')throw Error('Export preference not restored');
  for(const width of [320,390,768,1024,1440,2560])for(const theme of ['light','dark']){
   await page.setViewportSize({width,height:900});await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);await page.goto(file+'?edit=paste');
   const issues=await page.locator('[data-editor-panel]').evaluate(panel=>{const errors=[];if(document.documentElement.scrollWidth>innerWidth+1)errors.push('page overflow');for(const input of panel.querySelectorAll('input:not([type=hidden]):not([type=checkbox]),textarea,select')){if(!input.getClientRects().length)continue;const r=input.getBoundingClientRect();if(r.left<0||r.right>innerWidth+1)errors.push('field overflow');}return errors;});if(issues.length)throw Error(issues.join(','));
  }
  await page.setViewportSize({width:1440,height:1000});await page.evaluate(()=>document.documentElement.dataset.theme='light');await page.screenshot({path:artifactDir+'/grid-editor-light.png'});
  await page.goto(base+'/collections');await page.locator('[data-recent-files]').getByRole('link',{name:'Reopen '+filename,exact:true}).waitFor();
  if(workbook){
   const name='editable-copy-'+Date.now()+'.xlsx';await page.locator('#import>summary').click();await page.locator('#import-file').setInputFiles({name,mimeType:'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',buffer:workbook});await page.getByRole('button',{name:'Open selected files',exact:true}).click();
   const source=page.locator('#collection-table tr').filter({hasText:name});await source.filter({hasText:'ready'}).waitFor();binary=base+await source.getByRole('link',{name,exact:true}).getAttribute('href');await page.goto(binary);
   await page.locator('.editor-copy-panel>summary').click();await page.getByRole('button',{name:'Create editable copy',exact:true}).click();await page.waitForURL(url=>url.pathname.startsWith('/collections/')&&url.origin+url.pathname!==binary);copy=page.url().split('?')[0];
   await page.locator('.sheet-data[data-editable=true]').waitFor();if(!await page.locator('h1').textContent().then(t=>t.includes('editable.json')))throw Error('Unreadable copy name');
   const original=await page.request.get(binary+'/export?original=1');if(!Buffer.from(await original.body()).equals(workbook))throw Error('Editable copy changed Excel source');
  }
  console.log('Grid editor passed: selection, keyboard, multiline paste, exact Undo, structural edits, find/replace, recent files, six widths and themes.');
 }finally{for(const target of [copy,binary,file].filter(Boolean)){await page.goto(base+'/collections');const csrf=await page.locator('meta[name=csrf-token]').getAttribute('content');await page.request.post(target+'/delete',{form:{csrf,confirm:'delete'}});}}
}
