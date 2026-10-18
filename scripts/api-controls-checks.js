async (page,artifactDir)=>{
 const base=new URL(page.url()).origin;
 await page.goto(base+'/settings');
 const form=page.locator('form[action="/settings/api"]');
 try{
  for(const width of [320,390,768,1024,1440,2560])for(const theme of ['light','dark']){
   await page.setViewportSize({width,height:900});await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);
   await form.locator('details').evaluateAll(nodes=>nodes.forEach(n=>n.open=true));
   const issues=await form.evaluate(el=>{
    const errors=[];if(document.documentElement.scrollWidth>innerWidth+1)errors.push('page overflow');
    for(const input of el.querySelectorAll('input:not([type=hidden]):not([type=checkbox]),textarea')){
     const r=input.getBoundingClientRect(),p=input.closest('label').getBoundingClientRect();
     if(r.left<p.left-1||r.right>p.right+1)errors.push('field escaped label');
     if(r.height<39)errors.push('short control');
    }return errors;
   });if(issues.length)throw Error(issues.join(','));
  }
  await page.setViewportSize({width:1440,height:1000});
  await form.locator('[name=rate_enabled]').check();await form.locator('[name=read_limit]').fill('2');await form.locator('[name=write_limit]').fill('1');
  await form.locator('[name=window_seconds]').fill('60');await form.locator('[name=default_page_size]').fill('1');await form.locator('[name=max_page_size]').fill('10');await form.locator('[name=origins]').fill('https://example.com');
  await form.getByRole('button',{name:'Save API controls',exact:true}).click();await page.waitForURL('**/settings?api_saved=1#api-settings');
  if(!await page.getByText('API settings saved.',{exact:true}).isVisible())throw Error('Missing save confirmation');
  for(let i=0;i<2;i++){const r=await page.request.get(base+'/api/collections',{headers:{Origin:'https://example.com'}});if(r.status()!==200||r.headers()['access-control-allow-origin']!=='https://example.com')throw Error('API policy not applied');}
  const limited=await page.request.get(base+'/api/collections');if(limited.status()!==429||!limited.headers()['retry-after'])throw Error('Missing rate limit');
  await page.screenshot({path:artifactDir+'/api-controls-dark.png'});
 }finally{
  await page.goto(base+'/settings');const reset=page.locator('form[action="/settings/api"]');await reset.locator('details').evaluateAll(nodes=>nodes.forEach(n=>n.open=true));await reset.locator('[name=rate_enabled]').uncheck();await reset.locator('[name=read_limit]').fill('120');await reset.locator('[name=write_limit]').fill('30');await reset.locator('[name=default_page_size]').fill('0');await reset.locator('[name=max_page_size]').fill('100');await reset.locator('[name=origins]').fill('');await reset.getByRole('button',{name:'Save API controls',exact:true}).click();await page.waitForURL('**/settings?api_saved=1#api-settings');
 }
 let file;
 try{
  const name='api-controls-'+Date.now()+'.json';
  await page.goto(base+'/collections');await page.locator('#import>summary').click();
  await page.locator('#import-file').setInputFiles({name,mimeType:'application/json',buffer:Buffer.from('[{"name":"API fixture"}]')});
  await page.getByRole('button',{name:'Open selected files',exact:true}).click();const row=page.locator('#collection-table tr').filter({hasText:name});await row.filter({hasText:'Done'}).waitFor({timeout:120000});
  file=base+await row.getByRole('link',{name,exact:true}).getAttribute('href');
  await page.goto(file+'?tab=configuration');const collection=page.locator('.api-policy form');
  await collection.locator('[name=rate_enabled]').check();await collection.locator('[name=read_limit]').fill('1');
  await collection.getByRole('button',{name:'Save request controls',exact:true}).click();await page.getByText('API controls saved.',{exact:true}).waitFor();
  const path=base+'/api'+new URL(file).pathname;
  if((await page.request.get(path)).status()!==200||(await page.request.get(path)).status()!==429)throw Error('Collection limit failed');
  if((await page.request.get(base+'/api/collections')).status()!==200)throw Error('Collection limit leaked globally');
  await page.reload();if(!await page.locator('.api-policy [name=rate_enabled]').isChecked())throw Error('Collection settings not restored');
 }finally{if(file){await page.goto(base+'/collections');const csrf=await page.locator('meta[name=csrf-token]').getAttribute('content');await page.request.post(file+'/delete',{form:{csrf,confirm:'delete'}});}}
 console.log('API controls passed: layouts, native save, CORS, rate limits, reset.');
}
