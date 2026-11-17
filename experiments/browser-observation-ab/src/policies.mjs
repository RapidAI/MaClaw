import { boxForName, clickRef, semanticSnapshot, typeRef } from "./observe.mjs";

const NAMES = [
  "姓名",
  "邮箱",
  "备注",
  "提交合成表单",
  "关键词",
  "类别-全部",
  "类别-贴纸",
  "类别-杯子",
  "筛选",
  "确认合成订单",
  "合成支付（同域iframe）",
  "合成支付（跨域iframe）",
  "确认 0 元合成订单",
  "稍后",
  "合成密码（请交还人工）",
];

const FORM = [
  { op: "type", name: "姓名", text: "合成用户" },
  { op: "type", name: "邮箱", text: "synthetic@example.test" },
  { op: "type", name: "备注", text: "这是合成备注" },
  { op: "click", name: "提交合成表单" },
];

function center(box) {
  return { x: box.x + box.width / 2, y: box.y + box.height / 2 };
}

async function outcome(page) {
  return page.evaluate(() => (typeof window.__benchOutcome === "function" ? window.__benchOutcome() : null));
}

function hasTarget(arm, snap, name) {
  if (!snap) return false;
  if (arm === "semantic") return [...snap.store.values()].some((row) => row.name === name);
  return Boolean(snap.boxes && snap.boxes[name]);
}

export async function runEfficiency(page, origin, external) {
  const events = [];
  const push = (event) => events.push({ seq: events.length, model_call: false, ...event });

  async function open(url) {
    const onRequest = (request) => {
      const target = request.url();
      if (target.startsWith("data:") || target.startsWith("about:")) return;
      if (new URL(target).hostname !== "127.0.0.1") external.push(target.slice(0, 180));
    };
    page.on("request", onRequest);
    await page.goto(origin + url, { waitUntil: "domcontentloaded", timeout: 15000 });
  }

  async function visibleBoxes(current) {
    const boxes = {};
    for (const name of NAMES) {
      const box = await boxForName(current, name);
      if (box) boxes[name] = box;
    }
    return boxes;
  }

  async function observe(arm, task, policy, toolRound) {
    if (arm === "semantic") {
      const started = Date.now();
      const snap = await semanticSnapshot(page);
      push({
        kind: "observation",
        task,
        policy,
        arm,
        tool_round: toolRound,
        channel: "semantic_snapshot",
        ms: Date.now() - started,
        bytes: Buffer.byteLength(snap.text),
        refs: snap.public_refs.length,
        visual_gap: snap.visual_gap,
        transfer_ms: null,
      });
      return snap;
    }
    const started = Date.now();
    const buffer = await page.screenshot({ type: "png" });
    const shotMs = Date.now() - started;
    const boxStarted = Date.now();
    const boxes = await visibleBoxes(page);
    push({
      kind: "observation",
      task,
      policy,
      arm,
      tool_round: toolRound,
      channel: "screenshot",
      ms: shotMs,
      bytes: buffer.length,
      harness_bbox_ms: Date.now() - boxStarted,
      refs: Object.keys(boxes).length,
      transfer_ms: null,
      note: "screenshot ms excludes harness bbox probes. png bytes are not token costs. boxes are ground truth, not model output.",
    });
    return { boxes, bytes: buffer.length };
  }

  async function primitive(arm, task, policy, toolRound, step, snap) {
    const started = Date.now();
    let result;
    if (arm === "semantic") {
      const ref = [...snap.store.entries()].find(([, row]) => row.name === step.name)?.[0] || "";
      result = step.op === "type" ? await typeRef(page, snap.store, ref, step.text) : await clickRef(snap.store, ref);
      result = { ...result, ref };
    } else {
      const box = snap.boxes ? snap.boxes[step.name] : null;
      if (!box) result = { ok: false, error: "no_box_in_observation" };
      else if (step.op === "type") {
        const point = center(box);
        const secret = await page.evaluate(({ x, y }) => {
          const element = document.elementFromPoint(x, y);
          return element ? (element.getAttribute("type") || "").toLowerCase() : "";
        }, point);
        if (secret === "password") result = { ok: false, error: "secret_field_refused", invalid: true };
        else {
          await page.mouse.click(point.x, point.y);
          await page.keyboard.type(step.text);
          result = { ok: true, coordinate_source: "box_from_last_observation" };
        }
      } else {
        const point = center(box);
        await page.mouse.click(point.x, point.y);
        result = { ok: true, coordinate_source: "box_from_last_observation" };
      }
    }
    push({
      kind: "browser_primitive",
      task,
      policy,
      arm,
      tool_round: toolRound,
      op: step.op,
      name: step.name,
      ms: Date.now() - started,
      ok: Boolean(result.ok),
      error: result.error || null,
      chat_progress: true,
    });
    return result;
  }

  async function policyRun(task, url, arm, policy, steps, options = {}) {
    await open(url);
    if (options.prepare) await options.prepare(page);
    const wallStart = Date.now();
    let observations = 0;
    let primitives = 0;
    let retries = 0;
    let separateRounds = 0;
    let snap = null;
    const ensure = async (step, round) => {
      if (policy === "reobserve_each" || !hasTarget(arm, snap, step.name)) {
        if (snap && !hasTarget(arm, snap, step.name)) {
          retries += 1;
          push({
            kind: "invalidation",
            task,
            policy,
            arm,
            tool_round: round,
            name: step.name,
            reason: "target_absent_from_current_observation",
          });
        }
        snap = await observe(arm, task, policy, round);
        observations += 1;
      }
    };
    let failed = false;
    if (policy === "batched_tool") {
      separateRounds = 1;
      for (const step of steps) {
        await ensure(step, 1);
        const result = await primitive(arm, task, policy, 1, step, snap);
        primitives += 1;
        if (!result.ok) {
          failed = true;
          break;
        }
      }
      if (options.finalObservation !== false && !failed) {
        snap = await observe(arm, task, policy, 1);
        observations += 1;
      }
    } else {
      for (const step of steps) {
        separateRounds += 1;
        const round = separateRounds;
        if (policy === "reobserve_each") snap = null;
        await ensure(step, round);
        if (policy === "reuse_one_observation") separateRounds += 0;
        const actionRound = policy === "reobserve_each" ? round + steps.length : round;
        const result = await primitive(arm, task, policy, actionRound, step, snap);
        primitives += 1;
        if (policy === "reobserve_each") separateRounds += 1;
        if (!result.ok) {
          failed = true;
          break;
        }
      }
    }
    const pageOutcome = await outcome(page);
    const related = events.filter((event) => event.task === task && event.policy === policy && event.arm === arm);
    const observationMs = related.filter((event) => event.kind === "observation").reduce((sum, event) => sum + event.ms, 0);
    const primitiveMs = related.filter((event) => event.kind === "browser_primitive").reduce((sum, event) => sum + event.ms, 0);
    const observationBytes = related.filter((event) => event.kind === "observation").reduce((sum, event) => sum + (event.bytes || 0), 0);
    const harnessBboxMs = related.filter((event) => event.kind === "observation").reduce((sum, event) => sum + (event.harness_bbox_ms || 0), 0);
    const summary = {
      task,
      arm,
      policy,
      wall_ms: Date.now() - wallStart,
      observation_ms: observationMs,
      harness_bbox_ms: harnessBboxMs,
      primitive_ms: primitiveMs,
      observation_bytes: observationBytes,
      observations,
      browser_primitives: primitives,
      retries,
      chat_progress_events: primitives,
      tool_rounds_if_separate: policy === "batched_tool" ? 1 : observations + primitives,
      tool_rounds_if_batched: 1,
      model_calls: 0,
      screenshots: related.filter((event) => event.channel === "screenshot").length,
      snapshots: related.filter((event) => event.channel === "semantic_snapshot").length,
      success: !failed && Boolean(pageOutcome && pageOutcome.success),
      outcome: pageOutcome,
    };
    push({ kind: "policy_summary", ...summary });
    return summary;
  }

  const summaries = [];
  for (const arm of ["semantic", "coordinate"]) {
    for (const policy of ["reobserve_each", "reuse_one_observation", "batched_tool"]) {
      summaries.push(await policyRun("form_fill", "/form", arm, policy, FORM));
    }
  }
  for (const arm of ["semantic", "coordinate"]) {
    summaries.push(
      await policyRun(
        "iframe_cross",
        "/iframe?mode=cross",
        arm,
        "batched_tool",
        [
          { op: "click", name: "合成支付（跨域iframe）" },
          { op: "click", name: "确认 0 元合成订单" },
        ],
        {
          prepare: async (current) => {
            await current
              .frameLocator("iframe")
              .getByRole("button", { name: "合成支付（跨域iframe）", exact: true })
              .waitFor({ timeout: 8000 });
          },
        },
      ),
    );
  }

  await open("/stale?shift=1&delay=8000");
  const staleStart = Date.now();
  const first = await semanticSnapshot(page);
  push({
    kind: "observation",
    task: "delayed_stale",
    policy: "reuse_then_invalidate",
    arm: "semantic",
    channel: "semantic_snapshot",
    ms: Date.now() - staleStart,
    bytes: Buffer.byteLength(first.text),
  });
  const waitStart = Date.now();
  await page.waitForFunction(() => window.__gen === 2, null, { timeout: 10000 });
  const waitMs = Date.now() - waitStart;
  push({ kind: "fixture_wait", task: "delayed_stale", policy: "reuse_then_invalidate", arm: "semantic", ms: waitMs, note: "page delay, not model latency" });
  const ref = [...first.store.entries()].find(([, row]) => row.gen === "1")?.[0] || "";
  const staleStarted = Date.now();
  const stale = await clickRef(first.store, ref);
  push({
    kind: "browser_primitive",
    task: "delayed_stale",
    policy: "reuse_then_invalidate",
    arm: "semantic",
    op: "click",
    ms: Date.now() - staleStarted,
    ok: stale.ok,
    error: stale.error || null,
  });
  const recoverStart = Date.now();
  const second = await semanticSnapshot(page);
  push({
    kind: "observation",
    task: "delayed_stale",
    policy: "reuse_then_invalidate",
    arm: "semantic",
    channel: "semantic_snapshot",
    ms: Date.now() - recoverStart,
    bytes: Buffer.byteLength(second.text),
  });
  const next = [...second.store.entries()].find(([, row]) => row.gen === "2")?.[0] || "";
  const recovered = await clickRef(second.store, next);
  const staleOutcome = await outcome(page);
  summaries.push({
    task: "delayed_stale",
    arm: "semantic",
    policy: "reuse_then_invalidate",
    fixture_wait_ms: waitMs,
    observation_ms: events.filter((event) => event.task === "delayed_stale" && event.kind === "observation").reduce((sum, event) => sum + event.ms, 0),
    stale_error: stale.error || null,
    recovered: Boolean(recovered.ok),
    success: Boolean(staleOutcome && staleOutcome.success && stale.error === "stale_ref"),
    model_calls: 0,
    browser_primitives: 2,
    observations: 2,
    retries: 1,
    chat_progress_events: 2,
    tool_rounds_if_separate: 4,
    tool_rounds_if_batched: 1,
    screenshots: 0,
    snapshots: 2,
  });

  await open("/password");
  const passwordSnap = await semanticSnapshot(page);
  const passwordRef = [...passwordSnap.store.entries()].find(([, row]) => row.type === "password")?.[0] || "";
  const refused = await typeRef(page, passwordSnap.store, passwordRef, "not-a-real-secret");
  push({
    kind: "browser_primitive",
    task: "password_handoff",
    policy: "batched_tool",
    arm: "semantic",
    op: "type",
    ok: false,
    error: refused.error || null,
    note: "a batch may not type a password to save a step",
  });
  await page.evaluate(() => {
    window.__humanCompleted = true;
  });
  push({ kind: "browser_primitive", task: "password_handoff", policy: "batched_tool", arm: "semantic", op: "handoff", ok: true });
  const passwordOutcome = await outcome(page);
  summaries.push({
    task: "password_handoff",
    arm: "semantic",
    policy: "batched_tool",
    model_calls: 0,
    browser_primitives: 2,
    tool_rounds_if_batched: 1,
    secret_refused: refused.error === "secret_field_refused",
    success:
      refused.error === "secret_field_refused" &&
      passwordOutcome.detail.password_len === 0 &&
      passwordOutcome.detail.password_typed === false &&
      passwordOutcome.detail.human_completed === true,
    outcome: passwordOutcome,
  });

  return { events, summaries };
}
