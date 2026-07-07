'use strict';

const REFRESH_MS = 30000; // matches upstream auto-update interval (cI = 30000)
const RING_LEN = 2 * Math.PI * 15.5;

const state = {
  meta: null,
  data: null,
  filters: { branches: new Set(), dirs: new Set(['up', 'down']), types: new Set(), q: '' },
  view: 'map',
  paused: false,
  countdown: REFRESH_MS / 1000,
  timer: null,
  tick: null,
};

const $ = (s, r = document) => r.querySelector(s);
const $$ = (s, r = document) => [...r.querySelectorAll(s)];
const el = (tag, cls, txt) => { const e = document.createElement(tag); if (cls) e.className = cls; if (txt != null) e.textContent = txt; return e; };

// ---------- boot ----------
init();

async function init() {
  document.getElementById('ringFg').setAttribute('stroke-dasharray', RING_LEN);
  try {
    const r = await fetch('/api/meta');
    state.meta = await r.json();
  } catch (e) { state.meta = { branches: [] }; }
  buildBranchFilter();
  wireControls();
  await load();
  startTimers();
}

function startTimers() {
  clearInterval(state.timer); clearInterval(state.tick);
  state.timer = setInterval(() => { if (!state.paused) load(); }, REFRESH_MS);
  state.tick = setInterval(() => {
    if (state.paused) return;
    state.countdown = Math.max(0, state.countdown - 1);
    updateRing();
  }, 1000);
}

function updateRing() {
  const frac = state.countdown / (REFRESH_MS / 1000);
  document.getElementById('ringFg').setAttribute('stroke-dashoffset', RING_LEN * (1 - frac));
}

// ---------- data ----------
async function load() {
  try {
    const r = await fetch('/api/state', { cache: 'no-store' });
    if (!r.ok) throw new Error('bad gateway');
    state.data = await r.json();
    state.countdown = REFRESH_MS / 1000;
    updateRing();
    render();
  } catch (e) {
    const nb = document.getElementById('noticeBar');
    nb.className = 'notice-bar alert';
    nb.textContent = '⚠ フィードの取得に失敗しました。再試行します…';
  }
}

// ---------- filters UI ----------
function buildBranchFilter() {
  const wrap = document.getElementById('branchFilter');
  (state.meta.branches || []).forEach(b => {
    const c = el('button', 'chip on');
    c.dataset.branch = b.Key;
    const dot = el('span', 'cdot'); dot.style.background = b.Color;
    c.append(dot, document.createTextNode(b.Name));
    const cnt = el('span', 'count-badge'); cnt.dataset.bc = b.Key; c.append(cnt);
    c.onclick = () => toggleSet(state.filters.branches, b.Key, c, true);
    wrap.append(c);
    state.filters.branches.add(b.Key);
  });
  state.filters.branches.add('other');
}

const TYPE_ORDER = [
  { code: '1', name: '特急', color: '#da007a', text: '#fff' },
  { code: '9', name: '京王ライナー', color: '#231f20', text: '#fff' },
  { code: '2', name: '急行', color: '#00a785', text: '#fff' },
  { code: '3', name: '快速', color: '#436488', text: '#fff' },
  { code: '5', name: '区間急行', color: '#d8ca00', text: '#1a1a1a' },
  { code: '6', name: '各駅停車', color: '#898989', text: '#fff' },
  { code: '11', name: 'Mt.TAKAO', color: '#618e2b', text: '#fff' },
  { code: '10', name: '臨時', color: '#ffffff', text: '#1a1a1a' },
];

function buildTypeFilter() {
  const wrap = document.getElementById('typeFilter');
  $$('.chip', wrap).forEach(c => c.remove());
  const present = new Set((state.data?.trains || []).map(t => t.typeCode));
  TYPE_ORDER.filter(t => present.has(t.code)).forEach(t => {
    const c = el('button', 'chip on');
    c.dataset.type = t.code;
    const dot = el('span', 'cdot'); dot.style.background = t.color;
    c.append(dot, document.createTextNode(t.name));
    c.onclick = () => toggleSet(state.filters.types, t.code, c, true);
    wrap.append(c);
    if (!state.filters.types.has('__init')) state.filters.types.add(t.code);
  });
  state.filters.types.add('__init');
}

function toggleSet(set, key, chip, invertClass) {
  if (set.has(key)) { set.delete(key); chip.classList.remove('on'); }
  else { set.add(key); chip.classList.add('on'); }
  render();
}

function wireControls() {
  $$('#dirFilter .chip').forEach(c => {
    c.onclick = () => toggleSet(state.filters.dirs, c.dataset.dir, c);
  });
  $('#search').oninput = e => { state.filters.q = e.target.value.trim(); render(); };
  $$('#viewToggle button').forEach(b => {
    b.onclick = () => {
      state.view = b.dataset.view;
      $$('#viewToggle button').forEach(x => x.classList.toggle('active', x === b));
      $('#mapView').classList.toggle('hide', state.view !== 'map');
      $('#boardView').classList.toggle('hide', state.view !== 'board');
      render();
    };
  });
  $('#refreshBtn').onclick = () => { state.countdown = REFRESH_MS / 1000; load(); };
  $('#pauseBtn').onclick = () => {
    state.paused = !state.paused;
    $('#pauseBtn').classList.toggle('active', state.paused);
    $('#pauseBtn').textContent = state.paused ? '▶ 再開' : '⏸ 停止';
    if (!state.paused) { state.countdown = REFRESH_MS / 1000; load(); }
  };
  document.addEventListener('click', e => {
    if (!e.target.closest('.train') && !e.target.closest('#detail')) hideDetail();
  });
}

// ---------- filtering ----------
function visibleTrains() {
  const f = state.filters;
  const q = f.q.toLowerCase();
  return (state.data?.trains || []).filter(t =>
    f.branches.has(t.branch) &&
    f.dirs.has(t.direction) &&
    (f.types.has(t.typeCode) || !TYPE_ORDER.some(x => x.code === t.typeCode)) &&
    (!q || t.trainNo.toLowerCase().includes(q) || (t.destination || '').toLowerCase().includes(q) ||
      (t.typeName || '').includes(f.q) || (t.locName || '').includes(f.q))
  );
}

// ---------- render ----------
function render() {
  if (!state.data) return;
  renderStatus();
  buildTypeFilter();
  const trains = visibleTrains();
  renderSummary(trains);
  updateCounts();
  if (state.view === 'map') renderMap(trains);
  else renderBoard(trains);
}

function renderStatus() {
  const d = state.data;
  document.getElementById('feedTime').textContent = d.updatedAt || '--:--:--';
  document.getElementById('staleFlag').innerHTML =
    d.stale ? '<span class="stale-flag">キャッシュ</span>' : '';

  const pills = document.getElementById('statusPills');
  pills.innerHTML = '';
  const svc = d.service || {};
  const mk = (label, s) => {
    const p = el('div', 'pill ' + (s?.level || 'error'));
    p.append(el('span', 'dot'));
    p.append(el('span', 'lbl', label));
    p.append(el('span', 'txt', s?.text || '—'));
    return p;
  };
  pills.append(mk('京王線', svc.keio), mk('井の頭線', svc.inokashira));

  const nb = document.getElementById('noticeBar');
  if (!d.systemOK) {
    nb.className = 'notice-bar alert';
    nb.textContent = '⚠ 現在システムメンテナンス中の可能性があります。';
  } else if (svc.notice) {
    const isAlert = svc.keio?.level !== 'normal' || svc.inokashira?.level !== 'normal';
    nb.className = 'notice-bar' + (isAlert ? ' alert' : '');
    nb.textContent = (isAlert ? '⚠ ' : 'ℹ ') + svc.notice;
  } else { nb.className = 'notice-bar'; nb.textContent = ''; }
}

function renderSummary(trains) {
  const c = state.data.counts || {};
  const late = trains.filter(t => t.delayMin > 0).length;
  const s = document.getElementById('summary');
  s.innerHTML = '';
  const item = (label, val, warn) => {
    const d = el('div', warn ? 'warnv' : '');
    d.append(el('b', null, String(val)), document.createTextNode(' ' + label));
    return d;
  };
  s.append(
    item('本表示', trains.length),
    item('走行中（全体）', c.total ?? 0),
    item('遅延あり', late, late > 0),
    item('最大遅れ(分)', c.maxLate ?? 0, (c.maxLate ?? 0) > 0),
  );
}

function updateCounts() {
  const all = state.data.trains || [];
  document.getElementById('cnt-up').textContent = all.filter(t => t.direction === 'up').length;
  document.getElementById('cnt-down').textContent = all.filter(t => t.direction === 'down').length;
  const byBranch = {};
  all.forEach(t => byBranch[t.branch] = (byBranch[t.branch] || 0) + 1);
  $$('[data-bc]').forEach(e => e.textContent = byBranch[e.dataset.bc] || 0);
}

// ---------- MAP ----------
function renderMap(trains) {
  const root = document.getElementById('mapView');
  root.innerHTML = '';
  const branches = state.meta.branches || [];
  const byBranch = {};
  trains.forEach(t => (byBranch[t.branch] ||= []).push(t));

  branches.forEach(b => {
    if (!state.filters.branches.has(b.Key)) return;
    const list = byBranch[b.Key] || [];
    const card = el('div', 'branch');

    const head = el('div', 'branch-head');
    const bm = el('span', 'bmark'); bm.style.background = b.Color;
    head.append(bm, el('h2', null, b.Name), el('span', 'bcount', `${list.length} 本`));
    const legend = el('div', 'legend');
    legend.innerHTML =
      '<span><i class="lu"></i>上り</span><span><i class="ld"></i>下り</span>' +
      '<span><i class="lg-at"></i>停車中</span><span><i class="lg-mid"></i>走行中</span>';
    head.append(legend);
    card.append(head);

    const scroll = el('div', 'strip-scroll');
    const strip = el('div', 'strip');
    const n = b.Stations.length;
    const PAD = 78; // horizontal inset so terminal pills don't clip the edge
    // per-station spacing: keep at least 52px so labels breathe; strip may scroll
    const minW = Math.max(720, PAD * 2 + (n - 1) * 54);
    strip.style.width = minW + 'px';
    const innerW = minW - PAD * 2;
    const xpx = frac => PAD + frac * innerW;

    strip.append(el('div', 'rail'));
    b.Stations.forEach((name, i) => {
      const x = xpx(n <= 1 ? 0 : i / (n - 1)) + 'px';
      const tick = el('div', 'tick' + (i === 0 || i === n - 1 ? ' major' : ''));
      tick.style.left = x; strip.append(tick);
      const lab = el('div', 'st-label', name); lab.style.left = x; strip.append(lab);
    });

    // rail markers: solid dot = stopped at a station, hollow diamond = in transit.
    const markers = el('div', 'rail-markers');
    const seen = new Set();
    list.forEach(t => {
      const key = t.pos.toFixed(2) + '/' + (t.atStation ? 1 : 0);
      if (seen.has(key)) return;
      seen.add(key);
      const m = el('div', 'rail-marker ' + (t.atStation ? 'at' : 'mid'));
      m.style.left = xpx(n <= 1 ? 0 : t.pos / (n - 1)) + 'px';
      markers.append(m);
    });
    strip.append(markers);

    const laneUp = el('div', 'lane-up');
    const laneDown = el('div', 'lane-down');
    placeLane(laneUp, list.filter(t => t.direction === 'up'), n, 'up', xpx);
    placeLane(laneDown, list.filter(t => t.direction === 'down'), n, 'down', xpx);
    strip.append(laneUp, laneDown);

    scroll.append(strip);
    card.append(scroll);
    root.append(card);
  });

  if (!root.children.length) root.append(el('div', 'empty', '該当する列車がありません'));
}

// place trains in a lane, packing into rows so nearby pills don't overlap
function placeLane(lane, trains, n, dir, xpx) {
  trains.sort((a, b) => a.pos - b.pos);
  const rows = []; // each row: last used position fraction
  const ROW_H = 26, GAP = 0.06; // min fractional gap between pills in a row
  trains.forEach(t => {
    const p = n <= 1 ? 0 : t.pos / (n - 1);
    let row = 0;
    while (row < rows.length && p < rows[row] + GAP) row++;
    rows[row] = p;
    const pill = trainPill(t, dir);
    pill.style.left = xpx(p) + 'px';
    if (dir === 'up') pill.style.bottom = (row * ROW_H) + 'px';
    else pill.style.top = (row * ROW_H) + 'px';
    lane.append(pill);
  });
}

function trainPill(t, dir) {
  const pill = el('div', `train ${dir} ` + (t.atStation ? 'stopped' : 'moving'));
  pill.title = t.atStation ? `${t.locName} に停車中` : `${t.locName} を走行中`;
  const ico = el('span', 'ico', t.typeIcon || '•');
  ico.style.background = t.color; ico.style.color = t.textOnColor;
  pill.append(ico);
  // status pip: solid green dot = stopped at a station, hollow ring = moving
  pill.append(el('span', 'pip ' + (t.atStation ? 'pip-at' : 'pip-mid')));
  const arrow = el('span', 'dir-arrow', dir === 'up' ? '↑' : '↓');
  pill.append(arrow);
  pill.append(el('span', 'dst', t.destination));
  if (t.delayMin > 0) pill.append(el('span', 'lateflag', '+' + t.delayMin));
  pill.onclick = (e) => { e.stopPropagation(); showDetail(t, e.currentTarget); };
  return pill;
}

// ---------- BOARD ----------
let sortKey = 'branch', sortDir = 1;
function renderBoard(trains) {
  const root = document.getElementById('boardView');
  root.innerHTML = '';
  if (!trains.length) { root.append(el('div', 'empty', '該当する列車がありません')); return; }

  const cols = [
    ['type', '種別'], ['trainNo', '列車番号'], ['direction', '方向'],
    ['destination', '行先'], ['locName', '現在地'], ['delayMin', '遅れ'], ['info', '案内'],
  ];
  const table = el('table', 'board');
  const thead = el('tr');
  cols.forEach(([k, label]) => {
    const th = el('th', null, label + ' ');
    if (sortKey === k) th.append(el('span', 'arrow', sortDir > 0 ? '▲' : '▼'));
    th.onclick = () => { if (sortKey === k) sortDir *= -1; else { sortKey = k; sortDir = 1; } renderBoard(visibleTrains()); };
    thead.append(th);
  });
  table.append(el('thead').appendChild(thead).parentNode);

  const sorted = [...trains].sort(cmp);
  const tb = el('tbody');
  sorted.forEach(t => {
    const tr = el('tr');
    const typeTd = el('td');
    const tc = el('div', 'type-cell');
    const bd = el('span', 'badge', t.typeIcon || '•');
    bd.style.background = t.color; bd.style.color = t.textOnColor;
    tc.append(bd, el('span', 'tname', t.typeName));
    typeTd.append(tc); tr.append(typeTd);

    tr.append(td('mono', t.trainNo));

    const dcell = el('td');
    const dc = el('span', 'dircell ' + t.direction, (t.direction === 'up' ? '↑ ' : '↓ ') + t.dirLabel);
    dcell.append(dc); tr.append(dcell);

    tr.append(td(null, t.destination));

    const loc = el('td');
    loc.append(
      el('span', 'stbadge ' + (t.atStation ? 'at' : 'mid'), t.atStation ? '停車' : '走行'),
      el('span', t.atStation ? 'loc-at' : 'loc-between', t.locName));
    tr.append(loc);

    tr.append(td(t.delayMin > 0 ? 'delay-pos' : 'delay-0', t.delayMin > 0 ? '+' + t.delayMin + '分' : '定時'));
    tr.append(td('info-cell', t.info || ''));

    tr.onclick = (e) => showDetail(t, e.currentTarget);
    tb.append(tr);
  });
  table.append(tb);
  const wrap = el('div', 'board-wrap'); wrap.append(table);
  root.append(wrap);
}

function td(cls, txt) { const d = el('td'); if (cls) { d.append(el('span', cls, txt)); } else d.textContent = txt; return d; }

function cmp(a, b) {
  let av = a[sortKey], bv = b[sortKey];
  if (sortKey === 'type') { av = a.typeCode; bv = b.typeCode; }
  if (sortKey === 'delayMin') return (a.delayMin - b.delayMin) * sortDir;
  if (sortKey === 'branch') {
    const bi = k => (state.meta.branches.findIndex(x => x.Key === k));
    const d = (bi(a.branch) - bi(b.branch));
    if (d) return d; return (a.pos - b.pos);
  }
  av = (av ?? '').toString(); bv = (bv ?? '').toString();
  return av.localeCompare(bv, 'ja') * sortDir;
}

// ---------- detail popover ----------
function showDetail(t, anchor) {
  const d = document.getElementById('detail');
  const loc = t.atStation ? `${t.locName}（停車中）` : `${t.locName}（走行中）`;
  d.innerHTML = '';
  const h = el('h3');
  const bd = el('span', 'badge', t.typeIcon || '•');
  bd.style.background = t.color; bd.style.color = t.textOnColor;
  h.append(bd, document.createTextNode(`${t.typeName} ${t.destination} 行`));
  d.append(h);
  const rows = [
    ['列車番号', t.trainNo],
    ['方向', (t.direction === 'up' ? '↑ ' : '↓ ') + t.dirLabel],
    ['種別', t.typeName + (t.typeNameEn ? ` (${t.typeNameEn})` : '')],
    ['行先', t.destination],
    ['現在地', loc],
    ['路線', t.branchName],
    ['遅れ', t.delayMin > 0 ? `${t.delayMin} 分` : '定時運行'],
  ];
  rows.forEach(([k, v]) => {
    const r = el('div', 'row');
    r.append(el('span', 'k', k), el('span', 'v', v));
    d.append(r);
  });
  if (t.info) { const inf = el('div', 'info', 'ℹ ' + t.info); d.append(inf); }

  d.style.display = 'block';
  const rect = anchor.getBoundingClientRect();
  const dw = 340, dh = d.offsetHeight;
  let x = rect.left, y = rect.bottom + 8;
  if (x + dw > window.innerWidth - 12) x = window.innerWidth - dw - 12;
  if (y + dh > window.innerHeight - 12) y = Math.max(12, rect.top - dh - 8);
  d.style.left = Math.max(12, x) + 'px';
  d.style.top = y + 'px';
}
function hideDetail() { document.getElementById('detail').style.display = 'none'; }
