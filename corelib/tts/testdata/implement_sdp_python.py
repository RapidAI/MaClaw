#!/usr/bin/env python3
"""Implement the complete StochasticDurationPredictor in Python, matching ONNX exactly."""
import numpy as np, onnxruntime as ort, onnx, math
from onnx import helper, TensorProto, numpy_helper

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
model = onnx.load(model_path)

weights = {}
for init in model.graph.initializer:
    weights[init.name] = numpy_helper.to_array(init)

# Get encoder output
model.graph.output.append(helper.make_tensor_value_info("/enc_p/encoder/Mul_2_output_0", TensorProto.FLOAT, None))
model.graph.output.append(helper.make_tensor_value_info("/Squeeze_output_0", TensorProto.FLOAT, None))
modified = model_path + ".sdp_impl.onnx"
onnx.save(model, modified)
sess = ort.InferenceSession(modified)

pids = [1,21,35,65,0,12,38,64,0,18,39,67,0,10,37,65,0,18,37,67,0,22,30,67,0,12,30,66,0,4,44,67,0,20,39,67,0,15,41,67,2]
out = sess.run(None, {"input": np.array([pids], dtype=np.int64),
                      "input_lengths": np.array([len(pids)], dtype=np.int64),
                      "scales": np.array([0.0, 1.0, 0.0], dtype=np.float32)})

enc_out = np.array(out[1]).squeeze()  # [192, T]
onnx_path = np.array(out[2]).squeeze()
onnx_durs = onnx_path.sum(axis=0).astype(int)
T = enc_out.shape[1]
print(f"Encoder output: {enc_out.shape}")
print(f"ONNX durations: {list(onnx_durs)}")

import torch
import torch.nn.functional as F

# ============================================================
# SDP Implementation
# ============================================================

def dds_conv_forward(x, w, nLayers=3):
    """DDSConv: Dilated Depth-Separable Conv with learned LayerNorm."""
    ch = x.shape[1]
    for i in range(nLayers):
        dilation = 3 ** i
        residual = x.clone()
        
        # Norm1 (learned gamma/beta)
        gamma1 = torch.from_numpy(w[f"norms_1.{i}.gamma"].copy())
        beta1 = torch.from_numpy(w[f"norms_1.{i}.beta"].copy())
        x = F.layer_norm(x.transpose(1, 2), [ch], gamma1, beta1).transpose(1, 2)
        
        # Depthwise separable conv
        pad = (3 - 1) * dilation // 2
        sep_w = torch.from_numpy(w[f"convs_sep.{i}.weight"].copy())
        sep_b = torch.from_numpy(w[f"convs_sep.{i}.bias"].copy())
        x = F.conv1d(x, sep_w, sep_b, padding=pad, dilation=dilation, groups=ch)
        
        # Norm2 (learned gamma/beta)
        gamma2 = torch.from_numpy(w[f"norms_2.{i}.gamma"].copy())
        beta2 = torch.from_numpy(w[f"norms_2.{i}.beta"].copy())
        x = F.layer_norm(x.transpose(1, 2), [ch], gamma2, beta2).transpose(1, 2)
        
        # 1x1 conv
        pw_w = torch.from_numpy(w[f"convs_1x1.{i}.weight"].copy())
        pw_b = torch.from_numpy(w[f"convs_1x1.{i}.bias"].copy())
        x = F.conv1d(x, pw_w, pw_b)
        
        # Residual + GELU
        x = x + residual
        x = x * 0.5 * (1.0 + torch.erf(x / math.sqrt(2.0)))
    return x

def piecewise_rational_quadratic_transform(inputs, unnormalized_widths, unnormalized_heights,
                                            unnormalized_derivatives, inverse=False,
                                            tails=None, tail_bound=5.0, min_bin_width=1e-3,
                                            min_bin_height=1e-3, min_derivative=1e-3):
    """Piecewise rational quadratic spline transform."""
    # This is the core of the neural spline flow
    num_bins = unnormalized_widths.shape[-1]
    
    if tails == 'linear':
        # Pad for linear tails
        widths = F.softmax(unnormalized_widths, dim=-1)
        widths = min_bin_width + (1 - min_bin_width * num_bins) * widths
        cumwidths = torch.cumsum(widths, dim=-1)
        cumwidths = F.pad(cumwidths, (1, 0), value=0.0)
        cumwidths = (2 * tail_bound) * cumwidths - tail_bound
        cumwidths[..., 0] = -tail_bound
        cumwidths[..., -1] = tail_bound
        widths = cumwidths[..., 1:] - cumwidths[..., :-1]
        
        derivatives = min_derivative + F.softplus(unnormalized_derivatives)
        
        heights = F.softmax(unnormalized_heights, dim=-1)
        heights = min_bin_height + (1 - min_bin_height * num_bins) * heights
        cumheights = torch.cumsum(heights, dim=-1)
        cumheights = F.pad(cumheights, (1, 0), value=0.0)
        cumheights = (2 * tail_bound) * cumheights - tail_bound
        cumheights[..., 0] = -tail_bound
        cumheights[..., -1] = tail_bound
        heights = cumheights[..., 1:] - cumheights[..., :-1]
    else:
        widths = F.softmax(unnormalized_widths, dim=-1)
        widths = min_bin_width + (1 - min_bin_width * num_bins) * widths
        cumwidths = torch.cumsum(widths, dim=-1)
        cumwidths = F.pad(cumwidths, (1, 0), value=0.0)
        widths = cumwidths[..., 1:] - cumwidths[..., :-1]
        
        derivatives = min_derivative + F.softplus(unnormalized_derivatives)
        
        heights = F.softmax(unnormalized_heights, dim=-1)
        heights = min_bin_height + (1 - min_bin_height * num_bins) * heights
        cumheights = torch.cumsum(heights, dim=-1)
        cumheights = F.pad(cumheights, (1, 0), value=0.0)
        heights = cumheights[..., 1:] - cumheights[..., :-1]
    
    if inverse:
        bin_idx = torch.searchsorted(cumheights[..., 1:], inputs.unsqueeze(-1)).squeeze(-1)
    else:
        bin_idx = torch.searchsorted(cumwidths[..., 1:], inputs.unsqueeze(-1)).squeeze(-1)
    bin_idx = bin_idx.clamp(0, num_bins - 1)
    
    input_cumwidths = cumwidths.gather(-1, bin_idx.unsqueeze(-1)).squeeze(-1)
    input_bin_widths = widths.gather(-1, bin_idx.unsqueeze(-1)).squeeze(-1)
    input_cumheights = cumheights.gather(-1, bin_idx.unsqueeze(-1)).squeeze(-1)
    input_heights = heights.gather(-1, bin_idx.unsqueeze(-1)).squeeze(-1)
    input_delta = heights.gather(-1, bin_idx.unsqueeze(-1)).squeeze(-1) / widths.gather(-1, bin_idx.unsqueeze(-1)).squeeze(-1)
    input_derivatives = derivatives.gather(-1, bin_idx.unsqueeze(-1)).squeeze(-1)
    input_derivatives_plus_one = derivatives.gather(-1, (bin_idx + 1).clamp(max=num_bins).unsqueeze(-1)).squeeze(-1)
    
    if inverse:
        a = (input_heights) * (input_derivatives + input_derivatives_plus_one - 2 * input_delta) + (inputs - input_cumheights) * (input_derivatives_plus_one + input_derivatives - 2 * input_delta)
        b = (input_heights) * input_delta - (inputs - input_cumheights) * (input_derivatives_plus_one + input_derivatives - 2 * input_delta)
        c = -input_delta * (inputs - input_cumheights)
        
        discriminant = b.pow(2) - 4 * a * c
        discriminant = discriminant.clamp(min=0)
        root = (2 * c) / (-b - torch.sqrt(discriminant))
        outputs = root * input_bin_widths + input_cumwidths
        
        theta_one_minus_theta = root * (1 - root)
        denominator = input_delta + ((input_derivatives + input_derivatives_plus_one - 2 * input_delta) * theta_one_minus_theta)
        derivative_numerator = input_delta.pow(2) * (input_derivatives_plus_one * root.pow(2) + 2 * input_delta * theta_one_minus_theta + input_derivatives * (1 - root).pow(2))
        logabsdet = torch.log(derivative_numerator) - 2 * torch.log(denominator.abs())
        return outputs, -logabsdet
    else:
        theta = (inputs - input_cumwidths) / input_bin_widths
        theta_one_minus_theta = theta * (1 - theta)
        
        numerator = input_heights * (input_delta * theta.pow(2) + input_derivatives * theta_one_minus_theta)
        denominator = input_delta + ((input_derivatives + input_derivatives_plus_one - 2 * input_delta) * theta_one_minus_theta)
        outputs = input_cumheights + numerator / denominator
        
        derivative_numerator = input_delta.pow(2) * (input_derivatives_plus_one * theta.pow(2) + 2 * input_delta * theta_one_minus_theta + input_derivatives * (1 - theta).pow(2))
        logabsdet = torch.log(derivative_numerator) - 2 * torch.log(denominator.abs())
        return outputs, logabsdet


def sdp_forward(x_np, noise_scale_w=0.0):
    """Complete SDP forward pass (inference mode = reverse)."""
    x = torch.from_numpy(x_np.copy()).unsqueeze(0)  # [1, 192, T]
    T_len = x.shape[2]
    
    # dp.pre
    w_pre = torch.from_numpy(weights["dp.pre.weight"].copy())
    b_pre = torch.from_numpy(weights["dp.pre.bias"].copy())
    h = F.conv1d(x, w_pre, b_pre)
    
    # dp.convs (DDSConv)
    convs_w = {k.replace("dp.convs.", ""): weights[k] for k in weights if k.startswith("dp.convs.")}
    h = dds_conv_forward(h, convs_w)
    
    # dp.proj
    w_proj = torch.from_numpy(weights["dp.proj.weight"].copy())
    b_proj = torch.from_numpy(weights["dp.proj.bias"].copy())
    h = F.conv1d(h, w_proj, b_proj)
    
    # In inference mode (reverse):
    # 1. Sample noise: w = randn(2, T) * noise_scale_w
    # 2. Transform through flows in reverse
    # 3. logw = sum(w[0])... actually logw = w[0] after all transforms
    
    # Sample noise
    if noise_scale_w > 0:
        noise = torch.randn(1, 2, T_len) * noise_scale_w
    else:
        noise = torch.zeros(1, 2, T_len)
    
    # flows.0: ElementwiseAffine (log-scale)
    log_s = torch.from_numpy(weights["dp.flows.0.m"].copy())  # [2, 1]
    # In reverse: w = w * exp(-log_s) (undo the scaling)
    # Actually flows.0 is applied first in forward, last in reverse
    # Forward: w → w * exp(log_s) + 0
    # Reverse: w → (w - 0) * exp(-log_s) = w * exp(-log_s)
    
    w = noise
    
    # Reverse through flows: 7, 6(flip), 5, 4(flip), 3, 2(flip), 1(flip), 0
    # Even indices: ConvFlow (3, 5, 7)
    # Odd indices: Flip (1, 2, 4, 6) — just reverse channels
    # Index 0: ElementwiseAffine
    
    # In VITS SDP, the flow order is:
    # flows = [ElementwiseAffine, Flip, ConvFlow, Flip, ConvFlow, Flip, ConvFlow, Flip, ConvFlow]
    # Wait, looking at the ONNX: dp.flows.0 (ElementwiseAffine), dp.flows.3/5/7 (ConvFlow)
    # The indices 1,2,4,6 are Flip and Log layers
    
    # Actually in VITS code:
    # flows = [ElementwiseAffine(2), *[ConvFlow(2, filter, kernel, n_layers) for _ in range(n_flows)]
    # with Flip() between each
    # So: flows = [EA, Flip, CF, Flip, CF, Flip, CF]
    # Indices: 0=EA, 1=Flip, 2=CF(=dp.flows.3), 3=Flip, 4=CF(=dp.flows.5), 5=Flip, 6=CF(=dp.flows.7)
    
    # Reverse order: CF(7), Flip, CF(5), Flip, CF(3), Flip, EA(0)
    
    # ConvFlow reverse for flows.7
    for flow_idx in [7, 5, 3]:
        # Flip first (in reverse, flip comes before ConvFlow)
        w = torch.flip(w, [1])
        
        # ConvFlow reverse
        w0 = w[:, :1, :]  # [1, 1, T]
        w1 = w[:, 1:, :]  # [1, 1, T]
        
        # Condition: pre + h
        f_pre_w = torch.from_numpy(weights[f"dp.flows.{flow_idx}.pre.weight"].copy())
        f_pre_b = torch.from_numpy(weights[f"dp.flows.{flow_idx}.pre.bias"].copy())
        h_w = F.conv1d(w0, f_pre_w, f_pre_b) + h
        
        # DDSConv
        f_convs_w = {k.replace(f"dp.flows.{flow_idx}.convs.", ""): weights[k] 
                     for k in weights if k.startswith(f"dp.flows.{flow_idx}.convs.")}
        h_w = dds_conv_forward(h_w, f_convs_w)
        
        # Proj → spline parameters
        f_proj_w = torch.from_numpy(weights[f"dp.flows.{flow_idx}.proj.weight"].copy())
        f_proj_b = torch.from_numpy(weights[f"dp.flows.{flow_idx}.proj.bias"].copy())
        params = F.conv1d(h_w, f_proj_w, f_proj_b)  # [1, num_bins*3-1, T]
        
        # Parse spline parameters
        num_bins = 10  # (29 = 10*3 - 1)
        b_size = params.shape[0]
        # params: [B, 29, T] → reshape to [B, T, 29]
        params = params.transpose(1, 2)  # [B, T, 29]
        
        unnormalized_widths = params[..., :num_bins]
        unnormalized_heights = params[..., num_bins:2*num_bins]
        unnormalized_derivatives = params[..., 2*num_bins:]  # [B, T, 9]
        # Pad derivatives
        unnormalized_derivatives = F.pad(unnormalized_derivatives, (1, 1))
        
        # Apply spline transform (inverse)
        w1_flat = w1.squeeze(1)  # [B, T]
        w1_transformed, _ = piecewise_rational_quadratic_transform(
            w1_flat, unnormalized_widths, unnormalized_heights, unnormalized_derivatives,
            inverse=True, tails='linear', tail_bound=5.0)
        
        w1 = w1_transformed.unsqueeze(1)  # [B, 1, T]
        w = torch.cat([w0, w1], dim=1)
    
    # Final flip
    w = torch.flip(w, [1])
    
    # ElementwiseAffine reverse (flows.0)
    # Forward: w = w * exp(log_s), Reverse: w = w * exp(-log_s)
    w = w * torch.exp(-log_s.unsqueeze(-1))
    
    # logw = w[0] (first channel)
    logw = w[0, 0, :].detach().numpy()  # [T]
    
    # Duration = ceil(exp(logw) * length_scale)
    durations = np.ceil(np.exp(logw) * 1.0).astype(int)
    durations = np.maximum(durations, 1)
    
    return logw, durations

logw, py_durs = sdp_forward(enc_out, noise_scale_w=0.0)
print(f"\nPython SDP durations: {list(py_durs)}")
print(f"ONNX durations:      {list(onnx_durs)}")
print(f"Match: {np.array_equal(py_durs, onnx_durs)}")
if not np.array_equal(py_durs, onnx_durs):
    diff = np.abs(py_durs - onnx_durs)
    print(f"Max diff: {diff.max()}, positions: {np.where(diff > 0)[0]}")

import os; os.remove(modified)
