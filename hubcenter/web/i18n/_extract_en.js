'use strict';
const fs = require('fs');
const path = require('path');
const dir = path.join(__dirname, '..', 'admin', 'assets', 'js');

function scanOneObject(src, j) {
  let depth = 0;
  let k = j;
  let inStr = false;
  let q = '';
  let esc = false;
  for (; k < src.length; k++) {
    const c = src[k];
    if (inStr) {
      if (esc) { esc = false; continue; }
      if (c === '\\') { esc = true; continue; }
      if (c === q) inStr = false;
      continue;
    }
    if (c === '"' || c === "'" || c === '`') { inStr = true; q = c; continue; }
    if (c === '{') depth++;
    else if (c === '}') {
      depth--;
      if (depth === 0) return src.slice(j, k + 1);
    }
  }
  return null;
}

function objectLiteralsInParens(src, openParen) {
  const out = [];
  let depth = 0;
  let inStr = false;
  let q = '';
  let esc = false;
  for (let k = openParen; k < src.length; k++) {
    const c = src[k];
    if (inStr) {
      if (esc) { esc = false; continue; }
      if (c === '\\') { esc = true; continue; }
      if (c === q) inStr = false;
      continue;
    }
    if (c === '"' || c === "'" || c === '`') { inStr = true; q = c; continue; }
    if (c === '(') depth++;
    else if (c === ')') {
      depth--;
      if (depth === 0) break;
    } else if (c === '{' && depth === 1) {
      const lit = scanOneObject(src, k);
      if (lit) {
        out.push(lit);
        k += lit.length - 1;
      }
    }
  }
  return out;
}

function extractAssigns(src, name) {
  const out = [];
  const needles = [
    'const ' + name + '=',
    'let ' + name + '=',
    'var ' + name + '=',
    name + '=',
    'Object.assign(' + name + ','
  ];
  let i = 0;
  while (i < src.length) {
    let found = -1;
    let which = '';
    for (const n of needles) {
      let from = i;
      while (from < src.length) {
        const p = src.indexOf(n, from);
        if (p === -1) break;
        const prev = p > 0 ? src[p - 1] : '';
        if (!/[A-Za-z0-9_]/.test(prev)) {
          if (found === -1 || p < found) {
            found = p;
            which = n;
          }
          break;
        }
        from = p + 1;
      }
    }
    if (found === -1) break;
    if (name === 'I18N_ZH' && out.length < 3) {
      console.log('match', found, JSON.stringify(which), JSON.stringify(src.slice(found, found + 50)));
    }
    let j = found + which.length;
    while (j < src.length && /\s/.test(src[j])) j++;
    if (found === 11944) {
      const probe = 'Object.assign(';
      fs.writeFileSync(path.join(__dirname, '_debug.txt'), JSON.stringify({
        j,
        sliceCodes: src.slice(j, j + 16).split('').map(c => c.charCodeAt(0)),
        probeCodes: probe.split('').map(c => c.charCodeAt(0)),
        starts: src.startsWith(probe, j)
      }));
    }
    if (src.startsWith('Object.assign(', j)) {
      const open = src.indexOf('(', j);
      const got = objectLiteralsInParens(src, open);
      if (name === 'I18N_ZH' && found < 15000) console.log('parens', open, got.length, got.map(s => s.length).slice(0, 5));
      out.push(...got);
      i = open + 1;
      continue;
    }
    if (which.startsWith('Object.assign(')) {
      const open = src.indexOf('(', found);
      out.push(...objectLiteralsInParens(src, open));
      i = open + 1;
      continue;
    }
    if (src[j] !== '{') {
      i = found + which.length;
      continue;
    }
    const lit = scanOneObject(src, j);
    if (!lit) break;
    out.push(lit);
    i = j + lit.length;
  }
  return out;
}

const ctx = { I18N_EN: {}, I18N_ZH: {} };
const files = [
  'admin-core.js',
  'ha-news-admin.js',
  'llm-service-tab.js',
  'compute-market-tab.js',
  'user-rankings-tab.js',
  'notification-admin.js',
  'latex-template-admin.js',
  'problem-reports-tab.js'
];
for (const file of files) {
  const full = path.join(dir, file);
  if (!fs.existsSync(full)) { console.log('missing', file); continue; }
  const src = fs.readFileSync(full, 'utf8');
  for (const name of ['I18N_EN', 'I18N_ZH']) {
    const lits = extractAssigns(src, name);
    if (file === 'admin-core.js' && name === 'I18N_ZH') {
      console.log('ZH lit lengths', lits.map(s => s.length).join(','));
      console.log('first lit head', JSON.stringify(lits[0] && lits[0].slice(0, 60)));
      console.log('longest head', JSON.stringify((lits.slice().sort((a,b)=>b.length-a.length)[0] || '').slice(0, 80)));
    }
    let ok = 0;
    for (const lit of lits) {
      try {
        const obj = Function('return (' + lit + ')')();
        if (obj && typeof obj === 'object') {
          Object.assign(ctx[name], obj);
          ok++;
        }
      } catch (e) {
        console.error('FAIL', file, name, e.message, lit.slice(0, 120).replace(/\n/g, ' '));
      }
    }
    console.log(file, name, 'lits', lits.length, 'ok', ok, 'keys', Object.keys(ctx[name]).length);
  }
}
const en = ctx.I18N_EN;
const zh = ctx.I18N_ZH;
const zhOnly = Object.keys(zh).filter(k => !(k in en));
const enOnly = Object.keys(en).filter(k => !(k in zh) || zh[k] === en[k]);
console.log('EN', Object.keys(en).length, 'ZH', Object.keys(zh).length, 'zhOnly', zhOnly.length);
console.log('same-as-en-or-missing-zh', enOnly.length);
const rows = [];
for (const k of Object.keys(en)) {
  const ev = en[k];
  const zv = zh[k];
  if (typeof ev !== 'string') continue;
  if (typeof zv === 'string' && zv !== ev) rows.push(k + '\t' + ev.replace(/\n/g, '\\n'));
}
rows.sort();
fs.writeFileSync(path.join(__dirname, '_to_translate.tsv'), rows.join('\n') + '\n');
console.log('to translate', rows.length);
fs.writeFileSync(path.join(__dirname, '_en_keys.json'), JSON.stringify(en, null, 2));
fs.writeFileSync(path.join(__dirname, '_zh_keys.json'), JSON.stringify(zh, null, 2));
