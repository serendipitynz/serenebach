import { createI18n } from '../core/i18n.js';
import { safeRead, safeWrite } from '../core/storage.js';
import { showToast } from '../core/toast.js';
import { openModal, closeModal } from '../core/modal.js';
import { readCSRFToken, csrfTokenFrom } from '../core/csrf.js';
import { uploadFile, uploadBatch } from './uploads.js';

const sbT = createI18n((typeof window !== 'undefined' && window.__sbI18n) || {});

var lastFocusedTextarea = null;

// Mirrors the uploadIcon template func in internal/handler/admin/templates.go.
// The library grid is rendered server-side and the picker builds its tiles in
// JS, so the same three lucide icons have to exist on both sides — change one
// and change the other.
var uploadIconSVG = {
  audio: '<svg class="icon-upload" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false"><path d="M4 11.55V4a2 2 0 0 1 2-2h8a2.4 2.4 0 0 1 1.706.706l3.588 3.588A2.4 2.4 0 0 1 20 8v12a2 2 0 0 1-2 2h-1.95"/><path d="M14 2v5a1 1 0 0 0 1 1h5"/><path d="M12 15a5 5 0 0 1 0 6"/><path d="M8 14.502a.5.5 0 0 0-.826-.381l-1.893 1.631a1 1 0 0 1-.651.243H3.5a.5.5 0 0 0-.5.501v3.006a.5.5 0 0 0 .5.501h1.129a1 1 0 0 1 .652.243l1.893 1.633a.5.5 0 0 0 .826-.38z"/></svg>',
  document: '<svg class="icon-upload" viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8l-6-6z" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"/><path d="M14 2v6h6M8 13h8M8 17h5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>',
  movie: '<svg class="icon-upload" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false"><path d="M4 12V4a2 2 0 0 1 2-2h8a2.4 2.4 0 0 1 1.706.706l3.588 3.588A2.4 2.4 0 0 1 20 8v12a2 2 0 0 1-2 2"/><path d="M14 2v5a1 1 0 0 0 1 1h5"/><path d="m10 17.843 3.033-1.755a.64.64 0 0 1 .967.56v4.704a.65.65 0 0 1-.967.56L10 20.157"/><rect width="7" height="6" x="3" y="16" rx="1"/></svg>'
};

export function initImagePicker(aceReady) {
  var picker = document.querySelector('[data-image-picker]');
  var pickerOpen = document.querySelector('[data-image-picker-open]');
  var pickerBody = picker ? picker.querySelector('[data-image-picker-body]') : null;
  var imageTargets = Array.prototype.slice.call(document.querySelectorAll('textarea[data-image-target]'));

  lastFocusedTextarea = imageTargets[0] || null;
  imageTargets.forEach(function (ta) {
    ta.addEventListener('focus', function () { lastFocusedTextarea = ta; });
  });

  aceReady.then(function (loaded) {
    if (!loaded) { return; }
    imageTargets.forEach(function (ta) {
      if (ta.__aceEditor) { ta.__aceEditor.on('focus', function () { lastFocusedTextarea = ta; }); }
    });
  });

  initOGBackgroundPicker(picker);
  initOGCardGeneration();
  initOGTextColor();
  initPickerUI(picker, pickerOpen, pickerBody);
  initPickerDragUpload(picker, pickerBody);
  initDropToTextarea(imageTargets, aceReady);
}

// ---- OG background picker --------------------------------------------
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
  if (!ogBgTargetInput) { return; }
  var stored = img.stored_path || (img.url || '').replace(/^\/img\//, '');
  ogBgTargetInput.value = stored;
  if (ogBgTargetPreview) {
    ogBgTargetPreview.src = img.url || ('/img/' + stored);
    ogBgTargetPreview.hidden = false;
  }
  ogBgTargetInput = null;
  ogBgTargetPreview = null;
  var picker = document.querySelector('[data-image-picker]');
  if (picker) { picker.hidden = true; }
}

function initOGBackgroundPicker(picker) {
  document.querySelectorAll('[data-og-bg-picker]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var fs = ogBgFieldset(btn);
      if (!fs.input || !picker) { return; }
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
      if (fs.input) { fs.input.value = ''; }
      if (fs.preview) { fs.preview.hidden = true; fs.preview.src = ''; }
    });
  });
}

// ---- OG card manual generation ---------------------------------------
function initOGCardGeneration() {
  document.querySelectorAll('[data-og-card-generate]').forEach(function (ogGenerateBtn) {
    ogGenerateBtn.addEventListener('click', function () {
      var match = window.location.pathname.match(/\/admin\/(entries|pages)\/(\d+)\/edit$/);
      if (!match) { return; }
      var statusEl = ogGenerateBtn.closest('[data-og-card-row]')?.querySelector('[data-og-card-status]');
      var preview = ogGenerateBtn.closest('[data-og-card-row]')?.querySelector('[data-og-card-preview]');
      ogGenerateBtn.disabled = true;
      if (statusEl) {
        statusEl.hidden = false;
        statusEl.textContent = '...';
      }
      var body = new URLSearchParams({ csrf_token: readCSRFToken() });
      fetch(window.location.pathname.replace(/\/edit$/, '/og'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: body,
        credentials: 'same-origin',
      })
        .then(function (res) { return res.ok ? res.json() : Promise.reject(res); })
        .then(function (data) {
          if (preview && data && data.url) {
            preview.src = data.url + (data.url.indexOf('?') >= 0 ? '&' : '?') + 'ts=' + data.ts;
            preview.hidden = false;
          }
          if (statusEl) { statusEl.textContent = 'OK'; }
        })
        .catch(function () {
          if (statusEl) { statusEl.textContent = 'NG'; }
        })
        .finally(function () {
          ogGenerateBtn.disabled = false;
        });
    });
  });
}

// ---- OG text-color controls ------------------------------------------
function initOGTextColor() {
  document.querySelectorAll('[data-og-text-color-field]').forEach(function (field) {
    var picker = field.querySelector('[data-og-text-color-picker]');
    var transparent = field.querySelector('[data-og-text-transparent]');
    var unset = field.querySelector('[data-og-text-color-unset]');
    var clear = field.querySelector('[data-og-text-color-clear]');
    function armUnset(v) { if (unset) { unset.value = v ? '1' : '0'; } }
    if (transparent) {
      transparent.addEventListener('change', function () {
        if (picker) { picker.disabled = transparent.checked; }
        armUnset(false);
      });
    }
    if (picker) {
      picker.addEventListener('input', function () { armUnset(false); });
    }
    if (clear) {
      clear.addEventListener('click', function () {
        if (transparent) { transparent.checked = false; }
        if (picker) { picker.disabled = false; picker.value = '#475569'; }
        armUnset(true);
      });
    }
  });
}

// ---- Picker UI -------------------------------------------------------
function initPickerUI(picker, pickerOpen, pickerBody) {
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

  var pickerFilter = picker && picker.querySelector('[data-image-picker-filter]');
  if (pickerFilter) {
    pickerFilter.addEventListener('input', function () {
      if (!pickerItems) { return; }
      var needle = pickerFilter.value.toLowerCase();
      if (needle === '') { renderPickerItems(pickerItems); return; }
      var filtered = pickerItems.filter(function (i) {
        return (i.filename || '').toLowerCase().indexOf(needle) !== -1;
      });
      renderPickerItems(filtered);
    });
  }
}

var pickerItems = null;

function loadPickerImages() {
  var pickerBody = document.querySelector('[data-image-picker] [data-image-picker-body]');
  if (!pickerBody) { return; }
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
  var pickerBody = document.querySelector('[data-image-picker] [data-image-picker-body]');
  if (!pickerBody) { return; }
  pickerBody.textContent = '';
  if (!items.length) { pickerBody.textContent = sbT('js.picker.empty'); return; }
  var ul = document.createElement('ul');
  ul.className = 'image-gallery';
  var visibleItems = ogBgTargetInput ? items.filter(function (i) {
    return (i.kind || 'image') === 'image';
  }) : items;
  visibleItems.forEach(function (img) {
    var li = document.createElement('li');
    li.className = 'image-tile';
    var kind = img.kind || 'image';
    var tile;
    if (kind === 'image') {
      tile = document.createElement('img');
      tile.src = img.thumb_url || img.url;
      tile.alt = img.filename || '';
      tile.draggable = true;
    } else {
      tile = document.createElement('div');
      tile.className = 'upload-icon-wrap';
      tile.innerHTML = uploadIconSVG[kind] || '';
    }
    tile.dataset.fullUrl = img.url;
    tile.dataset.filename = img.filename || '';
    tile.dataset.kind = kind;
    li.addEventListener('click', function () {
      if (ogBgTargetInput) {
        applyOGBGPick(img);
        return;
      }
      insertFileMarkup(img.url, img.alt || img.filename || '', kind);
    });
    if (tile.draggable) {
      tile.addEventListener('dragstart', function (e) {
        if (!e.dataTransfer) { return; }
        e.dataTransfer.setData('text/uri-list', img.url);
        e.dataTransfer.setData('text/plain', img.url);
        e.dataTransfer.setData('application/x-sb-image', JSON.stringify({
          url: img.url, filename: img.filename || '', alt: img.alt || '', kind: kind
        }));
        e.dataTransfer.effectAllowed = 'copy';
      });
    }
    li.appendChild(pickerFigure(tile, img.filename || ''));
    ul.appendChild(li);
  });
  pickerBody.appendChild(ul);
}

// Same figure/figcaption shape the library grid renders from images.html, so
// the .image-tile rules (fixed media height, ellipsised name) apply here too
// and a long filename can no longer stretch the card.
function pickerFigure(tile, filename) {
  var figure = document.createElement('figure');
  figure.appendChild(tile);
  var caption = document.createElement('figcaption');
  var name = document.createElement('span');
  name.className = 'name';
  name.textContent = filename;
  name.title = filename;
  caption.appendChild(name);
  figure.appendChild(caption);
  return figure;
}

// ---- Picker drag-and-drop upload -------------------------------------
function initPickerDragUpload(picker, pickerBody) {
  if (!picker) { return; }
  picker.addEventListener('dragover', function (e) {
    if (!e.dataTransfer) { return; }
    if (hasFiles(e.dataTransfer)) {
      e.preventDefault();
      picker.classList.add('image-picker-dragover');
    }
  });
  picker.addEventListener('dragleave', function (e) {
    picker.classList.remove('image-picker-dragover');
  });
  picker.addEventListener('drop', function (e) {
    if (!e.dataTransfer) { return; }
    picker.classList.remove('image-picker-dragover');
    if (!hasFiles(e.dataTransfer)) { return; }
    e.preventDefault();

    uploadBatch(e.dataTransfer.files, readCSRFToken(), {
      setProgress: function (text) {
        if (pickerBody) { pickerBody.textContent = text; }
      },
      onDone: loadPickerImages
    });
  });
}

// ---- Drop to textarea / Ace ------------------------------------------
function initDropToTextarea(imageTargets, aceReady) {
  imageTargets.forEach(function (ta) {
    bindEntryAreaDrop(ta, ta);
  });
  aceReady.then(function (loaded) {
    if (!loaded) { return; }
    imageTargets.forEach(function (ta) {
      if (ta.__aceWrap) { bindEntryAreaDrop(ta.__aceWrap, ta); }
    });
  });
}

function insertFileMarkup(url, alt, kind) {
  var safeAlt = alt.replace(/"/g, '&quot;');
  var target = lastFocusedTextarea;
  if (!target) {
    var imageTargets = Array.prototype.slice.call(document.querySelectorAll('textarea[data-image-target]'));
    target = imageTargets[0] || null;
  }
  if (!target) { return; }

  var form = target.closest && target.closest('form');
  var isMarkdown = false;
  if (form) {
    var fmt = form.querySelector('select[data-code-editor-format]');
    if (fmt && fmt.value === 'markdown') {
      isMarkdown = true;
    }
  }
  var filename = alt || url.split('/').pop() || 'file';
  var tag;
  switch (kind || 'image') {
    case 'image':
      tag = isMarkdown ? '![' + alt + '](' + url + ')' : '<img src="' + url + '" alt="' + safeAlt + '">';
      break;
    case 'audio':
      tag = isMarkdown ? '[' + filename + '](' + url + ')' : '<audio controls src="' + url + '"></audio>';
      break;
    case 'movie':
      tag = isMarkdown ? '[' + filename + '](' + url + ')' : '<video controls src="' + url + '"></video>';
      break;
    case 'document':
      tag = isMarkdown ? '[' + filename + '](' + url + ')' : '<a href="' + url + '" download>' + filename + '</a>';
      break;
    default:
      tag = isMarkdown ? '[' + filename + '](' + url + ')' : '<a href="' + url + '">' + filename + '</a>';
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

function bindEntryAreaDrop(dropTarget, hostTextarea) {
  dropTarget.addEventListener('dragover', function (e) {
    if (!e.dataTransfer) { return; }
    if (hasFiles(e.dataTransfer) || hasDraggedImageURL(e.dataTransfer)) {
      e.preventDefault();
      dropTarget.classList.add('drag-over');
    }
  });
  dropTarget.addEventListener('dragleave', function () { dropTarget.classList.remove('drag-over'); });
  dropTarget.addEventListener('drop', function (e) {
    if (!e.dataTransfer) { return; }
    dropTarget.classList.remove('drag-over');
    lastFocusedTextarea = hostTextarea;

    var dt = e.dataTransfer;
    if (!(dt.files && dt.files.length)) {
      var payload = dt.getData('application/x-sb-image');
      if (payload) {
        e.preventDefault();
        try {
          var parsed = JSON.parse(payload);
          insertFileMarkup(parsed.url, parsed.alt || parsed.filename || '', parsed.kind || 'image');
        } catch (err) { /* ignore malformed payload */ }
        return;
      }
      var url = dt.getData('text/uri-list') || dt.getData('text/plain');
      if (url) {
        e.preventDefault();
        insertFileMarkup(url, '', 'image');
      }
      return;
    }

    e.preventDefault();
    var token = csrfTokenFrom(hostTextarea);
    var files = dt.files;
    Array.prototype.slice.call(files).reduce(function (chain, file) {
      return chain.then(function () {
        return uploadFile(file, token).then(function (result) {
          if (result.ok && result.body && result.body.url) {
            insertFileMarkup(result.body.url, result.body.filename || '', result.body.kind || 'image');
          } else {
            alert((file.name || 'file') + ': ' + (result.body && result.body.error || ('HTTP ' + result.status)));
          }
        });
      });
    }, Promise.resolve());
  });
}

function hasFiles(dt) {
  if (!dt.types) { return false; }
  for (var i = 0; i < dt.types.length; i++) {
    if (dt.types[i] === 'Files') { return true; }
  }
  return false;
}

function hasDraggedImageURL(dt) {
  if (!dt.types) { return false; }
  for (var i = 0; i < dt.types.length; i++) {
    var t = dt.types[i];
    if (t === 'application/x-sb-image' || t === 'text/uri-list') { return true; }
  }
  return false;
}
