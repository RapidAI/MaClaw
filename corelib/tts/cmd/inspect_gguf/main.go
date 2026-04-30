// Quick tool to inspect GGUF tensor names for MeloTTS weight mapping.
package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: inspect_gguf <path.gguf>")
		os.Exit(1)
	}
	mf, err := gguf.OpenMmap(os.Args[1])
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	defer mf.CloseMmap()

	fmt.Printf("=== Metadata (%d keys) ===\n", len(mf.Meta))
	for k, v := range mf.Meta {
		switch {
		case v.Str != "":
			fmt.Printf("  %s = %q\n", k, v.Str)
		case v.F32 != 0:
			fmt.Printf("  %s = %f\n", k, v.F32)
		default:
			fmt.Printf("  %s = %d (u32=%d, i32=%d)\n", k, v.U32, v.U32, v.I32)
		}
	}

	fmt.Printf("\n=== Tensors (%d) ===\n", len(mf.Tensors))
	names := make([]string, 0, len(mf.Tensors))
	for name := range mf.Tensors {
		names = append(names, name)
	}
	sort.Strings(names)

	// Group by prefix
	prefixCount := map[string]int{}
	for _, name := range names {
		prefix := name
		for i, c := range name {
			if c == '.' {
				prefix = name[:i]
				break
			}
		}
		prefixCount[prefix]++
	}

	fmt.Println("\n--- By prefix ---")
	prefixes := make([]string, 0, len(prefixCount))
	for p := range prefixCount {
		prefixes = append(prefixes, p)
	}
	sort.Strings(prefixes)
	for _, p := range prefixes {
		fmt.Printf("  %s.*  (%d tensors)\n", p, prefixCount[p])
	}

	// Print first 100 tensor names with shapes
	fmt.Println("\n--- First 100 tensors ---")
	for i, name := range names {
		if i >= 800 {
			fmt.Printf("  ... and %d more\n", len(names)-100)
			break
		}
		ti := mf.Tensors[name]
		dims := make([]uint64, ti.NDims)
		for j := uint32(0); j < ti.NDims; j++ {
			dims[j] = ti.Dims[j]
		}
		typeName := "f32"
		switch ti.Type {
		case gguf.TypeF16:
			typeName = "f16"
		case gguf.TypeQ8_0:
			typeName = "q8_0"
		case gguf.TypeQ4_0:
			typeName = "q4_0"
		}
		fmt.Printf("  %-60s %v  %s\n", name, dims, typeName)
	}
}
