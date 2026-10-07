#!/usr/bin/env python3
"""One-command shadow-log -> local intent head recalibration.

  # 1) export rows to label (no --labels yet)
  python3 scripts/intent_shadow_recalibrate.py ~/.maclaw/logs/intent-shadow/ \
      --head head_mlp_qwen3go_GK.json --export-dir ~/shadow_review
  #    -> ~/shadow_review/label_queue.{csv,jsonl}: fill the `label` column
  # 2) refit T / tau / tau_s on the HUMAN labels and write a new head
  python3 scripts/intent_shadow_recalibrate.py ~/.maclaw/logs/intent-shadow/ \
      --head head_mlp_qwen3go_GK.json --labels ~/shadow_review/label_queue.csv \
      --out head_mlp_qwen3go_xf.json [--retrain]

Only human labels are used as truth. The remote L3 verdict in the log is a
noisy reference: it is exported next to each row to help the labeller but is
never used to fit anything.

Fitting rule (same as maclaw_reranker/scripts/train_cls_heads.py, which fit
the shipped heads offline):
  T     : minimise NLL of the human label (golden-section on log T)
  tau   : smallest t in linspace(0.30, 0.99, 70) with >= --min-accept accepted
          rows and accepted accuracy >= --target (default 0.95)
  tau_s : start at max(tau, 0.95); raise by 0.01 until accepted sensitive
          predictions reach --target-sensitive precision (default 0.97), cap 0.99
Probabilities come from a full head forward pass when the row carries its L2
vector (MACLAW_INTENT_SHADOW_EMBED=1); otherwise from the logged top-k
re-tempered with the tail spread uniformly (approximation, reported).

--retrain (needs vectors): fine-tunes the output layer on the labelled
vectors with an L2 pull toward the shipped weights (no original training data
is needed), then refits T/tau/tau_s on a held-out split of the labels.

Stdlib only; uses numpy when importable (much faster forward pass). Writes the
new head JSON with "calibration": {source: "human_labels", ...provenance}.
guiapp keeps a human-calibrated head's tau_s (otherwise it applies 0.95).
"""
import argparse
import base64
import csv
import datetime
import glob
import hashlib
import json
import math
import os
import random
import struct
import sys

try:
    import numpy as np
except Exception:  # pragma: no cover
    np = None

UNKNOWN = "unknown"


# ---------------------------------------------------------------- loading
def load_logs(paths):
    files = []
    for p in paths:
        if os.path.isdir(p):
            files += sorted(glob.glob(os.path.join(p, "*.jsonl")) + glob.glob(os.path.join(p, "*.jsonl.*")))
        else:
            files.append(p)
    rows, bad = [], 0
    for f in files:
        with open(f, encoding="utf-8") as fh:
            for ln in fh:
                ln = ln.strip()
                if not ln:
                    continue
                try:
                    r = json.loads(ln)
                except ValueError:
                    bad += 1
                    continue
                if r.get("v") == 1:
                    r["_file"] = os.path.basename(f)
                    rows.append(r)
    return rows, files, bad


def g(r, *path, default=None):
    for k in path:
        if not isinstance(r, dict) or k not in r:
            return default
        r = r[k]
    return r


def decode_emb(e):
    if not e or e.get("dtype") not in ("f16", "f16le", "float16") or not e.get("b64"):
        return None
    b = base64.b64decode(e["b64"])
    return list(struct.unpack("<%de" % (len(b) // 2), b))


def row_key(r):
    """Stable key: turn_id + text hash (turn ids restart per process)."""
    return "%s|%s" % (r.get("turn_id", ""), g(r, "text", "hash", default="") or "")


def load_head(path):
    with open(path, "rb") as fh:
        raw = fh.read()
    h = json.loads(raw)
    h["_sha12"] = hashlib.sha256(raw).hexdigest()[:12]
    h["_id"] = os.path.basename(path) + "@" + h["_sha12"]
    return h


# ---------------------------------------------------------------- head math
class Head:
    def __init__(self, h):
        self.h = h
        self.labels = h["labels"]
        self.sens = set(h.get("sensitive") or [])
        self.l2 = bool(h.get("l2norm"))
        self.T = float(h.get("temperature") or 1.0)
        if np is not None:
            self.w1 = np.array(h["w1"], dtype=np.float64) if h.get("w1") else None
            self.b1 = np.array(h["b1"], dtype=np.float64) if h.get("b1") else None
            self.w2 = np.array(h["w2"], dtype=np.float64)
            self.b2 = np.array(h["b2"], dtype=np.float64)
            self.mean = np.array(h["mean"]) if h.get("mean") else None
            self.scale = np.array(h["scale"]) if h.get("scale") else None
        self.dim = len(h["w1"][0]) if h.get("w1") else len(h["w2"][0])

    def hidden(self, X):
        """X: list of vectors -> features feeding w2 (numpy or lists)."""
        if np is not None:
            X = np.array(X, dtype=np.float64)
            if self.l2:
                X = X / np.maximum(np.linalg.norm(X, axis=1, keepdims=True), 1e-12)
            if self.mean is not None:
                s = np.where(self.scale == 0, 1, self.scale)
                X = (X - self.mean) / s
            if self.w1 is not None:
                X = np.maximum(0, X @ self.w1.T + self.b1)
            return X
        out = []
        for v in X:
            if self.l2:
                n = math.sqrt(sum(x * x for x in v)) or 1e-12
                v = [x / n for x in v]
            if self.h.get("mean"):
                v = [(x - m) / (s or 1) for x, m, s in zip(v, self.h["mean"], self.h["scale"])]
            if self.h.get("w1"):
                v = [max(0.0, sum(a * b for a, b in zip(row, v)) + bb) for row, bb in zip(self.h["w1"], self.h["b1"])]
            out.append(v)
        return out

    def logits(self, H, w2=None, b2=None):
        if np is not None:
            w2 = self.w2 if w2 is None else w2
            b2 = self.b2 if b2 is None else b2
            return np.asarray(H) @ w2.T + b2
        return [[sum(a * b for a, b in zip(row, v)) + bb for row, bb in zip(self.h["w2"], self.h["b2"])] for v in H]


def softmax(z, T):
    m = max(z)
    e = [math.exp((x - m) / T) for x in z]
    s = sum(e)
    return [x / s for x in e]


def topk_probs_retempered(topk, n_labels, a):
    """Approximate p'_i ∝ p_i^a from logged top-k; tail spread uniformly."""
    ps = {t["label"]: max(float(t["p"]), 1e-300) for t in topk}
    k = len(ps)
    rest = max(1.0 - sum(ps.values()), 0.0)
    m = max(n_labels - k, 0)
    tail_each = (rest / m) if m and rest > 0 else 0.0
    num = {l: p ** a for l, p in ps.items()}
    den = sum(num.values()) + (m * tail_each ** a if tail_each > 0 else 0.0)
    return {l: v / den for l, v in num.items()}, (m * tail_each ** a / den if tail_each > 0 else 0.0) / max(m, 1)


# ---------------------------------------------------------------- export
def export_queue(rows, out_dir, low_margin, sample, export_all, seed=0):
    """Disagreement / low-confidence / sensitive rows first, plus a uniform
    random sample of the remaining (confident, agreeing) rows: calibration
    needs a representative slice, not only the hard cases."""
    os.makedirs(out_dir, exist_ok=True)
    rnd = random.Random(seed)
    q = []
    for r in rows:
        loc = r.get("local") or {}
        if not loc.get("label"):
            continue
        rem = r.get("remote") or {}
        dis = rem.get("outcome") == "ok" and rem.get("label") and rem.get("label") != loc.get("label")
        low = loc.get("decision") != "accept" or float(loc.get("p") or 0) < float(loc.get("threshold") or 0) + low_margin
        sens = bool(g(r, "sensitive", "local")) or bool(g(r, "sensitive", "remote"))
        rs = False
        if not (dis or low or sens):
            if not (export_all or rnd.random() < sample):
                continue
            rs = True
        prio = 0 if (dis and sens) else 1 if dis else 2 if low else 3 if sens else 4
        q.append((prio, -float(loc.get("p") or 0), r, dis, low, sens))
    q.sort(key=lambda t: (t[0], t[1]))
    cols = ["key", "priority", "reason", "text", "local_label", "local_p", "local_decision", "local_top3",
            "remote_label", "remote_outcome", "final_label", "has_vector", "label"]
    recs = []
    for prio, _, r, dis, low, sens in q:
        rs = prio == 4
        loc = r.get("local") or {}
        rem = r.get("remote") or {}
        recs.append({
            "key": row_key(r), "priority": prio,
            "reason": ",".join(x for x, f in (("disagree", dis), ("low_conf", low), ("sensitive", sens), ("random_sample", rs)) if f),
            "text": g(r, "text", "full", default="") or "",
            "local_label": loc.get("label"), "local_p": round(float(loc.get("p") or 0), 4),
            "local_decision": loc.get("decision"),
            "local_top3": " ".join("%s:%.2f" % (t["label"], t["p"]) for t in (loc.get("topk") or [])[:3]),
            "remote_label": rem.get("label") or "", "remote_outcome": rem.get("outcome") or "",
            "final_label": g(r, "final", "label", default=""), "has_vector": bool(r.get("emb")), "label": "",
        })
    with open(os.path.join(out_dir, "label_queue.jsonl"), "w", encoding="utf-8") as fh:
        for x in recs:
            fh.write(json.dumps(x, ensure_ascii=False) + "\n")
    with open(os.path.join(out_dir, "label_queue.csv"), "w", encoding="utf-8", newline="") as fh:
        w = csv.DictWriter(fh, fieldnames=cols)
        w.writeheader()
        w.writerows(recs)
    no_text = sum(1 for x in recs if not x["text"])
    print("export: %d rows -> %s/label_queue.{csv,jsonl} (disagree+sensitive %d, disagree %d, low-conf %d, sensitive-only %d, random sample of confident rows %d; %d without text: collect with MACLAW_INTENT_SHADOW_TEXT=full to label them)"
          % (len(recs), out_dir, sum(1 for x in q if x[0] == 0), sum(1 for x in q if x[0] == 1),
             sum(1 for x in q if x[0] == 2), sum(1 for x in q if x[0] == 3), sum(1 for x in q if x[0] == 4), no_text))
    return recs


def load_labels(path):
    out = {}
    if path.endswith(".csv"):
        with open(path, encoding="utf-8", newline="") as fh:
            it = list(csv.DictReader(fh))
    else:
        with open(path, encoding="utf-8") as fh:
            it = [json.loads(l) for l in fh if l.strip()]
    for x in it:
        lab = (x.get("label") or "").strip()
        if lab and x.get("key"):
            out[x["key"]] = lab
    return out


# ---------------------------------------------------------------- fitting
def fit_T(nll_of, lo=0.05, hi=20.0, iters=80):
    a, b = math.log(lo), math.log(hi)
    gr = (math.sqrt(5) - 1) / 2
    c, d = b - gr * (b - a), a + gr * (b - a)
    fc, fd = nll_of(math.exp(c)), nll_of(math.exp(d))
    for _ in range(iters):
        if fc < fd:
            b, d, fd = d, c, fc
            c = b - gr * (b - a)
            fc = nll_of(math.exp(c))
        else:
            a, c, fc = c, d, fd
            d = a + gr * (b - a)
            fd = nll_of(math.exp(d))
    return math.exp((a + b) / 2)


def fit_thresholds(conf, pred, gold, sens, target, target_s, min_accept):
    corr = [p == y for p, y in zip(pred, gold)]
    tau = 0.99
    for i in range(70):
        t = 0.30 + i * (0.99 - 0.30) / 69
        k = [c for c, cf in zip(corr, conf) if cf >= t]
        if len(k) >= min_accept and sum(k) / len(k) >= target:
            tau = t
            break
    tau_s = max(tau, 0.95)
    t = tau_s
    while t <= 0.991:
        k = [c for c, cf, p in zip(corr, conf, pred) if cf >= t and p in sens]
        tau_s = t
        if not k or sum(k) / len(k) >= target_s:
            break
        t = round(t + 0.01, 10)
    return tau, min(tau_s, 0.99)


def evaluate(conf, pred, gold, sens, tau, tau_s):
    acc = [cf >= (tau_s if p in sens else tau) and p != UNKNOWN for cf, p in zip(conf, pred)]
    n = len(gold)
    na = sum(acc)
    ok = sum(1 for a, p, y in zip(acc, pred, gold) if a and p == y)
    sfe = sum(1 for a, p, y in zip(acc, pred, gold) if a and p in sens and p != y)
    top1 = sum(1 for p, y in zip(pred, gold) if p == y)
    return {"n": n, "top1_acc": round(top1 / n, 4) if n else None, "coverage": round(na / n, 4) if n else None,
            "accepted_acc": round(ok / na, 4) if na else None, "sensitive_false_accepts": sfe}


def nll(P, y_idx):
    return -sum(math.log(max(p[i], 1e-300)) for p, i in zip(P, y_idx)) / max(len(y_idx), 1)


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("logs", nargs="+", help="shadow log dir(s) or *.jsonl files")
    ap.add_argument("--head", required=True, help="head JSON the logs were recorded with (and the one to update)")
    ap.add_argument("--labels", help="labelled label_queue.{csv,jsonl} (column/field `label` = HUMAN label)")
    ap.add_argument("--out", help="new head JSON path (required with --labels)")
    ap.add_argument("--export-dir", help="write label_queue.{csv,jsonl} here")
    ap.add_argument("--low-margin", type=float, default=0.10, help="export rows with local p < threshold + margin")
    ap.add_argument("--sample", type=float, default=0.25, help="also export this fraction of confident agreeing rows (unbiased calibration slice)")
    ap.add_argument("--export-all", action="store_true", help="export every row with a local verdict")
    ap.add_argument("--target", type=float, default=0.95)
    ap.add_argument("--target-sensitive", type=float, default=0.97)
    ap.add_argument("--min-accept", type=int, default=5)
    ap.add_argument("--min-labels", type=int, default=200, help="refuse to write a head with fewer human labels (use --force)")
    ap.add_argument("--force", action="store_true")
    ap.add_argument("--retrain", action="store_true", help="fine-tune output layer on labelled vectors (needs EMBED=1 logs)")
    ap.add_argument("--retrain-l2", type=float, default=1.0, help="L2 pull toward shipped weights during --retrain")
    ap.add_argument("--holdout", type=float, default=0.3, help="--retrain: fraction of labels held out for T/tau fitting")
    ap.add_argument("--seed", type=int, default=0)
    ap.add_argument("--report", help="also write a JSON summary here")
    a = ap.parse_args()

    rows, files, bad = load_logs(a.logs)
    hj = load_head(a.head)
    head = Head(hj)
    print("logs: %d records from %d file(s) (%d unparsable); head %s (T=%.3f tau=%.3f tau_s=%.3f, %d labels, dim %d)"
          % (len(rows), len(files), bad, hj["_id"], head.T, hj.get("tau", 0), hj.get("tau_s", 0), len(head.labels), head.dim))
    same_head = [r for r in rows if (r.get("head_id") or "").endswith("@" + hj["_sha12"])]
    if len(same_head) != len(rows):
        print("note: %d/%d records were logged with this head; other rows use their stored vector (if any) and are skipped otherwise" % (len(same_head), len(rows)))
    if a.export_dir:
        export_queue(rows, a.export_dir, a.low_margin, a.sample, a.export_all, a.seed)
    if not a.labels:
        if not a.export_dir:
            print("nothing to do: pass --export-dir to export rows for labelling, or --labels/--out to refit")
        return 0
    if not a.out:
        ap.error("--out is required with --labels")

    labels = load_labels(a.labels)
    lid = {l: i for i, l in enumerate(head.labels)}
    data, skipped_unknown_label, no_signal = [], 0, 0
    for r in rows:
        y = labels.get(row_key(r))
        if not y:
            continue
        if y not in lid:
            skipped_unknown_label += 1
            continue
        vec = decode_emb(r.get("emb"))
        if vec is not None and len(vec) != head.dim:
            vec = None
        own = (r.get("head_id") or "").endswith("@" + hj["_sha12"])
        if vec is None and not (own and g(r, "local", "topk")):
            no_signal += 1
            continue
        data.append({"y": y, "vec": vec, "topk": g(r, "local", "topk"), "own": own, "key": row_key(r)})
    n_vec = sum(1 for d in data if d["vec"] is not None)
    print("labels: %d human labels in file, %d matched usable rows (%d with vectors), %d with a label not in the head, %d without vector/top-k"
          % (len(labels), len(data), n_vec, skipped_unknown_label, no_signal))
    if len(data) < a.min_labels and not a.force:
        print("refusing: only %d usable human labels (< --min-labels %d); collect more or pass --force" % (len(data), a.min_labels))
        return 2
    exact = n_vec == len(data)
    sens = head.sens
    w2 = b2 = None
    retrain_info = None
    fit_rows = data
    if a.retrain:
        if np is None or not exact:
            print("--retrain needs numpy and a stored vector on every labelled row (MACLAW_INTENT_SHADOW_EMBED=1)")
            return 2
        rnd = random.Random(a.seed)
        idx = list(range(len(data)))
        rnd.shuffle(idx)
        n_hold = max(int(len(idx) * a.holdout), 1)
        hold, train = [data[i] for i in idx[:n_hold]], [data[i] for i in idx[n_hold:]]
        Htr = head.hidden([d["vec"] for d in train])
        ytr = np.array([lid[d["y"]] for d in train])
        w2, b2 = head.w2.copy(), head.b2.copy()
        w0, b0 = head.w2.copy(), head.b2.copy()
        lr = 0.05
        for _ in range(300):
            Z = Htr @ w2.T + b2
            Z -= Z.max(1, keepdims=True)
            P = np.exp(Z)
            P /= P.sum(1, keepdims=True)
            P[np.arange(len(ytr)), ytr] -= 1
            P /= len(ytr)
            w2 -= lr * (P.T @ Htr + a.retrain_l2 * (w2 - w0) / len(ytr))
            b2 -= lr * (P.sum(0) + a.retrain_l2 * (b2 - b0) / len(ytr))
        retrain_info = {"layer": "output", "train_rows": len(train), "holdout_rows": len(hold), "l2_to_shipped": a.retrain_l2, "steps": 300, "lr": lr}
        fit_rows = hold
        print("retrain: output layer fine-tuned on %d labelled vectors; T/tau/tau_s fitted on %d held-out labels" % (len(train), len(hold)))

    y_idx = [lid[d["y"]] for d in fit_rows]
    if exact:
        Z = head.logits(head.hidden([d["vec"] for d in fit_rows]), w2, b2)
        Z = Z.tolist() if np is not None else Z

        def probs(T):
            return [softmax(z, T) for z in Z]
    else:
        def probs(T):
            out = []
            for d in fit_rows:
                pk, tail = topk_probs_retempered(d["topk"], len(head.labels), head.T / T)
                out.append([pk.get(l, tail) for l in head.labels])
            return out
    T_old = head.T
    if exact and w2 is None:
        # Parity: Python forward at the shipped T vs the top-1 p the Go head logged.
        P0 = probs(T_old)
        dev = [abs(max(p) - float(d["topk"][0]["p"])) for p, d in zip(P0, fit_rows) if d["own"] and d["topk"]]
        if dev:
            print("parity: python forward vs logged Go top-1 p over %d rows: max |dp| = %.2e (float16 vectors)" % (len(dev), max(dev)))
    if w2 is not None:
        Zb = head.logits(head.hidden([d["vec"] for d in fit_rows]))
        Zb = Zb.tolist()
        Pb = [softmax(z, T_old) for z in Zb]
        cb = [max(p) for p in Pb]
        pb = [head.labels[max(range(len(p)), key=p.__getitem__)] for p in Pb]
        print("  shipped head (original weights, T, tau, tau_s) on the held-out labels: %s" % json.dumps(
            evaluate(cb, pb, [d["y"] for d in fit_rows], sens, hj.get("tau", 0), max(hj.get("tau_s", 0), hj.get("tau", 0)))))
    if not exact:
        print("note: %d/%d rows have no vector; their probabilities are re-tempered from the logged top-%d (approximation)"
              % (len(fit_rows) - sum(1 for d in fit_rows if d["vec"] is not None), len(fit_rows), len(fit_rows[0]["topk"]) if fit_rows else 0))
    T_new = fit_T(lambda T: nll(probs(T), y_idx))
    res = {}
    for name, T in (("old", T_old), ("new", T_new)):
        P = probs(T)
        conf = [max(p) for p in P]
        pred = [head.labels[max(range(len(p)), key=p.__getitem__)] for p in P]
        gold = [d["y"] for d in fit_rows]
        tau, tau_s = fit_thresholds(conf, pred, gold, sens, a.target, a.target_sensitive, a.min_accept)
        res[name] = {"T": T, "nll": nll(P, y_idx), "fit_tau": tau, "fit_tau_s": tau_s,
                     "eval_shipped_thresholds": evaluate(conf, pred, gold, sens, hj.get("tau", 0), max(hj.get("tau_s", 0), hj.get("tau", 0))),
                     "eval_fitted_thresholds": evaluate(conf, pred, gold, sens, tau, tau_s)}
    new = res["new"]
    print("fit (human labels, n=%d): T %.3f -> %.3f (NLL %.4f -> %.4f); tau %.3f -> %.3f; tau_s %.3f -> %.3f"
          % (len(fit_rows), T_old, new["T"], res["old"]["nll"], new["nll"], hj.get("tau", 0), new["fit_tau"], hj.get("tau_s", 0), new["fit_tau_s"]))
    print("  %s T/tau/tau_s on these labels: %s" % ("retrained weights + shipped" if w2 is not None else "shipped", json.dumps(res["old"]["eval_shipped_thresholds"])))
    print("  refit   T/tau/tau_s on these labels: %s (in-sample)" % json.dumps(new["eval_fitted_thresholds"]))

    out = {k: v for k, v in hj.items() if not k.startswith("_")}
    out["temperature"] = new["T"]
    out["tau"] = new["fit_tau"]
    out["tau_s"] = new["fit_tau_s"]
    if w2 is not None:
        out["w2"] = w2.tolist()
        out["b2"] = b2.tolist()
    if not out.get("embed_model"):
        out["embed_model"] = "qwen3" if head.dim == 1024 else "gemma" if head.dim == 768 else ""
    out["calibration"] = {
        "source": "human_labels",
        "created": datetime.datetime.now().astimezone().isoformat(timespec="seconds"),
        "base_head": hj["_id"],
        "logs": [os.path.abspath(f) for f in files],
        "labels_file": os.path.abspath(a.labels),
        "n_labels": len(data), "n_fit": len(fit_rows), "probabilities": "exact_forward" if exact else "approx_topk_retempered",
        "rule": "T=argmin NLL; tau=min t in linspace(.30,.99,70) with acc>=%.2f (>=%d accepted); tau_s from max(tau,.95) +0.01 until sensitive precision>=%.2f (cap .99)" % (a.target, a.min_accept, a.target_sensitive),
        "previous": {"temperature": T_old, "tau": hj.get("tau"), "tau_s": hj.get("tau_s")},
        "retrain": retrain_info,
        "eval_in_sample": new["eval_fitted_thresholds"],
        "script": "scripts/intent_shadow_recalibrate.py",
    }
    out["note"] = (hj.get("note") or "") + " | recalibrated on human-labelled shadow turns"
    with open(a.out, "w") as fh:
        json.dump(out, fh)
    print("wrote %s" % a.out)
    if a.report:
        with open(a.report, "w") as fh:
            json.dump({"fit": res, "calibration": out["calibration"]}, fh, indent=1)
    return 0


if __name__ == "__main__":
    sys.exit(main())
