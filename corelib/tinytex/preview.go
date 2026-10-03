package tinytex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
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

// NeedsBibtex reports a BibTeX rerun requested by the log. Natbib says
// "undefined citations". The kernel only says "Citation `key' undefined",
// and a missing .bbl is announced as "No file main.bbl". The last two clues
// have to share a line. "No file main.toc" beside "(./main.bbl)" is an opened
// bibliography, and "undefined references" beside an unrelated "citation"
// is a cross-reference.
func NeedsBibtex(log string) bool {
	lower := strings.ToLower(log)
	if strings.Contains(lower, "rerun bibtex") || strings.Contains(lower, "(re)run bibtex") || strings.Contains(lower, "undefined citations") {
		return true
	}
	for _, line := range strings.Split(lower, "\n") {
		if strings.Contains(line, "no file ") && strings.Contains(line, ".bbl") {
			return true
		}
		if strings.Contains(line, "citation") && strings.Contains(line, "undefined") {
			return true
		}
	}
	return false
}

// bibliographyNote is the short record of a bibliography that is still wrong:
// an undefined citation, or a BibTeX "Warning--" line. At most five lines.
// Ordinary TeX noise (overfull boxes, "undefined references") is not included,
// so a later up-to-date report can repeat this record without replaying the log.
func bibliographyNote(log string) string {
	var kept []string
	seen := map[string]struct{}{}
	for _, line := range strings.Split(log, "\n") {
		trim := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if !bibliographyNoteLine(trim) {
			continue
		}
		if len(trim) > 200 {
			trim = trim[:200]
		}
		key := strings.ToLower(trim)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		kept = append(kept, trim)
		if len(kept) == 5 {
			break
		}
	}
	return strings.Join(kept, "\n")
}

func bibliographyNoteLine(line string) bool {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "undefined citations") {
		return true
	}
	if strings.Contains(lower, "citation") && strings.Contains(lower, "undefined") {
		return true
	}
	return strings.Contains(lower, "warning--")
}

// bibtexSyntaxFailure is a BibTeX parse error. "I found no \citation commands"
// also exits 2 and is not one of these: a draft with a database and no \cite
// still has a usable PDF. The open-file errors are handled separately so the
// style install can retry before this runs.
func bibtexSyntaxFailure(log string) string {
	for _, line := range strings.Split(log, "\n") {
		trim := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if strings.Contains(strings.ToLower(trim), "i was expecting") {
			return trim
		}
	}
	return ""
}

func withBibNote(log, note string) string {
	note = strings.TrimSpace(note)
	if note == "" || strings.Contains(log, note) {
		return log
	}
	if strings.TrimSpace(log) == "" {
		return note
	}
	return strings.TrimSpace(log) + "\n" + note
}

var (
	auxBibdataRe  = regexp.MustCompile(`(?m)^\\bibdata\{([^{}]*)\}`)
	auxBibstyleRe = regexp.MustCompile(`(?m)^\\bibstyle\{([^{}]*)\}`)
)

// bibliographyStale reports that the .aux asks for BibTeX and the .bbl is
// missing or older than a bibliography file. A changed .bib still satisfies
// every citation key, so the log does not ask for BibTeX by itself.
func bibliographyStale(dir, stem string) bool {
	aux, err := os.ReadFile(filepath.Join(dir, stem+".aux"))
	if err != nil {
		return false
	}
	text := string(aux)
	data := auxBibdataRe.FindAllStringSubmatch(text, -1)
	if len(data) == 0 {
		return false
	}
	bblInfo, bblErr := os.Stat(filepath.Join(dir, stem+".bbl"))
	if bblErr != nil || bblInfo.IsDir() {
		return true
	}
	for _, match := range auxBibstyleRe.FindAllStringSubmatch(text, -1) {
		// A missing .bst is a TeX tree style. Only a copy next to the paper
		// can be newer than the .bbl.
		if auxInputNewer(dir, match[1], ".bst", bblInfo, false) {
			return true
		}
	}
	for _, match := range data {
		for _, name := range strings.Split(match[1], ",") {
			if auxInputNewer(dir, name, ".bib", bblInfo, true) {
				return true
			}
		}
	}
	return false
}

func auxInputNewer(dir, name, ext string, bbl os.FileInfo, missingIsStale bool) bool {
	name = strings.TrimSpace(strings.ReplaceAll(name, `\`, "/"))
	if name == "" {
		return false
	}
	if !strings.EqualFold(filepath.Ext(name), ext) {
		name += ext
	}
	path := filepath.FromSlash(name)
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return missingIsStale
	}
	return info.ModTime().After(bbl.ModTime())
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
		strings.Contains(lower, "rerun to get bibliographical references right") ||
		strings.Contains(lower, "rerun to get outlines right") ||
		strings.Contains(lower, "please rerun latex") ||
		strings.Contains(lower, "please (re)run latex")
}

// repairBlockRe is the edit the model is required to return. The marker and
// the two source line numbers are the fields; a space after the marker is
// optional. The body replaces that closed range.
var repairBlockRe = regexp.MustCompile(`(?s)@@@\s*(\d+)\s+(\d+)\s*\n(.*?)@@@`)

const repairWindowRadius = 12

// ErrorLine is the first file:line reported in a TeX log. The line is 1-based.
func ErrorLine(log string) (int, bool) {
	match := texErrorRe.FindStringSubmatch(log)
	if len(match) < 3 {
		return 0, false
	}
	n, err := strconv.Atoi(match[2])
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// splitSourceLines drops the empty element Split leaves after a final newline,
// so a window does not gain a phantom line past the last source line.
func splitSourceLines(source string) (lines []string, trailingNewline bool) {
	trailingNewline = strings.HasSuffix(source, "\n")
	lines = strings.Split(source, "\n")
	if trailingNewline && len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines, trailingNewline
}

func joinSourceLines(lines []string, trailingNewline bool) string {
	joined := strings.Join(lines, "\n")
	if trailingNewline {
		return joined + "\n"
	}
	return joined
}

// FormatRepairWindow returns a numbered slice around line. start and end are
// 1-based and inclusive.
func FormatRepairWindow(source string, line int) (start, end int, numbered string, ok bool) {
	lines, _ := splitSourceLines(source)
	if line < 1 || line > len(lines) {
		return 0, 0, "", false
	}
	start = line - repairWindowRadius
	if start < 1 {
		start = 1
	}
	end = line + repairWindowRadius
	if end > len(lines) {
		end = len(lines)
	}
	var b strings.Builder
	for i := start; i <= end; i++ {
		fmt.Fprintf(&b, "%d|%s\n", i, lines[i-1])
	}
	return start, end, b.String(), true
}

// ApplyRepairReply applies the line range named in the model reply.
// The range must cover the line the compiler reported and must sit inside
// the window that was shown to the model. Lines outside that range stay.
func ApplyRepairReply(source, excerpt, reply string) (string, bool) {
	line, ok := ErrorLine(excerpt)
	if !ok {
		return "", false
	}
	winStart, winEnd, _, ok := FormatRepairWindow(source, line)
	if !ok {
		return "", false
	}
	match := repairBlockRe.FindStringSubmatch(extractLatexFence(reply))
	if len(match) != 4 {
		return "", false
	}
	start, err1 := strconv.Atoi(match[1])
	end, err2 := strconv.Atoi(match[2])
	if err1 != nil || err2 != nil || start > end || start < winStart || end > winEnd || line < start || line > end {
		return "", false
	}
	inner := strings.Trim(match[3], "\r\n")
	var replLines []string
	if inner != "" {
		replLines = strings.Split(inner, "\n")
		for i, replLine := range replLines {
			replLines[i] = strings.TrimRight(replLine, "\r")
		}
	}
	origLines, trailingNL := splitSourceLines(source)
	if start < 1 || end > len(origLines) {
		return "", false
	}
	out := append([]string{}, origLines[:start-1]...)
	out = append(out, replLines...)
	out = append(out, origLines[end:]...)
	joined := joinSourceLines(out, trailingNL)
	if declaresDocument(source) && !declaresDocument(joined) {
		return "", false
	}
	if strings.TrimSpace(joined) == strings.TrimSpace(source) {
		return "", false
	}
	return joined, true
}

// ErrorExcerpt is the compiler's error record: the first file:line error and
// the lines TeX printed under it, through the end-of-run trailer. Warnings
// above that error stay out. The record is what a repair is shown.
func ErrorExcerpt(log string) string {
	return errorRecord(log, "", "")
}

// ErrorExcerptFor is the error record whose file:line belongs to file.
// file is the source the repair will edit, so the line number in the record
// is a line of that source. A line number from a different file is not used.
func ErrorExcerptFor(log, root, file string) string {
	if strings.TrimSpace(file) == "" {
		return ErrorExcerpt(log)
	}
	return errorRecord(log, root, file)
}

func errorRecord(log, root, file string) string {
	lines := strings.Split(log, "\n")
	start := -1
	for i, line := range lines {
		match := texErrorRe.FindStringSubmatch(line)
		if len(match) < 3 {
			continue
		}
		if file != "" && !texLogNamesFile(match[1], root, file) {
			continue
		}
		start = i
		break
	}
	if start < 0 {
		if file != "" {
			return ""
		}
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "!") {
				start = i
				break
			}
		}
	}
	if start < 0 {
		return clip(log, 2000)
	}
	var b strings.Builder
	n := 0
	for i := start; i < len(lines) && n < 40; i++ {
		line := strings.TrimRight(lines[i], "\r")
		if i > start && (texLogTrailer(line) || (file != "" && texLogOtherFile(line, root, file))) {
			break
		}
		b.WriteString(line)
		b.WriteByte('\n')
		n++
	}
	text := strings.TrimSpace(b.String())
	if text == "" {
		return clip(log, 2000)
	}
	return clip(text, 4000)
}

func texLogOtherFile(line, root, file string) bool {
	match := texErrorRe.FindStringSubmatch(line)
	return len(match) >= 3 && !texLogNamesFile(match[1], root, file)
}

// texLogTrailer reports the summary TeX prints after the error record.
func texLogTrailer(line string) bool {
	trim := strings.TrimSpace(line)
	return strings.HasPrefix(trim, "Here is how much of TeX's memory") ||
		strings.HasPrefix(trim, "Output written on ") ||
		strings.HasPrefix(trim, "Transcript written on ")
}

// texLogNamesFile reports that a file:line path from the log is file.
// The engine's working directory is root, and the log path is relative to it.
func texLogNamesFile(logName, root, file string) bool {
	logName = strings.TrimSpace(logName)
	file = strings.TrimSpace(file)
	if logName == "" || file == "" {
		return false
	}
	file = filepath.Clean(file)
	cleaned := filepath.Clean(logName)
	if filepath.IsAbs(cleaned) {
		return strings.EqualFold(cleaned, file)
	}
	if strings.TrimSpace(root) == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(filepath.Join(root, logName)), file)
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
	return tool.LatexDocument(body)
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
		bibNote   string
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
			msg := "xelatex 无法启动"
			if ctx.Err() != nil {
				msg = "编译已取消"
			} else if errors.Is(runErr, context.DeadlineExceeded) {
				msg = "编译超时"
			}
			return PreviewResult{Log: clip(lastLog, 4000), Message: msg}, runErr
		}
		if code == 0 {
			pdf := strings.TrimSuffix(main, filepath.Ext(main)) + ".pdf"
			if !bibRan {
				bin, label := "", ""
				arg := strings.TrimSuffix(filepath.Base(main), filepath.Ext(main))
				if NeedsBiber(log) && opt.Biber != "" {
					bin, label = opt.Biber, "biber"
				} else if opt.Bibtex != "" && (NeedsBibtex(log) || bibliographyStale(root, arg)) {
					bin, label = opt.Bibtex, "bibtex"
				}
				if bin != "" {
					bibRan = true
					bibOut := runBibliography(ctx, opt, bin, label, root, arg, &installed, pkgDone)
					if line := bibOpenFailure(bibOut); line != "" {
						return PreviewResult{Log: clip(bibOut, 4000), Message: line}, errCompileFailed
					}
					if line := bibtexSyntaxFailure(bibOut); line != "" {
						return PreviewResult{Log: clip(bibOut, 4000), Message: line}, errCompileFailed
					}
					// The next engine pass replaces lastLog. Keep BibTeX's
					// own warnings, or a rebuilt .bbl still looks clean.
					if note := bibliographyNote(bibOut); note != "" {
						bibNote = note
					}
					continue
				}
			}
			if NeedsRerun(log) && reruns < 2 {
				reruns++
				continue
			}
			if st, statErr := os.Stat(pdf); statErr == nil && st.Size() > 0 && !st.IsDir() {
				return PreviewResult{PDFPath: pdf, Repaired: repaired, Log: withBibNote(clip(lastLog, 4000), bibNote)}, nil
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
		target := ErrorFile(root, log, main)
		source, readErr := readRepairSource(target)
		if readErr != nil {
			break
		}
		// A rejected reply leaves the file unchanged, so ask again without
		// another engine pass. A transport error still stops the loop.
		wrote := false
		for repairs < maxSourceRepairs && opt.Repair != nil && !wrote {
			phase(opt, "repairing", filepath.Base(target))
			replacement, ok, repairErr := opt.Repair(ctx, target, source, ErrorExcerptFor(log, root, target))
			repairs++
			if repairErr != nil {
				break
			}
			if !ok {
				continue
			}
			if err := writeRepaired(target, source, replacement); err != nil {
				return PreviewResult{Log: clip(lastLog, 4000), Message: "无法写回修复后的源文件"}, err
			}
			repaired = true
			wrote = true
		}
		if !wrote {
			break
		}
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
