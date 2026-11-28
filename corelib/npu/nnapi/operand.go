package nnapi

// operandType matches ANeuralNetworksOperandType on arm64 and amd64
// (LP64): int32 type, uint32 dimensionCount, pointer, float scale, int32 zeroPoint.
// A scalar uses dimensionCount 0 and a nil dimensions pointer.
// A dimension of 0 is unknown until Execution_setInput supplies the concrete size.
type operandType struct {
	typ            int32
	dimensionCount uint32
	dimensions     *uint32
	scale          float32
	zeroPoint      int32
}
