var modalHost = document.querySelector('[data-modal-host]');
var modalTitle = modalHost && modalHost.querySelector('[data-modal-title]');
var modalBody = modalHost && modalHost.querySelector('[data-modal-body]');
var modalFoot = modalHost && modalHost.querySelector('[data-modal-foot]');
var modalDialog = modalHost && modalHost.querySelector('.modal');
var modalLastFocus = null;

export function openModal(opts) {
  if (!modalHost) return;
  modalLastFocus = document.activeElement;
  modalTitle.textContent = opts.title || '';
  modalBody.innerHTML = '';
  if (opts.bodyNode) modalBody.appendChild(opts.bodyNode);
  else if (typeof opts.bodyHTML === 'string') modalBody.innerHTML = opts.bodyHTML;
  else if (typeof opts.bodyText === 'string') modalBody.textContent = opts.bodyText;
  modalDialog.className = 'modal' + (opts.variant ? ' modal-' + opts.variant : '');
  modalFoot.innerHTML = '';
  if (opts.footerNode) {
    modalFoot.appendChild(opts.footerNode);
    modalFoot.hidden = false;
  } else {
    modalFoot.hidden = true;
  }
  modalHost.hidden = false;
  setTimeout(function () { try { modalDialog.focus(); } catch (e) {} }, 0);
}

export function closeModal() {
  if (!modalHost) return;
  modalHost.hidden = true;
  modalBody.innerHTML = '';
  modalFoot.innerHTML = '';
  if (modalLastFocus && modalLastFocus.focus) {
    try { modalLastFocus.focus(); } catch (e) {}
  }
}

if (modalHost) {
  modalHost.addEventListener('click', function (e) {
    if (e.target === modalHost) closeModal();
  });
  var closeBtn = modalHost.querySelector('[data-modal-close]');
  if (closeBtn) closeBtn.addEventListener('click', closeModal);
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && !modalHost.hidden) closeModal();
  });
}

// Expose for legacy inline scripts and external callers.
window.__sbAdminModal = { open: openModal, close: closeModal };
