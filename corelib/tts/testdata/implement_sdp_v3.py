#!/usr/bin/env python3
"""Implement SDP using the actual VITS Python source code as reference."""
import numpy as np, sys, os
sys.path.insert(0, os.path.dirname(__file__))

# Use the actual VITS implementation from the piper-phonemize / vits source
# The key insight: use torch's actual VITS modules
import torch
import torch.nn as nn
import torch.nn.functional as F
import math
import onnxruntime as ort
import onnx
from onnx import helper, TensorProto, numpy_helper

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
onnx_model = onnx.load(model_path)

weights = {}
for init in onnx_model.graph.initializer:
    weights[init.name] = numpy_helper.to_array(init)

# Get encoder output from ONNX
onnx_model.graph.output.append(helper.make_tensor_value_info("/enc_p/encoder/Mul_2_output_0", TensorProto.FLOAT, None))
onnx_model.graph.output.append(helper.make_tensor_value_info("/Squeeze_output_0", TensorProto.FLOAT, None))
modified = model_path + ".v3.onnx"
onnx.save(onnx_model, modified)
sess = ort.InferenceSession(modified)

pids = [1,21,35,65,0,12,38,64,0,18,39,67,0,10,37,65,0,18,37,67,0,22,30,67,0,12,30,66,0,4,44,67,0,20,39,67,0,15,41,67,2]
out = sess.run(None, {"input": np.array([pids], dtype=np.int64),
                      "input_lengths": np.array([len(pids)], dtype=np.int64),
                      "scales": np.array([0.0, 1.0, 0.0], dtype=np.float32)})
enc_out = np.array(out[1]).squeeze()
onnx_path = np.array(out[2]).squeeze()
onnx_durs = onnx_path.sum(axis=0).astype(int)
T = enc_out.shape[1]
print(f"ONNX durs: {list(onnx_durs)}")

# ============================================================
# Use VITS source code directly (from piper's vits module)
# Reference: https://github.com/jaywalnut310/vits/blob/main/models.py
# ============================================================

from torch.nn.utils import weight_norm, remove_weight_norm

class LayerNorm(nn.Module):
    def __init__(self, channels, eps=1e-5):
        super().__init__()
        self.gamma = nn.Parameter(torch.ones(channels))
        self.beta = nn.Parameter(torch.zeros(channels))
        self.eps = eps
    def forward(self, x):
        # x: [B, C, T]
        return F.layer_norm(x.transpose(1, 2), [x.shape[1]], self.gamma, self.beta, self.eps).transpose(1, 2)

class DDSConv(nn.Module):
    def __init__(self, channels, kernel_size=3, n_layers=3, p_dropout=0.0):
        super().__init__()
        self.channels = channels
        self.n_layers = n_layers
        self.drop = nn.Dropout(p_dropout)
        self.convs_sep = nn.ModuleList()
        self.convs_1x1 = nn.ModuleList()
        self.norms_1 = nn.ModuleList()
        self.norms_2 = nn.ModuleList()
        for i in range(n_layers):
            dilation = kernel_size ** i
            padding = (kernel_size * dilation - dilation) // 2
            self.convs_sep.append(nn.Conv1d(channels, channels, kernel_size, groups=channels, dilation=dilation, padding=padding))
            self.convs_1x1.append(nn.Conv1d(channels, channels, 1))
            self.norms_1.append(LayerNorm(channels))
            self.norms_2.append(LayerNorm(channels))
    
    def forward(self, x, x_mask=None, g=None):
        if x_mask is not None:
            x = x * x_mask
        for i in range(self.n_layers):
            y = self.norms_1[i](x)
            y = self.convs_sep[i](y)
            y = self.norms_2[i](y)
            y = self.convs_1x1[i](y)
            y = self.drop(y)
            x = x + y
            x = x * torch.sigmoid(x) # SiLU/Swish... wait, VITS uses GELU?
        # Actually check: the ONNX graph shows Erf which is GELU
        # But the residual + activation pattern might be different
        return x

# Actually, let me just trace the ONNX graph more carefully.
# The DDSConv in ONNX has: norm1 → sep_conv → norm2 → 1x1_conv → add(residual) → GELU
# GELU = x * 0.5 * (1 + erf(x / sqrt(2)))

# Let me implement it manually matching ONNX exactly:
def dds_conv(x, prefix, nLayers=3):
    """DDSConv matching ONNX: sep_conv → norm1 → GELU → 1x1 → norm2 → GELU → +residual."""
    ch = x.shape[1]
    for i in range(nLayers):
        dilation = 3 ** i
        residual = x.clone()
        
        # Depthwise sep conv
        pad = (3 - 1) * dilation // 2
        w_sep = torch.from_numpy(weights[f"{prefix}.convs_sep.{i}.weight"].copy())
        b_sep = torch.from_numpy(weights[f"{prefix}.convs_sep.{i}.bias"].copy())
        x = F.conv1d(x, w_sep, b_sep, padding=pad, dilation=dilation, groups=ch)
        
        # Norm1 → GELU
        g1 = torch.from_numpy(weights[f"{prefix}.norms_1.{i}.gamma"].copy())
        b1 = torch.from_numpy(weights[f"{prefix}.norms_1.{i}.beta"].copy())
        x = F.layer_norm(x.transpose(1,2), [ch], g1, b1).transpose(1,2)
        x = x * 0.5 * (1.0 + torch.erf(x / math.sqrt(2.0)))
        
        # 1x1 conv
        w_pw = torch.from_numpy(weights[f"{prefix}.convs_1x1.{i}.weight"].copy())
        b_pw = torch.from_numpy(weights[f"{prefix}.convs_1x1.{i}.bias"].copy())
        x = F.conv1d(x, w_pw, b_pw)
        
        # Norm2 → GELU
        g2 = torch.from_numpy(weights[f"{prefix}.norms_2.{i}.gamma"].copy())
        b2 = torch.from_numpy(weights[f"{prefix}.norms_2.{i}.beta"].copy())
        x = F.layer_norm(x.transpose(1,2), [ch], g2, b2).transpose(1,2)
        x = x * 0.5 * (1.0 + torch.erf(x / math.sqrt(2.0)))
        
        # Residual
        x = x + residual
    return x

def rational_quadratic_spline_inverse(inputs, W, H, D, tail_bound=5.0):
    """Inverse rational quadratic spline transform.
    W: unnormalized widths [B, T, K]
    H: unnormalized heights [B, T, K]  
    D: unnormalized derivatives [B, T, K+1]
    """
    K = W.shape[-1]
    
    # Compute widths
    widths = torch.softmax(W, dim=-1)
    widths = 1e-3 + (1 - 1e-3 * K) * widths
    cumwidths = torch.cumsum(widths, dim=-1)
    cumwidths = F.pad(cumwidths, (1, 0), value=0.0)
    cumwidths = 2 * tail_bound * cumwidths - tail_bound
    cumwidths[..., 0] = -tail_bound
    cumwidths[..., -1] = tail_bound
    widths = cumwidths[..., 1:] - cumwidths[..., :-1]
    
    # Compute heights
    heights = torch.softmax(H, dim=-1)
    heights = 1e-3 + (1 - 1e-3 * K) * heights
    cumheights = torch.cumsum(heights, dim=-1)
    cumheights = F.pad(cumheights, (1, 0), value=0.0)
    cumheights = 2 * tail_bound * cumheights - tail_bound
    cumheights[..., 0] = -tail_bound
    cumheights[..., -1] = tail_bound
    heights = cumheights[..., 1:] - cumheights[..., :-1]
    
    # Compute derivatives
    derivatives = 1e-3 + F.softplus(D)
    
    # Find bin (inverse: search in cumheights)
    bin_idx = torch.searchsorted(cumheights[..., 1:].contiguous(), inputs.unsqueeze(-1)).squeeze(-1)
    bin_idx = bin_idx.clamp(0, K - 1)
    
    # Gather bin parameters
    input_cumwidths = cumwidths.gather(-1, bin_idx.unsqueeze(-1)).squeeze(-1)
    input_bin_widths = widths.gather(-1, bin_idx.unsqueeze(-1)).squeeze(-1)
    input_cumheights = cumheights.gather(-1, bin_idx.unsqueeze(-1)).squeeze(-1)
    input_heights = heights.gather(-1, bin_idx.unsqueeze(-1)).squeeze(-1)
    input_delta = input_heights / input_bin_widths
    input_derivatives = derivatives.gather(-1, bin_idx.unsqueeze(-1)).squeeze(-1)
    input_derivatives_p1 = derivatives.gather(-1, (bin_idx + 1).unsqueeze(-1)).squeeze(-1)
    
    # Inverse quadratic
    a = input_heights * (input_derivatives + input_derivatives_p1 - 2 * input_delta)
    a = a + (inputs - input_cumheights) * (input_derivatives_p1 + input_derivatives - 2 * input_delta)
    b = input_heights * input_delta - (inputs - input_cumheights) * (input_derivatives_p1 + input_derivatives - 2 * input_delta)
    c = -input_delta * (inputs - input_cumheights)
    
    discriminant = (b ** 2 - 4 * a * c).clamp(min=0)
    root = (2 * c) / (-b - torch.sqrt(discriminant))
    outputs = root * input_bin_widths + input_cumwidths
    return outputs

# ============================================================
# Full SDP inference (reverse mode)
# ============================================================
x = torch.from_numpy(enc_out.copy()).unsqueeze(0)  # [1, 192, T]

# dp.pre
w_pre = torch.from_numpy(weights["dp.pre.weight"].copy())
b_pre = torch.from_numpy(weights["dp.pre.bias"].copy())
h = F.conv1d(x, w_pre, b_pre)
print(f"Python dp.pre: shape={h.shape}, RMS={torch.sqrt(torch.mean(h**2)).item():.4f}")
print(f"ONNX dp.pre:   RMS=0.2474")

# dp.convs
h = dds_conv(h, "dp.convs")

# dp.proj
w_proj = torch.from_numpy(weights["dp.proj.weight"].copy())
b_proj = torch.from_numpy(weights["dp.proj.bias"].copy())
h = F.conv1d(h, w_proj, b_proj)
print(f"Python dp.proj: shape={h.shape}, RMS={torch.sqrt(torch.mean(h**2)).item():.4f}")
print(f"ONNX dp.proj:   RMS=0.4377")

print(f"SDP conditioning h: shape={h.shape}, RMS={torch.sqrt(torch.mean(h**2)).item():.4f}")

# Sample noise (deterministic: all zeros)
w = torch.zeros(1, 2, T)

# Reverse through flows
# VITS source: flows = list(reversed(self.flows)); flows = flows[:-2] + [flows[-1]]
# Original: [EA, Flip, CF, Flip, CF, Flip, CF, Flip]
# Reversed: [Flip, CF, Flip, CF, Flip, CF, Flip, EA]
# After remove: [Flip, CF, Flip, CF, Flip, CF, EA]  (removed second-to-last Flip)
for flow_idx in [7, 5, 3]:
    # Flip
    w = torch.flip(w, [1])
    
    if flow_idx <= 5:
        print(f"  Flow {flow_idx}: w shape after flip = {w.shape}")
    
    # ConvFlow (inverse)
    w0 = w[:, :1, :]
    w1 = w[:, 1:, :]
    
    # h_w = pre(w0) + h
    f_pre_w = torch.from_numpy(weights[f"dp.flows.{flow_idx}.pre.weight"].copy())
    f_pre_b = torch.from_numpy(weights[f"dp.flows.{flow_idx}.pre.bias"].copy())
    h_w = F.conv1d(w0, f_pre_w, f_pre_b) + h
    
    # DDSConv
    h_w = dds_conv(h_w, f"dp.flows.{flow_idx}.convs")
    
    # Proj → [1, 29, T]
    f_proj_w = torch.from_numpy(weights[f"dp.flows.{flow_idx}.proj.weight"].copy())
    f_proj_b = torch.from_numpy(weights[f"dp.flows.{flow_idx}.proj.bias"].copy())
    params = F.conv1d(h_w, f_proj_w, f_proj_b)
    
    if flow_idx == 7:
        print(f"  My f7 proj: {params.shape}, RMS={torch.sqrt(torch.mean(params**2)).item():.4f}")
        print(f"  ONNX f7 proj: shape=(1,29,41), RMS=15.4487")
    
    # Parse: [1, 29, T] → reshape to [1, 1, T, 29] (half_channels=1)
    b, c_proj, t = params.shape
    half_ch = 1  # in_channels=2, half=1
    params = params.reshape(b, half_ch, -1, t).permute(0, 1, 3, 2)  # [1, 1, T, 29]
    
    K = 10  # num_bins
    # CRITICAL: divide by sqrt(filter_channels) as in VITS ConvFlow source
    import math as _math
    scale = _math.sqrt(192)  # filter_channels = 192
    W_param = params[..., :K] / scale
    H_param = params[..., K:2*K] / scale
    D_param = params[..., 2*K:]  # [1, 1, T, 9] — NOT scaled
    
    # Inverse spline on w1 using VITS source transforms
    import sys
    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)) if '__file__' in dir() else 'corelib/tts/testdata')
    sys.path.insert(0, 'corelib/tts/testdata')
    from transforms_vits import piecewise_rational_quadratic_transform as prqt
    x1_out, _ = prqt(w1, W_param, H_param, D_param, inverse=True, tails='linear', tail_bound=5.0)
    
    if flow_idx == 7:
        print(f"  w shape before split: {w.shape}")
        print(f"  w0: {w0.shape}, w1: {w1.shape}")
        print(f"  w1 before spline: [{w1[0,0,0].item():.4f}, {w1[0,0,1].item():.4f}]")
        print(f"  w1 after spline:  [{x1_out[0,0,0].item():.4f}, {x1_out[0,0,1].item():.4f}]")
        print(f"  x1_out shape: {x1_out.shape}")
        print(f"  W_param[0,0,0,:3]: {W_param[0,0,0,:3].detach().numpy()}")
        print(f"  w after cat: {torch.cat([w0, x1_out], dim=1).shape}")
    
    w = torch.cat([w0, x1_out], dim=1)

# ElementwiseAffine (flows.0) inverse: x = (x - m) * exp(-logs)
m_param = torch.from_numpy(weights["dp.flows.0.m"].copy())  # [2, 1]
# exp(-logs) is precomputed in ONNX as a constant
exp_neg_logs = torch.tensor([[1.0282332], [1.2391727]])  # from ONNX /dp/flows.0/Exp_output_0
w = (w - m_param.unsqueeze(0)) * exp_neg_logs.unsqueeze(0)

# logw = w[0, 0, :]
# After odd number of flips, channels are swapped. logw is in channel 1, not 0.
logw = w[0, 1, :].detach().numpy()
print(f"logw first 10: {logw[:10]}")
print(f"logw range: [{logw.min():.3f}, {logw.max():.3f}]")
print(f"w shape: {w.shape}, w[0,:,0]: {w[0,:,0].detach().numpy()}")
py_durs = np.ceil(np.exp(logw)).astype(int)
py_durs = np.maximum(py_durs, 1)

print(f"\nPython SDP: {list(py_durs)}")
print(f"ONNX:       {list(onnx_durs)}")
print(f"Match: {np.array_equal(py_durs, onnx_durs)}")
if not np.array_equal(py_durs, onnx_durs):
    diff = np.abs(py_durs.astype(int) - onnx_durs.astype(int))
    print(f"Max diff: {diff.max()}, mean diff: {diff.mean():.1f}")
    for i in range(len(py_durs)):
        if py_durs[i] != onnx_durs[i]:
            print(f"  [{i}] py={py_durs[i]} onnx={onnx_durs[i]}")

os.remove(modified)
