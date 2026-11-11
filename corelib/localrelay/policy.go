package localrelay

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Root is one folder the person allowed. Write false is read-only.
type Root struct {
	Path  string `json:"path"`
	Write bool   `json:"write"`
}

// Settings is the person's policy for bots on this computer. It lives on
// this computer (the GUI's data folder), never on Hub.
type Settings struct {
	FilesEnabled bool   `json:"files_enabled"`
	Roots        []Root `json:"roots"`
	// MaxTransferMB caps one file in either direction.
	MaxTransferMB  int  `json:"max_transfer_mb"`
	NetworkEnabled bool `json:"network_enabled"`
	// NetAllow are targets the person pre-approved: "192.168.1.0/24",
	// "10.0.0.5:22", "gpu.lab:22,8888", "*.lan". Anything else asks first.
	NetAllow []string `json:"net_allow"`
	// TunnelIdleMinutes closes a tunnel nobody used for this long.
	TunnelIdleMinutes int `json:"tunnel_idle_minutes"`
	// ConfirmSeconds is how long a question waits for the person.
	ConfirmSeconds int `json:"confirm_seconds"`
	// Approved are "always allow" answers: "<bot id>|<class>".
	Approved []string `json:"approved"`
}

const (
	DefaultMaxTransferMB     = 200
	HardMaxTransferMB        = 4096
	DefaultTunnelIdleMinutes = 30
	DefaultConfirmSeconds    = 120
	// BaseFolder is the folder under the home directory where bots write.
	BaseFolder = "MaClaw"
)

// DefaultSettings: bots may write only in ~/MaClaw (each bot gets its own
// subfolder) and read Downloads, Desktop and Documents. The network relay is
// on, but every target asks the person first until they pre-approve it.
func DefaultSettings(home string) Settings {
	s := Settings{
		FilesEnabled:      true,
		MaxTransferMB:     DefaultMaxTransferMB,
		NetworkEnabled:    true,
		TunnelIdleMinutes: DefaultTunnelIdleMinutes,
		ConfirmSeconds:    DefaultConfirmSeconds,
	}
	if strings.TrimSpace(home) != "" {
		s.Roots = []Root{
			{Path: filepath.Join(home, BaseFolder), Write: true},
			{Path: filepath.Join(home, "Downloads")},
			{Path: filepath.Join(home, "Desktop")},
			{Path: filepath.Join(home, "Documents")},
		}
	}
	return s
}

// Normalize fills limits and drops empty or duplicate entries.
func (s Settings) Normalize() Settings {
	if s.MaxTransferMB <= 0 {
		s.MaxTransferMB = DefaultMaxTransferMB
	}
	if s.MaxTransferMB > HardMaxTransferMB {
		s.MaxTransferMB = HardMaxTransferMB
	}
	if s.TunnelIdleMinutes <= 0 {
		s.TunnelIdleMinutes = DefaultTunnelIdleMinutes
	}
	if s.ConfirmSeconds <= 0 {
		s.ConfirmSeconds = DefaultConfirmSeconds
	}
	if s.ConfirmSeconds > 600 {
		s.ConfirmSeconds = 600
	}
	seen := map[string]int{}
	var roots []Root
	for _, r := range s.Roots {
		p := strings.TrimSpace(r.Path)
		if p == "" || !filepath.IsAbs(p) {
			continue
		}
		p = filepath.Clean(p)
		key := foldPath(p)
		if i, ok := seen[key]; ok {
			roots[i].Write = roots[i].Write || r.Write
			continue
		}
		seen[key] = len(roots)
		roots = append(roots, Root{Path: p, Write: r.Write})
	}
	s.Roots = roots
	s.NetAllow = cleanList(s.NetAllow)
	s.Approved = cleanList(s.Approved)
	return s
}

func cleanList(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[strings.ToLower(v)] {
			continue
		}
		seen[strings.ToLower(v)] = true
		out = append(out, v)
	}
	return out
}

// MaxBytes is the per-file cap in bytes.
func (s Settings) MaxBytes() int64 {
	return int64(s.Normalize().MaxTransferMB) << 20
}

// BotFolder is the bot's own writable folder: <first writable root>/<bot>.
// A first writable root named MaClaw is preferred.
func (s Settings) BotFolder(botName, botID string) string {
	var base string
	for _, r := range s.Roots {
		if r.Write && strings.EqualFold(filepath.Base(r.Path), BaseFolder) {
			base = r.Path
			break
		}
	}
	if base == "" {
		for _, r := range s.Roots {
			if r.Write {
				base = r.Path
				break
			}
		}
	}
	if base == "" {
		return ""
	}
	name := SafeName(botName)
	if name == "" {
		name = SafeName(botID)
	}
	if name == "" {
		name = "bot"
	}
	return filepath.Join(base, name)
}

// Access is what a request does to a path.
type Access string

const (
	AccessRead  Access = "read"
	AccessWrite Access = "write"
)

// PathDecision is the policy answer for one path.
type PathDecision struct {
	// Resolved is the cleaned path with symlinks resolved (for a write, the
	// folder's resolved path).
	Resolved string
	// Root is the allowed root containing Resolved, "" when outside.
	Root string
	// Class is what one "always allow" answer covers.
	Class string
	// Outside is true when no allowed root covers this access.
	Outside bool
}

// ExpandHome turns a leading ~ into home.
func ExpandHome(p, home string) string {
	p = strings.TrimSpace(p)
	if home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		return filepath.Join(home, p[2:])
	}
	return p
}

// CheckPath resolves p (absolute after ~ expansion) and decides where it
// stands. The path must exist for a read; for a write p is the destination
// folder and must exist. Symlinks are resolved before the root test, so a
// link inside an allowed folder that points elsewhere counts as elsewhere.
func (s Settings) CheckPath(p, home string, access Access) (PathDecision, error) {
	p = ExpandHome(p, home)
	if p == "" || strings.ContainsRune(p, 0) || !utf8.ValidString(p) {
		return PathDecision{}, &Err{Code: CodeInvalid, Message: "the path is empty or invalid"}
	}
	if !filepath.IsAbs(p) {
		return PathDecision{}, &Err{Code: CodeInvalid, Message: fmt.Sprintf("use a full path on this computer, not %q", p)}
	}
	clean := filepath.Clean(p)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		if os.IsNotExist(err) {
			return PathDecision{}, &Err{Code: CodeNotFound, Message: fmt.Sprintf("%s does not exist on this computer", clean)}
		}
		return PathDecision{}, &Err{Code: CodeFailed, Message: fmt.Sprintf("cannot open %s: %v", clean, err)}
	}
	if !filepath.IsAbs(resolved) {
		resolved, _ = filepath.Abs(resolved)
	}
	out := PathDecision{Resolved: resolved, Outside: true}
	best := -1
	for _, r := range s.Roots {
		if access == AccessWrite && !r.Write {
			continue
		}
		rootReal := r.Path
		if real, err := filepath.EvalSymlinks(r.Path); err == nil {
			rootReal = real
		}
		if Within(rootReal, resolved) && len(rootReal) > best {
			best = len(rootReal)
			out.Root = r.Path
			out.Outside = false
		}
	}
	if out.Outside {
		out.Class = "path:" + string(access) + ":" + foldPath(resolved)
	} else {
		out.Class = "root:" + string(access) + ":" + foldPath(out.Root)
	}
	return out, nil
}

// Within reports whether p is root or below it. Both must be clean and
// absolute. Case is ignored on Windows and macOS.
func Within(root, p string) bool {
	root, p = foldPath(filepath.Clean(root)), foldPath(filepath.Clean(p))
	if root == p {
		return true
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func foldPath(p string) string {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.ToLower(p)
	}
	return p
}

var unsafeName = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// SafeName keeps one file name: no folders, no characters Windows refuses,
// no leading dots that would hide it, at most 180 bytes.
func SafeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = unsafeName.ReplaceAllString(name, "_")
	name = strings.Trim(name, " .")
	if name == "" || name == "." || name == ".." {
		return ""
	}
	switch strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name))) {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "LPT1", "LPT2", "LPT3":
		name = "_" + name
	}
	for len(name) > 180 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}

// CreateUnique creates dir/name, or "name (1).ext", "name (2).ext", ... when
// that exists. O_EXCL makes it impossible to overwrite a file that appeared
// meanwhile. The caller writes and closes the file.
func CreateUnique(dir, name string) (*os.File, string, error) {
	name = SafeName(name)
	if name == "" {
		name = "file"
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; i < 1000; i++ {
		candidate := name
		if i > 0 {
			candidate = stem + " (" + strconv.Itoa(i) + ")" + ext
		}
		full := filepath.Join(dir, candidate)
		f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return f, full, nil
		}
		if os.IsExist(err) {
			continue
		}
		return nil, "", err
	}
	return nil, "", fmt.Errorf("too many files named %s", name)
}

// NetDecision is the policy answer for one network target.
type NetDecision struct {
	// IP is the address to dial: the one that was checked, so a later DNS
	// answer cannot swap the target.
	IP      net.IP
	Addr    string
	Allowed bool // pre-approved by a NetAllow rule
	Class   string
	Private bool // RFC1918, ULA, link-local or loopback
	Local   bool // loopback: this computer itself
}

// CheckNet resolves host with lookup (net.DefaultResolver when nil) and
// matches the NetAllow rules.
func (s Settings) CheckNet(host string, port int, lookup func(string) ([]net.IP, error)) (NetDecision, error) {
	host = strings.TrimSpace(strings.Trim(strings.TrimSpace(host), "[]"))
	if host == "" || port <= 0 || port > 65535 || strings.ContainsAny(host, " /\\@") {
		return NetDecision{}, &Err{Code: CodeInvalid, Message: "host or port is invalid"}
	}
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		if lookup == nil {
			lookup = net.LookupIP
		}
		got, err := lookup(host)
		if err != nil || len(got) == 0 {
			return NetDecision{}, &Err{Code: CodeNotFound, Message: fmt.Sprintf("%s does not resolve on this computer's network", host)}
		}
		ips = got
	}
	sort.SliceStable(ips, func(i, j int) bool { return ips[i].To4() != nil && ips[j].To4() == nil })
	ip := ips[0]
	d := NetDecision{IP: ip, Addr: net.JoinHostPort(ip.String(), strconv.Itoa(port))}
	d.Local = ip.IsLoopback() || ip.IsUnspecified()
	d.Private = d.Local || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	d.Class = "net:" + strings.ToLower(host) + ":" + strconv.Itoa(port)
	for _, rule := range s.NetAllow {
		if MatchNetRule(rule, host, ip, port) {
			d.Allowed = true
			d.Class = "netrule:" + strings.ToLower(rule)
			break
		}
	}
	return d, nil
}

// MatchNetRule matches "cidr|ip|host|*.suffix[:port[,port...]]". A rule
// without ports allows every port.
func MatchNetRule(rule, host string, ip net.IP, port int) bool {
	rule = strings.TrimSpace(strings.ToLower(rule))
	if rule == "" {
		return false
	}
	target, ports := rule, ""
	if strings.HasPrefix(target, "[") {
		if end := strings.Index(target, "]"); end > 0 {
			ports = strings.TrimPrefix(target[end+1:], ":")
			target = target[1:end]
		}
	} else if i := strings.LastIndex(target, ":"); i >= 0 && strings.Count(target, ":") == 1 {
		target, ports = target[:i], target[i+1:]
	}
	if ports != "" {
		ok := false
		for _, p := range strings.Split(ports, ",") {
			p = strings.TrimSpace(p)
			if lo, hi, found := strings.Cut(p, "-"); found {
				l, e1 := strconv.Atoi(lo)
				h, e2 := strconv.Atoi(hi)
				if e1 == nil && e2 == nil && port >= l && port <= h {
					ok = true
				}
			} else if n, err := strconv.Atoi(p); err == nil && n == port {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if _, cidr, err := net.ParseCIDR(target); err == nil {
		return ip != nil && cidr.Contains(ip)
	}
	if rip := net.ParseIP(target); rip != nil {
		return ip != nil && rip.Equal(ip)
	}
	if strings.HasPrefix(target, "*.") {
		return strings.HasSuffix(host, target[1:])
	}
	return host == target
}
