package yolo

// Bottleneck is a residual block: two Conv2dBNSiLU layers with optional shortcut.
type Bottleneck struct {
	CV1      *Conv2dBNSiLU // 3x3 conv
	CV2      *Conv2dBNSiLU // 3x3 conv
	Shortcut bool          // add input to output
}

func (b *Bottleneck) Forward(x *Tensor) *Tensor {
	out := b.CV1.Forward(x)
	out = b.CV2.Forward(out)
	if b.Shortcut && x.Shape[1] == out.Shape[1] {
		out.Add(x)
	}
	return out
}

// C3k is a Cross Stage Partial block with 3 convolutions and N bottlenecks.
// Architecture:
//   1. cv1: 1x1 conv (reduce channels)
//   2. cv2: 1x1 conv (reduce channels)
//   3. Pass cv1 output through N bottlenecks sequentially
//   4. Concat cv2 output + bottleneck output
//   5. cv3: 1x1 conv (merge)
type C3k struct {
	CV1         *Conv2dBNSiLU
	CV2         *Conv2dBNSiLU
	CV3         *Conv2dBNSiLU
	Bottlenecks []*Bottleneck
}

func (c *C3k) Forward(x *Tensor) *Tensor {
	y1 := c.CV1.Forward(x)
	// Pass through bottleneck sequence
	for _, bn := range c.Bottlenecks {
		y1 = bn.Forward(y1)
	}
	y2 := c.CV2.Forward(x)
	cat := ConcatChannel(y1, y2)
	return c.CV3.Forward(cat)
}

// C3k2 is the C2f-like module used in YOLO11. It splits the cv1 output,
// passes one half through C3k sub-modules, then concatenates and reduces.
// Architecture:
//   1. cv1: 1x1 conv to expand channels
//   2. Split output into two halves along channel dim
//   3. Pass second half through N C3k sub-modules, collecting outputs
//   4. Concat all chunks along channel dim
//   5. cv2: 1x1 conv to reduce channels
type C3k2 struct {
	CV1     *Conv2dBNSiLU
	CV2     *Conv2dBNSiLU
	Modules []*C3k // sub-modules (typically 1)
}

func (c *C3k2) Forward(x *Tensor) *Tensor {
	y := c.CV1.Forward(x)
	halfC := y.Shape[1] / 2
	chunks := []*Tensor{
		y.SliceChannel(0, halfC),
		y.SliceChannel(halfC, y.Shape[1]),
	}

	current := chunks[len(chunks)-1]
	for _, m := range c.Modules {
		current = m.Forward(current)
		chunks = append(chunks, current)
	}

	cat := ConcatChannel(chunks...)
	return c.CV2.Forward(cat)
}

// C2f is the Cross Stage Partial v2 with feed-forward module (YOLOv8 original).
// Kept for backward compatibility. C3k2 is the YOLO11 replacement.
type C2f struct {
	CV1         *Conv2dBNSiLU
	CV2         *Conv2dBNSiLU
	Bottlenecks []*Bottleneck
}

func (c *C2f) Forward(x *Tensor) *Tensor {
	// Step 1: expand
	y := c.CV1.Forward(x)

	// Step 2: split into two halves
	halfC := y.Shape[1] / 2
	chunks := []*Tensor{
		y.SliceChannel(0, halfC),
		y.SliceChannel(halfC, y.Shape[1]),
	}

	// Step 3: pass through bottlenecks, each takes the last chunk
	current := chunks[len(chunks)-1]
	for _, bn := range c.Bottlenecks {
		current = bn.Forward(current)
		chunks = append(chunks, current)
	}

	// Step 4: concat all chunks
	cat := ConcatChannel(chunks...)

	// Step 5: reduce
	return c.CV2.Forward(cat)
}

// SPPF is Spatial Pyramid Pooling - Fast.
// Architecture:
//   1. cv1: 1x1 conv
//   2. Three sequential 5x5 max pools (each takes previous output)
//   3. Concat input + 3 pool outputs along channel dim
//   4. cv2: 1x1 conv
type SPPF struct {
	CV1 *Conv2dBNSiLU
	CV2 *Conv2dBNSiLU
	K   int // pool kernel size (default 5)
}

func (s *SPPF) Forward(x *Tensor) *Tensor {
	y := s.CV1.Forward(x)
	k := s.K
	if k == 0 {
		k = 5
	}
	pad := k / 2

	p1 := y.MaxPool2d(k, 1, pad)
	p2 := p1.MaxPool2d(k, 1, pad)
	p3 := p2.MaxPool2d(k, 1, pad)

	cat := ConcatChannel(y, p1, p2, p3)
	return s.CV2.Forward(cat)
}
