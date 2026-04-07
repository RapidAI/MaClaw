package freeproxy

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestWithBrowserSign tests v2/chat using a signature captured from the browser.
// To use: open browser DevTools, send a chat message on ai.dangbei.com,
// copy the sign/nonce/timestamp from the request headers, and paste below.
//
// This test proves whether the issue is signing or something else.
func TestWithBrowserSign(t *testing.T) {
	requireIntegrationTest(t)
	// === PASTE BROWSER VALUES HERE ===
	browserSign := ""      // e.g. "A1B2C3D4..."
	browserNonce := ""     // e.g. "abc123..."
	browserTimestamp := ""  // e.g. "1774258000"
	browserBody := ""      // copy the request body from DevTools
	// === END PASTE ===

	if browserSign == "" {
		t.Skip("No browser sign values provided. Capture from DevTools and paste above.")
	}

	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".maclaw", "freeproxy")

	auth := NewAuthStore(cacheDir)
	if err := auth.Load(); err != nil {
		t.Fatalf("Load auth: %v", err)
	}
	if !auth.HasAuth() {
		t.Skip("No persisted cookie")
	}

	cookie := auth.GetCookie()

	headers := map[string]string{
		"content-type": "application/json",
		"accept":       "text/event-stream",
		"cookie":       cookie,
		"origin":       "https://ai.dangbei.com",
		"referer":      "https://ai.dangbei.com/",
		"user-agent":   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		"sign":         browserSign,
		"nonce":        browserNonce,
		"timestamp":    browserTimestamp,
		"apptype":      "5",
		"lang":         "zh",
		"client-ver":   "1.0.2",
		"appversion":   "1.3.9",
		"version":      "v2",
		"token":        "",
		"connection":   "close",
	}

	raw, err := rawHTTPPost("ai-api.dangbei.net", "/ai-search/chatApi/v2/chat", headers, []byte(browserBody), 15*time.Second)
	if err != nil {
		t.Fatalf("rawHTTPPost: %v", err)
	}
	defer raw.Close()

	t.Logf("Status: %d %s", raw.StatusCode, raw.StatusText)
	bodyReader := raw.Body()
	defer bodyReader.Close()
	buf, _ := io.ReadAll(io.LimitReader(bodyReader, 4096))
	if len(buf) > 0 {
		t.Logf("Body: %s", string(buf))
	}
}

// TestInjectSignToPage creates a bookmarklet-style JS snippet that users can
// run in the browser console to capture the sign values.
func TestInjectSignToPage(t *testing.T) {
	requireIntegrationTest(t)
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".maclaw", "freeproxy")

	auth := NewAuthStore(cacheDir)
	if err := auth.Load(); err != nil {
		t.Fatalf("Load auth: %v", err)
	}
	if !auth.HasAuth() {
		t.Skip("No persisted cookie")
	}

	client := NewDangbeiClient(auth)
	ctx := context.Background()
	if !client.IsAuthenticated(ctx) {
		t.Fatal("Not authenticated")
	}

	convID, err := client.CreateSession(ctx)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Logf("ConversationID: %s", convID)
	// Don't delete - we'll use it in the browser

	bodyStr := fmt.Sprintf(
		`{"stream":true,"botCode":"AI_SEARCH","conversationId":"%s","question":"hi","agentId":""}`,
		convID,
	)

	t.Log("=== Run this in browser console on ai.dangbei.com ===")
	t.Logf(`
// Step 1: Get the sign module (it's already loaded)
// The sign function is available as module 72660's No export
// We need to find it through webpack's require

// Try to find the webpack require function
let webpackRequire;
const scripts = document.querySelectorAll('script');
// The easiest way: intercept the next fetch to /chatApi/v2/chat
const origFetch = window.fetch;
window.fetch = async function(url, init) {
    if (typeof url === 'string' && url.includes('/chatApi/v2/chat')) {
        console.log('=== INTERCEPTED v2/chat ===');
        console.log('URL:', url);
        console.log('Body:', init?.body);
        console.log('Headers:');
        if (init?.headers) {
            for (const [k, v] of Object.entries(init.headers)) {
                console.log('  ' + k + ': ' + v);
            }
        }
    }
    return origFetch.apply(this, arguments);
};
console.log('Fetch interceptor installed. Now send a message in the chat.');
console.log('ConversationID for testing: %s');
console.log('Body for testing: %s');
`, convID, bodyStr)
}

// TestCompareGoAndNodeSign runs the same body through both Go WASM and Node.js WASM
// and compares the nonce format and sign format.
func TestCompareGoAndNodeSign(t *testing.T) {
	requireIntegrationTest(t)
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".maclaw", "freeproxy")

	// Go WASM sign
	signer := NewWasmSigner(cacheDir)
	ctx := context.Background()
	defer signer.Close(ctx)
	if err := signer.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}

	body := `{"stream":true,"botCode":"AI_SEARCH","conversationId":"12345","question":"hi","agentId":""}`
	goSR, err := signer.Sign(ctx, body, "/chatApi/v2/chat")
	if err != nil {
		t.Fatalf("Go Sign: %v", err)
	}
	t.Logf("Go:   sign=%s nonce=%s ts=%d", goSR.Sign, goSR.Nonce, goSR.Timestamp)
	t.Logf("Go:   sign_len=%d nonce_len=%d nonce_chars=%v", len(goSR.Sign), len(goSR.Nonce), analyzeChars(goSR.Nonce))

	// Node.js WASM sign
	script := fmt.Sprintf(`
import{readFileSync}from'fs';import{join}from'path';import{homedir}from'os';
const wp=join(homedir(),'.maclaw','freeproxy','sign_bg.wasm');
let w,V=0;const e=new TextEncoder('utf-8');
function gs(p,l){return new TextDecoder('utf-8',{ignoreBOM:true,fatal:true}).decode(new Uint8Array(w.memory.buffer,p,l))}
function ps(s,m,r){if(!r){const b=e.encode(s);const p=m(b.length,1)>>>0;new Uint8Array(w.memory.buffer).subarray(p,p+b.length).set(b);V=b.length;return p}let l=s.length,p=m(l,1)>>>0;const mem=new Uint8Array(w.memory.buffer);let o=0;for(;o<l;o++){const c=s.charCodeAt(o);if(c>127)break;mem[p+o]=c}if(o!==l){if(o!==0)s=s.slice(o);p=r(p,l,l=o+s.length*3,1)>>>0;const v=new Uint8Array(w.memory.buffer).subarray(p+o,p+l);const ret=e.encodeInto(s,v);o+=ret.written;p=r(p,l,o,1)>>>0}V=o;return p}
function at(o){const i=w.__externref_table_alloc();w.__wbindgen_export_2.set(i,o);return i}
function c(e){return e==null}
function he(f,a){try{return f.apply(this,a)}catch(e){w.__wbindgen_exn_store(at(e))}}
const imp={wbg:{__wbg_buffer_609cc3eee51ed158:e=>e.buffer,__wbg_call_672a4d21634d4a24:function(){return he(function(e,t){return e.call(t)},arguments)},__wbg_call_7cccdd69e0791ae2:function(){return he(function(e,t,n){return e.call(t,n)},arguments)},__wbg_crypto_ed58b8e10a292839:e=>e.crypto,__wbg_document_d249400bd7bd996d:e=>{const t=e.document;return c(t)?0:at(t)},__wbg_getElementById_f827f0d6648718a8:(e,t,n)=>{const r=e.getElementById(gs(t,n));return c(r)?0:at(r)},__wbg_getRandomValues_bcb4912f16000dc4:function(){return he(function(e,t){e.getRandomValues(t)},arguments)},__wbg_getTime_46267b1c24877e30:e=>e.getTime(),__wbg_instanceof_Window_def73ea0955fc569:e=>{let t;try{t=e instanceof globalThis.Window}catch{t=false}return t},__wbg_msCrypto_0a36e2ec3a343d26:e=>e.msCrypto,__wbg_new0_f788a2397c7ca929:()=>new Date(),__wbg_new_405e22f390576ce2:()=>({}),__wbg_new_5e0be73521bc8c17:()=>new Map(),__wbg_new_78feb108b6472713:()=>[],__wbg_new_a12002a7f91c75be:e=>new Uint8Array(e),__wbg_newnoargs_105ed471475aaf50:(e,t)=>new Function(gs(e,t)),__wbg_newwithbyteoffsetandlength_d97e637ebe145a9a:(e,t,n)=>new Uint8Array(e,t>>>0,n>>>0),__wbg_newwithlength_a381634e90c276d4:e=>new Uint8Array(e>>>0),__wbg_node_02999533c4ea02e3:e=>e.node,__wbg_process_5c1d670bc53614b8:e=>e.process,__wbg_randomFillSync_ab2cfe79ebbf2740:function(){return he(function(e,t){e.randomFillSync(t)},arguments)},__wbg_require_79b1e9274cde3c87:function(){return he(function(){return module.require},arguments)},__wbg_set_37837023f3d740e8:(e,t,n)=>{e[t>>>0]=n},__wbg_set_3f1d0b984ed272ed:(e,t,n)=>{e[t]=n},__wbg_set_65595bdd868b3009:(e,t,n)=>{e.set(t,n>>>0)},__wbg_set_8fc6bf8a5b1071d1:(e,t,n)=>e.set(t,n),__wbg_static_accessor_GLOBAL_88a902d13a557d07:()=>{const e=typeof global==='undefined'?null:global;return c(e)?0:at(e)},__wbg_static_accessor_GLOBAL_THIS_56578be7e9f832b0:()=>{const e=typeof globalThis==='undefined'?null:globalThis;return c(e)?0:at(e)},__wbg_static_accessor_SELF_37c5d418e4bf5819:()=>{const e=typeof self==='undefined'?null:self;return c(e)?0:at(e)},__wbg_static_accessor_WINDOW_5de37043a91a9c40:()=>{const e=typeof window==='undefined'?null:window;return c(e)?0:at(e)},__wbg_subarray_aa9065fa9dc5df96:(e,t,n)=>e.subarray(t>>>0,n>>>0),__wbg_versions_c71aa1626a93e0a1:e=>e.versions,__wbindgen_bigint_from_i64:e=>e,__wbindgen_bigint_from_u64:e=>BigInt.asUintN(64,e),__wbindgen_debug_string:(e,t)=>{const s=String(w.__wbindgen_export_2.get(t));const p=ps(s,w.__wbindgen_malloc,w.__wbindgen_realloc);const d=new DataView(w.memory.buffer);d.setInt32(e+4,V,true);d.setInt32(e+0,p,true)},__wbindgen_error_new:(e,t)=>new Error(gs(e,t)),__wbindgen_init_externref_table:()=>{const t=w.__wbindgen_export_2;const o=t.grow(4);t.set(0,undefined);t.set(o+0,undefined);t.set(o+1,null);t.set(o+2,true);t.set(o+3,false)},__wbindgen_is_function:e=>typeof e==='function',__wbindgen_is_object:e=>typeof e==='object'&&e!==null,__wbindgen_is_string:e=>typeof e==='string',__wbindgen_is_undefined:e=>e===undefined,__wbindgen_memory:()=>w.memory,__wbindgen_number_new:e=>e,__wbindgen_string_new:(e,t)=>gs(e,t),__wbindgen_throw:(e,t)=>{throw new Error(gs(e,t))}}};
const wb=readFileSync(wp);const{instance}=await WebAssembly.instantiate(wb,imp);w=instance.exports;w.__wbindgen_start();
const bp=ps(%q,w.__wbindgen_malloc,w.__wbindgen_realloc);const bl=V;
const up=ps('/chatApi/v2/chat',w.__wbindgen_malloc,w.__wbindgen_realloc);const ul=V;
const m=w.get_sign(bp,bl,up,ul);
console.log(JSON.stringify({sign:m.get('sign'),nonce:m.get('nonce'),timestamp:m.get('timestamp')}));
`, body)

	tmpFile, _ := os.CreateTemp("", "node-sign-*.mjs")
	tmpFile.WriteString(script)
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	out, err2 := exec.Command("node", tmpFile.Name()).CombinedOutput()
	if err2 != nil {
		t.Fatalf("Node sign: %v\n%s", err2, string(out))
	}
	t.Logf("Node: %s", string(out))
}

func analyzeChars(s string) string {
	hasUpper := false
	hasLower := false
	hasDigit := false
	hasSpecial := false
	for _, c := range s {
		switch {
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		case c >= 'a' && c <= 'z':
			hasLower = true
		case c >= '0' && c <= '9':
			hasDigit = true
		default:
			hasSpecial = true
		}
	}
	return fmt.Sprintf("upper=%v lower=%v digit=%v special=%v", hasUpper, hasLower, hasDigit, hasSpecial)
}
