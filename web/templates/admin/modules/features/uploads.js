import { createI18n } from '../core/i18n.js';
import { showToast } from '../core/toast.js';
import { wireDragHover } from './drop-to-input.js';

const sbT = createI18n((typeof window !== 'undefined' && window.__sbI18n) || {});

export function uploadFile(file, csrfToken, endpoint) {
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
      return { ok: res.ok, status: res.status, body: {} };
    });
  });
}

export function uploadBatch(files, token, opts) {
  opts = opts || {};
  var setProgress = opts.setProgress || function () {};
  var onDone = opts.onDone || function () {};
  var total = files.length;
  var done = 0;
  var errors = [];
  var altPending = [];

  setProgress(sbT('js.upload.uploading', 0, total));

  var chain = Promise.resolve();
  Array.prototype.slice.call(files).forEach(function (file) {
    chain = chain.then(function () {
      return uploadFile(file, token, opts.endpoint).then(function (result) {
        done += 1;
        if (!result.ok) {
          errors.push((file.name || 'file') + ': ' + (result.body && result.body.error || ('HTTP ' + result.status)));
        } else if (result.body && result.body.auto_alt_requested && result.body.id) {
          altPending.push(result.body.id);
        }
        setProgress(sbT('js.upload.uploading', done, total));
      });
    });
  });

  return chain.then(function () {
    if (errors.length) { alert(errors.join('\n')); }
    if (altPending.length === 0) {
      onDone();
      return;
    }
    setProgress(sbT('js.ai.altGenerating', altPending.length));
    var altPromises = altPending.map(function (id) {
      return fetch('/admin/images/' + id + '/alt', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token, 'Accept': 'application/json' },
        credentials: 'same-origin'
      }).then(function (res) { return res.json().catch(function () { return { ok: false }; }); })
        .catch(function () { return { ok: false }; });
    });
    return Promise.all(altPromises).then(function (results) {
      var failed = results.filter(function (r) { return !r.ok; }).length;
      showToast(failed > 0 ? sbT('js.ai.altFail', failed) : sbT('js.ai.altDone', results.length));
      onDone();
    });
  });
}

export function initUploadForms() {
  var dropForms = document.querySelectorAll('[data-upload]');
  dropForms.forEach(function (form) {
    var zone = form.querySelector('[data-drop-zone]');
    var input = form.querySelector('[data-drop-input]');
    var progress = form.querySelector('.drop-zone-progress');
    if (!zone || !input) return;

    wireDragHover(zone, 'drag-over');
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
      uploadBatch(files, token, {
        endpoint: endpoint,
        setProgress: function (text) {
          if (progress) { progress.hidden = false; progress.textContent = text; }
        },
        onDone: function () { window.location.reload(); }
      });
    }
  });
}
