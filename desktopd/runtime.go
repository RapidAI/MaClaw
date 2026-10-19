package desktopd

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// Exec kinds are the container operations the control plane asks a runtime
// to run. They stay docker exec on DockerRuntime.
const (
	ExecFile  = "file"
	ExecShell = "shell"
	ExecShot  = "shot"
	ExecClick = "click"
	ExecCtl   = "ctl"
)

// ExecRequest is one docker exec. Args begin with "exec" and name this
// user's container. Combined matches docker's own exit (click, screenshot,
// ctl). File and shell keep the command exit code on ExecResult.
type ExecRequest struct {
	Kind     string
	Args     []string
	Stdin    io.Reader
	Combined bool
}

// ExecResult is the text of one exec. Exit is the command status when
// Combined is false. A transport failure is the error, not Exit.
type ExecResult struct {
	Out  string
	Exit int
}

// RestoreResult is a container brought back to a running supervisor.
// EnsureOut is the supervisor ensure line. The gate token in that line is
// not written to meta.json.
type RestoreResult struct {
	Container string
	EnsureOut string
}

// RuntimeStatus is the machine the control plane can see. DockerRuntime
// reports running or absent. Sleeping and waking belong to a later runtime.
type RuntimeStatus struct {
	State   string      `json:"state"`
	Runtime string      `json:"runtime"`
	Screens []ScreenRow `json:"screens,omitempty"`
}

// Runtime is the control plane's machine seam. Hub's HTTP shape does not
// change. DockerRuntime wraps today's container calls. It is not Firecracker.
type Runtime interface {
	Ensure(ctx context.Context, spec Spec) error
	Exec(ctx context.Context, spec Spec, req ExecRequest) (ExecResult, error)
	Snapshot(ctx context.Context, spec Spec) error
	Restore(ctx context.Context, spec Spec) (RestoreResult, error)
	Stop(ctx context.Context, spec Spec) (Desktop, error)
	Status(ctx context.Context, spec Spec) (RuntimeStatus, error)
}

// DockerRuntime wraps the container calls this process already makes.
// Ensure starts a stopped container and does not create a missing one.
// Restore is create plus the supervisor ensure. Snapshot flushes and syncs
// and leaves the container. Stop is the existing stop. Exec is docker exec.
type DockerRuntime struct {
	s *Service
}

func (s *Service) runtime() Runtime {
	if s != nil && s.Machines != nil {
		return s.Machines
	}
	return DockerRuntime{s: s}
}

// Ensure is today's wake. A missing container stays missing so a file or
// bash call does not create one. Session open uses Restore.
func (d DockerRuntime) Ensure(ctx context.Context, spec Spec) error {
	return d.s.ensureUnlocked(ctx, spec)
}

// Exec runs one docker exec on this user's container. run, rm, and commit
// are not exec.
func (d DockerRuntime) Exec(ctx context.Context, spec Spec, req ExecRequest) (ExecResult, error) {
	if err := execAllowed(spec, req); err != nil {
		return ExecResult{}, err
	}
	if req.Combined {
		out, err := d.s.docker(ctx, req.Args...)
		return ExecResult{Out: out}, err
	}
	out, code, err := d.s.runCommand(ctx, req.Stdin, req.Args...)
	return ExecResult{Out: out, Exit: code}, err
}

func execAllowed(spec Spec, req ExecRequest) error {
	switch req.Kind {
	case ExecFile, ExecShell, ExecShot, ExecClick, ExecCtl:
	default:
		return fmt.Errorf("%w: runtime exec kind is unknown", ErrInvalid)
	}
	if len(req.Args) == 0 || req.Args[0] != "exec" {
		return fmt.Errorf("%w: runtime exec is a docker exec", ErrInvalid)
	}
	name := containerName(spec.TenantID, spec.UserID)
	if name == "" {
		return fmt.Errorf("%w: user is required", ErrInvalid)
	}
	for _, arg := range req.Args {
		if arg == name {
			return nil
		}
	}
	return fmt.Errorf("%w: runtime exec stays on this container", ErrInvalid)
}

// Snapshot flushes the browser and syncs the four volumes. The container
// stays. Commit runs only inside create, and only when that path is about
// to delete the container.
func (d DockerRuntime) Snapshot(ctx context.Context, spec Spec) error {
	name := containerName(spec.TenantID, spec.UserID)
	if name == "" {
		return fmt.Errorf("%w: user is required", ErrInvalid)
	}
	return d.s.prepareSleep(ctx, name, spec.TenantID, spec.UserID)
}

// Restore is createUnlocked plus the supervisor ensure. A container that
// still matches is docker start or left running. It is not removed. A
// missing container is created from the state image and the four volumes,
// which is the existing run. This is not the file or bash wake.
func (d DockerRuntime) Restore(ctx context.Context, spec Spec) (RestoreResult, error) {
	created, err := d.s.createUnlocked(ctx, spec)
	if err != nil {
		return RestoreResult{}, err
	}
	key, err := specKey(spec)
	if err != nil {
		return RestoreResult{}, err
	}
	out, err := d.s.docker(ctx, "exec", created.Container, "python3", "/desktop_supervisor.py", "ensure", key)
	if err != nil {
		return RestoreResult{}, err
	}
	return RestoreResult{Container: created.Container, EnsureOut: out}, nil
}

// Stop is the existing stop. A bot display is left for its own idle. The
// owner's stop flushes, then docker stop. It does not remove the container.
func (d DockerRuntime) Stop(ctx context.Context, spec Spec) (Desktop, error) {
	return d.s.stopUnlocked(ctx, spec)
}

// Status reads docker and the screen table. It does not exec the supervisor
// and it does not report sleeping or waking. A stopped container is absent.
func (d DockerRuntime) Status(ctx context.Context, spec Spec) (RuntimeStatus, error) {
	name := containerName(spec.TenantID, spec.UserID)
	if name == "" {
		return RuntimeStatus{}, fmt.Errorf("%w: user is required", ErrInvalid)
	}
	out, err := d.s.docker(ctx, "inspect", "--format", "{{.State.Running}}", name)
	state := computerMetaStateAbsent
	if err != nil {
		if !missingObject(err) && !missingObject(fmt.Errorf("%s", out)) {
			return RuntimeStatus{}, err
		}
	} else if strings.TrimSpace(out) == "true" {
		state = computerMetaStateRunning
	}
	status := RuntimeStatus{State: state, Runtime: "docker"}
	if meta, metaErr := d.s.ReadComputerMeta(spec.TenantID, spec.UserID); metaErr == nil {
		status.Screens = meta.Screens
	}
	return status, nil
}

// MachineStatus is the control-plane status. It does not add a route.
func (s *Service) MachineStatus(ctx context.Context, spec Spec) (RuntimeStatus, error) {
	spec, err := normalize(spec)
	if err != nil {
		return RuntimeStatus{}, err
	}
	var status RuntimeStatus
	err = s.withUser(spec.TenantID, spec.UserID, func() error {
		var statusErr error
		status, statusErr = s.runtime().Status(ctx, spec)
		return statusErr
	})
	return status, err
}
