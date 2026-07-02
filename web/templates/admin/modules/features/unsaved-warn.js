// unsaved-warn.js — beforeunload guard for admin edit forms.
//
// Any form tagged [data-unsaved-warn] gets a browser beforeunload
// warning if its contents diverge from the initial snapshot. Covers
// entry / category / link / template / user / profile editors. We
// compare a string-keyed FormData snapshot (file inputs excluded, since
// File objects don't serialise and their own pickers handle unsaved
// state separately), and skip the prompt during legitimate form
// submission.
//
// DOM contract:
//   form[data-unsaved-warn] (any number) — snapshotted after Ace is
//   ready so editor hydration doesn't count as an edit.
// Takes the Ace ready Promise so the baseline snapshot waits for the
// editor to populate its backing <textarea>.

export function initUnsavedWarn(aceReady) {
  var forms = document.querySelectorAll('form[data-unsaved-warn]');
  if (!forms.length) { return; }
  var submitting = false;
  forms.forEach(function (form) {
    var initial = null;
    aceReady.then(function () {
      setTimeout(function () { initial = snapshot(form); }, 0);
    });

    form.addEventListener('submit', function () { submitting = true; });
    document.addEventListener('submit', function () { submitting = true; }, true);

    window.addEventListener('beforeunload', function (e) {
      if (submitting) { return; }
      if (!document.body.contains(form)) { return; }
      if (initial === null) { return; }
      if (snapshot(form) === initial) { return; }
      e.preventDefault();
      e.returnValue = '';
      return '';
    });
  });
}

// snapshot returns a stable string form of every scalar field on
// `form`, suitable for comparison. Keys are URI-encoded and sorted so
// unordered FormData iteration doesn't produce false positives.
function snapshot(form) {
  if (!window.FormData) { return ''; }
  var fd = new FormData(form);
  var pairs = [];
  fd.forEach(function (v, k) {
    if (typeof v === 'string') {
      pairs.push(encodeURIComponent(k) + '=' + encodeURIComponent(v));
    }
  });
  pairs.sort();
  return pairs.join('&');
}
