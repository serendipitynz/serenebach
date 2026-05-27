// Tiny admin.js: mobile drawer + image upload drop-zone + editor insert.
// Keep vanilla and free of build steps so "drop the binary in" still
// works end-to-end without a bundler.

import { createI18n } from './modules/core/i18n.js';
import { safeRead, safeWrite } from './modules/core/storage.js';
import { showToast, initToastPromotion } from './modules/core/toast.js';
import { readCSRFToken, csrfTokenFrom } from './modules/core/csrf.js';
import { openModal, closeModal } from './modules/core/modal.js';
import { setButtonLoading } from './modules/core/loading.js';
import { initAppearanceLanguage } from './modules/features/appearance-language.js';
import { initNavigation } from './modules/features/navigation.js';
import { initSortableLists } from './modules/features/sortable-list.js';
import { initLinkKindToggle } from './modules/features/link-form.js';
import { initDateFormatPreview } from './modules/features/date-format-preview.js';
import { initDropToInput } from './modules/features/drop-to-input.js';
import { initUploadForms } from './modules/features/uploads.js';
import { initImageLibrary } from './modules/features/image-library.js';
import { initImagePicker } from './modules/features/image-picker.js';
import { initAceEditors, setAIButtonCallback } from './modules/features/ace-editor.js';
import { runAceAI, initAISuggestButtons, initAITestButton } from './modules/features/ai-assist.js';
import { initHintTooltips } from './modules/features/hint-tooltip.js';

const sbT = createI18n((typeof window !== 'undefined' && window.__sbI18n) || {});

initToastPromotion();
initAppearanceLanguage();
initNavigation();
initSortableLists(sbT);
initLinkKindToggle();
initDateFormatPreview();
initDropToInput();
initUploadForms();
initImageLibrary();
initHintTooltips();

const ace = initAceEditors();
initImagePicker(ace.ready);
setAIButtonCallback(runAceAI);
initAISuggestButtons();
initAITestButton();

// ---- comment detail modal ------------------------------------------
// Author / body cells on the moderation list carry the full comment
// payload via data-* attributes. Click → modal with author metadata +
// the complete body (no line clamp inside the dialog).
document.querySelectorAll('.cell-clickable[data-comment-body]').forEach(function (cell) {
  cell.addEventListener('click', function (e) {
    if (e.target.closest('a, button, form')) return;
    e.preventDefault();
    var author = cell.getAttribute('data-comment-author') || '';
    var email = cell.getAttribute('data-comment-email') || '';
    var url = cell.getAttribute('data-comment-url') || '';
    var body = cell.getAttribute('data-comment-body') || '';
    var ip = cell.getAttribute('data-comment-ip') || '';
    var dl = document.createElement('dl');
    dl.className = 'kv';
    var DASH = sbT('js.field.dash');
    appendKV(dl, sbT('js.field.commentAuthor'), author || DASH);
    appendKV(dl, sbT('js.field.email'), email || DASH);
    appendKV(dl, 'URL', url ? linkifyNode(url) : DASH);
    appendKV(dl, 'IP', ip || DASH);
    appendKV(dl, sbT('js.field.commentBody'), body || DASH);
    openModal({ title: sbT('js.modal.comment'), bodyNode: dl });
  });
});
function appendKV(dl, key, value) {
  var dt = document.createElement('dt'); dt.textContent = key;
  var dd = document.createElement('dd');
  if (value && value.nodeType) dd.appendChild(value); else dd.textContent = value;
  dl.appendChild(dt); dl.appendChild(dd);
}
function linkifyNode(u) {
  var a = document.createElement('a');
  a.href = u; a.target = '_blank'; a.rel = 'noopener nofollow';
  a.textContent = u; return a;
}

// ---- template info / save-as / export modals ----------------------

// Info button in the design-settings list: pull the metadata off the
// row's data-meta-* attributes and render a read-only modal.
document.querySelectorAll('[data-template-info]').forEach(function (btn) {
  btn.addEventListener('click', function () {
    var container = document.createElement('div');
    var dl = document.createElement('dl');
    dl.className = 'kv';
    var DASH = sbT('js.field.dash');
    appendKV(dl, sbT('js.field.templateName'), btn.getAttribute('data-meta-name') || DASH);
    appendKV(dl, sbT('js.field.author'), btn.getAttribute('data-meta-author') || DASH);
    var addr = btn.getAttribute('data-meta-address');
    appendKV(dl, 'URL', addr ? linkifyNode(addr) : DASH);
    appendKV(dl, sbT('js.field.version'), btn.getAttribute('data-meta-version') || DASH);
    container.appendChild(dl);

    var memoHtml = btn.getAttribute('data-meta-memo-html');
    if (memoHtml) {
      var hr = document.createElement('hr');
      container.appendChild(hr);
      var memoDiv = document.createElement('div');
      memoDiv.className = 'md-content';
      memoDiv.innerHTML = memoHtml;
      container.appendChild(memoDiv);
    }
    openModal({ title: sbT('js.modal.templateInfo'), bodyNode: container });
  });
});

// Save-as: prompt for a new name, write it to the hidden field, and
// submit the form with the save-as action so the server clones.
var saveAsBtn = document.querySelector('[data-template-save-as]');
if (saveAsBtn) {
  saveAsBtn.addEventListener('click', function () {
    var form = saveAsBtn.closest('form');
    var nameField = form.querySelector('[data-template-new-name]');
    var tplID = form.getAttribute('action').split('/')[3];
    var currentName = saveAsBtn.getAttribute('data-current-name') || '';

    var wrap = document.createElement('div');
    wrap.className = 'form-stack';
    var label = document.createElement('label');
    label.textContent = sbT('js.modal.saveAs.nameLabel');
    var input = document.createElement('input');
    input.type = 'text';
    input.value = currentName + sbT('js.modal.saveAs.suffix');
    label.appendChild(input);
    wrap.appendChild(label);

    var footer = document.createElement('div');
    footer.style.display = 'flex';
    footer.style.gap = '0.5rem';
    var cancel = document.createElement('button');
    cancel.type = 'button';
    cancel.textContent = sbT('js.action.cancel');
    cancel.addEventListener('click', closeModal);
    var ok = document.createElement('button');
    ok.type = 'button';
    ok.className = 'primary';
    ok.textContent = sbT('js.action.save');
    ok.addEventListener('click', function () {
      var v = input.value.trim();
      if (!v) { input.focus(); return; }
      if (nameField) nameField.value = v;
      form.action = '/admin/templates/' + tplID + '/save-as';
      if (form.requestSubmit) {
        form.requestSubmit();
      } else {
        var btn = document.createElement('button');
        btn.type = 'submit';
        btn.style.display = 'none';
        form.appendChild(btn);
        btn.click();
        form.removeChild(btn);
      }
    });
    footer.appendChild(cancel);
    footer.appendChild(ok);

    openModal({ title: sbT('js.action.saveAs'), bodyNode: wrap, footerNode: footer });
    setTimeout(function () { input.focus(); input.select(); }, 0);
  });
}

// Rename: inline edit of just the template name. POSTs to a dedicated
// /rename endpoint and patches the page header in place — the main
// editor's unsaved-changes state is left alone, so authors can fix a
// typo without losing in-flight body edits.
var renameBtn = document.querySelector('[data-template-rename]');
if (renameBtn) {
  renameBtn.addEventListener('click', function () {
    var tplID = renameBtn.getAttribute('data-template-id');
    var currentName = renameBtn.getAttribute('data-current-name') || '';

    var wrap = document.createElement('div');
    wrap.className = 'form-stack';
    var label = document.createElement('label');
    label.textContent = sbT('js.modal.rename.nameLabel');
    var input = document.createElement('input');
    input.type = 'text';
    input.value = currentName;
    input.maxLength = 200;
    label.appendChild(input);
    wrap.appendChild(label);

    var errBox = document.createElement('p');
    errBox.className = 'alert error';
    errBox.hidden = true;
    wrap.appendChild(errBox);

    var footer = document.createElement('div');
    footer.style.display = 'flex';
    footer.style.gap = '0.5rem';
    var cancel = document.createElement('button');
    cancel.type = 'button';
    cancel.textContent = sbT('js.action.cancel');
    cancel.addEventListener('click', closeModal);
    var ok = document.createElement('button');
    ok.type = 'button';
    ok.className = 'primary';
    ok.textContent = sbT('js.action.save');

    function submit() {
      var v = input.value.trim();
      if (!v) { input.focus(); return; }
      if (v === currentName) { closeModal(); return; }
      ok.disabled = true;
      cancel.disabled = true;
      errBox.hidden = true;
      var token = readCSRFToken();
      var body = new URLSearchParams({ name: v, csrf_token: token });
      var url = (window.__sbRoot || '') + '/admin/templates/' + tplID + '/rename';
      fetch(url, {
        method: 'POST',
        headers: { 'X-CSRF-Token': token, 'Accept': 'application/json' },
        body: body,
        credentials: 'same-origin'
      })
        .then(function (res) {
          return res.json().then(function (data) { return { ok: res.ok, data: data }; });
        })
        .then(function (r) {
          if (!r.ok || !r.data || !r.data.ok) {
            errBox.textContent = (r.data && r.data.error) ? r.data.error : sbT('js.modal.rename.failed');
            errBox.hidden = false;
            ok.disabled = false;
            cancel.disabled = false;
            return;
          }
          var newName = r.data.name;
          currentName = newName;
          renameBtn.setAttribute('data-current-name', newName);
          var span = document.querySelector('[data-template-name]');
          if (span) span.textContent = newName;
          // Rebuild document.title from server-supplied prefix/suffix
          // attributes rather than parsing the current title — a name
          // containing ": " or " | " would otherwise be split mid-name.
          var titlePrefix = renameBtn.getAttribute('data-page-title-prefix');
          var titleSuffix = renameBtn.getAttribute('data-page-title-suffix') || '';
          if (titlePrefix !== null) {
            document.title = titlePrefix + newName + titleSuffix;
          }
          // Keep the save-as / export buttons' pre-fill in sync.
          var saveAsBtn = document.querySelector('[data-template-save-as]');
          if (saveAsBtn) saveAsBtn.setAttribute('data-current-name', newName);
          var exportBtn = document.querySelector('[data-template-export]');
          if (exportBtn) exportBtn.setAttribute('data-current-name', newName);
          closeModal();
        })
        .catch(function () {
          errBox.textContent = sbT('js.modal.rename.failed');
          errBox.hidden = false;
          ok.disabled = false;
          cancel.disabled = false;
        });
    }

    ok.addEventListener('click', submit);
    input.addEventListener('keydown', function (e) {
      if (e.key === 'Enter') { e.preventDefault(); submit(); }
    });
    footer.appendChild(cancel);
    footer.appendChild(ok);

    openModal({ title: sbT('js.modal.rename.title'), bodyNode: wrap, footerNode: footer });
    setTimeout(function () { input.focus(); input.select(); }, 0);
  });
}

// Export: prompt for optional name / memo overrides, then nav to the
// GET export endpoint with those values on the query string.
var exportBtn = document.querySelector('[data-template-export]');
if (exportBtn) {
  exportBtn.addEventListener('click', function () {
    var id = exportBtn.getAttribute('data-export-id');
    var currentName = exportBtn.getAttribute('data-current-name') || '';
    var currentMemo = exportBtn.getAttribute('data-current-memo') || '';

    var wrap = document.createElement('div');
    wrap.className = 'form-stack';

    var nameLabel = document.createElement('label');
    nameLabel.textContent = sbT('js.modal.export.nameLabel');
    var nameInput = document.createElement('input');
    nameInput.type = 'text';
    nameInput.value = currentName;
    nameLabel.appendChild(nameInput);

    var memoLabel = document.createElement('label');
    memoLabel.textContent = sbT('js.modal.export.memoLabel');
    var memoArea = document.createElement('textarea');
    memoArea.rows = 6;
    memoArea.value = currentMemo;
    memoLabel.appendChild(memoArea);

    wrap.appendChild(nameLabel);
    wrap.appendChild(memoLabel);

    var footer = document.createElement('div');
    footer.style.display = 'flex';
    footer.style.gap = '0.5rem';
    var cancel = document.createElement('button');
    cancel.type = 'button'; cancel.textContent = sbT('js.action.cancel');
    cancel.addEventListener('click', closeModal);
    var ok = document.createElement('button');
    ok.type = 'button'; ok.className = 'primary'; ok.textContent = sbT('js.action.download');
    ok.addEventListener('click', function () {
      var q = [];
      if (nameInput.value.trim() !== '' && nameInput.value !== currentName) {
        q.push('name=' + encodeURIComponent(nameInput.value.trim()));
      }
      if (memoArea.value !== currentMemo) {
        q.push('memo=' + encodeURIComponent(memoArea.value));
      }
      var url = '/admin/templates/' + id + '/export' + (q.length ? '?' + q.join('&') : '');
      window.location.href = url;
      closeModal();
    });
    footer.appendChild(cancel); footer.appendChild(ok);

    openModal({ title: sbT('js.modal.export'), bodyNode: wrap, footerNode: footer, variant: 'wide' });
    setTimeout(function () { nameInput.focus(); nameInput.select(); }, 0);
  });
}

// ---- entry-form save-mode buttons (draft / dynamic) ------------------
// Left button is a constant "save as draft regardless of dropdown"
// escape hatch; right button is normally "公開して保存" (primary)
// but flips to "非公開で保存" (secondary) the moment the operator
// selects 非公開 from the status dropdown — one extra affordance
// only where it's needed. Draft + published share the same primary
// right button so the common publish flow stays visually
// unchanged.
var entryForm = document.querySelector('[data-entry-form]');
var entryStatusSelect = entryForm && entryForm.querySelector('[data-entry-status]');
if (entryForm && entryStatusSelect) {
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

// ---- unsaved-change warning on admin edit forms --------------------
// Any form tagged [data-unsaved-warn] gets a browser beforeunload
// warning if its contents diverge from the initial snapshot. Covers
// entry / category / link / template / user / profile editors. We
// compare a string-keyed FormData snapshot (file inputs excluded,
// since File objects don't serialise and their own pickers handle
// unsaved state separately), and skip the prompt during legitimate
// form submission.
initUnsavedWarn(ace.ready);

function initUnsavedWarn(aceReady) {
  var forms = document.querySelectorAll('form[data-unsaved-warn]');
  if (!forms.length) return;
  var submitting = false;
  forms.forEach(function (form) {
    var initial = null;
    aceReady.then(function () {
      setTimeout(function () { initial = snapshot(form); }, 0);
    });

    form.addEventListener('submit', function () { submitting = true; });
    document.addEventListener('submit', function () { submitting = true; }, true);

    window.addEventListener('beforeunload', function (e) {
      if (submitting) return;
      if (!document.body.contains(form)) return;
      if (initial === null) return;
      if (snapshot(form) === initial) return;
      e.preventDefault();
      e.returnValue = '';
      return '';
    });
  });
}

// snapshot returns a stable string form of every scalar field on
// `form`, suitable for comparison. Keys are URI-encoded and sorted
// so unordered FormData iteration doesn't produce false positives.
function snapshot(form) {
  if (!window.FormData) return '';
  var fd = new FormData(form);
  var pairs = [];
  fd.forEach(function (v, k) {
    if (typeof v === 'string') {
      pairs.push(encodeURIComponent(k) + '=' + encodeURIComponent(v));
    }
  });
  pairs.sort();
  return pairs.join('&');
}
