package hopspec

import "strings"

func Validate(spec SpecAST) []error {
	var errs []error
	add := func(code, message string, step Step) {
		errs = append(errs, Diagnostic{Code: code, Line: step.Source.LineStart, Message: message})
	}
	if strings.TrimSpace(spec.Goal) == "" {
		errs = append(errs, Diagnostic{Code: "MISSING_GOAL", Message: "Goal is required"})
	}
	declared := map[string]VariableDecl{}
	validateDeclarations := func(decls []VariableDecl, code string) {
		for _, decl := range decls {
			if decl.Name == "" || decl.Type == "" {
				errs = append(errs, Diagnostic{Code: code, Message: "variable name and type are required"})
				continue
			}
			if _, exists := declared[decl.Name]; exists {
				errs = append(errs, Diagnostic{Code: "DUPLICATE_VARIABLE", Message: "variable " + decl.Name + " is declared more than once"})
				continue
			}
			declared[decl.Name] = decl
		}
	}
	validateDeclarations(spec.Inputs, "INVALID_INPUT_DECL")
	validateDeclarations(spec.Outputs, "INVALID_OUTPUT_DECL")
	produced := map[string]bool{}
	bareExit := false
	var walk func([]*Step, *Step, int)
	walk = func(steps []*Step, parent *Step, loopDepth int) {
		for _, step := range steps {
			if step.Parallel && step.Type != StepCall && step.Type != StepSubtask && step.Type != StepCase {
				add("PARALLEL_STEP_TYPE", "parallel "+step.ID+" must be a call, subtask, or case", *step)
			}
			if step.Type == StepLoop {
				if step.ForEach == nil && step.MaxIterations == 0 {
					add("LOOP_BOUND_REQUIRED", "loop "+step.ID+" requires max or for-each", *step)
				}
				if step.ForEach != nil {
					list, ok := declared[step.ForEach.List]
					if !ok {
						add("UNKNOWN_INPUT", "loop "+step.ID+" reads undeclared variable "+step.ForEach.List, *step)
					} else {
						itemType := strings.TrimSuffix(strings.TrimPrefix(list.Type, "["), "]")
						if !strings.HasPrefix(list.Type, "[") || !strings.HasSuffix(list.Type, "]") {
							add("FOR_EACH_LIST_TYPE", "loop "+step.ID+" requires a [T] list variable", *step)
						}
						if _, exists := declared[step.ForEach.Item]; !exists {
							declared[step.ForEach.Item] = VariableDecl{Name: step.ForEach.Item, Type: itemType}
						}
					}
				}
			}
			if step.Type == StepBranch {
				for _, child := range step.Children {
					if child.Type != StepCase {
						add("BRANCH_CASE_REQUIRED", "branch "+step.ID+" may contain only case steps", *child)
					}
				}
			}
			if step.Type == StepCase && (parent == nil || parent.Type != StepBranch) {
				add("CASE_OUTSIDE_BRANCH", "case "+step.ID+" must belong to a branch", *step)
			}
			if (step.Type == StepBreak || step.Type == StepContinue) && loopDepth == 0 {
				add("LOOP_CONTROL_OUTSIDE_LOOP", "step "+step.ID+" must be inside a loop", *step)
			}
			if step.Type == StepOnFail && (parent == nil || parent.Type != StepSubtask && parent.Type != StepCase) {
				add("ON_FAIL_OUTSIDE_CONTAINER", "on_fail "+step.ID+" must belong to subtask or case", *step)
			}
			if step.Type == StepCall {
				if step.Call == nil || strings.TrimSpace(step.Call.Callee) == "" {
					add("CALL_TARGET_REQUIRED", "call "+step.ID+" requires a callee", *step)
				} else {
					for _, source := range step.Call.Inputs {
						if _, ok := declared[source]; !ok {
							add("UNKNOWN_INPUT", "call "+step.ID+" maps undeclared variable "+source, *step)
						}
					}
				}
			}
			for _, input := range step.Inputs {
				if _, ok := declared[input]; !ok {
					add("UNKNOWN_INPUT", "step "+step.ID+" reads undeclared variable "+input, *step)
				}
			}
			for _, output := range step.Outputs {
				if output.Name == "" {
					add("INVALID_OUTPUT_DECL", "step "+step.ID+" has an output without a name", *step)
					continue
				}
				if step.Type == StepExit && output.Name == "none" {
					continue
				}
				produced[output.Name] = true
				if prior, exists := declared[output.Name]; exists {
					if output.Type != "" && output.Type != "any" && prior.Type != "any" && prior.Type != output.Type {
						add("VARIABLE_TYPE_CONFLICT", "variable "+output.Name+" changes type from "+prior.Type+" to "+output.Type, *step)
					}
				} else {
					if output.Type == "" {
						add("MISSING_OUTPUT_TYPE", "first declaration of "+output.Name+" requires a type", *step)
					}
					declared[output.Name] = output
				}
			}
			if step.Body != "" && step.Type != StepAct && step.Type != StepCheck && step.Type != StepCommit {
				add("BODY_NOT_ALLOWED", "step "+step.ID+" cannot contain hop_python body", *step)
			}
			if step.Type == StepCommit && strings.TrimSpace(step.Body) == "" {
				add("COMMIT_BODY_REQUIRED", "commit "+step.ID+" requires hop_python body", *step)
			}
			if step.Type == StepCheck || step.Type == StepConfirm {
				hasBool := false
				for _, output := range step.Outputs {
					if output.Type == "bool" {
						hasBool = true
					}
				}
				if !hasBool {
					code := "CHECK_BOOL_REQUIRED"
					if step.Type == StepConfirm {
						code = "CONFIRM_BOOL_REQUIRED"
					}
					add(code, "step "+step.ID+" requires a bool verdict output", *step)
				}
			}
			if step.Type == StepExit && len(step.Outputs) == 0 {
				bareExit = true
			}
			childLoopDepth := loopDepth
			if step.Type == StepLoop {
				childLoopDepth++
			}
			walk(step.Children, step, childLoopDepth)
		}
	}
	walk(spec.Steps, nil, 0)
	if len(spec.Steps) > 0 && !bareExit {
		for _, output := range spec.Outputs {
			if !produced[output.Name] {
				errs = append(errs, Diagnostic{Code: "OUTPUT_WITHOUT_PRODUCER", Message: "declared output " + output.Name + " has no producing step"})
			}
		}
	}
	return errs
}
