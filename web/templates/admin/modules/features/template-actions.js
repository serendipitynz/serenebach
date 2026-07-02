// template-actions.js — design-settings template actions: read-only info
// modal, save-as (clone), inline rename (fetch → JSON), and export.
//
// DOM contract (each block is optional; absent triggers are skipped):
//   [data-template-info]     button; reads data-meta-name / -author /
//     -address / -version / -memo-html for a read-only info modal.
//   [data-template-save-as]  button inside the editor <form>; reads
//     data-current-name, writes the chosen name into
//     [data-template-new-name], and submits the form to .../save-as so
//     the server clones. Template id is taken from the form action path.
//   [data-template-rename]   button; reads data-template-id,
//     data-current-name, data-page-title-prefix / -suffix. POSTs
//     {name, csrf_token} to /admin/templates/{id}/rename and expects
//     JSON {ok, name?, error?}; patches [data-template-name] +
//     document.title and keeps save-as / export pre-fills in sync.
//   [data-template-export]   button; reads data-export-id,
//     data-current-name, data-current-memo; navigates to
//     .../export?name&memo.
// Depends on: core/modal, core/csrf (readCSRFToken), core/kv, window.__sbRoot.
// No-JS fallback: none — these are enhancement-only buttons.

import { createI18n } from '../core/i18n.js';
import { openModal, closeModal } from '../core/modal.js';
import { readCSRFToken } from '../core/csrf.js';
import { appendKV, linkifyNode } from '../core/kv.js';

const sbT = createI18n((typeof window !== 'undefined' && window.__sbI18n) || {});

export function initTemplateActions() {
  initTemplateInfo();
  initTemplateSaveAs();
  initTemplateRename();
  initTemplateExport();
}

// Info button in the design-settings list: pull the metadata off the
// row's data-meta-* attributes and render a read-only modal.
function initTemplateInfo() {
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
}

// Save-as: prompt for a new name, write it to the hidden field, and
// submit the form with the save-as action so the server clones.
function initTemplateSaveAs() {
  var saveAsBtn = document.querySelector('[data-template-save-as]');
  if (!saveAsBtn) { return; }
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
      if (nameField) { nameField.value = v; }
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
function initTemplateRename() {
  var renameBtn = document.querySelector('[data-template-rename]');
  if (!renameBtn) { return; }
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
          if (span) { span.textContent = newName; }
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
          if (saveAsBtn) { saveAsBtn.setAttribute('data-current-name', newName); }
          var exportBtn = document.querySelector('[data-template-export]');
          if (exportBtn) { exportBtn.setAttribute('data-current-name', newName); }
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
function initTemplateExport() {
  var exportBtn = document.querySelector('[data-template-export]');
  if (!exportBtn) { return; }
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
