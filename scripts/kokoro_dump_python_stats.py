from pathlib import Path
import json, math
import torch
from kokoro import KModel

root = Path(r"D:\workprj\aicoder\tts_eval\kokoro_go_assets")
meta = json.loads((root / "quality_zh_short_meta.json").read_text(encoding="utf-8"))
snap = Path(r"C:\Users\ma139\.cache\huggingface\hub\models--hexgrad--Kokoro-82M\snapshots\f3ff3571791e39611d31c381e3a41a3af07b4987")
m = KModel(repo_id="hexgrad/Kokoro-82M", config=str(snap/"config.json"), model=str(snap/"kokoro-v1_0.pth")).eval()
pack = torch.load(snap/"voices"/f"{meta['voice']}.pt", map_location="cpu", weights_only=True)
ps = meta["phonemes"]
ids = [m.vocab.get(p) for p in ps if m.vocab.get(p) is not None]
input_ids = torch.LongTensor([[0, *ids, 0]])
ref_s = pack[len(ps)-1]
input_lengths = torch.full((input_ids.shape[0],), input_ids.shape[-1], dtype=torch.long)
text_mask = torch.arange(input_lengths.max()).unsqueeze(0).expand(input_lengths.shape[0], -1).type_as(input_lengths)
text_mask = torch.gt(text_mask+1, input_lengths.unsqueeze(1))
with torch.no_grad():
    bert_dur = m.bert(input_ids, attention_mask=(~text_mask).int())
    d_en = m.bert_encoder(bert_dur).transpose(-1, -2)
    s = ref_s[:, 128:]
    d = m.predictor.text_encoder(d_en, s, input_lengths, text_mask)
    x, _ = m.predictor.lstm(d)
    duration = m.predictor.duration_proj(x)
    duration = torch.sigmoid(duration).sum(axis=-1)
    pred_dur = torch.round(duration).clamp(min=1).long().squeeze()
    indices = torch.repeat_interleave(torch.arange(input_ids.shape[1]), pred_dur)
    pred_aln_trg = torch.zeros((input_ids.shape[1], indices.shape[0]))
    pred_aln_trg[indices, torch.arange(indices.shape[0])] = 1
    pred_aln_trg = pred_aln_trg.unsqueeze(0)
    en = d.transpose(-1, -2) @ pred_aln_trg
    F0_pred, N_pred = m.predictor.F0Ntrain(en, s)
    t_en = m.text_encoder(input_ids, input_lengths, text_mask)
    asr = t_en @ pred_aln_trg
    # decoder pre-generator equivalent
    dec = m.decoder
    F0 = dec.F0_conv(F0_pred.unsqueeze(1))
    N = dec.N_conv(N_pred.unsqueeze(1))
    dx = torch.cat([asr, F0, N], axis=1)
    dx = dec.encode(dx, ref_s[:, :128])
    asr_res = dec.asr_res(asr)
    res = True
    for block in dec.decode:
        if res:
            dx = torch.cat([dx, asr_res, F0, N], axis=1)
        dx = block(dx, ref_s[:, :128])
        if block.upsample_type != "none":
            res = False

def stat(t):
    a = t.detach().cpu().float().reshape(-1)
    return {"shape": list(t.shape), "min": float(a.min()), "max": float(a.max()), "mean": float(a.mean()), "rms": float(torch.sqrt((a*a).mean()))}

stats = {
    "input_ids": [0, *ids, 0],
    "pred_dur": pred_dur.tolist(),
    "bert_dur": stat(bert_dur),
    "d_en": stat(d_en),
    "duration_encoder_d": stat(d),
    "F0_pred": stat(F0_pred),
    "N_pred": stat(N_pred),
    "text_encoder_t_en": stat(t_en),
    "asr": stat(asr),
    "decoder_pre_generator": stat(dx),
}
(root / "quality_python_stats.json").write_text(json.dumps(stats, ensure_ascii=False, indent=2), encoding="utf-8")
print(json.dumps(stats, ensure_ascii=False, indent=2))
