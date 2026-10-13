// Generate screenshots from a real, isolated FluxPort instance.
const {chromium}=require('playwright');
const path=require('node:path');
(async()=>{
 const target=new URL(process.argv[2]||'http://127.0.0.1:58904');
 if(!['127.0.0.1','localhost','[::1]'].includes(target.hostname))throw Error('Use an isolated local server');
 const base=target.origin;
 const browser=await chromium.launch({headless:true,...(process.env.FLUXPORT_BROWSER_CHANNEL?{channel:process.env.FLUXPORT_BROWSER_CHANNEL}:{})});
 const dir=path.resolve(__dirname,'../docs/images');
 try{
  const page=await browser.newPage({viewport:{width:1440,height:950}});
  await page.goto(base+'/collections');
  if(await page.locator('html').getAttribute('data-theme')!=='light')await page.getByRole('button',{name:'Switch color theme',exact:true}).click();
  await page.locator('#import>summary').click();
  const a={order_id:1001,customer:' Ada ',total:42.5,status:'Paid',details:{region:'Europe'}};
  const records=[a,a,{order_id:1002,customer:'Grace',total:129,status:'Pending',details:{region:'North America'}},{order_id:1003,customer:' 山田 ',total:25,status:'Paid',details:{region:'Asia'}}];
  await page.locator('#import-file').setInputFiles({name:'sales-example.json',mimeType:'application/json',buffer:Buffer.from(JSON.stringify(records))});
  await page.getByRole('button',{name:'Open selected files',exact:true}).click();
  await page.locator('#collection-table tr').filter({hasText:'sales-example.json'}).filter({hasText:'ready'}).last().waitFor();
  await page.getByRole('link',{name:'sales-example.json',exact:true}).last().click();
  const id=new URL(page.url()).pathname.split('/')[2];
  await page.waitForTimeout(350);
  await page.screenshot({path:path.join(dir,'workspace.png')});
  await page.locator('[data-workspace-target=clean]').click();
  await page.getByLabel('Column',{exact:true}).fill('order_id');await page.getByRole('button',{name:'Add change',exact:true}).click();
  await page.waitForFunction(()=>!document.querySelector('#file-workspace-tool[aria-busy=true]'));
  await page.getByLabel('Action',{exact:true}).selectOption('trim');await page.getByLabel('Column',{exact:true}).fill('customer');await page.getByRole('button',{name:'Add change',exact:true}).click();
  await page.waitForFunction(()=>!document.querySelector('#file-workspace-tool[aria-busy=true]'));
  await page.getByText('Save steps for later',{exact:true}).click();await page.locator('[data-cleanup-form] input[name=name]').fill('Clean sales data');
  await page.setViewportSize({width:1440,height:1150});
  await page.screenshot({path:path.join(dir,'changes.png')});
  await page.locator('.cleanup-download select').selectOption('xlsx');await page.getByRole('button',{name:'Download result',exact:true}).click();
  await page.locator('#collection-download-status').getByRole('link',{name:'Download file',exact:true}).waitFor();
  const saved=await page.locator('[data-download-steps]').inputValue();
  await page.setViewportSize({width:1280,height:900});await page.screenshot({path:path.join(dir,'downloads.png')});
  await page.goto('file:///'+path.join(dir,'social-preview.html').replaceAll('\\','/'));
  await page.setViewportSize({width:1280,height:640});await page.screenshot({path:path.join(dir,'social-preview.png')});
  await page.goto(base+'/pipelines?saved='+saved);await page.getByText('Recipe settings and API',{exact:true}).click();await page.getByRole('button',{name:'Delete recipe',exact:true}).click();
  await page.goto(base+'/collections/'+id);await page.getByText('File settings',{exact:true}).click();await page.locator('input[name=confirm]').check();await page.getByRole('button',{name:'Delete source file',exact:true}).click();
  console.log('Real light-mode Data, Changes, Downloads and social preview screenshots generated.');
 }finally{await browser.close();}
})().catch(e=>{console.error(e.stack);process.exitCode=1});
