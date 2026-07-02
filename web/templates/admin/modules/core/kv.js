// kv.js — helpers for the read-only key/value definition lists shown by
// the comment-detail and template-info modals. Kept in core because both
// feature modules render the same <dl class="kv"> shape.

// appendKV appends a <dt>/<dd> pair to a <dl>. A DOM node value is
// appended as-is; anything else is coerced to text.
export function appendKV(dl, key, value) {
  var dt = document.createElement('dt'); dt.textContent = key;
  var dd = document.createElement('dd');
  if (value && value.nodeType) { dd.appendChild(value); } else { dd.textContent = value; }
  dl.appendChild(dt); dl.appendChild(dd);
}

// linkifyNode returns an <a> that opens `u` in a new tab without leaking
// the referrer or exposing window.opener.
export function linkifyNode(u) {
  var a = document.createElement('a');
  a.href = u; a.target = '_blank'; a.rel = 'noopener nofollow';
  a.textContent = u; return a;
}
