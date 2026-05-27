export function initSortableLists(sbT) {
  initSortableList(sbT, 'category');
  initSortableList(sbT, 'template');
  initSortableList(sbT, 'user');
  initSortableList(sbT, 'link');
}

function initSortableList(sbT, kind) {
  var table = document.querySelector('[data-' + kind + '-sortable]');
  if (!table) return;
  var tbody = table.querySelector('tbody');
  var status = table.parentNode.querySelector('[data-reorder-status]');
  var token = table.getAttribute('data-csrf') || '';
  var idAttr = 'data-' + kind + '-id';
  var endpoint = table.getAttribute('data-sort-endpoint') || '/admin/' + kind + 's/reorder';
  if (!tbody) return;

  var dragged = null;

  tbody.addEventListener('dragstart', function (e) {
    var row = closestRow(e.target);
    if (!row) return;
    dragged = row;
    row.classList.add('dragging');
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', row.getAttribute(idAttr) || '');
    }
  });

  tbody.addEventListener('dragend', function () {
    if (dragged) dragged.classList.remove('dragging');
    clearDropMarkers();
    dragged = null;
  });

  tbody.addEventListener('dragover', function (e) {
    if (!dragged) return;
    e.preventDefault();
    var row = closestRow(e.target);
    if (!row || row === dragged) return;
    clearDropMarkers();
    if (insertBefore(e, row)) {
      row.classList.add('drop-above');
    } else {
      row.classList.add('drop-below');
    }
  });

  tbody.addEventListener('drop', function (e) {
    if (!dragged) return;
    e.preventDefault();
    var row = closestRow(e.target);
    clearDropMarkers();
    if (!row || row === dragged) return;
    if (insertBefore(e, row)) {
      tbody.insertBefore(dragged, row);
    } else {
      tbody.insertBefore(dragged, row.nextSibling);
    }
    persistOrder();
  });

  function closestRow(el) {
    while (el && el !== tbody) {
      if (el.tagName === 'TR' && el.hasAttribute(idAttr)) return el;
      el = el.parentNode;
    }
    return null;
  }

  function insertBefore(e, row) {
    var rect = row.getBoundingClientRect();
    return (e.clientY - rect.top) < rect.height / 2;
  }

  function clearDropMarkers() {
    tbody.querySelectorAll('.drop-above, .drop-below').forEach(function (r) {
      r.classList.remove('drop-above');
      r.classList.remove('drop-below');
    });
  }

  function persistOrder() {
    var ids = [];
    tbody.querySelectorAll('tr[' + idAttr + ']').forEach(function (r) {
      var raw = r.getAttribute(idAttr);
      var n = parseInt(raw, 10);
      if (!isNaN(n)) ids.push(n);
    });
    flashStatus(sbT('js.reorder.saving'), '');
    fetch(endpoint, {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': token,
        'Accept': 'application/json'
      },
      body: JSON.stringify({ ids: ids })
    }).then(function (res) {
      if (res.ok) flashStatus(sbT('js.reorder.saved'), 'success');
      else flashStatus(sbT('js.reorder.errorHTTP', res.status), 'error');
    }).catch(function () {
      flashStatus(sbT('js.reorder.errorGeneric'), 'error');
    });
  }

  function flashStatus(msg, cls) {
    if (!status) return;
    status.hidden = false;
    status.textContent = msg;
    status.className = 'reorder-status' + (cls ? ' ' + cls : '');
  }
}
