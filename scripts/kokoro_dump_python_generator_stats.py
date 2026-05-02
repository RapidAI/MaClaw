from pathlib import Path
import json, torch
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
input_lengths = torch.full((1,), input_ids.shape[-1], dtype=torch.long)
text_mask = torch.arange(input_lengths.max()).unsqueeze(0).expand(1, -1).type_as(input_lengths)
text_mask = torch.gt(text_mask+1, input_lengths.unsqueeze(1))

def stat(t):
    a=t.detach().cpu().float().reshape(-1)
    return {"shape": list(t.shape), "min": float(a.min()), "max": float(a.max()), "mean": float(a.mean()), "rms": float(torch.sqrt((a*a).mean()))}

with torch.no_grad():
    bert_dur = m.bert(input_ids, attention_mask=(~text_mask).int())
    d_en = m.bert_encoder(bert_dur).transpose(-1, -2)
    s_dur = ref_s[:, 128:]
    d = m.predictor.text_encoder(d_en, s_dur, input_lengths, text_mask)
    xdur, _ = m.predictor.lstm(d)
    duration = torch.sigmoid(m.predictor.duration_proj(xdur)).sum(axis=-1)
    pred_dur = torch.round(duration).clamp(min=1).long().squeeze()
    indices = torch.repeat_interleave(torch.arange(input_ids.shape[1]), pred_dur)
    pred_aln_trg = torch.zeros((input_ids.shape[1], indices.shape[0]))
    pred_aln_trg[indices, torch.arange(indices.shape[0])] = 1
    pred_aln_trg = pred_aln_trg.unsqueeze(0)
    en = d.transpose(-1,-2) @ pred_aln_trg
    F0_pred, N_pred = m.predictor.F0Ntrain(en, s_dur)
    t_en = m.text_encoder(input_ids, input_lengths, text_mask)
    asr = t_en @ pred_aln_trg
    dec=m.decoder; s=ref_s[:, :128]
    F0 = dec.F0_conv(F0_pred.unsqueeze(1)); N=dec.N_conv(N_pred.unsqueeze(1))
    x = dec.encode(torch.cat([asr,F0,N], axis=1), s)
    asr_res = dec.asr_res(asr); res=True
    for block in dec.decode:
        if res: x=torch.cat([x,asr_res,F0,N], axis=1)
        x=block(x,s)
        if block.upsample_type != 'none': res=False
    g=dec.generator
    stats={"input": stat(x)}
    f0 = g.f0_upsamp(F0_pred[:, None]).transpose(1,2)
    har_source, _, _ = g.m_source(f0)
    har_source = har_source.transpose(1,2).squeeze(1)
    har_spec, har_phase = g.stft.transform(har_source)
    har=torch.cat([har_spec,har_phase], dim=1)
    for i in range(g.num_upsamples):
        x=torch.nn.functional.leaky_relu(x, negative_slope=0.1)
        stats[f"stage{i}_after_lrelu"] = stat(x)
        x_source=g.noise_convs[i](har)
        x_source=g.noise_res[i](x_source,s)
        x=g.ups[i](x)
        stats[f"stage{i}_after_ups"] = stat(x)
        if i==g.num_upsamples-1:
            x=g.reflection_pad(x)
            stats["stage1_after_reflect"] = stat(x)
        stats[f"stage{i}_source"] = stat(x_source)
        x=x+x_source
        stats[f"stage{i}_after_source_add"] = stat(x)
        xs=None
        for j in range(g.num_kernels):
            block_idx=i*g.num_kernels+j
            y=g.resblocks[block_idx](x,s)
            stats[f"resblock{block_idx}_out"] = stat(y)
            xs = y if xs is None else xs + y
        x=xs/g.num_kernels
        stats[f"stage{i}_out"] = stat(x)
    x=torch.nn.functional.leaky_relu(x)
    stats["pre_conv_post_lrelu"] = stat(x)
    post=g.conv_post(x)
    stats["conv_post"] = stat(post)
    spec=torch.exp(post[:,:11,:]); phase=torch.sin(post[:,11:,:])
    audio=g.stft.inverse(spec, phase)
    stats["audio"] = stat(audio)
(root/"quality_python_generator_stats.json").write_text(json.dumps(stats, indent=2), encoding="utf-8")
print(json.dumps(stats, indent=2))


