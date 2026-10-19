(function () {
  'use strict';
  // Draws the path tables from the data block, one page at a time.
  // Everything from the data is put on the page with textContent only.
  var holder = document.getElementById('pk-data');
  if (!holder) { return; }
  var D = JSON.parse(holder.textContent);
  var S = D.s;
  var W = D.w;
  var SIZES = [25, 50, 100];
  var STATUS = ['Missed', 'Covered', 'Covered (stale trace)'];
  var MARK = ['✗', '✓', '✓'];
  var query = '';
  var states = [];
  var audit = null;
  var printing = false;

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) { e.className = cls; }
    if (text !== undefined && text !== null) { e.appendChild(document.createTextNode(String(text))); }
    return e;
  }
  function add(parent) {
    for (var i = 1; i < arguments.length; i++) {
      if (arguments[i]) { parent.appendChild(arguments[i]); }
    }
    return parent;
  }
  function clear(e) {
    while (e.firstChild) { e.removeChild(e.firstChild); }
  }
  function compareText(a, b) {
    if (a < b) { return -1; }
    if (a > b) { return 1; }
    return 0;
  }
  function str(i) {
    if (i < 0) { return ''; }
    return S[i] === '\u0000' ? '' : S[i];
  }
  function pathText(r) {
    var out = [];
    for (var i = 0; i < r[7].length; i++) { out.push(str(r[7][i])); }
    return out.join('--> ');
  }
  function haystack(r) {
    if (r.q === undefined) {
      r.q = (r[1] + ' ' + str(r[3]) + ' ' + str(r[4]) + ' ' + str(r[5]) + ' ' + str(r[6]) + ' ' + pathText(r)).toLowerCase();
    }
    return r.q;
  }
  function short(text) {
    if (text.length <= 110) { return text; }
    return text.slice(0, 107) + '...';
  }
  function select(id, labelText, values, current, onChange) {
    var wrap = el('label', 'ctl');
    wrap.setAttribute('for', id);
    add(wrap, el('span', null, labelText));
    var s = el('select');
    s.id = id;
    for (var i = 0; i < values.length; i++) {
      var o = el('option', null, values[i][1]);
      o.value = values[i][0];
      if (values[i][0] === current) { o.selected = true; }
      s.appendChild(o);
    }
    s.addEventListener('change', function () { onChange(s.value); });
    wrap.appendChild(s);
    return wrap;
  }
  function copyText(text, button) {
    function done() {
      button.textContent = 'Copied';
      setTimeout(function () { button.textContent = 'Copy'; }, 1500);
    }
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done, function () {
        button.textContent = 'Copy failed';
        setTimeout(function () { button.textContent = 'Copy'; }, 1500);
      });
      return;
    }
    var t = el('textarea');
    t.value = text;
    document.body.appendChild(t);
    t.select();
    try { document.execCommand('copy'); done(); } catch (e) { /* nothing to do */ }
    document.body.removeChild(t);
  }

  // A pager: Prev, page numbers with gaps, Next.
  function pager(total, st, onGo) {
    var pages = Math.max(1, Math.ceil(total / st.size));
    if (st.page > pages) { st.page = pages; }
    var nav = el('nav', 'pager');
    nav.setAttribute('aria-label', 'Pages');
    if (pages <= 1) { return nav; }
    function btn(label, page, current, disabled) {
      var b = el('button', current ? 'cur' : null, label);
      b.type = 'button';
      if (current) { b.setAttribute('aria-current', 'page'); }
      if (disabled) { b.disabled = true; }
      b.addEventListener('click', function () { st.page = page; onGo(); });
      return b;
    }
    add(nav, btn('Prev', st.page - 1, false, st.page === 1));
    var last = 0;
    for (var p = 1; p <= pages; p++) {
      if (p === 1 || p === pages || (p >= st.page - 1 && p <= st.page + 1)) {
        if (p - last > 1) { add(nav, el('span', 'gap', '…')); }
        add(nav, btn(String(p), p, p === st.page, false));
        last = p;
      }
    }
    add(nav, btn('Next', st.page + 1, false, st.page === pages));
    return nav;
  }

  function showing(st, total, none) {
    if (total === 0) { return none; }
    var from = (st.page - 1) * st.size + 1;
    var to = Math.min(total, st.page * st.size);
    return 'Showing ' + from + '–' + to + ' of ' + total;
  }

  // ---- the path table of one workflow ----
  function filtered(wi) {
    var st = states[wi];
    var q = query.toLowerCase();
    var rows = [];
    var all = W[wi].r;
    for (var i = 0; i < all.length; i++) {
      var r = all[i];
      if (st.status === 'covered' && r[2] === 0) { continue; }
      if (st.status === 'missed' && r[2] !== 0) { continue; }
      if (st.bucket !== '' && str(r[3]) !== st.bucket) { continue; }
      if (st.label !== '' && str(r[4]) !== st.label) { continue; }
      if (q !== '' && haystack(r).indexOf(q) < 0) { continue; }
      rows.push(r);
    }
    var key = st.sort;
    var dir = st.dir;
    function val(r) {
      if (key === 'id') { return r[1]; }
      if (key === 'st') { return r[2]; }
      if (key === 'b') { return str(r[3]); }
      if (key === 'l') { return str(r[4]); }
      return r[0];
    }
    rows.sort(function (a, b) {
      var x = val(a);
      var y = val(b);
      if (x < y) { return -dir; }
      if (x > y) { return dir; }
      return a[0] - b[0];
    });
    return rows;
  }

  function stepsList(r) {
    var ol = el('ol', 'steps');
    var parts = pathText(r).split('--> ');
    for (var i = 0; i < parts.length; i++) {
      var t = parts[i];
      if (i === 0 && t.indexOf('Start -> ') === 0) { t = t.slice(9); }
      var li = el('li');
      var cut = t.lastIndexOf(' --');
      if (i < parts.length - 1 && cut > 0) {
        add(li, el('span', 'decision', t.slice(0, cut)), el('span', 'arrow', ' → '), el('span', 'exit', t.slice(cut + 3)));
      } else {
        li.appendChild(el('span', 'decision', t));
      }
      ol.appendChild(li);
    }
    return ol;
  }

  function detailsCell(r, cols) {
    var td = el('td', 'detail');
    td.colSpan = cols;
    var dl = el('dl');
    function row(k, v) {
      if (v === '') { return; }
      add(dl, el('dt', null, k), el('dd', null, v));
    }
    row('Path id', r[1]);
    row('Status', STATUS[r[2]]);
    if (r.length > 8 && r[8] > 0) { row('Same behaviour as', 'path ' + r[8] + ' (it makes the same steps outside the workflow)'); }
    if (D.c) {
      row('Bucket', str(r[3]));
      row('Label', str(r[4]));
      row('Decided by', str(r[5]) === '' ? 'nobody: no detector, rule, override or comment applies' : str(r[5]));
      row('Why', str(r[6]));
    }
    td.appendChild(dl);
    add(td, el('h4', null, 'The path, step by step'), stepsList(r));
    return td;
  }

  function buildTable(wi, root) {
    var w = W[wi];
    var st = states[wi];
    var ctl = el('div', 'controls');
    var id = 'f' + wi + '-';
    if (D.m) {
      add(ctl, select(id + 's', 'Status', [['', 'All'], ['covered', 'Covered'], ['missed', 'Missed']], st.status, function (v) { st.status = v; st.page = 1; draw(wi); }));
    }
    if (D.c) {
      var buckets = {};
      var labels = {};
      for (var i = 0; i < w.r.length; i++) {
        buckets[str(w.r[i][3])] = true;
        if (str(w.r[i][4]) !== '') { labels[str(w.r[i][4])] = true; }
      }
      var bo = [['', 'All']];
      Object.keys(buckets).sort(compareText).forEach(function (k) { bo.push([k, k]); });
      var lo = [['', 'All']];
      Object.keys(labels).sort(compareText).forEach(function (k) { lo.push([k, k]); });
      add(ctl, select(id + 'b', 'Bucket', bo, st.bucket, function (v) { st.bucket = v; st.page = 1; draw(wi); }));
      add(ctl, select(id + 'l', 'Reason label', lo, st.label, function (v) { st.label = v; st.page = 1; draw(wi); }));
    }
    add(ctl, select(id + 'n', 'Rows per page', SIZES.map(function (n) { return [String(n), String(n)]; }), String(st.size), function (v) { st.size = parseInt(v, 10); st.page = 1; draw(wi); }));
    var live = el('p', 'showing');
    live.setAttribute('aria-live', 'polite');
    var box = el('div', 'tablebox');
    var pg = el('div', 'pagerbox');
    add(root, ctl, live, box, pg);
    st.ui = { live: live, box: box, pager: pg };
  }

  function draw(wi) {
    var st = states[wi];
    if (!st.ui) { return; }
    var w = W[wi];
    var rows = filtered(wi);
    var view = { size: st.size, page: st.page };
    if (printing) {
      view.size = rows.length + 1;
      view.page = 1;
    } else {
      var pages = Math.max(1, Math.ceil(rows.length / st.size));
      if (st.page > pages) { st.page = pages; }
      view.page = st.page;
    }
    var from = (view.page - 1) * view.size;
    var chunk = rows.slice(from, from + view.size);
    st.ui.live.textContent = showing(view, rows.length, w.r.length === 0 ? 'This workflow has no paths.' : 'No paths match the filters.');
    var cols = [['n', '#'], ['id', 'Path id']];
    if (D.m) { cols.push(['st', 'Status']); }
    if (D.c) { cols.push(['b', 'Bucket'], ['l', 'Reason label'], ['', 'Short explanation']); }
    cols.push(['', 'Details']);
    var table = el('table', 'paths');
    add(table, el('caption', 'sr', 'Paths of ' + w.n));
    var tr = el('tr');
    cols.forEach(function (c) {
      var th = el('th');
      th.scope = 'col';
      if (c[0] === '') {
        th.textContent = c[1];
      } else {
        var b = el('button', 'sort', c[1] + (st.sort === c[0] ? (st.dir > 0 ? ' ▲' : ' ▼') : ''));
        b.type = 'button';
        b.addEventListener('click', function () {
          if (st.sort === c[0]) { st.dir = -st.dir; } else { st.sort = c[0]; st.dir = 1; }
          draw(wi);
        });
        th.setAttribute('aria-sort', st.sort === c[0] ? (st.dir > 0 ? 'ascending' : 'descending') : 'none');
        th.appendChild(b);
      }
      tr.appendChild(th);
    });
    add(table, add(el('thead'), tr));
    var body = el('tbody');
    chunk.forEach(function (r) {
      var row = el('tr', 'p' + (D.m ? (r[2] === 0 ? ' missed' : ' covered') : ''));
      row.id = 'row-' + wi + '-' + r[0];
      function cell(label, node) {
        var td = el('td');
        td.setAttribute('data-label', label);
        if (typeof node === 'string') { td.textContent = node; } else if (node) { td.appendChild(node); }
        return td;
      }
      row.appendChild(cell('#', String(r[0])));
      var idCell = el('span', 'idcell');
      add(idCell, el('code', null, r[1] === '' ? '(no id)' : r[1]));
      if (r[1] !== '') {
        var cp = el('button', 'copy', 'Copy');
        cp.type = 'button';
        cp.setAttribute('aria-label', 'Copy path id ' + r[1]);
        cp.addEventListener('click', function () { copyText(r[1], cp); });
        idCell.appendChild(cp);
      }
      row.appendChild(cell('Path id', idCell));
      if (D.m) {
        row.appendChild(cell('Status', el('span', 'status s' + r[2], MARK[r[2]] + ' ' + STATUS[r[2]])));
      }
      if (D.c) {
        row.appendChild(cell('Bucket', el('span', 'bucket b-' + str(r[3]), str(r[3]))));
        row.appendChild(cell('Reason label', str(r[4]) === '' ? 'none' : str(r[4])));
        row.appendChild(cell('Short explanation', (str(r[3]) === 'testable' && str(r[4]) === '') ? 'Ordinary path: nothing marks it impossible.' : short(str(r[6]))));
      }
      var open = st.open[r[0]] === true;
      var tb = el('button', 'toggle', open ? 'Hide' : 'Show');
      tb.type = 'button';
      tb.setAttribute('aria-expanded', open ? 'true' : 'false');
      tb.addEventListener('click', function () {
        st.open[r[0]] = !open;
        draw(wi);
      });
      row.appendChild(cell('Details', tb));
      body.appendChild(row);
      if (open) {
        var d = el('tr', 'det');
        d.appendChild(detailsCell(r, cols.length));
        body.appendChild(d);
      }
    });
    table.appendChild(body);
    clear(st.ui.box);
    if (chunk.length > 0) { st.ui.box.appendChild(table); }
    clear(st.ui.pager);
    var total = rows.length;
    if (!printing) { st.ui.pager.appendChild(pager(total, st, function () { draw(wi); })); }
    var sum = document.getElementById('match-' + wi);
    if (sum) { sum.textContent = query === '' ? '' : total + ' of ' + w.r.length + ' match'; }
  }

  // ---- the stage paths of one workflow: pick a stage, page through its paths ----
  function buildStages(wi) {
    var w = W[wi];
    var root = document.querySelector('[data-sg="' + wi + '"]');
    if (!w.sg || !root || root.firstChild) { return; }
    var st = { stage: 0, status: '', page: 1, size: 25 };
    var ctl = el('div', 'controls');
    var live = el('p', 'showing');
    live.setAttribute('aria-live', 'polite');
    var box = el('div', 'tablebox');
    var pg = el('div', 'pagerbox');
    var opts = [];
    w.sg.forEach(function (s, k) { if (!s.b) { opts.push([String(k), 'Stage ' + (k + 1) + ' (' + s.n + ' paths)']); } });
    if (opts.length === 0) { return; }
    st.stage = parseInt(opts[0][0], 10);
    function drawStage() {
      var rows = w.sg[st.stage].p.filter(function (r) {
        return st.status === '' || (st.status === 'covered' ? r[1] === 1 : r[1] === 0);
      });
      var pages = Math.max(1, Math.ceil(rows.length / st.size));
      if (st.page > pages) { st.page = pages; }
      live.textContent = showing(st, rows.length, 'No stage paths match the filters.');
      clear(box);
      clear(pg);
      var chunk = printing ? rows : rows.slice((st.page - 1) * st.size, st.page * st.size);
      if (chunk.length > 0) {
        var table = el('table', 'paths');
        add(table, el('caption', 'sr', 'Paths of stage ' + (st.stage + 1) + ' of ' + w.n));
        var tr = el('tr');
        ['Stage path id', 'Status', 'Bucket', 'Reason label', 'Path'].forEach(function (h) {
          var th = el('th', null, h);
          th.scope = 'col';
          tr.appendChild(th);
        });
        add(table, add(el('thead'), tr));
        var body = el('tbody');
        chunk.forEach(function (r) {
          var row = el('tr', 'p' + (r[1] === 0 ? ' missed' : ' covered'));
          function cell(label, node) {
            var td = el('td');
            td.setAttribute('data-label', label);
            if (typeof node === 'string') { td.textContent = node; } else { td.appendChild(node); }
            row.appendChild(td);
          }
          cell('Stage path id', el('code', null, r[0]));
          cell('Status', el('span', 'status s' + r[1], MARK[r[1]] + ' ' + STATUS[r[1]]));
          cell('Bucket', r[2] < 0 ? '' : el('span', 'bucket b-' + str(r[2]), str(r[2])));
          cell('Reason label', r[3] < 0 || str(r[3]) === '' ? 'none' : str(r[3]));
          cell('Path', short(str(r[4])));
          body.appendChild(row);
        });
        add(table, body);
        box.appendChild(table);
      }
      if (!printing) { pg.appendChild(pager(rows.length, st, drawStage)); }
    }
    add(ctl, select('sg' + wi + 'k', 'Stage', opts, String(st.stage), function (v) { st.stage = parseInt(v, 10); st.page = 1; drawStage(); }));
    add(ctl, select('sg' + wi + 's', 'Status', [['', 'All'], ['covered', 'Covered'], ['missed', 'Missed']], st.status, function (v) { st.status = v; st.page = 1; drawStage(); }));
    add(ctl, select('sg' + wi + 'n', 'Rows per page', SIZES.map(function (n) { return [String(n), String(n)]; }), String(st.size), function (v) { st.size = parseInt(v, 10); st.page = 1; drawStage(); }));
    add(root, ctl, live, box, pg);
    drawStage();
  }

  function buildWorkflow(wi) {
    var st = states[wi];
    buildStages(wi);
    if (st.ui) { return; }
    var root = document.querySelector('[data-pt="' + wi + '"]');
    if (!root) { return; }
    clear(root);
    buildTable(wi, root);
    draw(wi);
  }

  // ---- the detector audit ----
  function drawAudit() {
    var root = document.getElementById('audit-root');
    if (!root || !D.a) { return; }
    var q = query.toLowerCase();
    var rows = D.a.filter(function (a) {
      if (q === '') { return true; }
      var h = (W[a[0]].n + ' ' + a[1] + ' ' + str(a[2]) + ' ' + str(a[3]) + ' ' + str(a[4]) + ' ' + str(a[5])).toLowerCase();
      return h.indexOf(q) >= 0;
    });
    if (!audit) { audit = { page: 1, size: 25 }; }
    var st = audit;
    clear(root);
    var ctl = el('div', 'controls');
    add(ctl, select('audit-size', 'Rows per page', SIZES.map(function (n) { return [String(n), String(n)]; }), String(st.size), function (v) { st.size = parseInt(v, 10); st.page = 1; drawAudit(); }));
    var apages = Math.max(1, Math.ceil(rows.length / st.size));
    if (st.page > apages) { st.page = apages; }
    var live = el('p', 'showing', showing(st, rows.length, 'No audit rows match the search.'));
    live.setAttribute('aria-live', 'polite');
    add(root, ctl, live);
    var from = (st.page - 1) * st.size;
    var chunk = printing ? rows : rows.slice(from, from + st.size);
    if (chunk.length > 0) {
      var table = el('table', 'audit-table');
      add(table, el('caption', 'sr', 'Paths that a detector marked cannot-be-tested'));
      var tr = el('tr');
      ['Workflow', 'Path', 'Label', 'Detector', 'Confidence', 'Reason'].forEach(function (h) {
        var th = el('th', null, h);
        th.scope = 'col';
        tr.appendChild(th);
      });
      add(table, add(el('thead'), tr));
      var body = el('tbody');
      chunk.forEach(function (a) {
        var row = el('tr');
        function cell(label, node) {
          var td = el('td');
          td.setAttribute('data-label', label);
          if (typeof node === 'string') { td.textContent = node; } else { td.appendChild(node); }
          row.appendChild(td);
        }
        cell('Workflow', W[a[0]].n);
        var link = el('a', null, String(a[1]));
        link.href = '#use-' + (a[0] + 1) + '-' + a[1];
        cell('Path', link);
        var lab = el('a', 'label', str(a[2]));
        lab.href = '#def-' + str(a[2]);
        cell('Label', lab);
        cell('Detector', str(a[3]));
        cell('Confidence', str(a[4]));
        cell('Reason', str(a[5]));
        body.appendChild(row);
      });
      add(table, body);
      add(root, add(el('div', 'tablebox'), table));
    }
    if (!printing) { add(root, pager(rows.length, st, drawAudit)); }
  }

  // ---- jump to one path: #use-<workflow number>-<path number> ----
  function jump() {
    var h = location.hash;
    if (h.indexOf('#use-') !== 0) { return; }
    var parts = h.slice(5).split('-');
    var wi = parseInt(parts[0], 10) - 1;
    var num = parseInt(parts[1], 10);
    if (!(wi >= 0 && wi < W.length) || !(num >= 1)) { return; }
    var st = states[wi];
    st.status = '';
    st.bucket = '';
    st.label = '';
    st.sort = 'n';
    st.dir = 1;
    if (query !== '') {
      query = '';
      states.forEach(function (other, oi) { if (oi !== wi) { other.page = 1; if (other.ui) { draw(oi); } } });
      if (audit) { audit.page = 1; }
      drawAudit();
    }
    var q = document.getElementById('q');
    if (q) { q.value = ''; }
    var d = document.getElementById('wf-' + wi);
    if (d) { d.open = true; }
    if (st.ui) {
      // rebuild the filter controls so they show "All"
      clear(document.querySelector('[data-pt="' + wi + '"]'));
      st.ui = null;
    }
    var idx = 0;
    for (var i = 0; i < W[wi].r.length; i++) {
      if (W[wi].r[i][0] === num) { idx = i; break; }
    }
    st.page = Math.floor(idx / st.size) + 1;
    st.open[num] = true;
    buildWorkflow(wi);
    var row = document.getElementById('row-' + wi + '-' + num);
    if (row) {
      row.classList.add('target');
      row.scrollIntoView();
    }
  }

  function start() {
    W.forEach(function (w, wi) {
      states.push({ status: '', bucket: '', label: '', sort: 'n', dir: 1, page: 1, size: 50, open: {}, ui: null });
      var d = document.getElementById('wf-' + wi);
      if (!d) { return; }
      d.addEventListener('toggle', function () { if (d.open) { buildWorkflow(wi); } });
      if (d.open) { buildWorkflow(wi); }
    });
    var q = document.getElementById('q');
    if (q) {
      q.parentNode.hidden = false;
      var timer = null;
      q.addEventListener('input', function () {
        clearTimeout(timer);
        timer = setTimeout(function () {
          query = q.value.trim();
          states.forEach(function (st, wi) { st.page = 1; if (st.ui) { draw(wi); } });
          if (audit) { audit.page = 1; }
          drawAudit();
        }, 150);
      });
    }
    drawAudit();
    document.documentElement.className = 'js';
    window.addEventListener('hashchange', jump);
    window.addEventListener('beforeprint', function () {
      printing = true;
      W.forEach(function (w, wi) {
        var d = document.getElementById('wf-' + wi);
        if (d) { d.open = true; }
        if (states[wi].ui) { draw(wi); } else { buildWorkflow(wi); }
      });
      drawAudit();
    });
    window.addEventListener('afterprint', function () {
      printing = false;
      states.forEach(function (st, wi) { if (st.ui) { draw(wi); } });
      drawAudit();
    });
    jump();
  }
  start();
}());
