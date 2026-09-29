package tinytex

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maxPreviewSteps   = 8
	maxPackageInstall = 3
	maxSourceRepairs  = 2
	maxRepairSource   = 100 << 10
)

// CommandRunner runs one TeX tool. exitCode is the process status.
// A start failure returns err with exitCode < 0. A non-zero TeX status is not an err.
type CommandRunner func(ctx context.Context, bin, dir string, args ...string) (output string, exitCode int, err error)

// PreviewOptions drives compile, package install, and source repair.
type PreviewOptions struct {
	Engine  string
	Tlmgr   string
	Bibtex  string
	Biber   string
	Run     CommandRunner
	Install func(ctx context.Context, pkg string) error
	// Repair returns a replacement for the file that failed to compile.
	// ok is false when the model reply is not a safe replacement.
	Repair  func(ctx context.Context, path, source, excerpt string) (replacement string, ok bool, err error)
	OnPhase func(phase, message string)
}

// PreviewResult is a finished or failed compile.
type PreviewResult struct {
	PDFPath  string
	Repaired bool
	Log      string
	Message  string
}

var (
	missingFileRe  = regexp.MustCompile("(?i)file `([^`]+\\.(?:sty|cls|def|fd|clo|bbx|cbx))' not found")
	missingImageRe = regexp.MustCompile("(?i)file `([^`]*example-image[^`']*)' not found")
	missingBstRe   = regexp.MustCompile(`(?i)couldn't open style file\s+(\S+)`)
	texErrorRe     = regexp.MustCompile(`(?m)^(?:\()?((?:[A-Za-z]:)?[^:\r\n]*?\.tex):(\d+):`)
	pkgNameRe      = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{1,63}$`)
)

// MissingPackages lists TeX files the log says are absent. User \input files are not included.
func MissingPackages(log string) []string {
	seen := map[string]struct{}{}
	var out []string
	collect := func(name string) {
		name = filepath.Base(strings.TrimSpace(name))
		if name == "" || name == "." {
			return
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	for _, match := range missingFileRe.FindAllStringSubmatch(log, -1) {
		collect(match[1])
	}
	// Publisher samples (elsarticle) include the standard mwe placeholder.
	// Those images are not style files, so the package rule above misses them.
	for _, match := range missingImageRe.FindAllStringSubmatch(log, -1) {
		collect(match[1])
	}
	return out
}

// PackageNameFromFile maps amsmath.sty to the tlmgr package amsmath.
func PackageNameFromFile(file string) string {
	base := filepath.Base(strings.TrimSpace(file))
	name := strings.TrimSuffix(base, filepath.Ext(base))
	lower := strings.ToLower(name)
	if lower == "example-image" || strings.HasPrefix(lower, "example-image-") {
		return "mwe"
	}
	if !pkgNameRe.MatchString(name) {
		return ""
	}
	return lower
}

// MissingBibStyles lists bibliography styles BibTeX could not open.
func MissingBibStyles(log string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, match := range missingBstRe.FindAllStringSubmatch(log, -1) {
		name := strings.Trim(match[1], "`'\".;,")
		if !strings.EqualFold(filepath.Ext(name), ".bst") {
			continue
		}
		name = filepath.Base(name)
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	return out
}

func bibOpenFailure(log string) string {
	for _, line := range strings.Split(log, "\n") {
		trim := strings.TrimSpace(line)
		lower := strings.ToLower(trim)
		if strings.Contains(lower, "couldn't open style file") || strings.Contains(lower, "couldn't open database file") {
			return trim
		}
	}
	return ""
}

func runBibliography(ctx context.Context, opt PreviewOptions, bin, label, root, arg string, installed *int, pkgDone map[string]struct{}) string {
	run := func() string {
		phase(opt, "bibtex", label)
		out, _, err := opt.Run(ctx, bin, root, arg)
		if err != nil && strings.TrimSpace(out) == "" {
			return err.Error()
		}
		return out
	}
	out := run()
	if opt.Install == nil || installed == nil {
		return out
	}
	installedAny := false
	for _, file := range MissingBibStyles(out) {
		if *installed >= maxPackageInstall {
			break
		}
		pkg := PackageNameFromFile(file)
		if pkg == "" {
			continue
		}
		if _, seen := pkgDone[pkg]; seen {
			continue
		}
		pkgDone[pkg] = struct{}{}
		*installed++
		phase(opt, "installing", pkg)
		if err := opt.Install(ctx, pkg); err == nil {
			installedAny = true
		}
	}
	if !installedAny {
		return out
	}
	return run()
}

// NeedsBibtex reports a BibTeX rerun requested by the log.
func NeedsBibtex(log string) bool {
	lower := strings.ToLower(log)
	return strings.Contains(lower, "rerun bibtex") || strings.Contains(lower, "(re)run bibtex") || strings.Contains(lower, "undefined citations")
}

// NeedsBiber reports a Biber rerun requested by the log.
func NeedsBiber(log string) bool {
	lower := strings.ToLower(log)
	return strings.Contains(lower, "rerun biber") || strings.Contains(lower, "(re)run biber")
}

// NeedsRerun reports cross-references, citations or outlines that need another engine pass.
func NeedsRerun(log string) bool {
	lower := strings.ToLower(log)
	return strings.Contains(lower, "rerun to get cross-references right") ||
		strings.Contains(lower, "rerun to get citations correct") ||
		strings.Contains(lower, "rerun to get outlines right") ||
		strings.Contains(lower, "please rerun latex") ||
		strings.Contains(lower, "please (re)run latex")
}

// ErrorExcerpt keeps the lines a repair model needs.
func ErrorExcerpt(log string) string {
	var b strings.Builder
	n := 0
	for _, line := range strings.Split(log, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "!") || strings.Contains(trim, ".tex:") || strings.Contains(trim, "not found") {
			b.WriteString(trim)
			b.WriteByte('\n')
			n++
			if n >= 40 {
				break
			}
		}
	}
	if b.Len() == 0 {
		return clip(log, 2000)
	}
	return clip(b.String(), 4000)
}

// ErrorFile is the .tex named by file:line errors, limited to root.
func ErrorFile(root, log, fallback string) string {
	root = filepath.Clean(root)
	for _, match := range texErrorRe.FindAllStringSubmatch(log, 8) {
		name := filepath.Clean(match[1])
		if filepath.IsAbs(name) {
			if withinDir(root, name) {
				if st, err := os.Stat(name); err == nil && !st.IsDir() {
					return name
				}
			}
			continue
		}
		candidate := filepath.Join(root, name)
		if !withinDir(root, candidate) {
			continue
		}
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
	}
	return fallback
}

// ResolveMain returns the file that contains \documentclass.
// A chapter \input is compiled through the parent that includes it.
func ResolveMain(path string) (string, error) {
	path = filepath.Clean(path)
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if declaresDocument(string(body)) {
		return path, nil
	}
	dir := filepath.Dir(path)
	for depth := 0; depth < 5; depth++ {
		if main := findIncludingMain(dir, path, depth); main != "" {
			return main, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return path, nil
}

// declaresDocument reports a document-class command at the start of a line.
// A comment or a sentence that mentions the command is not a main file.
func declaresDocument(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if lineStartsDocument(texCodePrefix(strings.TrimRight(line, "\r"))) {
			return true
		}
	}
	return false
}

func texCodePrefix(line string) string {
	var b strings.Builder
	backslashes := 0
	for _, r := range line {
		if r == '%' && backslashes%2 == 0 {
			break
		}
		b.WriteRune(r)
		if r == '\\' {
			backslashes++
		} else {
			backslashes = 0
		}
	}
	return strings.TrimSpace(b.String())
}

func lineStartsDocument(code string) bool {
	return strings.HasPrefix(code, `\documentclass`) || strings.HasPrefix(code, `\documentstyle`)
}

func findIncludingMain(dir, opened string, depth int) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".tex") {
			continue
		}
		candidate := filepath.Join(dir, entry.Name())
		if filepath.Clean(candidate) == filepath.Clean(opened) {
			continue
		}
		raw, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		text := string(raw)
		if declaresDocument(text) && referencesOpened(text, opened, candidate, depth) {
			return candidate
		}
	}
	return ""
}

func referencesOpened(body, opened, candidate string, depth int) bool {
	names := make([]string, 0, 4)
	if depth <= 1 {
		base := strings.TrimSuffix(filepath.Base(opened), filepath.Ext(opened))
		if base != "" && base != "." {
			names = append(names, base, base+".tex")
		}
	}
	if rel, err := filepath.Rel(filepath.Dir(candidate), opened); err == nil {
		rel = filepath.ToSlash(rel)
		if rel != "" && rel != "." && !strings.HasPrefix(rel, "../") && rel != ".." {
			names = append(names, rel)
			if stem := strings.TrimSuffix(rel, filepath.Ext(rel)); stem != rel {
				names = append(names, stem)
			}
		}
	}
	seen := map[string]struct{}{}
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		for _, cmd := range []string{`\input{`, `\include{`, `\subfile{`} {
			if strings.Contains(body, cmd+name+`}`) {
				return true
			}
		}
	}
	return false
}

// AcceptRepairedSource extracts a fenced reply and rejects truncated replacements.
func AcceptRepairedSource(original, reply string) (string, bool) {
	body := extractLatexFence(reply)
	body = strings.TrimSpace(body)
	if body == "" {
		return "", false
	}
	orig := strings.TrimSpace(original)
	if declaresDocument(orig) && !declaresDocument(body) {
		return "", false
	}
	if len(body) < 20 {
		return "", false
	}
	if len(orig) > 400 && len(body)*2 < len(orig) {
		return "", false
	}
	return body, true
}

func extractLatexFence(reply string) string {
	reply = strings.TrimSpace(reply)
	start := strings.Index(reply, "```")
	if start < 0 {
		return reply
	}
	rest := reply[start+3:]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[nl+1:]
	}
	if end := strings.LastIndex(rest, "```"); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// Preview compiles texPath, installs missing packages, reruns bibliography,
// and applies up to two source repairs.
func Preview(ctx context.Context, texPath string, opt PreviewOptions) (PreviewResult, error) {
	if opt.Run == nil || strings.TrimSpace(opt.Engine) == "" {
		return PreviewResult{Message: "未找到 xelatex"}, errNoEngine
	}
	main, err := ResolveMain(texPath)
	if err != nil {
		return PreviewResult{Message: "无法读取 LaTeX 文件"}, err
	}
	root := filepath.Dir(main)
	var (
		lastLog   string
		repaired  bool
		installed int
		repairs   int
		bibRan    bool
		reruns    int
		pkgDone   = map[string]struct{}{}
	)
	for step := 0; step < maxPreviewSteps; step++ {
		if err := ctx.Err(); err != nil {
			return PreviewResult{Log: clip(lastLog, 4000), Message: "编译已取消"}, err
		}
		phase(opt, "compiling", filepath.Base(main))
		log, code, runErr := opt.Run(ctx, opt.Engine, root, "-interaction=nonstopmode", "-halt-on-error", "-file-line-error", filepath.Base(main))
		lastLog = log
		if runErr != nil {
			return PreviewResult{Log: clip(lastLog, 4000), Message: "xelatex 无法启动"}, runErr
		}
		if code == 0 {
			pdf := strings.TrimSuffix(main, filepath.Ext(main)) + ".pdf"
			if !bibRan {
				bin, label := "", ""
				if NeedsBiber(log) && opt.Biber != "" {
					bin, label = opt.Biber, "biber"
				} else if NeedsBibtex(log) && opt.Bibtex != "" {
					bin, label = opt.Bibtex, "bibtex"
				}
				if bin != "" {
					bibRan = true
					arg := strings.TrimSuffix(filepath.Base(main), filepath.Ext(main))
					bibOut := runBibliography(ctx, opt, bin, label, root, arg, &installed, pkgDone)
					if line := bibOpenFailure(bibOut); line != "" {
						return PreviewResult{Log: clip(bibOut, 4000), Message: line}, errCompileFailed
					}
					if strings.TrimSpace(bibOut) != "" {
						lastLog = strings.TrimSpace(log) + "\n" + strings.TrimSpace(bibOut)
					}
					continue
				}
			}
			if NeedsRerun(log) && reruns < 2 {
				reruns++
				continue
			}
			if st, statErr := os.Stat(pdf); statErr == nil && st.Size() > 0 && !st.IsDir() {
				return PreviewResult{PDFPath: pdf, Repaired: repaired, Log: clip(lastLog, 4000)}, nil
			}
		}

		didInstall := false
		if installed < maxPackageInstall && opt.Install != nil {
			for _, file := range MissingPackages(log) {
				pkg := PackageNameFromFile(file)
				if pkg == "" {
					continue
				}
				if _, seen := pkgDone[pkg]; seen {
					continue
				}
				pkgDone[pkg] = struct{}{}
				installed++
				phase(opt, "installing", pkg)
				if err := opt.Install(ctx, pkg); err == nil {
					didInstall = true
				}
				if installed >= maxPackageInstall {
					break
				}
			}
		}
		if didInstall {
			continue
		}
		if repairs >= maxSourceRepairs || opt.Repair == nil {
			break
		}
		target := ErrorFile(root, log, main)
		source, readErr := readRepairSource(target)
		if readErr != nil {
			break
		}
		phase(opt, "repairing", filepath.Base(target))
		replacement, ok, repairErr := opt.Repair(ctx, target, source, ErrorExcerpt(log))
		repairs++
		if repairErr != nil || !ok {
			if repairErr != nil {
				break
			}
			continue
		}
		if err := writeRepaired(target, source, replacement); err != nil {
			return PreviewResult{Log: clip(lastLog, 4000), Message: "无法写回修复后的源文件"}, err
		}
		repaired = true
	}
	message := "编译失败"
	if excerpt := ErrorExcerpt(lastLog); excerpt != "" {
		message = excerpt
	}
	return PreviewResult{Repaired: repaired, Log: clip(lastLog, 4000), Message: message}, errCompileFailed
}

func phase(opt PreviewOptions, name, message string) {
	if opt.OnPhase != nil {
		opt.OnPhase(name, message)
	}
}

func readRepairSource(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, maxRepairSource+1)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return "", err
	}
	if n > maxRepairSource {
		return "", errSourceTooLarge
	}
	return string(buf[:n]), nil
}

func writeRepaired(path, original, replacement string) error {
	bak := path + ".maclaw-bak"
	if _, err := os.Stat(bak); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err := WriteAtomic(bak, []byte(original)); err != nil {
			return err
		}
	}
	return WriteAtomic(path, []byte(replacement))
}

// WriteAtomic replaces dest only after the new bytes are flushed. The
// temporary file stays under .maclaw, which cloud sync already ignores.
func WriteAtomic(dest string, data []byte) error {
	stage := filepath.Join(filepath.Dir(dest), ".maclaw")
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(stage, "tex-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return RenameReplacing(tmpName, dest)
}

func withinDir(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
