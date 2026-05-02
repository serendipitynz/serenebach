// Tiny admin.js: mobile drawer + image upload drop-zone + editor insert.
// Keep vanilla and free of build steps so "drop the binary in" still
// works end-to-end without a bundler.

(function () {
  'use strict';

  // ---- flash alert → toast promotion ---------------------------------
  // Server-rendered flash messages (<p class="alert success|error">)
  // get promoted to a bottom-center toast so the save-confirmation
  // pattern matches the rest of the admin UI. Success alerts are
  // ephemeral and get removed from the DOM after promotion; error
  // alerts stay inline so form-validation text remains readable after
  // the toast fades. .alert.warning (e.g. analytics disabled) is
  // persistent info and deliberately left alone.
  document.querySelectorAll('.alert.success').forEach(function (el) {
    var msg = (el.textContent || '').trim();
    if (msg) showToast(msg);
    el.remove();
  });
  document.querySelectorAll('.alert.error').forEach(function (el) {
    var msg = (el.textContent || '').trim();
    if (msg) showToast(msg, 'error');
  });

  // ---- appearance / language prefs -----------------------------------
  // Reflect the stored appearance preference into the <select> and apply
  // any change immediately. The pre-init script in layout.html already
  // set data-theme before CSS painted; this keeps the two in sync.
  var appearanceSelect = document.querySelector('[data-appearance-select]');
  if (appearanceSelect) {
    var stored = safeRead('sb_admin_appearance') || 'auto';
    appearanceSelect.value = stored;
    appearanceSelect.addEventListener('change', function () {
      var v = appearanceSelect.value;
      if (v !== 'light' && v !== 'dark' && v !== 'auto') return;
      safeWrite('sb_admin_appearance', v);
      document.documentElement.setAttribute('data-theme', v);
    });
  }
  var languageSelect = document.querySelector('[data-language-select]');
  if (languageSelect) {
    // The server renders <option selected> via {{Locale}}, so the
    // dropdown is already correct on first paint. No client-side
    // state restoration is needed — and it would not work under
    // Sakura's ENC_ cookie protection anyway (the value is encrypted
    // opaque to JS).
    languageSelect.addEventListener('change', function () {
      var v = languageSelect.value;
      if (v !== 'ja' && v !== 'en') return;
      var body = new URLSearchParams({ lang: v, csrf_token: readCSRFToken() });
      var endpoint = (window.__sbRoot || '') + '/admin/settings/language';
      fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: body,
        credentials: 'same-origin',
      }).then(function (res) {
        if (res.ok) window.location.reload();
      });
    });
  }

  function safeRead(k) { try { return localStorage.getItem(k); } catch (e) { return null; } }
  function safeWrite(k, v) { try { localStorage.setItem(k, v); } catch (e) { /* ignore */ } }

  // sbT looks up a key in the per-request i18n bundle layout.html
  // dropped into window.__sbI18n. If the key is missing the literal
  // key comes back so missing strings surface visibly. sbT supports
  // printf-style %d / %s substitutions so the caller can pass
  // additional args after the key.
  function sbT(key) {
    var bundle = (typeof window !== 'undefined' && window.__sbI18n) || {};
    var tmpl = bundle[key] || key;
    if (arguments.length <= 1) return tmpl;
    var args = Array.prototype.slice.call(arguments, 1);
    var i = 0;
    return tmpl.replace(/%[ds]/g, function () {
      var v = args[i++];
      return v === undefined ? '' : String(v);
    });
  }

  // ---- Ace code editor (lazy-loaded) ----------------------------------
  // Any <textarea data-code-editor="html|css|markdown|text"> on the
  // page gets upgraded to an Ace editor. Ace itself is only fetched
  // when at least one such textarea exists — most admin pages don't
  // pay that cost. Theme follows the admin appearance pref (Solarized
  // light / dark) and switches live when the user toggles.
  var codeTargets = document.querySelectorAll('[data-code-editor]');
  var aceEditors = [];
  var aceReadyResolve = null;
  var aceReady = new Promise(function (res) { aceReadyResolve = res; });
  if (codeTargets.length > 0) {
    loadScript('/admin/static/ace/ace.js').then(function () {
      if (!window.ace) { aceReadyResolve(false); return; }
      window.ace.config.set('basePath', '/admin/static/ace/');
      codeTargets.forEach(upgradeTextareaToAce);
      aceReadyResolve(true);
    }).catch(function () {
      // Ace failed to load — textareas keep working as-is, just
      // without highlighting.
      console.warn('ace editor failed to load; falling back to plain textareas');
      aceReadyResolve(false);
    });
  } else {
    aceReadyResolve(false);
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
  function upgradeTextareaToAce(textarea) {
    var mode = textarea.getAttribute('data-code-editor') || 'text';
    var rows = parseInt(textarea.getAttribute('rows') || '14', 10);
    var defaultHeight = Math.max(rows * 20, 240);
    // Persisted height applies to every editor uniformly — matches the
    // user's "全てのエディタに適用して問題ありません" directive and keeps
    // the storage surface to a single key.
    var storedHeight = parseInt(safeRead('sb_admin_editor_height') || '', 10);
    var initialHeight = isFinite(storedHeight) && storedHeight >= 120 ? storedHeight : defaultHeight;

    // Chrome wrapper: toolbar + search bar + editor canvas + resize
    // handle. Inserted in place of the textarea so every admin editor
    // picks up the same affordances (search, fullscreen, resize).
    var chrome = document.createElement('div');
    chrome.className = 'ace-chrome';
    // The AI assist buttons only render when the signed-in user has
    // a provider configured on /admin/settings/ai — layout.html
    // flags this via window.__sbAIEnabled. Turning AI off in
    // settings hides the buttons entirely instead of leaving them
    // visible but guaranteed to fail.
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
    });
    applyAceTheme(editor);

    // Keep textarea in sync so the form POST always carries the
    // latest content — CodeMirror-era editors had famously flaky
    // "submit-before-blur" edge cases; this pattern dodges them.
    editor.session.on('change', function () {
      textarea.value = editor.getValue();
    });
    var form = textarea.closest('form');
    if (form) {
      form.addEventListener('submit', function () {
        textarea.value = editor.getValue();
      });
    }
    // Cross-references used by the image picker and format-select live
    // swap: pick up the editor from the textarea without re-querying.
    textarea.__aceEditor = editor;
    textarea.__aceWrap = wrap;
    editor.__hostTextarea = textarea;
    aceEditors.push(editor);

    wireChrome(chrome, wrap, editor);
  }

  // applyEditorHeightToAll sets every other Ace editor on the page
  // to the given pixel height so the user doesn't end up with two
  // editors at different sizes on the same screen (e.g. body vs
  // 追記 on the entry form). Skips the editor the resize originated
  // from — that one is already at `h`.
  function applyEditorHeightToAll(h, exceptWrap) {
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

  // ---- editor chrome: search / fullscreen / resize --------------------

  // wireChrome wires the toolbar buttons, search bar, resize handle,
  // and fullscreen class toggle onto one editor instance. Kept in a
  // single function so each editor owns its own little event scope —
  // multiple editors per page work without leaking state.
  function wireChrome(chrome, wrap, editor) {
    var searchBar = chrome.querySelector('[data-ace-search]');
    var searchInput = chrome.querySelector('[data-ace-search-input]');
    var searchCount = chrome.querySelector('[data-ace-search-count]');
    var toggleBtn = chrome.querySelector('[data-ace-search-toggle]');
    var prevBtn = chrome.querySelector('[data-ace-search-prev]');
    var nextBtn = chrome.querySelector('[data-ace-search-next]');
    var closeBtn = chrome.querySelector('[data-ace-search-close]');
    var fsBtn = chrome.querySelector('[data-ace-fullscreen]');
    var resize = chrome.querySelector('[data-ace-resize]');

    // --- search ---
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

    // Cmd/Ctrl+F inside the editor opens the inline search bar
    // instead of the browser's. Ace already captures these keys, so
    // we bind the command on the editor — not window — to avoid
    // stealing the shortcut when focus is elsewhere on the page.
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

    // --- fullscreen ---
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

    // --- AI writing assist ---
    // Toolbar buttons dispatch to POST /admin/ai/compose. Each action
    // picks up context differently: rewrite + summarise require a
    // selection, continue uses text before the cursor (or the whole
    // buffer). A spinner state on the button + a toast handle the
    // wait; a failure surfaces the server-returned i18n key.
    var aiButtons = chrome.querySelectorAll('[data-ace-ai]');
    Array.prototype.forEach.call(aiButtons, function (btn) {
      btn.addEventListener('click', function () {
        runAceAI(editor, btn, btn.getAttribute('data-ace-ai'));
      });
    });

    // --- vertical resize handle ---
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
        // Persist the final height globally so every other editor
        // mount (this session or future) opens at the same size.
        // Same-page editors sync immediately so the user's next
        // glance at another editor isn't out of step.
        var finalH = Math.round(wrap.getBoundingClientRect().height);
        safeWrite('sb_admin_editor_height', String(finalH));
        applyEditorHeightToAll(finalH, wrap);
      });
    }
  }

  // ---- sbtemplate Ace mode --------------------------------------------
  //
  // applyEditorMode resolves a data-code-editor value to the right Ace
  // mode. "sbtemplate" is synthesised on the fly: we load the stock
  // HtmlMode + its highlight rules, subclass the rules to recognise
  // SB's `{tag}` and `<!-- BEGIN/END name -->` constructs, and hand a
  // concrete Mode instance to setMode. Registering via ace.define
  // doesn't work here — Ace still tries to net-fetch
  // mode-sbtemplate.js from basePath, which 404s to text/plain and
  // triggers a strict-MIME block in the browser.

  var sbModeCtor = null;      // cached once mode-html.js finishes loading
  var sbModePending = null;   // promise in flight while we wait

  function applyEditorMode(editor, mode) {
    if (mode !== 'sbtemplate') {
      editor.session.setMode('ace/mode/' + mode);
      return;
    }
    // Start the editor on plain HTML highlighting so the user sees
    // colour immediately, then upgrade to the SB-extended rules once
    // HtmlMode has been pulled in via the mode-html.js chunk.
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
    // auto — follow the OS setting via matchMedia.
    return window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches;
  }
  // React to theme changes (the appearance <select> in /admin/settings
  // mutates data-theme on <html>; we swap every mounted editor's theme
  // to match).
  new MutationObserver(function () {
    aceEditors.forEach(applyAceTheme);
  }).observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
  if (window.matchMedia) {
    var mql = window.matchMedia('(prefers-color-scheme: dark)');
    var listener = function () {
      if (document.documentElement.getAttribute('data-theme') === 'auto') {
        aceEditors.forEach(applyAceTheme);
      }
    };
    if (mql.addEventListener) mql.addEventListener('change', listener);
    else if (mql.addListener) mql.addListener(listener);
  }

  // ---- modal -----------------------------------------------------------
  // Single host element shared across every page. Callers pass the body
  // HTML or a DOM node + optional title / footer buttons. Close actions
  // work via the X button, backdrop click, or ESC key — all three funnel
  // through closeModal() so state stays consistent.
  var modalHost = document.querySelector('[data-modal-host]');
  var modalTitle = modalHost && modalHost.querySelector('[data-modal-title]');
  var modalBody = modalHost && modalHost.querySelector('[data-modal-body]');
  var modalFoot = modalHost && modalHost.querySelector('[data-modal-foot]');
  var modalDialog = modalHost && modalHost.querySelector('.modal');
  var modalLastFocus = null;

  function openModal(opts) {
    if (!modalHost) return;
    modalLastFocus = document.activeElement;
    modalTitle.textContent = opts.title || '';
    modalBody.innerHTML = '';
    if (opts.bodyNode) modalBody.appendChild(opts.bodyNode);
    else if (typeof opts.bodyHTML === 'string') modalBody.innerHTML = opts.bodyHTML;
    else if (typeof opts.bodyText === 'string') modalBody.textContent = opts.bodyText;
    modalDialog.className = 'modal' + (opts.variant ? ' modal-' + opts.variant : '');
    modalFoot.innerHTML = '';
    if (opts.footerNode) {
      modalFoot.appendChild(opts.footerNode);
      modalFoot.hidden = false;
    } else {
      modalFoot.hidden = true;
    }
    modalHost.hidden = false;
    // Focus the dialog so keyboard users land inside the modal; ESC
    // captured by the document-level listener.
    setTimeout(function () { try { modalDialog.focus(); } catch (e) {} }, 0);
  }
  function closeModal() {
    if (!modalHost) return;
    modalHost.hidden = true;
    modalBody.innerHTML = '';
    modalFoot.innerHTML = '';
    if (modalLastFocus && modalLastFocus.focus) {
      try { modalLastFocus.focus(); } catch (e) {}
    }
  }
  if (modalHost) {
    modalHost.addEventListener('click', function (e) {
      if (e.target === modalHost) closeModal();
    });
    var closeBtn = modalHost.querySelector('[data-modal-close]');
    if (closeBtn) closeBtn.addEventListener('click', closeModal);
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && !modalHost.hidden) closeModal();
    });
  }
  // Expose for other code in this file (image click, comment modal, etc).
  window.__sbAdminModal = { open: openModal, close: closeModal };

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
        form.submit();
      });
      footer.appendChild(cancel);
      footer.appendChild(ok);

      openModal({ title: sbT('js.action.saveAs'), bodyNode: wrap, footerNode: footer });
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

  // ---- image preview (gallery tiles + list rows) ----------------------
  // Both the tile and list-table rows carry data-image-url / data-image-
  // alt so the same click handler opens a fit-to-viewport preview modal.
  document.querySelectorAll('[data-image-url]').forEach(function (host) {
    var url = host.getAttribute('data-image-url');
    var alt = host.getAttribute('data-image-alt') || '';
    if (!url) return;
    // In the grid view the clickable target is the <figure>; in the
    // list view it's the row's thumbnail image. Bind to the element
    // that already has a zoom-in cursor when available, else fall
    // back to the host itself.
    var trigger = host.querySelector('figure') || host.querySelector('.image-row-icon') || host;
    trigger.style.cursor = 'zoom-in';
    trigger.addEventListener('click', function (e) {
      if (e.target.closest('form, a, button')) return;
      e.preventDefault();
      var img = document.createElement('img');
      img.src = url;
      img.alt = alt;
      openModal({ title: alt || sbT('js.modal.image'), variant: 'image', bodyNode: img });
    });
  });

  // ---- image library view toggle persistence --------------------------
  // The <a class="view-btn"> links already carry the URL the server
  // needs (?view=grid|list). Write that choice to localStorage so the
  // pre-init script in layout.html can restore it on the next visit.
  document.querySelectorAll('.view-toggle .view-btn').forEach(function (a) {
    a.addEventListener('click', function () {
      var m = (a.getAttribute('href') || '').match(/[?&]view=(\w+)/);
      if (m) safeWrite('sb_admin_image_view', m[1]);
    });
  });

  // ---- image filename filter (gallery + list) -------------------------
  // Client-side filter; the input lives in .library-controls and
  // narrows whichever filterable container is on the page. Grid hides
  // non-matching <li> tiles; list hides non-matching <tr> rows.
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

  // ---- mobile drawer ----------------------------------------------------
  // Burger toggles the sidebar on phone-class viewports. Clicking a
  // sidebar link closes it automatically (the navigation answers the
  // "why did I open this?"), and tapping the backdrop or pressing
  // Escape gives a cancel path — both are touch-critical on mobile
  // where there's no easy way back to the burger button itself.
  var burger = document.querySelector('[data-toggle-nav]');
  if (burger) {
    burger.addEventListener('click', function (e) {
      e.stopPropagation();
      document.body.classList.toggle('nav-open');
    });
    var links = document.querySelectorAll('.sidebar a');
    for (var i = 0; i < links.length; i++) {
      links[i].addEventListener('click', function () {
        document.body.classList.remove('nav-open');
      });
    }
    document.addEventListener('click', function (e) {
      if (!document.body.classList.contains('nav-open')) return;
      if (e.target.closest && e.target.closest('.sidebar')) return;
      document.body.classList.remove('nav-open');
    });
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && document.body.classList.contains('nav-open')) {
        document.body.classList.remove('nav-open');
      }
    });
  }

  // ---- helpers ----------------------------------------------------------
  function csrfTokenFrom(el) {
    return (el.closest('[data-csrf]') || document.querySelector('[data-csrf]') ||
      document.querySelector('[data-upload]') || document.body).getAttribute('data-csrf') || '';
  }

  function uploadFile(file, csrfToken, endpoint) {
    // endpoint defaults to the image upload endpoint so the editor's
    // drop-onto-textarea paths (which pass no endpoint) keep working.
    var url = endpoint || '/admin/images';
    var fd = new FormData();
    fd.append('file', file);
    fd.append('csrf_token', csrfToken);
    return fetch(url, {
      method: 'POST',
      body: fd,
      headers: {
        'Accept': 'application/json',
        'X-CSRF-Token': csrfToken
      },
      credentials: 'same-origin'
    }).then(function (res) {
      return res.json().then(function (json) {
        return { ok: res.ok, status: res.status, body: json };
      }).catch(function () {
        // Non-JSON error body (HTML 5xx page). Surface the status so the
        // caller can still report something useful.
        return { ok: res.ok, status: res.status, body: {} };
      });
    });
  }

  // ---- drop zone on /admin/images --------------------------------------
  var dropForms = document.querySelectorAll('[data-upload]');
  dropForms.forEach(function (form) {
    var zone = form.querySelector('[data-drop-zone]');
    var input = form.querySelector('[data-drop-input]');
    var progress = form.querySelector('.drop-zone-progress');
    if (!zone || !input) return;

    ['dragenter', 'dragover'].forEach(function (evt) {
      zone.addEventListener(evt, function (e) {
        e.preventDefault(); e.stopPropagation();
        zone.classList.add('drag-over');
      });
    });
    ['dragleave', 'drop'].forEach(function (evt) {
      zone.addEventListener(evt, function (e) {
        e.preventDefault(); e.stopPropagation();
        zone.classList.remove('drag-over');
      });
    });
    zone.addEventListener('drop', function (e) {
      var files = e.dataTransfer && e.dataTransfer.files;
      if (!files || !files.length) return;
      submitFiles(files);
    });
    input.addEventListener('change', function () {
      if (!input.files || !input.files.length) return;
      submitFiles(input.files);
    });

    function submitFiles(files) {
      var token = form.getAttribute('data-csrf') || '';
      var endpoint = form.getAttribute('action') || '/admin/images';
      var total = files.length;
      var done = 0;
      var errors = [];
      if (progress) { progress.hidden = false; progress.textContent = sbT('js.upload.uploading', 0, total); }

      var chain = Promise.resolve();
      // Collect ids whose upload response asked for auto-alt. We'll
      // hit /admin/images/{id}/alt for each in parallel after the
      // upload pass finishes, keeping the upload latency unchanged.
      var altPending = [];
      for (var i = 0; i < files.length; i++) {
        (function (file) {
          chain = chain.then(function () {
            return uploadFile(file, token, endpoint).then(function (result) {
              done += 1;
              if (!result.ok) {
                errors.push((file.name || 'file') + ': ' + (result.body && result.body.error || ('HTTP ' + result.status)));
              } else if (result.body && result.body.auto_alt_requested && result.body.id) {
                altPending.push(result.body.id);
              }
              if (progress) progress.textContent = sbT('js.upload.uploading', done, total);
            });
          });
        })(files[i]);
      }
      chain.then(function () {
        if (errors.length) { alert(errors.join('\n')); }
        if (altPending.length === 0) {
          window.location.reload();
          return;
        }
        // Auto-alt run: surface the "generating..." state so the user
        // knows the server is still working, then reload once every
        // alt call has returned (success or failure).
        if (progress) progress.textContent = sbT('js.ai.altGenerating', altPending.length);
        var altPromises = altPending.map(function (id) {
          return fetch('/admin/images/' + id + '/alt', {
            method: 'POST',
            headers: { 'X-CSRF-Token': token, 'Accept': 'application/json' },
            credentials: 'same-origin'
          }).then(function (res) { return res.json().catch(function () { return { ok: false }; }); })
            .catch(function () { return { ok: false }; });
        });
        Promise.all(altPromises).then(function (results) {
          var failed = results.filter(function (r) { return !r.ok; }).length;
          if (failed > 0) {
            showToast(sbT('js.ai.altFail', failed));
          } else {
            showToast(sbT('js.ai.altDone', results.length));
          }
          window.location.reload();
        });
      });
    }
  });

  // ---- copy-URL buttons (gallery) --------------------------------------
  // On success: flash a checkmark inside icon-style buttons (or swap
  // the text on link-style buttons) AND raise a small toast so the
  // confirmation is visible even when the cursor has already moved on.
  var checkSVG = '<svg class="icon" viewBox="0 0 20 20" aria-hidden="true" focusable="false"><path d="M4 10.5l4 4L16 6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>';

  document.querySelectorAll('[data-copy-url]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var url = btn.getAttribute('data-copy-url') || '';
      if (!url) return;
      var abs = new URL(url, window.location.origin).toString();
      copyViaClipboard(abs, btn);
    });
  });

  // [data-copy-text] copies the attribute value verbatim — used for
  // literal tokens like `{site_parts}filename` that shouldn't go
  // through URL resolution.
  document.querySelectorAll('[data-copy-text]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var text = btn.getAttribute('data-copy-text') || '';
      if (!text) return;
      copyViaClipboard(text, btn);
    });
  });

  function copyViaClipboard(text, btn) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(function () {
        onCopySuccess(btn);
      });
    } else {
      // Fallback for older browsers: select a hidden text node.
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
  function showToast(msg, variant) {
    var el = document.createElement('div');
    el.className = variant === 'error' ? 'toast error' : 'toast';
    el.textContent = msg;
    document.body.appendChild(el);
    // Trigger the transition: add .visible on the next frame so the
    // initial transform/opacity apply before the transition starts.
    requestAnimationFrame(function () { el.classList.add('visible'); });
    // Dwell time scales with message length so long error messages
    // (e.g. the reasoning-exhausted hint) are actually readable. Base
    // values stay unchanged for short messages; the extra ~50 ms per
    // character above ~20 chars lifts a 100-char error from 3.0 s to
    // ~7.0 s without dragging short toasts off-screen.
    var len = (msg || '').length;
    var base = variant === 'error' ? 3000 : 1800;
    var perChar = variant === 'error' ? 60 : 40;
    var dwell = base + Math.max(0, len - 20) * perChar;
    var ceiling = variant === 'error' ? 12000 : 6000;
    if (dwell > ceiling) dwell = ceiling;
    setTimeout(function () {
      el.classList.remove('visible');
      setTimeout(function () { el.remove(); }, 200);
    }, dwell);
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

  // ---- image picker + drop-to-insert (shared across forms) -------------
  // Insertion targets are any textarea marked `data-image-target` — the
  // entry form flags body/more, the profile + category forms flag their
  // description textarea. Picker shell (`[data-image-picker-open]` +
  // `[data-image-picker]`) lives once per page; lastFocusedTextarea
  // remembers where to insert when the user clicks a picker tile.
  var picker = document.querySelector('[data-image-picker]');
  var pickerOpen = document.querySelector('[data-image-picker-open]');
  var pickerBody = picker ? picker.querySelector('[data-image-picker-body]') : null;
  var imageTargets = Array.prototype.slice.call(document.querySelectorAll('textarea[data-image-target]'));

  var lastFocusedTextarea = imageTargets[0] || null;
  imageTargets.forEach(function (ta) {
    ta.addEventListener('focus', function () { lastFocusedTextarea = ta; });
  });

  // After Ace finishes mounting, layer rich-editor wiring on top:
  //   - track focus inside Ace so the picker inserts into the right editor
  //   - live-swap Ace mode when any `[data-code-editor-format]` select
  //     changes — mode updates are scoped to the select's form so
  //     sibling forms don't interfere.
  //   - resize the entry-form 追記 editor when <details> opens (Ace
  //     computes layout as 0×0 while its container is display:none).
  aceReady.then(function (loaded) {
    if (!loaded) return;
    imageTargets.forEach(function (ta) {
      if (ta.__aceEditor) ta.__aceEditor.on('focus', function () { lastFocusedTextarea = ta; });
    });
    document.querySelectorAll('select[data-code-editor-format]').forEach(function (sel) {
      sel.addEventListener('change', function () {
        var mode = aceModeForFormat(sel.value);
        var scope = sel.closest('form') || document;
        scope.querySelectorAll('textarea[data-code-editor-dynamic]').forEach(function (ta) {
          if (ta.__aceEditor) ta.__aceEditor.session.setMode('ace/mode/' + mode);
        });
        // Persist entry-form format picks so the next 新規記事
        // opens in the user's preferred format by default.
        var form = sel.closest('form[data-entry-form]');
        if (form) safeWrite('sb_admin_entry_format', sel.value);
      });
    });
    // On the 新規記事 form, restore the last-used format pick from
    // localStorage and fire a synthetic change so the Ace mode swap
    // handler above runs. We scope by the form action — only the
    // "/admin/entries/new" endpoint treats the stored value as the
    // default; editing an existing entry keeps the saved format.
    applyStoredEntryFormatDefault();
    // Any <details> containing Ace-backed textareas needs a resize()
    // call on open, since Ace computes layout as 0×0 while the
    // <details> is closed (display:none on the child wrapper). Covers
    // both the entry-form 追記 panel and the template-form
    // 個別記事用 HTML collapsible.
    document.querySelectorAll('details').forEach(function (details) {
      details.addEventListener('toggle', function () {
        if (!details.open) return;
        details.querySelectorAll('textarea').forEach(function (ta) {
          if (ta.__aceEditor) ta.__aceEditor.resize(true);
        });
      });
    });
  });

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

  // ---- OG background picker --------------------------------------------
  // Reuses the image-picker modal so the operator doesn't learn a new
  // control. [data-og-bg-picker] buttons opt into "select mode": the
  // next tile click stores the image's stored_path into a companion
  // [data-og-bg-input] hidden input, shows a preview in
  // [data-og-bg-preview], and closes the modal — no insertion into a
  // body textarea. [data-og-bg-clear] wipes the same triplet.
  var ogBgTargetInput = null;
  var ogBgTargetPreview = null;
  function ogBgFieldset(btn) {
    var wrap = btn.closest('[data-og-bg-field]') || document;
    return {
      input: wrap.querySelector('[data-og-bg-input]'),
      preview: wrap.querySelector('[data-og-bg-preview]')
    };
  }
  function applyOGBGPick(img) {
    if (!ogBgTargetInput) return;
    // img.stored_path is `2024/12/file.jpg`; strip the /img/ prefix
    // off url as the fallback path if the server didn't include it.
    var stored = img.stored_path || (img.url || '').replace(/^\/img\//, '');
    ogBgTargetInput.value = stored;
    if (ogBgTargetPreview) {
      ogBgTargetPreview.src = img.url || ('/img/' + stored);
      ogBgTargetPreview.hidden = false;
    }
    ogBgTargetInput = null;
    ogBgTargetPreview = null;
    if (picker) picker.hidden = true;
  }
  document.querySelectorAll('[data-og-bg-picker]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var fs = ogBgFieldset(btn);
      // Settings page doesn't ship the [data-image-picker-open] button
      // the entry form does — guarding on `picker` alone is enough.
      if (!fs.input || !picker) return;
      ogBgTargetInput = fs.input;
      ogBgTargetPreview = fs.preview;
      picker.hidden = false;
      if (!picker.__loaded) {
        loadPickerImages();
        picker.__loaded = true;
      }
    });
  });
  document.querySelectorAll('[data-og-bg-clear]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var fs = ogBgFieldset(btn);
      if (fs.input) fs.input.value = '';
      if (fs.preview) { fs.preview.hidden = true; fs.preview.src = ''; }
    });
  });

  // OG text-color controls — checkbox toggles picker enable state,
  // Clear wipes both and flips the unset marker so the server stores
  // empty (== "use defaults") rather than whatever hex the picker
  // happens to hold. Any interaction with the picker or checkbox
  // re-arms the unset marker to 0 so a subsequent save persists the
  // new choice.
  document.querySelectorAll('[data-og-text-color-field]').forEach(function (field) {
    var picker = field.querySelector('[data-og-text-color-picker]');
    var transparent = field.querySelector('[data-og-text-transparent]');
    var unset = field.querySelector('[data-og-text-color-unset]');
    var clear = field.querySelector('[data-og-text-color-clear]');
    function armUnset(v) { if (unset) unset.value = v ? '1' : '0'; }
    if (transparent) {
      transparent.addEventListener('change', function () {
        if (picker) picker.disabled = transparent.checked;
        armUnset(false);
      });
    }
    if (picker) {
      picker.addEventListener('input', function () { armUnset(false); });
    }
    if (clear) {
      clear.addEventListener('click', function () {
        if (transparent) transparent.checked = false;
        if (picker) { picker.disabled = false; picker.value = '#475569'; }
        armUnset(true);
      });
    }
  });

  if (picker && pickerOpen && pickerBody) {
    var loaded = false;
    pickerOpen.addEventListener('click', function () {
      picker.hidden = !picker.hidden;
      if (!picker.hidden && !loaded) {
        loadPickerImages();
        loaded = true;
      }
    });
  }

  // Cached picker items so the filter input can narrow without
  // re-fetching. Populated on first picker open.
  var pickerItems = null;

  function loadPickerImages() {
    if (!pickerBody) return;
    pickerBody.textContent = sbT('js.picker.loading');
    fetch('/admin/images?format=json', {
      headers: { 'Accept': 'application/json' },
      credentials: 'same-origin'
    }).then(function (res) { return res.json(); })
      .then(function (payload) {
        pickerItems = (payload && payload.images) || [];
        renderPickerItems(pickerItems);
      })
      .catch(function () { pickerBody.textContent = sbT('js.picker.loadError'); });
  }
  function renderPickerItems(items) {
    pickerBody.textContent = '';
    if (!items.length) { pickerBody.textContent = sbT('js.picker.empty'); return; }
    var ul = document.createElement('ul');
    ul.className = 'image-gallery';
    items.forEach(function (img) {
      var li = document.createElement('li');
      li.className = 'image-tile';
      var image = document.createElement('img');
      image.src = img.thumb_url || img.url;
      image.alt = img.filename || '';
      image.draggable = true;
      image.dataset.fullUrl = img.url;
      image.dataset.filename = img.filename || '';
      image.addEventListener('click', function () {
        // OG background picker divert: when the caller opened the
        // modal via [data-og-bg-picker] the next tile click fills the
        // bound input instead of inserting an <img> into a body
        // textarea. Single-shot — one pick per picker open.
        if (ogBgTargetInput) {
          applyOGBGPick(img);
          return;
        }
        insertImageMarkup(img.url, img.alt || img.filename || '');
      });
      image.addEventListener('dragstart', function (e) {
        if (!e.dataTransfer) return;
        e.dataTransfer.setData('text/uri-list', img.url);
        e.dataTransfer.setData('text/plain', img.url);
        e.dataTransfer.setData('application/x-sb-image', JSON.stringify({
          url: img.url, filename: img.filename || '', alt: img.alt || ''
        }));
        e.dataTransfer.effectAllowed = 'copy';
      });
      li.appendChild(image);
      ul.appendChild(li);
    });
    pickerBody.appendChild(ul);
  }

  // Wire the filter input once the picker exists in the DOM.
  var pickerFilter = picker && picker.querySelector('[data-image-picker-filter]');
  if (pickerFilter) {
    pickerFilter.addEventListener('input', function () {
      if (!pickerItems) return;
      var needle = pickerFilter.value.toLowerCase();
      if (needle === '') { renderPickerItems(pickerItems); return; }
      var filtered = pickerItems.filter(function (i) {
        return (i.filename || '').toLowerCase().indexOf(needle) !== -1;
      });
      renderPickerItems(filtered);
    });
  }

  function insertImageMarkup(url, alt) {
    // Markdown-aware insert: when the currently focused editor sits in
    // a form whose format select is "markdown", emit the Markdown img
    // syntax instead of raw HTML. Keeps copy-pasted descriptions
    // consistent with what the author sees rendered.
    var safeAlt = alt.replace(/"/g, '&quot;');
    var target = lastFocusedTextarea || imageTargets[0];
    if (!target) return;
    var tag = '<img src="' + url + '" alt="' + safeAlt + '">';
    var form = target.closest && target.closest('form');
    if (form) {
      var fmt = form.querySelector('select[data-code-editor-format]');
      if (fmt && fmt.value === 'markdown') {
        tag = '![' + alt + '](' + url + ')';
      }
    }
    if (target.__aceEditor) {
      target.__aceEditor.focus();
      target.__aceEditor.insert(tag);
      target.value = target.__aceEditor.getValue();
      return;
    }
    var start = target.selectionStart || 0;
    var end = target.selectionEnd || 0;
    target.value = target.value.slice(0, start) + tag + target.value.slice(end);
    target.focus();
    target.selectionStart = target.selectionEnd = start + tag.length;
  }

  // Drop onto any image-target textarea handles two sources:
  //   1. OS files  → upload + insert <img>
  //   2. Picker thumbnails (in-page drag) → just insert <img>
  //
  // When Ace is mounted the real drop surface is the .ace-wrap div (the
  // <textarea> itself is display:none), so we re-bind the same handlers
  // on the wrap once aceReady resolves.
  imageTargets.forEach(function (ta) {
    bindEntryAreaDrop(ta, ta);
  });
  aceReady.then(function (loaded) {
    if (!loaded) return;
    imageTargets.forEach(function (ta) {
      if (ta.__aceWrap) bindEntryAreaDrop(ta.__aceWrap, ta);
    });
  });

  function bindEntryAreaDrop(dropTarget, hostTextarea) {
    dropTarget.addEventListener('dragover', function (e) {
      if (!e.dataTransfer) return;
      if (hasFiles(e.dataTransfer) || hasDraggedImageURL(e.dataTransfer)) {
        e.preventDefault();
        dropTarget.classList.add('drag-over');
      }
    });
    dropTarget.addEventListener('dragleave', function () { dropTarget.classList.remove('drag-over'); });
    dropTarget.addEventListener('drop', function (e) {
      if (!e.dataTransfer) return;
      dropTarget.classList.remove('drag-over');
      lastFocusedTextarea = hostTextarea;

      // In-page drag from the picker (no File objects).
      var dt = e.dataTransfer;
      if (!(dt.files && dt.files.length)) {
        var payload = dt.getData('application/x-sb-image');
        if (payload) {
          e.preventDefault();
          try {
            var parsed = JSON.parse(payload);
            insertImageMarkup(parsed.url, parsed.alt || parsed.filename || '');
          } catch (err) { /* ignore malformed payload */ }
          return;
        }
        // Fallback: a URL-like drop (e.g. copy-paste from another tab).
        var url = dt.getData('text/uri-list') || dt.getData('text/plain');
        if (url && isLikelyImageURL(url)) {
          e.preventDefault();
          insertImageMarkup(url, '');
        }
        return;
      }

      // OS file drop: upload then insert.
      e.preventDefault();
      var token = csrfTokenFrom(hostTextarea);
      var files = dt.files;
      Array.prototype.slice.call(files).reduce(function (chain, file) {
        return chain.then(function () {
          return uploadFile(file, token).then(function (result) {
            if (result.ok && result.body && result.body.url) {
              insertImageMarkup(result.body.url, result.body.filename || '');
            } else {
              alert((file.name || 'file') + ': ' + (result.body && result.body.error || ('HTTP ' + result.status)));
            }
          });
        });
      }, Promise.resolve());
    });
  }

  function hasDraggedImageURL(dt) {
    if (!dt.types) return false;
    for (var i = 0; i < dt.types.length; i++) {
      var t = dt.types[i];
      if (t === 'application/x-sb-image' || t === 'text/uri-list') return true;
    }
    return false;
  }

  function isLikelyImageURL(s) {
    return /^\/?img\//.test(s) || /\.(png|jpe?g|gif|webp)(\?.*)?$/i.test(s);
  }

  // ---- sortable admin lists (drag-and-drop reorder) -------------------
  // Generic: any table tagged data-<kind>-sortable posts [ids] back to the
  // matching /admin/<kind>/reorder endpoint on drop. Rows supply their id
  // via data-<kind>-id and the whole <tr> is draggable. Keeps the category
  // list and the template list (and anything later) on one code path.
  initSortableList('category');
  initSortableList('template');
  initSortableList('user');
  initSortableList('link');

  // ---- link form: show/hide URI / target / group / disp when the
  // 種類 selector flips. Only renders on the NEW form (existing rows
  // lock their kind and skip the radios entirely).
  initLinkKindToggle();

  // ---- unsaved-change warning on admin edit forms --------------------
  // Any form tagged [data-unsaved-warn] gets a browser beforeunload
  // warning if its contents diverge from the initial snapshot. Covers
  // entry / category / link / template / user / profile editors. We
  // compare a string-keyed FormData snapshot (file inputs excluded,
  // since File objects don't serialise and their own pickers handle
  // unsaved state separately), and skip the prompt during legitimate
  // form submission.
  initUnsavedWarn();

  function initUnsavedWarn() {
    var forms = document.querySelectorAll('form[data-unsaved-warn]');
    if (!forms.length) return;
    var submitting = false;
    forms.forEach(function (form) {
      // Baseline snapshot is deferred until Ace has mounted AND any
      // programmatic field restoration has settled. Specifically: the
      // new-entry form applies a stored default フォーマット (markdown /
      // sbtext) after Ace resolves, which dispatches a synthetic
      // change event. If we snapshot before that fires the restored
      // value shows up as a user edit and beforeunload warns without
      // the user touching a thing. Hanging the snapshot off aceReady
      // covers both the code-editor pages and the simpler forms
      // (aceReady resolves immediately when no editor is on-page).
      var initial = null;
      aceReady.then(function () {
        // One more tick so any synchronous change-handlers queued off
        // the restoration step have fully propagated into the DOM
        // before we read it.
        setTimeout(function () { initial = snapshot(form); }, 0);
      });

      form.addEventListener('submit', function () { submitting = true; });

      // Any form on the page that posts (e.g. per-row delete forms in
      // a member-list panel below the main edit form) should also
      // clear the guard, otherwise the click triggers a redundant
      // prompt after the user already confirmed the delete.
      document.addEventListener('submit', function () { submitting = true; }, true);

      window.addEventListener('beforeunload', function (e) {
        if (submitting) return;
        if (!document.body.contains(form)) return;
        if (initial === null) return; // pre-baseline window; nothing to compare
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

  function initLinkKindToggle() {
    var form = document.querySelector('[data-link-form]');
    if (!form) return;
    var fields = form.querySelector('[data-link-fields]');
    if (!fields) return;
    var radios = form.querySelectorAll('[data-link-kind]');
    if (!radios.length) return;
    function sync() {
      var selected = form.querySelector('[data-link-kind]:checked');
      var isGroup = selected && selected.value === 'group';
      if (isGroup) fields.setAttribute('hidden', '');
      else fields.removeAttribute('hidden');
    }
    for (var i = 0; i < radios.length; i++) {
      radios[i].addEventListener('change', sync);
    }
    sync();
  }

  function initSortableList(kind) {
    var table = document.querySelector('[data-' + kind + '-sortable]');
    if (!table) return;
    var tbody = table.querySelector('tbody');
    var status = table.parentNode.querySelector('[data-reorder-status]');
    var token = table.getAttribute('data-csrf') || '';
    var idAttr = 'data-' + kind + '-id';
    var endpoint = '/admin/' + kind + 's/reorder';
    if (!tbody) return;

    var dragged = null;

    tbody.addEventListener('dragstart', function (e) {
      var row = closestRow(e.target);
      if (!row) return;
      dragged = row;
      row.classList.add('dragging');
      if (e.dataTransfer) {
        e.dataTransfer.effectAllowed = 'move';
        // Firefox needs some data set on the transfer to start a drag.
        e.dataTransfer.setData('text/plain', row.getAttribute(idAttr) || '');
      }
    });

    tbody.addEventListener('dragend', function () {
      if (dragged) dragged.classList.remove('dragging');
      clearDropMarkers();
      dragged = null;
    });

    tbody.addEventListener('dragover', function (e) {
      if (!dragged) return;
      e.preventDefault();
      var row = closestRow(e.target);
      if (!row || row === dragged) return;
      clearDropMarkers();
      if (insertBefore(e, row)) {
        row.classList.add('drop-above');
      } else {
        row.classList.add('drop-below');
      }
    });

    tbody.addEventListener('drop', function (e) {
      if (!dragged) return;
      e.preventDefault();
      var row = closestRow(e.target);
      clearDropMarkers();
      if (!row || row === dragged) return;
      if (insertBefore(e, row)) {
        tbody.insertBefore(dragged, row);
      } else {
        tbody.insertBefore(dragged, row.nextSibling);
      }
      persistOrder();
    });

    function closestRow(el) {
      while (el && el !== tbody) {
        if (el.tagName === 'TR' && el.hasAttribute(idAttr)) return el;
        el = el.parentNode;
      }
      return null;
    }

    function insertBefore(e, row) {
      var rect = row.getBoundingClientRect();
      return (e.clientY - rect.top) < rect.height / 2;
    }

    function clearDropMarkers() {
      tbody.querySelectorAll('.drop-above, .drop-below').forEach(function (r) {
        r.classList.remove('drop-above');
        r.classList.remove('drop-below');
      });
    }

    function persistOrder() {
      var ids = [];
      tbody.querySelectorAll('tr[' + idAttr + ']').forEach(function (r) {
        var raw = r.getAttribute(idAttr);
        var n = parseInt(raw, 10);
        if (!isNaN(n)) ids.push(n);
      });
      flashStatus(sbT('js.reorder.saving'), '');
      fetch(endpoint, {
        method: 'POST',
        credentials: 'same-origin',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': token,
          'Accept': 'application/json'
        },
        body: JSON.stringify({ ids: ids })
      }).then(function (res) {
        if (res.ok) flashStatus(sbT('js.reorder.saved'), 'success');
        else flashStatus(sbT('js.reorder.errorHTTP', res.status), 'error');
      }).catch(function () {
        flashStatus(sbT('js.reorder.errorGeneric'), 'error');
      });
    }

    function flashStatus(msg, cls) {
      if (!status) return;
      status.hidden = false;
      status.textContent = msg;
      status.className = 'reorder-status' + (cls ? ' ' + cls : '');
    }
  }

  function hasFiles(dt) {
    if (!dt.types) return false;
    for (var i = 0; i < dt.types.length; i++) {
      if (dt.types[i] === 'Files') return true;
    }
    return false;
  }

  // ---- date-format live preview ---------------------------------------
  // On the デザイン設定 > 設定 page each of the 5 pattern inputs gets a
  // sibling `<span data-date-format-preview>` that re-renders as the
  // author types. Mirrors the server-side dateformat.Expand logic so
  // the preview and the public site always agree; if this ever drifts,
  // the server render wins (this is just a typing aid).
  (function () {
    var section = document.querySelector('[data-date-format-section]');
    if (!section) return;
    var lang = section.getAttribute('data-lang') || 'en';
    section.querySelectorAll('[data-date-format-input]').forEach(function (input) {
      var preview = input.parentNode.querySelector('[data-date-format-preview]');
      if (!preview) return;
      var update = function () {
        var out = expandDateFormat(input.value, new Date(), lang);
        preview.textContent = out;
      };
      input.addEventListener('input', update);
    });
  })();

  function expandDateFormat(pattern, d, lang) {
    if (!pattern) return '';
    var tokens = dateFormatTokens(d, lang);
    return pattern.replace(/%([A-Za-z0-9]+)%/g, function (match, name) {
      return Object.prototype.hasOwnProperty.call(tokens, name) ? tokens[name] : match;
    });
  }
  function dateFormatTokens(d, lang) {
    var pad2 = function (n) { return (n < 10 ? '0' : '') + n; };
    var y = d.getFullYear();
    var mo = d.getMonth() + 1;
    var day = d.getDate();
    var h = d.getHours();
    var mi = d.getMinutes();
    var se = d.getSeconds();
    var wk = d.getDay();
    var tz = (function () {
      var m = -d.getTimezoneOffset();
      var sign = m >= 0 ? '+' : '-';
      var abs = Math.abs(m);
      return sign + pad2(Math.floor(abs / 60)) + pad2(abs % 60);
    })();
    var weekLongEN = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
    var weekShortEN = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
    var monthLongEN = ['', 'January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];
    var monthShortEN = ['', 'Jan.', 'Feb.', 'Mar.', 'Apr.', 'May.', 'Jun.', 'Jul.', 'Aug.', 'Sep.', 'Oct.', 'Nov.', 'Dec.'];
    var weekLongJA = ['日曜日', '月曜日', '火曜日', '水曜日', '木曜日', '金曜日', '土曜日'];
    var weekShortJA = ['日', '月', '火', '水', '木', '金', '土'];
    var dayOrd = (function () {
      if (lang === 'ja') return day + '日';
      var mod100 = day % 100;
      if (mod100 >= 11 && mod100 <= 13) return day + 'th';
      switch (day % 10) {
        case 1: return day + 'st';
        case 2: return day + 'nd';
        case 3: return day + 'rd';
      }
      return day + 'th';
    })();
    var h11 = h % 12;
    var h12 = h % 12 || 12;
    return {
      Year: String(y),
      YearShort: pad2(y % 100),
      Mon: pad2(mo),
      MonNum: String(mo),
      MonShort: lang === 'ja' ? (mo + '月') : monthShortEN[mo],
      MonLong: lang === 'ja' ? (mo + '月') : monthLongEN[mo],
      Day: pad2(day),
      DayShort: String(day),
      DayOrd: dayOrd,
      Week: lang === 'ja' ? weekShortJA[wk] : weekShortEN[wk],
      WeekLong: lang === 'ja' ? weekLongJA[wk] : weekLongEN[wk],
      Hour: pad2(h),
      Hour24: String(h),
      Hour11: pad2(h11),
      Hour12: pad2(h12),
      HourAP: h < 12 ? 'AM' : 'PM',
      Min: pad2(mi),
      Sec: pad2(se),
      Zone: tz
    };
  }

  // readCSRFToken locates a csrf_token hidden input anywhere on the
  // page and returns its value. The admin layout renders one per
  // rendered form so every page reaches the fetch paths with a
  // populated token without relying on a dedicated attribute.
  function readCSRFToken() {
    var el = document.querySelector('input[name="csrf_token"]');
    return el && el.value ? el.value : '';
  }

  // Swap a button into a "loading" state with a spinning indicator
  // while a network request is in flight. The previous textContent is
  // captured up-front and restored by the matching restore() call so
  // even nested-i18n labels survive the round trip. Returns a restore
  // closure the caller invokes on settle (success OR failure).
  function setButtonLoading(btn) {
    if (!btn) return function () {};
    var originalLabel = btn.textContent;
    btn.disabled = true;
    btn.setAttribute('aria-busy', 'true');
    var spinner = document.createElement('span');
    spinner.className = 'sb-spinner';
    spinner.setAttribute('aria-hidden', 'true');
    btn.replaceChildren(spinner);
    return function restore() {
      btn.disabled = false;
      btn.removeAttribute('aria-busy');
      btn.textContent = originalLabel;
    };
  }

  // ---- Ace AI toolbar dispatcher --------------------------------------
  // Shared helper so the three toolbar buttons (rewrite / continue /
  // summarise) POST to /admin/ai/compose with the right context +
  // apply the response in an action-appropriate way (replace
  // selection, insert at cursor, show as a popover).
  function runAceAI(editor, btn, action) {
    if (!editor || !action) return;
    var selection = (editor.getSelectedText() || '').trim();
    var full = editor.getValue();
    var req = { action: action, format: detectEditorFormat(editor) };

    if (action === 'rewrite' || action === 'summarise') {
      if (!selection) {
        showToast(sbT('js.ai.selectionRequired'));
        return;
      }
      req.text = selection;
    } else if (action === 'continue') {
      // Prefer the text up to the cursor (what the author has
      // written so far). Fall back to the full buffer when the
      // cursor is at position 0.
      var cursorPos = editor.getCursorPosition();
      var doc = editor.session.getDocument();
      var upto = doc.getTextRange({ start: { row: 0, column: 0 }, end: cursorPos });
      req.context = upto.trim() || full.trim();
      if (!req.context) {
        showToast(sbT('js.ai.contextRequired'));
        return;
      }
    }

    var restore = setButtonLoading(btn);
    showToast(sbT('js.ai.thinking'));

    fetch('/admin/ai/compose', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Accept': 'application/json',
        'X-CSRF-Token': readCSRFToken(),
      },
      body: JSON.stringify(req),
      credentials: 'same-origin'
    }).then(function (res) { return res.json().catch(function () { return { ok: false, error: 'parse' }; }); })
      .then(function (data) {
        if (!data || !data.ok) {
          var key = (data && data.error) || 'provider_error';
          showToast(sbT('js.ai.err.' + key));
          return;
        }
        applyAIResult(editor, action, selection, data.text || '');
      })
      .catch(function () { showToast(sbT('js.ai.err.provider_error')); })
      .then(restore);
  }

  function applyAIResult(editor, action, selection, text) {
    if (!text) return;
    editor.focus();
    if (action === 'rewrite') {
      // Replace the selection in place.
      editor.session.replace(editor.selection.getRange(), text);
      return;
    }
    if (action === 'continue') {
      // Insert a paragraph break + the new text at the cursor.
      editor.insert('\n\n' + text);
      return;
    }
    if (action === 'summarise') {
      // Summaries get inserted at the top of the buffer — or above
      // the selection if the user had one — wrapped with a format-
      // aware heading so the author can move / rewrite it.
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

      fetch('/admin/ai/compose', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Accept': 'application/json',
          'X-CSRF-Token': readCSRFToken(),
        },
        body: JSON.stringify({
          action: action,
          text: textForPrompt,
          format: form.querySelector('select[name="format"]') ? form.querySelector('select[name="format"]').value : 'html',
          language: document.documentElement.lang || 'ja',
        }),
        credentials: 'same-origin'
      }).then(function (res) { return res.json().catch(function () { return { ok: false, error: 'parse' }; }); })
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
})();
