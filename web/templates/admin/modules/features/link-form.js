export function initLinkKindToggle() {
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
