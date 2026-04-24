package yolo

// YOLO11l model architecture (OmniParser V2 icon detection variant).
//
// Actual structure from model inspection:
//
// Backbone:
//   [0]  Conv(3→64, k=3, s=2, p=1)
//   [1]  Conv(64→128, k=3, s=2, p=1)
//   [2]  C3k2(128→256, n=1)
//   [3]  Conv(256→256, k=3, s=2, p=1)
//   [4]  C3k2(256→512, n=1)              ← P3
//   [5]  Conv(512→512, k=3, s=2, p=1)
//   [6]  C3k2(512→512, n=1)              ← P4
//   [7]  Conv(512→512, k=3, s=2, p=1)
//   [8]  C3k2(512→512, n=1)
//   [9]  SPPF(512→512, k=5)
//   [10] C2PSA(512→512, n=1)             ← P5
//
// Neck (FPN + PAN):
//   [11] Upsample(P5, 2x)
//   [12] Concat(11, P4)                  → 1024ch
//   [13] C3k2(1024→512, n=1)             ← N3
//   [14] Upsample(N3, 2x)
//   [15] Concat(14, P3)                  → 1024ch
//   [16] C3k2(1024→256, n=1)             ← N4 (detect scale 0, stride 8)
//   [17] Conv(256→256, k=3, s=2, p=1)
//   [18] Concat(17, N3)                  → 768ch
//   [19] C3k2(768→512, n=1)              ← N5 (detect scale 1, stride 16)
//   [20] Conv(512→512, k=3, s=2, p=1)
//   [21] Concat(20, P5)                  → 1024ch
//   [22] C3k2(1024→512, n=1)             ← N6 (detect scale 2, stride 32)
//
// Head:
//   [23] Detect(N4, N5, N6)

// Model is a complete YOLO11 detection model.
type Model struct {
	// Backbone
	B0  *Conv2dBNSiLU
	B1  *Conv2dBNSiLU
	B2  *C3k2
	B3  *Conv2dBNSiLU
	B4  *C3k2
	B5  *Conv2dBNSiLU
	B6  *C3k2
	B7  *Conv2dBNSiLU
	B8  *C3k2
	B9  *SPPF
	B10 *C2PSA

	// Neck
	N13 *C3k2
	N16 *C3k2
	N17 *Conv2dBNSiLU
	N19 *C3k2
	N20 *Conv2dBNSiLU
	N22 *C3k2

	// Head
	Head *DetectHead

	// Config
	InputSize int // 640
	NC        int // number of classes
}

// Forward runs the full YOLO11 inference pipeline.
func (m *Model) Forward(x *Tensor) *Tensor {
	// Backbone
	x = m.B0.Forward(x)   // [1, 64, 320, 320]
	x = m.B1.Forward(x)   // [1, 128, 160, 160]
	x = m.B2.Forward(x)   // [1, 256, 160, 160]
	x = m.B3.Forward(x)   // [1, 256, 80, 80]
	p3 := m.B4.Forward(x) // [1, 512, 80, 80]
	x = m.B5.Forward(p3)  // [1, 512, 40, 40]
	p4 := m.B6.Forward(x) // [1, 512, 40, 40]
	x = m.B7.Forward(p4)  // [1, 512, 20, 20]
	x = m.B8.Forward(x)   // [1, 512, 20, 20]
	x = m.B9.Forward(x)   // [1, 512, 20, 20]
	p5 := m.B10.Forward(x) // [1, 512, 20, 20]

	// Neck — FPN (top-down)
	up1 := p5.Upsample2x()            // [1, 512, 40, 40]
	cat1 := ConcatChannel(up1, p4)     // [1, 1024, 40, 40]
	n3 := m.N13.Forward(cat1)          // [1, 512, 40, 40]

	up2 := n3.Upsample2x()            // [1, 512, 80, 80]
	cat2 := ConcatChannel(up2, p3)     // [1, 1024, 80, 80]
	n4 := m.N16.Forward(cat2)          // [1, 256, 80, 80]

	// Neck — PAN (bottom-up)
	down1 := m.N17.Forward(n4)         // [1, 256, 40, 40]
	cat3 := ConcatChannel(down1, n3)   // [1, 768, 40, 40]
	n5 := m.N19.Forward(cat3)          // [1, 512, 40, 40]

	down2 := m.N20.Forward(n5)         // [1, 512, 20, 20]
	cat4 := ConcatChannel(down2, p5)   // [1, 1024, 20, 20]
	n6 := m.N22.Forward(cat4)          // [1, 512, 20, 20]

	// Head
	return m.Head.Forward([]*Tensor{n4, n5, n6})
}

// Detect runs full inference: preprocess → forward → postprocess.
func (m *Model) Detect(pngBase64 string, confThresh, iouThresh float32) ([]Detection, error) {
	imgW, imgH, input, err := PreprocessBase64(pngBase64, m.InputSize)
	if err != nil {
		return nil, err
	}
	preds := m.Forward(input)
	dets := PostProcess(preds, confThresh, iouThresh, imgW, imgH, m.InputSize)
	return dets, nil
}
