export function createI18n(bundle) {
  return function sbT(key) {
    var tmpl = bundle[key] || key;
    if (arguments.length <= 1) return tmpl;
    var args = Array.prototype.slice.call(arguments, 1);
    var i = 0;
    return tmpl.replace(/%[ds]/g, function () {
      var v = args[i++];
      return v === undefined ? '' : String(v);
    });
  };
}
