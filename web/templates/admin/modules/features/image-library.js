import { createI18n } from '../core/i18n.js';
import { safeRead, safeWrite } from '../core/storage.js';
import { showToast } from '../core/toast.js';
import { openModal, closeModal } from '../core/modal.js';
import { readCSRFToken } from '../core/csrf.js';

const sbT = createI18n((typeof window !== 'undefined' && window.__sbI18n) || {});

export function initImageLibrary() {
  initImagePreview();
  initViewTogglePersistence();
  initImageFilter();
  initCopyURLButtons();
  initCustomTagHint();
  initRenameModal();
}

function initImagePreview() {
  document.querySelectorAll('[data-image-url]').forEach(function (host) {
    var url = host.getAttribute('data-image-url');
    if (!url) return;
    var trigger = host.querySelector('figure') || host.querySelector('.image-row-icon') || host;
    trigger.style.cursor = 'zoom-in';
    trigger.addEventListener('click', function (e) {
      if (e.target.closest('form, a, button')) return;
      e.preventDefault();
      var alt = host.getAttribute('data-image-alt') || '';
      var img = document.createElement('img');
      img.src = url;
      img.alt = alt;
      openModal({ title: alt || sbT('js.modal.image'), variant: 'image', bodyNode: img });
    });
  });
}

function initViewTogglePersistence() {
  document.querySelectorAll('.view-toggle .view-btn').forEach(function (a) {
    a.addEventListener('click', function () {
      var m = (a.getAttribute('href') || '').match(/[?&]view=(\w+)/);
      if (m) safeWrite('sb_admin_image_view', m[1]);
    });
  });
}

function initImageFilter() {
  var filterInput = document.querySelector('[data-image-filter]');
  if (filterInput) {
    filterInput.addEventListener('input', function () {
      var needle = filterInput.value.toLowerCase();
      document.querySelectorAll('[data-image-filterable]').forEach(function (container) {
        container.querySelectorAll('[data-filename]').forEach(function (node) {
          var name = (node.getAttribute('data-filename') || '').toLowerCase();
          node.style.display = (needle === '' || name.indexOf(needle) !== -1) ? '' : 'none';
        });
      });
    });
  }
}

var checkSVG = '<svg class="icon" viewBox="0 0 20 20" aria-hidden="true" focusable="false"><path d="M4 10.5l4 4L16 6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>';

function initCopyURLButtons() {
  document.querySelectorAll('[data-copy-url]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var url = btn.getAttribute('data-copy-url') || '';
      if (!url) return;
      var kind = btn.getAttribute('data-copy-kind') || btn.closest('[data-kind]')?.getAttribute('data-kind') || 'image';
      var filename = btn.getAttribute('data-copy-filename') || btn.closest('[data-filename]')?.getAttribute('data-filename') || '';
      var text;
      switch (kind) {
        case 'image':
          text = '<img src="' + url + '" alt="' + (filename || '') + '">';
          break;
        case 'audio':
          text = '<audio controls src="' + url + '"></audio>';
          break;
        case 'movie':
          text = '<video controls src="' + url + '"></video>';
          break;
        case 'document':
          text = '<a href="' + url + '" download>' + (filename || url.split('/').pop() || 'file') + '</a>';
          break;
        default:
          text = url;
      }
      copyViaClipboard(text, btn);
    });
  });

  document.querySelectorAll('[data-copy-text]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var text = btn.getAttribute('data-copy-text') || '';
      if (!text) return;
      copyViaClipboard(text, btn);
    });
  });
}

function initCustomTagHint() {
  var input = document.querySelector('[data-customtag-name-input]');
  var hint = document.querySelector('[data-customtag-value-hint]');
  if (!input || !hint) return;
  var named = hint.getAttribute('data-hint-named') || '';
  var empty = hint.getAttribute('data-hint-empty') || '';
  function update() {
    var name = (input.value || '').trim();
    if (!name) {
      hint.textContent = empty;
      return;
    }
    hint.textContent = named.replace('%s', '{custom_' + name + '}');
  }
  input.addEventListener('input', update);
}

function copyViaClipboard(text, btn) {
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(text).then(function () {
      onCopySuccess(btn);
    });
  } else {
    var ta = document.createElement('textarea');
    ta.value = text;
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand('copy'); onCopySuccess(btn); } catch (e) { /* ignore */ }
    document.body.removeChild(ta);
  }
}

function onCopySuccess(btn) {
  if (btn.querySelector('svg')) {
    flashIcon(btn, checkSVG);
  } else {
    flashText(btn, sbT('js.copy.done'));
  }
  showToast(sbT('js.copy.done'));
}

function flashIcon(btn, html) {
  var original = btn.innerHTML;
  btn.innerHTML = html;
  setTimeout(function () { btn.innerHTML = original; }, 1200);
}

function flashText(btn, msg) {
  var original = btn.textContent;
  btn.textContent = msg;
  setTimeout(function () { btn.textContent = original; }, 1200);
}

function initRenameModal() {
  document.querySelectorAll('[data-rename-id]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var id = btn.getAttribute('data-rename-id');
      var currentName = btn.getAttribute('data-rename-current') || '';

      var wrap = document.createElement('div');
      wrap.className = 'form-stack';
      var label = document.createElement('label');
      label.textContent = sbT('js.modal.renameImage.nameLabel');
      var input = document.createElement('input');
      input.type = 'text';
      input.value = currentName;
      input.maxLength = 255;
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
        var body = new URLSearchParams({ filename: v, csrf_token: token });
        var url = (window.__sbRoot || '') + '/admin/images/' + id + '/rename';
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
              errBox.textContent = (r.data && r.data.error) ? r.data.error : sbT('js.modal.renameImage.failed');
              errBox.hidden = false;
              ok.disabled = false;
              cancel.disabled = false;
              return;
            }
            var newName = r.data.filename;
            currentName = newName;
            btn.setAttribute('data-rename-current', newName);

            // Update the closest row/tile in place so the list stays
            // current without a full reload.
            var row = btn.closest('[data-filename]');
            if (row) {
              row.setAttribute('data-filename', newName);
              row.setAttribute('data-image-alt', newName);
              var titleHost = row.querySelector('.cell-clamp-host');
              if (titleHost) titleHost.title = newName;
              var clamp = row.querySelector('.cell-clamp-2');
              if (clamp) clamp.textContent = newName;
              var nameSpan = row.querySelector('.name');
              if (nameSpan) {
                nameSpan.textContent = newName;
                nameSpan.title = newName;
              }
              row.querySelectorAll('img').forEach(function (img) {
                img.alt = newName;
              });
            }

            closeModal();
            showToast(sbT('js.save.done'));
          })
          .catch(function () {
            errBox.textContent = sbT('js.modal.renameImage.failed');
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

      openModal({ title: sbT('js.modal.renameImage.title'), bodyNode: wrap, footerNode: footer });
      setTimeout(function () { input.focus(); input.select(); }, 0);
    });
  });
}
