import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import zlib from "node:zlib";

const ANCHORS = ["合成导语在第一张图之前", "第二段在中间图之前", "第三段在最后一张图之前"];
const ENDING = "结尾不再放图";
const TITLE = "合成标题不少于五字";

const READ_STRUCTURE = `() => {
  const read = (root) => {
    const blocks = [];
    let text = "";
    let top = 0;
    const flush = () => {
      const value = text.replace(/\\s+/g, " ").trim();
      if (value) blocks.push({ type: "text", text: value, top });
      text = "";
    };
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT);
    while (walker.nextNode()) {
      const node = walker.currentNode;
      if (node.nodeType === 3) {
        const range = document.createRange();
        range.selectNodeContents(node);
        const rect = range.getBoundingClientRect();
        if (!text) top = rect.top;
        text += node.nodeValue || "";
      } else if (node.tagName === "IMG") {
        flush();
        const rect = node.getBoundingClientRect();
        blocks.push({
          type: "img",
          alt: node.alt || "",
          top: rect.top,
          width: rect.width,
          height: rect.height,
          src_kind: (node.getAttribute("src") || "").startsWith("blob:") ? "blob" : "other",
        });
      }
    }
    flush();
    return blocks;
  };
  const editor = document.querySelector("[contenteditable='true']");
  const preview = document.querySelector("[data-preview]");
  return {
    title: (document.querySelector("#title") || {}).value || "",
    published: window.__published === true,
    toolbar_clicks: window.__toolbarClicks || 0,
    file_changes: window.__fileChanges || 0,
    editor: editor ? read(editor) : [],
    preview: preview ? read(preview) : [],
  };
}`;

function crc32(buffer) {
  let crc = ~0;
  for (const byte of buffer) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit += 1) crc = crc & 1 ? 0xedb88320 ^ (crc >>> 1) : crc >>> 1;
  }
  return ~crc >>> 0;
}

function chunk(type, data) {
  const body = Buffer.concat([Buffer.from(type), data]);
  const out = Buffer.alloc(12 + data.length);
  out.writeUInt32BE(data.length, 0);
  body.copy(out, 4);
  out.writeUInt32BE(crc32(body), 8 + data.length);
  return out;
}

function solidPNG(width, height, rgb) {
  const raw = Buffer.alloc((width * 3 + 1) * height);
  for (let y = 0; y < height; y += 1) {
    const start = y * (width * 3 + 1);
    raw[start] = 0;
    for (let x = 0; x < width; x += 1) {
      raw[start + 1 + x * 3] = rgb[0];
      raw[start + 2 + x * 3] = rgb[1];
      raw[start + 3 + x * 3] = rgb[2];
    }
  }
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(width, 0);
  ihdr.writeUInt32BE(height, 4);
  ihdr[8] = 8;
  ihdr[9] = 2;
  return Buffer.concat([
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
    chunk("IHDR", ihdr),
    chunk("IDAT", zlib.deflateSync(raw)),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

export function judgeArticle(structure) {
  const problems = [];
  if (structure.title !== TITLE) problems.push("title");
  if (structure.published) problems.push("published");
  const images = structure.editor.filter((block) => block.type === "img");
  const texts = structure.editor.filter((block) => block.type === "text").map((block) => block.text);
  if (images.length !== 3) problems.push(`image_count:${images.length}`);
  const alts = images.map((image) => image.alt);
  if (JSON.stringify(alts) !== JSON.stringify(["fig-1.png", "fig-2.png", "fig-3.png"])) problems.push(`image_order:${alts.join(",")}`);
  for (const image of images) {
    if (image.src_kind !== "blob" || image.width < 60 || image.height < 40) problems.push("image_not_rendered");
  }
  const pattern = [];
  for (const block of structure.editor) {
    if (block.type === "img") pattern.push("img");
    else if (ANCHORS.some((anchor) => block.text.includes(anchor)) || block.text.includes(ENDING)) pattern.push("text");
  }
  if (pattern.join(",") !== "text,img,text,img,text,img,text") problems.push(`positions:${pattern.join(",")}`);
  for (let index = 0; index < structure.editor.length; index += 1) {
    const block = structure.editor[index];
    if (block.type !== "img") continue;
    const previous = structure.editor[index - 1];
    const next = structure.editor[index + 1];
    if (!previous || !next || previous.top > block.top || next.top < block.top) problems.push(`vertical:${index}`);
  }
  for (const anchor of ANCHORS) {
    if (!texts.some((text) => text.includes(anchor))) problems.push(`missing_text:${anchor}`);
  }
  if (!texts.some((text) => text.includes(ENDING))) problems.push("missing_ending");
  const previewImages = structure.preview.filter((block) => block.type === "img").map((image) => image.alt);
  if (JSON.stringify(previewImages) !== JSON.stringify(alts)) problems.push(`preview:${previewImages.join(",")}`);
  return { ok: problems.length === 0, problems, image_count: images.length, alts };
}

function missingSlot(structure) {
  const expected = [
    ["合成导语在第一张图之前", "fig-1.png"],
    ["第二段在中间图之前", "fig-2.png"],
    ["第三段在最后一张图之前", "fig-3.png"],
  ];
  const present = new Set(structure.editor.filter((block) => block.type === "img").map((image) => image.alt));
  return expected.find(([, alt]) => !present.has(alt)) || null;
}

async function readStructure(scope) {
  return scope.evaluate(`(${READ_STRUCTURE})()`);
}

async function insertFigure(page, scope, file, trace, task) {
  const started = Date.now();
  const chooserPromise = page.waitForEvent("filechooser");
  await scope.getByRole("button", { name: "插入图片" }).click();
  const chooser = await chooserPromise;
  await chooser.setFiles(file);
  trace.push({
    kind: "browser_primitive",
    task,
    op: "choose_file",
    file: path.basename(file),
    ms: Date.now() - started,
    model_call: false,
    chat_progress: true,
    query: "role=button name=插入图片 + filechooser",
  });
}

async function typeIn(scope, text, trace, task) {
  const started = Date.now();
  const hasCaret = await scope.evaluate(() => {
    const editor = document.querySelector("[contenteditable='true']");
    const selection = window.getSelection();
    const inside = Boolean(selection && selection.rangeCount > 0 && editor.contains(selection.anchorNode) && (editor.innerText || "").trim());
    if (inside) editor.focus();
    return inside;
  });
  if (!hasCaret) await scope.locator("[contenteditable='true']").click();
  await scope.page().keyboard.type(text);
  await scope.page().keyboard.press("Enter");
  trace.push({
    kind: "browser_primitive",
    task,
    op: "type",
    chars: text.length,
    ms: Date.now() - started,
    model_call: false,
    chat_progress: true,
    query: "contenteditable",
  });
}

export async function runRichtext(browser, origin, outDir) {
  const imageDir = path.join(outDir, "images");
  await mkdir(imageDir, { recursive: true });
  const files = [];
  const colors = [
    [220, 38, 38],
    [22, 163, 74],
    [37, 99, 235],
  ];
  for (let index = 0; index < 3; index += 1) {
    const file = path.join(imageDir, `fig-${index + 1}.png`);
    await writeFile(file, solidPNG(120, 80, colors[index]));
    files.push(file);
  }
  const context = await browser.newContext({ viewport: { width: 1100, height: 800 }, deviceScaleFactor: 1, locale: "zh-CN" });
  const page = await context.newPage();
  const trace = [];
  const cases = [];

  async function openEditor(url) {
    await page.goto(origin + url, { waitUntil: "domcontentloaded" });
    if (url.includes("richtext-host")) {
      await page.frameLocator("iframe").locator("[contenteditable='true']").waitFor({ timeout: 10000 });
      const frame = page.frames().find((item) => item.url().includes("/editor-app"));
      if (!frame) throw new Error(`editor frame missing: ${page.frames().map((item) => item.url()).join(" | ")}`);
      return frame;
    }
    await page.waitForSelector("[contenteditable='true']");
    return page.mainFrame();
  }

  const scopeOf = (frame) => ({
    evaluate: (expression) => frame.evaluate(expression),
    getByRole: (role, options) => frame.getByRole(role, options),
    locator: (selector) => frame.locator(selector),
    page: () => page,
  });

  {
    const frame = await openEditor("/editor-app?swallow=0");
    const scope = scopeOf(frame);
    await scope.getByRole("textbox", { name: "文章标题" }).fill(TITLE);
    await typeIn(scope, "将被选区覆盖的句子", trace, "overwrite_hazard");
    await frame.evaluate(() => {
      const editor = document.querySelector("[contenteditable='true']");
      const selection = window.getSelection();
      const range = document.createRange();
      range.selectNodeContents(editor);
      selection.removeAllRanges();
      selection.addRange(range);
    });
    await page.keyboard.type("覆盖");
    const after = await readStructure(scope);
    const text = after.editor.filter((block) => block.type === "text").map((block) => block.text).join("");
    cases.push({
      id: "overwrite_hazard",
      reused_capability: "contenteditable selection replaces the range",
      text_after: text,
      detected_overwrite: text === "覆盖",
      model_calls: 0,
    });
  }

  {
    const frame = await openEditor("/editor-app?swallow=0&caret=before");
    const scope = scopeOf(frame);
    await scope.getByRole("textbox", { name: "文章标题" }).fill(TITLE);
    await typeIn(scope, ANCHORS[0], trace, "caret_drift_probe");
    await insertFigure(page, scope, files[0], trace, "caret_drift_probe");
    await typeIn(scope, ENDING, trace, "caret_drift_probe");
    const drifted = judgeArticle(await readStructure(scope));
    cases.push({
      id: "caret_drift_probe",
      note: "fixture leaves the caret before the image; the next paragraph is not after it",
      judge_ok: drifted.ok,
      problems: drifted.problems,
      detected_bad_order: drifted.problems.some((problem) => problem.startsWith("positions")),
      model_calls: 0,
    });
  }

  async function article(task, url) {
    const eventsBefore = trace.length;
    const frame = await openEditor(url);
    const scope = scopeOf(frame);
    const titleStarted = Date.now();
    await scope.getByRole("textbox", { name: "文章标题" }).fill(TITLE);
    trace.push({ kind: "browser_primitive", task, op: "fill", name: "文章标题", ms: Date.now() - titleStarted, model_call: false, query: "role=textbox name=文章标题" });
    const paragraphs = [...ANCHORS, ENDING];
    for (let index = 0; index < files.length; index += 1) {
      await typeIn(scope, paragraphs[index], trace, task);
      await insertFigure(page, scope, files[index], trace, task);
      const observed = await readStructure(scope);
      trace.push({
        kind: "observation",
        task,
        channel: "editor_dom_and_preview",
        images: observed.editor.filter((block) => block.type === "img").length,
        model_call: false,
      });
    }
    await typeIn(scope, ENDING, trace, task);
    let structure = await readStructure(scope);
    let judged = judgeArticle(structure);
    let recovery = null;
    if (!judged.ok) {
      const slot = missingSlot(structure);
      if (slot) {
        const [anchor, alt] = slot;
        const file = files.find((item) => path.basename(item) === alt);
        const placed = await frame.evaluate((needle) => {
          const editor = document.querySelector("[contenteditable='true']");
          const walker = document.createTreeWalker(editor, NodeFilter.SHOW_TEXT);
          let node = walker.nextNode();
          while (node) {
            if ((node.nodeValue || "").includes(needle)) {
              const range = document.createRange();
              range.setStart(node, node.nodeValue.length);
              range.collapse(true);
              const selection = window.getSelection();
              selection.removeAllRanges();
              selection.addRange(range);
              editor.focus();
              return true;
            }
            node = walker.nextNode();
          }
          return false;
        }, anchor);
        trace.push({
          kind: "recovery",
          task,
          op: "place_caret_after_anchor_text",
          anchor,
          placed,
          model_call: false,
          site_keyword: false,
        });
        if (placed && file) await insertFigure(page, scope, file, trace, task);
        structure = await readStructure(scope);
        judged = judgeArticle(structure);
        recovery = { anchor, alt, judge: judged };
      }
    }
    const shot = await page.screenshot({ type: "png" });
    await writeFile(path.join(outDir, `${task}.png`), shot);
    cases.push({
      id: task,
      model_calls: 0,
      browser_primitives: trace.slice(eventsBefore).filter((event) => event.kind === "browser_primitive").length,
      observations: trace.slice(eventsBefore).filter((event) => event.kind === "observation").length,
      recovery,
      partial_state_is_not_success: false,
      published: structure.published,
      judge: judged,
      driver_queries: ["role=textbox name=文章标题", "contenteditable", "role=button name=插入图片", "filechooser", "anchor text"],
      not_used: [".article-bar__title", "editor.csdn.net", "mp.csdn.net", "发布文章"],
    });
  }

  {
    const frame = await openEditor("/editor-app?swallow=0");
    const scope = scopeOf(frame);
    await scope.getByRole("textbox", { name: "文章标题" }).fill(TITLE);
    for (const paragraph of [...ANCHORS, ENDING]) await typeIn(scope, paragraph, trace, "toolbar_without_file");
    const chooserPromise = page.waitForEvent("filechooser");
    await scope.getByRole("button", { name: "插入图片" }).click();
    await chooserPromise;
    const structure = await readStructure(scope);
    const images = structure.editor.filter((block) => block.type === "img").length;
    cases.push({
      id: "toolbar_without_file",
      note: "title and body are present and the image button was clicked; no file was chosen",
      title: structure.title,
      body_has_anchors: ANCHORS.every((anchor) => structure.editor.some((block) => block.type === "text" && block.text.includes(anchor))),
      image_count: images,
      success: false,
      proves_images_inserted: false,
      model_calls: 0,
    });
  }

  await article("richtext_top", "/editor-app");
  await article("richtext_iframe", "/richtext-host");

  await context.close();
  const failures = [];
  const hazard = cases.find((item) => item.id === "overwrite_hazard");
  const drift = cases.find((item) => item.id === "caret_drift_probe");
  const toolbar = cases.find((item) => item.id === "toolbar_without_file");
  const top = cases.find((item) => item.id === "richtext_top");
  const framed = cases.find((item) => item.id === "richtext_iframe");
  if (!hazard.detected_overwrite) failures.push("overwrite hazard was not detected");
  if (!drift.detected_bad_order) failures.push("caret drift was not detected");
  if (toolbar.image_count !== 0 || toolbar.proves_images_inserted) failures.push("toolbar click counted as insertion");
  if (!top.judge.ok) failures.push(`top editor: ${top.judge.problems.join("; ")}`);
  if (!framed.judge.ok) failures.push(`iframe editor: ${framed.judge.problems.join("; ")}`);
  if (top.published || framed.published) failures.push("publish flag was set");
  return {
    kind: "synthetic_richtext_not_csdn_publish",
    model_calls: 0,
    not_csdn_latency: "shopping/file-organizing step counts are not this task",
    public_editor_note: "editor.csdn.net/md initial HTML is an empty #app shell titled 写文章-CSDN博客 and loads csdn-upload.js plus /release/md chunks. Those chunks contain the strings 插入图片, 上传图片, 文章标题, contenteditable, article-bar, editor__inner. No login and no article was published. The fixture does not reuse that script.",
    reusable_capabilities: [
      "contenteditable typing with a collapsed caret",
      "toolbar button plus file chooser",
      "editor DOM and preview image count, order, and vertical position",
      "observe then correct a missing image by anchor text",
    ],
    csdn_specific_helpers_used: [],
    cases,
    trace,
    failures,
  };
}
