# corelib/yolo — Pure Go YOLOv8 Inference Engine

Pure Go implementation of YOLOv8 inference for OmniParser's icon detection model.
No CGo, no ONNX, no Python dependency.

## Status

**Phase 1 完成**：基础框架（tensor 库、Conv2d+BN+SiLU、C2f/SPPF/Bottleneck、Detect Head+NMS、权重加载、图像预处理）。20 个单元测试通过。

**Phase 2 待实现**：OmniParser V2 的实际模型不是标准 YOLOv8s，而是一个更大的变体（可能是 YOLOv11），包含以下额外模块：
- Depthwise Separable Conv（分类头使用 groups=channels 的卷积）
- C2fCIB（Cross-stage Information Bottleneck，3 个 conv 的 C2f 变体）
- PSA/Attention（Partial Self-Attention，含 QKV + positional encoding + FFN）
- 网络层编号与标准 YOLOv8s 不同（检测头在 model.23 而非 model.22）

## Architecture

```
tensor.go     — Multi-dimensional float32 tensor with basic ops
conv.go       — Conv2d + BatchNorm + SiLU (fused), im2col optimization
blocks.go     — C2f, SPPF, Bottleneck, Upsample, Concat
model.go      — YOLOv8 network graph: backbone + neck + detect head
detect.go     — Anchor-free detection head: DFL decode + NMS
weights.go    — Load weights from custom binary format (.yolow)
preprocess.go — Image resize + letterbox + normalize to tensor
```

## Weight Conversion

Use `cmd/convert_yolo_weights/main.py` to convert ultralytics `.pt` to `.yolow`:

```bash
python cmd/convert_yolo_weights/main.py --input weights/model.pt --output weights/model.yolow
```

## Usage

```go
model, err := yolo.LoadModel("weights/omniparser-v2.yolow")
detections, err := model.Detect(pngBase64, 0.3, 0.5) // conf_thresh, iou_thresh
for _, d := range detections {
    fmt.Printf("box=(%d,%d,%d,%d) conf=%.2f\n", d.X, d.Y, d.W, d.H, d.Confidence)
}
```
