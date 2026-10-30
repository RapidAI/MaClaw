package mailbox

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const outboundFileCap = 8

// OutboundFile is one file frozen into a draft. The bytes are what the
// confirmation showed; SMTP does not read the path again.
type OutboundFile struct {
	Name  string
	Bytes []byte
}

// loadOutboundFiles reads attachment paths from the task workspace. A path
// outside that directory, a missing file, or a total over AttachmentCapBytes
// is refused before the send budget is spent.
func (h *Host) loadOutboundFiles(paths []string) ([]OutboundFile, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	if len(paths) > outboundFileCap {
		return nil, classErr("invalid", "附件超过 8 个")
	}
	root := ""
	if h != nil {
		root = strings.TrimSpace(h.WorkingDir)
	}
	if root == "" {
		return nil, classErr("invalid", "附件必须位于任务工作区")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, classErr("invalid", "附件必须位于任务工作区")
	}
	rootReal, err := outboundResolve(rootAbs)
	if err != nil || rootReal == "" {
		return nil, classErr("invalid", "附件必须位于任务工作区")
	}
	var total int
	out := make([]OutboundFile, 0, len(paths))
	for _, raw := range paths {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.ContainsRune(raw, 0) {
			return nil, classErr("invalid", "附件必须位于任务工作区")
		}
		target := raw
		if !filepath.IsAbs(target) {
			target = filepath.Join(rootAbs, target)
		}
		abs, err := filepath.Abs(target)
		if err != nil {
			return nil, classErr("invalid", "附件必须位于任务工作区")
		}
		// The workspace itself may be a junction. The path the model passes
		// can be the target directory, which is outside the junction string
		// and still the same files. The resolved path is the boundary.
		resolved, err := outboundResolve(abs)
		if err != nil || resolved == "" {
			return nil, classErr("invalid", "找不到附件")
		}
		if !outboundPathInside(rootReal, resolved) {
			return nil, classErr("invalid", "附件必须位于任务工作区")
		}
		info, err := os.Stat(resolved)
		if err != nil || info == nil || !info.Mode().IsRegular() {
			return nil, classErr("invalid", "找不到附件")
		}
		size := int(info.Size())
		if size < 0 || size > AttachmentCapBytes || total+size > AttachmentCapBytes {
			return nil, classErr("too_large", "附件超过 10MB，没有保存")
		}
		body, err := os.ReadFile(resolved)
		if err != nil {
			return nil, classErr("invalid", "找不到附件")
		}
		if len(body) > AttachmentCapBytes || total+len(body) > AttachmentCapBytes {
			return nil, classErr("too_large", "附件超过 10MB，没有保存")
		}
		total += len(body)
		name := filepath.Base(resolved)
		if name == "." || name == string(filepath.Separator) || name == "" {
			return nil, classErr("invalid", "找不到附件")
		}
		out = append(out, OutboundFile{Name: name, Bytes: body})
	}
	return out, nil
}

// outboundPathInside reports whether target is a file strictly inside root.
// Comparison ignores Windows case and a \\?\ prefix. A volume root such as
// C:\ has no extra separator before the file name. C:\work does not contain
// C:\workspace.
func outboundPathInside(root, target string) bool {
	root = outboundComparePath(root)
	target = outboundComparePath(target)
	if root == "." || target == "." || root == "" || len(target) <= len(root) {
		return false
	}
	if !strings.EqualFold(target[:len(root)], root) {
		return false
	}
	last := root[len(root)-1]
	if last == '/' || last == '\\' {
		return true
	}
	sep := target[len(root)]
	return sep == '/' || sep == '\\'
}

func outboundComparePath(path string) string {
	for _, prefix := range []string{`\\?\`, `\??\`} {
		if len(path) >= len(prefix) && strings.EqualFold(path[:len(prefix)], prefix) {
			path = path[len(prefix):]
			if len(path) >= 4 && strings.EqualFold(path[:4], `UNC\`) {
				path = `\\` + path[4:]
			}
			break
		}
	}
	return filepath.Clean(path)
}

// outboundResolve returns the path after directory links. A Windows junction
// is followed because Readlink returns its target even though Lstat does not
// mark it as a symlink. The result is what Stat and ReadFile will open.
func outboundResolve(path string) (string, error) {
	path = filepath.Clean(path)
	walked := 0
	for {
		next, followed, err := outboundFollowOne(path)
		if err != nil {
			return "", err
		}
		if !followed {
			return next, nil
		}
		walked++
		if walked > 255 {
			return "", classErr("invalid", "找不到附件")
		}
		path = next
	}
}

func outboundFollowOne(path string) (string, bool, error) {
	vol := outboundVolumePrefix(path)
	dest := path[:vol]
	rest := path[vol:]
	for rest != "" {
		rest = strings.TrimLeft(rest, `/\`)
		if rest == "" {
			break
		}
		elem, next := splitPathElem(rest)
		if elem == "." {
			rest = next
			continue
		}
		if elem == ".." {
			if len(dest) > vol {
				dest = filepath.Dir(dest)
				if len(dest) < vol {
					dest = path[:vol]
				}
			}
			rest = next
			continue
		}
		parent := dest
		candidate := filepath.Join(dest, elem)
		target, err := os.Readlink(candidate)
		if err == nil && target != "" {
			base := filepath.Clean(target)
			if !filepath.IsAbs(base) {
				base = filepath.Clean(filepath.Join(parent, target))
			}
			if next != "" {
				base = filepath.Join(base, strings.TrimLeft(next, `/\`))
			}
			base = outboundComparePath(base)
			if base == outboundComparePath(candidate) && next == "" {
				return candidate, false, nil
			}
			return base, true, nil
		}
		// Readlink failed. A Windows junction is not ModeSymlink; if it is
		// also not a plain directory, Stat would still follow it and read
		// whatever it points at. Refuse that path instead of opening it.
		info, statErr := os.Lstat(candidate)
		if statErr != nil {
			if !os.IsNotExist(statErr) {
				return "", false, statErr
			}
		} else if !plainOutboundNode(info, next == "") {
			return "", false, classErr("invalid", "找不到附件")
		}
		dest = candidate
		rest = next
	}
	return filepath.Clean(dest), false, nil
}

func plainOutboundNode(info os.FileInfo, final bool) bool {
	if info == nil {
		return false
	}
	mode := info.Mode()
	if mode&os.ModeSymlink != 0 || mode&os.ModeIrregular != 0 {
		return false
	}
	if info.IsDir() {
		return true
	}
	// A file may only be the last component. A directory used as a file,
	// or a file used as a directory, is not opened.
	return final && mode.IsRegular()
}

func outboundVolumePrefix(path string) int {
	vol := filepath.VolumeName(path)
	n := len(vol)
	if n < len(path) && (path[n] == '/' || path[n] == '\\') {
		n++
	}
	return n
}

func splitPathElem(rest string) (string, string) {
	i := strings.IndexAny(rest, `/\`)
	if i < 0 {
		return rest, ""
	}
	return rest[:i], rest[i:]
}

// writeMixedMessage emits multipart/mixed. The boundary is chosen so it does
// not occur in any part, and every part, including each attachment, is
// preceded by that boundary. A body glued to the next part is not a file a
// client can save.
func writeMixedMessage(b *strings.Builder, msg OutboundMessage) {
	parts := make([]string, 1+len(msg.Files))
	var text strings.Builder
	text.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	writeTextBody(&text, msg.Text)
	parts[0] = text.String()
	for i, file := range msg.Files {
		parts[i+1] = attachmentPart(file)
	}
	boundary := mixedBoundary(strings.Join(parts, "\r\n"))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n")
	for _, part := range parts {
		b.WriteString("\r\n--" + boundary + "\r\n")
		b.WriteString(part)
		if !strings.HasSuffix(part, "\r\n") {
			b.WriteString("\r\n")
		}
	}
	b.WriteString("--" + boundary + "--\r\n")
}

func mixedBoundary(payload string) string {
	var nonce [16]byte
	boundary := ""
	for range 4 {
		_, _ = rand.Read(nonce[:])
		boundary = fmt.Sprintf("maclaw_%x", nonce)
		if !strings.Contains(payload, boundary) {
			return boundary
		}
	}
	for strings.Contains(payload, boundary) {
		boundary += "x"
	}
	return boundary
}

func attachmentPart(file OutboundFile) string {
	name := strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r < 32 {
			return -1
		}
		return r
	}, oneLine(file.Name))
	if name == "" {
		name = "attachment"
	}
	encoded := mime.BEncoding.Encode("UTF-8", name)
	escaped := url.PathEscape(name)
	var b strings.Builder
	writeMIMELine(&b, "Content-Type: "+outboundMediaType(name)+"; name=\""+encoded+"\"")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	writeContentDisposition(&b, encoded, escaped)
	b.WriteString("\r\n")
	raw := base64.StdEncoding.EncodeToString(file.Bytes)
	for len(raw) > 76 {
		b.WriteString(raw[:76])
		b.WriteString("\r\n")
		raw = raw[76:]
	}
	b.WriteString(raw)
	b.WriteString("\r\n")
	return b.String()
}

// writeMIMELine folds at spaces so an encoded filename stays under the SMTP
// 998-octet line limit. A fold is a CRLF plus a space, which header parsers
// treat as one space.
func writeMIMELine(b *strings.Builder, line string) {
	for len(line) > 900 {
		cut := 900
		if sp := strings.LastIndex(line[:cut], " "); sp > 0 {
			cut = sp
		}
		b.WriteString(line[:cut])
		b.WriteString("\r\n")
		line = " " + strings.TrimLeft(line[cut:], " ")
	}
	b.WriteString(line)
	b.WriteString("\r\n")
}

func writeContentDisposition(b *strings.Builder, encoded, escaped string) {
	head := "Content-Disposition: attachment; filename=\"" + encoded + "\""
	star := "filename*=UTF-8''" + escaped
	if len(head)+2+len(star) <= 900 {
		b.WriteString(head + "; " + star + "\r\n")
		return
	}
	writeMIMELine(b, head+";")
	writeFilenameStar(b, escaped)
}

func writeFilenameStar(b *strings.Builder, escaped string) {
	plain := " filename*=UTF-8''" + escaped
	if len(plain) <= 900 {
		b.WriteString(plain + "\r\n")
		return
	}
	firstRoom := 900 - len(" filename*0*=UTF-8''")
	first := takePercent(escaped, firstRoom)
	rest := escaped[len(first):]
	b.WriteString(" filename*0*=UTF-8''" + first + ";\r\n")
	for n := 1; len(rest) > 0; n++ {
		prefix := fmt.Sprintf(" filename*%d*=", n)
		chunk := takePercent(rest, 900-len(prefix))
		if chunk == "" {
			chunk = rest[:1]
		}
		rest = rest[len(chunk):]
		end := ""
		if len(rest) > 0 {
			end = ";"
		}
		b.WriteString(prefix + chunk + end + "\r\n")
	}
}

// takePercent returns a prefix of a percent-encoded name that is not longer
// than room and does not split a %XX sequence.
func takePercent(s string, room int) string {
	if room < 1 || s == "" {
		return ""
	}
	if room > len(s) {
		return s
	}
	n := room
	for n > 0 && !percentCut(s, n) {
		n--
	}
	if n == 0 {
		return s[:room]
	}
	return s[:n]
}

func percentCut(s string, n int) bool {
	if n >= len(s) {
		return true
	}
	if s[n-1] == '%' {
		return false
	}
	if n >= 2 && s[n-2] == '%' && isHex(s[n-1]) {
		return false
	}
	return true
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func outboundMediaType(name string) string {
	if strings.EqualFold(filepath.Ext(name), ".pdf") {
		return "application/pdf"
	}
	return "application/octet-stream"
}
