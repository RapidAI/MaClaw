#!/usr/bin/env python3
"""Analyse maclaw intent shadow-mode logs (MACLAW_INTENT_SHADOW=1).

Reads the JSONL records written by corelib/intent/shadow.go (schema v1,
including rotated files *.jsonl.1 ...) and reports how the fully local L3
head compares with the remote L3 tree classifier.

IMPORTANT: the remote L3 verdict is a NOISY REFERENCE, NOT GROUND TRUTH.
"Agreement" below means agreement with the remote LLM, which itself makes
mistakes. Use it to find disagreements worth labelling and to pick
thresholds provisionally; final thresholds should be validated on
human-labelled rows (see --export-dir).

Pure standard library (Python 3.8+). Usage:
  python3 intent_shadow_analyze.py ~/.maclaw/logs/intent-shadow/ \
      [--target 0.95,0.97] [--min-n 50] [--export-dir out/] [--gold gold.jsonl]
"""
import argparse
import base64
import collections
import csv
import glob
import json
import math
import os
import struct
import sys

BANNER = ("NOTE: remote L3 is a noisy reference, NOT ground truth. "
          "'agree' = agrees with the remote LLM verdict.")


def load(paths):
    files = []
    for p in paths:
        if os.path.isdir(p):
            files += sorted(glob.glob(os.path.join(p, "*.jsonl*")))
        else:
            files += sorted(glob.glob(p))
    recs, bad = [], 0
    for f in files:
        with open(f, encoding="utf-8") as fh:
            for line in fh:
                line = line.strip()
                if not line:
                    continue
                try:
                    recs.append(json.loads(line))
                except json.JSONDecodeError:
                    bad += 1
    return files, recs, bad


def g(r, *path, default=None):
    cur = r
    for p in path:
        if not isinstance(cur, dict) or p not in cur:
            return default
        cur = cur[p]
    return cur


def wilson(k, n, z=1.96):
    if n == 0:
        return (float("nan"), float("nan"))
    p = k / n
    d = 1 + z * z / n
    c = (p + z * z / (2 * n)) / d
    h = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / d
    return (c - h, c + h)


def pct(xs, q):
    if not xs:
        return float("nan")
    xs = sorted(xs)
    i = min(len(xs) - 1, max(0, int(round(q * (len(xs) - 1)))))
    return xs[i]


def fmt_rate(k, n):
    if n == 0:
        return "n/a (n=0)"
    lo, hi = wilson(k, n)
    return f"{k/n:.3f} ({k}/{n}, 95% CI {lo:.3f}-{hi:.3f})"


def decode_emb(e):
    """Decode the optional float16 embedding field into a list of floats."""
    raw = base64.b64decode(e["b64"])
    return list(struct.unpack("<%de" % (len(raw) // 2), raw))


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("paths", nargs="+", help="log files, globs or directories")
    ap.add_argument("--target", default="0.95,0.97", help="target agreement-with-remote for τ suggestions")
    ap.add_argument("--min-n", type=int, default=50, help="minimum covered rows for a τ suggestion")
    ap.add_argument("--export-dir", help="write labelling exports (needs text=full rows)")
    ap.add_argument("--gold", help="optional jsonl {text,intent}: report true accuracy (validation/stub runs only)")
    ap.add_argument("--confusion-top", type=int, default=12)
    a = ap.parse_args()
    targets = [float(x) for x in a.target.split(",") if x]

    files, recs, bad = load(a.paths)
    print("=" * 78)
    print(BANNER)
    print("=" * 78)
    print(f"files: {len(files)}  records: {len(recs)}  unparsable lines: {bad}")
    if not recs:
        return
    by_kind = collections.Counter(r.get("kind") for r in recs)
    print("kinds:", dict(by_kind))
    print("model_id:", dict(collections.Counter(r.get("model_id") for r in recs)))
    print("head_id:", dict(collections.Counter(r.get("head_id") for r in recs)))
    print("text modes:", dict(collections.Counter(g(r, "text", "mode") for r in recs)))
    l3 = [r for r in recs if r.get("kind") == "l3"]
    l2s = [r for r in recs if r.get("kind") == "l2_sample"]

    # ---------------------------------------------------------------- overview
    print("\n## 1. L3-reaching turns: remote outcome / decided_by / local decision")
    print("remote outcome:", dict(collections.Counter(g(r, "remote", "outcome") for r in l3).most_common()))
    print("decided_by:", dict(collections.Counter(r.get("decided_by") for r in l3).most_common()))
    print("local decision:", dict(collections.Counter(g(r, "local", "decision") or ("error:" + str(g(r, "local", "error"))) for r in l3).most_common()))
    rl = [g(r, "remote", "latency_ms") for r in l3 if g(r, "remote", "called")]
    ll = [g(r, "local", "latency_us") for r in recs if g(r, "local", "latency_us") is not None]
    print(f"remote latency ms (called): p50 {pct(rl,.5):.1f}  p95 {pct(rl,.95):.1f}  max {max(rl) if rl else float('nan'):.1f}  n={len(rl)}")
    print(f"local head latency µs (worker, off the hot path): p50 {pct(ll,.5):.1f}  p95 {pct(ll,.95):.1f}  n={len(ll)}")
    ep = sum(1 for r in l3 if g(r, "remote", "endpoint_failure"))
    print(f"remote endpoint failures (fed the 10 s skip window): {ep}")

    # ---------------------------------------------------------------- agreement
    ok = [r for r in l3 if g(r, "remote", "outcome") == "ok" and g(r, "local", "label")]
    print(f"\n## 2. Agreement with remote (remote ok & local ran: n={len(ok)})")
    k = sum(1 for r in ok if g(r, "local", "label") == g(r, "remote", "label"))
    print("local top-1 == remote:", fmt_rate(k, len(ok)))
    acc = [r for r in ok if g(r, "local", "decision") == "accept"]
    ka = sum(1 for r in acc if g(r, "local", "label") == g(r, "remote", "label"))
    print(f"local coverage at its own τ/τ_s (accept): {len(acc)/len(ok) if ok else float('nan'):.3f} ({len(acc)}/{len(ok)})")
    print("agreement among accepted:", fmt_rate(ka, len(acc)))
    unk = [r for r in ok if g(r, "local", "decision") == "unknown"]
    print(f"local says confident 'unknown': {len(unk)}; local abstains (ambiguous): {sum(1 for r in ok if g(r,'local','decision')=='ambiguous')}")
    top3 = sum(1 for r in ok if g(r, "remote", "label") in [x["label"] for x in (g(r, "local", "topk") or [])[:3]])
    print("remote label within local top-3:", fmt_rate(top3, len(ok)))

    # ---------------------------------------------------------------- confusion
    print(f"\n## 3. Confusion (rows = remote, cols = local top-1; top {a.confusion_top} remote labels)")
    conf = collections.Counter((g(r, "remote", "label"), g(r, "local", "label")) for r in ok)
    rem_freq = collections.Counter(g(r, "remote", "label") for r in ok)
    labs = [l for l, _ in rem_freq.most_common(a.confusion_top)]
    w = max([len(x) for x in labs] + [6])
    print("  columns: " + "  ".join(f"[{i}]={l}" for i, l in enumerate(labs)))
    print(" " * w + " | " + " ".join(f"{'['+str(i)+']':>5}" for i in range(len(labs))) + " | other   n agree")
    for rl_ in labs:
        row = [conf[(rl_, cl)] for cl in labs]
        n = rem_freq[rl_]
        other = n - sum(row)
        print(f"{rl_:<{w}} | " + " ".join(f"{x:>6}" for x in row) + f" | {other:>5} {n:>3} {conf[(rl_, rl_)]/n:.2f}")
    print("most frequent disagreements (remote → local):")
    for (rr, ll_), c in sorted(((p, c) for p, c in conf.items() if p[0] != p[1]), key=lambda x: -x[1])[:10]:
        print(f"  {rr} → {ll_}: {c}")

    # ---------------------------------------------------------------- curves
    print("\n## 4. Coverage / agreement-with-remote vs threshold (recomputed from logged top-1 p)")
    sens_of = {}
    for r in ok:
        sens_of[g(r, "local", "label")] = g(r, "sensitive", "local")
    ns = [r for r in ok if not g(r, "sensitive", "local") and g(r, "local", "label") != "unknown"]
    ss = [r for r in ok if g(r, "sensitive", "local")]
    grid = [0.5, 0.6, 0.7, 0.75, 0.8, 0.82, 0.85, 0.88, 0.9, 0.92, 0.95, 0.97, 0.98, 0.99]

    def curve(rows, name):
        print(f"{name}: n={len(rows)}")
        print("   thr  coverage  agree(covered)        n_cov")
        out = []
        for t in grid:
            cov = [r for r in rows if g(r, "local", "p") >= t]
            kk = sum(1 for r in cov if g(r, "local", "label") == g(r, "remote", "label"))
            ag = kk / len(cov) if cov else float("nan")
            lo = wilson(kk, len(cov))[0] if cov else float("nan")
            out.append((t, len(cov), kk, ag, lo))
            print(f"  {t:.2f}  {len(cov)/len(rows) if rows else float('nan'):.3f}     {ag:.3f} (lo {lo:.3f})   {len(cov):>5}")
        return out

    cn = curve(ns, "τ (non-sensitive local top-1, excl. unknown)")
    cs = curve(ss, "τ_s (sensitive local top-1)")

    # ---------------------------------------------------------------- suggestions
    print(f"\n## 5. Suggested thresholds (smallest grid value whose agreement-with-remote ≥ target, n_cov ≥ {a.min_n})")
    for name, c in (("τ", cn), ("τ_s", cs)):
        for tg in targets:
            pick = next(((t, n, ag, lo) for t, n, kk, ag, lo in c if n >= a.min_n and ag >= tg), None)
            pick_lo = next(((t, n, ag, lo) for t, n, kk, ag, lo in c if n >= a.min_n and lo >= tg), None)
            s1 = f"{pick[0]:.2f} (agree {pick[2]:.3f}, n={pick[1]})" if pick else "none (not reachable / too few rows)"
            s2 = f"{pick_lo[0]:.2f}" if pick_lo else "none"
            print(f"  {name} @ target {tg:.2f}: point estimate {s1}; Wilson-lower-bound version {s2}")
    print("  (remote errors cap measurable agreement: if remote is ~X% accurate, agreement cannot exceed ~X%.")
    print("   'none' therefore often means 'reference too noisy', not 'head too weak': label the disagreements (--export-dir)")
    print("   and recalibrate τ/τ_s on human labels; treat these suggestions as provisional.)")

    # ---------------------------------------------------------------- sensitive
    print("\n## 6. Sensitive labels")
    sdis = [r for r in ok if (g(r, "sensitive", "local") or g(r, "sensitive", "remote")) and g(r, "local", "label") != g(r, "remote", "label")]
    print(f"rows where local or remote label is sensitive and they disagree: {len(sdis)}")
    sacc = [r for r in ok if g(r, "sensitive", "local") and g(r, "local", "decision") == "accept"]
    sbad = [r for r in sacc if g(r, "local", "label") != g(r, "remote", "label")]
    print("local ACCEPTED a sensitive label that remote disagrees with:", fmt_rate(len(sbad), len(sacc)), "(as share of sensitive accepts)")
    print("  by (local → remote):", dict(collections.Counter((g(r, "local", "label"), g(r, "remote", "label")) for r in sbad).most_common(10)))
    rs = [r for r in ok if g(r, "sensitive", "remote")]
    print(f"remote said sensitive: {len(rs)}; local accepted same label: {sum(1 for r in rs if g(r,'local','decision')=='accept' and g(r,'local','label')==g(r,'remote','label'))}")

    # ---------------------------------------------------------------- failures
    fail = [r for r in l3 if g(r, "remote", "outcome") not in ("ok",) and g(r, "local", "label")]
    print(f"\n## 7. Turns where remote did not answer (n={len(fail)}): what local would have done")
    print("  by remote outcome:", dict(collections.Counter(g(r, "remote", "outcome") for r in fail)))
    print("  local decision:", dict(collections.Counter(g(r, "local", "decision") for r in fail)))
    print("  user-visible result on those turns:", dict(collections.Counter(r.get("decided_by") for r in fail)))

    # ---------------------------------------------------------------- L2 audit
    print("\n## 8. Layer 2 audit")
    print(f"8a. L2-decided sample (kind=l2_sample, n={len(l2s)}): L2 primary vs local head")
    l2ok = [r for r in l2s if g(r, "local", "label")]
    k2 = sum(1 for r in l2ok if g(r, "local", "label") == g(r, "l2", "primary"))
    print("  local top-1 == L2 primary:", fmt_rate(k2, len(l2ok)))
    l2acc = [r for r in l2ok if g(r, "local", "decision") == "accept"]
    print("  … among local accepts:", fmt_rate(sum(1 for r in l2acc if g(r, "local", "label") == g(r, "l2", "primary")), len(l2acc)))
    print("  disagreements (L2 → local):", dict(collections.Counter((g(r, "l2", "primary"), g(r, "local", "label")) for r in l2ok if g(r, "local", "label") != g(r, "l2", "primary")).most_common(8)))
    print("8b. Escalated turns with a remote answer: does the L2 leader match remote, by mapped L2 top-1 score")
    esc = [r for r in ok if g(r, "l2", "topk")]
    buckets = [(0, .6), (.6, .65), (.65, .70), (.70, .74), (.74, .78), (.78, 1.01)]
    for lo_, hi_ in buckets:
        rows = [r for r in esc if lo_ <= g(r, "l2", "topk")[0]["mapped"] < hi_]
        kk = sum(1 for r in rows if g(r, "l2", "topk")[0]["label"] == g(r, "remote", "label"))
        print(f"  mapped top-1 in [{lo_:.2f},{hi_:.2f}): L2 leader == remote {fmt_rate(kk, len(rows))}")
    gaps = [(r, g(r, "l2", "topk")[0]["mapped"] - (g(r, "l2", "topk")[1]["mapped"] if len(g(r, "l2", "topk")) > 1 else 0)) for r in esc]
    for lo_, hi_ in ((0, .03), (.03, .05), (.05, .10), (.10, 1)):
        rows = [r for r, gp in gaps if lo_ <= gp < hi_]
        kk = sum(1 for r in rows if g(r, "l2", "topk")[0]["label"] == g(r, "remote", "label"))
        print(f"  mapped gap in [{lo_:.2f},{hi_:.2f}): L2 leader == remote {fmt_rate(kk, len(rows))}")

    # ---------------------------------------------------------------- gold (validation only)
    if a.gold:
        gold = {}
        with open(a.gold, encoding="utf-8") as fh:
            for line in fh:
                try:
                    x = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if x.get("text"):
                    gold[x["text"]] = x.get("intent")
        rows = [r for r in recs if g(r, "text", "full") in gold]
        print(f"\n## 9. Against supplied gold labels (validation only; n={len(rows)} rows with full text in gold)")
        ro = [r for r in rows if g(r, "remote", "outcome") == "ok"]
        print("  remote accuracy:", fmt_rate(sum(1 for r in ro if g(r, "remote", "label") == gold[g(r, "text", "full")]), len(ro)))
        lo2 = [r for r in rows if g(r, "local", "label")]
        print("  local top-1 accuracy:", fmt_rate(sum(1 for r in lo2 if g(r, "local", "label") == gold[g(r, "text", "full")]), len(lo2)))
        la = [r for r in lo2 if g(r, "local", "decision") == "accept"]
        print("  local accuracy among accepts:", fmt_rate(sum(1 for r in la if g(r, "local", "label") == gold[g(r, "text", "full")]), len(la)))
        fo = [r for r in rows if r.get("final", {}).get("label")]
        print("  final (user-visible) primary accuracy:", fmt_rate(sum(1 for r in fo if g(r, "final", "label") == gold[g(r, "text", "full")]), len(fo)))

    # ---------------------------------------------------------------- export
    if a.export_dir:
        os.makedirs(a.export_dir, exist_ok=True)
        rows = [r for r in recs if g(r, "text", "full")]
        def prio(r):
            dis = g(r, "local", "label") != g(r, "remote", "label")
            sens = bool(g(r, "sensitive", "local") or g(r, "sensitive", "remote"))
            return (0 if (dis and sens) else 1 if dis else 2 if sens else 3)
        rows.sort(key=prio)
        jp = os.path.join(a.export_dir, "shadow_label_queue.jsonl")
        cp = os.path.join(a.export_dir, "shadow_label_queue.csv")
        with open(jp, "w", encoding="utf-8") as fj, open(cp, "w", encoding="utf-8", newline="") as fc:
            cw = csv.writer(fc)
            hdr = ["turn_id", "priority", "text", "remote_label", "remote_outcome", "local_label", "local_p", "local_decision", "l2_primary", "final_label", "gold_label"]
            cw.writerow(hdr)
            for r in rows:
                row = [r.get("turn_id"), prio(r), g(r, "text", "full"), g(r, "remote", "label"), g(r, "remote", "outcome"),
                       g(r, "local", "label"), g(r, "local", "p"), g(r, "local", "decision"), g(r, "l2", "primary"), g(r, "final", "label"), ""]
                cw.writerow(row)
                fj.write(json.dumps(dict(zip(hdr, row)), ensure_ascii=False) + "\n")
        with_emb = sum(1 for r in recs if r.get("emb"))
        print(f"\n## 10. Export: {len(rows)} rows with text → {jp} / {cp} (priority 0 = sensitive disagreement, 1 = disagreement, 2 = sensitive, 3 = rest; fill gold_label)")
        print(f"   rows carrying an embedding (retrain without text, decode with decode_emb()): {with_emb}")
        if not rows:
            print("   (no rows with text: logs were written with MACLAW_INTENT_SHADOW_TEXT=hash|none)")


if __name__ == "__main__":
    main()
