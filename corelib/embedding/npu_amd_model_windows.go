//go:build windows && amd64

// npu_amd_model_windows.go — model wiring on top of the raw IPU bindings.
//
// Kept apart from npu_amd_ipu_windows.go so the syscall layer stays a thin,
// reviewable mirror of RadeonML.h while the sequence of calls a real forward
// needs (set input info -> set input -> ask output shape -> set output ->
// prepare -> infer) lives here.

package embedding

import (
	"fmt"
	"runtime"
	"unsafe"
)

// asciiPtr converts a Go string to a NUL-terminated ANSI string that stays
// alive for the duration of one syscall. An empty name passes a NULL pointer,
// which the API documents as "the single input/output node".
func asciiPtr(s string) (uintptr, []byte) {
	if s == "" {
		return 0, nil
	}
	b := append([]byte(s), 0)
	return uintptr(unsafe.Pointer(&b[0])), b
}

// setInputInfo declares the input shape before the tensor is bound. The IPU
// backend needs static shapes, and this call is what fixes them.
func (r *rmlIPURuntime) setInputInfo(model uintptr, name string, shape []uint32) error {
	if r.procSetInputInfo == nil {
		return fmt.Errorf("IPU setInputInfo: not available")
	}
	info := rmlTensorInfo{DType: rmlDTypeFloat32, Layout: rmlLayoutNC}
	for i, d := range shape {
		if i >= len(info.Shape) {
			return fmt.Errorf("IPU setInputInfo: rank %d exceeds max 5", len(shape))
		}
		info.Shape[i] = d
	}
	p, keep := asciiPtr(name)
	status, _, _ := r.procSetInputInfo.Call(
		model,
		p,
		uintptr(unsafe.Pointer(&info)),
	)
	runtime.KeepAlive(keep)
	if int32(status) != rmlOK {
		return r.statusError("rmlSetModelInputInfo", int32(status))
	}
	return nil
}

// setInput binds an input tensor to the model.
func (r *rmlIPURuntime) setInput(model uintptr, name string, tensor uintptr) error {
	p, keep := asciiPtr(name)
	status, _, _ := r.setModelInput.Call(model, p, tensor)
	runtime.KeepAlive(keep)
	if int32(status) != rmlOK {
		return r.statusError("rmlSetModelInput", int32(status))
	}
	return nil
}

// setOutput binds an output tensor to the model.
func (r *rmlIPURuntime) setOutput(model uintptr, name string, tensor uintptr) error {
	p, keep := asciiPtr(name)
	status, _, _ := r.setModelOutput.Call(model, p, tensor)
	runtime.KeepAlive(keep)
	if int32(status) != rmlOK {
		return r.statusError("rmlSetModelOutput", int32(status))
	}
	return nil
}

// outputShape asks the model for its output tensor shape. Valid only after the
// inputs have been fixed; the API returns RML_ERROR_MODEL_NOT_READY otherwise.
func (r *rmlIPURuntime) outputShape(model uintptr) ([]uint32, error) {
	if r.getOutputInfo == nil {
		// No way to ask: assume the documented [1, dim] layout with the
		// conventional 1024-wide embedding. Better to try than to decline.
		return []uint32{1, 1024}, nil
	}
	var info rmlTensorInfo
	status, _, _ := r.getOutputInfo.Call(model, 0, uintptr(unsafe.Pointer(&info)))
	if int32(status) != rmlOK {
		return nil, r.statusError("rmlGetModelOutputInfo", int32(status))
	}
	if info.DType != rmlDTypeFloat32 {
		return nil, fmt.Errorf("IPU 输出类型 %d 不是 float32（本机绑定只处理 f32）", info.DType)
	}
	// Rank is implied by the layout: NC/NCHW are 2-D here, C is 1-D.
	rank := 2
	if info.Layout == rmlLayoutC {
		rank = 1
	}
	out := make([]uint32, rank)
	for i := 0; i < rank; i++ {
		out[i] = info.Shape[i]
		if out[i] == 0 {
			return nil, fmt.Errorf("IPU 输出第 %d 维为 0（图未就绪）", i)
		}
	}
	return out, nil
}

// prepare finalises the model before the first infer.
func (r *rmlIPURuntime) prepare(model uintptr) error {
	if r.prepareModel == nil {
		return nil // older backend: rmlInfer prepares implicitly
	}
	status, _, _ := r.prepareModel.Call(model)
	if int32(status) != rmlOK {
		return r.statusError("rmlPrepareModel", int32(status))
	}
	return nil
}

// inferModel runs one forward pass. The caller has already bound the tensors.
func (r *rmlIPURuntime) inferModel(model uintptr) error {
	status, _, _ := r.infer.Call(model)
	if int32(status) != rmlOK {
		return r.statusError("rmlInfer", int32(status))
	}
	return nil
}
