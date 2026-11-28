package nnapi

import "fmt"

// wireKind is one NNAPI operation input. The sequence matches
// NeuralNetworksTypes.h, which is not the same order as operation.in.
type wireKind uint8

const (
	wireT0 wireKind = iota
	wireT1
	wireT2
	wireAxis
	wireSplits
	wireFused
	wireBeta
	wireAdjX
	wireAdjY
	wirePadSame
	wireStride1
	wireDepthMul
	wireKeepDims
)

// wireInputs returns the NNAPI input list for op.
// SPLIT is tensor, axis, count (ANEURALNETWORKS_SPLIT).
// GATHER is params, axis, indices (ANEURALNETWORKS_GATHER).
func wireInputs(op operation) (string, []wireKind, error) {
	switch op.code {
	case opAdd:
		if len(op.in) < 2 {
			return "", nil, fmt.Errorf("add inputs %d", len(op.in))
		}
		return "ADD", []wireKind{wireT0, wireT1, wireFused}, nil
	case opMul:
		if len(op.in) < 2 {
			return "", nil, fmt.Errorf("mul inputs %d", len(op.in))
		}
		return "MUL", []wireKind{wireT0, wireT1, wireFused}, nil
	case opSub:
		if len(op.in) < 2 {
			return "", nil, fmt.Errorf("sub inputs %d", len(op.in))
		}
		return "SUB", []wireKind{wireT0, wireT1, wireFused}, nil
	case opFC:
		if len(op.in) < 3 {
			return "", nil, fmt.Errorf("fully connected inputs %d", len(op.in))
		}
		return "FULLY_CONNECTED", []wireKind{wireT0, wireT1, wireT2, wireFused}, nil
	case opReshape:
		if len(op.in) < 2 {
			return "", nil, fmt.Errorf("reshape inputs %d", len(op.in))
		}
		return "RESHAPE", []wireKind{wireT0, wireT1}, nil
	case opTranspose:
		if len(op.in) < 2 {
			return "", nil, fmt.Errorf("transpose inputs %d", len(op.in))
		}
		return "TRANSPOSE", []wireKind{wireT0, wireT1}, nil
	case opSplit:
		if len(op.in) < 1 || op.splits <= 0 {
			return "", nil, fmt.Errorf("split")
		}
		return "SPLIT", []wireKind{wireT0, wireAxis, wireSplits}, nil
	case opBMM:
		if len(op.in) < 2 {
			return "", nil, fmt.Errorf("batch matmul inputs %d", len(op.in))
		}
		return "BATCH_MATMUL", []wireKind{wireT0, wireT1, wireAdjX, wireAdjY}, nil
	case opSoftmax:
		if len(op.in) < 1 {
			return "", nil, fmt.Errorf("softmax")
		}
		return "SOFTMAX", []wireKind{wireT0, wireBeta, wireAxis}, nil
	case opDWConv:
		if len(op.in) < 3 {
			return "", nil, fmt.Errorf("depthwise inputs %d", len(op.in))
		}
		return "DEPTHWISE_CONV_2D", []wireKind{
			wireT0, wireT1, wireT2,
			wirePadSame, wireStride1, wireStride1, wireDepthMul, wireFused,
		}, nil
	case opMean:
		if len(op.in) < 2 {
			return "", nil, fmt.Errorf("mean inputs %d", len(op.in))
		}
		return "MEAN", []wireKind{wireT0, wireT1, wireKeepDims}, nil
	case opRsqrt:
		if len(op.in) < 1 {
			return "", nil, fmt.Errorf("rsqrt")
		}
		return "RSQRT", []wireKind{wireT0}, nil
	case opArgMax:
		if len(op.in) < 1 {
			return "", nil, fmt.Errorf("argmax")
		}
		return "ARGMAX", []wireKind{wireT0, wireAxis}, nil
	case opTanh:
		if len(op.in) < 1 {
			return "", nil, fmt.Errorf("tanh")
		}
		return "TANH", []wireKind{wireT0}, nil
	case opGather:
		if len(op.in) < 2 {
			return "", nil, fmt.Errorf("gather inputs %d", len(op.in))
		}
		return "GATHER", []wireKind{wireT0, wireAxis, wireT1}, nil
	default:
		return "", nil, fmt.Errorf("unknown op %d", op.code)
	}
}
