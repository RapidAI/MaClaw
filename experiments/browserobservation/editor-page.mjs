const BANNER = "SYNTHETIC FIXTURE — 合成编辑器，不是 CSDN，不会发布";

export function editorDocument(search = "") {
  const params = new URLSearchParams(search);
  const caret = params.get("caret") === "before" ? "before" : "after";
  const swallow = params.get("swallow") === "0" ? "0" : "1";
  const ackMs = Number(params.get("ack") || "0");
  const selectMode = params.get("select") === "duplicate" ? "duplicate" : "native";
  return `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>synthetic-editor</title>
<style>
  body { font: 16px/1.45 sans-serif; margin: 20px; background: #f8fafc; color: #111; }
  .banner { background: #fef3c7; border: 1px solid #d97706; padding: 8px 12px; }
  .toolbar { display: flex; gap: 8px; margin: 12px 0; }
  button { font: inherit; padding: 6px 10px; }
  input[type="text"] { font: inherit; width: 100%; max-width: 640px; padding: 8px; }
  #file { position: absolute; left: -9999px; width: 1px; height: 1px; }
  [contenteditable] { min-height: 220px; border: 1px solid #94a3b8; padding: 12px; background: white; }
  [contenteditable] img { width: 120px; height: 80px; display: block; margin: 8px 0; object-fit: cover; }
  #preview { min-height: 80px; border: 1px dashed #64748b; padding: 12px; background: white; }
  #preview img { width: 120px; height: 80px; display: block; margin: 8px 0; }
</style>
</head>
<body>
<div class="banner">${BANNER}</div>
<label for="title">文章标题</label>
<input id="title" class="article-bar__title" type="text" autocomplete="off">
<div class="toolbar" role="toolbar" aria-label="编辑工具">
  <button type="button" id="image-button">插入图片</button>
  <button type="button" id="link-button">插入链接</button>
  <button type="button" id="preview-button">预览</button>
  <button type="button" id="publish-button">发布文章</button>
</div>
<label for="link-url">链接地址</label>
<input id="link-url" type="url" autocomplete="off">
<p id="status" role="status"></p>
<input id="file" type="file" accept="image/*">
<div id="editor" contenteditable="true" role="textbox" aria-multiline="true" aria-label="正文"></div>
<h2>渲染预览</h2>
<div id="preview" data-preview="1"></div>
<script>
  const caretMode = ${JSON.stringify(caret)};
  const swallowMiddle = ${JSON.stringify(swallow)} === "1";
  const ackMs = ${JSON.stringify(Number.isFinite(ackMs) && ackMs > 0 ? ackMs : 0)};
  const selectMode = ${JSON.stringify(selectMode)};
  const editor = document.getElementById("editor");
  const file = document.getElementById("file");
  const preview = document.getElementById("preview");
  const status = document.getElementById("status");
  let saved = null;
  let swallowed = false;
  window.__published = false;
  window.__toolbarClicks = 0;
  window.__fileChanges = 0;
  window.__pendingUploads = 0;
  window.__apiCalls = 0;
  window.__keyboardUndos = 0;
  window.__backups = [];
  function snapshot() {
    return { html: editor.innerHTML, text: editor.innerText };
  }
  function restore(previous) {
    editor.innerHTML = previous.html;
    renderPreview();
  }
  function saveRange() {
    const selection = window.getSelection();
    if (!selection || selection.rangeCount === 0) return;
    const range = selection.getRangeAt(0);
    if (editor.contains(range.startContainer)) saved = range.cloneRange();
  }
  function renderPreview() {
    preview.innerHTML = editor.innerHTML;
  }
  editor.addEventListener("keyup", saveRange);
  editor.addEventListener("mouseup", saveRange);
  editor.addEventListener("input", renderPreview);
  document.getElementById("image-button").addEventListener("mousedown", (event) => {
    event.preventDefault();
    saveRange();
  });
  document.getElementById("image-button").addEventListener("click", () => {
    window.__toolbarClicks += 1;
    file.click();
  });
  document.getElementById("preview-button").addEventListener("click", renderPreview);
  document.getElementById("publish-button").addEventListener("click", () => {
    window.__published = true;
  });
  document.getElementById("link-button").addEventListener("click", () => {
    const href = document.getElementById("link-url").value.trim();
    if (!href) return;
    const link = document.createElement("a");
    link.href = href;
    link.textContent = href;
    editor.appendChild(link);
    renderPreview();
  });
  editor.addEventListener("keydown", (event) => {
    const modified = event.ctrlKey || event.metaKey;
    if (modified && event.key.toLowerCase() === "a" && selectMode === "duplicate") {
      event.preventDefault();
      window.__backups.push(snapshot());
      editor.insertAdjacentHTML("beforeend", editor.innerHTML);
      renderPreview();
    }
    if (modified && event.key.toLowerCase() === "z") {
      event.preventDefault();
      const previous = window.__backups.pop();
      if (!previous) return;
      restore(previous);
      window.__keyboardUndos += 1;
    }
  });
  function insertNow(chosen) {
    const image = document.createElement("img");
    image.src = URL.createObjectURL(chosen);
    image.alt = chosen.name;
    const range = saved && editor.contains(saved.startContainer) ? saved : null;
    if (range) {
      range.collapse(caretMode === "before");
      range.insertNode(image);
      const next = document.createRange();
      if (caretMode === "before") next.setStartBefore(image);
      else next.setStartAfter(image);
      next.collapse(true);
      const selection = window.getSelection();
      selection.removeAllRanges();
      selection.addRange(next);
      saved = next.cloneRange();
    } else {
      editor.appendChild(image);
    }
    renderPreview();
  }
  function queueInsert(chosen) {
    if (ackMs <= 0) {
      insertNow(chosen);
      return;
    }
    window.__pendingUploads += 1;
    editor.setAttribute("aria-busy", "true");
    status.textContent = "正在插入";
    setTimeout(() => {
      insertNow(chosen);
      window.__pendingUploads -= 1;
      if (window.__pendingUploads <= 0) {
        editor.setAttribute("aria-busy", "false");
        status.textContent = "";
      }
    }, ackMs);
  }
  file.addEventListener("change", () => {
    const chosen = file.files && file.files[0];
    window.__fileChanges += 1;
    if (!chosen) return;
    const existing = editor.querySelectorAll("img").length;
    if (swallowMiddle && !swallowed && existing === 1) {
      swallowed = true;
      file.value = "";
      return;
    }
    const pending = chosen;
    file.value = "";
    queueInsert(pending);
  });
  window.editorApi = {
    getDocument() {
      window.__apiCalls += 1;
      return snapshot();
    },
    replaceDocument(html) {
      window.__apiCalls += 1;
      window.__backups.push(snapshot());
      editor.innerHTML = String(html || "");
      renderPreview();
    },
    undo() {
      window.__apiCalls += 1;
      const previous = window.__backups.pop();
      if (!previous) return false;
      restore(previous);
      return true;
    },
  };
</script>
</body>
</html>`;
}

export function editorHostDocument(frameURL) {
  return `<!doctype html>
<html lang="zh-CN">
<head><meta charset="utf-8"><title>synthetic-editor-host</title></head>
<body>
<p>SYNTHETIC FIXTURE — 编辑器在同域 iframe 里。不会发布。</p>
<iframe title="synthetic-editor-frame" src="${frameURL}" style="width:960px;height:720px;border:1px solid #94a3b8"></iframe>
</body>
</html>`;
}
