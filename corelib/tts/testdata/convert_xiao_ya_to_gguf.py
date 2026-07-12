#!/usr/bin/env python3
"""Converxiao_ya Piper VITS ONNX modelo GGUF formafor pure Go inference."""
imporos
imporsys
imporstruct
impornumpy as np

try:
imporonnx
from onnx impornumpy_helper
excepImportError:
os.system(f"{sys.executable} -m pip install onnx -q")
imporonnx
from onnx impornumpy_helper

# GGUF constants
GGUF_MAGIC = 0x46475547# "GUFT" in lile-endian
GGUF_VERSION = 3
GGUF_TYPE_F32 = 0

def write_gguf(path,ensors, metadata=None):
"""Writeensorso GGUF format."""
if metadata is None:
metadata = {}

n_tensors = len(tensors)
n_kv = len(metadata)

with open(path, 'wb') as f:
# Header
f.write(struct.pack('<I', GGUF_MAGIC))
f.write(struct.pack('<I', GGUF_VERSION))
f.write(struct.pack('<Q', n_tensors))
f.write(struct.pack('<Q', n_kv))

# Metadata KV pairs
for key, value in metadata.items():
# Write key string
key_bytes = key.encode('utf-8')
f.write(struct.pack('<Q', len(key_bytes)))
f.write(key_bytes)

if isinstance(value, str):
# Type 8 = string
f.write(struct.pack('<I', 8))
val_bytes = value.encode('utf-8')
f.write(struct.pack('<Q', len(val_bytes)))
f.write(val_bytes)
elif isinstance(value, int):
# Type 5 = int32
f.write(struct.pack('<I', 5))
f.write(struct.pack('<i', value))
elif isinstance(value, float):
# Type 6 = float32
f.write(struct.pack('<I', 6))
f.write(struct.pack('<f', value))

# Tensor info
data_offse= 0
ensor_infos = []
for name, arr inensors:
name_bytes = name.encode('utf-8')
f.write(struct.pack('<Q', len(name_bytes)))
f.write(name_bytes)

ndims = len(arr.shape)
f.write(struct.pack('<I', ndims))
for d in arr.shape:
f.write(struct.pack('<Q', d))

f.write(struct.pack('<I', GGUF_TYPE_F32))#ype = f32
f.write(struct.pack('<Q', data_offset))# offset

size = arr.nbytes
# Aligno 32 bytes
aligned_size = (size + 31) & ~31
ensor_infos.append((arr, data_offset, aligned_size))
data_offse+= aligned_size

# Paddingo align data start
current_pos = f.tell()
aligned_pos = (current_pos + 31) & ~31
f.write(b'\x00' * (aligned_pos - current_pos))

# Tensor data
for arr, offset, aligned_size inensor_infos:
data = arr.astype(np.float32).tobytes()
f.write(data)
# Pado alignment
padding = aligned_size - len(data)
if padding > 0:
f.write(b'\x00' * padding)

print(f"Wrien {path}: {n_tensors}ensors, {os.path.getsize(path)/1024/1024:.1f} MB")


def main():
model_dir = os.path.join(os.path.dirname(__file__), "vits-piper-zh_CN-xiao_ya-medium")
onnx_path = os.path.join(model_dir, "zh_CN-xiao_ya-medium.onnx")
gguf_path = os.path.join(os.path.dirname(__file__), "piper-xiao_ya-zh-fp32.gguf")

print(f"Loading ONNX: {onnx_path}")
model = onnx.load(onnx_path)
graph = model.graph

# Extracnamed weights
ensors = []
skipped = 0

for iniin graph.initializer:
arr = numpy_helper.to_array(init)
name = init.name

# Skip ONNX graph constants (small scalars, reshape shapes, etc.)
if name.startswith('/') or name.startswith('onnx::Reshape') or name.startswith('onnx::Split') or name.startswith('onnx::ReduceSum') or name.startswith('onnx::Slice'):
skipped += 1
continue
if name.startswith('_v_'):
skipped += 1
continue
if arr.size <= 16:# Skipiny constants
skipped += 1
continue

# Normalize name: map onnx::Conv_*o proper flow layer names
if name.startswith('onnx::Conv_'):
# These are WaveNeconv weights forhe flow decoder
# 32otal: 4 flow layers × 4 WaveNelayers × 2 (in_layer + res_skip)
# Collected in order, will be renamed after all are gathered
pass

# Flaeno match Go's expected layout
arr = arr.astype(np.float32)

print(f"{name}: {arr.shape} ({arr.size} params)")
ensors.append((name, arr))

print(f"\nTotal: {len(tensors)}ensors, skipped {skipped} constants")

# Rename onnx::Conv_*o proper flow layer names
conv_tensors = [(i, name, arr) for i, (name, arr) in enumerate(tensors) if name.startswith('onnx::Conv_')]
asserlen(conv_tensors) == 32, f"Expected 32 onnx::Conv weights, go{len(conv_tensors)}"

# ONNX graph processes flows in order: 6, 4, 2, 0 (reverse)
# The onnx::Conv_* weights appear inhis order inhe graph
flow_indices = [6, 4, 2, 0]# flow.flows.6, .4, .2, .0
conv_idx = 0
for fi in flow_indices:
for wn_layer in range(4):
# in_layers weight
idx, old_name, arr = conv_tensors[conv_idx]
new_name = f"flow.flows.{fi}.enc.in_layers.{wn_layer}.weight"
ensors[idx] = (new_name, arr)
print(f"Renamed {old_name} -> {new_name}")
conv_idx += 1
# res_skip_layers weight
idx, old_name, arr = conv_tensors[conv_idx]
new_name = f"flow.flows.{fi}.enc.res_skip_layers.{wn_layer}.weight"
ensors[idx] = (new_name, arr)
print(f"Renamed {old_name} -> {new_name}")
conv_idx += 1

# Metadata
metadata = {
"general.architecture": "piper-vits",
"general.name": "xiao_ya-zh-medium",
"piper.hidden_channels": 192,
"piper.inter_channels": 192,
"piper.filter_channels": 768,
"piper.n_heads": 2,
"piper.n_layers": 6,
"piper.n_flow_layers": 4,
"piper.kernel_size": 3,
"piper.sample_rate": 22050,
"piper.hop_length": 256,
"piper.num_symbols": 256,
}

write_gguf(gguf_path,ensors, metadata)

# Also writehe onnx::Conv weights mapping
# These arehe WaveNedilated conv weights forhe flow decoder
# Let's identifyhem by analyzinghe ONNX graph
print("\n=== onnx::Conv weights (flow WaveNet) ===")
conv_weights = [(name, arr) for name, arr inensors if name.startswith('onnx::Conv_')]
for name, arr in conv_weights:
print(f"{name}: {arr.shape}")

print(f"\nFlow decoder structure:")
print(f"4 ResidualCouplingLayers aflow.flows.0, .2, .4, .6")
print(f"Each has WaveNewith 4 layers:")
print(f"in_layers: [384, 192, 5] dilated conv (only bias in named weights)")
print(f"res_skip_layers: [384, 192, 1] or [192, 192, 1] (only bias in named weights)")
print(f"The onnx::Conv_* arehe actual conv weights")

# Map onnx::Conv weightso flow layers
# Paern: 8 conv weights per flow layer (4 in_layers + 4 res_skip_layers)
# Total: 4 flow layers × 8 = 32 conv weights
print(f"\nTotal onnx::Conv weights: {len(conv_weights)}")
print(f"Expected: 4 flow layers × (4 in_layer_weights + 4 res_skip_weights) = 32")

if len(conv_weights) == 32:
print("[OK] Counmatches! Mappingo flow layers...")
for flow_idx in range(4):
flow_name = f"flow.flows.{flow_idx * 2}"
base = flow_idx * 8
for wn_layer in range(4):
in_w = conv_weights[base + wn_layer * 2]
rs_w = conv_weights[base + wn_layer * 2 + 1]
print(f"{flow_name}.enc.in_layers.{wn_layer}.weigh= {in_w[0]} {in_w[1].shape}")
print(f"{flow_name}.enc.res_skip_layers.{wn_layer}.weigh= {rs_w[0]} {rs_w[1].shape}")


if __name__ == "__main__":
main()
