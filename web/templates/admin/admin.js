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

const ace = initAceEditors();
initImagePicker(ace.ready);
setAIButtonCallback(runAceAI);

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

function hasFiles(dt) {
  if (!dt.types) return false;
  for (var i = 0; i < dt.types.length; i++) {
    if (dt.types[i] === 'Files') return true;
  }
  return false;
}

// ---- AI suggestion popup -------------------------------------------
// Floating popup that shows AI-generated text with copy/insert
// actions. Draggable header, minimizable, reusable singleton.
var aiPopupInstance = null;
var aiPopupRequestCounter = 0;

function getAIPopup() {
  if (aiPopupInstance) return aiPopupInstance;
  var root = document.createElement('div');
  root.className = 'ai-popup';
  root.innerHTML =
    '<div class="ai-popup-header">' +
      '<span class="ai-popup-title"></span>' +
      '<span class="ai-popup-spinner sb-spinner" aria-hidden="true"></span>' +
      '<button type="button" class="ai-popup-minimize" aria-label="minimize">[-]</button>' +
    '</div>' +
    '<div class="ai-popup-body">' +
      '<pre class="ai-popup-text"></pre>' +
    '</div>' +
    '<div class="ai-popup-footer">' +
      '<button type="button" class="btn" data-ai-popup-close></button>' +
      '<button type="button" class="btn" data-ai-popup-copy></button>' +
      '<button type="button" class="btn btn-primary" data-ai-popup-insert></button>' +
    '</div>';
  document.body.appendChild(root);

  var titleEl = root.querySelector('.ai-popup-title');
  var spinnerEl = root.querySelector('.ai-popup-spinner');
  var textEl = root.querySelector('.ai-popup-text');
  var closeBtn = root.querySelector('[data-ai-popup-close]');
  var copyBtn = root.querySelector('[data-ai-popup-copy]');
  var insertBtn = root.querySelector('[data-ai-popup-insert]');
  var minimizeBtn = root.querySelector('.ai-popup-minimize');
  var header = root.querySelector('.ai-popup-header');
  var body = root.querySelector('.ai-popup-body');
  var footer = root.querySelector('.ai-popup-footer');

  closeBtn.textContent = sbT('js.ai.close');
  copyBtn.textContent = sbT('js.ai.copy');
  insertBtn.textContent = sbT('js.ai.insert');

  function svgIcon(paths) {
    return '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + paths + '</svg>';
  }
  var iconMinus = svgIcon('<path d="M5 12h14"/>');
  var iconPlus = svgIcon('<path d="M5 12h14"/><path d="M12 5v14"/>');
  minimizeBtn.innerHTML = iconMinus;

  var currentText = '';
  var currentEditor = null;
  var currentAction = '';
  var currentSelection = '';
  var currentRequestId = 0;
  var minimized = false;

  function updateButtons() {
    var hasError = textEl.classList.contains('ai-popup-text--error');
    var hasText = !!currentText && !hasError;
    copyBtn.disabled = !hasText;
    insertBtn.disabled = !hasText || !currentEditor;
  }

  closeBtn.addEventListener('click', function () { root.hidden = true; });
  copyBtn.addEventListener('click', function () {
    if (!currentText) return;
    navigator.clipboard.writeText(currentText).catch(function () {});
  });
  insertBtn.addEventListener('click', function () {
    if (!currentText || !currentEditor) return;
    applyAIResult(currentEditor, currentAction, currentSelection, currentText);
    root.hidden = true;
  });
  minimizeBtn.addEventListener('click', function () {
    minimized = !minimized;
    root.classList.toggle('ai-popup--minimized', minimized);
    minimizeBtn.innerHTML = minimized ? iconPlus : iconMinus;
    minimizeBtn.setAttribute('aria-label', minimized ? 'restore' : 'minimize');
  });

  // Drag handling — document-level listeners so the popup only moves
  // while the pointer is actually held down. Avoids pointer-capture
  // quirks where hovering the header can trigger movement.
  var dragging = false;
  var dragOffsetX = 0;
  var dragOffsetY = 0;

  function onPopupDragMove(e) {
    if (!dragging) return;
    root.style.left = (e.clientX - dragOffsetX) + 'px';
    root.style.top = (e.clientY - dragOffsetY) + 'px';
  }
  function onPopupDragUp() {
    if (!dragging) return;
    dragging = false;
    document.removeEventListener('pointermove', onPopupDragMove);
    document.removeEventListener('pointerup', onPopupDragUp);
  }

  header.addEventListener('pointerdown', function (e) {
    if (e.target.closest('.ai-popup-minimize')) return;
    dragging = true;
    var rect = root.getBoundingClientRect();
    dragOffsetX = e.clientX - rect.left;
    dragOffsetY = e.clientY - rect.top;
    root.style.transform = 'none';
    root.style.left = rect.left + 'px';
    root.style.top = rect.top + 'px';
    document.addEventListener('pointermove', onPopupDragMove);
    document.addEventListener('pointerup', onPopupDragUp);
    e.preventDefault();
  });

  aiPopupInstance = {
    el: root,
    open: function (title, editor, action, selection, requestId) {
      currentRequestId = requestId;
      currentEditor = editor || null;
      currentAction = action || '';
      currentSelection = selection || '';
      currentText = '';
      titleEl.textContent = title || '';
      textEl.textContent = sbT('js.ai.processing');
      textEl.className = 'ai-popup-text ai-popup-text--loading';
      spinnerEl.style.display = '';
      closeBtn.disabled = false;
      copyBtn.disabled = true;
      insertBtn.disabled = true;
      minimized = false;
      root.classList.remove('ai-popup--minimized');
      minimizeBtn.innerHTML = iconMinus;
      minimizeBtn.setAttribute('aria-label', 'minimize');
      root.hidden = false;
      // Center on first open; subsequent opens keep last position unless closed
      if (!root.style.left) {
        root.style.left = Math.max(16, Math.round((window.innerWidth - 360) / 2)) + 'px';
        root.style.top = Math.max(16, Math.round((window.innerHeight - 240) / 2)) + 'px';
      }
    },
    setContent: function (text, requestId) {
      if (requestId !== currentRequestId) return;
      currentText = text || '';
      textEl.textContent = currentText;
      textEl.className = 'ai-popup-text';
      spinnerEl.style.display = 'none';
      updateButtons();
    },
    setError: function (msg, requestId) {
      if (requestId !== currentRequestId) return;
      currentText = '';
      textEl.textContent = msg || sbT('js.ai.err.provider_error');
      textEl.className = 'ai-popup-text ai-popup-text--error';
      spinnerEl.style.display = 'none';
      updateButtons();
    },
    close: function () {
      root.hidden = true;
    },
  };
  return aiPopupInstance;
}

// postCompose POSTs `payload` to /admin/ai/compose and resolves to the
// parsed JSON, falling back to {ok:false,error:'parse'} when the body
// isn't JSON. Callers chain their own .then to drive the popup / toast.
function postCompose(payload) {
  return fetch('/admin/ai/compose', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Accept': 'application/json',
      'X-CSRF-Token': readCSRFToken(),
    },
    body: JSON.stringify(payload),
    credentials: 'same-origin'
  }).then(function (res) { return res.json().catch(function () { return { ok: false, error: 'parse' }; }); });
}

// ---- Ace AI toolbar dispatcher --------------------------------------
// Shared helper so the three toolbar buttons (rewrite / continue /
// summarise) POST to /admin/ai/compose with the right context +
// show the reply inside a suggestion popup instead of inserting
// immediately.
function runAceAI(editor, btn, action) {
  if (!editor || !action) return;
  var selection = (editor.getSelectedText() || '').trim();
  var full = editor.getValue();
  var req = {
    action: action,
    format: detectEditorFormat(editor),
    language: document.documentElement.lang || 'ja',
  };

  if (action === 'rewrite' || action === 'summarise') {
    if (!selection) {
      showToast(sbT('js.ai.selectionRequired'));
      return;
    }
    req.text = selection;
  } else if (action === 'continue') {
    var cursorPos = editor.getCursorPosition();
    var doc = editor.session.getDocument();
    var upto = doc.getTextRange({ start: { row: 0, column: 0 }, end: cursorPos });
    req.context = upto.trim() || full.trim();
    if (!req.context) {
      showToast(sbT('js.ai.contextRequired'));
      return;
    }
  }

  var popup = getAIPopup();
  var requestId = ++aiPopupRequestCounter;
  var titleKey = 'js.ai.popupTitle.' + action;
  popup.open(sbT(titleKey), editor, action, selection, requestId);

  var restore = setButtonLoading(btn);

  postCompose(req)
    .then(function (data) {
      if (!data || !data.ok) {
        var key = (data && data.error) || 'provider_error';
        popup.setError(sbT('js.ai.err.' + key), requestId);
        return;
      }
      popup.setContent(data.text || '', requestId);
    })
    .catch(function () { popup.setError(sbT('js.ai.err.provider_error'), requestId); })
    .then(restore);
}

function applyAIResult(editor, action, selection, text) {
  if (!text) return;
  editor.focus();
  if (action === 'rewrite') {
    editor.session.replace(editor.selection.getRange(), text);
    return;
  }
  if (action === 'continue') {
    editor.insert('\n\n' + text);
    return;
  }
  if (action === 'summarise') {
    var heading = detectEditorFormat(editor) === 'markdown' ? '## Summary\n\n' : '<h2>Summary</h2>\n\n';
    editor.navigateFileStart();
    editor.insert(heading + text + '\n\n');
    return;
  }
}

function detectEditorFormat(editor) {
  var textarea = editor && editor.__hostTextarea;
  if (!textarea) return 'html';
  var form = textarea.closest && textarea.closest('form');
  if (!form) return 'html';
  var sel = form.querySelector('select[data-code-editor-format]');
  if (sel && sel.value) return sel.value;
  return 'html';
}

// ---- Entry-form suggest buttons -------------------------------------
// [data-ai-suggest] buttons dispatch title / tags / keywords
// generation based on the current body content. The reply fills
// the bound input — title inline, tags / keywords appended to
// whatever the author already entered so an existing list isn't
// blown away by accident.
Array.prototype.forEach.call(document.querySelectorAll('[data-ai-suggest]'), function (btn) {
  btn.addEventListener('click', function () {
    var action = btn.getAttribute('data-ai-suggest');
    var form = btn.closest('form');
    if (!form) return;
    var bodyEl = form.querySelector('textarea[name="body"]');
    var titleEl = form.querySelector('input[name="title"]');
    if (!bodyEl) return;
    // Prefer the live Ace value when the editor's mounted; the
    // textarea may still be stale until submit.
    var body = bodyEl.__aceEditor ? bodyEl.__aceEditor.getValue() : bodyEl.value;
    var title = titleEl ? titleEl.value : '';
    var textForPrompt = (title ? title + '\n\n' : '') + body;
    if (!body.trim()) {
      showToast(sbT('js.ai.bodyRequired'));
      return;
    }

    var restore = setButtonLoading(btn);
    showToast(sbT('js.ai.thinking'));

    postCompose({
      action: action,
      text: textForPrompt,
      format: form.querySelector('select[name="format"]') ? form.querySelector('select[name="format"]').value : 'html',
      language: document.documentElement.lang || 'ja',
    })
      .then(function (data) {
        if (!data || !data.ok) {
          showToast(sbT('js.ai.err.' + (data && data.error || 'provider_error')));
          return;
        }
        applySuggestion(form, action, data.text || '');
      })
      .catch(function () { showToast(sbT('js.ai.err.provider_error')); })
      .then(restore);
  });
});

function applySuggestion(form, action, suggestion) {
  var clean = (suggestion || '').trim().replace(/^["'「]+|["'」]+$/g, '');
  if (action === 'title') {
    var titleEl = form.querySelector('input[name="title"]');
    if (titleEl) {
      titleEl.value = clean;
      titleEl.dispatchEvent(new Event('input', { bubbles: true }));
    }
    return;
  }
  var targetName = action === 'tags' ? 'tags' : 'keywords';
  var target = form.querySelector('input[name="' + targetName + '"]');
  if (!target) return;
  // Append-merge: split both existing + suggested on "," and
  // uniquify so "tag1, tag2" + "tag2, tag3" becomes "tag1, tag2, tag3".
  var existing = (target.value || '').split(',').map(function (s) { return s.trim(); }).filter(Boolean);
  var suggested = clean.split(/[,、]/).map(function (s) { return s.trim(); }).filter(Boolean);
  var merged = existing.slice();
  suggested.forEach(function (s) {
    var lower = s.toLowerCase();
    if (!merged.some(function (e) { return e.toLowerCase() === lower; })) {
      merged.push(s);
    }
  });
  target.value = merged.join(', ');
  target.dispatchEvent(new Event('input', { bubbles: true }));
}

// ---- AI settings test button ----------------------------------------
// The AI settings form posts to /admin/settings/ai. This wires the
// 疎通テスト button so the user can verify the saved provider
// responds before committing to using it in the editor.
var aiTestBtn = document.querySelector('[data-ai-test-btn]');
if (aiTestBtn) {
  var aiResultSlot = document.querySelector('[data-ai-test-result]');
  aiTestBtn.addEventListener('click', function () {
    if (!aiResultSlot) return;
    var restore = setButtonLoading(aiTestBtn);
    aiResultSlot.hidden = false;
    aiResultSlot.classList.remove('error');
    aiResultSlot.textContent = sbT('js.ai.testing');

    var form = new FormData();
    var csrf = document.querySelector('input[name="csrf_token"]');
    if (csrf) form.append('csrf_token', csrf.value);

    fetch('/admin/settings/ai/test', {
      method: 'POST',
      body: form,
      credentials: 'same-origin',
      headers: { 'Accept': 'application/json' }
    }).then(function (res) {
      return res.json().catch(function () { return { ok: false, error: 'HTTP ' + res.status }; });
    }).then(function (data) {
      if (data && data.ok) {
        aiResultSlot.classList.remove('error');
        aiResultSlot.textContent = sbT('js.ai.testOK') + ' — ' + (data.text || '');
        showToast(sbT('js.ai.testOK'));
      } else {
        aiResultSlot.classList.add('error');
        aiResultSlot.textContent = sbT('js.ai.testFail') + ': ' + ((data && data.error) || 'unknown');
        showToast(sbT('js.ai.testFail'));
      }
    }).catch(function (err) {
      aiResultSlot.classList.add('error');
      aiResultSlot.textContent = sbT('js.ai.testFail') + ': ' + err;
    }).then(restore);
  });
}