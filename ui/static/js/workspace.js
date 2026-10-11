/* Controls operate on the visible page, never an entire dataset. */
(() => {
  const byId = id => document.getElementById(id);
  function syncConverter() {
    const mode = byId('input-mode');
    if (!mode) return;
    const collection = mode.value === 'collection';
    byId('collection-input').hidden = !collection;
    byId('manual-input').hidden = collection;
    document.querySelector('.converter-grid').classList.toggle('collection-mode', collection);
    for (const id of ['convert-input', 'converter-file']) byId(id).disabled = collection;
    const picker = byId('converter-collection');
    if (picker) picker.disabled = !collection;
    const from = byId('source-format');
    from.disabled = collection;
    if (collection) {
      const option = picker?.selectedOptions[0];
      if (option?.dataset.format) from.value = option.dataset.format;
      byId('collection-input-status').textContent = option?.value
        ? option.dataset.rows + ' records · ' + option.dataset.status + '. Converts the complete source; large results run in File activity.'
        : 'Choose a collection. Your original source stays unchanged.';
    }
  }
  function applyColumns(save = false) {
    const detail = document.querySelector('[data-collection]');
    if (!detail) return;
    const boxes = [...detail.querySelectorAll('[data-column-index]')];
    for (const box of boxes) for (const cell of detail.querySelectorAll('[data-column="' + box.dataset.columnIndex + '"]')) cell.hidden = !box.checked;
    if (save && boxes.length) {
      try { localStorage.setItem('fluxport-columns-' + detail.dataset.collection, JSON.stringify(boxes.filter(b => b.checked).map(b => b.dataset.columnName))); } catch (_) {}
    }
  }
  function restoreColumns() {
    const detail = document.querySelector('[data-collection]');
    if (!detail) return;
    try {
      const saved = JSON.parse(localStorage.getItem('fluxport-columns-' + detail.dataset.collection));
      if (Array.isArray(saved)) detail.querySelectorAll('[data-column-index]').forEach(box => { box.checked = saved.includes(box.dataset.columnName); });
    } catch (_) {}
    applyColumns();
  }
  document.addEventListener('change', event => {
    if (event.target.matches('#input-mode,#converter-collection')) syncConverter();
    if (event.target.matches('[data-column-index]')) applyColumns(true);
  });
  document.addEventListener('click', event => {
    const button = event.target.closest('[data-columns]');
    if (!button) return;
    document.querySelectorAll('[data-column-index]').forEach((box, i) => { box.checked = button.dataset.columns === 'all' || i < 12; });
    applyColumns(true);
  });
  document.body.addEventListener('htmx:afterSwap', () => { syncConverter(); restoreColumns(); });
  syncConverter(); restoreColumns();
})();
