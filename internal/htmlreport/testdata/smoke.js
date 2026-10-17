// Runs the page's own script against a tiny DOM stub and drives it the way a
// reader would. Usage: node smoke.js < page.html   (the page comes from standard
// input; prints one JSON object)
'use strict';
const fs = require('fs');
const html = fs.readFileSync(0, 'utf8');

const registry = {};
class Node_ {
  constructor(tag) { this.tagName = tag; this.children = []; this.attrs = {}; this.listeners = {}; this.className = ''; this.parentNode = null; this._text = null; this.id = ''; this.hidden = false; this.open = false; this.value = ''; }
  appendChild(c) { if (c.parentNode) { c.parentNode.removeChild(c); } c.parentNode = this; this.children.push(c); return c; }
  removeChild(c) { const i = this.children.indexOf(c); if (i >= 0) { this.children.splice(i, 1); } c.parentNode = null; return c; }
  get firstChild() { return this.children[0] || null; }
  setAttribute(k, v) { this.attrs[k] = String(v); }
  getAttribute(k) { return this.attrs[k]; }
  addEventListener(t, f) { (this.listeners[t] = this.listeners[t] || []).push(f); }
  fire(t) { (this.listeners[t] || []).forEach((f) => f({})); }
  get classList() { const self = this; return { add(c) { self.className += ' ' + c; } }; }
  scrollIntoView() { this.scrolled = true; }
  select() {}
  get textContent() { if (this._text !== null) { return this._text; } return this.children.map((c) => c.textContent).join(''); }
  set textContent(v) { this.children = []; this._text = String(v); }
  set innerHTML(v) { throw new Error('innerHTML must never be used'); }
  set outerHTML(v) { throw new Error('outerHTML must never be used'); }
}
class Text_ extends Node_ { constructor(t) { super('#text'); this._text = t; } }
function walk(n, f) { f(n); n.children.forEach((c) => walk(c, f)); }
function find(root, pred) { const out = []; walk(root, (n) => { if (pred(n)) { out.push(n); } }); return out; }
const has = (n, c) => (' ' + n.className + ' ').indexOf(' ' + c + ' ') >= 0;

const body = new Node_('body');
const documentElement = new Node_('html');
// the elements the page has before its script runs
const ids = {};
html.replace(/\bid="((?:wf|match)-\d+|q|audit-root)"/g, (m, id) => { ids[id] = true; return m; });
Object.keys(ids).forEach((id) => { const e = new Node_(id.startsWith('wf-') ? 'details' : 'div'); e.id = id; registry[id] = e; });
if (registry.q) { const wrap = new Node_('div'); wrap.hidden = true; wrap.appendChild(registry.q); }
const pts = {};
html.replace(/data-pt="(\d+)"/g, (m, n) => { const e = new Node_('div'); pts[n] = e; return m; });
const m = /<script id="pk-data" type="application\/json">([\s\S]*?)<\/script>/.exec(html);
const dataEl = new Node_('script'); dataEl.textContent = m[1]; registry['pk-data'] = dataEl;
const first = Object.keys(registry).filter((k) => k.startsWith('wf-'));
// the server opens the first workflow only
first.forEach((k, i) => { registry[k].open = (k === 'wf-0'); });

const globalListeners = {};
global.document = {
  getElementById: (id) => { if (registry[id]) { return registry[id]; } let hit = null; Object.keys(pts).forEach((k) => walk(pts[k], (n) => { if (n.id === id) { hit = n; } })); return hit; },
  querySelector: (sel) => { const mm = /\[data-pt="(\d+)"\]/.exec(sel); return mm ? pts[mm[1]] || null : null; },
  createElement: (t) => new Node_(t),
  createTextNode: (t) => new Text_(t),
  documentElement,
  body,
  execCommand: () => true,
};
global.window = { addEventListener: (t, f) => { (globalListeners[t] = globalListeners[t] || []).push(f); } };
global.location = { hash: '' };
Object.defineProperty(global, 'navigator', { value: { clipboard: { writeText: () => Promise.resolve() } }, configurable: true });
const timers = [];
global.setTimeout = (f) => { timers.push(f); return timers.length; };
global.clearTimeout = () => {};

const code = /<script id="pk-code">([\s\S]*?)<\/script>/.exec(html)[1];
(0, eval)(code);

const out = { ok: true, checks: {} };
const rowsOf = (root) => find(root, (n) => n.tagName === 'tr' && has(n, 'p'));
const showing = (root) => find(root, (n) => has(n, 'showing'))[0].textContent;
const root0 = pts['0'];
out.checks.firstRows = rowsOf(root0).length;
out.checks.firstShowing = showing(root0);
out.checks.jsClass = documentElement.className;
out.checks.searchVisible = registry.q ? registry.q.parentNode.hidden === false : null;
const selects = find(root0, (n) => n.tagName === 'select');
const byLabel = (label) => selects.find((s) => s.id.endsWith(label));
function choose(sel, v) { sel.value = v; sel.fire('change'); }
const size = byLabel('-n');
choose(size, '25');
out.checks.size25Rows = rowsOf(root0).length;
const nextBtn = () => find(root0, (n) => n.tagName === 'button' && n.textContent === 'Next')[0];
if (nextBtn() && !nextBtn().disabled) { nextBtn().fire('click'); out.checks.page2Showing = showing(root0); }
const status = byLabel('-s');
if (status) {
  choose(status, 'missed');
  const rows = rowsOf(root0);
  out.checks.missedRows = rows.length;
  out.checks.missedAllMissed = rows.every((r) => has(r, 'missed'));
  out.checks.missedShowing = showing(root0);
  choose(status, '');
}
// the search box
if (registry.q) {
  registry.q.value = 'zzzz-no-such-path';
  registry.q.fire('input');
  timers.forEach((f) => f());
  out.checks.noMatchShowing = showing(root0);
  out.checks.noMatchRows = rowsOf(root0).length;
  registry.q.value = '';
  registry.q.fire('input');
  timers.forEach((f) => f());
}
// open a row's details
const toggle = find(root0, (n) => n.tagName === 'button' && n.textContent === 'Show')[0];
if (toggle) {
  toggle.fire('click');
  out.checks.detailRows = find(root0, (n) => n.tagName === 'tr' && has(n, 'det')).length;
  out.checks.detailHasSteps = find(root0, (n) => n.tagName === 'ol' && has(n, 'steps')).length;
}
// the audit: go to its page 2, then search: the audit must go back to page 1
if (registry['audit-root']) {
  const aroot = registry['audit-root'];
  const next = find(aroot, (n) => n.tagName === 'button' && n.textContent === 'Next')[0];
  if (next) {
    next.fire('click');
    out.checks.auditPage2Showing = find(aroot, (n) => has(n, 'showing'))[0].textContent;
    registry.q.value = '1';
    registry.q.fire('input');
    timers.forEach((f) => f());
    out.checks.auditAfterSearchShowing = find(aroot, (n) => has(n, 'showing'))[0].textContent;
    registry.q.value = '';
    registry.q.fire('input');
    timers.forEach((f) => f());
  }
}
// a search, then a jump: other open workflows must not keep the old filter
if (registry.q && Object.keys(pts).length > 1) {
  registry['wf-1'].open = true;
  registry['wf-1'].fire('toggle');
  registry.q.value = 'zzzz-no-such-path';
  registry.q.fire('input');
  timers.forEach((f) => f());
  out.checks.otherBeforeJump = find(pts['1'], (n) => has(n, 'showing'))[0].textContent;
  location.hash = '#use-1-3';
  (globalListeners.hashchange || []).forEach((f) => f({}));
  out.checks.otherAfterJump = find(pts['1'], (n) => has(n, 'showing'))[0].textContent;
}
// jump to a path by its link
const lastWf = Object.keys(pts).length - 1;
location.hash = '#use-' + (lastWf + 1) + '-1';
(globalListeners.hashchange || []).forEach((f) => f({}));
const target = find(pts[String(lastWf)], (n) => has(n, 'target'));
out.checks.jumpTarget = target.length;
// printing shows every row
(globalListeners.beforeprint || []).forEach((f) => f({}));
out.checks.printRows = rowsOf(root0).length;
(globalListeners.afterprint || []).forEach((f) => f({}));
out.checks.afterPrintRows = rowsOf(root0).length;
out.checks.audit = registry['audit-root'] ? find(registry['audit-root'], (n) => n.tagName === 'tr').length : null;
console.log(JSON.stringify(out));
