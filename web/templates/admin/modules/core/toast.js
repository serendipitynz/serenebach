export function showToast(msg, variant) {
  var el = document.createElement('div');
  el.className = variant === 'error' ? 'toast error' : 'toast';
  el.textContent = msg;
  document.body.appendChild(el);
  requestAnimationFrame(function () { el.classList.add('visible'); });
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

export function initToastPromotion() {
  document.querySelectorAll('.alert.success').forEach(function (el) {
    var msg = (el.textContent || '').trim();
    if (msg) showToast(msg);
    el.remove();
  });
  document.querySelectorAll('.alert.error').forEach(function (el) {
    var msg = (el.textContent || '').trim();
    if (msg) showToast(msg, 'error');
  });
}
