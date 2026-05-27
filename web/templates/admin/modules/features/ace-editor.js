import { safeRead, safeWrite } from '../core/storage.js';
import { createI18n } from '../core/i18n.js';

const sbT = createI18n((typeof window !== 'undefined' && window.__sbI18n) || {});

var aiButtonCallback = null;

export function setAIButtonCallback(fn) {
  aiButtonCallback = fn;
}

export function initAceEditors() {
  var codeTargets = document.querySelectorAll('[data-code-editor]');
  var aceEditors = [];
  var aceReadyResolve = null;
  var aceReady = new Promise(function (res) { aceReadyResolve = res; });

  if (codeTargets.length > 0) {
    var aceBase = (window.__sbRoot || '') + '/admin/static/ace/';
    loadScript(aceBase + 'ace.js').then(function () {
      if (!window.ace) { aceReadyResolve(false); return; }
      window.ace.config.set('basePath', aceBase);
      codeTargets.forEach(function (textarea) {
        upgradeTextareaToAce(textarea, aceEditors);
      });
      aceReadyResolve(true);
    }).catch(function () {
      console.warn('ace editor failed to load; falling back to plain textareas');
      aceReadyResolve(false);
    });
  } else {
    aceReadyResolve(false);
  }

  aceReady.then(function (loaded) {
    if (!loaded) return;
    document.querySelectorAll('select[data-code-editor-format]').forEach(function (sel) {
      sel.addEventListener('change', function () {
        var mode = aceModeForFormat(sel.value);
        var scope = sel.closest('form') || document;
        scope.querySelectorAll('textarea[data-code-editor-dynamic]').forEach(function (ta) {
          if (ta.__aceEditor) ta.__aceEditor.session.setMode('ace/mode/' + mode);
        });
        var form = sel.closest('form[data-entry-form]');
        if (form) safeWrite('sb_admin_entry_format', sel.value);
      });
    });
    applyStoredEntryFormatDefault();
    document.querySelectorAll('details').forEach(function (details) {
      details.addEventListener('toggle', function () {
        if (!details.open) return;
        details.querySelectorAll('textarea').forEach(function (ta) {
          if (ta.__aceEditor) ta.__aceEditor.resize(true);
        });
      });
    });
  });

  return {
    ready: aceReady,
    editors: aceEditors,
    getEditorForTextarea: function (textarea) {
      return textarea && textarea.__aceEditor || null;
    },
  };
}

function loadScript(src) {
  return new Promise(function (resolve, reject) {
    var existing = document.querySelector('script[data-src="' + src + '"]');
    if (existing) { resolve(); return; }
    var s = document.createElement('script');
    s.src = src;
    s.setAttribute('data-src', src);
    s.onload = function () { resolve(); };
    s.onerror = function () { reject(new Error('load failed: ' + src)); };
    document.head.appendChild(s);
  });
}

function upgradeTextareaToAce(textarea, aceEditors) {
  var mode = textarea.getAttribute('data-code-editor') || 'text';
  var rows = parseInt(textarea.getAttribute('rows') || '14', 10);
  var defaultHeight = Math.max(rows * 20, 240);
  var storedHeight = parseInt(safeRead('sb_admin_editor_height') || '', 10);
  var initialHeight = isFinite(storedHeight) && storedHeight >= 120 ? storedHeight : defaultHeight;

  var chrome = document.createElement('div');
  chrome.className = 'ace-chrome';
  var aiChunk = '';
  if (window.__sbAIEnabled) {
    aiChunk =
      '<span class="ace-chrome-sep" aria-hidden="true"></span>' +
      '<button type="button" class="btn-icon" data-ace-ai="rewrite" title="' + sbT('js.ai.rewriteTitle') + '" aria-label="' + sbT('js.ai.rewriteTitle') + '">✎✨</button>' +
      '<button type="button" class="btn-icon" data-ace-ai="continue" title="' + sbT('js.ai.continueTitle') + '" aria-label="' + sbT('js.ai.continueTitle') + '">▶✨</button>' +
      '<button type="button" class="btn-icon" data-ace-ai="summarise" title="' + sbT('js.ai.summariseTitle') + '" aria-label="' + sbT('js.ai.summariseTitle') + '">Σ✨</button>';
  }
  chrome.innerHTML =
    '<div class="ace-chrome-toolbar" data-ace-toolbar>' +
      '<button type="button" class="btn-icon" data-ace-search-toggle title="' + sbT('js.ace.searchTitle') + '" aria-label="' + sbT('js.ace.searchAria') + '">🔍</button>' +
      aiChunk +
      '<span class="ace-chrome-spacer" aria-hidden="true"></span>' +
      '<button type="button" class="btn-icon" data-ace-fullscreen title="' + sbT('js.ace.fullscreenTitle') + '" aria-label="' + sbT('js.ace.fullscreenAria') + '">⛶</button>' +
    '</div>' +
    '<div class="ace-chrome-search" hidden data-ace-search>' +
      '<input type="search" class="ace-chrome-search-input" data-ace-search-input placeholder="' + sbT('js.ace.searchPlaceholder') + '">' +
      '<span class="ace-chrome-search-count" data-ace-search-count aria-live="polite"></span>' +
      '<button type="button" class="btn-icon" data-ace-search-prev title="' + sbT('js.ace.prev') + '" aria-label="' + sbT('js.ace.prev') + '">↑</button>' +
      '<button type="button" class="btn-icon" data-ace-search-next title="' + sbT('js.ace.nextTitle') + '" aria-label="' + sbT('js.ace.next') + '">↓</button>' +
      '<button type="button" class="btn-icon" data-ace-search-close title="' + sbT('js.action.close') + ' (Esc)" aria-label="' + sbT('js.action.close') + '">✕</button>' +
    '</div>';

  var wrap = document.createElement('div');
  wrap.className = 'ace-wrap';
  wrap.style.width = '100%';
  wrap.style.height = initialHeight + 'px';
  chrome.appendChild(wrap);

  var resizeHandle = document.createElement('div');
  resizeHandle.className = 'ace-chrome-resize';
  resizeHandle.setAttribute('data-ace-resize', '');
  resizeHandle.setAttribute('title', sbT('js.aria.resizeHandle'));
  chrome.appendChild(resizeHandle);

  textarea.parentNode.insertBefore(chrome, textarea);
  textarea.style.display = 'none';

  var editor = window.ace.edit(wrap);
  applyEditorMode(editor, mode);
  editor.setValue(textarea.value, -1);
  editor.setOptions({
    useSoftTabs: true,
    tabSize: 2,
    showPrintMargin: false,
    fontSize: 13,
    wrap: true,
    useWorker: false,
  });
  applyAceTheme(editor);

  editor.session.on('change', function () {
    textarea.value = editor.getValue();
  });
  var form = textarea.closest('form');
  if (form) {
    form.addEventListener('submit', function () {
      textarea.value = editor.getValue();
    });
  }
  textarea.__aceEditor = editor;
  textarea.__aceWrap = wrap;
  editor.__hostTextarea = textarea;
  aceEditors.push(editor);

  wireChrome(chrome, wrap, editor, aceEditors);
}

function applyEditorHeightToAll(h, exceptWrap, aceEditors) {
  if (!(h > 0)) return;
  aceEditors.forEach(function (ed) {
    var ta = ed.__hostTextarea;
    if (!ta) return;
    var w = ta.__aceWrap;
    if (!w || w === exceptWrap) return;
    w.style.height = h + 'px';
    ed.resize(true);
  });
}

function wireChrome(chrome, wrap, editor, aceEditors) {
  var searchBar = chrome.querySelector('[data-ace-search]');
  var searchInput = chrome.querySelector('[data-ace-search-input]');
  var searchCount = chrome.querySelector('[data-ace-search-count]');
  var toggleBtn = chrome.querySelector('[data-ace-search-toggle]');
  var prevBtn = chrome.querySelector('[data-ace-search-prev]');
  var nextBtn = chrome.querySelector('[data-ace-search-next]');
  var closeBtn = chrome.querySelector('[data-ace-search-close]');
  var fsBtn = chrome.querySelector('[data-ace-fullscreen]');
  var resize = chrome.querySelector('[data-ace-resize]');

  function openSearch() {
    if (!searchBar) return;
    searchBar.hidden = false;
    var sel = editor.getSelectedText();
    if (sel) searchInput.value = sel;
    searchInput.focus();
    searchInput.select();
    runSearch(false);
  }
  function closeSearch() {
    searchBar.hidden = true;
    editor.focus();
  }
  function runSearch(skipCurrent) {
    var needle = searchInput.value;
    if (!needle) { searchCount.textContent = ''; return; }
    var found = editor.find(needle, {
      backwards: false,
      wrap: true,
      caseSensitive: false,
      wholeWord: false,
      regExp: false,
      skipCurrent: !!skipCurrent,
    });
    searchCount.textContent = found ? '' : sbT('js.ace.noMatch');
  }
  function findNext() {
    if (!searchInput.value) return;
    editor.findNext();
  }
  function findPrev() {
    if (!searchInput.value) return;
    editor.findPrevious();
  }

  if (toggleBtn) toggleBtn.addEventListener('click', function () {
    if (searchBar.hidden) openSearch(); else closeSearch();
  });
  if (closeBtn) closeBtn.addEventListener('click', closeSearch);
  if (nextBtn) nextBtn.addEventListener('click', findNext);
  if (prevBtn) prevBtn.addEventListener('click', findPrev);
  if (searchInput) {
    searchInput.addEventListener('input', function () { runSearch(false); });
    searchInput.addEventListener('keydown', function (e) {
      if (e.key === 'Enter') {
        e.preventDefault();
        if (e.shiftKey) findPrev(); else findNext();
      } else if (e.key === 'Escape') {
        e.preventDefault();
        closeSearch();
      }
    });
  }

  editor.commands.addCommand({
    name: 'admin-find',
    bindKey: { win: 'Ctrl-F', mac: 'Cmd-F' },
    exec: openSearch,
    readOnly: true,
  });
  editor.commands.addCommand({
    name: 'admin-find-next',
    bindKey: { win: 'Ctrl-G', mac: 'Cmd-G' },
    exec: findNext,
    readOnly: true,
  });

  function toggleFullscreen() {
    chrome.classList.toggle('ace-chrome--fullscreen');
    editor.resize(true);
    if (chrome.classList.contains('ace-chrome--fullscreen')) {
      editor.focus();
    }
  }
  if (fsBtn) fsBtn.addEventListener('click', toggleFullscreen);
  editor.commands.addCommand({
    name: 'admin-fullscreen-exit',
    bindKey: { win: 'Esc', mac: 'Esc' },
    exec: function () {
      if (chrome.classList.contains('ace-chrome--fullscreen')) {
        chrome.classList.remove('ace-chrome--fullscreen');
        editor.resize(true);
      }
    },
  });

  var aiButtons = chrome.querySelectorAll('[data-ace-ai]');
  Array.prototype.forEach.call(aiButtons, function (btn) {
    btn.addEventListener('click', function () {
      if (aiButtonCallback) {
        aiButtonCallback(editor, btn, btn.getAttribute('data-ace-ai'));
      }
    });
  });

  if (resize) {
    var dragging = false;
    var startY = 0;
    var startH = 0;
    resize.addEventListener('pointerdown', function (e) {
      dragging = true;
      startY = e.clientY;
      startH = wrap.getBoundingClientRect().height;
      resize.setPointerCapture(e.pointerId);
      e.preventDefault();
    });
    resize.addEventListener('pointermove', function (e) {
      if (!dragging) return;
      var dy = e.clientY - startY;
      var next = Math.max(120, startH + dy);
      wrap.style.height = next + 'px';
      editor.resize(true);
    });
    resize.addEventListener('pointerup', function (e) {
      if (!dragging) return;
      dragging = false;
      try { resize.releasePointerCapture(e.pointerId); } catch (_) { /* ignore */ }
      var finalH = Math.round(wrap.getBoundingClientRect().height);
      safeWrite('sb_admin_editor_height', String(finalH));
      applyEditorHeightToAll(finalH, wrap, aceEditors);
    });
  }
}

var sbModeCtor = null;
var sbModePending = null;

function applyEditorMode(editor, mode) {
  if (mode !== 'sbtemplate') {
    editor.session.setMode('ace/mode/' + mode);
    return;
  }
  editor.session.setMode('ace/mode/html');
  ensureSBMode().then(function (Ctor) {
    if (!Ctor) return;
    editor.session.setMode(new Ctor());
  });
}

function ensureSBMode() {
  if (sbModeCtor) return Promise.resolve(sbModeCtor);
  if (sbModePending) return sbModePending;
  sbModePending = new Promise(function (resolve) {
    window.ace.config.loadModule('ace/mode/html', function () {
      try {
        var oop = window.ace.require('ace/lib/oop');
        var HtmlMode = window.ace.require('ace/mode/html').Mode;
        var HtmlHighlightRules = window.ace.require('ace/mode/html_highlight_rules').HtmlHighlightRules;

        function SBRules() {
          HtmlHighlightRules.call(this);
          var extra = [
            { token: 'sb_block', regex: /<!--\s*(?:BEGIN|END)\s+[\w.]+\s*-->/ },
            { token: 'sb_tag', regex: /\{-?[a-zA-Z_][a-zA-Z0-9_.-]*\}/ },
          ];
          for (var state in this.$rules) {
            if (Object.prototype.hasOwnProperty.call(this.$rules, state)) {
              this.$rules[state] = extra.concat(this.$rules[state]);
            }
          }
          this.normalizeRules();
        }
        oop.inherits(SBRules, HtmlHighlightRules);

        function Mode() {
          HtmlMode.call(this);
          this.HighlightRules = SBRules;
        }
        oop.inherits(Mode, HtmlMode);

        sbModeCtor = Mode;
        resolve(Mode);
      } catch (e) {
        console.warn('sbtemplate mode init failed', e);
        resolve(null);
      }
    });
  });
  return sbModePending;
}

function applyAceTheme(editor) {
  var theme = aceCurrentDark() ? 'ace/theme/solarized_dark' : 'ace/theme/solarized_light';
  editor.setTheme(theme);
}

function aceCurrentDark() {
  var t = document.documentElement.getAttribute('data-theme');
  if (t === 'dark') return true;
  if (t === 'light') return false;
  return window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches;
}

new MutationObserver(function () {
  var editors = document.querySelectorAll('[data-code-editor]');
  editors.forEach(function (ta) {
    if (ta.__aceEditor) applyAceTheme(ta.__aceEditor);
  });
}).observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });

if (window.matchMedia) {
  var mql = window.matchMedia('(prefers-color-scheme: dark)');
  var listener = function () {
    if (document.documentElement.getAttribute('data-theme') === 'auto') {
      var editors = document.querySelectorAll('[data-code-editor]');
      editors.forEach(function (ta) {
        if (ta.__aceEditor) applyAceTheme(ta.__aceEditor);
      });
    }
  };
  if (mql.addEventListener) mql.addEventListener('change', listener);
  else if (mql.addListener) mql.addListener(listener);
}

function applyStoredEntryFormatDefault() {
  var form = document.querySelector('form[data-entry-form]');
  if (!form) return;
  var action = form.getAttribute('action') || '';
  if (!/\/admin\/entries\/new$/.test(action)) return;
  var sel = form.querySelector('select[data-code-editor-format]');
  if (!sel) return;
  var stored = safeRead('sb_admin_entry_format');
  if (!stored || stored === sel.value) return;
  var valid = false;
  for (var i = 0; i < sel.options.length; i++) {
    if (sel.options[i].value === stored) { valid = true; break; }
  }
  if (!valid) return;
  sel.value = stored;
  sel.dispatchEvent(new Event('change', { bubbles: true }));
}

function aceModeForFormat(value) {
  if (value === 'markdown') return 'markdown';
  if (value === 'sbtext') return 'text';
  return 'html';
}
