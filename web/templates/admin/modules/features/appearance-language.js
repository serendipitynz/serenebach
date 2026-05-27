import { safeRead, safeWrite } from '../core/storage.js';
import { readCSRFToken } from '../core/csrf.js';

export function initAppearanceLanguage() {
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
}
