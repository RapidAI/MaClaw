//go:build ignore

// check_wails_bindings.go — Build-time verification that frontend-referenced
// Wails bindings exist in the generated App.js file and have a matching native
// App method.
//
// Usage (from project root):
//   go run scripts/check_wails_bindings.go
//
// Strategy: scan the FRONTEND source code for dynamic wailsApp references and
// verify each one exists in App.js. Then compare every App.js export with the
// native *App methods in guiapp. That second check catches stale generated wrappers
// that would otherwise resolve to undefined at runtime.
//
// This catches the exact class of bug that caused the "功能不可用" error:
// frontend code references a Go binding that doesn't exist in the generated file.

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Patterns that indicate the frontend is trying to call a Wails binding dynamically:
//
//	(wailsApp as any).MethodName / (mod as any).MethodName
//
// We require the variable name to look like a Wails module reference.
var dynamicCallRe = regexp.MustCompile(`\((?:wailsApp|mod|wailsMod|module)\s+as\s+any\)\.([A-Z]\w+)`)

// Known non-binding names that happen to match the pattern (DOM/TS built-ins)
var falsePositives = map[string]bool{
	"Image": true, "URL": true, "File": true, "Blob": true,
	"FormData": true, "Headers": true, "Request": true, "Response": true,
}

// jsExportRe matches JS binding exports: export function MethodName(...)
var jsExportRe = regexp.MustCompile(`^export function (\w+)\(([^)]*)\)`)
var goAppMethodRe = regexp.MustCompile(`^func \(a \*App\) ([A-Z]\w+)\(`)

// countBindingParams counts formal parameters as top-level comma-separated
// segments. Go parameter groups share this shape: "a, b string" is two
// segments, matching the two Wails-generated JS arguments.
func countBindingParams(list string) int {
	if strings.TrimSpace(list) == "" {
		return 0
	}
	depth := 0
	commas := 0
	for _, r := range list {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				commas++
			}
		}
	}
	return commas + 1
}

// goAppMethodParams scans one Go source file and returns the parameter count
// of each single- or multi-line *App method signature. A leading
// context.Context parameter is excluded: Wails never exposes it to JS.
func goAppMethodParams(path string) (map[string]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	params := map[string]int{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var collecting string
	var name string
	depth := 0
	endIdx := -1
	track := func(text string) {
		for i, r := range text {
			switch r {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 && endIdx < 0 {
					endIdx = len(collecting) + i
				}
			}
		}
		collecting += text
	}
	finish := func() {
		if name == "" {
			return
		}
		// Drop the surrounding parameter parentheses and a leading
		// context.Context parameter, which Wails never exposes to JS.
		list := collecting
		if idx := strings.Index(list, "("); idx >= 0 {
			list = list[idx+1:]
		}
		if endIdx >= 0 && endIdx-1 <= len(list) && endIdx > 0 {
			list = list[:endIdx-1]
		}
		count := countBindingParams(list)
		first := strings.TrimSpace(strings.SplitN(list, ",", 2)[0])
		if strings.HasSuffix(first, "context.Context") {
			count--
		}
		params[name] = count
		name = ""
		collecting = ""
		endIdx = -1
		depth = 0
	}
	for scanner.Scan() {
		line := scanner.Text()
		if name == "" {
			if m := goAppMethodRe.FindStringSubmatch(line); m != nil {
				name = m[1]
				track(line[strings.Index(line, m[1])+len(m[1]):])
				if endIdx >= 0 {
					finish()
				}
			}
			continue
		}
		track("\n" + line)
		if endIdx >= 0 {
			finish()
		}
	}
	return params, scanner.Err()
}

func main() {
	frontendSrcDir := filepath.Join("guiapp", "frontend", "src")
	bindingFile := filepath.Join("guiapp", "frontend", "wailsjs", "go", "main", "App.js")

	// 1. Scan frontend source for dynamic binding references (the risky pattern)
	// These are method calls via `(wailsApp as any).Foo` which bypass TypeScript
	// type checking — if the binding is missing, it's undefined at runtime.
	frontendRefs := map[string]string{} // method name -> first file
	err := filepath.Walk(frontendSrcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".ts" && ext != ".tsx" {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		relPath, _ := filepath.Rel("guiapp/frontend", path)
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			// Skip comments
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
				continue
			}
			for _, match := range dynamicCallRe.FindAllStringSubmatch(line, -1) {
				name := match[1]
				if _, exists := frontendRefs[name]; !exists {
					if !falsePositives[name] {
						frontendRefs[name] = relPath
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: cannot walk frontend src dir: %v\n", err)
		os.Exit(1)
	}

	// 2. Collect all exported functions from the JS binding file
	jsMethods := map[string]bool{}
	jsParams := map[string]int{}
	f, err := os.Open(bindingFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: cannot read binding file %s: %v\n", bindingFile, err)
		os.Exit(1)
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if m := jsExportRe.FindStringSubmatch(line); m != nil {
			jsMethods[m[1]] = true
			jsParams[m[1]] = countBindingParams(m[2])
		}
	}
	f.Close()

	// 2b. Do not infer this from TypeScript declarations: they are generated
	// alongside App.js and cannot expose a stale native binding.
	nativeMethods := map[string]bool{}
	nativeParams := map[string]int{}
	err = filepath.Walk("guiapp", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		fileParams, err := goAppMethodParams(path)
		if err != nil {
			return nil
		}
		for name, count := range fileParams {
			nativeMethods[name] = true
			nativeParams[name] = count
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: cannot walk guiapp Go sources: %v\n", err)
		os.Exit(1)
	}

	// 3. Find frontend dynamic references that are missing from binding file
	var missing []string
	for name, source := range frontendRefs {
		if !jsMethods[name] {
			missing = append(missing, fmt.Sprintf("  %s  (referenced in %s)", name, source))
		}
	}

	for name := range jsMethods {
		if !nativeMethods[name] {
			missing = append(missing, fmt.Sprintf("  %s  (exported by App.js but missing from native *App methods)", name))
		}
	}

	// 4. Arity drift: a hand-edited wrapper that drops a Go parameter passes
	// undefined into the native call at runtime.
	//
	// arityAllowlist documents pre-existing wrappers whose generated argument
	// count never matched the native signature (both are unused dead bindings;
	// verified against the initial guiapp commit 290048ba, where the mismatch
	// already existed). New entries must not be added: fix the wrapper instead.
	arityAllowlist := map[string]bool{
		"DeliverIMText":             true, // wrapper: 4 args, native: ctx + 3 params
		"DeliverIMFromTaskDelivery": true, // wrapper: 3 args, native: ctx + 2 params
	}
	for name, jsCount := range jsParams {
		goCount, ok := nativeParams[name]
		if !ok || arityAllowlist[name] {
			continue
		}
		if jsCount != goCount {
			missing = append(missing, fmt.Sprintf("  %s  (App.js wrapper takes %d arg(s), native method takes %d)", name, jsCount, goCount))
		}
	}

	if len(missing) == 0 {
		fmt.Printf("OK: %d dynamic frontend references and %d generated App.js bindings have native App methods.\n", len(frontendRefs), len(jsMethods))
		return
	}

	sort.Strings(missing)
	fmt.Fprintf(os.Stderr, "BINDING DRIFT: %d method(s) are missing a generated or native binding:\n\n", len(missing))
	for _, m := range missing {
		fmt.Fprintln(os.Stderr, m)
	}
	fmt.Fprintf(os.Stderr, "\nA frontend call needs both a generated App.js export and a native Go *App method.\n")
	fmt.Fprintf(os.Stderr, "Otherwise it will be undefined at runtime. Regenerate bindings or restore the method.\n")
	os.Exit(1)
}
