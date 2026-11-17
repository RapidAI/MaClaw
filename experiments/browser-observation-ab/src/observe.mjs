const INTERACTIVE = new Set([
  "button",
  "link",
  "textbox",
  "searchbox",
  "combobox",
  "checkbox",
  "radio",
  "menuitem",
  "tab",
  "switch",
  "slider",
  "option",
  "spinbutton",
]);

const COLLECT_IN_FRAME = `() => {
  const implicit = (el) => {
    const tag = el.localName;
    const type = (el.getAttribute("type") || "").toLowerCase();
    if (tag === "a" && el.hasAttribute("href")) return "link";
    if (tag === "button" || tag === "summary") return "button";
    if (tag === "select") return "combobox";
    if (tag === "textarea") return "textbox";
    if (tag === "input") {
      if (type === "checkbox") return "checkbox";
      if (type === "radio") return "radio";
      if (type === "search") return "searchbox";
      if (type === "button" || type === "submit" || type === "reset") return "button";
      return "textbox";
    }
    return (el.getAttribute("role") || tag || "").toLowerCase();
  };
  const nameOf = (el) => {
    const labelled = el.labels && el.labels[0] ? el.labels[0].innerText : "";
    return String(
      el.getAttribute("aria-label") ||
        labelled ||
        el.getAttribute("placeholder") ||
        el.innerText ||
        el.value ||
        "",
    )
      .replace(/\\s+/g, " ")
      .trim()
      .slice(0, 160);
  };
  window.__benchSeq = window.__benchSeq || 1;
  const selector = 'a[href],button,input:not([type="hidden"]),textarea,select,summary,[role]';
  const rows = [];
  for (const el of document.querySelectorAll(selector)) {
    const rect = el.getBoundingClientRect();
    if (rect.width < 2 || rect.height < 2) continue;
    const style = getComputedStyle(el);
    if (style.display === "none" || style.visibility === "hidden") continue;
    if (!el.__benchId) el.__benchId = "b" + window.__benchSeq++;
    rows.push({
      bench_id: el.__benchId,
      role: (el.getAttribute("role") || implicit(el) || "").toLowerCase(),
      name: nameOf(el),
      tag: el.localName,
      type: (el.getAttribute("type") || "").toLowerCase(),
      gen: el.getAttribute("data-gen") || "",
    });
  }
  return {
    href: location.href,
    rows,
    canvas: document.querySelectorAll("canvas").length,
    text_sample: String(document.body ? document.body.innerText : "").replace(/\\s+/g, " ").trim().slice(0, 500),
  };
}`;

function namesInclude(rows, needle) {
  return rows.some((row) => String(row.name || "").includes(needle) || String(row.text || "").includes(needle));
}

export async function topDocumentWalk(page) {
  const started = Date.now();
  const value = await page.evaluate(`(() => {
    const read = ${COLLECT_IN_FRAME};
    const own = read();
    const frames = [];
    for (const iframe of document.querySelectorAll("iframe,frame")) {
      const src = iframe.getAttribute("src") || "";
      try {
        const doc = iframe.contentDocument;
        if (!doc) {
          frames.push({ src, blocked: true, reason: "null contentDocument" });
          continue;
        }
        const probe = doc.querySelector("button,input,a,textarea,select");
        frames.push({
          src,
          blocked: false,
          button_text: probe ? String(probe.innerText || probe.value || "").trim().slice(0, 160) : "",
        });
      } catch (error) {
        frames.push({ src, blocked: true, reason: String(error && error.name || "exception") });
      }
    }
    return { own, frames };
  })()`);
  return {
    channel: "top_document_dom",
    ms: Date.now() - started,
    controls: value.own.rows.map(publicRow),
    iframe_probes: value.frames,
    canvas: value.own.canvas,
  };
}

export async function frameSessionDom(page) {
  const started = Date.now();
  const frames = [];
  for (const frame of page.frames()) {
    try {
      const value = await frame.evaluate(`(${COLLECT_IN_FRAME})()`);
      frames.push({ url: frame.url(), error: null, ...value });
    } catch (error) {
      frames.push({ url: frame.url(), error: String(error.message || error).slice(0, 300), rows: [], canvas: 0, text_sample: "" });
    }
  }
  return {
    channel: "playwright_frame_dom",
    ms: Date.now() - started,
    frames: frames.map((frame) => ({
      url: frame.url,
      error: frame.error,
      canvas: frame.canvas || 0,
      text_has_canvas_phrase: String(frame.text_sample || "").includes("把滑块拖到右侧"),
      controls: (frame.rows || []).map(publicRow),
    })),
  };
}

export async function ariaSnapshots(page) {
  const started = Date.now();
  const frames = [];
  for (const frame of page.frames()) {
    try {
      const text = await frame.locator("body").ariaSnapshot({ timeout: 3000 });
      frames.push({ url: frame.url(), error: null, text: String(text || "").slice(0, 4000) });
    } catch (error) {
      frames.push({ url: frame.url(), error: String(error.message || error).slice(0, 300), text: "" });
    }
  }
  return { channel: "playwright_aria_snapshot", ms: Date.now() - started, frames };
}

function axNames(nodes) {
  const out = [];
  for (const node of nodes || []) {
    if (node.ignored) continue;
    const role = String(node.role && node.role.value || "").toLowerCase();
    if (!INTERACTIVE.has(role)) continue;
    out.push({
      role,
      name: String(node.name && node.name.value || "").slice(0, 160),
      backend_node_id: node.backendDOMNodeId || 0,
    });
  }
  return out;
}

export async function cdpAccessibility(page) {
  const started = Date.now();
  const cdp = await page.context().newCDPSession(page);
  try {
    await cdp.send("Accessibility.enable");
    const tree = await cdp.send("Page.getFrameTree");
    const frames = [];
    const walk = (node) => {
      frames.push({
        id: node.frame.id,
        url: node.frame.url,
        parent_id: node.frame.parentId || "",
      });
      for (const child of node.childFrames || []) walk(child);
    };
    walk(tree.frameTree);
    const perFrame = [];
    for (const frame of frames) {
      try {
        const ax = await cdp.send("Accessibility.getFullAXTree", { frameId: frame.id });
        perFrame.push({
          frame_id: frame.id,
          url: frame.url,
          error: null,
          node_count: (ax.nodes || []).length,
          controls: axNames(ax.nodes),
        });
      } catch (error) {
        perFrame.push({
          frame_id: frame.id,
          url: frame.url,
          error: String(error.message || error).slice(0, 300),
          node_count: 0,
          controls: [],
        });
      }
    }
    let aggregated = { error: null, node_count: 0, controls: [] };
    try {
      const ax = await cdp.send("Accessibility.getFullAXTree");
      aggregated = { error: null, node_count: (ax.nodes || []).length, controls: axNames(ax.nodes) };
    } catch (error) {
      aggregated = { error: String(error.message || error).slice(0, 300), node_count: 0, controls: [] };
    }
    return {
      channel: "cdp_accessibility_main_session",
      ms: Date.now() - started,
      frame_tree: frames,
      per_frame: perFrame,
      aggregated,
    };
  } finally {
    await cdp.detach().catch(() => {});
  }
}

function publicRow(row) {
  return { role: row.role, name: row.name, tag: row.tag, type: row.type, gen: row.gen };
}

export function channelSeesName(observation, needle) {
  if (!needle) return false;
  if (observation.channel === "top_document_dom") {
    return namesInclude(observation.controls, needle) ||
      observation.iframe_probes.some((probe) => !probe.blocked && String(probe.button_text || "").includes(needle));
  }
  if (observation.channel === "playwright_frame_dom") {
    return observation.frames.some((frame) => namesInclude(frame.controls, needle) || frame.text_has_canvas_phrase && needle.includes("滑块"));
  }
  if (observation.channel === "playwright_aria_snapshot") {
    return observation.frames.some((frame) => String(frame.text || "").includes(needle));
  }
  if (observation.channel === "cdp_accessibility_main_session") {
    const rows = [
      ...observation.aggregated.controls,
      ...observation.per_frame.flatMap((frame) => frame.controls),
    ];
    return namesInclude(rows, needle);
  }
  return false;
}

export async function semanticSnapshot(page) {
  const started = Date.now();
  const store = new Map();
  const lines = [];
  let canvas = 0;
  let index = 1;
  for (const frame of page.frames()) {
    let value;
    try {
      value = await frame.evaluate(`(${COLLECT_IN_FRAME})()`);
    } catch (error) {
      lines.push(`# frame unreadable ${String(error.message || error).slice(0, 120)}`);
      continue;
    }
    canvas += value.canvas || 0;
    for (const row of value.rows) {
      const ref = "r" + index;
      index += 1;
      store.set(ref, { frame, benchId: row.bench_id, role: row.role, name: row.name, type: row.type, gen: row.gen });
      lines.push(`${ref} ${row.role} "${row.name}"`);
    }
  }
  return {
    ms: Date.now() - started,
    text: lines.join("\n"),
    store,
    canvas,
    visual_gap: canvas > 0 && ![...store.values()].some((row) => row.role === "slider" || row.name.includes("滑块")),
    public_refs: [...store.entries()].map(([ref, row]) => ({ ref, role: row.role, name: row.name })),
  };
}

export async function clickRef(store, ref) {
  const item = store.get(ref);
  if (!item) return { ok: false, error: "unknown_ref", invalid: true };
  let found;
  try {
    found = await item.frame.evaluate((benchId) => {
      const element = [...document.querySelectorAll("*")].find((node) => node.__benchId === benchId);
      if (!element || !element.isConnected) return { ok: false };
      const rect = element.getBoundingClientRect();
      if (rect.width < 2 || rect.height < 2) return { ok: false };
      if ((element.getAttribute("type") || "").toLowerCase() === "password") return { ok: false, secret: true };
      element.click();
      return { ok: true, secret: (element.getAttribute("type") || "").toLowerCase() === "password" };
    }, item.benchId);
  } catch (error) {
    return { ok: false, error: "stale_ref", invalid: true, detail: String(error.message || error).slice(0, 180) };
  }
  if (found && found.secret) return { ok: false, error: "secret_field_refused", invalid: true };
  if (!found || !found.ok) return { ok: false, error: "stale_ref", invalid: true };
  return { ok: true };
}

export async function typeRef(page, store, ref, text) {
  const item = store.get(ref);
  if (!item) return { ok: false, error: "unknown_ref", invalid: true };
  if (item.type === "password") return { ok: false, error: "secret_field_refused", invalid: true };
  let focused;
  try {
    focused = await item.frame.evaluate((benchId) => {
      const element = [...document.querySelectorAll("*")].find((node) => node.__benchId === benchId);
      if (!element || !element.isConnected) return { ok: false };
      if ((element.getAttribute("type") || "").toLowerCase() === "password") return { ok: false, secret: true };
      element.focus();
      return { ok: true };
    }, item.benchId);
  } catch (error) {
    return { ok: false, error: "stale_ref", invalid: true, detail: String(error.message || error).slice(0, 180) };
  }
  if (focused && focused.secret) return { ok: false, error: "secret_field_refused", invalid: true };
  if (!focused || !focused.ok) return { ok: false, error: "stale_ref", invalid: true };
  await page.keyboard.type(text);
  return { ok: true };
}

export async function boxForName(page, name) {
  for (const frame of page.frames()) {
    const locator = frame.getByRole("button", { name, exact: true }).or(frame.getByRole("textbox", { name, exact: true }));
    const count = await locator.count().catch(() => 0);
    if (!count) continue;
    const target = locator.first();
    await target.scrollIntoViewIfNeeded().catch(() => {});
    const box = await target.boundingBox();
    if (box) return box;
  }
  return null;
}
