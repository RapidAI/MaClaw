//go:build ignore

// check_wails_bindings.go — Build-time verification that frontend-referenced
// Wails bindings exist in the generated App.js file.
//
// Usage (from project root):
//   go run scripts/check_wails_bindings.go
//
// Strategy: Instead of scanning ALL exported Go methods (which includes many
// internal-only methods), this script scans the FRONTEND source code for
// references to wailsApp methods, then verifies each one exists in App.js.
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
//   (wailsApp as any).MethodName / (mod as any).MethodName
// We require the variable name to look like a Wails module reference.
var dynamicCallRe = regexp.MustCompile(`\((?:wailsApp|mod|wailsMod|module)\s+as\s+any\)\.([A-Z]\w+)`)

// Known non-binding names that happen to match the pattern (DOM/TS built-ins)
var falsePositives = map[string]bool{
	"Image": true, "URL": true, "File": true, "Blob": true,
	"FormData": true, "Headers": true, "Request": true, "Response": true,
}

// jsExportRe matches JS binding exports: export function MethodName(...)
var jsExportRe = regexp.MustCompile(`^export function (\w+)\(`)

func main() {
	frontendSrcDir := filepath.Join("gui", "frontend", "src")
	bindingFile := filepath.Join("gui", "frontend", "wailsjs", "go", "main", "App.js")

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

		relPath, _ := filepath.Rel("gui/frontend", path)
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
		}
	}
	f.Close()

	// 3. Find frontend dynamic references that are missing from binding file
	var missing []string
	for name, source := range frontendRefs {
		if !jsMethods[name] {
			missing = append(missing, fmt.Sprintf("  %s  (referenced in %s)", name, source))
		}
	}

	if len(missing) == 0 {
		fmt.Printf("OK: all %d dynamically-referenced bindings exist in App.js.\n", len(frontendRefs))
		os.Exit(0)
	}

	sort.Strings(missing)
	fmt.Fprintf(os.Stderr, "BINDING DRIFT: %d dynamically-referenced method(s) missing from %s:\n\n",
		len(missing), bindingFile)
	for _, m := range missing {
		fmt.Fprintln(os.Stderr, m)
	}
	fmt.Fprintf(os.Stderr, "\nThese methods are called via (mod as any).X in frontend code but\n")
	fmt.Fprintf(os.Stderr, "have no export in the generated binding file. They will be undefined at runtime.\n")
	fmt.Fprintf(os.Stderr, "\nFix: add the missing bindings to App.js and App.d.ts.\n")
	os.Exit(1)
}
