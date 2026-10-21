// Runs the page script against a small DOM stub and drives the parts made for
// people who test the way a keyboard user would: focus on an "i" marker opens
// its tooltip, Escape closes it, a click outside closes it, "Only missed"
// filters a checklist, "Show more" pages it. Usage: node ui.js < page.html
'use strict';
const fs = require('fs');
const html = fs.readFileSync(0, 'utf8');

class El {
  constructor(tag) { this.tagName = tag.toUpperCase(); this.children = []; this.attrs = {}; this.listeners = {}; this.className = ''; this.parentNode = null; this._text = ''; this.id = ''; this.hidden = false; this.open = false; this.checked = false; this.style = {}; }
  appendChild(c) { c.parentNode = this; this.children.push(c); return c; }
  setAttribute(k, v) { this.attrs[k] = String(v); }
  getAttribute(k) { return this.attrs[k]; }
  addEventListener(t, f) { (this.listeners[t] = this.listeners[t] || []).push(f); }
  fire(t, ev) { (this.listeners[t] || []).forEach((f) => f(Object.assign({ stopPropagation() {}, target: this }, ev || {}))); }
  contains(n) { for (let x = n; x; x = x.parentNode) { if (x === this) { return true; } } return false; }
  getBoundingClientRect() { return { left: 10, bottom: 20 }; }
  get textContent() { return this._text; }
  set textContent(v) { this._text = String(v); }
  querySelector(sel) { return (this.sub[sel] || [])[0] || null; }
  querySelectorAll(sel) { return this.sub[sel] || []; }
  set innerHTML(v) { throw new Error('innerHTML must never be used'); }
}
const body = new El('body');
const docEl = new El('html'); docEl.clientWidth = 1000;
const defs = {};
// one stub per glossary entry
html.replace(/<div id="(g-[a-z-]+)" class="term"><dt>([^<]*)<\/dt><dd>([^<]*)/g, (m, id, dt, dd) => { const e = new El('div'); e.id = id; e._text = dt + ' ' + dd; defs[id] = e; return m; });
const tips = [];
html.replace(/<button type="button" class="tip" data-tip="([a-z-]+)"/g, (m, k) => { const b = new El('button'); b.attrs['data-tip'] = k; b.className = 'tip'; tips.push(b); return m; });
// checklists: count the decisions of each, the missed ones are those with "hasmissed"
const boxes = [];
const chkRe = /<div class="chk" data-chk>([\s\S]*?)<\/div>\s*<\/details>/g;
html.replace(chkRe, (m, inner) => {
  const box = new El('div'); box.sub = { 'li.dec': [], '[data-only-missed]': [], '.pager': [], '[data-more]': [], '.showing': [] };
  (inner.match(/<li class="dec[^"]*"/g) || []).forEach((c) => { const li = new El('li'); li.className = c.slice(10, -1); box.sub['li.dec'].push(li); });
  if (inner.indexOf('data-only-missed') >= 0) {
    const only = new El('input'); box.sub['[data-only-missed]'] = [only];
    const pager = new El('p'); pager.hidden = true; const more = new El('button'); const sh = new El('span');
    pager.sub = { '[data-more]': [more], '.showing': [sh] };
    box.sub['.pager'] = [pager];
    box.pager = pager; box.more = more; box.sh = sh; box.only = only;
  }
  boxes.push(box); return m;
});
const globalL = {};
const docL = {};
global.document = {
  documentElement: docEl, body,
  getElementById: (id) => defs[id] || (id === 'pk-tip' ? tipBoxRef() : null) || null,
  createElement: (t) => new El(t),
  createTextNode: (t) => { const e = new El('#text'); e._text = t; return e; },
  querySelector: () => null,
  querySelectorAll: (sel) => {
    if (sel === 'button.tip') { return tips; }
    if (sel === '[data-chk]') { return boxes; }
    return [];
  },
  addEventListener: (t, f) => { (docL[t] = docL[t] || []).push(f); },
};
function tipBoxRef() { return body.children.find((c) => c.id === 'pk-tip'); }
global.window = { pageXOffset: 0, pageYOffset: 0, addEventListener: (t, f) => { (globalL[t] = globalL[t] || []).push(f); } };
global.location = { hash: '' };
Object.defineProperty(global, 'navigator', { value: { clipboard: { writeText: () => Promise.resolve() } }, configurable: true });
global.setTimeout = () => 1; global.clearTimeout = () => {};
// the page's own data block and elements are not needed by ui(): give the script an empty report
const dataEl = new El('script'); const dm = /<script id="pk-data" type="application\/json">([\s\S]*?)<\/script>/.exec(html);
dataEl._text = dm ? dm[1] : '{}';
const realGet = document.getElementById;
document.getElementById = (id) => (id === 'pk-data' ? dataEl : realGet(id));
const code = /<script id="pk-code">([\s\S]*?)<\/script>/.exec(html)[1];
(0, eval)(code);

const out = { checks: {} };
const c = out.checks;
c.tips = tips.length;
c.defs = Object.keys(defs).length;
const tip = tips[0];
tip.fire('focus');
const box = tipBoxRef();
c.openedByFocus = !!box && box.hidden === false && box.textContent.length > 20 && box.getAttribute('role') === 'tooltip';
c.expanded = tip.getAttribute('aria-expanded');
docL.keydown.forEach((f) => f({ key: 'Escape' }));
c.closedByEscape = box.hidden === true && tip.getAttribute('aria-expanded') === 'false';
tip.fire('click');
c.openedByClick = box.hidden === false;
docL.click.forEach((f) => f({ target: body }));
c.closedByOutside = box.hidden === true;
tip.fire('mouseenter'); c.openedByHover = box.hidden === false; tip.fire('mouseleave'); c.closedByLeave = box.hidden === true;
// checklists
const withTools = boxes.find((b) => b.only);
if (withTools) {
  const lis = withTools.sub['li.dec'];
  c.decisions = lis.length;
  c.shownFirst = lis.filter((l) => !l.hidden).length;
  withTools.only.checked = true; withTools.only.fire('change');
  const missed = lis.filter((l) => l.className.indexOf('hasmissed') >= 0).length;
  c.onlyMissedShown = lis.filter((l) => !l.hidden).length;
  c.onlyMissedExpected = Math.min(missed, 25);
  withTools.only.checked = false; withTools.only.fire('change');
}
console.log(JSON.stringify(out));
