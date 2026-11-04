package guiapp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/hopjit"
	"github.com/RapidAI/CodeClaw/corelib/hopspec"
)

func (a *App) RunHopTask(task string) (hopjit.ReuseResult, error) {
	store, err := a.openHopJITPersistence()
	if err != nil {
		return hopjit.ReuseResult{}, err
	}
	defer store.Close()
	return runHopTaskWithPersistence(context.Background(), store, task, &guiHopJITCaller{app: a})
}

func runHopTaskWithPersistence(ctx context.Context, store hopjit.PersistenceProvider, task string, caller *guiHopJITCaller) (hopjit.ReuseResult, error) {
	spec, err := compileHopSpec(ctx, task, caller)
	if err != nil {
		return hopjit.ReuseResult{}, err
	}
	return runHopSpecWithPersistence(ctx, store, spec, nil, caller)
}

func compileHopSpec(ctx context.Context, task string, caller *guiHopJITCaller) (string, error) {
	task = strings.TrimSpace(task)
	if task == "" {
		return "", errors.New("HopJIT task is empty")
	}
	if caller == nil {
		return "", errors.New("HopJIT compiler caller is unavailable")
	}
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": "把用户任务编译成 HopSpec v3 Markdown。只返回规约正文，不要代码围栏。必须包含 # Spec、Goal、Outputs、## Steps；优先使用 reason、带 hop_python body 的 act、check 和 exit。不得使用 commit。所有步骤编号连续，输出必须有类型，关键结果必须有 check。不要声明需要调用方提供的 Inputs。"},
		map[string]interface{}{"role": "user", "content": task},
	}
	text, err := caller.call(ctx, messages, false)
	if err != nil {
		return "", err
	}
	spec := stripHopJITCodeFence(text)
	if err := validateCompiledHopSpec(spec); err != nil {
		return "", err
	}
	return spec, nil
}

func validateCompiledHopSpec(markdown string) error {
	spec, parseErrs := hopspec.Parse(markdown)
	if len(parseErrs) > 0 {
		return errors.Join(parseErrs...)
	}
	if validationErrs := hopspec.Validate(spec); len(validationErrs) > 0 {
		return errors.Join(validationErrs...)
	}
	if len(spec.Steps) == 0 {
		return errors.New("compiled HopSpec contains no steps")
	}
	for _, step := range spec.AllSteps() {
		if step.Type == hopspec.StepCommit {
			return fmt.Errorf("compiled HopSpec commit is not allowed: step %s", step.ID)
		}
	}
	return nil
}
