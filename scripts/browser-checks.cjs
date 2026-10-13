// Run with Playwright installed (NODE_PATH can point at an external installation).
// This runner avoids Windows' command-line length limit for the shared callback.
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const cp = require('node:child_process');
const {chromium} = require('playwright');

(async () => {
  const url = new URL(process.argv[2]);
  if (!['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)) throw Error('Use an isolated local FluxPort server. These checks create and delete test data.');
  const browser = await chromium.launch({headless:true, ...(process.env.FLUXPORT_BROWSER_CHANNEL ? {channel:process.env.FLUXPORT_BROWSER_CHANNEL} : {})});
  try {
    const page = await browser.newPage({viewport:{width:1440,height:1000}});
    if(process.env.WORKSPACE_DEV_ASSETS==='1')await page.route('**/static/**',async route=>{const file=path.resolve(__dirname,'../ui'+new URL(route.request().url()).pathname);if(fs.existsSync(file))await route.fulfill({path:file});else await route.continue();});
    await page.goto(url.href);
    const callback = vm.runInNewContext('(' + fs.readFileSync(path.join(__dirname,'browser-checks.js'),'utf8') + ')', {Buffer, URL, Date, Error});
    const artifactDir=path.resolve(__dirname,'../output/playwright');
    fs.mkdirSync(artifactDir,{recursive:true});
    try { await callback(page,artifactDir); } catch(error) { console.error('Workspace status: '+(await page.locator('#file-workspace-tool').textContent().catch(()=>'(no workspace)')).slice(-3000));await page.screenshot({path:path.join(artifactDir,'regression-failure.png')});throw error; }
    cp.execFileSync('go',['run','scripts/browser-fixtures.go',artifactDir],{cwd:path.resolve(__dirname,'..'),timeout:60000});
    const improvements=vm.runInNewContext('('+fs.readFileSync(path.join(__dirname,'workspace-improvements.js'),'utf8')+')',{Buffer,URL,Date,Error,console});
    await improvements(page,artifactDir,fs.readFileSync(path.join(artifactDir,'multi-sheet.xlsx')));
    const workspace=vm.runInNewContext('('+fs.readFileSync(path.join(__dirname,'file-workspace-checks.js'),'utf8')+')',{Buffer,URL,Date,Error,console});
    await workspace(page,artifactDir);
    console.log('Browser workflow checks passed: imports, partial failures, pagination, quality, pipelines, undo/redo, export, exclusions, restore, themes and mobile.');
  } finally { await browser.close(); }
})().catch(error => { console.error(error.stack); process.exitCode=1; });
