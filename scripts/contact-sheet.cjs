// Generate a light/dark contact sheet of every main page for visual review.
// Usage: node scripts/contact-sheet.cjs http://127.0.0.1:58904 [outDir]
const {chromium}=require('playwright');
const path=require('node:path');
const fs=require('fs');
(async()=>{
 const target=new URL(process.argv[2]||'http://127.0.0.1:58904');
 if(!['127.0.0.1','localhost','[::1]'].includes(target.hostname))throw Error('Use an isolated local server');
 const base=target.origin;
 const out=path.resolve(process.argv[3]||path.join(__dirname,'../output/contact-sheet'));
 fs.mkdirSync(out,{recursive:true});
 const browser=await chromium.launch({headless:true,...(process.env.FLUXPORT_BROWSER_CHANNEL?{channel:process.env.FLUXPORT_BROWSER_CHANNEL}:{})});
 try{
  const page=await browser.newPage({viewport:{width:1440,height:950}});
  await page.goto(base+'/collections');
  await page.locator('#import>summary').click();
  const records=[{order_id:1001,customer:' Ada ',total:42.5,status:'Paid',details:{region:'Europe'}},{order_id:1002,customer:'Grace',total:129,status:'Pending',details:{region:'North America'}},{order_id:1003,customer:'Kai',total:25,status:'Paid',details:{region:'Asia'}}];
  await page.locator('#import-file').setInputFiles({name:'contact-sheet-sample.json',mimeType:'application/json',buffer:Buffer.from(JSON.stringify(records))});
  await page.getByRole('button',{name:'Open selected files',exact:true}).click();
  const row=page.locator('#collection-table tr').filter({hasText:'contact-sheet-sample.json'}).last();
  await row.filter({hasText:'Done'}).waitFor({timeout:60000});
  const file=base+await row.getByRole('link',{name:'contact-sheet-sample.json',exact:true}).getAttribute('href');
  const pages=[['files','/collections'],['file-data',file],['clean',file+'/changes'],['sql',file+'/sql'],['summary',file+'/insights'],['checks',file+'/quality'],['downloads',file+'?tab=downloads'],['converter','/converter'],['sql-lab','/sql'],['saved-steps','/pipelines'],['api','/api'],['overview','/overview']];
  for(const theme of ['light','dark']){
   for(const [name,url] of pages){
    await page.goto(url.startsWith('http')?url:base+url);await page.waitForLoadState('networkidle').catch(()=>{});
    await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);
    await page.waitForTimeout(400);
    await page.screenshot({path:path.join(out,`${name}-${theme}.png`),fullPage:true});
   }
  }
  console.log('Contact sheet written to '+out);
 }finally{await browser.close();}
})().catch(e=>{console.error('Contact sheet failed:',e.message);process.exit(1);});