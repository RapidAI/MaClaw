import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import zlib from "node:zlib";

const READ = `() => ({
  title: document.querySelector("#title").value,
  busy: document.querySelector("#editor").getAttribute("aria-busy") === "true",
  pending: window.__pendingUploads || 0,
  text: document.querySelector("#editor").innerText.replace(/\\s+/g, " ").trim(),
  images: [...document.querySelector("#editor").querySelectorAll("img")].map((img) => img.alt),
  links: [...document.querySelector("#editor").querySelectorAll("a")].map((anchor) => anchor.getAttribute("href") || ""),
  api_calls: window.__apiCalls || 0,
  keyboard_undos: window.__keyboardUndos || 0,
  backups: (window.__backups || []).length,
  published: window.__published === true,
  focus: document.activeElement ? document.activeElement.id : "",
})`;

export function decide(observation) {
  if (observation.busy || observation.pending > 0) {
    return { action: "wait", reason: "upload_not_acknowledged", arm: "semantic" };
  }
  const images = observation.images || [];
  const duplicateImages = images.filter((alt, index) => images.indexOf(alt) !== index);
  if (duplicateImages.length > 0 || observation.duplicated_text) {
    return { action: "undo_then_switch", reason: "integrity", switch_to: "editor_api", arm: "hybrid_editor_api" };
  }
  if ((observation.links || []).length > 0 && images.length < (observation.expected_images || 1)) {
    return { action: "reject_link_substitution", reason: "link_is_not_image", arm: "semantic" };
  }
  if (observation.tried_select_all && observation.focus && observation.focus !== "editor") {
    return { action: "refocus_editor", reason: "selection_target_mismatch", arm: "semantic" };
  }
  return { action: "continue_semantic", reason: "stable", arm: "semantic" };
}

function crc32(buffer) {
  let crc = ~0;
  for (const byte of buffer) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit += 1) crc = crc & 1 ? 0xedb88320 ^ (crc >>> 1) : crc >>> 1;
  }
  return ~crc >>> 0;
}

function solidPNG() {
  const raw = Buffer.alloc(121 * 40);
  for (let y = 0; y < 40; y += 1) {
    const start = y * 121;
    raw[start] = 0;
    for (let x = 0; x < 40; x += 1) {
      raw[start + 1 + x * 3] = 20;
      raw[start + 2 + x * 3] = 80;
      raw[start + 3 + x * 3] = 200;
    }
  }
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(40, 0);
  ihdr.writeUInt32BE(40, 4);
  ihdr[8] = 8;
  ihdr[9] = 2;
  const chunk = (type, data) => {
    const body = Buffer.concat([Buffer.from(type), data]);
    const out = Buffer.alloc(12 + data.length);
    out.writeUInt32BE(data.length, 0);
    body.copy(out, 4);
    out.writeUInt32BE(crc32(body), 8 + data.length);
    return out;
  };
  return Buffer.concat([
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
    chunk("IHDR", ihdr),
    chunk("IDAT", zlib.deflateSync(raw)),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

async function read(frame) {
  return frame.evaluate(`(${READ})()`);
}

async function choose(page, frame, file, trace, task) {
  const started = Date.now();
  const chooser = page.waitForEvent("filechooser");
  await frame.getByRole("button", { name: "插入图片" }).click();
  await (await chooser).setFiles(file);
  trace.push({
    kind: "browser_primitive",
    task,
    op: "choose_file",
    ms: Date.now() - started,
    model_call: false,
    arm: "semantic",
    query: "role=button name=插入图片 + filechooser",
  });
}

export async function runEditorFaults(browser, origin, outDir) {
  await mkdir(outDir, { recursive: true });
  const file = path.join(outDir, "fig-delay.png");
  await writeFile(file, solidPNG());
  const context = await browser.newContext({ viewport: { width: 1100, height: 800 }, deviceScaleFactor: 1, locale: "zh-CN" });
  const page = await context.newPage();
  const external = [];
  page.on("request", (request) => {
    const target = request.url();
    if (target.startsWith("data:") || target.startsWith("blob:") || target.startsWith("about:")) return;
    if (new URL(target).hostname !== "127.0.0.1") external.push(target.slice(0, 180));
  });
  const trace = [];
  const cases = [];

  async function open(query) {
    await page.goto(`${origin}/editor-app?swallow=0&${query}`, { waitUntil: "domcontentloaded" });
    await page.locator("#editor").click();
    await page.keyboard.type("合成段落");
    await page.keyboard.press("Enter");
  }

  {
    await open("ack=700");
    await choose(page, page, file, trace, "delayed_ack_wait");
    const pending = await read(page.mainFrame());
    const decision = decide({ ...pending, expected_images: 1 });
    trace.push({ kind: "strategy", task: "delayed_ack_wait", decision, model_call: false, api_calls: pending.api_calls });
    if (decision.action === "wait") {
      await page.waitForFunction(() => document.querySelector("#editor").getAttribute("aria-busy") !== "true" && document.querySelectorAll("#editor img").length === 1);
    }
    const done = await read(page.mainFrame());
    cases.push({
      id: "delayed_ack_wait",
      arm: "semantic",
      model_calls: 0,
      api_calls: done.api_calls,
      decision,
      images: done.images,
      success: decision.action === "wait" && done.images.length === 1 && done.api_calls === 0 && !done.published,
    });
  }

  {
    await open("ack=700");
    await choose(page, page, file, trace, "delayed_ack_eager_retry");
    const first = await read(page.mainFrame());
    if (first.images.length === 0) await choose(page, page, file, trace, "delayed_ack_eager_retry");
    await page.waitForFunction(() => (window.__pendingUploads || 0) === 0 && document.querySelectorAll("#editor img").length >= 1);
    await page.waitForTimeout(800);
    const done = await read(page.mainFrame());
    const decision = decide({ ...done, duplicated_text: false, expected_images: 1 });
    cases.push({
      id: "delayed_ack_eager_retry",
      arm: "semantic",
      model_calls: 0,
      api_calls: done.api_calls,
      images: done.images,
      duplicate_detected: done.images.length > 1,
      decision,
      success: false,
      note: "immediate retry while aria-busy inserts a second image; this case is the failure, not a win",
    });
  }

  {
    await open("ack=0");
    const before = await read(page.mainFrame());
    await page.locator("#link-url").click();
    await page.keyboard.press("Control+A");
    await page.keyboard.type("not-the-body");
    const after = await read(page.mainFrame());
    const decision = decide({ ...after, tried_select_all: true, expected_images: 1 });
    cases.push({
      id: "ctrl_a_wrong_focus",
      arm: "semantic",
      model_calls: 0,
      api_calls: after.api_calls,
      focus: after.focus,
      body_unchanged: after.text === before.text,
      decision,
      success: after.text === before.text && after.focus !== "editor" && decision.action === "refocus_editor" && after.api_calls === 0,
    });
  }

  {
    await open("ack=0&select=duplicate");
    const before = await read(page.mainFrame());
    await page.locator("#editor").click();
    await page.keyboard.press("Control+A");
    const duplicated = await read(page.mainFrame());
    const decision = decide({ ...duplicated, duplicated_text: duplicated.text.length > before.text.length + 2, expected_images: 1 });
    await page.keyboard.press("Control+Z");
    const restored = await read(page.mainFrame());
    cases.push({
      id: "ctrl_a_duplicates_then_keyboard_undo",
      arm: "semantic",
      model_calls: 0,
      api_calls: restored.api_calls,
      keyboard_undos: restored.keyboard_undos,
      duplicated_text: duplicated.text,
      restored_text: restored.text,
      decision,
      success:
        duplicated.text.includes("合成段落") &&
        duplicated.text.length > before.text.length + 2 &&
        restored.text === before.text &&
        restored.keyboard_undos === 1 &&
        restored.api_calls === 0 &&
        decision.action === "undo_then_switch",
    });
  }

  {
    await open("ack=0&select=duplicate");
    await page.locator("#editor").click();
    await page.keyboard.press("Control+A");
    const scrambled = await read(page.mainFrame());
    const backup = await page.evaluate(() => (window.__backups[0] ? window.__backups[0].html : ""));
    await page.evaluate((html) => window.editorApi.replaceDocument(html), backup);
    const replaced = await read(page.mainFrame());
    const undoWorked = await page.evaluate(() => window.editorApi.undo());
    const undone = await read(page.mainFrame());
    trace.push({
      kind: "editor_api",
      task: "hybrid_replace",
      op: "replaceDocument",
      model_call: false,
      arm: "hybrid_editor_api",
      note: "disclosed hybrid; not a semantic snapshot result",
    });
    cases.push({
      id: "hybrid_replace_with_backup",
      arm: "hybrid_editor_api",
      counts_as_semantic_win: false,
      model_calls: 0,
      api_calls: undone.api_calls,
      replaced_text: replaced.text,
      undone_text: undone.text,
      scrambled_text: scrambled.text,
      undo_worked: undoWorked === true,
      published: undone.published,
      success:
        replaced.text === "合成段落" &&
        undone.text === scrambled.text &&
        undone.api_calls >= 2 &&
        undone.published === false,
    });
  }

  {
    await open("ack=0");
    await page.locator("#link-url").fill("https://arxiv.org/abs/0000.00000");
    await page.getByRole("button", { name: "插入链接" }).click();
    const done = await read(page.mainFrame());
    const decision = decide({ ...done, expected_images: 1 });
    cases.push({
      id: "link_is_not_image",
      arm: "semantic",
      model_calls: 0,
      api_calls: done.api_calls,
      images: done.images,
      links: done.links,
      decision,
      success: done.images.length === 0 && done.links.some((href) => href.includes("arxiv.org")) && decision.action === "reject_link_substitution" && external.length === 0,
    });
  }

  await page.screenshot({ path: path.join(outDir, "editor-faults.png") });
  await context.close();
  const failures = cases.filter((item) => item.id !== "delayed_ack_eager_retry" && item.success !== true).map((item) => item.id);
  if (!cases.find((item) => item.id === "delayed_ack_eager_retry" && item.duplicate_detected)) failures.push("eager retry did not duplicate");
  if (external.length) failures.push("external request");
  return {
    kind: "synthetic_editor_faults_not_a_publish",
    model_calls: 0,
    llm_ab_executed: false,
    not_a_semantic_win: ["hybrid_replace_with_backup"],
    public_bundle_note: "The fetched editor.csdn.net/md app chunk contained no CodeMirror identifier. This hybrid arm is the fixture's own documented replace/undo API, not a CSDN endpoint.",
    reusable_strategy: "wait while aria-busy; reject link substitution; undo a duplicated document; switch to the editor API only after integrity fails",
    csdn_specific_helpers_used: [],
    external_requests: external,
    cases,
    trace,
    failures,
  };
}
