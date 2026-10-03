package buildverify

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tinytex"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const runTimeout = 10 * time.Minute

var (
	// ErrUnrecognised means the directory is not one of the reviewed project kinds.
	ErrUnrecognised = errors.New("build_verify_project_unrecognised")
	// ErrUnsupported means the kind has no reviewed program for this task.
	ErrUnsupported = errors.New("build_verify_task_unsupported")
	// ErrTimeout means the reviewed program did not finish inside the host budget.
	ErrTimeout = errors.New("build_verify_timeout")
)

// compileLatex is the directory compiler. Tests replace it; production uses
// the reviewed TeX engine and never a command line from the model.
var compileLatex = tinytex.CompileDirectory

// Run executes one reviewed verification task. The model supplies the task
// name and, optionally, a workspace subdirectory. The program, its arguments
// and the files it compiles are chosen here.
func Run(parent context.Context, workspace, runDir, task string) (string, error) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, runTimeout)
	defer cancel()
	kind, ok := tool.BuildVerifyProjectKind(workspace, runDir)
	if !ok {
		return "", ErrUnrecognised
	}
	if kind == "latex" {
		if task != "build" {
			return "", ErrUnsupported
		}
		out, err := compileLatex(ctx, runDir, tinytex.ReviewedCompileOptions(corelib.MaclawDataDir()))
		projected := tool.BuildVerifyProjection(out, "")
		if ctx.Err() == context.DeadlineExceeded {
			return projected, ErrTimeout
		}
		if err != nil {
			return projected, err
		}
		return projected, nil
	}
	argv, ok := tool.BuildVerifyCommand(kind, task)
	if !ok {
		return "", ErrUnsupported
	}
	// Direct execution. There is no shell and no model-supplied argument.
	cmd := tool.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = runDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	out := tool.BuildVerifyProjection(stdout.String(), stderr.String())
	if ctx.Err() == context.DeadlineExceeded {
		return out, ErrTimeout
	}
	// A failing build or test is the answer that was asked for. Reporting it
	// as an adapter error would hide the diagnostics.
	if runErr != nil {
		return strings.TrimSpace(out + "\n" + runErr.Error()), nil
	}
	if strings.TrimSpace(out) == "" {
		return task + " passed", nil
	}
	return out, nil
}
