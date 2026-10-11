// Pass this callback to Playwright CLI run-code against an isolated FluxPort instance.
// It creates synthetic files, preserves/restores one, then deletes its own sources.
async (page) => {
  const base = new URL(page.url()).origin;
  const prefix = `browser-check-${Date.now()}`;
  const filename = `${prefix}.json`;
  const csvname = `${prefix}.csv`;
  const badname = `${prefix}-malformed.json`;
  const rows = Array.from({length: 100}, (_, i) => ({id: i + 1, name: `User ${i + 1}`, note: 'Unicode 山田 · synthetic data'}));
  await page.goto(`${base}/collections`);
  await page.locator('#import > summary').click();
  await page.locator('#import-file').setInputFiles([
    {name: filename, mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(rows))},
    {name: csvname, mimeType: 'text/csv', buffer: Buffer.from('id,name\n1,Example\n2,"Multiline\nvalue"\n')},
    {name: badname, mimeType: 'application/json', buffer: Buffer.from('[{"unfinished":')}
  ]);
  await page.getByRole('button', {name: 'Import selected files', exact: true}).click();
  await page.locator('#collection-table tr').filter({hasText: filename}).filter({hasText: 'ready'}).waitFor();
  await page.locator('#collection-table tr').filter({hasText: csvname}).filter({hasText: 'ready'}).waitFor();
  await page.locator('#job-activity .job-row').filter({hasText: badname}).filter({hasText: 'failed'}).first().waitFor();
  await page.getByRole('link', {name: filename, exact: true}).click();
  await page.getByRole('link', {name: 'Next →', exact: true}).click();
  await page.getByText('Source records · page 2', {exact: true}).waitFor();
  if (await page.locator('#detail-content tbody tr').count() !== 20) throw Error('Pagination row count');
  await page.getByRole('link', {name: 'Raw JSON', exact: true}).click();
  await page.locator('#detail-content pre').waitFor();
  await page.getByRole('link', {name: 'Data preview', exact: true}).click();
  const display = `${prefix} renamed`;
  await page.locator('#display-name').fill(display);
  await page.getByRole('button', {name: 'Rename', exact: true}).click();
  await page.getByRole('heading', {name: display, exact: true}).waitFor();
  await page.locator('#export-format').selectOption('csv');
  await page.getByRole('button', {name: 'Prepare download', exact: true}).click();
  await page.locator('#job-activity .job-row').filter({hasText: display}).getByRole('link', {name: 'Download', exact: true}).first().waitFor();
  await page.goto(`${base}/collections`);
  await page.getByRole('button', {name: `Remove ${display}`, exact: true}).click();
  await page.getByRole('link', {name: display, exact: true}).waitFor({state: 'detached'});
  await page.getByRole('link', {name: 'Excluded files', exact: true}).click();
  await page.locator('.job-row').filter({hasText: display}).getByRole('button', {name: 'Restore', exact: true}).click();
  await page.goto(`${base}/collections`);
  await page.getByRole('link', {name: display, exact: true}).click();
  await page.locator('#detail-content tbody tr').first().waitFor();
  for (const width of [1440, 390]) {
    await page.setViewportSize({width, height: width === 390 ? 844 : 1000});
    if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)) throw Error('Page overflow');
    const box = await page.locator('.delete-source .checkbox-setting span').boundingBox();
    if (!box || box.x + box.width > width) throw Error('Confirmation text overflow');
    await page.getByRole('button', {name: 'Switch color theme', exact: true}).click();
  }
  await page.setViewportSize({width: 1440, height: 1000});
  for (const name of [display, csvname, badname]) {
    await page.goto(`${base}/collections`);
    await page.getByRole('link', {name, exact: true}).click();
    await page.locator('input[name=confirm]').check();
    await page.getByRole('button', {name: 'Delete source file', exact: true}).click();
    await page.getByRole('heading', {name: 'Collections', exact: true}).waitFor();
  }
  await page.waitForTimeout(1000);
  const refreshes = [];
  const listener = request => {if (/\/(jobs|collections)(\?|$)/.test(request.url())) refreshes.push(request.url());};
  page.on('request', listener);
  await page.waitForTimeout(3000);
  page.off('request', listener);
  if (refreshes.length) throw Error('Unexpected idle refresh');
}
