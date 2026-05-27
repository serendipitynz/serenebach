export function readCSRFToken() {
  var el = document.querySelector('input[name="csrf_token"]');
  return el && el.value ? el.value : '';
}

export function csrfTokenFrom(el) {
  return (el.closest('[data-csrf]') || document.querySelector('[data-csrf]') ||
    document.querySelector('[data-upload]') || document.body).getAttribute('data-csrf') || '';
}
