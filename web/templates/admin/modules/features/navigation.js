export function initNavigation() {
  var burger = document.querySelector('[data-toggle-nav]');
  if (!burger) return;

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
