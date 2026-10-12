// Run with Playwright installed (NODE_PATH can point at an external installation).
// This runner avoids Windows' command-line length limit for the shared callback.
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const {chromium} = require('playwright');

(async () => {
  const url = new URL(process.argv[2]);
  if (!['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)) throw Error('Use an isolated local FluxPort server. These checks create and delete test data.');
  const browser = await chromium.launch({headless:true, ...(process.env.FLUXPORT_BROWSER_CHANNEL ? {channel:process.env.FLUXPORT_BROWSER_CHANNEL} : {})});
  try {
    const page = await browser.newPage({viewport:{width:1440,height:1000}});
    await page.goto(url.href);
    const callback = vm.runInNewContext('(' + fs.readFileSync(path.join(__dirname,'browser-checks.js'),'utf8') + ')', {Buffer, URL, Date, Error});
    const artifactDir=path.resolve(__dirname,'../output/playwright');
    fs.mkdirSync(artifactDir,{recursive:true});
    await callback(page,artifactDir);
    console.log('Browser workflow checks passed: imports, partial failures, pagination, quality, pipelines, undo/redo, export, exclusions, restore, themes and mobile.');
  } finally { await browser.close(); }
})().catch(error => { console.error(error.stack); process.exitCode=1; });
