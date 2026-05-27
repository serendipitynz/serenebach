export function initDateFormatPreview() {
  var section = document.querySelector('[data-date-format-section]');
  if (!section) return;
  var lang = section.getAttribute('data-lang') || 'en';
  section.querySelectorAll('[data-date-format-input]').forEach(function (input) {
    var preview = input.parentNode.querySelector('[data-date-format-preview]');
    if (!preview) return;
    var update = function () {
      var out = expandDateFormat(input.value, new Date(), lang);
      preview.textContent = out;
    };
    input.addEventListener('input', update);
  });
}

function expandDateFormat(pattern, d, lang) {
  if (!pattern) return '';
  var tokens = dateFormatTokens(d, lang);
  return pattern.replace(/%([A-Za-z0-9]+)%/g, function (match, name) {
    return Object.prototype.hasOwnProperty.call(tokens, name) ? tokens[name] : match;
  });
}

function dateFormatTokens(d, lang) {
  var pad2 = function (n) { return (n < 10 ? '0' : '') + n; };
  var y = d.getFullYear();
  var mo = d.getMonth() + 1;
  var day = d.getDate();
  var h = d.getHours();
  var mi = d.getMinutes();
  var se = d.getSeconds();
  var wk = d.getDay();
  var tz = (function () {
    var m = -d.getTimezoneOffset();
    var sign = m >= 0 ? '+' : '-';
    var abs = Math.abs(m);
    return sign + pad2(Math.floor(abs / 60)) + pad2(abs % 60);
  })();
  var weekLongEN = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
  var weekShortEN = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
  var monthLongEN = ['', 'January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];
  var monthShortEN = ['', 'Jan.', 'Feb.', 'Mar.', 'Apr.', 'May.', 'Jun.', 'Jul.', 'Aug.', 'Sep.', 'Oct.', 'Nov.', 'Dec.'];
  var weekLongJA = ['日曜日', '月曜日', '火曜日', '水曜日', '木曜日', '金曜日', '土曜日'];
  var weekShortJA = ['日', '月', '火', '水', '木', '金', '土'];
  var dayOrd = (function () {
    if (lang === 'ja') return day + '日';
    var mod100 = day % 100;
    if (mod100 >= 11 && mod100 <= 13) return day + 'th';
    switch (day % 10) {
      case 1: return day + 'st';
      case 2: return day + 'nd';
      case 3: return day + 'rd';
    }
    return day + 'th';
  })();
  var h11 = h % 12;
  var h12 = h % 12 || 12;
  return {
    Year: String(y),
    YearShort: pad2(y % 100),
    Mon: pad2(mo),
    MonNum: String(mo),
    MonShort: lang === 'ja' ? (mo + '月') : monthShortEN[mo],
    MonLong: lang === 'ja' ? (mo + '月') : monthLongEN[mo],
    Day: pad2(day),
    DayShort: String(day),
    DayOrd: dayOrd,
    Week: lang === 'ja' ? weekShortJA[wk] : weekShortEN[wk],
    WeekLong: lang === 'ja' ? weekLongJA[wk] : weekLongEN[wk],
    Hour: pad2(h),
    Hour24: String(h),
    Hour11: pad2(h11),
    Hour12: pad2(h12),
    HourAP: h < 12 ? 'AM' : 'PM',
    Min: pad2(mi),
    Sec: pad2(se),
    Zone: tz
  };
}
