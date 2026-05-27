import { createI18n } from '../core/i18n.js';
import { showToast } from '../core/toast.js';
import { readCSRFToken } from '../core/csrf.js';
import { setButtonLoading } from '../core/loading.js';

const sbT = createI18n((typeof window !== 'undefined' && window.__sbI18n) || {});

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

export function runAceAI(editor, btn, action) {
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

export function initAISuggestButtons() {
  Array.prototype.forEach.call(document.querySelectorAll('[data-ai-suggest]'), function (btn) {
    btn.addEventListener('click', function () {
      var action = btn.getAttribute('data-ai-suggest');
      var form = btn.closest('form');
      if (!form) return;
      var bodyEl = form.querySelector('textarea[name="body"]');
      var titleEl = form.querySelector('input[name="title"]');
      if (!bodyEl) return;
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
}

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

export function initAITestButton() {
  var aiTestBtn = document.querySelector('[data-ai-test-btn]');
  if (!aiTestBtn) return;
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
