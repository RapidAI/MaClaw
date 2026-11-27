package main

import "encoding/json"

// Shared page helper. Identity is a hash of the element, its index among
// editables, and the frame. The epoch changes with navigation and with the
// number of editables. The action recomputes that identity and writes only
// when it still matches. It does not select-all.

const desktopInputFieldJS = `function __maclawHash(s) {
  var h = 2166136261;
  for (var i = 0; i < s.length; i++) { h ^= s.charCodeAt(i); h = Math.imul(h, 16777619); }
  h = h >>> 0;
  return h === 0 ? 1 : h;
}
function __maclawSecret(el) {
  return !!(el && el.tagName === "INPUT" && String(el.type || "").toLowerCase() === "password");
}
function __maclawBusy(el) {
  return !!(el && el.getAttribute && el.getAttribute("aria-busy") === "true");
}
function __maclawEditables(doc) {
  return Array.prototype.slice.call(doc.querySelectorAll("input,textarea,[contenteditable=''],[contenteditable='true'],[contenteditable=true],[contenteditable=plaintext-only]"));
}
function __maclawRoot(el) {
  if (!el) return null;
  if (el.closest) {
    var box = el.closest("[contenteditable=''],[contenteditable='true'],[contenteditable=true],[contenteditable=plaintext-only]");
    if (box) return box;
  }
  return el;
}
function __maclawWalk(start) {
  var d = start.ownerDocument || document;
  var el = start;
  for (var hop = 0; hop < 6 && el; hop++) {
    if (el.tagName === "IFRAME") {
      try { d = el.contentDocument; el = d && d.activeElement; } catch (e) { break; }
      continue;
    }
    if (el.shadowRoot && el.shadowRoot.activeElement) { el = el.shadowRoot.activeElement; continue; }
    break;
  }
  return { doc: d, el: el };
}
function __maclawIdentity(el) {
  var doc = el.ownerDocument;
  var list = __maclawEditables(doc);
  var index = list.indexOf(el);
  if (index < 0) index = list.indexOf(__maclawRoot(el));
  var name = el.getAttribute("name") || el.getAttribute("aria-label") || "";
  var token = [el.tagName, el.id || "", name, String(index)].join("|");
  var frame = "top";
  if (doc.defaultView && doc.defaultView.frameElement) {
    var fr = doc.defaultView.frameElement;
    frame = "frame:" + (fr.id || "") + ":" + (fr.getAttribute("name") || "");
  }
  return {
    element_id: __maclawHash(frame + "|" + token),
    element_key: (el.getAttribute("role") || el.tagName) + ":" + name + ":" + index,
    frame_id: frame + ":" + __maclawHash((doc.location && doc.location.pathname || "") + (doc.location && doc.location.search || "")),
    index: index
  };
}
function __maclawFind(doc, id) {
  var list = __maclawEditables(doc);
  for (var i = 0; i < list.length; i++) {
    if (__maclawIdentity(list[i]).element_id === id) return list[i];
  }
  return null;
}
function __maclawEditorBusy(root) {
  if (!root) return false;
  if (__maclawBusy(root)) return true;
  if (root.querySelector && root.querySelector("[aria-busy='true']")) return true;
  var parent = root.parentElement;
  if (__maclawBusy(parent)) return true;
  if (!parent) return false;
  var kids = parent.children;
  for (var i = 0; i < kids.length; i++) {
    if (__maclawBusy(kids[i])) return true;
    if (kids[i].querySelector && kids[i].querySelector("[aria-busy='true']")) return true;
  }
  return false;
}
function __maclawReadField() {
  var walked = __maclawWalk(document.activeElement || document.body);
  var el = walked.el;
  var doc = walked.doc || document;
  if (__maclawSecret(el)) {
    return { secret: true, denied: true, trusted: false, text: "", identified: false, element_id: 0 };
  }
  var root = __maclawRoot(el);
  var text = "";
  if (root && (root.tagName === "INPUT" || root.tagName === "TEXTAREA")) text = root.value || "";
  else if (root) text = root.innerText || root.textContent || "";
  var images = root && root.querySelectorAll ? root.querySelectorAll("img").length : 0;
  var busy = __maclawEditorBusy(root);
  var idn = root ? __maclawIdentity(root) : { element_id: 0, element_key: "", frame_id: "top:0" };
  var focused = !!(doc.hasFocus && doc.hasFocus());
  var sep = "\u241e";
  var clipped = text.indexOf(sep) >= 0;
  var identified = !!(root && focused && !clipped && idn.element_id);
  var width = 0;
  try { width = Math.round((root && root.getBoundingClientRect && root.getBoundingClientRect().width) || 0); } catch (e) {}
  var editables = __maclawEditables(doc).length;
  var epoch = __maclawHash((doc.location ? doc.location.pathname + doc.location.search : "") + "|" + editables + "|" + (doc.body ? doc.body.childElementCount : 0));
  return {
    text: identified ? text : "",
    trusted: identified,
    denied: !focused,
    secret: false,
    clipped: clipped,
    images: images,
    upload_pending: busy,
    editor_busy: busy,
    element_key: identified ? idn.element_key : "",
    identified: identified,
    element_id: identified ? idn.element_id : 0,
    frame_id: idn.frame_id,
    ref_epoch: epoch,
    layout_scale: Math.round((doc.defaultView && doc.defaultView.devicePixelRatio || 1) * 1000) + width,
    semantic: !!(identified && images === 0 && root && (root.tagName === "INPUT" || root.tagName === "TEXTAREA" || root.isContentEditable)),
    clipboard: false,
    keys: false,
    editor_replace: !!(identified && images > 0 && root && root.isContentEditable)
  };
}
`

const desktopInputObserveExpr = "/* maclaw-input-observe */\n" + desktopInputFieldJS + "JSON.stringify(__maclawReadField())"

const desktopInputActJS = `(function(){
  var act = MACLAW_INPUT_ACT;
  var now = __maclawReadField();
  if (now.secret) return JSON.stringify({ok:false, error:"secret"});
  if (!now.identified || now.element_id !== act.element_id || now.element_key !== act.element_key) {
    return JSON.stringify({ok:false, error:"wrong_field"});
  }
  if (now.frame_id !== act.frame_id || now.ref_epoch !== act.ref_epoch || now.layout_scale !== act.layout_scale) {
    return JSON.stringify({ok:false, error:"stale_target"});
  }
  var walked = __maclawWalk(document.activeElement || document.body);
  var root = __maclawFind(walked.doc || document, act.element_id);
  if (!root || root !== __maclawRoot(walked.el)) return JSON.stringify({ok:false, error:"wrong_field"});
  function imgs(){ return root.querySelectorAll ? root.querySelectorAll("img").length : 0; }
  var before = imgs();
  var expected = String(act.text || "");
  if (root.tagName === "INPUT" || root.tagName === "TEXTAREA") {
    root.focus();
    root.value = expected;
    root.dispatchEvent(new Event("input", {bubbles:true}));
    return JSON.stringify({ok: root.value === expected, images: before, images_before: before});
  }
  var walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  var nodes = [], node;
  while ((node = walker.nextNode())) nodes.push(node);
  var backup = nodes.map(function(n){ return n.nodeValue; });
  if (nodes.length === 0) root.appendChild(document.createTextNode(expected));
  else {
    nodes[0].nodeValue = expected;
    for (var i = 1; i < nodes.length; i++) nodes[i].nodeValue = "";
  }
  var after = imgs();
  if (after < before) {
    for (var j = 0; j < nodes.length; j++) nodes[j].nodeValue = backup[j];
    return JSON.stringify({ok:false, error:"images_lost", images: imgs(), images_before: before});
  }
  return JSON.stringify({ok:true, images: after, images_before: before});
})()`

func desktopInputActExpr(kind, text, elementKey, frameID string, elementID, refEpoch, layoutScale int) string {
	payload, _ := json.Marshal(map[string]any{
		"kind": kind, "text": text, "element_id": elementID,
		"element_key": elementKey, "frame_id": frameID,
		"ref_epoch": refEpoch, "layout_scale": layoutScale,
	})
	return "/* maclaw-input-act */\nvar MACLAW_INPUT_ACT = " + string(payload) + ";\n" + desktopInputFieldJS + desktopInputActJS
}
