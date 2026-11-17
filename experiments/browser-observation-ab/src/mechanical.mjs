import {
  ariaSnapshots,
  boxForName,
  cdpAccessibility,
  channelSeesName,
  clickRef,
  frameSessionDom,
  semanticSnapshot,
  topDocumentWalk,
  typeRef,
} from "./observe.mjs";
import { runEfficiency } from "./policies.mjs";
import { decodePNG, isGreen, isMagenta, isYellow, sampleRegion } from "./png.mjs";

async function outcome(page) {
  return page.evaluate(() => (typeof window.__benchOutcome === "function" ? window.__benchOutcome() : null));
}

async function goto(page, url, external) {
  const onRequest = (request) => {
    const target = request.url();
    if (target.startsWith("data:") || target.startsWith("about:")) return;
    const host = new URL(target).hostname;
    if (host !== "127.0.0.1") external.push(target.slice(0, 180));
  };
  page.on("request", onRequest);
  await page.goto(url, { waitUntil: "domcontentloaded", timeout: 15000 });
}

function center(box) {
  return { x: box.x + box.width / 2, y: box.y + box.height / 2 };
}

async function clickXY(page, box) {
  const point = center(box);
  const hit = await page.evaluate(({ x, y }) => {
    const element = document.elementFromPoint(x, y);
    return element ? `${element.tagName}:${(element.innerText || "").slice(0, 24)}` : "none";
  }, point);
  await page.mouse.click(point.x, point.y);
  await page.waitForTimeout(100);
  return { ok: true, point, hit };
}

async function typeXY(page, box, text) {
  const secret = await page.evaluate(({ x, y }) => {
    const element = document.elementFromPoint(x, y);
    return element ? (element.getAttribute("type") || "").toLowerCase() : "";
  }, center(box));
  if (secret === "password") return { ok: false, error: "secret_field_refused", invalid: true };
  await clickXY(page, box);
  await page.keyboard.type(text);
  return { ok: true };
}

async function handoff(page) {
  await page.evaluate(() => {
    window.__humanCompleted = true;
  });
  return { ok: true, human_completed_by_harness: true };
}

function refFor(snapshot, name, gen) {
  for (const [ref, row] of snapshot.store.entries()) {
    if (row.name !== name) continue;
    if (gen && row.gen !== gen) continue;
    return ref;
  }
  return "";
}

async function extraction(page, needle) {
  const started = Date.now();
  const top = await topDocumentWalk(page);
  const frames = await frameSessionDom(page);
  const aria = await ariaSnapshots(page);
  const ax = await cdpAccessibility(page);
  const pngBuffer = await page.screenshot({ type: "png" });
  const png = decodePNG(pngBuffer);
  return {
    ms: Date.now() - started,
    needle,
    sees: {
      top_document_dom: channelSeesName(top, needle),
      playwright_frame_dom: channelSeesName(frames, needle),
      playwright_aria_snapshot: channelSeesName(aria, needle),
      cdp_accessibility_main_session: channelSeesName(ax, needle),
    },
    top,
    frames,
    aria,
    ax,
    screenshot: {
      bytes: pngBuffer.length,
      decoded: Boolean(png),
      width: png ? png.width : 0,
      height: png ? png.height : 0,
    },
    png,
    pngBuffer,
  };
}

function probe(png, box, predicate) {
  const sampled = sampleRegion(png, box, predicate);
  return { ...sampled, hit: sampled.hits >= 3, box };
}

async function runNamed(page, steps) {
  const actions = [];
  let snapshot = await semanticSnapshot(page);
  for (const step of steps) {
    if (step.op === "type") {
      const ref = refFor(snapshot, step.name);
      const result = await typeRef(page, snapshot.store, ref, step.text);
      actions.push({ op: "type_ref", name: step.name, ref, ...result });
      if (!result.ok) return { actions, snapshot_text: snapshot.text };
    } else if (step.op === "click") {
      if (step.refresh) snapshot = await semanticSnapshot(page);
      const ref = refFor(snapshot, step.name);
      const result = await clickRef(snapshot.store, ref);
      actions.push({ op: "click_ref", name: step.name, ref, ...result });
      if (!result.ok) return { actions, snapshot_text: snapshot.text };
    }
  }
  return { actions, snapshot_text: snapshot.text };
}

async function runNamedXY(page, steps) {
  const actions = [];
  for (const step of steps) {
    const box = await boxForName(page, step.name);
    if (!box) {
      actions.push({ op: step.op, name: step.name, ok: false, error: "no_box" });
      return { actions };
    }
    if (step.op === "type") actions.push({ op: "type_xy", name: step.name, ...(await typeXY(page, box, step.text)) });
    else actions.push({ op: "click_xy", name: step.name, ...(await clickXY(page, box)) });
  }
  return { actions };
}

const FORM_STEPS = [
  { op: "type", name: "姓名", text: "合成用户" },
  { op: "type", name: "邮箱", text: "synthetic@example.test" },
  { op: "type", name: "备注", text: "这是合成备注" },
  { op: "click", name: "提交合成表单" },
];

const SEARCH_STEPS = [
  { op: "type", name: "关键词", text: "苹果" },
  { op: "click", name: "类别-杯子" },
  { op: "click", name: "筛选" },
];

async function iframeSteps(payName) {
  return [
    { op: "click", name: payName },
    { op: "click", name: "确认 0 元合成订单", refresh: true },
  ];
}

async function prepareIframe(page, payName) {
  await page.frameLocator("iframe").getByRole("button", { name: payName, exact: true }).waitFor({ timeout: 8000 });
}

async function staleCase(page, arm) {
  const first = await semanticSnapshot(page);
  const ref = refFor(first, "确认合成订单", "1");
  const box = await boxForName(page, "确认合成订单");
  await page.waitForFunction(() => window.__gen === 2, null, { timeout: 10000 });
  if (arm === "semantic") {
    const stale = await clickRef(first.store, ref);
    const mid = await outcome(page);
    const second = await semanticSnapshot(page);
    const next = refFor(second, "确认合成订单", "2");
    const click = next ? await clickRef(second.store, next) : { ok: false, error: "missing_gen2" };
    return {
      arm,
      gen1_ref: ref,
      stale,
      clicked_gen_after_stale: mid && mid.detail.clicked_gen,
      fresh: click,
      outcome: await outcome(page),
    };
  }
  const oldClick = box ? await clickXY(page, box) : { ok: false, error: "no_box" };
  const afterOld = await outcome(page);
  const freshBox = await boxForName(page, "确认合成订单");
  const freshClick = freshBox ? await clickXY(page, freshBox) : { ok: false, error: "no_fresh_box" };
  return {
    arm,
    old_point: oldClick,
    outcome_after_old_point: afterOld,
    fresh_point: freshClick,
    fresh_point_note: "fresh coordinates come from the harness bounding box, not from a vision model",
    outcome: await outcome(page),
  };
}

export async function runOnce(browser, origin, saveShot) {
  const external = [];
  const failures = [];
  const context = await browser.newContext({
    viewport: { width: 1100, height: 720 },
    deviceScaleFactor: 1,
    locale: "zh-CN",
  });
  const page = await context.newPage();
  const check = (name, ok, detail) => {
    if (!ok) failures.push({ name, detail });
  };

  await goto(page, `${origin}/calibration`, external);
  const calibration = await extraction(page, "");
  const swatch = probe(calibration.png, { x: 10, y: 10, width: 40, height: 40 }, isGreen);
  check("pixel_calibration", swatch.hit, swatch);
  if (saveShot) await saveShot("calibration.png", calibration.pngBuffer);

  async function measure(id, url, needle, pixel) {
    await goto(page, origin + url, external);
    const data = await extraction(page, needle);
    let pixels = null;
    if (pixel) {
      const box = await boxForName(page, pixel.name).catch(() => null);
      const target = pixel.box || box;
      pixels = probe(data.png, target, pixel.predicate);
    }
    if (saveShot) await saveShot(`${id}.png`, data.pngBuffer);
    const { png, pngBuffer, ...serial } = data;
    return { ...serial, pixel: pixels };
  }

  const formObserved = await measure("form_fill", "/form", "提交合成表单", {
    name: "提交合成表单",
    predicate: isMagenta,
  });
  await goto(page, `${origin}/form`, external);
  const formSemantic = await runNamed(page, FORM_STEPS);
  formSemantic.outcome = await outcome(page);
  await goto(page, `${origin}/form`, external);
  const formCoordinate = await runNamedXY(page, FORM_STEPS);
  formCoordinate.outcome = await outcome(page);
  check("form_top_dom", formObserved.sees.top_document_dom, formObserved.sees);
  check("form_semantic", formSemantic.outcome && formSemantic.outcome.success, formSemantic.outcome);
  check("form_coordinate", formCoordinate.outcome && formCoordinate.outcome.success, formCoordinate.outcome);
  check("form_pixel", formObserved.pixel && formObserved.pixel.hit, formObserved.pixel);

  const searchObserved = await measure("search_filter", "/search", "筛选", null);
  await goto(page, `${origin}/search`, external);
  const searchSemantic = await runNamed(page, SEARCH_STEPS);
  searchSemantic.outcome = await outcome(page);
  await goto(page, `${origin}/search`, external);
  const searchCoordinate = await runNamedXY(page, SEARCH_STEPS);
  searchCoordinate.outcome = await outcome(page);
  check("search_frame_dom", searchObserved.sees.playwright_frame_dom, searchObserved.sees);
  check("search_semantic", searchSemantic.outcome && searchSemantic.outcome.success, searchSemantic.outcome);
  check("search_coordinate", searchCoordinate.outcome && searchCoordinate.outcome.success, searchCoordinate.outcome);

  const staleObserved = await measure("delayed_stale", "/stale?shift=1&delay=60000", "确认合成订单", null);
  await goto(page, `${origin}/stale?shift=0&delay=8000`, external);
  const staleSameSemantic = await staleCase(page, "semantic");
  await goto(page, `${origin}/stale?shift=0&delay=8000`, external);
  const staleSameCoordinate = await staleCase(page, "coordinate");
  await goto(page, `${origin}/stale?shift=1&delay=8000`, external);
  const staleShiftSemantic = await staleCase(page, "semantic");
  await goto(page, `${origin}/stale?shift=1&delay=8000`, external);
  const staleShiftCoordinate = await staleCase(page, "coordinate");
  check("stale_same_ref_rejected", staleSameSemantic.stale && staleSameSemantic.stale.error === "stale_ref", staleSameSemantic.stale);
  check("stale_same_old_ref_did_not_click", staleSameSemantic.clicked_gen_after_stale === 0, staleSameSemantic);
  check("stale_same_fresh_ref", staleSameSemantic.outcome && staleSameSemantic.outcome.success, staleSameSemantic.outcome);
  check(
    "stale_same_old_point_hits_replacement",
    staleSameCoordinate.outcome_after_old_point && staleSameCoordinate.outcome_after_old_point.success,
    staleSameCoordinate.outcome_after_old_point,
  );
  check("stale_shift_old_ref_rejected", staleShiftSemantic.stale && staleShiftSemantic.stale.error === "stale_ref", staleShiftSemantic.stale);
  check("stale_shift_fresh_ref", staleShiftSemantic.outcome && staleShiftSemantic.outcome.success, staleShiftSemantic.outcome);
  check(
    "stale_shift_old_point_misses",
    staleShiftCoordinate.outcome_after_old_point && staleShiftCoordinate.outcome_after_old_point.success === false,
    staleShiftCoordinate.outcome_after_old_point,
  );
  check("stale_shift_fresh_point", staleShiftCoordinate.outcome && staleShiftCoordinate.outcome.success, staleShiftCoordinate.outcome);

  async function iframeCase(id, mode, payName) {
    const observed = await measure(id, `/iframe?mode=${mode}`, payName, { name: payName, predicate: isMagenta });
    const steps = await iframeSteps(payName);
    await goto(page, `${origin}/iframe?mode=${mode}`, external);
    await prepareIframe(page, payName);
    const semantic = await runNamed(page, steps);
    semantic.outcome = await outcome(page);
    await goto(page, `${origin}/iframe?mode=${mode}`, external);
    await prepareIframe(page, payName);
    const coordinate = await runNamedXY(page, [
      { op: "click", name: payName },
      { op: "click", name: "确认 0 元合成订单" },
    ]);
    coordinate.outcome = await outcome(page);
    return { observed, semantic, coordinate };
  }

  const iframeSame = await iframeCase("iframe_same", "same", "合成支付（同域iframe）");
  const iframeCross = await iframeCase("iframe_cross", "cross", "合成支付（跨域iframe）");
  const sameProbe = iframeSame.observed.top.iframe_probes.find((item) => item.src.includes("/frame"));
  const crossProbe = iframeCross.observed.top.iframe_probes.find((item) => item.src.includes("/frame"));
  check("iframe_same_content_document", Boolean(sameProbe && sameProbe.blocked === false && String(sameProbe.button_text).includes("同域")), sameProbe);
  check("iframe_cross_content_document_blocked", Boolean(crossProbe && crossProbe.blocked === true), crossProbe);
  check("iframe_cross_frame_session_sees_button", iframeCross.observed.sees.playwright_frame_dom, iframeCross.observed.sees);
  check("iframe_same_semantic", iframeSame.semantic.outcome && iframeSame.semantic.outcome.success, iframeSame.semantic.outcome);
  check("iframe_same_coordinate", iframeSame.coordinate.outcome && iframeSame.coordinate.outcome.success, iframeSame.coordinate.outcome);
  check("iframe_cross_semantic", iframeCross.semantic.outcome && iframeCross.semantic.outcome.success, iframeCross.semantic.outcome);
  check("iframe_cross_coordinate", iframeCross.coordinate.outcome && iframeCross.coordinate.outcome.success, iframeCross.coordinate.outcome);
  check("iframe_cross_pixel", iframeCross.observed.pixel && iframeCross.observed.pixel.hit, iframeCross.observed.pixel);

  const canvasObserved = await measure("canvas_drag", "/canvas", "把滑块拖到右侧（仅画布）", null);
  const canvasShot = decodePNG(await page.screenshot({ type: "png" }));
  const canvasBox = await page.locator("canvas").boundingBox();
  const knob = canvasBox ? { x: canvasBox.x + 16, y: canvasBox.y + 66, width: 28, height: 32 } : null;
  const canvasPixel = probe(canvasShot, knob, isYellow);
  await goto(page, `${origin}/canvas`, external);
  const canvasSemantic = await semanticSnapshot(page);
  const canvasSemanticHit = [...canvasSemantic.store.values()].some((row) => row.name.includes("把滑块拖到右侧"));
  await goto(page, `${origin}/canvas`, external);
  await page.waitForFunction(() => window.__oracle, null, { timeout: 3000 });
  const oracle = await page.evaluate(() => window.__oracle || null);
  if (oracle) {
    await page.mouse.move(oracle.x1, oracle.y1);
    await page.mouse.down();
    await page.mouse.move(oracle.x2, oracle.y2, { steps: 15 });
    await page.mouse.up();
  }
  const canvasCoordinate = { oracle, outcome: await outcome(page), note: "drag points are fixture instrumentation, not a model reading the screenshot" };
  check("canvas_phrase_absent_from_semantic_refs", canvasSemanticHit === false, canvasSemantic.public_refs);
  check("canvas_phrase_absent_from_top_dom_names", canvasObserved.sees.top_document_dom === false, canvasObserved.sees);
  check("canvas_yellow_in_screenshot", canvasPixel.hit, canvasPixel);
  check("canvas_coordinate_drag", canvasCoordinate.outcome && canvasCoordinate.outcome.success, canvasCoordinate.outcome);

  await goto(page, `${origin}/password`, external);
  const passwordObserved = await extraction(page, "合成密码（请交还人工）");
  const passwordSnap = await semanticSnapshot(page);
  const passwordRef = refFor(passwordSnap, "合成密码（请交还人工）");
  const refused = await typeRef(page, passwordSnap.store, passwordRef, "dummy-secret-not-used");
  const afterRefusal = await outcome(page);
  const handed = await handoff(page);
  const passwordOutcome = await outcome(page);
  const passwordSuccess =
    handed.human_completed_by_harness === true &&
    passwordOutcome.detail.human_completed === true &&
    passwordOutcome.detail.password_len === 0 &&
    passwordOutcome.detail.password_typed === false;
  check("password_type_refused", refused.error === "secret_field_refused", refused);
  check("password_unchanged_before_handoff", afterRefusal.detail.password_len === 0 && afterRefusal.detail.human_completed === false, afterRefusal);
  check("password_handoff_wiring", passwordSuccess, passwordOutcome);
  const { png, pngBuffer, ...passwordSerial } = passwordObserved;
  const efficiency = await runEfficiency(page, origin, external);
  for (const summary of efficiency.summaries) {
    if (summary.success === false && summary.task !== "canvas_drag") {
      failures.push({ name: `efficiency:${summary.task}:${summary.arm}:${summary.policy}`, detail: summary.outcome || summary });
    }
  }

  await context.close();
  return {
    kind: "mechanical_and_interface_upper_bound_not_model_ab",
    external_requests: external,
    failures,
    tasks: {
      form_fill: { observed: stripPng(formObserved), semantic_upper_bound: publicActions(formSemantic), coordinate_upper_bound: publicActions(formCoordinate) },
      search_filter: { observed: stripPng(searchObserved), semantic_upper_bound: publicActions(searchSemantic), coordinate_upper_bound: publicActions(searchCoordinate) },
      delayed_stale: {
        observed: stripPng(staleObserved),
        same_place: { semantic: publicStale(staleSameSemantic), coordinate: publicStale(staleSameCoordinate) },
        shifted: { semantic: publicStale(staleShiftSemantic), coordinate: publicStale(staleShiftCoordinate) },
      },
      iframe_same: publicIframe(iframeSame),
      iframe_cross: publicIframe(iframeCross),
      canvas_drag: {
        observed: stripPng(canvasObserved),
        pixel: canvasPixel,
        semantic_upper_bound: { found_canvas_phrase_ref: canvasSemanticHit, visual_gap: canvasSemantic.visual_gap, refs: canvasSemantic.public_refs },
        coordinate_upper_bound: canvasCoordinate,
      },
      password_handoff: {
        observed_sees: passwordSerial.sees,
        type_refused: refused,
        handoff: handed,
        outcome: passwordOutcome,
        wiring_success: passwordSuccess,
        note: "wiring check: the harness sets human_completed only inside handoff. This is not a model trial.",
      },
    },
    efficiency,
  };
}

function stripPng(data) {
  if (!data) return data;
  const { png, pngBuffer, ...rest } = data;
  return rest;
}

function publicActions(result) {
  return {
    actions: (result.actions || []).map(({ point, ...action }) => action),
    outcome: result.outcome || null,
  };
}

function publicStale(result) {
  const { arm, ...rest } = result;
  return { arm, ...rest };
}

function publicIframe(result) {
  return {
    sees: result.observed.sees,
    iframe_probes: result.observed.top.iframe_probes,
    ax_per_frame_errors: result.observed.ax.per_frame.map((frame) => ({ url: frame.url, error: frame.error, controls: frame.controls.length })),
    ax_aggregated_names: result.observed.ax.aggregated.controls.map((row) => row.name),
    pixel: result.observed.pixel,
    semantic_upper_bound: publicActions(result.semantic),
    coordinate_upper_bound: publicActions(result.coordinate),
  };
}
