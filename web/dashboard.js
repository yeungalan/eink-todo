'use strict';

/*
  E-ink dashboard controller.

  Everything it shows is Tokyo-local, so the reload cadence and the "next train"
  window are keyed off Asia/Tokyo wall-clock time (computed with Intl so it is
  correct regardless of the Kindle's own timezone).

  Auto-reload schedule (JST) — quiet overnight, tightest around the 11:00–12:00
  commute window when the next-train panel is live:

      23:00–08:00   every 3 hours
      08:00–11:00   every 30 minutes
      11:00–12:00   every 5 minutes   (+ 次の電車 panel)
      12:00–18:00   every 30 minutes
      18:00–19:00   every 30 minutes   (bridge; unspecified in the brief)
      19:00–23:00   every 1 hour
*/

var WX_ICON = './img/weather/';
var ST_ICON = './img/'; // 運行情報 status glyphs: normal.svg / info.svg / adjust.svg

var timer = null;

// ---------- Tokyo clock ----------
var jstHM = new Intl.DateTimeFormat('en-GB', {
  timeZone: 'Asia/Tokyo', hour12: false, hour: '2-digit', minute: '2-digit'
});
var jstStamp = new Intl.DateTimeFormat('en-GB', {
  timeZone: 'Asia/Tokyo', hour12: false,
  year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit'
});

function tokyoHour() {
  var parts = jstHM.formatToParts(new Date());
  for (var i = 0; i < parts.length; i++) {
    if (parts[i].type === 'hour') return parseInt(parts[i].value, 10);
  }
  return new Date().getHours();
}

// "2026/07/10 10:25" in JST, from a Date (default now).
function stamp(d) {
  var p = {};
  jstStamp.formatToParts(d || new Date()).forEach(function (x) { p[x.type] = x.value; });
  return p.year + '/' + p.month + '/' + p.day + ' ' + p.hour + ':' + p.minute;
}
function hm(d) { return jstHM.format(d || new Date()); }

// interval for the given JST hour: [milliseconds, human label]
function intervalFor(hour) {
  if (hour >= 23 || hour < 8) return [3 * 3600e3, '3時間'];
  if (hour < 11) return [30 * 60e3, '30分'];
  if (hour < 12) return [5 * 60e3, '5分'];      // 11:00–12:00
  if (hour < 18) return [30 * 60e3, '30分'];
  if (hour < 19) return [30 * 60e3, '30分'];     // 18:00–19:00 bridge
  return [60 * 60e3, '1時間'];                    // 19:00–23:00
}

// 次の電車 panel is live only inside the 11:00–12:00 window.
function inNextTrainWindow(hour) { return hour === 11; }

// ---------- fetch helpers ----------
async function getJSON(url) {
  var r = await fetch(url, { cache: 'no-store' });
  if (!r.ok) throw new Error(url + ' -> ' + r.status);
  return r.json();
}

// ---------- weather ----------
function renderWeather(w) {
  var set = function (id, v) { var e = document.getElementById(id); if (e) e.textContent = v; };
  document.getElementById('wxIcon').src = WX_ICON + (w.icon || 'wi-na') + '.svg';
  document.getElementById('wxHeadIcon').src = WX_ICON + (w.icon || 'wi-na') + '.svg';
  set('wxTemp', Math.round(w.tempC));
  set('wxCond', w.conditionJa);
  set('wxHigh', Math.round(w.highC));
  set('wxLow', Math.round(w.lowC));
  set('wxFeels', Math.round(w.feelsC));
  set('wxExtra', '降水' + w.precipProb + '%　湿度' + w.humidity + '%　風' + Math.round(w.windKmh) + 'km/h');
  set('wxObs', w.observedAt || '--:--');
}

async function loadWeather() {
  try { renderWeather(await getJSON('/api/weather')); }
  catch (e) { document.getElementById('wxCond').textContent = '天気を取得できません'; }
}

// ---------- 運行情報 ----------
function renderDiainfo(d) {
  var ul = document.getElementById('lineList');
  ul.innerHTML = '';
  (d.lines || []).forEach(function (li) {
    var row = document.createElement('li');

    var img = document.createElement('img');
    img.className = 'marker';
    img.src = ST_ICON + (li.icon || 'normal') + '.svg';
    img.width = 30; img.height = 30; img.alt = '';
    row.appendChild(img);

    var box = document.createElement('div');
    var name = document.createElement('div');
    name.className = 'line-name';
    name.textContent = li.name;
    var state = document.createElement('div');
    state.className = 'line-state' + (li.level !== 'normal' ? ' alert' : '');
    state.textContent = li.status;
    box.appendChild(name); box.appendChild(state);
    row.appendChild(box);
    ul.appendChild(row);
  });
  if (!ul.children.length) {
    ul.innerHTML = '<li><div><div class="line-state">運行情報を取得できません</div></div></li>';
  }
}

async function loadDiainfo() {
  try { renderDiainfo(await getJSON('/api/diainfo')); }
  catch (e) {
    document.getElementById('lineList').innerHTML =
      '<li><div><div class="line-state">運行情報を取得できません</div></div></li>';
  }
}

// ---------- 次の電車 (幡ヶ谷 → 新線新宿) ----------
function renderNextTrain(nt) {
  var list = document.getElementById('ntList');
  list.innerHTML = '';
  var trains = nt.trains || [];
  if (!trains.length) {
    list.innerHTML = '<li class="nt-empty">' + (nt.note || '接近中の列車はありません') + '</li>';
    return;
  }
  trains.forEach(function (t) {
    var li = document.createElement('li');

    var badge = document.createElement('span');
    badge.className = 'nt-badge';
    badge.textContent = t.typeIcon || '•';
    li.appendChild(badge);

    var eta = document.createElement('span');
    eta.className = 'nt-eta';
    eta.textContent = t.stopsAway === 0 ? 'まもなく' : (t.stopsAway + '駅前');
    li.appendChild(eta);

    var dst = document.createElement('span');
    dst.className = 'nt-dst';
    dst.textContent = t.destination + '行';
    li.appendChild(dst);

    if (t.delayMin > 0) {
      var late = document.createElement('span');
      late.className = 'nt-late';
      late.textContent = '+' + t.delayMin + '分';
      li.appendChild(late);
    }
    list.appendChild(li);
  });
}

async function loadNextTrain() {
  try { renderNextTrain(await getJSON('/api/nexttrain')); }
  catch (e) {
    document.getElementById('ntList').innerHTML =
      '<li class="nt-empty">次の電車情報を取得できません</li>';
  }
}

// ---------- reload orchestration ----------
async function loadAll() {
  var hour = tokyoHour();
  var showNext = inNextTrainWindow(hour);
  document.getElementById('nextTrain').hidden = !showNext;

  var jobs = [loadWeather(), loadDiainfo()];
  if (showNext) jobs.push(loadNextTrain());
  await Promise.all(jobs);

  document.getElementById('updated').textContent = stamp();
}

function schedule() {
  var iv = intervalFor(tokyoHour());
  var next = new Date(Date.now() + iv[0]);
  document.getElementById('nextUpdate').textContent = hm(next) + '（' + iv[1] + '毎）';
  clearTimeout(timer);
  timer = setTimeout(cycle, iv[0]);
}

async function cycle() {
  await loadAll();
  schedule();   // re-evaluate interval each time so the cadence adapts by hour
}

// ---------- todo counter (unchanged behaviour) ----------
function wireTodo() {
  var boxes = Array.prototype.slice.call(
    document.querySelectorAll('#todoList input[type=checkbox]'));
  var done = document.getElementById('done');
  var total = document.getElementById('total');
  total.textContent = boxes.length;
  function recount() {
    done.textContent = boxes.filter(function (b) { return b.checked; }).length;
  }
  boxes.forEach(function (b) { b.addEventListener('change', recount); });
  recount();
}

// ---------- boot ----------
wireTodo();
cycle();
