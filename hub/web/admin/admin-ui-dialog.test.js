/*
 * AdminUI confirmDialog / promptDialog tests.
 * Run: node hub/web/admin/admin-ui-dialog.test.js
 * ASCII only.
 */
'use strict';

const fs = require('fs');
const path = require('path');
const vm = require('vm');

let passed = 0;
let failed = 0;

function assert(cond, msg) {
  if (cond) passed += 1;
  else {
    failed += 1;
    console.error('  FAIL:', msg);
  }
}

function assertEqual(actual, expected, msg) {
  if (actual === expected) passed += 1;
  else {
    failed += 1;
    console.error('  FAIL:', msg, '| expected:', expected, '| got:', actual);
  }
}

function createMockDocument() {
  const byId = Object.create(null);
  const listeners = { keydown: [] };

  function makeEl(tag, id) {
    const kids = [];
    const elListeners = {};
    const classSet = {};
    const el = {
      tagName: String(tag || 'DIV').toUpperCase(),
      id: id || '',
      value: '',
      textContent: '',
      type: 'text',
      parentNode: null,
      style: {},
      dataset: {},
      isConnected: false,
      classList: {
        add: function(c) { classSet[c] = true; },
        remove: function(c) { delete classSet[c]; },
        contains: function(c) { return !!classSet[c]; }
      },
      getAttribute: function(name) {
        if (name === 'aria-hidden') return el._ariaHidden || null;
        return null;
      },
      setAttribute: function() {},
      focus: function() { doc.activeElement = el; },
      select: function() {},
      contains: function(node) {
        if (node === el) return true;
        return kids.indexOf(node) >= 0 || kids.some(function(k) {
          return k.contains && k.contains(node);
        });
      },
      addEventListener: function(type, fn) {
        elListeners[type] = elListeners[type] || [];
        elListeners[type].push(fn);
      },
      removeEventListener: function(type, fn) {
        const arr = elListeners[type] || [];
        elListeners[type] = arr.filter(function(f) { return f !== fn; });
      },
      click: function() {
        (elListeners.click || []).forEach(function(fn) {
          fn({ type: 'click', target: el, preventDefault: function() {}, stopPropagation: function() {} });
        });
      },
      querySelector: function(sel) {
        if (sel && sel.charAt(0) === '#') return byId[sel.slice(1)] || null;
        return null;
      },
      querySelectorAll: function(sel) {
        if (sel && sel.indexOf('button') === 0) {
          return kids.filter(function(k) { return k.tagName === 'BUTTON'; });
        }
        return kids.slice();
      },
      getClientRects: function() { return [{ width: 10, height: 10 }]; },
      offsetWidth: 10,
      offsetHeight: 10,
      _html: '',
      get innerHTML() { return el._html; },
      set innerHTML(html) {
        el._html = String(html || '');
        kids.length = 0;
        const re = /<([a-zA-Z0-9]+)([^>]*)>/g;
        let m;
        while ((m = re.exec(el._html))) {
          const tag = m[1].toLowerCase();
          const attrs = m[2] || '';
          const idMatch = attrs.match(/\sid="([^"]+)"/);
          if (!idMatch) continue;
          // Skip structural wrappers without ids we care about later via querySelectorAll buttons.
          const child = makeEl(tag, idMatch[1]);
          child.parentNode = el;
          child.isConnected = true;
          const typeMatch = attrs.match(/\stype="([^"]+)"/);
          if (typeMatch) child.type = typeMatch[1];
          const valMatch = attrs.match(/\svalue="([^"]*)"/);
          if (valMatch) child.value = valMatch[1];
          // textarea body: rough extract
          if (tag === 'textarea') {
            const bodyRe = new RegExp('<textarea[^>]*id="' + idMatch[1] + '"[^>]*>([\\s\\S]*?)</textarea>');
            const bm = el._html.match(bodyRe);
            if (bm) child.value = bm[1];
          }
          byId[child.id] = child;
          kids.push(child);
        }
      }
    };
    if (id) byId[id] = el;
    return el;
  }

  const body = makeEl('body', '');
  body.appendChild = function(child) {
    child.parentNode = body;
    child.isConnected = true;
    if (child.id) byId[child.id] = child;
    // parse className for overlay checks
    if (child.className) {
      String(child.className).split(/\s+/).forEach(function(c) {
        if (c) child.classList.add(c);
      });
    }
  };
  body.removeChild = function(child) {
    if (child && child.id && byId[child.id] === child) delete byId[child.id];
    if (child) {
      child.parentNode = null;
      child.isConnected = false;
    }
  };
  body.style = {};

  const doc = {
    body: body,
    activeElement: body,
    documentElement: body,
    getElementById: function(id) { return byId[id] || null; },
    createElement: function(tag) { return makeEl(tag, ''); },
    querySelector: function(sel) {
      if (sel === '.admin-ui-dialog-overlay') {
        const keys = Object.keys(byId);
        for (let i = 0; i < keys.length; i++) {
          const el = byId[keys[i]];
          if (el && el.classList && el.classList.contains('admin-ui-dialog-overlay') && el.isConnected) {
            return el;
          }
        }
        return null;
      }
      if (sel && sel.charAt(0) === '#') return byId[sel.slice(1)] || null;
      return null;
    },
    addEventListener: function(type, fn, capture) {
      if (type === 'keydown') listeners.keydown.push({ fn: fn, capture: !!capture });
    },
    removeEventListener: function(type, fn) {
      if (type === 'keydown') {
        listeners.keydown = listeners.keydown.filter(function(x) { return x.fn !== fn; });
      }
    },
    contains: function(node) {
      return !!(node && node.isConnected);
    },
    _dispatchKey: function(key, target, extra) {
      extra = extra || {};
      let stoppedImmediate = false;
      const event = {
        key: key,
        target: target || doc.activeElement,
        defaultPrevented: false,
        isComposing: !!extra.isComposing,
        keyCode: extra.keyCode || 0,
        which: extra.which || 0,
        shiftKey: !!extra.shiftKey,
        preventDefault: function() { event.defaultPrevented = true; },
        stopPropagation: function() {},
        stopImmediatePropagation: function() {
          stoppedImmediate = true;
          event.defaultPrevented = true;
        }
      };
      // capture first
      const caps = listeners.keydown.filter(function(x) { return x.capture; });
      for (let i = 0; i < caps.length; i++) {
        if (stoppedImmediate) break;
        caps[i].fn(event);
      }
      if (!event.defaultPrevented && !stoppedImmediate) {
        listeners.keydown.filter(function(x) { return !x.capture; }).forEach(function(x) { x.fn(event); });
      }
      return event;
    }
  };
  return doc;
}

function loadAdminUI() {
  const source = fs.readFileSync(path.join(__dirname, 'admin-ui.js'), 'utf8');
  const document = createMockDocument();
  const sandbox = {
    window: null,
    document: document,
    console: console,
    Object: Object,
    Array: Array,
    String: String,
    Number: Number,
    Boolean: Boolean,
    Math: Math,
    JSON: JSON,
    Promise: Promise,
    setTimeout: setTimeout,
    clearTimeout: clearTimeout
  };
  sandbox.window = sandbox;
  sandbox.global = sandbox;
  vm.createContext(sandbox);
  vm.runInContext(source, sandbox, { filename: 'admin-ui.js' });
  return { AdminUI: sandbox.AdminUI || sandbox.window.AdminUI, document: document, sandbox: sandbox };
}

async function run() {
  console.log('admin-ui dialog tests');

  // --- confirm accept ---
  {
    const ctx = loadAdminUI();
    assert(!!ctx.AdminUI, 'AdminUI exported');
    assert(typeof ctx.AdminUI.confirmDialog === 'function', 'confirmDialog exists');
    assert(typeof ctx.AdminUI.promptDialog === 'function', 'promptDialog exists');
    assertEqual(ctx.AdminUI.dialogZIndex, 20000, 'dialog z-index above content overlays');

    const p = ctx.AdminUI.confirmDialog('Delete item?', { title: 'Confirm', danger: true });
    assert(ctx.AdminUI.isDialogOpen(), 'isDialogOpen true after confirm open');
    assert(!!ctx.document.getElementById('adminUiConfirmDialogOverlay'), 'confirm overlay mounted');
    const ok = ctx.document.getElementById('adminUiConfirmOkBtn');
    assert(!!ok, 'ok button present');
    ok.click();
    const result = await p;
    assertEqual(result, true, 'confirm OK resolves true');
    assertEqual(ctx.AdminUI.isDialogOpen(), false, 'isDialogOpen false after close');
  }

  // --- confirm cancel ---
  {
    const ctx = loadAdminUI();
    const p = ctx.AdminUI.confirmDialog('Sure?');
    const cancel = ctx.document.getElementById('adminUiConfirmCancelBtn');
    cancel.click();
    assertEqual(await p, false, 'confirm cancel resolves false');
  }

  // --- confirm Escape ---
  {
    const ctx = loadAdminUI();
    const p = ctx.AdminUI.confirmDialog('Esc me');
    ctx.document._dispatchKey('Escape');
    assertEqual(await p, false, 'confirm Escape resolves false');
  }

  // --- confirm Enter confirms ---
  {
    const ctx = loadAdminUI();
    const p = ctx.AdminUI.confirmDialog('Enter me');
    ctx.document._dispatchKey('Enter');
    assertEqual(await p, true, 'confirm Enter resolves true');
  }

  // --- IME composition should not confirm ---
  {
    const ctx = loadAdminUI();
    let settled = false;
    const p = ctx.AdminUI.confirmDialog('IME').then(function(v) {
      settled = true;
      return v;
    });
    ctx.document._dispatchKey('Enter', null, { isComposing: true, keyCode: 229 });
    // give microtask a chance
    await Promise.resolve();
    assertEqual(settled, false, 'IME Enter does not settle confirm');
    ctx.document.getElementById('adminUiConfirmCancelBtn').click();
    assertEqual(await p, false, 'confirm still cancellable after IME');
  }

  // --- prompt required empty stays open ---
  {
    const ctx = loadAdminUI();
    let settled = false;
    const p = ctx.AdminUI.promptDialog('Name?', { required: true, title: 'New' }).then(function(v) {
      settled = true;
      return v;
    });
    const ok = ctx.document.getElementById('adminUiPromptOkBtn');
    ok.click();
    await Promise.resolve();
    assertEqual(settled, false, 'required empty prompt does not settle');
    const err = ctx.document.getElementById('adminUiPromptError');
    assert(!!err && !!err.textContent, 'required empty shows error');
    const input = ctx.document.getElementById('adminUiPromptInput');
    input.value = '  Lib A  ';
    ok.click();
    assertEqual(await p, 'Lib A', 'prompt trims value');
  }

  // --- prompt cancel ---
  {
    const ctx = loadAdminUI();
    const p = ctx.AdminUI.promptDialog('x');
    ctx.document.getElementById('adminUiPromptCancelBtn').click();
    assertEqual(await p, null, 'prompt cancel resolves null');
  }

  // --- supersede: opening second dialog cancels first ---
  {
    const ctx = loadAdminUI();
    const first = ctx.AdminUI.confirmDialog('first');
    const second = ctx.AdminUI.promptDialog('second', { defaultValue: 'v' });
    assertEqual(await first, false, 'superseded confirm resolves false');
    const input = ctx.document.getElementById('adminUiPromptInput');
    assert(!!input, 'second prompt input exists');
    assertEqual(input.value, 'v', 'second prompt default value');
    ctx.document.getElementById('adminUiPromptOkBtn').click();
    assertEqual(await second, 'v', 'second prompt resolves value');
  }

  // --- dismissActiveDialog ---
  {
    const ctx = loadAdminUI();
    const p = ctx.AdminUI.confirmDialog('dismiss');
    assert(ctx.AdminUI.isDialogOpen(), 'open before dismiss');
    ctx.AdminUI.dismissActiveDialog();
    assertEqual(await p, false, 'dismissActiveDialog cancels confirm');
    assertEqual(ctx.AdminUI.isDialogOpen(), false, 'closed after dismiss');
  }

  // --- body scroll lock ---
  {
    const ctx = loadAdminUI();
    ctx.document.body.style.overflow = '';
    const p = ctx.AdminUI.confirmDialog('scroll');
    assertEqual(ctx.document.body.style.overflow, 'hidden', 'body overflow locked while open');
    ctx.document.getElementById('adminUiConfirmOkBtn').click();
    await p;
    assertEqual(ctx.document.body.style.overflow, '', 'body overflow restored after close');
  }

  // --- double OK is idempotent ---
  {
    const ctx = loadAdminUI();
    const p = ctx.AdminUI.confirmDialog('once');
    const ok = ctx.document.getElementById('adminUiConfirmOkBtn');
    ok.click();
    ok.click();
    assertEqual(await p, true, 'double OK still resolves true once');
    assertEqual(ctx.AdminUI.isDialogOpen(), false, 'closed after double OK');
  }

  // --- no native dialog APIs invoked ---
  {
    const ctx = loadAdminUI();
    let nativeUsed = false;
    ctx.sandbox.prompt = function() { nativeUsed = true; return 'x'; };
    ctx.sandbox.confirm = function() { nativeUsed = true; return true; };
    ctx.sandbox.alert = function() { nativeUsed = true; };
    const p = ctx.AdminUI.promptDialog('native?', { defaultValue: 'a' });
    ctx.document.getElementById('adminUiPromptOkBtn').click();
    assertEqual(await p, 'a', 'custom prompt returns value');
    assertEqual(nativeUsed, false, 'never calls window.prompt/confirm/alert');
  }

  console.log('Result: ' + passed + ' passed, ' + failed + ' failed');
  process.exitCode = failed ? 1 : 0;
}

run().catch(function(err) {
  console.error(err);
  process.exitCode = 1;
});
