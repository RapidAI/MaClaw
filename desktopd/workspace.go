package desktopd

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// The tool workspace is a headless sibling of the desktop container. bash
// and file calls run in it while the desktop is asleep, so a task that only
// writes files or runs commands never boots Xvfb or Chromium.
//
// It runs the same image as the user's desktop (the private state image
// when there is one), mounts the same private and package volumes, and runs
// as the same unprivileged container: no published ports, no extra
// capabilities, the same memory and cpu limits. ~/, /opt, /usr/local,
// /desktops, and the apt/dpkg roots (/usr, /etc, /var/lib/dpkg, /var/lib/apt)
// are therefore the same files the desktop sees.
// While the desktop is running, bash and file calls go to the desktop
// itself, as before, so a GUI program and a shell see one process space.
//
// Its labels are maclaw.work.*, not maclaw.tenant/maclaw.user: the desktop
// reaper, the admin list and the sleep path never mistake it for a desktop.
const (
	workspacePrefix     = "maclaw-work-"
	workspaceTenantLbl  = "maclaw.work.tenant"
	workspaceUserLbl    = "maclaw.work.user"
	workspaceImageLbl   = "maclaw.work.image"
	workspaceHold       = `trap 'exit 0' TERM INT; sleep infinity & wait`
	workspaceStopGrace  = "5"
)

func workspaceName(tenantID, userID string) string {
	key, err := desktop.UserKey(tenantID, userID)
	if err != nil {
		return ""
	}
	return workspacePrefix + key
}

// toolContainer is the container one bash or file call runs in: the
// desktop while it is running, otherwise the user's workspace, started (or
// created) here. The desktop is never started by this. Call it under the
// user lock (read or write); it takes no lock itself.
func (s *Service) toolContainer(ctx context.Context, spec Spec) (string, bool, error) {
	desktopName := containerName(spec.TenantID, spec.UserID)
	if s == nil || !s.ToolWorkspace || s.Machines != nil {
		// A machine runtime has no Docker sibling; it keeps its own wake.
		return desktopName, true, nil
	}
	if running, err := s.containerRunning(ctx, desktopName); err == nil && running {
		return desktopName, true, nil
	}
	name, err := s.ensureWorkspace(ctx, spec)
	return name, false, err
}

// workspaceImage is the image the desktop container runs, by id (its state
// image when it has one), so packages the desktop installed are there too
// and a re-committed state tag is noticed. derived reports a state image,
// whose supervisor is refreshed from the base like the desktop's.
func (s *Service) workspaceImage(ctx context.Context, spec Spec) (image string, derived bool, err error) {
	if out, inspectErr := s.docker(ctx, "inspect", "--format", "{{.Image}}|{{.Config.Image}}", containerName(spec.TenantID, spec.UserID)); inspectErr == nil {
		parts := strings.SplitN(strings.TrimSpace(out), "|", 2)
		if len(parts) == 2 && parts[0] != "" {
			return parts[0], parts[1] != spec.Image, nil
		}
	}
	image, err = s.resolveImage(ctx, spec)
	return image, image != spec.Image, err
}

func (s *Service) ensureWorkspace(ctx context.Context, spec Spec) (string, error) {
	name := workspaceName(spec.TenantID, spec.UserID)
	if name == "" {
		return "", ErrInvalid
	}
	image, derived, err := s.workspaceImage(ctx, spec)
	if err != nil {
		return "", err
	}
	out, inspectErr := s.docker(ctx, "inspect", "--format", `{{.State.Running}}|{{index .Config.Labels "`+workspaceImageLbl+`"}}|{{range .Mounts}}{{.Name}}={{.Destination}} {{end}}`, name)
	exists := inspectErr == nil
	if inspectErr != nil && !missingObject(inspectErr) {
		return "", inspectErr
	}
	if exists {
		parts := strings.SplitN(strings.TrimSpace(out), "|", 3)
		needRecreate := false
		reason := ""
		if len(parts) >= 2 && parts[1] != image {
			needRecreate, reason = true, "image "+image
		} else if len(parts) < 3 || !packageMountsReady(parts[2], spec) {
			needRecreate, reason = true, "package volumes"
		}
		if needRecreate {
			log.Printf("desktopd: recreating workspace %s (%s)", name, reason)
			if _, err := s.docker(ctx, "rm", "-f", name); err != nil {
				return "", err
			}
			exists = false
		} else if parts[0] == "true" {
			s.noteWorkspaceUse(name)
			return name, nil
		}
	}
	if exists {
		if _, err := s.docker(ctx, "start", name); err != nil {
			return "", err
		}
		s.noteWorkspaceUse(name)
		return name, nil
	}
	mounts, err := desktop.UserMounts(spec.TenantID, spec.UserID)
	if err != nil {
		return "", err
	}
	if err := s.seedPackageVolumes(ctx, spec, image); err != nil {
		return "", err
	}
	args := []string{
		"run", "-d",
		"--name", name,
		"--entrypoint", "sh",
		"--restart", "no",
		"--memory", spec.Memory,
		"--cpus", spec.CPUs,
		"--label", workspaceTenantLbl + "=" + spec.TenantID,
		"--label", workspaceUserLbl + "=" + spec.UserID,
		"--label", workspaceImageLbl + "=" + image,
		"--env", "HOME=/home/desktop",
		"--env", "HTTP_PROXY=" + s.desktopProxy(),
		"--env", "HTTPS_PROXY=" + s.desktopProxy(),
		"--env", "http_proxy=" + s.desktopProxy(),
		"--env", "https_proxy=" + s.desktopProxy(),
		"--env", "NO_PROXY=" + desktopProxyNoProxy,
		"--env", "no_proxy=" + desktopProxyNoProxy,
	}
	args = appendUserMountArgs(args, mounts)
	args = append(args, image, "-c", workspaceHold)
	if _, err := s.docker(ctx, args...); err != nil {
		// Two calls raced to create it; the other one won.
		if running, runErr := s.containerRunning(ctx, name); runErr == nil && running {
			s.noteWorkspaceUse(name)
			return name, nil
		}
		return "", err
	}
	if derived {
		s.refreshSupervisor(ctx, name, spec.Image)
	}
	log.Printf("desktopd: started workspace %s (image %s)", name, image)
	s.noteWorkspaceUse(name)
	return name, nil
}

func (s *Service) noteWorkspaceUse(name string) {
	if s != nil && name != "" {
		s.workspaceUse.Store(name, s.now())
	}
}

// noteToolUse refreshes the stamp of whichever container ran the call: the
// desktop's host stamp when it ran on the desktop, the workspace stamp
// otherwise (so a workspace call never keeps a desktop awake).
func (s *Service) noteToolUse(spec Spec, container string, onDesktop bool) {
	if onDesktop {
		s.noteComputerActivity(spec.TenantID, spec.UserID)
		return
	}
	s.noteWorkspaceUse(container)
}

func (s *Service) workspaceIdle() time.Duration {
	if s != nil && s.WorkspaceMaxIdle > 0 {
		return s.WorkspaceMaxIdle
	}
	return WorkspaceIdleDefault
}

// reapWorkspaces stops workspaces with no bash or file call for maxIdle. A
// workspace this process has not seen yet (desktopd restarted) gets a fresh
// stamp, so it lives one more idle period. Callers that pass <=0 get
// workspaceIdle() (WorkspaceMaxIdle or WorkspaceIdleDefault), not the
// historical 10m fallback that used to couple to a short desktop sleep.
func (s *Service) reapWorkspaces(ctx context.Context, maxIdle time.Duration) {
	if maxIdle <= 0 {
		maxIdle = s.workspaceIdle()
	}
	out, err := s.docker(ctx, "ps", "--filter", "label="+workspaceUserLbl, "--format", "{{.Names}}\\t{{.Label \""+workspaceTenantLbl+"\"}}\\t{{.Label \""+workspaceUserLbl+"\"}}")
	if err != nil {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		if len(fields) != 3 || !strings.HasPrefix(fields[0], workspacePrefix) {
			continue
		}
		name, tenantID, userID := fields[0], fields[1], fields[2]
		v, ok := s.workspaceUse.Load(name)
		if !ok {
			s.noteWorkspaceUse(name)
			continue
		}
		at, _ := v.(time.Time)
		if s.now().Sub(at) < maxIdle || s.userBusy(tenantID, userID) {
			continue
		}
		err := s.withUser(tenantID, userID, func() error {
			if s.userBusy(tenantID, userID) {
				return nil
			}
			_, err := s.docker(context.WithoutCancel(ctx), "stop", "--time", workspaceStopGrace, name)
			return err
		})
		if err != nil {
			log.Printf("[desktopd] workspace stop failed container=%s: %v", name, err)
			continue
		}
		s.workspaceUse.Delete(name)
		log.Printf("[desktopd] stopped idle workspace container=%s", name)
	}
}

// EnsureTools is what the bash, file and upload routes call before the op.
// With Docker it starts nothing: the op itself picks the running desktop or
// the workspace. A machine runtime keeps the desktop wake it always had.
func (s *Service) EnsureTools(ctx context.Context, spec Spec) error {
	if s == nil || !s.ToolWorkspace || s.Machines != nil {
		return s.Ensure(ctx, spec)
	}
	_, err := normalize(spec)
	return err
}
