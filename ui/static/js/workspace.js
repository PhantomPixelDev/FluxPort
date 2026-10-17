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
  document.addEventListener('change', event => {
    if (event.target.matches('#input-mode,#converter-collection')) syncConverter();
  });
  document.addEventListener('click', event => {
	if(event.target.closest('[data-sql-insert]')){const form=event.target.closest('form'),input=form?.querySelector('[data-sql-identifier]'),editor=form?.querySelector('textarea[name="sql"]');if(input?.value&&editor){const name='"'+input.value.replaceAll('"','""')+'"';editor.setRangeText(name,editor.selectionStart,editor.selectionEnd,'end');editor.focus();editor.dispatchEvent(new Event('input',{bubbles:true}));}return;}
  });
  document.body.addEventListener('htmx:afterSwap', () => { syncConverter(); });
  syncConverter();
})();
