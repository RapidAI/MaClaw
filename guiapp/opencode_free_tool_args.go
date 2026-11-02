package guiapp

import (
	"encoding/json"
	"path"
	"path/filepath"
	"strings"
)

// openCodePromptWorkspace is the directory baked into the captured OpenCode
// system prompt. The free model treats it as the cwd. Task files belong in
// the Maclaw workspace instead.
const openCodePromptWorkspace = `D:\oc-empty`

func openCodeFreeWorkspace(h *IMMessageHandler, userID string) string {
	if h == nil {
		return ""
	}
	if dir := strings.TrimSpace(h.projectTabWorkDirForOwner(userID)); dir != "" {
		return dir
	}
	return strings.TrimSpace(trustedPrincipalBoundWorkspace(h, userID))
}

// confineOpenCodeFreeToolArgs moves file and shell paths out of the captured
// prompt directory and into the task workspace. Relative paths stay relative
// so the existing workspace resolver still applies.
func confineOpenCodeFreeToolArgs(name, argsJSON, workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" || strings.TrimSpace(argsJSON) == "" {
		return argsJSON
	}
	var args map[string]interface{}
	if json.Unmarshal([]byte(argsJSON), &args) != nil || args == nil {
		return argsJSON
	}
	changed := false
	switch strings.TrimSpace(name) {
	case "read_file", "write_file":
		path, _ := args["path"].(string)
		if next, ok := relocateOpenCodePath(path, workspace); ok {
			args["path"] = next
			changed = true
		}
		if strings.TrimSpace(name) == "write_file" {
			if content, ok := args["content"].(string); ok {
				next := rewriteOpenCodePromptPaths(content, workspace)
				if next != content {
					args["content"] = next
					changed = true
				}
			}
		}
	case "bash":
		dir, hasDir := args["working_dir"].(string)
		fromWorkdir := false
		if !hasDir {
			if alt, ok := args["workdir"].(string); ok {
				dir, hasDir, fromWorkdir = alt, true, true
			}
		}
		if hasDir {
			if next, moved := relocateOpenCodePath(dir, workspace); moved {
				if openCodePathInside(next, workspace) && !strings.EqualFold(filepath.Clean(next), filepath.Clean(workspace)) {
					args["working_dir"] = next
				} else {
					delete(args, "working_dir")
				}
				delete(args, "workdir")
				changed = true
			} else if fromWorkdir {
				args["working_dir"] = dir
				delete(args, "workdir")
				changed = true
			}
		}
		if command, ok := args["command"].(string); ok {
			next := rewriteOpenCodePromptPaths(command, workspace)
			if next != command {
				args["command"] = next
				changed = true
			}
		}
	}
	if !changed {
		return argsJSON
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return argsJSON
	}
	return string(encoded)
}

func relocateOpenCodePath(path, workspace string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) {
		return "", false
	}
	cleaned := filepath.Clean(path)
	if openCodePathInside(cleaned, workspace) {
		return "", false
	}
	for _, prompt := range openCodePromptDirForms() {
		prompt = filepath.Clean(prompt)
		if strings.EqualFold(cleaned, prompt) {
			return workspace, true
		}
		rel, err := filepath.Rel(prompt, cleaned)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		return filepath.Join(workspace, rel), true
	}
	// Leave other absolute paths where they are. A Python install path such
	// as C:\Users\...\python.exe must still run; only the prompt directory
	// is a stand-in for this task.
	return "", false
}

func rewriteOpenCodePromptPaths(text, workspace string) string {
	// Forward slashes. A Windows replacement such as \tasks or \beijing is a
	// tab or a backspace inside the Python string the model just wrote.
	// A normal Python string writes the directory as D:\\oc-empty, which is
	// two backslash characters in the file. That form has to move too.
	slash := filepath.ToSlash(filepath.Clean(workspace))
	out := text
	for _, prompt := range openCodePromptDirForms() {
		out = replacePromptSourcePath(out, prompt, slash)
		if strings.Contains(prompt, `\`) {
			out = replacePromptSourcePath(out, strings.ReplaceAll(prompt, `\`, `\\`), slash)
		}
	}
	return out
}

func openCodePromptDirForms() []string {
	return []string{openCodePromptWorkspace, strings.ReplaceAll(openCodePromptWorkspace, `\`, `/`)}
}

func openCodePathInside(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if strings.EqualFold(path, root) {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func replacePromptSourcePath(s, old, replacement string) string {
	if old == "" || s == "" {
		return s
	}
	var b strings.Builder
	rest := s
	folded := strings.ToLower(rest)
	needle := strings.ToLower(old)
	for {
		i := strings.Index(folded, needle)
		if i < 0 {
			b.WriteString(rest)
			return b.String()
		}
		end := i + len(old)
		if end < len(rest) && !openCodePromptBoundary(rest[end]) {
			b.WriteString(rest[:end])
			rest = rest[end:]
			folded = folded[end:]
			continue
		}
		rel, n := consumeSourceRelative(rest[end:])
		cleaned, ok := cleanSourceRelative(rel)
		if !ok {
			b.WriteString(rest[:end])
			rest = rest[end:]
			folded = folded[end:]
			continue
		}
		b.WriteString(rest[:i])
		b.WriteString(replacement)
		b.WriteString(cleaned)
		rest = rest[end+n:]
		folded = folded[end+n:]
	}
}

// consumeSourceRelative takes the path tail after the prompt directory.
// A Python string writes one separator as two backslash characters; a raw
// string or a shell command writes one. Both are accepted so a mixed tail
// is not left glued to the replacement. It stops at a quote, space, or
// shell metacharacter so the rest of the script stays put.
func consumeSourceRelative(rest string) (string, int) {
	var b strings.Builder
	i := 0
	for i < len(rest) {
		if i+1 < len(rest) && rest[i] == '\\' && rest[i+1] == '\\' {
			if i+2 >= len(rest) || !sourcePathAtom(rest[i+2]) {
				break
			}
			b.WriteByte('/')
			i += 2
			continue
		}
		c := rest[i]
		if c == '\\' || c == '/' {
			if i+1 >= len(rest) || !sourcePathAtom(rest[i+1]) {
				break
			}
			b.WriteByte('/')
			i++
			continue
		}
		if !sourcePathAtom(c) {
			break
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), i
}

// cleanSourceRelative collapses sub/../keep.pdf to keep.pdf. A tail that
// still leaves the prompt directory is refused, so the workspace prefix is
// not joined onto ../secret.
func cleanSourceRelative(rel string) (string, bool) {
	trimmed := strings.TrimPrefix(rel, "/")
	if trimmed == "" {
		return "", true
	}
	cleaned := path.Clean(trimmed)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || path.IsAbs(cleaned) {
		return "", false
	}
	if cleaned == "." {
		return "", true
	}
	return "/" + cleaned, true
}

func sourcePathAtom(c byte) bool {
	switch c {
	case '\\', '/', '"', '\'', ' ', '\t', '\r', '\n', '<', '>', '|', '?', '*', ';', '&', '`', ',':
		return false
	default:
		return c > 32
	}
}

func openCodePromptBoundary(c byte) bool {
	switch c {
	case '\\', '/', '"', '\'', ' ', '\t', '\r', '\n':
		return true
	default:
		return false
	}
}
