// Package desktop is the shared desktop contract used by the Docker service,
// Hub, and MaClawSrv. The Docker service process stays in its own directory.
package desktop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultImage is the XFCE desktop built from desktopd/image/Dockerfile.v2.
	// Hub sends its configured image; this default only applies when that is
	// empty. Changing the tag (not rebuilding the same tag) is what makes
	// desktopd move existing users onto a new image, see desktopd/service.go.
	DefaultImage = "maclaw-gui:2"
	// LegacyImage is the fluxbox-only image. Containers and per-user state
	// images created before desktopd recorded the maclaw.image label are
	// assumed to come from it.
	LegacyImage = "maclaw-gui:1"
	// XFCE plus Chromium needs more headroom than fluxbox did; a full desktop
	// with a few apps open measured about 600MiB, Chromium tabs add the rest.
	DefaultMemory  = "3g"
	DefaultCPUs    = "1.5"
	DefaultShmSize = "1g"
	// DefaultDisplay is the X display the supervisor uses inside a per-user
	// container (DISPLAY_MIN in desktop_supervisor.py, CDP gate 19000+20).
	DefaultDisplay = ":20"
	// ImageLabel records, on a desktop container and on the per-user state
	// image committed from it, which requested image (tag) it came from.
	ImageLabel = "maclaw.image"
	// ProxyPort is the container port published for the per-user CDP proxy.
	// Display :20 maps to this port inside the image.
	ProxyPort = "19020"
	// VNCPort is the container port for the noVNC page used when a person
	// has to log in or solve a captcha.
	VNCPort = "6080"
	// MaxAppArgs is one batched xdotool invocation. Sixteen steps of the
	// longest app action stay under this limit.
	MaxAppArgs = 128
	// MaxOpenArgs is the argv after the program name for one app_open.
	// The program is started detached on the desktop display. It is not
	// a shell, so a long command belongs in the terminal after it opens.
	MaxOpenArgs = 8
	// DesktopHome is the home directory the desktop supervisor gives every
	// program it starts. docker exec does not inherit that process
	// environment, so a later launch has to set HOME itself.
	DesktopHome = "/home/desktop"
	// AppWindowMiss is what app_run tells the model when xdotool exits 1.
	// That exit means the named window is not open. It does not mean the
	// desktop cannot start a program.
	AppWindowMiss = "app_run only uses a window that is already open. exit status 1 means that window is not open. Start the program with app_open"
	// MaxInstallPackages is one apt-get install. A desktop install is a
	// package name, not a shell command.
	MaxInstallPackages = 8
	// InstallBudget is how long apt-get update plus install may run.
	// The HTTP clients wait InstallClientTimeout so the stop sentence
	// comes back. Both stay inside the 30-minute bot message budget.
	InstallBudget = 20 * time.Minute
	// InstallReplyGrace is the extra minute after apt-get stops. The
	// install clients and the desktopd write deadline use it to write
	// and read the budget sentence.
	InstallReplyGrace = time.Minute
	// InstallClientTimeout is the MaClawSrv install client, the Hub
	// install client, and the desktopd install read/write deadline.
	InstallClientTimeout = InstallBudget + InstallReplyGrace
	// AptFailureRunes is the end of an apt-get log that still fits in Hub's
	// upstream detail. That detail keeps the first 200 runes, so a longer
	// body would show download progress and drop the error at the end.
	AptFailureRunes = 160
)

// packageNamePattern is a Debian package name. It rejects shell
// metacharacters, paths, and version pins.
var packageNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]{1,62}$`)

// PackageNames checks one install request. A string may list several names
// separated by spaces or commas. Duplicates are dropped.
func PackageNames(raw any) ([]string, error) {
	var items []string
	switch typed := raw.(type) {
	case string:
		items = splitPackageText(typed)
	case []string:
		items = append([]string(nil), typed...)
	case []any:
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%w: package name is invalid", ErrInvalid)
			}
			items = append(items, text)
		}
	default:
		return nil, fmt.Errorf("%w: package name is required", ErrInvalid)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("%w: package name is required", ErrInvalid)
	}
	if len(items) > MaxInstallPackages {
		return nil, fmt.Errorf("%w: too many packages", ErrInvalid)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item)
		if !packageNamePattern.MatchString(name) || seen[name] {
			if !packageNamePattern.MatchString(name) {
				return nil, fmt.Errorf("%w: package name is invalid", ErrInvalid)
			}
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: package name is required", ErrInvalid)
	}
	return out, nil
}

func splitPackageText(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
}

// aptMirrorWaitScript runs before apt-get. ensure() spawns apt-mirror and
// returns before that process takes the lock, so a waiter that only locks
// and unlocks can still lose the race. If the lock is free, this process
// holds it and configures the sources itself; the background probe then
// skips. If the probe already holds the lock, this process waits until that
// probe has written the sources.
const aptMirrorWaitScript = "import fcntl, importlib.util\n" +
	"f = open('/run/maclaw-apt-mirror.lock', 'a', encoding='utf-8')\n" +
	"try:\n" +
	"    won = False\n" +
	"    try:\n" +
	"        fcntl.flock(f, fcntl.LOCK_EX | fcntl.LOCK_NB)\n" +
	"        won = True\n" +
	"    except OSError:\n" +
	"        fcntl.flock(f, fcntl.LOCK_EX)\n" +
	"    if won:\n" +
	"        spec = importlib.util.spec_from_file_location('maclaw_desktop_supervisor', '/desktop_supervisor.py')\n" +
	"        if spec is not None and spec.loader is not None:\n" +
	"            mod = importlib.util.module_from_spec(spec)\n" +
	"            spec.loader.exec_module(mod)\n" +
	"            if not mod.apt_config_current():\n" +
	"                mod.configure_apt()\n" +
	"finally:\n" +
	"    fcntl.flock(f, fcntl.LOCK_UN)\n" +
	"    f.close()\n"

// AptMirrorWaitArgs is docker exec python that waits for that lock.
func AptMirrorWaitArgs(container string) []string {
	return []string{"exec", container, "python3", "-c", aptMirrorWaitScript}
}

// AptFailureText is the end of an apt-get log. Callers that keep only the
// start of an error body still show the E: line.
func AptFailureText(output string, err error) string {
	text := strings.TrimSpace(output)
	if text == "" && err != nil {
		text = strings.TrimSpace(err.Error())
	}
	if text == "" {
		return "apt-get failed"
	}
	runes := []rune(text)
	if len(runes) > AptFailureRunes {
		text = strings.TrimSpace(string(runes[len(runes)-AptFailureRunes:]))
	}
	if text == "" {
		return "apt-get failed"
	}
	return text
}

// BoundInstall limits apt-get to InstallBudget. A nil parent uses
// Background. A parent deadline that is sooner still wins.
func BoundInstall(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, InstallBudget)
}

// InstallStopError reports why apt-get stopped when the context has ended.
// A deadline is the budget sentence. Any other cancel is the context error.
// A live context returns nil so the apt error stays visible.
func InstallStopError(ctx context.Context) error {
	if ctx == nil || ctx.Err() == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("package install stopped because the time budget ran out")
	}
	return ctx.Err()
}

// AptUpdateArgs is docker exec apt-get update for one desktop container.
func AptUpdateArgs(container string) []string {
	return []string{"exec", "-e", "DEBIAN_FRONTEND=noninteractive", container, "apt-get", "update", "-qq"}
}

// dpkgConfigureScript waits for the locks dpkg itself refuses to wait for,
// then becomes dpkg. dpkg uses a non-blocking fcntl lock, so a direct
// dpkg --configure fails while a killed apt-get still holds that lock.
// The fds stay open across exec so this process already owns the locks
// when dpkg tries to take them.
const dpkgConfigureScript = "import fcntl, os\n" +
	"for path in ('/var/lib/dpkg/lock-frontend', '/var/lib/dpkg/lock'):\n" +
	"    fd = os.open(path, os.O_RDWR | os.O_CREAT, 0o640)\n" +
	"    os.set_inheritable(fd, True)\n" +
	"    while True:\n" +
	"        try:\n" +
	"            fcntl.lockf(fd, fcntl.LOCK_EX)\n" +
	"            break\n" +
	"        except InterruptedError:\n" +
	"            continue\n" +
	"os.execvp('dpkg', ['dpkg', '--force-confdef', '--force-confold', '--configure', '-a'])\n"

// DpkgConfigureArgs finishes packages a killed apt-get left unpacked or
// half-configured. A half-installed package makes this step fail; apt-get
// -f install finishes that state. Confdef and confold keep dpkg from
// waiting on a conffile prompt.
func DpkgConfigureArgs(container string) []string {
	return []string{
		"exec", "-e", "DEBIAN_FRONTEND=noninteractive", container,
		"python3", "-c", dpkgConfigureScript,
	}
}

// AptFixArgs is docker exec apt-get -f install with no package names.
// apt-get install of the requested names refuses unless those names
// themselves repair the breakage. A healthy database exits immediately.
// Fixing a conflict can remove a package.
func AptFixArgs(container string) []string {
	return []string{
		"exec", "-e", "DEBIAN_FRONTEND=noninteractive", container,
		"apt-get", "-f", "install", "-y",
		"-o", "Dpkg::Options::=--force-confdef",
		"-o", "Dpkg::Options::=--force-confold",
	}
}

// AptInstallArgs is docker exec apt-get install for names PackageNames accepted.
func AptInstallArgs(container string, packages []string) []string {
	argv := []string{
		"exec", "-e", "DEBIAN_FRONTEND=noninteractive", container,
		"apt-get", "install", "-y",
		"-o", "Dpkg::Options::=--force-confdef",
		"-o", "Dpkg::Options::=--force-confold",
	}
	return append(argv, packages...)
}

// RunPackageInstall configures pending packages, updates the package lists,
// fixes a half-installed package, then installs names. dpkg --configure -a
// exits non-zero for a half-installed package and cannot finish it. apt
// still accepts update once the dpkg journal is clean, and apt-get -f install
// then repacks that package. If update also fails, the configure error is
// returned: apt would only repeat that dpkg must be run. A deadline or
// cancel stops the sequence and drops that step's output.
func RunPackageInstall(ctx context.Context, run func([]string) (string, error), container string, names []string) (string, error) {
	repairOut, repairErr := run(DpkgConfigureArgs(container))
	if stop := InstallStopError(ctx); stop != nil {
		return "", stop
	}
	updateOut, updateErr := run(AptUpdateArgs(container))
	if stop := InstallStopError(ctx); stop != nil {
		return "", stop
	}
	if updateErr != nil {
		if repairErr != nil {
			return repairOut, repairErr
		}
		return updateOut, updateErr
	}
	fixOut, fixErr := run(AptFixArgs(container))
	if stop := InstallStopError(ctx); stop != nil {
		return "", stop
	}
	if fixErr != nil {
		return fixOut, fixErr
	}
	out, err := run(AptInstallArgs(container, names))
	if stop := InstallStopError(ctx); stop != nil {
		return "", stop
	}
	return out, err
}

// ErrInvalid means the desktop identity or resource request cannot be used.
var ErrInvalid = errors.New("invalid desktop request")

// Resources are the container limits applied when a desktop is created.
type Resources struct {
	Image   string
	Memory  string
	CPUs    string
	ShmSize string
}

// EnsureStatus is the one-line JSON the in-container supervisor prints.
type EnsureStatus struct {
	Display string
	CDPPort int
	Token   string
}

// Mount is one private volume for a user's desktop data.
type Mount struct {
	Volume string
	Target string
}

// PrivateMounts are the volumes that hold one user's desktop and nobody else's.
// Browser logins live on /desktops. App logins live in the home directory.
// Software installed under /opt or /usr/local stays on those volumes.
func PrivateMounts(tenantID, userID string) ([]Mount, error) {
	key, err := UserKey(tenantID, userID)
	if err != nil {
		return nil, err
	}
	return []Mount{
		{Volume: "maclaw-desktops-" + key, Target: "/desktops"},
		{Volume: "maclaw-home-" + key, Target: "/home/desktop"},
		{Volume: "maclaw-opt-" + key, Target: "/opt"},
		{Volume: "maclaw-local-" + key, Target: "/usr/local"},
	}, nil
}

// StateImage is the private image that keeps packages installed into this
// user's container. It is never the shared desktop image.
func StateImage(tenantID, userID string) (string, error) {
	key, err := UserKey(tenantID, userID)
	if err != nil {
		return "", err
	}
	return "maclaw-desktop-user-" + key + ":state", nil
}

// UserKey is the desktop identity for one account. Instance id is not part
// of the key: every agent instance of this user shares the desktop.
func UserKey(tenantID, userID string) (string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", fmt.Errorf("%w: user is required", ErrInvalid)
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(tenantID) + "\x00" + userID))
	return hex.EncodeToString(sum[:8]), nil
}

func ValidUserKey(key string) bool {
	return userKeyPattern.MatchString(key)
}

func ValidUserID(raw string) bool {
	return userPattern.MatchString(strings.TrimSpace(raw))
}

func ValidDisplay(display string) bool {
	return displayPattern.MatchString(strings.TrimSpace(display))
}

const (
	maxOpenProgramBytes = 256
	maxOpenArgBytes     = 1024
)

// openProgramName is one path segment of a program app_open may start.
// A shell metacharacter, a space, or ".." does not match.
var openProgramName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

// openProgramBlocked is the basename app_open refuses. A shell or an
// interpreter would be a headless command. A package tool belongs to
// action=install. A browser has to stay the supervised Chromium, which
// the browser steps already drive.
func openProgramBlocked(base string) bool {
	switch strings.ToLower(base) {
	case "sh", "bash", "dash", "ash", "zsh", "ksh", "mksh", "csh", "tcsh", "fish", "busybox", "rbash",
		"env", "nice", "nohup", "setsid", "timeout", "stdbuf", "xargs", "sudo", "su", "doas", "pkexec",
		"python", "python2", "python3", "perl", "ruby", "node", "nodejs", "lua", "php",
		"apt", "apt-get", "dpkg",
		"xdg-open", "gio", "gtk-launch",
		"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "chrome",
		"firefox", "firefox-esr", "maclaw-browser", "x-www-browser", "sensible-browser":
		return true
	default:
		return false
	}
}

// ProgramArgs reads the argv array of one app_open. A missing array is
// an empty argv. A string would be a shell line, so it is rejected.
func ProgramArgs(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	switch typed := raw.(type) {
	case []string:
		return append([]string(nil), typed...), nil
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%w: program argument is invalid", ErrInvalid)
			}
			out = append(out, text)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%w: program argument is invalid", ErrInvalid)
	}
}

// OpenArgv checks the program and its arguments. The program is one name
// or an absolute path. The result is the argv docker exec runs directly.
func OpenArgv(program string, args []string) ([]string, error) {
	program = strings.TrimSpace(program)
	if program == "" {
		return nil, fmt.Errorf("%w: program is required", ErrInvalid)
	}
	if len(program) > maxOpenProgramBytes || strings.ContainsAny(program, "\r\n\x00 \t") || strings.HasPrefix(program, "-") {
		return nil, fmt.Errorf("%w: program is invalid", ErrInvalid)
	}
	base, err := openProgramBase(program)
	if err != nil {
		return nil, err
	}
	if openProgramBlocked(base) {
		return nil, fmt.Errorf("%w: a shell, interpreter, package tool, or browser cannot be started with app_open", ErrInvalid)
	}
	if len(args) > MaxOpenArgs {
		return nil, fmt.Errorf("%w: too many program arguments", ErrInvalid)
	}
	out := make([]string, 0, 1+len(args))
	out = append(out, program)
	for _, arg := range args {
		if arg == "" || len(arg) > maxOpenArgBytes || strings.ContainsAny(arg, "\r\n\x00") {
			return nil, fmt.Errorf("%w: program argument is invalid", ErrInvalid)
		}
		out = append(out, arg)
	}
	return out, nil
}

func openProgramBase(program string) (string, error) {
	if strings.Contains(program, "/") {
		if !strings.HasPrefix(program, "/") || strings.Contains(program, "//") {
			return "", fmt.Errorf("%w: program is invalid", ErrInvalid)
		}
		parts := strings.Split(program, "/")
		if len(parts) < 3 {
			return "", fmt.Errorf("%w: program is invalid", ErrInvalid)
		}
		for i, part := range parts {
			if i == 0 {
				continue
			}
			if !openProgramName.MatchString(part) {
				return "", fmt.Errorf("%w: program is invalid", ErrInvalid)
			}
		}
		return parts[len(parts)-1], nil
	}
	if !openProgramName.MatchString(program) {
		return "", fmt.Errorf("%w: program is invalid", ErrInvalid)
	}
	return program, nil
}

// displayNumber is the X display the supervisor started. ":020" and ":20"
// are the same display, and the session bus socket uses the integer.
func displayNumber(display string) (int, error) {
	display = strings.TrimSpace(display)
	if !ValidDisplay(display) {
		return 0, fmt.Errorf("%w: display is invalid", ErrInvalid)
	}
	n, err := strconv.Atoi(display[1:])
	if err != nil || n < 0 || n > 999 {
		return 0, fmt.Errorf("%w: display is invalid", ErrInvalid)
	}
	return n, nil
}

// SessionBusAddress is the D-Bus socket the desktop supervisor listens on
// for this display (/tmp/.maclaw-dbus-20 for :20). GTK programs such as
// xfce4-terminal need it. xterm only needs DISPLAY.
func SessionBusAddress(display string) (string, error) {
	n, err := displayNumber(display)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("unix:path=/tmp/.maclaw-dbus-%d", n), nil
}

// OpenExecArgs is docker exec -d of a checked program. -d returns as soon
// as the process is started, so a GUI is not waited on. There is no shell.
// The working directory is the desktop home: a terminal's shell inherits
// it, so a later compile writes there instead of the container root.
func OpenExecArgs(container, display, program string, args []string) ([]string, error) {
	argv, err := OpenArgv(program, args)
	if err != nil {
		return nil, err
	}
	container = strings.TrimSpace(container)
	if container == "" || strings.ContainsAny(container, " \t\r\n\x00") || strings.HasPrefix(container, "-") {
		return nil, fmt.Errorf("%w: container is invalid", ErrInvalid)
	}
	n, err := displayNumber(display)
	if err != nil {
		return nil, err
	}
	bus, err := SessionBusAddress(display)
	if err != nil {
		return nil, err
	}
	// The image defaults to fcitx pinyin. app_run types with xdotool key
	// events, and a client of that input method turns ASCII such as g++
	// into pinyin. These overrides apply only to the program being
	// started. gtk-im-context-simple and compose are in-process: xim would
	// look for the fcitx XIM server that @im=none turns off. The browser
	// keeps fcitx.
	command := []string{
		"exec", "-d",
		"-w", DesktopHome,
		"-e", fmt.Sprintf("DISPLAY=:%d", n),
		"-e", "DBUS_SESSION_BUS_ADDRESS=" + bus,
		"-e", "HOME=" + DesktopHome,
		"-e", "XMODIFIERS=@im=none",
		"-e", "GTK_IM_MODULE=gtk-im-context-simple",
		"-e", "QT_IM_MODULE=compose",
		"-e", "SDL_IM_MODULE=",
		container,
	}
	return append(command, detachTerminal(argv)...), nil
}

// detachTerminal starts xfce4-terminal as its own process. Without
// --disable-server the client asks the session bus for an existing
// terminal and exits. That process still has fcitx, so the input-method
// overrides above never reach the window. Skipping registration also
// leaves a terminal the person opened on fcitx.
func detachTerminal(argv []string) []string {
	if len(argv) == 0 {
		return argv
	}
	base := argv[0]
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	if !strings.EqualFold(base, "xfce4-terminal") {
		return argv
	}
	for _, arg := range argv[1:] {
		if arg == "--disable-server" || arg == "-D" {
			return argv
		}
	}
	out := make([]string, 0, len(argv)+1)
	out = append(out, argv[0], "--disable-server")
	return append(out, argv[1:]...)
}

// OpenedText is the tool result after a program has been started.
func OpenedText(program string) string {
	return "opened " + strings.TrimSpace(program)
}

// AppFailureText explains an xdotool exit 1 that printed nothing else.
// That is a search for a window which is not open. A display error, or
// exit status 10 and 127, stays visible: those are not a missing window.
func AppFailureText(output string, err error) string {
	text := strings.TrimSpace(output)
	if text == "" && err != nil {
		text = strings.TrimSpace(err.Error())
	}
	detail := stripExitStatus(text)
	if detail == "" {
		if isExitStatusOne(text) {
			return AppWindowMiss
		}
		if text == "" {
			return "app_run failed"
		}
		return clipFailure(text)
	}
	return clipFailure(detail)
}

// OpenFailureText explains a program that was not started. Docker's
// "executable file not found" is a missing package. Other "no such file"
// failures, such as the desktop home not existing yet, keep their text.
func OpenFailureText(program, output string, err error) string {
	text := strings.TrimSpace(output)
	if text == "" && err != nil {
		text = strings.TrimSpace(err.Error())
	}
	name := strings.TrimSpace(program)
	if name == "" {
		name = "program"
	}
	if strings.Contains(text, "executable file not found") {
		return "program is not installed in this desktop: " + name
	}
	if text == "" {
		return "app_open failed"
	}
	return clipFailure(text)
}

// isExitStatusOne reports Go's exact exit status 1. "exit status 127"
// contains the digits of status 1 and is a different failure.
func isExitStatusOne(text string) bool {
	const phrase = "exit status 1"
	for i := 0; i < len(text); {
		j := strings.Index(text[i:], phrase)
		if j < 0 {
			return false
		}
		j += i
		end := j + len(phrase)
		if end >= len(text) || text[end] < '0' || text[end] > '9' {
			return true
		}
		i = end
	}
	return false
}

func stripExitStatus(text string) string {
	for {
		i := strings.Index(text, "exit status ")
		if i < 0 {
			break
		}
		end := i + len("exit status ")
		for end < len(text) && text[end] >= '0' && text[end] <= '9' {
			end++
		}
		text = text[:i] + " " + text[end:]
	}
	for _, prefix := range []string{"desktop app command failed:", "docker request failed:"} {
		text = strings.ReplaceAll(text, prefix, " ")
	}
	return strings.Trim(strings.TrimSpace(text), ":")
}

func clipFailure(text string) string {
	runes := []rune(text)
	if len(runes) > AptFailureRunes {
		text = strings.TrimSpace(string(runes[len(runes)-AptFailureRunes:]))
	}
	if text == "" {
		return "app_open failed"
	}
	return text
}

// NormalizeResources fills defaults and checks image, memory, cpu, and shm.
func NormalizeResources(image, memory, cpus, shm string) (Resources, error) {
	if strings.TrimSpace(image) == "" {
		image = DefaultImage
	}
	if strings.TrimSpace(memory) == "" {
		memory = DefaultMemory
	}
	if strings.TrimSpace(cpus) == "" {
		cpus = DefaultCPUs
	}
	if strings.TrimSpace(shm) == "" {
		shm = DefaultShmSize
	}
	var err error
	if image, err = NormalizeImage(image); err != nil {
		return Resources{}, err
	}
	if memory, err = NormalizeMemory(memory); err != nil {
		return Resources{}, err
	}
	if cpus, err = NormalizeCPUs(cpus); err != nil {
		return Resources{}, err
	}
	if shm, err = NormalizeMemory(shm); err != nil {
		return Resources{}, fmt.Errorf("%w: shm_size is invalid", ErrInvalid)
	}
	return Resources{Image: image, Memory: memory, CPUs: cpus, ShmSize: shm}, nil
}

func NormalizeImage(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !imagePattern.MatchString(raw) {
		return "", fmt.Errorf("%w: image is invalid", ErrInvalid)
	}
	return raw, nil
}

func NormalizeMemory(raw string) (string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if !memoryPattern.MatchString(raw) {
		return "", fmt.Errorf("%w: memory is invalid", ErrInvalid)
	}
	return raw, nil
}

func NormalizeCPUs(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 || value > 64 {
		return "", fmt.Errorf("%w: cpus is invalid", ErrInvalid)
	}
	return strconv.FormatFloat(value, 'f', -1, 64), nil
}

// ParseEnsure reads the supervisor's JSON status line.
func ParseEnsure(out string) (EnsureStatus, error) {
	var status EnsureStatus
	found := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var payload struct {
			Display string `json:"display"`
			CDPPort int    `json:"cdp_port"`
			Token   string `json:"token"`
		}
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			return EnsureStatus{}, fmt.Errorf("%w: desktop supervisor returned invalid status", ErrInvalid)
		}
		status = EnsureStatus{Display: payload.Display, CDPPort: payload.CDPPort, Token: payload.Token}
		found = true
	}
	if !found || !ValidDisplay(status.Display) || status.CDPPort <= 0 || status.CDPPort > 65535 {
		return EnsureStatus{}, fmt.Errorf("%w: desktop supervisor returned no endpoint", ErrInvalid)
	}
	// An older image without token gates reports no token; callers fall back
	// to the legacy tokenless URL in that case.
	if status.Token != "" && !ValidGateToken(status.Token) {
		return EnsureStatus{}, fmt.Errorf("%w: desktop supervisor returned an invalid token", ErrInvalid)
	}
	return status, nil
}

// ValidGateToken matches the hex tokens the in-container supervisor issues for
// its CDP and noVNC gates.
func ValidGateToken(token string) bool {
	return gateTokenPattern.MatchString(token)
}

var (
	imagePattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,200}$`)
	memoryPattern    = regexp.MustCompile(`(?i)^[1-9][0-9]{0,6}([kmg]i?b?)?$`)
	userPattern      = regexp.MustCompile(`^[A-Za-z0-9_.:@+-]{1,200}$`)
	userKeyPattern   = regexp.MustCompile(`^[0-9a-f]{16}$`)
	gateTokenPattern = regexp.MustCompile(`^[0-9a-f]{16,64}$`)
	displayPattern   = regexp.MustCompile(`^:[0-9]{1,3}$`)
)

// MaxScreenshotBytes bounds one decoded desktop screenshot. A 1440x900 PNG is
// usually well under 2MB; anything far larger is not a screenshot.
const MaxScreenshotBytes = 16 << 20

// ScreenshotScript runs inside a desktop container (sh -c, DISPLAY set) and
// prints the root window as base64 PNG. ImageMagick import is preferred
// (maclaw-gui:2); scrot covers maclaw-gui:1. Base64 keeps the image intact
// through runners that return combined text output, and tool diagnostics go
// to /dev/null so they cannot corrupt it. The temp file is removed. Nothing
// is left on the desktop unless ScreenshotScriptFor is given a file name.
const ScreenshotScript = `d=$(mktemp -d) || exit 1
f="$d/screen.png"
if command -v import >/dev/null 2>&1; then
  import -window root "png:$f" >/dev/null 2>&1
elif command -v scrot >/dev/null 2>&1; then
  scrot -o "$f" >/dev/null 2>&1
else
  false
fi
s=$?
if [ "$s" -eq 0 ]; then base64 -w0 "$f"; s=$?; fi
rm -rf "$d"
exit "$s"`

// desktopShotDir is the folder the person sees as ~/Desktop.
const desktopShotDir = DesktopHome + "/Desktop"

// DesktopShotFileName accepts one PNG file name. An empty name means the
// screenshot is not written anywhere. A slash, a .. segment, or any other
// suffix is rejected so the name can be placed in the capture script.
func DesktopShotFileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	if len(name) > 64 || strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) || !strings.HasSuffix(name, ".png") {
		return "", fmt.Errorf("desktop screenshot name is invalid")
	}
	base := strings.TrimSuffix(name, ".png")
	if base == "" || strings.HasPrefix(base, ".") {
		return "", fmt.Errorf("desktop screenshot name is invalid")
	}
	for _, r := range base {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' && r != '.' {
			return "", fmt.Errorf("desktop screenshot name is invalid")
		}
	}
	return name, nil
}

// DesktopShotPath is the absolute path a saved screenshot uses.
func DesktopShotPath(name string) string {
	safe, err := DesktopShotFileName(name)
	if err != nil || safe == "" {
		return ""
	}
	return desktopShotDir + "/" + safe
}

// ScreenshotScriptFor is ScreenshotScript, and when name is set it copies the
// PNG onto the desktop before the temp directory is removed. A failed copy
// fails the script, so a later Saved line is not printed for a missing file.
func ScreenshotScriptFor(name string) (string, error) {
	safe, err := DesktopShotFileName(name)
	if err != nil {
		return "", err
	}
	if safe == "" {
		return ScreenshotScript, nil
	}
	path := desktopShotDir + "/" + safe
	const marker = `if [ "$s" -eq 0 ]; then base64 -w0 "$f"; s=$?; fi`
	save := "mkdir -p " + desktopShotDir + ` && cp "$f" ` + path + " && chmod 644 " + path
	next := `if [ "$s" -eq 0 ]; then ` + save + ` && base64 -w0 "$f"; s=$?; fi`
	if !strings.Contains(ScreenshotScript, marker) {
		return "", fmt.Errorf("desktop screenshot script is missing its output step")
	}
	return strings.Replace(ScreenshotScript, marker, next, 1), nil
}

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// IsPNG reports whether data starts with the PNG signature.
func IsPNG(data []byte) bool {
	return bytes.HasPrefix(data, pngSignature)
}

// DecodeScreenshot turns ScreenshotScript output back into PNG bytes.
func DecodeScreenshot(out string) ([]byte, error) {
	encoded := strings.Join(strings.Fields(out), "")
	if encoded == "" || base64.StdEncoding.DecodedLen(len(encoded)) > MaxScreenshotBytes {
		return nil, fmt.Errorf("screenshot is empty or too large")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || !IsPNG(data) {
		return nil, fmt.Errorf("screenshot is not a PNG image")
	}
	return data, nil
}
