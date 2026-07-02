// comment-detail-modal.js — comment moderation list: click an author or
// body cell to open a read-only modal with the full comment payload
// (no line clamp inside the dialog).
//
// DOM contract:
//   .cell-clickable[data-comment-body]  (required) the clickable cell;
//     also reads data-comment-author / -email / -url / -ip for the modal.
//   Clicks landing on a nested <a>/<button>/<form> are ignored so row
//   actions keep working.
// Depends on: core/modal (openModal), core/kv (appendKV, linkifyNode).
// No network I/O. No-JS fallback: the cells stay plain, non-clickable text.

import { createI18n } from '../core/i18n.js';
import { openModal } from '../core/modal.js';
import { appendKV, linkifyNode } from '../core/kv.js';

const sbT = createI18n((typeof window !== 'undefined' && window.__sbI18n) || {});

export function initCommentDetailModal() {
  document.querySelectorAll('.cell-clickable[data-comment-body]').forEach(function (cell) {
    cell.addEventListener('click', function (e) {
      if (e.target.closest('a, button, form')) { return; }
      e.preventDefault();
      var author = cell.getAttribute('data-comment-author') || '';
      var email = cell.getAttribute('data-comment-email') || '';
      var url = cell.getAttribute('data-comment-url') || '';
      var body = cell.getAttribute('data-comment-body') || '';
      var ip = cell.getAttribute('data-comment-ip') || '';
      var dl = document.createElement('dl');
      dl.className = 'kv';
      var DASH = sbT('js.field.dash');
      appendKV(dl, sbT('js.field.commentAuthor'), author || DASH);
      appendKV(dl, sbT('js.field.email'), email || DASH);
      appendKV(dl, 'URL', url ? linkifyNode(url) : DASH);
      appendKV(dl, 'IP', ip || DASH);
      appendKV(dl, sbT('js.field.commentBody'), body || DASH);
      openModal({ title: sbT('js.modal.comment'), bodyNode: dl });
    });
  });
}
