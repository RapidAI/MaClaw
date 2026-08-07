// onnxdump is a debug CLI that prints a structural summary of an ONNX model:
// ir_version, opset, graph inputs/outputs with shapes, initializer stats,
// node count, and a per-op-type histogram including the union of attribute
// names seen for each op.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/onnxrt"
)

var (
	runFlag     = flag.Bool("run", false, "run a timed dummy forward pass after the dump")
	shapeFlag   = flag.String("shape", "", "input shape for -run, e.g. 1,3,736,736 (default: declared shape, dynamic dims = 64)")
	itersFlag   = flag.Int("iters", 3, "iterations for -run timing")
	profileFlag = flag.String("cpuprofile", "", "write CPU profile of -run iterations to this file")
	quietFlag   = flag.Bool("q", false, "skip the structural dump (with -run, only print run output)")
)

func shapeString(vi onnxrt.ValueInfo) string {
	parts := make([]string, len(vi.Shape))
	for i, d := range vi.Shape {
		if d.Param != "" {
			parts[i] = d.Param
		} else if d.Value < 0 {
			parts[i] = "?"
		} else {
			parts[i] = fmt.Sprintf("%d", d.Value)
		}
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func dimIsDynamic(d onnxrt.Dim) bool { return d.Param != "" || d.Value < 0 }

func main() {
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: onnxdump [-run] [-shape 1,3,736,736] [-iters N] <model.onnx>")
		os.Exit(2)
	}
	modelPath := flag.Arg(0)
	data, err := os.ReadFile(modelPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		os.Exit(1)
	}
	m, err := onnxrt.ParseModel(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		os.Exit(1)
	}
	g := m.Graph

	if *quietFlag {
		if *runFlag {
			runForward(modelPath, m)
		}
		return
	}

	fmt.Printf("file: %s (%d bytes)\n", modelPath, len(data))
	fmt.Printf("ir_version: %d\n", m.IRVersion)
	fmt.Printf("opset: %d\n", m.Opset)
	fmt.Printf("graph name: %q\n", g.Name)

	fmt.Println("graph inputs:")
	for _, vi := range g.Inputs {
		if _, isInit := g.Initializers[vi.Name]; isInit {
			continue // weights listed as inputs (old IR convention)
		}
		fmt.Printf("  %s: %s %s\n", vi.Name, vi.ElemType, shapeString(vi))
	}
	fmt.Println("graph outputs:")
	for _, vi := range g.Outputs {
		fmt.Printf("  %s: %s %s\n", vi.Name, vi.ElemType, shapeString(vi))
	}

	// Dynamic vs static dims of the real inputs.
	dyn, stat := 0, 0
	for _, vi := range g.Inputs {
		if _, isInit := g.Initializers[vi.Name]; isInit {
			continue
		}
		for _, d := range vi.Shape {
			if dimIsDynamic(d) {
				dyn++
			} else {
				stat++
			}
		}
	}
	fmt.Printf("input dims: %d dynamic, %d static\n", dyn, stat)

	// Initializer stats.
	var totalFloats, totalElems int64
	typeCount := map[onnxrt.TensorDataType]int{}
	var external []string
	for _, t := range g.Initializers {
		totalElems += t.NumElements()
		if t.DataType == onnxrt.TypeFloat {
			totalFloats += t.NumElements()
		}
		typeCount[t.DataType]++
		if t.DataLocation == 1 {
			external = append(external, t.Name)
		}
	}
	fmt.Printf("initializers: %d tensors, %d total elements (%d float)\n",
		len(g.Initializers), totalElems, totalFloats)
	var types []string
	for dt, c := range typeCount {
		types = append(types, fmt.Sprintf("%s:%d", dt, c))
	}
	sort.Strings(types)
	fmt.Printf("initializer dtypes: %s\n", strings.Join(types, " "))
	if len(external) > 0 {
		sort.Strings(external)
		fmt.Printf("WARNING external-data initializers: %v\n", external)
	}

	fmt.Printf("nodes: %d\n", len(g.Nodes))

	// Op histogram + attribute-name union per op type.
	hist := map[string]int{}
	attrs := map[string]map[string]bool{}
	for _, n := range g.Nodes {
		hist[n.OpType]++
		if attrs[n.OpType] == nil {
			attrs[n.OpType] = map[string]bool{}
		}
		for name := range n.Attrs {
			attrs[n.OpType][name] = true
		}
	}
	ops := make([]string, 0, len(hist))
	for op := range hist {
		ops = append(ops, op)
	}
	sort.Slice(ops, func(i, j int) bool {
		if hist[ops[i]] != hist[ops[j]] {
			return hist[ops[i]] > hist[ops[j]] // most frequent first
		}
		return ops[i] < ops[j]
	})
	fmt.Println("op histogram:")
	for _, op := range ops {
		names := make([]string, 0, len(attrs[op]))
		for name := range attrs[op] {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Printf("  %-20s %4d   attrs: {%s}\n", op, hist[op], strings.Join(names, ", "))
	}

	if *runFlag {
		runForward(modelPath, m)
	}
}

// runForward executes a timed dummy forward pass with deterministic input.
func runForward(path string, m *onnxrt.Model) {
	if *profileFlag != "" {
		f, err := os.Create(*profileFlag)
		if err != nil {
			fmt.Fprintln(os.Stderr, "cpuprofile:", err)
			os.Exit(1)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintln(os.Stderr, "cpuprofile:", err)
			os.Exit(1)
		}
		defer func() {
			pprof.StopCPUProfile()
			f.Close()
		}()
	}
	graph, err := onnxrt.NewGraph(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "prepare:", err)
		os.Exit(1)
	}
	inputs := map[string]*onnxrt.Tensor{}
	var override []int
	if *shapeFlag != "" {
		for _, s := range strings.Split(*shapeFlag, ",") {
			v, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil || v <= 0 {
				fmt.Fprintln(os.Stderr, "bad -shape:", *shapeFlag)
				os.Exit(2)
			}
			override = append(override, v)
		}
	}
	for _, name := range graph.InputNames() {
		var shape []int
		if override != nil {
			shape = override
		} else {
			// use declared shape; dynamic dims default to 64
			for _, vi := range m.Graph.Inputs {
				if vi.Name != name {
					continue
				}
				for _, d := range vi.Shape {
					if d.Value >= 0 && d.Param == "" {
						shape = append(shape, int(d.Value))
					} else {
						shape = append(shape, 64)
					}
				}
			}
		}
		t := onnxrt.NewFloat(shape...)
		// deterministic pseudo-random input in [-1, 1]
		var state uint32 = 12345
		for i := range t.F32 {
			state = state*1664525 + 1013904223
			t.F32[i] = float32(state>>8)/float32(1<<24)*2 - 1
		}
		inputs[name] = t
		fmt.Printf("run input %s shape %v\n", name, shape)
	}
	for it := 0; it < *itersFlag; it++ {
		start := time.Now()
		outs, err := graph.Run(inputs)
		if err != nil {
			fmt.Fprintln(os.Stderr, "run:", err)
			os.Exit(1)
		}
		el := time.Since(start)
		for _, name := range graph.OutputNames() {
			t := outs[name]
			finite := true
			maxAbs := float32(0)
			for _, v := range t.F32 {
				if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
					finite = false
					break
				}
				if a := float32(math.Abs(float64(v))); a > maxAbs {
					maxAbs = a
				}
			}
			fmt.Printf("iter %d: %v  output %s shape %v finite=%v maxAbs=%g\n", it, el, name, t.Shape, finite, maxAbs)
		}
	}
}
