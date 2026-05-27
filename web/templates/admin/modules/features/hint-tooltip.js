// Click-to-reveal help tooltips. Each [data-hint-tip] (rendered by the
// layout's "hintTip" partial) wraps a (?) button and a hidden body.
// Clicking the button toggles its body; any outside click or Escape
// closes whichever one is open. Only one stays open at a time so the
// floating boxes never stack on top of each other.
export function initHintTooltips() {
  var tips = document.querySelectorAll('[data-hint-tip]');
  if (!tips.length) return;

  function close(tip) {
    var btn = tip.querySelector('.hint-tip-btn');
    var body = tip.querySelector('.hint-tip-body');
    if (btn) btn.setAttribute('aria-expanded', 'false');
    if (body) body.setAttribute('hidden', '');
  }

  function closeAll(except) {
    tips.forEach(function (tip) {
      if (tip !== except) close(tip);
    });
  }

  tips.forEach(function (tip) {
    var btn = tip.querySelector('.hint-tip-btn');
    var body = tip.querySelector('.hint-tip-body');
    if (!btn || !body) return;
    btn.addEventListener('click', function (e) {
      e.preventDefault();
      e.stopPropagation();
      var open = btn.getAttribute('aria-expanded') === 'true';
      closeAll(tip);
      if (open) {
        close(tip);
      } else {
        btn.setAttribute('aria-expanded', 'true');
        body.removeAttribute('hidden');
      }
    });
  });

  document.addEventListener('click', function (e) {
    if (e.target.closest('[data-hint-tip]')) return;
    closeAll(null);
  });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') closeAll(null);
  });
}
