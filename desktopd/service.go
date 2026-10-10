// Package desktopd is the Docker desktop service. Its source lives in this
// directory so the service can be built and deployed on its own host.
// Shared desktop identity and resource rules come from corelib/desktop.
// Hub calls this process over HTTP and does not run Docker itself.
package desktopd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

const (
	DefaultImage   = desktop.DefaultImage
	DefaultMemory  = desktop.DefaultMemory
	DefaultCPUs    = desktop.DefaultCPUs
	DefaultShmSize = desktop.DefaultShmSize
)

var (
	ErrInvalid = desktop.ErrInvalid
	ErrDocker  = errors.New("docker request failed")
)

// Runner executes docker arguments. Tests replace it.
type Runner func(ctx context.Context, args ...string) (string, error)

// CommandRunner runs docker with an optional stdin. A non-zero exit from the
// program inside the container is an exit code with a nil error. Err is set
// only when docker itself cannot run the command.
type CommandRunner func(ctx context.Context, stdin io.Reader, args ...string) (output string, exitCode int, err error)

// Service runs one Docker host. userGates keep a user's stop from overlapping
// the next start: the stop writes the website login, and a start in the
// middle would open a second browser.
type Service struct {
	Run           Runner
	RunCommand    CommandRunner
	AdvertiseHost string
	// DesktopProxyURL is injected into desktop containers as HTTP(S)_PROXY so
	// the browser and other tools egress through the forward proxy (see
	// forwardproxy.go). Empty keeps desktops on the default route. It is also
	// recorded on the container as maclaw.desktop-proxy: changing it recreates
	// the desktop so the new value takes effect. Egress, when set, supplies
	// the effective value per creation (the panel's file-backed config).
	DesktopProxyURL string
	Egress          *EgressSource
	// ImageSources maps a local image name to the registry ref it is pulled
	// from when missing (image_source.go); ImagePullTimeout bounds that pull.
	ImageSources     map[string]string
	ImagePullTimeout time.Duration
	// pulls tracks in-flight registry pulls by image: concurrent desktop
	// requests join one pull and a failed registry is not re-hit on every
	// request (imagePullRetryAfter backoff, see image_source.go).
	pulls     imagePulls
	userGates sync.Map
}

// desktopProxy resolves the proxy URL the NEXT created desktop receives: the
// panel's egress config wins wholesale when set (an empty value there means
// direct egress), the static env-derived field otherwise.
func (s *Service) desktopProxy() string {
	if s != nil && s.Egress != nil {
		if cfg, fromPanel, err := s.Egress.Get(); err == nil && fromPanel {
			return cfg.DesktopProxyURL
		}
	}
	return s.DesktopProxyURL
}

// Spec is the desktop Hub asks this service to create for one user.
type Spec struct {
	TenantID string
	UserID   string
	Image    string
	Memory   string
	CPUs     string
	ShmSize  string
}

// Desktop is one user's container on this Docker host.
type Desktop struct {
	TenantID  string `json:"tenant_id"`
	UserID    string `json:"user_id"`
	Container string `json:"container"`
	Image     string `json:"image"`
	Memory    string `json:"memory"`
	CPUs      string `json:"cpus"`
	ShmSize   string `json:"shm_size"`
	Status    string `json:"status"`
}

func (s *Service) Create(ctx context.Context, spec Spec) (Desktop, error) {
	spec, err := normalize(spec)
	if err != nil {
		return Desktop{}, err
	}
	var out Desktop
	err = s.withUser(spec.TenantID, spec.UserID, func() error {
		var createErr error
		out, createErr = s.createUnlocked(ctx, spec)
		return createErr
	})
	return out, err
}

func (s *Service) createUnlocked(ctx context.Context, spec Spec) (Desktop, error) {
	spec, err := normalize(spec)
	if err != nil {
		return Desktop{}, err
	}
	// spec.Image stays the requested image (for example maclaw-gui:2) for the
	// whole call. The image docker actually runs may be this user's private
	// state image; that is resolved last and only used for docker run.
	name := containerName(spec.TenantID, spec.UserID)
	current, err := s.inspect(ctx, name)
	if err != nil {
		return Desktop{}, err
	}
	if reason := s.recreateReason(current, spec); reason != "" {
		// The requested image must exist before anything is torn down: a
		// missing or unpullable image has to leave the old desktop in place.
		if err := s.installImage(ctx, spec.Image); err != nil {
			return Desktop{}, err
		}
		log.Printf("desktopd: recreating %s (%s); private volumes are kept", name, reason)
		// The profile volume survives the new container, but only after Chromium
		// has written the website login. rm -f would otherwise kill it first.
		writeCtx, cancelWrite := loginWriteContext(ctx)
		s.flushBrowser(writeCtx, name, spec.TenantID, spec.UserID)
		// rm -f kills the container at once. Stop first so pid 1 can finish
		// writing the website login into this user's profile.
		_ = s.stopContainer(writeCtx, name)
		cancelWrite()
		// The committed layer is labelled with the image the OLD container came
		// from. When the requested image changed, resolveImage below sees the
		// mismatch and sets this layer aside instead of running it.
		if err := s.keepUserLayer(ctx, name, spec, containerImage(current)); err != nil {
			return Desktop{}, err
		}
		if !privateMountsReady(current.Mounts, spec) {
			if err := s.migrateLiveFiles(ctx, name, spec); err != nil {
				return Desktop{}, err
			}
		}
		if _, err := s.docker(ctx, "rm", "-f", name); err != nil {
			return Desktop{}, err
		}
		current.Exists = false
	}
	image, err := s.resolveImage(ctx, spec)
	if err != nil {
		return Desktop{}, err
	}
	if !current.Exists {
		if err := s.runContainer(ctx, name, spec, image); err != nil {
			return Desktop{}, err
		}
	} else {
		if _, err := s.docker(ctx, "update", "--memory", spec.Memory, "--cpus", spec.CPUs, name); err != nil {
			return Desktop{}, err
		}
		if !current.Running {
			if _, err := s.docker(ctx, "start", name); err != nil {
				return Desktop{}, err
			}
		}
	}
	running := spec
	running.Image = image
	return desktopOf(running, name, "running"), nil
}

// desktopProxyLabel records the egress proxy URL a container was created
// with, so changing DESKTOPD_DESKTOP_PROXY_URL recreates the desktop.
const desktopProxyLabel = "maclaw.desktop-proxy"

// desktopProxyNoProxy keeps container-local traffic off the egress proxy.
// The browser additionally bypasses loopback targets on its own.
const desktopProxyNoProxy = "localhost,127.0.0.1,::1"

// containerState is what desktopd reads back from an existing desktop.
type containerState struct {
	Exists  bool
	Running bool
	Shm     string
	Mounts  string
	// Image is the maclaw.image label: the requested image this container was
	// created for. Containers created before the label existed leave it empty.
	Image string
	// ConfigImage is the image name docker run was given. For those older
	// containers it is the legacy image or this user's state image.
	ConfigImage string
	// DesktopProxy is the maclaw.desktop-proxy label: the egress proxy URL
	// this container was created with ("" before the label existed).
	DesktopProxy string
}

// containerImage is the requested image an existing container belongs to.
func containerImage(c containerState) string {
	if image := labelValue(c.Image); image != "" {
		return image
	}
	// No label: created by a desktopd that only knew the legacy image. It ran
	// either that image directly or this user's state image committed from it.
	configImage := labelValue(c.ConfigImage)
	if configImage == "" || isStateImage(configImage) {
		return desktop.LegacyImage
	}
	return configImage
}

// recreateReason decides whether an existing container must be replaced.
// Replacing keeps every private volume (/desktops, /home/desktop, /opt,
// /usr/local) and commits the container layer first; see createUnlocked.
// An empty reason means the container is reused as is.
//
// The image check compares requested image names, not image IDs. Rebuilding
// maclaw-gui:2 in place (for example a deploy that only refreshes the
// supervisor, which remote_deploy.sh also copies into running containers)
// must not restart every desktop and drop the user's installed packages.
// Moving users onto a new image is done by changing the tag.
//
// The desktop proxy label is compared against the service's configured URL:
// enabling, changing or disabling the egress proxy recreates each desktop the
// next time it is opened, and the container env always reflects the value.
func (s *Service) recreateReason(c containerState, spec Spec) string {
	if !c.Exists {
		return ""
	}
	if !privateMountsReady(c.Mounts, spec) {
		return "private volumes are not mounted"
	}
	if c.Shm != "" && !strings.EqualFold(c.Shm, spec.ShmSize) {
		return "shm size changed"
	}
	if image := containerImage(c); image != spec.Image {
		return "image changed from " + image + " to " + spec.Image
	}
	if c.DesktopProxy != s.desktopProxy() {
		return "desktop proxy changed from " + c.DesktopProxy + " to " + s.desktopProxy()
	}
	return ""
}

// stateImageUsable reports whether this user's committed state image may be
// run for the requested image. A state image holds the packages a user
// installed on top of one specific base image. Running a v1-based state image
// when maclaw-gui:2 is requested would silently keep the user on v1, so only a
// state image committed from the same requested image is used. State images
// committed before the label existed came from the legacy image.
func stateImageUsable(label, requested string) bool {
	label = labelValue(label)
	if label == "" {
		return requested == desktop.LegacyImage
	}
	return label == requested
}

func isStateImage(image string) bool {
	return strings.HasPrefix(image, "maclaw-desktop-user-") && strings.HasSuffix(image, ":state")
}

// labelValue treats docker's rendering of a missing label as empty.
func labelValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "<no value>" {
		return ""
	}
	return value
}

func (s *Service) Stop(ctx context.Context, tenantID, userID string) (Desktop, error) {
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return Desktop{}, err
	}
	var out Desktop
	err = s.withUser(spec.TenantID, spec.UserID, func() error {
		var stopErr error
		out, stopErr = s.stopUnlocked(ctx, spec)
		return stopErr
	})
	return out, err
}

func (s *Service) stopUnlocked(ctx context.Context, spec Spec) (Desktop, error) {
	name := containerName(spec.TenantID, spec.UserID)
	// Best effort. A stopped container has nothing to flush, and a failed
	// flush must not leave the desktop running.
	// The caller may already have disconnected. This write still has to finish,
	// or the next session opens a second browser and the login is left behind.
	// --time stays above the supervisor flush (15s). Pid 1 is not Chromium.
	// It catches this signal, asks the browser to write the website login,
	// and only then exits. A shorter grace kills that write.
	writeCtx, cancel := loginWriteContext(ctx)
	defer cancel()
	s.flushBrowser(writeCtx, name, spec.TenantID, spec.UserID)
	if err := s.stopContainer(writeCtx, name); err != nil {
		return Desktop{}, err
	}
	out := desktopOf(spec, name, "stopped")
	out.Image, out.Memory, out.CPUs, out.ShmSize = "", "", "", ""
	return out, nil
}

// Session is the remote handle MaClawSrv uses when it is not on this machine.
// cdp_url uses the advertised host and the published port, not the container IP.
type Session struct {
	CDP       string `json:"cdp_url"`
	Display   string `json:"display"`
	Container string `json:"container"`
	Novnc     string `json:"novnc_url,omitempty"`
}

func (s *Service) OpenSession(ctx context.Context, spec Spec) (Session, error) {
	spec, err := normalize(spec)
	if err != nil {
		return Session{}, err
	}
	var session Session
	err = s.withUser(spec.TenantID, spec.UserID, func() error {
		var openErr error
		session, openErr = s.openSessionUnlocked(ctx, spec)
		return openErr
	})
	return session, err
}

func (s *Service) openSessionUnlocked(ctx context.Context, spec Spec) (Session, error) {
	host, err := s.advertiseHost()
	if err != nil {
		return Session{}, err
	}
	created, err := s.createUnlocked(ctx, spec)
	if err != nil {
		return Session{}, err
	}
	spec, _ = normalize(spec)
	key, err := desktop.UserKey(spec.TenantID, spec.UserID)
	if err != nil {
		return Session{}, err
	}
	out, err := s.docker(ctx, "exec", created.Container, "python3", "/desktop_supervisor.py", "ensure", key)
	if err != nil {
		return Session{}, err
	}
	status, err := desktop.ParseEnsure(out)
	if err != nil {
		return Session{}, fmt.Errorf("%w: %s", ErrDocker, err.Error())
	}
	if strconv.Itoa(status.CDPPort) != desktop.ProxyPort {
		return Session{}, fmt.Errorf("%w: desktop proxy port is not published", ErrDocker)
	}
	published, err := s.docker(ctx, "port", created.Container, desktop.ProxyPort+"/tcp")
	if err != nil {
		return Session{}, err
	}
	hostPort, err := publishedPort(published)
	if err != nil {
		return Session{}, err
	}
	// The token rides as URL userinfo. The CDP client turns it into a Bearer
	// header and the Hub noVNC proxy attaches it when relaying the handoff,
	// so the published browser ports refuse unauthenticated peers.
	authority := host
	if status.Token != "" {
		authority = "desktop:" + status.Token + "@" + host
	}
	session := Session{CDP: "http://" + authority + ":" + hostPort, Display: status.Display, Container: created.Container}
	if publishedVNC, err := s.docker(ctx, "port", created.Container, desktop.VNCPort+"/tcp"); err == nil {
		if vncPort, err := publishedPort(publishedVNC); err == nil {
			session.Novnc = "http://" + authority + ":" + vncPort + "/vnc.html?autoconnect=1&resize=scale"
		}
	}
	return session, nil
}

func (s *Service) App(ctx context.Context, tenantID, userID, display string, args []string) (string, error) {
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return "", err
	}
	if !desktop.ValidDisplay(display) {
		return "", fmt.Errorf("%w: display is invalid", ErrInvalid)
	}
	if len(args) == 0 || len(args) > desktop.MaxAppArgs {
		return "", fmt.Errorf("%w: app command is invalid", ErrInvalid)
	}
	for _, arg := range args {
		// A carriage return stays inside one argument. It is not a
		// shell break, and xdotool type does not treat it as Enter.
		// A newline or NUL is not an argument.
		if strings.TrimSpace(arg) == "" || strings.ContainsAny(arg, "\n\x00") {
			return "", fmt.Errorf("%w: app command is invalid", ErrInvalid)
		}
	}
	var out string
	err = s.withUser(spec.TenantID, spec.UserID, func() error {
		var runErr error
		command := append([]string{"exec", "-e", "DISPLAY=" + display, containerName(spec.TenantID, spec.UserID), "xdotool"}, args...)
		out, runErr = s.docker(ctx, command...)
		return runErr
	})
	return out, err
}

// Open starts one GUI program on the user's display and returns without
// waiting for it. The argv is the program itself. It is not a shell, and
// it is not xdotool.
func (s *Service) Open(ctx context.Context, tenantID, userID, display, program string, args []string) (string, error) {
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return "", err
	}
	command, err := desktop.OpenExecArgs(containerName(spec.TenantID, spec.UserID), display, program, args)
	if err != nil {
		return "", err
	}
	var out string
	err = s.withUser(spec.TenantID, spec.UserID, func() error {
		var runErr error
		out, runErr = s.docker(ctx, command...)
		return runErr
	})
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(out) == "" {
		return desktop.OpenedText(program), nil
	}
	return out, nil
}

// Install runs apt-get update, apt-get -f install, and apt-get install in
// the user's desktop container. Package names are argv, not a shell command.
func (s *Service) Install(ctx context.Context, tenantID, userID string, packages []string) (string, error) {
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return "", err
	}
	names, err := desktop.PackageNames(packages)
	if err != nil {
		return "", err
	}
	// The HTTP clients and the write deadline wait one minute longer
	// than this context, so the budget sentence can be written and read.
	ctx, cancel := desktop.BoundInstall(ctx)
	defer cancel()
	container := containerName(spec.TenantID, spec.UserID)
	var output string
	err = s.withUser(spec.TenantID, spec.UserID, func() error {
		// ensure() starts apt-mirror in the background and returns before
		// that process takes the lock. The wait either blocks until the
		// probe has written the sources, or holds the lock and writes
		// them itself. A wait failure still falls through to apt-get.
		_, _ = s.docker(ctx, desktop.AptMirrorWaitArgs(container)...)
		if stop := desktop.InstallStopError(ctx); stop != nil {
			return stop
		}
		text, runErr := desktop.RunPackageInstall(ctx, func(args []string) (string, error) {
			return s.docker(ctx, args...)
		}, container, names)
		output = text
		return runErr
	})
	if err != nil {
		// A deadline kills apt-get mid-log. The tail of that log is
		// download progress, which hides the fact that the time ran out.
		// A plain cancel still drops that tail and returns the context error.
		if stop := desktop.InstallStopError(ctx); stop != nil {
			return "", stop
		}
		return tailInstall(output), err
	}
	return "installed: " + strings.Join(names, " ") + "\n" + tailInstall(output), nil
}

func tailInstall(text string) string {
	const limit = 8000
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	return text[len(text)-limit:]
}

// Screenshot returns a PNG of the user's X display. It does not start the
// desktop: like App it acts on the container a session already opened.
func (s *Service) Screenshot(ctx context.Context, tenantID, userID, display string) ([]byte, error) {
	data, _, err := s.CaptureScreenshot(ctx, tenantID, userID, display, "")
	return data, err
}

// CaptureScreenshot returns the PNG and, when name is a plain .png file name,
// also copies that file to /home/desktop/Desktop before the temp capture is removed.
// The returned path is set only after the copy command exits 0.
func (s *Service) CaptureScreenshot(ctx context.Context, tenantID, userID, display, name string) ([]byte, string, error) {
	script, err := desktop.ScreenshotScriptFor(name)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %s", ErrInvalid, err.Error())
	}
	spec, err := normalize(Spec{TenantID: tenantID, UserID: userID})
	if err != nil {
		return nil, "", err
	}
	display = strings.TrimSpace(display)
	if display == "" {
		display = desktop.DefaultDisplay
	}
	if !desktop.ValidDisplay(display) {
		return nil, "", fmt.Errorf("%w: display is invalid", ErrInvalid)
	}
	var out string
	err = s.withUser(spec.TenantID, spec.UserID, func() error {
		var runErr error
		out, runErr = s.docker(ctx, "exec", "-e", "DISPLAY="+display, containerName(spec.TenantID, spec.UserID), "sh", "-c", script)
		return runErr
	})
	if err != nil {
		return nil, "", err
	}
	data, err := decodeScreenshot(out)
	if err != nil {
		return nil, "", err
	}
	return data, desktop.DesktopShotPath(name), nil
}

func decodeScreenshot(out string) ([]byte, error) {
	data, err := desktop.DecodeScreenshot(out)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrDocker, err.Error())
	}
	return data, nil
}

// loginWriteTimeout covers the supervisor flush and the container stop grace.
// A cancelled request must not cut that short.
const loginWriteTimeout = 60 * time.Second

func loginWriteContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(ctx), loginWriteTimeout)
}

func (s *Service) withUser(tenantID, userID string, fn func() error) error {
	if s == nil {
		return fn()
	}
	key := tenantID + "\x00" + userID
	gate, _ := s.userGates.LoadOrStore(key, &sync.Mutex{})
	gate.(*sync.Mutex).Lock()
	defer gate.(*sync.Mutex).Unlock()
	return fn()
}

func (s *Service) advertiseHost() (string, error) {
	host := ""
	if s != nil {
		host = strings.TrimSpace(s.AdvertiseHost)
	}
	if host == "" {
		host = strings.TrimSpace(os.Getenv("DESKTOPD_ADVERTISE_HOST"))
	}
	if host == "" || strings.ContainsAny(host, " /\r\n") {
		return "", fmt.Errorf("%w: advertise host is required", ErrInvalid)
	}
	return host, nil
}

// advertiseHostOrEmpty is the non-failing form for the admin panel, which
// should render even before the host is configured.
func (s *Service) advertiseHostOrEmpty() string {
	host, err := s.advertiseHost()
	if err != nil {
		return ""
	}
	return host
}

// resolveImage uses this user's private image when one exists, so packages
// installed into the container stay with that user. A new user starts from
// the shared image and never receives another user's layer.
//
// spec.Image must be the requested image. A state image committed from a
// different requested image (for example a maclaw-gui:1 layer when
// maclaw-gui:2 is requested) is not run: it is retired, and the user starts
// from the requested image. Their browser profile, home, /opt and /usr/local
// are volumes and carry over; only packages installed into the old image
// layer stay behind in the retired image.
func (s *Service) resolveImage(ctx context.Context, spec Spec) (string, error) {
	state, err := desktop.StateImage(spec.TenantID, spec.UserID)
	if err != nil {
		return "", err
	}
	if label, err := s.docker(ctx, "image", "inspect", "--format", `{{index .Config.Labels "`+desktop.ImageLabel+`"}}`, state); err == nil {
		if stateImageUsable(label, spec.Image) {
			return state, nil
		}
		s.retireStateImage(ctx, state, labelValue(label), spec.Image)
	}
	if err := s.installImage(ctx, spec.Image); err != nil {
		return "", err
	}
	return spec.Image, nil
}

// retireStateImage moves a stale state image to the :prev tag. Only one
// generation is kept (the next retirement overwrites it), so disk use stays
// bounded while an operator can still recover packages by hand. Untagging
// :state does not delete layers a container still uses. Failure is logged
// and otherwise ignored: the stale image is skipped either way.
func (s *Service) retireStateImage(ctx context.Context, state, from, requested string) {
	if from == "" {
		from = desktop.LegacyImage
	}
	prev := strings.TrimSuffix(state, ":state") + ":prev"
	log.Printf("desktopd: %s was committed from %s, not %s; keeping it as %s", state, from, requested, prev)
	if _, err := s.docker(ctx, "tag", state, prev); err != nil {
		log.Printf("desktopd: tag %s: %v", prev, err)
		return
	}
	if _, err := s.docker(ctx, "rmi", state); err != nil {
		log.Printf("desktopd: untag %s: %v", state, err)
	}
}

// keepUserLayer copies installed packages into the user's private image
// before the container is replaced. Volumes already hold logins and home files.
// migrateLiveFiles copies this user's files out of the running container
// before a private volume is mounted over them. Only this user's browser
// profile is copied, so a previously shared /desktops tree does not come along.
func (s *Service) migrateLiveFiles(ctx context.Context, name string, spec Spec) error {
	mounts, err := desktop.PrivateMounts(spec.TenantID, spec.UserID)
	if err != nil {
		return err
	}
	state, err := desktop.StateImage(spec.TenantID, spec.UserID)
	if err != nil {
		return err
	}
	key, err := desktop.UserKey(spec.TenantID, spec.UserID)
	if err != nil {
		return err
	}
	for _, mount := range mounts {
		if _, err := s.docker(ctx, "volume", "create", mount.Volume); err != nil {
			return err
		}
	}
	tmp := "maclaw-migrate-" + key
	args := []string{"run", "-d", "--name", tmp, "--entrypoint", "sleep"}
	var mkdir strings.Builder
	for _, mount := range mounts {
		side := "/mnt" + mount.Target
		args = append(args, "--volume", mount.Volume+":"+side)
		fmt.Fprintf(&mkdir, "mkdir -p %s; ", side)
	}
	args = append(args, state, "60")
	if _, err := s.docker(ctx, args...); err != nil {
		return err
	}
	if _, err := s.docker(ctx, "exec", tmp, "sh", "-c", mkdir.String()); err != nil {
		_, _ = s.docker(ctx, "rm", "-f", tmp)
		return err
	}
	for _, mount := range mounts {
		src := mount.Target
		dest := "/mnt" + mount.Target
		if mount.Target == "/desktops" {
			src = "/desktops/" + key
			dest = "/mnt/desktops/" + key
			if _, err := s.docker(ctx, "exec", tmp, "mkdir", "-p", dest); err != nil {
				_, _ = s.docker(ctx, "rm", "-f", tmp)
				return err
			}
		}
		if err := s.copyContainerPath(ctx, name+":"+src, tmp+":"+dest); err != nil {
			_, _ = s.docker(ctx, "rm", "-f", tmp)
			return err
		}
	}
	_, err = s.docker(ctx, "rm", "-f", tmp)
	return err
}

func (s *Service) copyContainerPath(ctx context.Context, from, to string) error {
	dir, err := os.MkdirTemp("", "maclaw-desktop-copy-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if _, err := s.docker(ctx, "cp", from, dir); err != nil {
		if missingPath(err) {
			return nil
		}
		return err
	}
	_, containerPath, ok := strings.Cut(from, ":")
	if !ok || path.Base(containerPath) == "." || path.Base(containerPath) == "/" {
		return fmt.Errorf("%w: desktop copy path is invalid", ErrDocker)
	}
	// docker cp into an existing directory creates a folder named after the
	// source. Copy that folder's contents so /desktops/<key>/profile stays
	// /desktops/<key>/profile on the private volume.
	leaf := filepath.Join(dir, path.Base(containerPath))
	_, err = s.docker(ctx, "cp", leaf+string(filepath.Separator)+".", to)
	return err
}

func missingPath(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "no such") || strings.Contains(text, "could not find") || strings.Contains(text, "not found")
}

// desktopStopGrace is longer than flush_browser's 15s wait. docker stop
// sends SIGKILL when this elapses, and that drops the website login.
const desktopStopGrace = "25"

// desktopHoldCommand is pid 1. sleep alone exits on SIGTERM and the runtime
// then kills Chromium before it can write cookies. The trap flushes first.
const desktopHoldCommand = `trap 'python3 /desktop_supervisor.py flush "$MACLAW_DESKTOP_KEY"; exit 0' TERM INT; sleep infinity & wait`

func (s *Service) stopContainer(ctx context.Context, name string) error {
	_, err := s.docker(ctx, "stop", "--time", desktopStopGrace, name)
	if err != nil && !missingObject(err) {
		return err
	}
	return nil
}

func (s *Service) flushBrowser(ctx context.Context, name, tenantID, userID string) {
	key, err := desktop.UserKey(tenantID, userID)
	if err != nil {
		return
	}
	_, _ = s.docker(ctx, "exec", name, "python3", "/desktop_supervisor.py", "flush", key)
}

// keepUserLayer commits the container to this user's state image. from is
// the requested image the container was created for; it is recorded as a
// label so resolveImage can tell which base the layer belongs to.
func (s *Service) keepUserLayer(ctx context.Context, name string, spec Spec, from string) error {
	state, err := desktop.StateImage(spec.TenantID, spec.UserID)
	if err != nil {
		return err
	}
	if _, err := s.docker(ctx, "commit", "--change", "LABEL "+desktop.ImageLabel+"="+strconv.Quote(from), name, state); err != nil {
		return err
	}
	return nil
}

// installImage moved to image_source.go: it consults the per-image source map
// (s.ImageSources) and bounds registry pulls with ImagePullTimeout.

// runContainer starts a new desktop from image. spec.Image is the requested
// image and is recorded in the maclaw.image label; image may be this user's
// state image built on top of it.
func (s *Service) runContainer(ctx context.Context, name string, spec Spec, image string) error {
	mounts, err := desktop.PrivateMounts(spec.TenantID, spec.UserID)
	if err != nil {
		return err
	}
	key, err := desktop.UserKey(spec.TenantID, spec.UserID)
	if err != nil {
		return err
	}
	args := []string{
		"run", "-d",
		"--name", name,
		"--entrypoint", "sh",
		"--restart", "unless-stopped",
		"--memory", spec.Memory,
		"--cpus", spec.CPUs,
		"--shm-size", spec.ShmSize,
		"--label", "maclaw.tenant=" + spec.TenantID,
		"--label", "maclaw.user=" + spec.UserID,
		"--label", "maclaw.memory=" + spec.Memory,
		"--label", "maclaw.cpus=" + spec.CPUs,
		"--label", "maclaw.shm=" + spec.ShmSize,
		"--label", desktop.ImageLabel + "=" + spec.Image,
		"--env", "HOME=/home/desktop",
		"--env", "MACLAW_DESKTOP_KEY=" + key,
		// The proxy env is always written — empty when no proxy is configured.
		// A committed state image inherits the container env, and a stale
		// HTTP(S)_PROXY from an earlier proxy configuration must not leak
		// into a desktop recreated after the proxy was disabled or moved.
		"--env", "HTTP_PROXY=" + s.desktopProxy(),
		"--env", "HTTPS_PROXY=" + s.desktopProxy(),
		"--env", "http_proxy=" + s.desktopProxy(),
		"--env", "https_proxy=" + s.desktopProxy(),
		"--env", "NO_PROXY=" + desktopProxyNoProxy,
		"--env", "no_proxy=" + desktopProxyNoProxy,
		"--label", desktopProxyLabel + "=" + s.desktopProxy(),
		"--publish", desktop.ProxyPort + "/tcp",
		"--publish", desktop.VNCPort + "/tcp",
	}
	for _, mount := range mounts {
		args = append(args, "--volume", mount.Volume+":"+mount.Target)
	}
	// The image command is not the desktop. This process only stays up so
	// desktopd can exec the supervisor. On docker stop it flushes Chromium
	// before exiting, which keeps the website login in the same profile.
	args = append(args, image, "-c", desktopHoldCommand)
	_, err = s.docker(ctx, args...)
	return err
}

// inspectFormat prints running|shm|mounts|maclaw.image|config image|desktop
// proxy. Mount and image names cannot contain "|"; the proxy URL is scheme
// + IP + port (validated by DesktopProxyBind), so it cannot either.
const inspectFormat = `{{.State.Running}}|{{index .Config.Labels "maclaw.shm"}}|{{range .Mounts}}{{.Name}}={{.Destination}} {{end}}|{{index .Config.Labels "` + desktop.ImageLabel + `"}}|{{.Config.Image}}|{{index .Config.Labels "` + desktopProxyLabel + `"}}`

func (s *Service) inspect(ctx context.Context, name string) (containerState, error) {
	out, callErr := s.docker(ctx, "inspect", "--format", inspectFormat, name)
	if callErr != nil {
		if missingObject(callErr) || missingObject(errors.New(out)) {
			return containerState{}, nil
		}
		return containerState{}, callErr
	}
	return parseInspect(out), nil
}

func parseInspect(out string) containerState {
	parts := strings.SplitN(strings.TrimSpace(out), "|", 6)
	state := containerState{Exists: true, Running: parts[0] == "true"}
	if len(parts) > 1 {
		state.Shm = labelValue(parts[1])
	}
	if len(parts) > 2 {
		state.Mounts = parts[2]
	}
	if len(parts) > 3 {
		state.Image = labelValue(parts[3])
	}
	if len(parts) > 4 {
		state.ConfigImage = labelValue(parts[4])
	}
	if len(parts) > 5 {
		state.DesktopProxy = labelValue(parts[5])
	}
	return state
}

func privateMountsReady(mountText string, spec Spec) bool {
	want, err := desktop.PrivateMounts(spec.TenantID, spec.UserID)
	if err != nil || len(want) == 0 {
		return false
	}
	have := map[string]bool{}
	for _, item := range strings.Fields(mountText) {
		have[item] = true
	}
	for _, mount := range want {
		if !have[mount.Volume+"="+mount.Target] {
			return false
		}
	}
	return true
}

func (s *Service) docker(ctx context.Context, args ...string) (string, error) {
	run := s.Run
	if run == nil {
		run = defaultRun
	}
	return run(ctx, args...)
}

func defaultRun(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, fmt.Errorf("%w: %s", ErrDocker, text)
	}
	return text, nil
}

func normalize(spec Spec) (Spec, error) {
	spec.TenantID = strings.TrimSpace(spec.TenantID)
	spec.UserID = strings.TrimSpace(spec.UserID)
	if spec.TenantID == "" || strings.ContainsAny(spec.TenantID, "\r\n\x00") {
		return Spec{}, fmt.Errorf("%w: tenant is required", ErrInvalid)
	}
	if !desktop.ValidUserID(spec.UserID) {
		return Spec{}, fmt.Errorf("%w: user is required", ErrInvalid)
	}
	resources, err := desktop.NormalizeResources(spec.Image, spec.Memory, spec.CPUs, spec.ShmSize)
	if err != nil {
		return Spec{}, err
	}
	spec.Image, spec.Memory, spec.CPUs, spec.ShmSize = resources.Image, resources.Memory, resources.CPUs, resources.ShmSize
	return spec, nil
}

func containerName(tenantID, userID string) string {
	key, err := desktop.UserKey(tenantID, userID)
	if err != nil {
		return ""
	}
	return "maclaw-desktop-" + key
}

func publishedPort(out string) (string, error) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		index := strings.LastIndex(line, ":")
		if index < 0 || index == len(line)-1 {
			continue
		}
		port := line[index+1:]
		if _, err := strconv.Atoi(port); err == nil {
			return port, nil
		}
	}
	return "", fmt.Errorf("%w: published desktop port is missing", ErrDocker)
}

func desktopOf(spec Spec, name, status string) Desktop {
	return Desktop{
		TenantID: spec.TenantID, UserID: spec.UserID, Container: name,
		Image: spec.Image, Memory: spec.Memory, CPUs: spec.CPUs, ShmSize: spec.ShmSize, Status: status,
	}
}

func missingObject(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "no such object") || strings.Contains(text, "no such container") || strings.Contains(text, "no such image")
}
