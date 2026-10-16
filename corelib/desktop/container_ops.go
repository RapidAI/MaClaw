package desktop

import (
	"encoding/base64"
	"fmt"
	"html"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// ContainerHome is the home directory the person sees on this desktop.
	// The container process is root. docker sets HOME here, and the
	// persistent volume is mounted here. ~/Desktop on the screen is
	// ContainerHome/Desktop. /root is the writable layer and is not that folder.
	ContainerHome = DesktopHome
	// FileContentMax is the largest file a bot may write or edit in one call.
	FileContentMax = 200 << 10
	// FileReadMax is the most text one read returns. A longer file is cut
	// from the head and marked truncated. An edit refuses a longer file
	// instead of writing the cut text back.
	FileReadMax = 200 << 10
	// BashCommandMax bounds one command string. The command runs as a single
	// argv inside the container.
	BashCommandMax = 16 << 10
	// BashOutputMax keeps the end of a command's output. Compile errors sit there.
	BashOutputMax = 32 << 10
	// BashBudget is how long one container command may run. It finishes inside
	// the two-minute hub client, and the output so far is still returned.
	BashBudget = 90 * time.Second
	// HTTPBodyMax bounds one web page or search page read inside the container.
	HTTPBodyMax = 200 << 10
	// ToolResultMax is how much of one desktop tool result the model sees.
	// File reads and command transcripts are already bounded before they
	// leave the container. A smaller outer preview cut the bytes a later
	// edit has to match, and the compiler lines at the end of a command.
	ToolResultMax = FileReadMax + 64
	// FileDeliverBase64Max is the longest standard base64 string one chat
	// attachment may carry. Hub reads a message up to 1 MiB plus one screenshot.
	FileDeliverBase64Max = 200_000
	// FileBytesMax is the most raw bytes that still encode inside that string.
	// A longer file is refused whole. deliver does not attach a prefix.
	FileBytesMax = FileDeliverBase64Max / 4 * 3
)

// ContainerPath resolves a bot path inside the desktop container. A relative
// path and a leading ~/ start at ContainerHome. The result is absolute and
// uses slashes. It is not a host path.
func ContainerPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n\x00\\") || strings.Contains(raw, ":") {
		return "", fmt.Errorf("%w: path is invalid", ErrInvalid)
	}
	switch {
	case raw == "~":
		raw = ContainerHome
	case strings.HasPrefix(raw, "~/"):
		raw = ContainerHome + "/" + strings.TrimPrefix(raw, "~/")
	case !strings.HasPrefix(raw, "/"):
		raw = ContainerHome + "/" + raw
	}
	cleaned := path.Clean(raw)
	if !strings.HasPrefix(cleaned, "/") || strings.Contains(cleaned, ":") {
		return "", fmt.Errorf("%w: path is invalid", ErrInvalid)
	}
	if len(cleaned) > 4096 {
		return "", fmt.Errorf("%w: path is too long", ErrInvalid)
	}
	return cleaned, nil
}

// ApplyEdit replaces one exact passage. An empty old string, a missing
// passage, or more than one match is refused so the file is not rewritten
// at the wrong place.
func ApplyEdit(content, old, new string) (string, error) {
	if old == "" {
		return "", fmt.Errorf("%w: old_string is required", ErrInvalid)
	}
	count := strings.Count(content, old)
	switch count {
	case 0:
		return "", fmt.Errorf("%w: old_string was not found", ErrInvalid)
	case 1:
		return strings.Replace(content, old, new, 1), nil
	default:
		return "", fmt.Errorf("%w: old_string matched %d times", ErrInvalid, count)
	}
}

// FileReadArgs reads at most max+1 bytes so the caller can tell a cut file
// from a file that ended on the limit.
func FileReadArgs(container, filePath string, max int) []string {
	if max < 1 {
		max = FileReadMax
	}
	return []string{"exec", container, "sh", "-c", `head -c "$2" -- "$1"`, "sh", filePath, strconv.Itoa(max + 1)}
}

// FileWriteArgs writes stdin to the path, creating parent directories.
func FileWriteArgs(container, filePath string) []string {
	return []string{"exec", "-i", container, "sh", "-c", `mkdir -p -- "$(dirname "$1")" && cat > "$1"`, "sh", filePath}
}

// FileListArgs lists one container path.
func FileListArgs(container, filePath string) []string {
	return []string{"exec", container, "ls", "-la", "--", filePath}
}

// FileBytesArgs prints one file as standard base64 with no line breaks.
// The JSON field is a Go string, and a PDF's bytes are not valid UTF-8, so
// the container encodes them before they cross that boundary. head reads one
// byte past max so a longer file is visible and is not returned as a cut file.
// base64 -w 0 is GNU coreutils, which this desktop image includes.
func FileBytesArgs(container, filePath string, max int) []string {
	if max < 1 {
		max = FileBytesMax
	}
	return []string{"exec", container, "sh", "-c", `head -c "$2" -- "$1" | base64 -w 0`, "sh", filePath, strconv.Itoa(max + 1)}
}

// DecodeFileBytes accepts the standard base64 of one file. Whitespace is
// ignored. An empty file, a payload that is not base64, or a file past
// FileBytesMax is refused. The caller does not attach a prefix of a longer file.
func DecodeFileBytes(encoded string) ([]byte, error) {
	encoded = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\r', '\n':
			return -1
		default:
			return r
		}
	}, encoded)
	if encoded == "" {
		return nil, fmt.Errorf("%w: file is empty", ErrInvalid)
	}
	if len(encoded) > FileDeliverBase64Max {
		return nil, fmt.Errorf("%w: file is too large", ErrInvalid)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: file could not be read", ErrInvalid)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: file is empty", ErrInvalid)
	}
	if len(raw) > FileBytesMax {
		return nil, fmt.Errorf("%w: file is too large", ErrInvalid)
	}
	return raw, nil
}

// BashArgs runs one command in the container as root, with ~ pointing at
// ContainerHome. The command is one argv. The host shell does not parse it.
func BashArgs(container, command string) ([]string, error) {
	command = strings.TrimSpace(command)
	if command == "" || strings.Contains(command, "\x00") {
		return nil, fmt.Errorf("%w: command is empty", ErrInvalid)
	}
	if len(command) > BashCommandMax {
		return nil, fmt.Errorf("%w: command is too long", ErrInvalid)
	}
	return []string{"exec", "-w", ContainerHome, "-e", "HOME=" + ContainerHome, container, "bash", "-c", command}, nil
}

// ProgramOutput is the text a container command returns to the model. The
// command's own exit is part of that text. A transport failure is a separate
// error, because folding the transcript into an HTTP error truncates it.
func ProgramOutput(output string, exitCode int, timedOut bool) string {
	text := CapTail(strings.TrimSpace(output), BashOutputMax)
	if text == "" {
		text = "(no output)"
	}
	if timedOut {
		return text + "\n(command timed out)"
	}
	if exitCode != 0 {
		return text + "\n(exit " + strconv.Itoa(exitCode) + ")"
	}
	return text
}

// LooksLikeHTML reports whether a fetched body should be read as a page.
// A bare "<p" is not enough: source code uses that character sequence.
func LooksLikeHTML(kind, body string) bool {
	if strings.Contains(strings.ToLower(kind), "html") {
		return true
	}
	snippet := strings.ToLower(body)
	if len(snippet) > 512 {
		snippet = snippet[:512]
	}
	return strings.Contains(snippet, "<html") || strings.Contains(snippet, "<!doctype") || strings.Contains(snippet, "<body")
}

// PublicURL accepts an http or https URL. The request is later made from
// inside the desktop container, so the container's own route and proxy apply.
func PublicURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n\x00") || len(raw) > 4096 {
		return "", fmt.Errorf("%w: url is invalid", ErrInvalid)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("%w: url is invalid", ErrInvalid)
	}
	return raw, nil
}

// DuckDuckGoURL is the HTML search page fetched from inside the container.
func DuckDuckGoURL(query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" || strings.ContainsAny(query, "\r\n\x00") || len(query) > 500 {
		return "", fmt.Errorf("%w: query is empty", ErrInvalid)
	}
	return "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query), nil
}

const curlMarker = "\n__MACLAW_HTTP__"

// CurlGetArgs fetches one URL with the container's curl. The container
// environment, including its HTTP proxy, is the process environment.
func CurlGetArgs(container, rawURL string, maxBytes int) ([]string, error) {
	rawURL, err := PublicURL(rawURL)
	if err != nil {
		return nil, err
	}
	if maxBytes < 1 {
		maxBytes = HTTPBodyMax
	}
	return []string{
		"exec", container, "curl", "-sS", "-L",
		"--max-time", "25",
		"--max-filesize", strconv.Itoa(maxBytes),
		"-A", "Mozilla/5.0",
		"-w", curlMarker + "%{http_code} %{content_type}",
		"--", rawURL,
	}, nil
}

// SplitCurl separates a curl body from the status marker written by CurlGetArgs.
func SplitCurl(output string) (body string, status int, contentType string) {
	index := strings.LastIndex(output, curlMarker)
	if index < 0 {
		return output, 0, ""
	}
	body = output[:index]
	rest := strings.TrimSpace(output[index+len(curlMarker):])
	fields := strings.SplitN(rest, " ", 2)
	status, _ = strconv.Atoi(fields[0])
	if len(fields) == 2 {
		contentType = strings.TrimSpace(fields[1])
	}
	return body, status, contentType
}

// CapHead keeps the start of a file read.
func CapHead(text string, max int) (string, bool) {
	if max < 1 || len(text) <= max {
		return text, false
	}
	return text[:max] + "\n[truncated]", true
}

// CapTail keeps the end of a command's output.
func CapTail(text string, max int) string {
	if max < 1 || len(text) <= max {
		return text
	}
	return "[truncated]\n" + text[len(text)-max:]
}

var (
	ddgResult    = regexp.MustCompile(`(?s)<a[^>]*class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	tagPattern   = regexp.MustCompile(`(?s)<script[\s\S]*?</script>|<style[\s\S]*?</style>|<[^>]+>`)
	spacePattern = regexp.MustCompile(`[ \t]+`)
	blankPattern = regexp.MustCompile(`\n{3,}`)
)

// SearchHit is one result read from a search page fetched in the container.
type SearchHit struct {
	Title string
	URL   string
}

// ParseDuckDuckGo reads result links from the DuckDuckGo HTML page.
func ParseDuckDuckGo(page string) []SearchHit {
	matches := ddgResult.FindAllStringSubmatch(page, 20)
	hits := make([]SearchHit, 0, len(matches))
	seen := map[string]bool{}
	for _, match := range matches {
		link := cleanSearchURL(html.UnescapeString(match[1]))
		title := strings.TrimSpace(spacePattern.ReplaceAllString(stripTags(html.UnescapeString(match[2])), " "))
		if link == "" || title == "" || seen[link] {
			continue
		}
		if parsed, err := url.Parse(link); err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			continue
		}
		seen[link] = true
		hits = append(hits, SearchHit{Title: title, URL: link})
	}
	return hits
}

func cleanSearchURL(raw string) string {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if target := parsed.Query().Get("uddg"); target != "" {
		return target
	}
	return raw
}

// HTMLText drops scripts, styles, and tags. It is the page text a fetch returns.
func HTMLText(page string) string {
	text := tagPattern.ReplaceAllString(page, "\n")
	text = html.UnescapeString(text)
	text = spacePattern.ReplaceAllString(text, " ")
	text = blankPattern.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

func stripTags(raw string) string {
	return tagPattern.ReplaceAllString(raw, "")
}
