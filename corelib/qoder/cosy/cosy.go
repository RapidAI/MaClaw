// Package cosy runs Qoder's own qoder_auth_wasm (extracted from the official
// CLI 1.1.64 runtime) inside a wazero sandbox to reproduce the signed request
// contract the site inference gateways expect: every /algo endpoint validates
// a COSY bearer token plus the Cosy-* header family, and POST bodies ride a
// custom encoding produced by the same wasm. The wasm is wasm-bindgen output;
// runtime.go implements its import shims on Go values.
package cosy

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	wazero "github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// UserInfo is the serde record the wasm's QoderContext requires: uid,
// encrypt_user_info and key must all be present. DataPolicyAgreed is optional;
// present-and-true flips the Cosy-Data-Policy header from the disagree default
// (the serde tolerates the extra field — verified against the official wasm).
type UserInfo struct {
	UID              string `json:"uid"`
	EncryptUserInfo  string `json:"encrypt_user_info"`
	Key              string `json:"key"`
	DataPolicyAgreed *bool  `json:"data_policy_agreed,omitempty"`
}

// Result is one prepared request: the rewritten URL (the wasm moves paths
// under /algo and appends the contract query), the signed header map, and for
// POST bodies the custom-encoded payload.
type Result struct {
	URL     string
	Headers map[string]string
	Body    string // POST bodies only; empty for GET
}

// Context is one wasm-backed signing context. Not safe for concurrent use;
// PrepareRequest serializes internally.
type Context struct {
	mod       api.Module
	ctxPtr    int32
	uid       string
	material  UserInfo
	mu        sync.Mutex
	closeOnce sync.Once
}

var moduleSeq atomic.Uint64

func nextModuleName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, moduleSeq.Add(1))
}

// instantiate brings up a fresh wasm instance from the shared compile.
func instantiate(name string) (api.Module, error) {
	rt, err := shared()
	if err != nil {
		return nil, err
	}
	return rt.mod.InstantiateModule(context.Background(), rt.compiled,
		wazero.NewModuleConfig().WithName(name).WithStartFunctions())
}

// stackPush / stackPop manage the wasm-bindgen scratch stack.
func (c *Context) stackPush() (int32, error) {
	rets, err := c.mod.ExportedFunction("__wbindgen_add_to_stack_pointer").Call(
		context.Background(), wasmNegative16) // -16
	if err != nil {
		return 0, err
	}
	return int32(uint32(rets[0])), nil
}

func (c *Context) stackPop() {
	_, _ = c.mod.ExportedFunction("__wbindgen_add_to_stack_pointer").Call(context.Background(), 16)
}

// readI32 decodes one little-endian i32 from linear memory.
func (c *Context) readI32(offset int32) int32 {
	return i32At(c.mod, offset)
}

// heapError renders a heap slot as an error message.
func heapError(idx int32) string {
	switch v := globalHeap.get(idx).(type) {
	case string:
		return v
	case error:
		return v.Error()
	default:
		return "unknown wasm error"
	}
}

// New creates the signing context: profile-key fingerprint from the wasm,
// fresh auth material generated from the uid, then the wasm QoderContext.
func New(machineID, cosyVersion, uid string) (*Context, error) {
	mod, err := instantiate(nextModuleName("cosy"))
	if err != nil {
		return nil, err
	}
	c := &Context{mod: mod}
	if err := c.newContext(machineID, cosyVersion, UserInfo{UID: uid, DataPolicyAgreed: boolPtr(true)}); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// GenerateAuthFields produces the {encrypt_user_info, key} signing material
// for a uid-bearing record via the standalone wasm export.
func GenerateAuthFields(record UserInfo) (UserInfo, error) {
	mod, err := instantiate(nextModuleName("cosy-gen"))
	if err != nil {
		return UserInfo{}, err
	}
	defer mod.Close(context.Background())
	return generateAuthFieldsOn(mod, record)
}

// generateAuthFieldsOn runs the material generation on an existing module
// instance so callers can reuse their own instance instead of spinning a
// throwaway one.
func generateAuthFieldsOn(mod api.Module, record UserInfo) (UserInfo, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return UserInfo{}, err
	}
	ptr, length, err := allocString(mod, string(raw))
	if err != nil {
		return UserInfo{}, err
	}

	stack, err := stackMove(mod, wasmNegative16)
	if err != nil {
		return UserInfo{}, err
	}
	defer func() { _, _ = stackMove(mod, 16) }()

	ret0(mod, "generate_runtime_auth_fields", uint64(uint32(stack)), uint64(uint32(ptr)), uint64(uint32(length)))
	// result area: (outPtr, outLen, errHeapIdx, errFlag)
	outPtr := i32At(mod, stack)
	outLen := i32At(mod, stack+4)
	errIdx := i32At(mod, stack+8)
	if i32At(mod, stack+12) != 0 {
		return UserInfo{}, fmt.Errorf("generate_runtime_auth_fields: %s", heapError(errIdx))
	}
	var material UserInfo
	if err := json.Unmarshal([]byte(readString(mod, outPtr, outLen)), &material); err != nil {
		return UserInfo{}, fmt.Errorf("auth material decode: %w", err)
	}
	if material.UID == "" {
		material.UID = record.UID
	}
	material.DataPolicyAgreed = record.DataPolicyAgreed
	return material, nil
}

func boolPtr(v bool) *bool { return &v }

// stackMove shifts the wasm-bindgen scratch stack by a signed delta.
func stackMove(mod api.Module, delta uint64) (int32, error) {
	rets, err := mod.ExportedFunction("__wbindgen_add_to_stack_pointer").Call(context.Background(), delta)
	if err != nil {
		return 0, err
	}
	return int32(uint32(rets[0])), nil
}

// newContext constructs the wasm QoderContext and stores its pointer. The
// material is generated on the context's own module instance — one wasm
// instantiation per context instead of one per step.
func (c *Context) newContext(machineID, cosyVersion string, record UserInfo) error {
	material, err := generateAuthFieldsOn(c.mod, record)
	if err != nil {
		return err
	}
	userRaw, err := json.Marshal(material)
	if err != nil {
		return err
	}
	machineIDPtr, machineIDLen, err := allocString(c.mod, machineID)
	if err != nil {
		return err
	}
	versionPtr, versionLen, err := allocString(c.mod, cosyVersion)
	if err != nil {
		return err
	}
	userPtr, userLen, err := allocString(c.mod, string(userRaw))
	if err != nil {
		return err
	}
	keyPtr, keyLen, err := allocString(c.mod, c.profileKeyFingerprintJSON())
	if err != nil {
		return err
	}

	stack, err := stackMove(c.mod, wasmNegative16)
	if err != nil {
		return err
	}
	defer func() { _, _ = stackMove(c.mod, 16) }()

	// qodercontext_new(outPtr, machineID(p,l), version(p,l), user(p,l), key(p,l))
	_, err = c.mod.ExportedFunction("qodercontext_new").Call(context.Background(),
		uint64(uint32(stack)),
		uint64(uint32(machineIDPtr)), uint64(uint32(machineIDLen)),
		uint64(uint32(versionPtr)), uint64(uint32(versionLen)),
		uint64(uint32(userPtr)), uint64(uint32(userLen)),
		uint64(uint32(keyPtr)), uint64(uint32(keyLen)))
	if err != nil {
		return fmt.Errorf("qodercontext_new: %w", err)
	}
	// result area: (ctxPtr, errHeapIdx, errFlag)
	if i32At(c.mod, stack+8) != 0 {
		return fmt.Errorf("qodercontext_new failed: %s", heapError(i32At(c.mod, stack+4)))
	}
	c.ctxPtr = i32At(c.mod, stack)
	c.uid = material.UID
	c.material = material
	return nil
}

func (c *Context) profileKeyFingerprintJSON() string {
	stack, err := c.stackPush()
	if err != nil {
		return ""
	}
	defer c.stackPop()
	_, _ = c.mod.ExportedFunction("get_profile_key_fingerprint").Call(context.Background(), uint64(uint32(stack)))
	return readString(c.mod, i32At(c.mod, stack), i32At(c.mod, stack+4))
}

func ret0(mod api.Module, name string, args ...uint64) uint64 {
	rets, _ := mod.ExportedFunction(name).Call(context.Background(), args...)
	if len(rets) == 0 {
		return 0
	}
	return rets[0]
}

func i32At(mod api.Module, offset int32) int32 {
	buf, ok := mod.ExportedMemory("memory").Read(uint32(offset), 4)
	if !ok {
		return 0
	}
	return int32(uint32(buf[0]) | uint32(buf[1])<<8 | uint32(buf[2])<<16 | uint32(buf[3])<<24)
}

// RefreshAuthFields folds the current device token into the signing material:
// the wasm re-derives its info blob around the token from the extended record
// {uid, encrypt_user_info, key, token}.
func (c *Context) RefreshAuthFields(token string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	// The record must carry the data-policy flag: refreshAuthFields re-derives
	// the context state, and omitting the flag resets the header to disagree.
	record := struct {
		UID              string `json:"uid"`
		EncryptUserInfo  string `json:"encrypt_user_info"`
		Key              string `json:"key"`
		Token            string `json:"token"`
		DataPolicyAgreed *bool  `json:"data_policy_agreed,omitempty"`
	}{c.uid, c.material.EncryptUserInfo, c.material.Key, token, c.material.DataPolicyAgreed}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	ptr, length, err := allocString(c.mod, string(raw))
	if err != nil {
		return err
	}

	stack, err := stackMove(c.mod, wasmNegative16)
	if err != nil {
		return err
	}
	defer func() { _, _ = stackMove(c.mod, 16) }()
	_, err = c.mod.ExportedFunction("qodercontext_refreshAuthFields").Call(context.Background(),
		uint64(uint32(stack)), uint64(uint32(c.ctxPtr)), uint64(uint32(ptr)), uint64(uint32(length)))
	if err != nil {
		return err
	}
	if i32At(c.mod, stack+4) != 0 {
		return fmt.Errorf("refreshAuthFields: %s", heapError(i32At(c.mod, stack)))
	}
	return nil
}

// PrepareRequest builds one signed request.
// mode is the wasm auth mode ("auth" for COSY-signed traffic);
// bodyJSON carries the request body for POST (the wasm encodes it);
// extraJSON optionally carries additional request configuration.
func (c *Context) PrepareRequest(endpoint, path, method, mode, bodyJSON, extraJSON string) (*Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	place := func(s string, optional bool) (uint64, uint64, error) {
		if optional && s == "" {
			return 0, 0, nil
		}
		ptr, length, err := allocString(c.mod, s)
		if err != nil {
			return 0, 0, err
		}
		return uint64(uint32(ptr)), uint64(uint32(length)), nil
	}

	stack, err := c.stackPush()
	if err != nil {
		return nil, err
	}
	defer c.stackPop()

	eP, eL, err := place(endpoint, false)
	if err != nil {
		return nil, err
	}
	pP, pL, err := place(path, false)
	if err != nil {
		return nil, err
	}
	mP, mL, err := place(method, false)
	if err != nil {
		return nil, err
	}
	mdP, mdL, err := place(mode, false)
	if err != nil {
		return nil, err
	}
	bP, bL, err := place(bodyJSON, true)
	if err != nil {
		return nil, err
	}
	xP, xL, err := place(extraJSON, true)
	if err != nil {
		return nil, err
	}

	prepare := c.mod.ExportedFunction("qodercontext_prepareRequest")
	if prepare == nil {
		return nil, fmt.Errorf("wasm export qodercontext_prepareRequest missing")
	}
	// (outPtr, ctxPtr, endpoint(p,l), path(p,l), method(p,l), mode(p,l), body?(p,l), extra?(p,l))
	_, err = prepare.Call(context.Background(),
		uint64(uint32(stack)), uint64(uint32(c.ctxPtr)),
		eP, eL, pP, pL, mP, mL, mdP, mdL, bP, bL, xP, xL)
	if err != nil {
		return nil, fmt.Errorf("qodercontext_prepareRequest: %w", err)
	}
	// result area: (resultPtr, errHeapIdx, errFlag)
	if i32At(c.mod, stack+8) != 0 {
		return nil, fmt.Errorf("prepareRequest failed: %s", heapError(i32At(c.mod, stack+4)))
	}
	resultPtr := i32At(c.mod, stack)
	url, err := c.resultURL(resultPtr)
	if err != nil {
		return nil, err
	}
	headers, err := c.readHeaders(resultPtr)
	if err != nil {
		return nil, err
	}
	return &Result{URL: url, Headers: headers}, nil
}

// resultURL reads the RequestResult's url through its wasm export.
func (c *Context) resultURL(resultPtr int32) (string, error) {
	stack, err := stackMove(c.mod, wasmNegative16)
	if err != nil {
		return "", err
	}
	defer func() { _, _ = stackMove(c.mod, 16) }()
	_, err = c.mod.ExportedFunction("requestresult_url").Call(context.Background(),
		uint64(uint32(stack)), uint64(uint32(resultPtr)))
	if err != nil {
		return "", err
	}
	return readString(c.mod, i32At(c.mod, stack), i32At(c.mod, stack+4)), nil
}

// PrepareInferRequest builds one signed inference request (chat / SSE类).
// The tiny request shape mirrors agent_query: endpoint ORIGIN (no path!),
// raw JSON body, model key, model source. Returns url + signed headers plus
// the ENCODED body (custom charset) that must be sent verbatim.
func (c *Context) PrepareInferRequest(endpoint, bodyJSON, modelKey, modelSource string) (*Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	stack, err := c.stackPush()
	if err != nil {
		return nil, err
	}
	defer c.stackPop()

	eP, eL, err := allocString(c.mod, endpoint)
	if err != nil {
		return nil, err
	}
	bP, bL, err := allocString(c.mod, bodyJSON)
	if err != nil {
		return nil, err
	}
	kP, kL, err := allocString(c.mod, modelKey)
	if err != nil {
		return nil, err
	}
	sP, sL, err := allocString(c.mod, modelSource)
	if err != nil {
		return nil, err
	}

	prepare := c.mod.ExportedFunction("qodercontext_prepareInferRequest")
	if prepare == nil {
		return nil, fmt.Errorf("wasm export qodercontext_prepareInferRequest missing")
	}
	// (outPtr, ctxPtr, endpoint(p,l), body?(p,l), key?(p,l), source?(p,l))
	_, err = prepare.Call(context.Background(),
		uint64(uint32(stack)), uint64(uint32(c.ctxPtr)),
		uint64(uint32(eP)), uint64(uint32(eL)),
		uint64(uint32(bP)), uint64(uint32(bL)),
		uint64(uint32(kP)), uint64(uint32(kL)),
		uint64(uint32(sP)), uint64(uint32(sL)))
	if err != nil {
		return nil, fmt.Errorf("qodercontext_prepareInferRequest: %w", err)
	}
	if i32At(c.mod, stack+8) != 0 {
		return nil, fmt.Errorf("prepareInferRequest failed: %s", heapError(i32At(c.mod, stack+4)))
	}
	resultPtr := i32At(c.mod, stack)
	url, err := c.resultURL(resultPtr)
	if err != nil {
		return nil, err
	}
	headers, err := c.readHeaders(resultPtr)
	if err != nil {
		return nil, err
	}
	body, err := c.resultBody(resultPtr)
	if err != nil {
		return nil, err
	}
	return &Result{URL: url, Headers: headers, Body: body}, nil
}

// resultBody reads the RequestResult's encoded body text.
func (c *Context) resultBody(resultPtr int32) (string, error) {
	stack, err := c.stackPush()
	if err != nil {
		return "", err
	}
	defer c.stackPop()
	_, err = c.mod.ExportedFunction("requestresult_body").Call(context.Background(),
		uint64(uint32(stack)), uint64(uint32(resultPtr)))
	if err != nil {
		return "", err
	}
	ptr := i32At(c.mod, stack)
	length := i32At(c.mod, stack+4)
	if ptr == 0 {
		return "", nil
	}
	defer func() {
		_, _ = c.mod.ExportedFunction("__wbindgen_export4").Call(context.Background(),
			uint64(uint32(ptr)), uint64(uint32(length)), 1)
	}()
	return readString(c.mod, ptr, length), nil
}

// readHeaders drains the result's header map through requestresult_headers.
func (c *Context) readHeaders(resultPtr int32) (map[string]string, error) {
	rets, err := c.mod.ExportedFunction("requestresult_headers").Call(context.Background(), uint64(uint32(resultPtr)))
	if err != nil {
		return nil, err
	}
	idx := int32(uint32(rets[0]))
	defer globalHeap.drop(idx)
	m, _ := globalHeap.get(idx).(map[string]string)
	if m == nil {
		m = map[string]string{}
	}
	return m, nil
}

// Close frees the wasm context; safe to call twice.
func (c *Context) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeOnce.Do(func() {
		if c.mod != nil {
			if c.ctxPtr != 0 {
				_, _ = c.mod.ExportedFunction("__wbg_qodercontext_free").Call(context.Background(), uint64(uint32(c.ctxPtr)), 0)
				c.ctxPtr = 0
			}
			_ = c.mod.Close(context.Background())
		}
	})
}
