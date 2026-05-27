export function wireDragHover(zone, hoverClass) {
  ['dragenter', 'dragover'].forEach(function (evt) {
    zone.addEventListener(evt, function (e) {
      e.preventDefault(); e.stopPropagation();
      zone.classList.add(hoverClass);
    });
  });
  ['dragleave', 'drop'].forEach(function (evt) {
    zone.addEventListener(evt, function (e) {
      e.preventDefault(); e.stopPropagation();
      zone.classList.remove(hoverClass);
    });
  });
}

export function initDropToInput() {
  document.querySelectorAll('[data-drop-to-input]').forEach(function (zone) {
    var input = zone.querySelector('[data-drop-input]');
    if (!input) return;
    var placeholder = zone.querySelector('[data-drop-placeholder]');
    var defaultText = placeholder ? placeholder.textContent : '';

    wireDragHover(zone, 'drag-over');
    zone.addEventListener('drop', function (e) {
      var files = e.dataTransfer && e.dataTransfer.files;
      if (!files || !files.length) return;
      try {
        var dt = new DataTransfer();
        dt.items.add(files[0]);
        input.files = dt.files;
      } catch (_) {
        return;
      }
      updateLabel();
    });
    input.addEventListener('change', updateLabel);

    function updateLabel() {
      if (!placeholder) return;
      placeholder.textContent = (input.files && input.files.length)
        ? input.files[0].name
        : defaultText;
    }
  });
}
