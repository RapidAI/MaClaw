package hopspec

import (
	"fmt"
	"sort"
	"strings"
)

func Format(spec SpecAST) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Spec: %s\n", spec.Title)
	if strings.TrimSpace(spec.ID) != "" {
		fmt.Fprintf(&b, "Id: %s\n", spec.ID)
	}
	fmt.Fprintf(&b, "Goal: %s\n", spec.Goal)
	formatStringList(&b, "Constraints", spec.Constraints)
	formatDeclarations(&b, "Inputs", spec.Inputs)
	formatDeclarations(&b, "Outputs", spec.Outputs)
	b.WriteString("## Steps\n")
	formatSteps(&b, spec.Steps, 0)
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func formatStringList(b *strings.Builder, title string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n", title)
	for _, value := range values {
		fmt.Fprintf(b, "- %s\n", value)
	}
}

func formatDeclarations(b *strings.Builder, title string, declarations []VariableDecl) {
	if len(declarations) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n", title)
	for _, declaration := range declarations {
		fmt.Fprintf(b, "- %s %% %s", declaration.Name, declaration.Type)
		if declaration.Description != "" {
			fmt.Fprintf(b, "  # %s", declaration.Description)
		}
		b.WriteByte('\n')
	}
}

func formatSteps(b *strings.Builder, steps []*Step, depth int) {
	for _, step := range steps {
		indent := strings.Repeat("  ", depth)
		descriptor := formatStepDescriptor(step)
		fmt.Fprintf(b, "%s%s. [%s]", indent, step.ID, descriptor)
		if step.Summary != "" {
			fmt.Fprintf(b, " %s", step.Summary)
		}
		b.WriteByte('\n')
		fieldIndent := indent + "  "
		if len(step.Inputs) > 0 {
			fmt.Fprintf(b, "%s- ← %s\n", fieldIndent, strings.Join(step.Inputs, ", "))
		}
		if len(step.Tools) > 0 {
			fmt.Fprintf(b, "%s- Tools: %s\n", fieldIndent, strings.Join(step.Tools, ", "))
		}
		for _, output := range step.Outputs {
			if step.Type == StepCall && step.Call != nil {
				if callee, ok := step.Call.Outputs[output.Name]; ok {
					fmt.Fprintf(b, "%s+ → %s: %s\n", fieldIndent, output.Name, callee)
					continue
				}
			}
			fmt.Fprintf(b, "%s+ → %s %% %s", fieldIndent, output.Name, output.Type)
			if output.Description != "" {
				fmt.Fprintf(b, "  # %s", output.Description)
			}
			b.WriteByte('\n')
		}
		formatInstruction(b, fieldIndent, step.Instruction)
		if step.Body != "" {
			fmt.Fprintf(b, "%s> ```hop_python\n", fieldIndent)
			for _, line := range strings.Split(strings.ReplaceAll(step.Body, "\r\n", "\n"), "\n") {
				fmt.Fprintf(b, "%s> %s\n", fieldIndent, line)
			}
			fmt.Fprintf(b, "%s> ```\n", fieldIndent)
		}
		formatSteps(b, step.Children, depth+1)
	}
}

func formatStepDescriptor(step *Step) string {
	var fields []string
	switch step.Type {
	case StepCall:
		if step.Call != nil {
			call := strings.TrimSpace(step.Call.Callee)
			if len(step.Call.Inputs) > 0 {
				keys := make([]string, 0, len(step.Call.Inputs))
				for target := range step.Call.Inputs {
					keys = append(keys, target)
				}
				sort.Strings(keys)
				mappings := make([]string, 0, len(keys))
				for _, target := range keys {
					mappings = append(mappings, target+": "+step.Call.Inputs[target])
				}
				call += "(" + strings.Join(mappings, ", ") + ")"
			}
			fields = append(fields, call)
		}
	case StepLoop:
		if step.ForEach != nil {
			descriptor := "for-each " + step.ForEach.Item + " in " + step.ForEach.List
			if step.ForEach.CollectItem != "" && step.ForEach.CollectInto != "" {
				descriptor += ", collect " + step.ForEach.CollectItem + " into " + step.ForEach.CollectInto
			}
			fields = append(fields, descriptor)
		}
		if step.MaxIterations > 0 {
			fields = append(fields, fmt.Sprintf("max=%d", step.MaxIterations))
		}
	}
	if step.Retry != nil {
		fields = append(fields, fmt.Sprintf("retry=%d", *step.Retry))
	}
	if step.Adaptive {
		fields = append(fields, "adaptive")
	}
	if step.Parallel {
		fields = append(fields, "parallel")
	}
	for _, name := range []string{"free", "final", "require_human"} {
		if value, ok := step.Attributes[name]; ok {
			if value == "true" {
				fields = append(fields, name)
			} else {
				fields = append(fields, name+"="+value)
			}
		}
	}
	descriptor := string(step.Type)
	if step.Type == StepCase {
		descriptor += "(" + step.Condition + ")"
	}
	if len(fields) > 0 {
		descriptor += " " + strings.Join(fields, " ")
	}
	return descriptor
}

func formatInstruction(b *strings.Builder, indent, instruction string) {
	if instruction == "" {
		return
	}
	for _, line := range strings.Split(strings.ReplaceAll(instruction, "\r\n", "\n"), "\n") {
		fmt.Fprintf(b, "%s> %s\n", indent, line)
	}
}
