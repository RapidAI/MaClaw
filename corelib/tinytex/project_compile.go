package tinytex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// graphicsRef matches the inclusion commands that make one document depend on
// another file in the same directory. The argument is a file the host found,
// never a string the model typed into the verification call.
var graphicsRef = regexp.MustCompile(`\\(?:includegraphics\*?|input|include|includestandalone)\s*(?:\[[^\]]*\])?\s*\{([^{}]+)\}`)

// primitiveInput matches TeX's brace-less \input, which LaTeX still runs.
// The braced form stays in graphicsRef. A quoted name is how the engine
// accepts a space in that form.
var primitiveInput = regexp.MustCompile(`\\input(?:\s+"([^"]+)"|\s+([^\s\\{}]+)|"([^"]+)")`)

// bibRef matches bibliography inputs. They are not compiled as documents,
// but a newer .bib must keep the article from being treated as finished.
var bibRef = regexp.MustCompile(`\\(?:bibliography|addbibresource)\s*(?:\[[^\]]*\])?\s*\{([^{}]+)\}`)

// bibStyleRef matches a bibliography style. A local .bst next to the paper,
// such as the elsarticle styles, changes the .bbl the same way a .bib does.
var bibStyleRef = regexp.MustCompile(`\\bibliographystyle\s*(?:\[[^\]]*\])?\s*\{([^{}]+)\}`)

// packageRef matches class and package names loaded from this directory.
// A missing name is a system file and does not force a rebuild.
var packageRef = regexp.MustCompile(`\\(?:documentclass|documentstyle|usepackage|RequirePackage)\s*(?:\[[^\]]*\])?\s*\{([^{}]+)\}`)

const (
	// latexInputWalkDepth is how many nested TeX sources are opened to see
	// whether a grandchild changed. The main file's directory is the lookup
	// root, because TeX resolves \input from the job's working directory.
	latexInputWalkDepth = 8
	// latexInputReadLimit matches the detection read cap. A larger included
	// source cannot be checked, so the document is compiled again.
	latexInputReadLimit = 1 << 20
)

// verifyCacheRoot is the directory that holds success stamps. Tests set it.
// Empty uses the user cache, then the temp directory.
var verifyCacheRoot string

// ReviewedCompileOptions selects the engine the product already uses for
// 编译预览 when that TinyTeX tree is verified, and otherwise the reviewed
// program names on PATH. It never accepts an engine path from the model.
func ReviewedCompileOptions(dataDir string) PreviewOptions {
	if dist, err := FindDistRoot(DistDir(dataDir)); err == nil && Verified(dist) {
		if engine, engErr := FindEngine(dist); engErr == nil {
			tlmgr, _ := FindTlmgr(dist)
			bibtex, _ := FindBin(dist, "bibtex.exe", "bibtex")
			biber, _ := FindBin(dist, "biber.exe", "biber")
			return compileOptions(engine, tlmgr, bibtex, biber)
		}
	}
	engine, err := exec.LookPath("pdflatex")
	if err != nil {
		engine, err = exec.LookPath("xelatex")
	}
	if err != nil {
		return PreviewOptions{}
	}
	bibtex, _ := exec.LookPath("bibtex")
	biber, _ := exec.LookPath("biber")
	return compileOptions(engine, "", bibtex, biber)
}

func compileOptions(engine, tlmgr, bibtex, biber string) PreviewOptions {
	opt := PreviewOptions{
		Engine: engine,
		Tlmgr:  tlmgr,
		Bibtex: bibtex,
		Biber:  biber,
		Run:    runReviewed,
	}
	if tlmgr == "" {
		return opt
	}
	opt.Install = func(ctx context.Context, pkg string) error {
		if PackageNameFromFile(pkg+".sty") == "" && PackageNameFromFile(pkg) == "" {
			return fmt.Errorf("package name rejected")
		}
		_, code, err := runReviewed(ctx, tlmgr, filepath.Dir(tlmgr), "install", pkg)
		if err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("tlmgr install %s exited %d", pkg, code)
		}
		return nil
	}
	return opt
}

type latexDoc struct {
	path string
	stem string
	body string
	refs []string
}

// CompileDirectory builds every top-level LaTeX document in dir. Figures and
// inputs are compiled before the document that includes them. A failure is
// reported in the result text and does not hide the other documents. Source
// repair is intentionally absent: verification must not rewrite the file the
// agent just edited.
func CompileDirectory(ctx context.Context, dir string, opt PreviewOptions) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	docs, err := latexDocuments(dir)
	if err != nil {
		return "", err
	}
	if len(docs) == 0 {
		return "该目录没有可编译的 LaTeX 主文件", nil
	}
	if opt.Run == nil || strings.TrimSpace(opt.Engine) == "" {
		return "未找到已审核的 LaTeX 引擎", nil
	}
	// Verification reports the compiler's answer. It must not rewrite the
	// source the agent just edited, even if a caller attached a repair hook.
	opt.Repair = nil
	order := orderLatexDocuments(docs)
	names := make(map[string]string, len(order))
	for _, doc := range order {
		names[strings.ToLower(doc.stem)] = filepath.Base(doc.path)
	}
	// Successes are reported first and failures last. The host keeps the tail
	// of a long log, so a figure that failed must not be pushed out by a later
	// document that still found the previous PDF.
	failed := map[string]struct{}{}
	var failedNames []string
	var warnings []string
	var okBuf, badBuf strings.Builder
	for _, doc := range order {
		if err := ctx.Err(); err != nil {
			return compileReport(&okBuf, &badBuf, failedNames, warnings), err
		}
		name := filepath.Base(doc.path)
		pdf := strings.TrimSuffix(doc.path, filepath.Ext(doc.path)) + ".pdf"
		removeLegacyStamp(pdf)
		warn := dependencyWarning(doc, failed, names)
		if warn != "" {
			warnings = append(warnings, name+" "+warn)
		} else if latexUpToDate(doc) {
			// A failed dependency still compiles the file that includes it.
			// An untouched document with a newer PDF does not: the elsarticle
			// bundle otherwise rebuilds every sample template on each edit.
			fmt.Fprintf(&okBuf, "%s 已是最新\n", name)
			// The stamp is a success bit plus any citation or BibTeX warning
			// that was still true. Repeating it is the only way the next
			// report shows the problem: this branch does not run TeX again.
			if note := verifiedNote(pdf); note != "" {
				okBuf.WriteString(note)
				okBuf.WriteByte('\n')
			}
			okBuf.WriteByte('\n')
			continue
		}
		result, runErr := Preview(ctx, doc.path, opt)
		if result.PDFPath != "" && runErr == nil {
			// Stamp the path the next run will stat. A preview that wrote a
			// different PDF is not a verified build of this document.
			if samePDF(pdf, result.PDFPath) {
				markVerified(pdf, bibliographyNote(result.Log))
			} else {
				clearVerified(pdf)
			}
			fmt.Fprintf(&okBuf, "%s -> %s\n", name, result.PDFPath)
			if log := diagnosticLog(result.Log); log != "" {
				okBuf.WriteString(log)
				okBuf.WriteByte('\n')
			}
			okBuf.WriteByte('\n')
			continue
		}
		clearVerified(pdf)
		failed[strings.ToLower(doc.stem)] = struct{}{}
		failedNames = append(failedNames, name)
		fmt.Fprintf(&badBuf, "%s\n", name)
		msg := strings.TrimSpace(result.Message)
		if msg == "" && runErr != nil {
			msg = runErr.Error()
		}
		if msg == "" {
			msg = "编译失败"
		}
		badBuf.WriteString(msg)
		badBuf.WriteByte('\n')
		if warn != "" {
			badBuf.WriteString(warn)
		}
		if log := diagnosticLog(result.Log); log != "" {
			badBuf.WriteString(log)
			badBuf.WriteByte('\n')
		}
		badBuf.WriteByte('\n')
	}
	return compileReport(&okBuf, &badBuf, failedNames, warnings), nil
}

func compileReport(okBuf, badBuf *strings.Builder, failedNames, warnings []string) string {
	var b strings.Builder
	b.WriteString(okBuf.String())
	b.WriteString(badBuf.String())
	if len(failedNames) > 0 {
		fmt.Fprintf(&b, "未通过: %s\n", strings.Join(failedNames, ", "))
	}
	for _, warning := range warnings {
		b.WriteString(warning)
		if !strings.HasSuffix(warning, "\n") {
			b.WriteByte('\n')
		}
	}
	return strings.TrimSpace(b.String())
}

func verifyCacheDir() string {
	if root := strings.TrimSpace(verifyCacheRoot); root != "" {
		return root
	}
	if dir, err := os.UserCacheDir(); err == nil && strings.TrimSpace(dir) != "" {
		return filepath.Join(dir, "maclaw", "latex-verify")
	}
	return filepath.Join(os.TempDir(), "maclaw-latex-verify")
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if runtime.GOOS == "windows" {
		abs = strings.ToLower(abs)
	}
	return abs, nil
}

func verifyStamp(pdf string) (string, error) {
	key, err := canonicalPath(pdf)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(verifyCacheDir(), hex.EncodeToString(sum[:])), nil
}

func samePDF(a, b string) bool {
	left, errA := canonicalPath(a)
	right, errB := canonicalPath(b)
	if errA != nil || errB != nil {
		return false
	}
	return left == right
}

// markVerified records a successful build outside the paper directory. The
// cache entry is keyed by the PDF path. note is empty when the bibliography
// was clean; otherwise it is the short warning repeated on a later skip.
// If the cache directory cannot be created, nothing is written and the next
// run compiles again.
func markVerified(pdf, note string) {
	if strings.TrimSpace(pdf) == "" {
		return
	}
	removeLegacyStamp(pdf)
	stamp, err := verifyStamp(pdf)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(stamp), 0o755); err != nil {
		return
	}
	note = strings.TrimSpace(note)
	if len(note) > 600 {
		note = note[:600]
	}
	_ = os.WriteFile(stamp, []byte(note), 0o644)
}

// verifiedNote is the warning stored with the success stamp. A missing or
// empty stamp means the previous build had nothing to repeat.
func verifiedNote(pdf string) string {
	stamp, err := verifyStamp(pdf)
	if err != nil {
		return ""
	}
	f, err := os.Open(stamp)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 601)
	n, _ := f.Read(buf)
	if n > 600 {
		n = 600
	}
	return strings.TrimSpace(string(buf[:n]))
}

func clearVerified(pdf string) {
	if strings.TrimSpace(pdf) == "" {
		return
	}
	removeLegacyStamp(pdf)
	stamp, err := verifyStamp(pdf)
	if err != nil {
		return
	}
	_ = os.Remove(stamp)
}

// removeLegacyStamp deletes the success file older builds wrote beside the
// PDF. The paper directory is not a cache.
func removeLegacyStamp(pdf string) {
	if strings.TrimSpace(pdf) == "" {
		return
	}
	_ = os.Remove(pdf + ".maclaw-ok")
}

// latexUpToDate reports that this process already compiled doc successfully
// and neither the source nor a file it includes has changed since. The stamp
// is required because a failed TeX run can still leave a newer PDF behind.
func latexUpToDate(doc latexDoc) bool {
	pdf := strings.TrimSuffix(doc.path, filepath.Ext(doc.path)) + ".pdf"
	pdfInfo, err := os.Stat(pdf)
	if err != nil || pdfInfo.IsDir() || pdfInfo.Size() == 0 {
		return false
	}
	stamp, stampErr := verifyStamp(pdf)
	if stampErr != nil {
		return false
	}
	stampInfo, err := os.Stat(stamp)
	if err != nil || stampInfo.IsDir() {
		return false
	}
	limit := stampInfo.ModTime()
	if pdfInfo.ModTime().Before(limit) {
		limit = pdfInfo.ModTime()
	}
	src, err := os.Stat(doc.path)
	if err != nil || src.ModTime().After(limit) {
		return false
	}
	return !inputsNewer(filepath.Dir(doc.path), doc.body, limit)
}

func inputsNewer(dir, body string, built time.Time) bool {
	return inputsNewerAt(dir, body, built, 0, map[string]struct{}{})
}

// inputsNewerAt walks included TeX sources from dir. depth counts sources
// already opened. Past the cap, a further TeX include cannot be checked and
// forces a build. A cycle does not.
func inputsNewerAt(dir, body string, built time.Time, depth int, seen map[string]struct{}) bool {
	code := tool.LatexCode(body)
	if packagesNewer(dir, code, built) || bibliographyNewer(dir, code, built) || stylesNewer(dir, code, built) {
		return true
	}
	for _, raw := range inputArgs(code) {
		force, texPath := resolveInput(dir, raw, built)
		if force {
			return true
		}
		if texPath == "" {
			continue
		}
		if depth >= latexInputWalkDepth {
			return true
		}
		if walkIncludedTex(dir, texPath, built, depth+1, seen) {
			return true
		}
	}
	return false
}

func walkIncludedTex(dir, path string, built time.Time, depth int, seen map[string]struct{}) bool {
	body, status := readIncluded(path, seen)
	switch status {
	case includeCycle, includeMissing:
		return false
	case includeUnread:
		return true
	default:
		return inputsNewerAt(dir, body, built, depth, seen)
	}
}

func bibliographyNewer(dir, code string, built time.Time) bool {
	for _, match := range bibRef.FindAllStringSubmatch(code, -1) {
		for _, name := range strings.Split(match[1], ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if !strings.EqualFold(filepath.Ext(name), ".bib") {
				name += ".bib"
			}
			force, _ := resolveInput(dir, name, built)
			if force {
				return true
			}
		}
	}
	return false
}

func packagesNewer(dir, code string, built time.Time) bool {
	for _, match := range packageRef.FindAllStringSubmatch(code, -1) {
		for _, name := range strings.Split(match[1], ",") {
			if localPackageNewer(dir, name, built) {
				return true
			}
		}
	}
	return false
}

func stylesNewer(dir, code string, built time.Time) bool {
	for _, match := range bibStyleRef.FindAllStringSubmatch(code, -1) {
		if localFilesNewer(dir, match[1], built, ".bst") {
			return true
		}
	}
	return false
}

// localPackageNewer stats name.cls and name.sty in dir when they exist.
// It does not search the TeX tree. A missing package stays a system file.
func localPackageNewer(dir, name string, built time.Time) bool {
	return localFilesNewer(dir, name, built, ".cls", ".sty")
}

func localFilesNewer(dir, name string, built time.Time, exts ...string) bool {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, `/\`) {
		return false
	}
	ext := strings.ToLower(filepath.Ext(name))
	var files []string
	if ext != "" {
		if !extListed(ext, exts) {
			return false
		}
		base := strings.TrimSuffix(name, filepath.Ext(name))
		if !pkgNameRe.MatchString(base) {
			return false
		}
		files = []string{name}
	} else {
		if !pkgNameRe.MatchString(name) {
			return false
		}
		for _, want := range exts {
			files = append(files, name+want)
		}
	}
	for _, file := range files {
		info, err := os.Stat(filepath.Join(dir, file))
		if err != nil || info.IsDir() {
			continue
		}
		if info.ModTime().After(built) {
			return true
		}
	}
	return false
}

func extListed(ext string, exts []string) bool {
	for _, want := range exts {
		if ext == want {
			return true
		}
	}
	return false
}

// resolveInput reports whether an included path must be rebuilt, and the TeX
// source behind it when one exists inside dir. A TeX file outside dir is not
// read. A missing include does not force a rebuild. An image or .bib outside
// dir is checked by mtime only.
func resolveInput(dir, raw string, built time.Time) (bool, string) {
	outside, files, texPath := locateInput(dir, raw)
	if outside && (texPath != "" || !dataFilesOnly(files)) {
		return true, ""
	}
	for _, file := range files {
		if file.info.ModTime().After(built) {
			return true, ""
		}
	}
	if outside {
		return false, ""
	}
	return false, texPath
}

type locatedFile struct {
	path string
	info os.FileInfo
}

func locateInput(dir, raw string) (bool, []locatedFile, string) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, `\`, "/"))
	if raw == "" {
		return false, nil, ""
	}
	rel := filepath.FromSlash(raw)
	names := []string{rel}
	if filepath.Ext(rel) == "" {
		for _, ext := range []string{".tex", ".ltx", ".latex", ".pdf", ".png", ".jpg", ".jpeg", ".eps", ".bib"} {
			names = append(names, rel+ext)
		}
	}
	var files []locatedFile
	texPath := ""
	outside := false
	for _, name := range names {
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, name)
		}
		if !withinDir(dir, path) {
			outside = true
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		files = append(files, locatedFile{path: path, info: info})
		if texPath == "" && isTexSource(path) {
			texPath = path
		}
	}
	return outside, files, texPath
}

func dataFilesOnly(files []locatedFile) bool {
	if len(files) == 0 {
		return true
	}
	for _, file := range files {
		switch strings.ToLower(filepath.Ext(file.path)) {
		case ".pdf", ".png", ".jpg", ".jpeg", ".eps", ".bib":
		default:
			return false
		}
	}
	return true
}

func inputArgs(code string) []string {
	var args []string
	for _, match := range graphicsRef.FindAllStringSubmatch(code, -1) {
		args = append(args, match[1])
	}
	for _, match := range primitiveInput.FindAllStringSubmatch(code, -1) {
		raw := ""
		for _, group := range match[1:] {
			if group != "" {
				raw = group
				break
			}
		}
		if raw != "" {
			args = append(args, raw)
		}
	}
	return args
}

type includeRead int

const (
	includeOK includeRead = iota
	includeCycle
	includeMissing
	includeUnread
)

func readIncluded(path string, seen map[string]struct{}) (string, includeRead) {
	key, err := canonicalPath(path)
	if err != nil {
		return "", includeUnread
	}
	if _, ok := seen[key]; ok {
		return "", includeCycle
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", includeMissing
	}
	if info.Size() > latexInputReadLimit {
		return "", includeUnread
	}
	body, err := os.ReadFile(path)
	if err != nil || int64(len(body)) > latexInputReadLimit {
		return "", includeUnread
	}
	seen[key] = struct{}{}
	return string(body), includeOK
}

// referencedDocuments lists top-level documents this source compiles against,
// including figures pulled in through a chapter. An include that cannot be
// opened counts as depending on every other document, so a figure still
// builds before the article that might use its PDF.
func referencedDocuments(dir, body string, index map[string]int, self, count int) []string {
	seenDoc := map[int]struct{}{}
	var refs []string
	add := func(stem string) {
		j, ok := index[stem]
		if !ok || j == self {
			return
		}
		if _, dup := seenDoc[j]; dup {
			return
		}
		seenDoc[j] = struct{}{}
		refs = append(refs, stem)
	}
	if walkDocRefs(dir, body, 0, map[string]struct{}{}, add) {
		stems := make([]string, count)
		for stem, j := range index {
			if j >= 0 && j < len(stems) {
				stems[j] = stem
			}
		}
		for j, stem := range stems {
			if stem != "" && j != self {
				add(stem)
			}
		}
	}
	return refs
}

func walkDocRefs(dir, body string, depth int, seen map[string]struct{}, add func(string)) bool {
	incomplete := false
	for _, raw := range inputArgs(tool.LatexCode(body)) {
		add(referenceStem(raw))
		outside, _, texPath := locateInput(dir, raw)
		if texPath == "" {
			continue
		}
		if outside || depth >= latexInputWalkDepth {
			incomplete = true
			continue
		}
		nested, status := readIncluded(texPath, seen)
		switch status {
		case includeCycle, includeMissing:
			continue
		case includeUnread:
			incomplete = true
			continue
		}
		if walkDocRefs(dir, nested, depth+1, seen, add) {
			incomplete = true
		}
	}
	return incomplete
}

func isTexSource(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".tex", ".ltx", ".latex":
		return true
	default:
		return false
	}
}

func diagnosticLog(log string) string {
	var kept []string
	for _, line := range strings.Split(log, "\n") {
		trim := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if trim == "" || !diagnosticLine(trim) {
			continue
		}
		kept = append(kept, trim)
	}
	if len(kept) > 40 {
		kept = kept[len(kept)-40:]
	}
	return strings.Join(kept, "\n")
}

func diagnosticLine(line string) bool {
	if texErrorRe.MatchString(line) {
		return true
	}
	lower := strings.ToLower(line)
	switch {
	case strings.HasPrefix(line, "!"):
		return true
	case strings.Contains(lower, "warning"), strings.Contains(lower, "error"):
		return true
	case strings.Contains(lower, "overfull"), strings.Contains(lower, "underfull"):
		return true
	case strings.Contains(lower, "output written"):
		return true
	case strings.Contains(lower, "emergency stop"), strings.Contains(lower, "fatal"):
		return true
	default:
		return false
	}
}

func dependencyWarning(doc latexDoc, failed map[string]struct{}, names map[string]string) string {
	var refs []string
	for _, ref := range doc.refs {
		if _, bad := failed[ref]; !bad {
			continue
		}
		name := names[ref]
		if name == "" {
			name = ref
		}
		refs = append(refs, name)
	}
	if len(refs) == 0 {
		return ""
	}
	return "引用了未编译成功的 " + strings.Join(refs, "、") + "，PDF 可能仍是上一份产物\n"
}

func latexDocuments(dir string) ([]latexDoc, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var docs []latexDoc
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		switch strings.ToLower(filepath.Ext(name)) {
		case ".tex", ".ltx", ".latex":
		default:
			continue
		}
		path := filepath.Join(dir, name)
		info, statErr := os.Lstat(path)
		if statErr != nil || info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil || !declaresDocument(string(body)) {
			continue
		}
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		docs = append(docs, latexDoc{path: path, stem: stem, body: string(body)})
	}
	sort.SliceStable(docs, func(i, j int) bool { return docs[i].stem < docs[j].stem })
	return docs, nil
}

func orderLatexDocuments(docs []latexDoc) []latexDoc {
	index := make(map[string]int, len(docs))
	for i, doc := range docs {
		stem := strings.ToLower(doc.stem)
		if _, exists := index[stem]; exists {
			continue
		}
		index[stem] = i
	}
	deps := make([][]int, len(docs))
	indegree := make([]int, len(docs))
	for i, doc := range docs {
		// A figure included only from a chapter is still a dependency. The
		// article must not be stamped current while that figure's PDF is old,
		// and a failed figure must stay visible in the tail warning.
		refs := referencedDocuments(filepath.Dir(doc.path), doc.body, index, i, len(docs))
		for _, ref := range refs {
			j := index[ref]
			// j is needed by i, so j comes first.
			deps[j] = append(deps[j], i)
			indegree[i]++
		}
		docs[i].refs = refs
	}
	ready := make([]int, 0, len(docs))
	for i := range docs {
		if indegree[i] == 0 {
			ready = append(ready, i)
		}
	}
	sort.Ints(ready)
	ordered := make([]latexDoc, 0, len(docs))
	for len(ready) > 0 {
		next := ready[0]
		ready = ready[1:]
		ordered = append(ordered, docs[next])
		var more []int
		for _, dep := range deps[next] {
			indegree[dep]--
			if indegree[dep] == 0 {
				more = append(more, dep)
			}
		}
		ready = append(ready, more...)
		sort.Ints(ready)
	}
	if len(ordered) < len(docs) {
		seen := map[string]struct{}{}
		for _, doc := range ordered {
			seen[doc.path] = struct{}{}
		}
		for _, doc := range docs {
			if _, ok := seen[doc.path]; !ok {
				ordered = append(ordered, doc)
			}
		}
	}
	return ordered
}

func referenceStem(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.ReplaceAll(raw, "\\", "/")
	base := filepath.Base(raw)
	switch strings.ToLower(filepath.Ext(base)) {
	case ".tex", ".ltx", ".latex", ".pdf", ".png", ".jpg", ".jpeg", ".eps":
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}
	return strings.ToLower(base)
}
