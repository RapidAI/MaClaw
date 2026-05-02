# Reuse previous generator stats script logic, but save conv_post raw float32.
from pathlib import Path
import json, torch, numpy as np
from kokoro import KModel
root=Path(r"D:\workprj\aicoder\tts_eval\kokoro_go_assets")
meta=json.loads((root/"quality_zh_short_meta.json").read_text(encoding="utf-8"))
snap=Path(r"C:\Users\ma139\.cache\huggingface\hub\models--hexgrad--Kokoro-82M\snapshots\f3ff3571791e39611d31c381e3a41a3af07b4987")
m=KModel(repo_id="hexgrad/Kokoro-82M", config=str(snap/"config.json"), model=str(snap/"kokoro-v1_0.pth")).eval()
pack=torch.load(snap/"voices"/f"{meta['voice']}.pt", map_location="cpu", weights_only=True)
ps=meta['phonemes']; ids=[m.vocab.get(p) for p in ps if m.vocab.get(p) is not None]
input_ids=torch.LongTensor([[0,*ids,0]]); ref_s=pack[len(ps)-1]
input_lengths=torch.full((1,), input_ids.shape[-1], dtype=torch.long)
text_mask=torch.arange(input_lengths.max()).unsqueeze(0).expand(1,-1).type_as(input_lengths)
text_mask=torch.gt(text_mask+1,input_lengths.unsqueeze(1))
with torch.no_grad():
    bert_dur=m.bert(input_ids, attention_mask=(~text_mask).int())
    d_en=m.bert_encoder(bert_dur).transpose(-1,-2); s_dur=ref_s[:,128:]
    d=m.predictor.text_encoder(d_en,s_dur,input_lengths,text_mask)
    xdur,_=m.predictor.lstm(d); duration=torch.sigmoid(m.predictor.duration_proj(xdur)).sum(axis=-1)
    pred_dur=torch.round(duration).clamp(min=1).long().squeeze()
    indices=torch.repeat_interleave(torch.arange(input_ids.shape[1]), pred_dur)
    aln=torch.zeros((input_ids.shape[1], indices.shape[0])); aln[indices, torch.arange(indices.shape[0])]=1; aln=aln.unsqueeze(0)
    en=d.transpose(-1,-2)@aln; F0_pred,N_pred=m.predictor.F0Ntrain(en,s_dur)
    t_en=m.text_encoder(input_ids,input_lengths,text_mask); asr=t_en@aln
    dec=m.decoder; s=ref_s[:,:128]
    F0=dec.F0_conv(F0_pred.unsqueeze(1)); N=dec.N_conv(N_pred.unsqueeze(1))
    x=dec.encode(torch.cat([asr,F0,N],axis=1),s); asr_res=dec.asr_res(asr); res=True
    for block in dec.decode:
        if res: x=torch.cat([x,asr_res,F0,N],axis=1)
        x=block(x,s)
        if block.upsample_type!='none': res=False
    g=dec.generator
    f0=g.f0_upsamp(F0_pred[:,None]).transpose(1,2)
    har_source,_,_=g.m_source(f0); har_source=har_source.transpose(1,2).squeeze(1)
    har_spec,har_phase=g.stft.transform(har_source); har=torch.cat([har_spec,har_phase],dim=1)
    for i in range(g.num_upsamples):
        x=torch.nn.functional.leaky_relu(x,negative_slope=0.1)
        xs=g.noise_res[i](g.noise_convs[i](har),s)
        x=g.ups[i](x)
        if i==g.num_upsamples-1: x=g.reflection_pad(x)
        x=x+xs
        acc=None
        for j in range(g.num_kernels):
            y=g.resblocks[i*g.num_kernels+j](x,s)
            acc=y if acc is None else acc+y
        x=acc/g.num_kernels
    x=torch.nn.functional.leaky_relu(x)
    post=g.conv_post(x).detach().cpu().contiguous().numpy().astype('<f4')
post.tofile(root/"quality_python_conv_post_f32.bin")
(root/"quality_python_conv_post_shape.json").write_text(json.dumps({"shape": list(post.shape)}, indent=2), encoding="utf-8")
print(post.shape, post.min(), post.max())
