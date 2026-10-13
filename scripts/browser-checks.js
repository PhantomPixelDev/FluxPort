// Pass this callback to Playwright CLI run-code against an isolated FluxPort instance.
// It creates synthetic files, preserves/restores one, then deletes its own sources.
async (page, artifactDir='output/playwright') => {
  const settled=()=>page.waitForFunction(()=>!document.querySelector('#file-workspace-tool[aria-busy=true],#detail-content[aria-busy=true]'));
  const base = new URL(page.url()).origin;
  const prefix = `browser-check-${Date.now()}`;
  const filename = `${prefix}.json`;
  const csvname = `${prefix}.csv`;
  const badname = `${prefix}-malformed.json`;
  const rows = Array.from({length: 100}, (_, i) => ({id: i + 1, name: `User ${i + 1}`, note: 'Unicode 山田 · synthetic data',big:'9007199254740993',nested:{precise:'9007199254740993'},empty:'',nullable:null,...(i%2?{}:{sometimes:true})}));
  await page.goto(`${base}/collections`);
  await page.locator('#import > summary').click();
  await page.locator('#import-file').setInputFiles([
    {name: filename, mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(rows).replaceAll('"9007199254740993"','9007199254740993'))},
    {name: csvname, mimeType: 'text/csv', buffer: Buffer.from('id,name\n1, Example \n1, Example \n2,"Multiline\nvalue"\n')},
    {name: badname, mimeType: 'application/json', buffer: Buffer.from('[{"unfinished":')}
  ]);
  await page.getByRole('button', {name: 'Open selected files', exact: true}).click();
  await page.locator('#collection-table tr').filter({hasText: filename}).filter({hasText: 'ready'}).waitFor();
  await page.locator('#collection-table tr').filter({hasText: csvname}).filter({hasText: 'ready'}).waitFor();
  await page.locator('#job-activity .job-row').filter({hasText: badname}).filter({hasText: 'failed'}).first().waitFor();
  await page.getByRole('link', {name: filename, exact: true}).click();
  await page.getByRole('link', {name: 'Page 5', exact: true}).click();
  await page.locator('.detail-body[data-page="5"]').waitFor();
  if (await page.locator('.sheet-pagination [aria-current="page"]').textContent() !== '5') throw Error('Numbered pagination current page');
  await page.getByRole('link', {name: 'Page 2', exact: true}).click();
  await page.locator('.detail-body[data-page="2"]').waitFor();
  if (await page.locator('#detail-content tbody tr').count() !== 20) throw Error('Pagination row count');
  await page.locator('.workspace-more>summary').click();
  await page.getByRole('link', {name: 'Raw JSON', exact: true}).click();
  await page.locator('#workspace-tool-content pre').waitFor();
  await page.getByRole('link', {name: 'Data', exact: true}).click();
  const display = `${prefix} renamed`;
  await page.getByText('File settings', {exact: true}).click();
  await page.locator('#display-name').fill(display);
  await page.getByRole('button', {name: 'Rename', exact: true}).click();
  await page.getByRole('heading', {name: display, exact: true}).waitFor();
  const collectionID = new URL(page.url()).pathname.split('/')[2];
  const compact=await page.locator('.sheet-table tbody tr').first().boundingBox();
  await page.getByLabel('Row density',{exact:true}).selectOption('comfortable');
  const comfortable=await page.locator('.sheet-table tbody tr').first().boundingBox();
  if (comfortable.height<=compact.height) throw Error('Density did not change row height');
  await page.reload();
  if (await page.getByLabel('Row density',{exact:true}).inputValue()!=='comfortable') throw Error('Density was not remembered');
  await page.getByLabel('Row density',{exact:true}).selectOption('compact');
  await page.getByRole('link',{name:'Sort id',exact:true}).click();
  await settled();
  await page.getByRole('link',{name:'Sort id',exact:true}).click();
  await settled();
  if (!await page.locator('th[aria-sort=descending]').count()) throw Error('Header sort cycle failed');
  await page.getByRole('link',{name:'Sort id',exact:true}).click();
  await settled();
  if (await page.locator('th[aria-sort]').count()) throw Error('Header sort clear failed');
  await page.locator('[data-inspect-cell]').first().click();
  if (!(await page.locator('#record-values').inputValue()).includes('9007199254740993')) throw Error('Inspector rounded nested numbers');
  await page.getByRole('button',{name:'Close record',exact:true}).click();
  await page.context().grantPermissions(['clipboard-read','clipboard-write']);
  await page.getByRole('button',{name:'Copy big value',exact:true}).first().click();
  if (await page.evaluate(()=>navigator.clipboard.readText())!=='9007199254740993') throw Error('Clipboard rounded numbers');
  for (const kind of ['empty','null','missing','json']) if (!await page.locator('td[data-display-kind='+kind+']').count()) throw Error('Missing cell state '+kind);
  await page.locator('[data-workspace-target=export]').click();
  if (await page.locator('#collection-download-format').inputValue() !== 'original') throw Error('Original download must be the default');
  // Delay the queued response until processing finishes, and suppress custom
  // progress notifications. A single post-swap catch-up must still reveal ready.
  await page.evaluate(()=>{window.__blockExportProgress=e=>e.stopImmediatePropagation();window.addEventListener('fluxport:progress',window.__blockExportProgress,true);});
  const exportRoute=`**/collections/${collectionID}/export`;
  await page.route(exportRoute,async route=>{const response=await route.fetch();await page.waitForTimeout(500);await route.fulfill({response});});
  for (const format of ['xlsx', 'parquet']) {
    const previous = page.locator('#collection-download-status [data-download-job]');
    const previousJob = await previous.count() ? await previous.getAttribute('data-download-job') : null;
    await page.locator('#collection-download-format').selectOption(format);
    await page.locator('[data-download-note]').filter({visible:true}).waitFor();
    await page.locator('[data-collection-download]').getByRole('button', {name:'Download',exact:true}).click();
    await page.waitForFunction(previous => {
      const job = document.querySelector('#collection-download-status [data-download-job]');
      return job && job.dataset.downloadJob !== previous;
    }, previousJob);
    const ready = page.locator('#collection-download-status').getByRole('link', {name:'Download file',exact:true});
    try { await ready.waitFor(); } catch (error) { throw Error('Export '+format+' did not become visible: '+await page.locator('#collection-download-status').textContent()+'; chooser open='+await page.locator('#download').getAttribute('open')); }
    const downloading = page.waitForEvent('download');
    await ready.click();
    if (await (await downloading).failure()) throw Error('Converted download failed');
    if (await page.locator('#collection-download-status [data-download-job]').count() !== 1) throw Error('Chooser should show one export');
    if(format==='xlsx'){await page.unroute(exportRoute);await page.evaluate(()=>window.removeEventListener('fluxport:progress',window.__blockExportProgress,true));}
  }
  await page.keyboard.press('Escape');
  if (await page.locator('#file-workspace-tool').isVisible()) throw Error('Escape must close the export panel');
  const nativeContext = await page.context().browser().newContext({javaScriptEnabled:false,acceptDownloads:true});
  try {
    const nativePage = await nativeContext.newPage();
    await nativePage.goto(`${base}/collections/${collectionID}`);
    await nativePage.getByRole('link',{name:'Export',exact:true}).click();
    const downloading = nativePage.waitForEvent('download');
    await nativePage.locator('[data-collection-download]').getByRole('button',{name:'Download',exact:true}).click();
    if (await (await downloading).failure()) throw Error('No-JavaScript original download failed');
    await nativePage.locator('#collection-download-format').selectOption('xlsx');
    await nativePage.locator('[data-collection-download]').getByRole('button',{name:'Download',exact:true}).click();
    if (!new URL(nativePage.url()).searchParams.get('download_job')) throw Error('Native export must navigate to its status');
    for (let attempt=0;attempt<20;attempt++) {
      if (await nativePage.locator('#collection-download-status').getByRole('link',{name:'Download file',exact:true}).isVisible()) break;
      await nativePage.waitForTimeout(250);
      await nativePage.getByRole('button',{name:'Refresh download status',exact:true}).click();
    }
    await nativePage.locator('#collection-download-status').getByRole('link',{name:'Download file',exact:true}).waitFor();
    await nativePage.goto(`${base}/collections/${collectionID}/changes`);
    await nativePage.getByLabel('Action',{exact:true}).selectOption('deduplicate');
    await nativePage.getByLabel('Column',{exact:true}).fill('id');
    await nativePage.getByRole('button',{name:'Add change',exact:true}).click();
    await nativePage.getByRole('heading',{name:'Before and after',exact:true}).waitFor();
    await nativePage.getByRole('button',{name:'Undo last change',exact:true}).click();
    if (await nativePage.locator('.cleanup-steps textarea[name=steps]').count()) throw Error('Native cleanup undo failed');
  } finally { await nativeContext.close(); }
  await page.locator('.workspace-more>summary').click();
  await page.getByRole('link', {name:'Validate data',exact:true}).click();
  await page.getByText('Validation rules and scan settings',{exact:true}).click();
  await page.getByLabel('Records to check').fill('10');
  await page.getByLabel('Rules (JSON array)').fill('[{"field":"missing","required":true}]');
  await page.getByRole('button',{name:'Run checks',exact:true}).click();
  await page.getByRole('heading',{name:'Validation issues',exact:true}).waitFor();
  if (!await page.getByLabel('Rules (JSON array)').isVisible()) await page.getByText('Validation rules and scan settings',{exact:true}).click();
  await page.getByLabel('Rules (JSON array)').fill('[{"field":"id","required":true,"type":"integer","unique":true}]');
  await page.getByRole('button',{name:'Save rules and check',exact:true}).click();
  await page.getByText('Validation rules saved.',{exact:true}).waitFor();
  await page.goto(`${base}/pipelines?collection=${collectionID}`);
  await page.getByLabel('Recipe name').fill(prefix);
  await page.locator('[data-new-operation]').selectOption('transform');
  await page.locator('[data-add-step]').click();
  await page.locator('[data-step-json]').fill('{"op":"transform","transform":{"kind":"upper","field":"name"}}');
  await page.locator('[data-new-operation]').selectOption('limit');
  await page.locator('[data-add-step]').click();
  await page.locator('[data-step-undo]').click();
  if (await page.locator('.pipeline-step').count() !== 1) throw Error('Pipeline undo failed');
  await page.locator('[data-step-redo]').click();
  if (await page.locator('.pipeline-step').count() !== 2) throw Error('Pipeline redo failed');
  await page.getByRole('button',{name:'Preview final result',exact:true}).click();
  await page.getByRole('heading',{name:'Preview through step 2',exact:true}).waitFor();
  await page.getByRole('button',{name:'Save recipe',exact:true}).click();
  await page.getByText('Recipe revisions',{exact:true}).waitFor();
  await page.getByText('Recipe settings and API',{exact:true}).click();
  await page.getByRole('button',{name:'Delete recipe',exact:true}).click();
  await page.goto(`${base}/collections/${collectionID}`);
  await page.locator('.dataset-tabs').getByRole('link',{name:'Data',exact:true}).click();
  await page.getByText('Save or download this view',{exact:true}).click();
  await page.locator('form[action$="/workbench/export"] select[name="format"]').selectOption('csv');
  await page.getByRole('button', {name: 'Download this view', exact: true}).click();
  await page.locator('#collection-download-status').getByRole('link', {name:'Download file',exact:true}).waitFor();
  await page.goto(`${base}/collections`);
  await page.getByRole('button', {name: `Remove ${display}`, exact: true}).click();
  await page.getByRole('link', {name: display, exact: true}).waitFor({state: 'detached'});
  await page.getByText('Manage the data folder',{exact:true}).click();
  await page.getByRole('link', {name: 'Excluded files', exact: true}).click();
  await page.locator('.job-row').filter({hasText: display}).getByRole('button', {name: 'Restore', exact: true}).click();
  await page.waitForFunction(async (id) => {
    const response = await fetch(`/api/collections/${id}?limit=1`);
    return response.ok;
  }, collectionID);
  await page.goto(`${base}/collections`);
  await page.getByRole('link', {name: display, exact: true}).click();
  await page.locator('#detail-content tbody tr').first().waitFor();
  for (const width of [1440,390]) {
    await page.setViewportSize({width,height:width===390?844:1000});
    for (let theme=0;theme<2;theme++) {
      await page.locator('[data-workspace-target=export]').click();
      await page.locator('#collection-download-format').selectOption('xlsx');
      const panel = await page.locator('.download-chooser').boundingBox();
      if (!panel || panel.x < 0 || panel.x + panel.width > width + 1) throw Error('Download chooser overflows');
      await page.screenshot({path:`${artifactDir}/download-${width}-${theme}.png`});
      await page.keyboard.press('Escape');
      await page.getByRole('button',{name:'Switch color theme',exact:true}).click();
      await page.waitForTimeout(300);
    }
  }
  await page.getByText('File settings',{exact:true}).click();
  for (const width of [1440, 390]) {
    await page.setViewportSize({width, height: width === 390 ? 844 : 1000});
    if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)) throw Error('Page overflow');
    const box = await page.locator('.delete-source .checkbox-setting span').boundingBox();
    if (!box || box.x + box.width > width) throw Error('Confirmation text overflow');
    await page.getByRole('button', {name: 'Switch color theme', exact: true}).click();
  }
  await page.setViewportSize({width: 1440, height: 1000});
  await page.goto(`${base}/collections`);
  await page.getByRole('link',{name:csvname,exact:true}).click();
  await page.locator('[data-workspace-target=clean]').click();
  await page.locator('[data-cleanup-form] input[name=field]').fill('id');
  await page.getByRole('button',{name:'Add change',exact:true}).click();
  await page.getByRole('heading',{name:'Before and after',exact:true}).waitFor();
  if (await page.locator('.change-comparison>div').last().locator('tbody tr').count()!==2) throw Error('Duplicate rows remained');
  await page.locator('[data-cleanup-action]').selectOption('trim');
  await page.locator('[data-cleanup-form] input[name=field]').fill('name');
  await page.getByRole('button',{name:'Add change',exact:true}).click();
  await settled();
  if ((await page.locator('.change-comparison>div').last().locator('tbody tr').first().locator('td').last().innerText())!=='Example') throw Error('Trim preview failed');
  for (const width of [1440,390]) {
    await page.setViewportSize({width,height:width===390?844:1000});
    if (await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1)) throw Error('Changes page overflow');
    await page.screenshot({path:`${artifactDir}/workspace-changes-${width}.png`,fullPage:true});
  }
  await page.setViewportSize({width:1440,height:1000});
  await page.getByRole('button',{name:'Undo last change',exact:true}).click();
  await settled();
  if (await page.locator('.cleanup-steps textarea[name=steps]').count()!==1) throw Error('Cleanup undo failed');
  await page.locator('[data-cleanup-action]').selectOption('trim');
  await page.locator('[data-cleanup-form] input[name=field]').fill('name');
  await page.getByRole('button',{name:'Add change',exact:true}).click();
  await settled();
  await page.getByText('Save steps for later',{exact:true}).click();
  await page.locator('[data-cleanup-form] input[name=name]').fill(prefix+' cleanup');
  await page.locator('.dataset-tabs').getByRole('link',{name:'Data',exact:true}).click();
  await page.locator('[data-workspace-target=clean]').click();
  await page.getByRole('heading',{name:'Before and after',exact:true}).waitFor();
  await page.getByRole('button',{name:'Save these steps',exact:true}).click();
  await page.waitForFunction(()=>!!document.querySelector('[data-cleanup-form]')?.dataset.saved);
  const savedID=await page.locator('[data-cleanup-form]').getAttribute('data-saved');
  if (!savedID) throw Error('Cleanup steps were not saved');
  await page.locator('.cleanup-download select').selectOption('xlsx');
  await page.getByRole('button',{name:'Download result',exact:true}).click();
  await page.locator('#collection-download-status').getByRole('link',{name:'Download file',exact:true}).waitFor();
  if (await page.locator('#collection-download-format').inputValue()!=='xlsx') throw Error('Downloads must show the prepared format');
  const cleanedDownload=page.waitForEvent('download');
  await page.locator('#collection-download-status').getByRole('link',{name:'Download file',exact:true}).click();
  if (await (await cleanedDownload).failure()) throw Error('Cleaned Excel download failed');
  await page.goto(`${base}/pipelines?saved=${savedID}`);
  await page.getByText('Recipe settings and API',{exact:true}).click();
  await page.getByRole('button',{name:'Delete recipe',exact:true}).click();
  for (const name of [display, csvname, badname]) {
    await page.goto(`${base}/collections`);
    await page.getByRole('link', {name, exact: true}).click();
    await page.getByText('File settings',{exact:true}).click();
    await page.locator('input[name=confirm]').check();
    await page.getByRole('button', {name: 'Delete source file', exact: true}).click();
    await page.getByRole('heading', {name: 'Your datasets', exact: true}).waitFor();
  }
  await page.waitForTimeout(1000);
  const refreshes = [];
  const listener = request => {if (/\/(jobs|collections)(\?|$)/.test(request.url())) refreshes.push(request.url());};
  page.on('request', listener);
  await page.waitForTimeout(3000);
  page.off('request', listener);
  if (refreshes.length) throw Error('Unexpected idle refresh');
}
