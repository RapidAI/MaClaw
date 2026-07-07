package skill

import (
	"regexp"
	"sort"
	"strings"
)

var (
	pythonImportLineRe  = regexp.MustCompile(`(?m)^\s*import\s+(.+)$`)
	pythonFromImportRe  = regexp.MustCompile(`(?m)^\s*from\s+([A-Za-z_][A-Za-z0-9_\.]*)\s+import\b`)
	nodeRequireRe       = regexp.MustCompile(`\brequire\(\s*["']([^"']+)["']\s*\)`)
	nodeImportRe        = regexp.MustCompile(`(?m)^\s*import(?:\s+[^"';]+?\s+from)?\s*["']([^"']+)["']`)
	nodeDynamicImportRe = regexp.MustCompile(`\bimport\(\s*["']([^"']+)["']\s*\)`)
)

// ExtractScriptRequires infers portable dependency declarations from crafted
// script source. It is intentionally conservative: Python imports are recorded
// only for known third-party modules to avoid treating local helper files as
// pip packages; Node bare package imports are safe enough to record directly.
func ExtractScriptRequires(script, language string) *SkillYAMLRequires {
	language = strings.ToLower(strings.TrimSpace(language))
	var req SkillYAMLRequires
	switch language {
	case "python", "python3", "py":
		req.Python = extractPythonRequires(script)
	case "node", "nodejs", "javascript", "js":
		req.Node = extractNodeRequires(script)
	default:
		if python := extractPythonRequires(script); len(python) > 0 {
			req.Python = python
		}
		if node := extractNodeRequires(script); len(node) > 0 {
			req.Node = node
		}
	}
	if len(req.Python) == 0 && len(req.Node) == 0 {
		return nil
	}
	return &req
}

func extractPythonRequires(script string) []string {
	seen := map[string]bool{}
	var result []string
	add := func(raw string) {
		top := pythonTopLevelImportName(raw)
		pkg := pythonImportPackageName(top)
		if pkg != "" && !seen[pkg] {
			seen[pkg] = true
			result = append(result, pkg)
		}
	}
	for _, line := range strings.Split(script, "\n") {
		if m := pythonImportLineRe.FindStringSubmatch(line); len(m) > 1 {
			for _, raw := range splitCSV(stripPythonLineComment(m[1])) {
				add(raw)
			}
			continue
		}
		if m := pythonFromImportRe.FindStringSubmatch(line); len(m) > 1 {
			add(m[1])
		}
	}
	return result
}

func pythonTopLevelImportName(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, ".") {
		return ""
	}
	if fields := strings.Fields(raw); len(fields) > 0 {
		raw = fields[0]
	}
	raw = strings.Trim(raw, "(),")
	return strings.Split(raw, ".")[0]
}

func stripPythonLineComment(line string) string {
	var quote rune
	runes := []rune(line)
	for i, r := range runes {
		if quote != 0 {
			if r == '\\' && i+1 < len(runes) {
				continue
			}
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == '#' {
			return string(runes[:i])
		}
	}
	return line
}

func pythonImportPackageName(module string) string {
	module = strings.TrimSpace(module)
	if module == "" || pythonStdlibModules[module] {
		return ""
	}
	if pkg, ok := pythonImportToPackage[module]; ok {
		return pkg
	}
	if pythonCommonThirdParty[module] {
		return module
	}
	return ""
}

var pythonImportToPackage = map[string]string{
	"bs4":        "beautifulsoup4",
	"cv2":        "opencv-python",
	"fitz":       "PyMuPDF",
	"PIL":        "Pillow",
	"docx":       "python-docx",
	"pptx":       "python-pptx",
	"rapidocr":   "rapidocr-onnxruntime",
	"sklearn":    "scikit-learn",
	"yaml":       "PyYAML",
	"weasyprint": "weasyprint",
}

var pythonCommonThirdParty = map[string]bool{
	"anthropic": true, "aiohttp": true, "click": true, "fastapi": true,
	"flask": true, "httpx": true, "jinja2": true, "lxml": true,
	"markdown": true, "markdown2": true, "matplotlib": true, "numpy": true,
	"openai": true, "pandas": true, "pdfplumber": true, "playwright": true,
	"pydantic": true, "pypdf": true, "PyPDF2": true, "requests": true,
	"reportlab": true, "selenium": true, "typer": true, "uvicorn": true,
}

var pythonStdlibModules = map[string]bool{
	"argparse": true, "asyncio": true, "base64": true, "collections": true,
	"contextlib": true, "csv": true, "dataclasses": true, "datetime": true,
	"decimal": true, "email": true, "enum": true, "functools": true,
	"glob": true, "gzip": true, "hashlib": true, "html": true, "http": true,
	"importlib": true, "inspect": true, "io": true, "itertools": true,
	"json": true, "logging": true, "math": true, "mimetypes": true, "os": true,
	"pathlib": true, "platform": true, "queue": true, "random": true, "re": true,
	"shlex": true, "shutil": true, "signal": true, "socket": true, "sqlite3": true,
	"statistics": true, "string": true, "subprocess": true, "sys": true,
	"tempfile": true, "textwrap": true, "threading": true, "time": true,
	"traceback": true, "typing": true, "unicodedata": true, "urllib": true,
	"uuid": true, "xml": true, "zipfile": true,
}

func extractNodeRequires(script string) []string {
	type nodeImportMatch struct {
		pos int
		raw string
	}
	var matches []nodeImportMatch
	collect := func(re *regexp.Regexp) {
		for _, m := range re.FindAllStringSubmatchIndex(script, -1) {
			if len(m) < 4 || m[2] < 0 || m[3] < 0 {
				continue
			}
			matches = append(matches, nodeImportMatch{
				pos: m[0],
				raw: script[m[2]:m[3]],
			})
		}
	}
	collect(nodeRequireRe)
	collect(nodeImportRe)
	collect(nodeDynamicImportRe)
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].pos < matches[j].pos
	})

	seen := map[string]bool{}
	var result []string
	add := func(raw string) {
		pkg := nodePackageName(raw)
		if pkg == "" || seen[pkg] {
			return
		}
		seen[pkg] = true
		result = append(result, pkg)
	}
	for _, match := range matches {
		add(match.raw)
	}
	return result
}

func nodePackageName(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "node:")
	if raw == "" || strings.HasPrefix(raw, ".") || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, `\`) {
		return ""
	}
	parts := strings.Split(raw, "/")
	if len(parts) == 0 {
		return ""
	}
	name := parts[0]
	if strings.HasPrefix(name, "@") && len(parts) > 1 {
		name = name + "/" + parts[1]
	}
	if nodeBuiltinModules[name] {
		return ""
	}
	return name
}

var nodeBuiltinModules = map[string]bool{
	"assert": true, "async_hooks": true, "buffer": true, "child_process": true,
	"cluster": true, "console": true, "constants": true, "crypto": true,
	"dgram": true, "diagnostics_channel": true, "dns": true, "domain": true,
	"events": true, "fs": true, "http": true, "http2": true, "https": true,
	"inspector": true, "module": true, "net": true, "os": true, "path": true,
	"perf_hooks": true, "process": true, "punycode": true, "querystring": true,
	"readline": true, "repl": true, "stream": true, "string_decoder": true,
	"timers": true, "tls": true, "tty": true, "url": true, "util": true,
	"v8": true, "vm": true, "worker_threads": true, "zlib": true,
}
