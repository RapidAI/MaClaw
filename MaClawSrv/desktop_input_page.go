package main

import "encoding/json"

// Page expressions for the browser relay. They read or replace the focused
// field and leave image elements in place. They are not a snapshot walker
// and they do not name a site.

const desktopInputObserveExpr = `/* maclaw-input-observe */
(function(){
  var sep = "\u241e";
  function secret(el){ return !!(el && el.tagName==="INPUT" && String(el.type||"").toLowerCase()==="password"); }
  var d = document, el = d.activeElement;
  for (var hop = 0; hop < 6 && el; hop++) {
    if (el.tagName === "IFRAME") {
      try { d = el.contentDocument; el = d && d.activeElement; } catch (e) { break; }
      continue;
    }
    if (el.shadowRoot && el.shadowRoot.activeElement) { el = el.shadowRoot.activeElement; continue; }
    break;
  }
  if (secret(el)) return JSON.stringify({secret:true, denied:true, trusted:false, text:""});
  var root = el && el.closest ? (el.closest("[contenteditable]") || el) : el;
  var text = "";
  if (root && (root.tagName==="INPUT" || root.tagName==="TEXTAREA")) text = root.value || "";
  else if (root) text = root.innerText || root.textContent || "";
  var images = root && root.querySelectorAll ? root.querySelectorAll("img").length : 0;
  var busy = !!(root && root.querySelector && root.querySelector("[aria-busy='true']"));
  var role = root ? (root.getAttribute("role") || root.tagName || "") : "";
  var name = root ? (root.getAttribute("name") || root.getAttribute("aria-label") || "") : "";
  var nav = (location.pathname || "") + (location.search || "");
  var h = 0;
  for (var i = 0; i < nav.length; i++) h = (h * 33 + nav.charCodeAt(i)) | 0;
  return JSON.stringify({
    text: text,
    trusted: !!(d.hasFocus && d.hasFocus()) && text.indexOf(sep) < 0,
    denied: !(d.hasFocus && d.hasFocus()),
    secret: false,
    clipped: text.indexOf(sep) >= 0,
    images: images,
    upload_pending: busy,
    editor_busy: busy,
    element_key: role + ":" + name,
    identified: !!(root && d.hasFocus && d.hasFocus() && !secret(root)),
    element_id: root ? 1 : 0,
    frame_id: "f" + h,
    ref_epoch: 0,
    layout_scale: Math.round((window.devicePixelRatio || 1) * 100),
    semantic: !!(root && images === 0 && (root.tagName==="INPUT" || root.tagName==="TEXTAREA" || root.isContentEditable)),
    clipboard: true,
    keys: true,
    editor_replace: !!(root && (root.isContentEditable || (root.closest && root.closest("[contenteditable]"))))
  });
})()`

const desktopInputActJS = `(function(){
  var act = MACLAW_INPUT_ACT;
  var el = document.activeElement;
  if (!el || (el.tagName==="INPUT" && String(el.type||"").toLowerCase()==="password")) {
    return JSON.stringify({ok:false, error:"secret"});
  }
  var root = (el.closest && el.closest("[contenteditable]")) || el;
  function imgs(){ return root.querySelectorAll ? root.querySelectorAll("img").length : 0; }
  var before = imgs();
  if (act.kind === "clear_field") {
    if (before > 0) return JSON.stringify({ok:false, error:"images_present", images: before, images_before: before});
    if (root.tagName==="INPUT" || root.tagName==="TEXTAREA") { root.value = ""; return JSON.stringify({ok:true, images:0, images_before:0}); }
    var w = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    var cleared;
    while ((cleared = w.nextNode())) cleared.nodeValue = "";
    return JSON.stringify({ok:true, images: imgs(), images_before: before});
  }
  var expected = String(act.text || "");
  if (root.tagName==="INPUT" || root.tagName==="TEXTAREA") {
    root.focus();
    root.value = expected;
    root.dispatchEvent(new Event("input", {bubbles:true}));
    return JSON.stringify({ok: root.value === expected, images: 0, images_before: 0});
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

func desktopInputActExpr(kind, text string, elementID int) string {
	payload, _ := json.Marshal(map[string]any{"kind": kind, "text": text, "element_id": elementID})
	return "/* maclaw-input-act */\nvar MACLAW_INPUT_ACT = " + string(payload) + ";\n" + desktopInputActJS
}
