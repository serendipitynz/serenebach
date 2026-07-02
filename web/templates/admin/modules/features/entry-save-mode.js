// entry-save-mode.js — entry editor save buttons.
//
// The left button is a constant "save as draft regardless of the
// dropdown" escape hatch; the right button is normally "公開して保存"
// (primary) but flips to "非公開で保存" (secondary) the moment the
// operator selects 非公開 from the status dropdown — one extra
// affordance only where it's needed. Draft + published share the same
// primary right button so the common publish flow stays visually
// unchanged.
//
// DOM contract:
//   [data-entry-form] (required container) holding [data-entry-status]
//     (the status <select>: 0 draft / 1 published / -1 closed).
//   [data-save-mode="draft"]   (optional) forces status = 0 on click.
//   [data-save-mode="dynamic"] (optional) primary button whose label /
//     primary-ness tracks the dropdown; on click it forces the status
//     the label promises. The dropdown stays the source of truth at
//     submit time.
// No network I/O; degrades to a plain submit when JS is off.

import { createI18n } from '../core/i18n.js';

const sbT = createI18n((typeof window !== 'undefined' && window.__sbI18n) || {});

export function initEntrySaveMode() {
  var entryForm = document.querySelector('[data-entry-form]');
  var entryStatusSelect = entryForm && entryForm.querySelector('[data-entry-status]');
  if (!entryForm || !entryStatusSelect) { return; }

  var draftBtn = entryForm.querySelector('[data-save-mode="draft"]');
  if (draftBtn) {
    draftBtn.addEventListener('click', function () {
      entryStatusSelect.value = '0';
    });
  }
  var dynamicBtn = entryForm.querySelector('[data-save-mode="dynamic"]');
  if (dynamicBtn) {
    var syncDynamicBtn = function () {
      if (entryStatusSelect.value === '-1') {
        dynamicBtn.textContent = sbT('js.entry.saveClose');
        dynamicBtn.classList.remove('primary');
      } else {
        dynamicBtn.textContent = sbT('js.entry.savePublish');
        dynamicBtn.classList.add('primary');
      }
    };
    syncDynamicBtn();
    entryStatusSelect.addEventListener('change', syncDynamicBtn);
    dynamicBtn.addEventListener('click', function () {
      // The button's visible intent must match the status that
      // actually ships: "公開して保存" forces publish, "非公開で
      // 保存" forces closed. Dropdown stays the single source of
      // truth at submit time.
      if (entryStatusSelect.value === '-1') {
        entryStatusSelect.value = '-1';
      } else {
        entryStatusSelect.value = '1';
      }
    });
  }
}
