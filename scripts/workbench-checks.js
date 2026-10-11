async (page) => {
  const base = new URL(page.url()).origin, errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/collections');
  await page.locator('#import > summary').click();
  const source='[{"id":1,"name":"Ada","active":true,"account":{"plan":"starter"},"large":9007199254740993},{"id":2,"name":"Grace","active":false,"account":{"plan":"pro"},"large":2}]';
  await page.locator('#import-file').setInputFiles({name:'workbench-browser.json',mimeType:'application/json',buffer:Buffer.from(source)});
  await page.getByRole('button',{name:'Import selected files',exact:true}).click();
  await page.waitForFunction(async()=>{const r=await fetch('/api/collections?q=workbench-browser');return (await r.json()).collections.some(c=>c.status==='ready');});
  const col=await page.evaluate(async()=>{const r=await fetch('/api/collections?q=workbench-browser');return (await r.json()).collections[0];});
  await page.goto(base+'/collections/'+col.id+'?tab=configuration');
  await page.locator('[name=crud]').check();
  await page.getByRole('button',{name:'Save configuration',exact:true}).click();
  await page.waitForFunction(()=>document.querySelector('#config-result')?.textContent.includes('saved'));
  await page.goto(base+'/collections/'+col.id+'/workbench');
  const cell=page.locator('.sheet-table tbody tr').first().locator('td[aria-label="Edit account"]');
  await cell.dblclick();await cell.locator('input').fill('{"plan":"enterprise"}');await cell.locator('input').press('Enter');
  await page.waitForFunction(()=>document.querySelector('.sheet-table tbody')?.textContent.includes('enterprise'));
  const result=await page.evaluate(async id=>{const r=await fetch('/api/collections/'+id+'/query',{method:'POST',headers:{'Content-Type':'application/json'},body:'{}'});return await r.text();},col.id);
  if(!result.includes('9007199254740993'))throw Error('Inline edit changed a large integer');
  await page.getByText('Filters and multi-column sorting',{exact:true}).click();
  await page.getByRole('button',{name:'Add filter',exact:true}).click();
  await page.locator('[data-filter-field]').fill('active');await page.locator('[data-filter-value]').fill('true');
  await page.getByRole('button',{name:'Add sort column',exact:true}).click();
  await page.locator('[data-sort-field]').fill('name');await page.locator('[data-sort-direction]').selectOption('desc');
  await page.getByRole('button',{name:'Apply view',exact:true}).click();await page.waitForLoadState();
  if(await page.locator('.sheet-table tbody tr').count()!==1)throw Error('Typed filter failed');
  await page.getByPlaceholder('For example: Active customers').fill('Browser active');
  await page.getByRole('button',{name:'Save current view',exact:true}).click();await page.waitForLoadState();
  if(!page.url().includes('view='))throw Error('Saved view did not reopen');
  await page.goto(base+'/sql');
  for(const box of await page.locator('[name=collections]:checked').all())await box.uncheck();
  await page.locator('[name=collections][value="'+col.id+'"]').check();
  await page.locator('#sql-editor').fill('SELECT name,json_extract(account,\'$.plan\') AS plan FROM "c_'+col.id+'" ORDER BY name');
  await page.getByRole('button',{name:'Run query',exact:true}).click();await page.waitForLoadState();
  if(!(await page.locator('main').innerText()).includes('enterprise'))throw Error('SQL values are incorrect');
  const screenshots=[];
  for(const theme of ['light','dark'])for(const width of [1440,390]){
    await page.setViewportSize({width,height:960});await page.evaluate(t=>{document.documentElement.dataset.theme=t;localStorage.setItem('fluxport-theme',t);},theme);
    if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('SQL page overflow at '+width);
    const path='output/playwright/sql-'+theme+'-'+width+'.png';await page.screenshot({path,fullPage:true});screenshots.push(path);
  }
  if(errors.length)throw Error(errors.join('; '));
  return {collection:col.id,inlineEditing:true,largeIntegerPreserved:true,typedFilters:true,savedViews:true,sql:true,screenshots,errors};
}
