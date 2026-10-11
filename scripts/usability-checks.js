async (page) => {
  const base = new URL(page.url()).origin;
  const failures = [];
  page.on('pageerror', error => failures.push(error.message));
  const rows = Array.from({length: 130}, (_, i) => ({id:i+1,name:'Record '+i,group:i%2?'other':'match',nested:{address:{city:'Berlin'},tags:['one','two']},...Object.fromEntries(Array.from({length:12},(_,j)=>['field_'+j,i+j]))}));
  await page.goto(base+'/collections');
  await page.locator('#import > summary').click();
  await page.locator('#import-file').setInputFiles({name:'usability-check.json',mimeType:'application/json',buffer:Buffer.from(JSON.stringify(rows))});
  await page.getByRole('button',{name:'Import selected files',exact:true}).click();
  await page.waitForFunction(async()=>{const r=await fetch('/api/collections?q=usability-check');const j=await r.json();return j.collections.some(c=>c.status==='ready');});
  const col = await page.evaluate(async()=>{const r=await fetch('/api/collections?q=usability-check');return (await r.json()).collections[0];});
  await page.goto(base+'/collections/'+col.id);
  await page.locator('#record-search').fill('match');
  await page.locator('#page-size').selectOption('50');
  await page.getByRole('button',{name:'Apply',exact:true}).click();
  await page.waitForFunction(()=>document.querySelector('.section-caption')?.textContent.includes('65 records'));
  if(await page.locator('.preview-table tbody tr').count()!==50)throw Error('Wrong page size');
  await page.locator('#jump-page').fill('2');
  await page.getByRole('button',{name:'Go',exact:true}).click();
  await page.waitForFunction(()=>document.querySelector('[data-page]')?.dataset.page==='2');
  if(await page.locator('.preview-table tbody tr').count()!==15)throw Error('Wrong search pagination');
  await page.locator('.column-controls > summary').click();
  const field = page.locator('[data-column-index]').nth(2);
  await field.uncheck();
  await page.reload();
  if(await page.locator('[data-column-index]').nth(2).isChecked())throw Error('Visible columns not remembered');
  await page.goto(base+'/converter?collection='+col.id);
  if(await page.locator('#manual-input').isVisible())throw Error('Collection mode still shows paste editor');
  await page.locator('#target-format').selectOption('yaml');
  await page.locator('.conversion-options > summary').click();
  await page.locator('[name=roundtrip]').check();
  await page.getByRole('button',{name:'Convert data →',exact:true}).click();
  await page.waitForFunction(()=>document.querySelector('#convert-output')?.dataset.ready==='true');
  if(!(await page.locator('#convert-output').inputValue()).includes('city: Berlin'))throw Error('Nested collection conversion lost data');
  if(!(await page.locator('.conversion-report').innerText()).includes('values and types match'))throw Error('Collection round trip failed');
  for(const width of [1440,390])for(const theme of ['dark','light']) {
    await page.setViewportSize({width,height:1000});
    for(const path of ['/', '/workflow?collection='+col.id, '/converter?collection='+col.id, '/collections/'+col.id]) {
      await page.goto(base+path);
      await page.evaluate(value=>document.documentElement.dataset.theme=value,theme);
      const overflow=await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1);
      if(overflow)throw Error('Horizontal page overflow: '+width+' '+theme+' '+path);
    }
  }
  await page.setViewportSize({width:1440,height:1000});
  await page.goto(base+'/converter?collection='+col.id);
  await page.screenshot({path:'output/playwright/usability-converter.png',fullPage:true});
  await page.setViewportSize({width:390,height:1000});
  await page.goto(base+'/workflow?collection='+col.id);
  await page.screenshot({path:'output/playwright/usability-mobile.png',fullPage:true});
  if(failures.length)throw Error(failures.join('; '));
  return {searchMatches:65,pageSizes:[50,15],columnPreferences:'persisted',nestedConversion:'round-trip match',layouts:'desktop/mobile, dark/light',pageErrors:failures.length};
}
