export function setButtonLoading(btn) {
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
