import { createI18n } from '../core/i18n.js';
import { openModal } from '../core/modal.js';

const sbT = createI18n((typeof window !== 'undefined' && window.__sbI18n) || {});

// Extension → preview kind. Mirrors the template-asset upload allowlist
// in internal/handler/admin/template_assets.go: images render inline,
// text/code is fetched and shown monospace, web fonts have no visual
// preview so they fall back to an icon.
var ASSET_KIND = {
  png: 'image', jpg: 'image', jpeg: 'image', gif: 'image', webp: 'image', svg: 'image',
  css: 'text', js: 'text', txt: 'text',
  woff: 'font', woff2: 'font', ttf: 'font', otf: 'font'
};

// lucide file-type-corner — web fonts can't be previewed visually, so
// the modal shows this "type" file icon instead. Same stroke style as
// the upload icons in templates.go / image-library.js.
var fontIconSVG = '<svg class="icon-upload modal-doc-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false"><path d="M12 22h6a2 2 0 0 0 2-2V8a2.4 2.4 0 0 0-.706-1.706l-3.588-3.588A2.4 2.4 0 0 0 14 2H6a2 2 0 0 0-2 2v6"/><path d="M14 2v5a1 1 0 0 0 1 1h5"/><path d="M3 16v-1.5a.5.5 0 0 1 .5-.5h7a.5.5 0 0 1 .5.5V16"/><path d="M6 22h2"/><path d="M7 14v8"/></svg>';

function kindForName(name) {
  var dot = name.lastIndexOf('.');
  if (dot < 0) return 'text';
  var ext = name.slice(dot + 1).toLowerCase();
  return ASSET_KIND[ext] || 'text';
}

export function initTemplateAssetPreview() {
  document.querySelectorAll('[data-asset-preview]').forEach(function (trigger) {
    trigger.addEventListener('click', function (e) {
      e.preventDefault();
      var base = trigger.getAttribute('data-asset-base') || '';
      var name = trigger.getAttribute('data-asset-name') || '';
      if (!base || !name) return;
      // Build the URL from the raw filename with encodeURIComponent: a
      // name containing #, ?, &, or spaces must be escaped per-segment,
      // otherwise the browser would read it as a fragment/query and
      // request the wrong (or truncated) path.
      openPreview(kindForName(name), base + encodeURIComponent(name), name);
    });
  });
}

function openPreview(kind, url, name) {
  if (kind === 'image') {
    var img = document.createElement('img');
    img.src = url;
    img.alt = name;
    openModal({ title: name, variant: 'image', bodyNode: img });
    return;
  }
  if (kind === 'font') {
    var wrap = document.createElement('div');
    wrap.className = 'modal-doc-preview';
    wrap.innerHTML = fontIconSVG;
    openModal({ title: name, variant: 'media', bodyNode: wrap });
    return;
  }
  // text: fetch the asset and show it monospace. Start with a loading
  // placeholder so the modal opens immediately, then swap in the body.
  var pre = document.createElement('pre');
  pre.className = 'modal-text-preview';
  pre.textContent = sbT('js.picker.loading');
  openModal({ title: name, variant: 'wide', bodyNode: pre });
  fetch(url, { credentials: 'same-origin' })
    .then(function (res) {
      if (!res.ok) throw new Error('HTTP ' + res.status);
      return res.text();
    })
    .then(function (text) {
      pre.textContent = text;
    })
    .catch(function () {
      pre.classList.add('modal-text-error');
      pre.textContent = sbT('js.modal.asset.loadError');
    });
}
