package browser

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const pageScriptSkillLimit = 40

// pageScriptSkillFileLimit is above the longest script ValidatePageScript
// accepts, plus the JSON around it. Anything larger is not a skill we wrote.
const pageScriptSkillFileLimit = 64 << 10

var (
	pageScriptSkillNamePattern   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}$`)
	pageScriptOwnerKeyPattern    = regexp.MustCompile(`^[0-9a-f]{16}$`)
	pageScriptHashedOwnerPattern = regexp.MustCompile(`^h[0-9a-f]{16}$`)
)

// pageScriptSkillMu keeps the 40-skill count and a Windows replace in one
// process from overlapping. A replace moves the live file aside before the
// new file takes its place.
var pageScriptSkillMu sync.Mutex

// PageScriptSkill is one saved browser batch. The desktop bot and the GUI
// store and rerun these through the same functions.
type PageScriptSkill struct {
	Name    string    `json:"name"`
	Host    string    `json:"host,omitempty"`
	Script  string    `json:"script"`
	Summary string    `json:"summary,omitempty"`
	SavedAt time.Time `json:"saved_at"`
}

// NormalizePageScriptSkillName accepts the kebab-case name a saved batch uses.
func NormalizePageScriptSkillName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("browser skill name is required")
	}
	if !pageScriptSkillNamePattern.MatchString(name) {
		return "", fmt.Errorf("browser skill name must be kebab-case, such as save-draft")
	}
	return name, nil
}

// PageScriptSkillHost reads the website a saved batch is allowed to run on.
func PageScriptSkillHost(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return NormalizePageScriptHost(parsed.Hostname())
}

// PageScriptSkillHostAllowed refuses a saved batch on a different website.
func PageScriptSkillHostAllowed(skillHost, pageURL string) error {
	skillHost = NormalizePageScriptHost(skillHost)
	if skillHost == "" {
		return fmt.Errorf("browser skill has no site and cannot run")
	}
	pageHost := PageScriptSkillHost(pageURL)
	if pageHost == "" {
		return fmt.Errorf("browser skill belongs to %s and this page has no address", skillHost)
	}
	if pageHost != skillHost {
		return fmt.Errorf("browser skill belongs to %s and this page is %s", skillHost, pageHost)
	}
	return nil
}

// RunPageScriptBatch runs one page script. A non-empty name stores the script
// when it finishes on an ordinary page, so a later RunSavedPageScript can
// repeat it. root is the skill directory; empty uses the shared data dir.
func RunPageScriptBatch(session *BrowserAgentSession, script, name, owner, root string) (*BrowserActionResult, error) {
	name = strings.TrimSpace(name)
	if name != "" {
		var err error
		name, err = NormalizePageScriptSkillName(name)
		if err != nil {
			return nil, err
		}
	}
	if session == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	result, err := session.RunPageScript(script)
	if err != nil || name == "" || !PageScriptCanSave(result) {
		return result, err
	}
	if pageScriptSkillLeftSite(result) {
		result.Display += "\nskill was not saved because the page left its website"
		return result, nil
	}
	saved, saveErr := SavePageScriptSkill(root, owner, name, script, pageScriptSkillPageURL(result), result.Display)
	if saveErr != nil {
		result.Display += "\nskill was not saved: " + saveErr.Error()
		return result, nil
	}
	result.Display += "\nsaved browser skill " + saved
	return result, nil
}

// RunSavedPageScript loads a batch stored for owner and runs it only on the
// website it was saved from.
func RunSavedPageScript(session *BrowserAgentSession, owner, name, root string) (*BrowserActionResult, error) {
	if session == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	skill, err := LoadPageScriptSkill(root, owner, name)
	if err != nil {
		return nil, err
	}
	if err := ValidatePageScript(skill.Script); err != nil {
		return nil, err
	}
	if _, ok := acceptableSkillHost(skill.Host); !ok {
		return nil, fmt.Errorf("browser skill has no site and cannot run")
	}
	return session.RunPageScriptForSite(skill.Script, skill.Host)
}

// SavePageScriptSkill stores one batch under owner. A 16-hex desktop user key
// is kept as the directory name so skills already on disk stay readable.
func SavePageScriptSkill(root, owner, name, script, pageURL, summary string) (string, error) {
	name, err := NormalizePageScriptSkillName(name)
	if err != nil {
		return "", err
	}
	if err := ValidatePageScript(script); err != nil {
		return "", err
	}
	host, ok := acceptableSkillHost(PageScriptSkillHost(pageURL))
	if !ok {
		return "", fmt.Errorf("browser skill was not saved because the page has no website address")
	}
	dir, err := pageScriptSkillDir(root, owner)
	if err != nil {
		return "", err
	}
	pageScriptSkillMu.Lock()
	defer pageScriptSkillMu.Unlock()
	if err := preparePageScriptSkillDir(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name+".json")
	info, statErr := os.Lstat(path)
	if statErr != nil && !os.IsNotExist(statErr) {
		return "", statErr
	}
	if statErr == nil && !info.Mode().IsRegular() {
		return "", fmt.Errorf("browser skill %s is damaged", name)
	}
	if os.IsNotExist(statErr) {
		entries, readErr := os.ReadDir(dir)
		if readErr != nil {
			return "", readErr
		}
		stems := pageScriptSkillStems(entries)
		if _, updating := stems[name]; !updating && len(stems) >= pageScriptSkillLimit {
			return "", fmt.Errorf("browser skill list is full")
		}
	}
	record := PageScriptSkill{
		Name:    name,
		Host:    host,
		Script:  script,
		Summary: pageScriptSkillSummary(summary),
		SavedAt: time.Now().UTC(),
	}
	body, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", err
	}
	temp := path + ".tmp"
	if err := writeSkillTemp(temp, append(body, '\n')); err != nil {
		return "", err
	}
	if err := replacePageScriptSkillFile(temp, path); err != nil {
		_ = os.Remove(temp)
		return "", err
	}
	_ = os.Remove(path + ".bak")
	return name, nil
}

// LoadPageScriptSkill reads one saved batch.
func LoadPageScriptSkill(root, owner, name string) (PageScriptSkill, error) {
	name, err := NormalizePageScriptSkillName(name)
	if err != nil {
		return PageScriptSkill{}, err
	}
	dir, err := pageScriptSkillDir(root, owner)
	if err != nil {
		return PageScriptSkill{}, err
	}
	pageScriptSkillMu.Lock()
	defer pageScriptSkillMu.Unlock()
	if err := rejectLinkedSkillDir(dir); err != nil {
		if os.IsNotExist(err) {
			return PageScriptSkill{}, fmt.Errorf("browser skill %s is not saved", name)
		}
		return PageScriptSkill{}, err
	}
	return readPageScriptSkill(dir, name)
}

// ListPageScriptSkills renders the batches stored for owner.
func ListPageScriptSkills(root, owner string) (string, error) {
	dir, err := pageScriptSkillDir(root, owner)
	if err != nil {
		return "", err
	}
	pageScriptSkillMu.Lock()
	defer pageScriptSkillMu.Unlock()
	if err := rejectLinkedSkillDir(dir); err != nil {
		if os.IsNotExist(err) {
			return "browser skills (0):\nnone yet. A successful action=script with name stores one.", nil
		}
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "browser skills (0):\nnone yet. A successful action=script with name stores one.", nil
		}
		return "", err
	}
	lines := make([]string, 0, len(entries))
	seen := map[string]struct{}{}
	for _, entry := range entries {
		name, ok := pageScriptSkillListName(dir, entry)
		if !ok {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		skill, loadErr := readPageScriptSkill(dir, name)
		if loadErr != nil {
			continue
		}
		line := "- " + skill.Name
		if skill.Host != "" {
			line += " host=" + skill.Host
		}
		if skill.Summary != "" {
			line += " " + skill.Summary
		}
		lines = append(lines, line)
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return "browser skills (0):\nnone yet. A successful action=script with name stores one.", nil
	}
	return fmt.Sprintf("browser skills (%d):\n%s", len(lines), strings.Join(lines, "\n")), nil
}

func pageScriptSkillDir(root, owner string) (string, error) {
	owner, err := canonicalPageScriptOwner(owner)
	if err != nil {
		return "", err
	}
	root = strings.TrimSpace(root)
	if root == "" {
		root = filepath.Join(corelib.MaclawDataDir(), "desktop-browser-skills")
	}
	root = filepath.Clean(root)
	dir := filepath.Clean(filepath.Join(root, owner))
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("browser skill requires an owner")
	}
	return dir, nil
}

func pageScriptSkillStems(entries []os.DirEntry) map[string]struct{} {
	stems := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		name, ok := pageScriptSkillStem(entry)
		if !ok {
			continue
		}
		stems[name] = struct{}{}
	}
	return stems
}

func pageScriptSkillListName(dir string, entry os.DirEntry) (string, bool) {
	name, backup, ok := pageScriptSkillEntry(entry)
	if !ok {
		return "", false
	}
	if backup {
		if _, err := os.Lstat(filepath.Join(dir, name+".json")); err == nil {
			return "", false
		}
	}
	return name, true
}

func pageScriptSkillStem(entry os.DirEntry) (string, bool) {
	name, _, ok := pageScriptSkillEntry(entry)
	return name, ok
}

func pageScriptSkillEntry(entry os.DirEntry) (string, bool, bool) {
	if entry == nil || entry.IsDir() {
		return "", false, false
	}
	fileName := entry.Name()
	backup := false
	switch {
	case strings.HasSuffix(fileName, ".json.bak"):
		fileName = strings.TrimSuffix(fileName, ".json.bak")
		backup = true
	case strings.HasSuffix(fileName, ".json"):
		fileName = strings.TrimSuffix(fileName, ".json")
	default:
		return "", false, false
	}
	name, err := NormalizePageScriptSkillName(fileName)
	if err != nil {
		return "", false, false
	}
	return name, backup, true
}

func pageScriptSkillLeftSite(result *BrowserActionResult) bool {
	if result == nil || result.Data == nil {
		return false
	}
	startHost := PageScriptSkillHost(pageScriptSkillDataString(result.Data, "start_url"))
	endHost := PageScriptSkillHost(pageScriptSkillDataString(result.Data, "url"))
	return startHost != "" && endHost != "" && startHost != endHost
}

func pageScriptSkillDataString(data map[string]interface{}, key string) string {
	value, _ := data[key].(string)
	return value
}

func pageScriptSkillPageURL(result *BrowserActionResult) string {
	if result == nil || result.Data == nil {
		return ""
	}
	start, _ := result.Data["start_url"].(string)
	return strings.TrimSpace(start)
}

func readPageScriptSkill(dir, name string) (PageScriptSkill, error) {
	path := filepath.Join(dir, name+".json")
	skill, err := readSkillRecord(path, name)
	if err == nil || !os.IsNotExist(err) {
		return skill, err
	}
	backup := path + ".bak"
	skill, err = readSkillRecord(backup, name)
	if err != nil {
		if os.IsNotExist(err) {
			return PageScriptSkill{}, fmt.Errorf("browser skill %s is not saved", name)
		}
		return PageScriptSkill{}, err
	}
	if renErr := os.Rename(backup, path); renErr != nil {
		return skill, nil
	}
	return skill, nil
}

func readSkillRecord(path, name string) (PageScriptSkill, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return PageScriptSkill{}, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > pageScriptSkillFileLimit {
		return PageScriptSkill{}, fmt.Errorf("browser skill %s is damaged", name)
	}
	body, err := readSkillFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return PageScriptSkill{}, err
		}
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			return PageScriptSkill{}, err
		}
		return PageScriptSkill{}, fmt.Errorf("browser skill %s is damaged", name)
	}
	skill, err := parsePageScriptSkill(body, name)
	if err != nil {
		return PageScriptSkill{}, err
	}
	return skill, nil
}

func parsePageScriptSkill(body []byte, name string) (PageScriptSkill, error) {
	var skill PageScriptSkill
	if err := json.Unmarshal(body, &skill); err != nil || skill.Name != name || ValidatePageScript(skill.Script) != nil {
		return PageScriptSkill{}, fmt.Errorf("browser skill %s is damaged", name)
	}
	host, ok := acceptableSkillHost(skill.Host)
	if !ok {
		return PageScriptSkill{}, fmt.Errorf("browser skill %s is damaged", name)
	}
	skill.Host = host
	skill.Summary = pageScriptSkillSummary(skill.Summary)
	return skill, nil
}

// acceptableSkillHost is the hostname written into a skill file. Save and
// load share it, so a record we store can be read back.
func acceptableSkillHost(host string) (string, bool) {
	host = NormalizePageScriptHost(host)
	if host == "" || len(host) > 253 || strings.Contains(host, "..") || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || strings.HasPrefix(host, "-") || strings.HasSuffix(host, "-") {
		return "", false
	}
	alnum := false
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			alnum = true
		case r == '.', r == '-', r == ':':
		default:
			return "", false
		}
	}
	if !alnum {
		return "", false
	}
	return host, true
}

func preparePageScriptSkillDir(dir string) error {
	if err := rejectLinkedSkillDir(dir); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return rejectLinkedSkillDir(dir)
}

func rejectLinkedSkillDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("browser skill requires an owner")
	}
	return nil
}

func writeSkillTemp(temp string, body []byte) error {
	if info, err := os.Lstat(temp); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("browser skill file is damaged")
		}
		if err := os.Remove(temp); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	file, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(body)
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(temp)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(temp)
		return closeErr
	}
	return nil
}

func readSkillFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, pageScriptSkillFileLimit+1))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 || len(body) > pageScriptSkillFileLimit {
		return nil, fmt.Errorf("browser skill file is damaged")
	}
	return body, nil
}

// canonicalPageScriptOwner maps an owner onto one directory name.
// A desktop user key stays 16 hex so skills already on disk keep their path.
// Any other owner is stored as h plus 16 hex, which cannot collide with that
// key. Passing the result through again returns it unchanged.
func canonicalPageScriptOwner(owner string) (string, error) {
	owner = strings.TrimSpace(owner)
	if pageScriptOwnerKeyPattern.MatchString(owner) || pageScriptHashedOwnerPattern.MatchString(owner) {
		return owner, nil
	}
	if owner == "" || strings.Contains(owner, "..") || strings.ContainsAny(owner, `/\`) || strings.ContainsRune(owner, 0) {
		return "", fmt.Errorf("browser skill requires an owner")
	}
	sum := sha256.Sum256([]byte("page-script-owner\x00" + owner))
	return "h" + hex.EncodeToString(sum[:8]), nil
}

func pageScriptSkillSummary(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > 160 {
		text = string(runes[:160]) + "..."
	}
	lower := strings.ToLower(text)
	// Button labels such as "Forgot password" stay. Assignments are the shape
	// a secret takes when it leaks into the result text.
	if strings.Contains(lower, "cookie=") || strings.Contains(lower, "password=") || strings.Contains(lower, "token=") {
		return ""
	}
	return text
}

// replacePageScriptSkillFile moves temp onto path. Windows refuses to rename
// over a file that is already there, so a saved skill can be updated.
func replacePageScriptSkillFile(temp, path string) error {
	if err := os.Rename(temp, path); err == nil {
		return nil
	}
	backup := path + ".bak"
	_ = os.Remove(backup)
	if _, err := os.Lstat(path); err == nil {
		if err := os.Rename(path, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Rename(backup, path)
		return err
	}
	_ = os.Remove(backup)
	return nil
}

func pageScriptToolOwner(session *BrowserAgentSession, args map[string]interface{}) (string, error) {
	owner := ""
	if session != nil {
		owner = strings.TrimSpace(session.OwnerID)
	}
	if owner == "" {
		owner = strings.TrimSpace(strArg(args, browserRuntimePolicyOwnerIDArg, ""))
	}
	if owner == "" {
		return "", fmt.Errorf("browser skill requires an owner")
	}
	return owner, nil
}

func browserScriptTool(args map[string]interface{}) string {
	agentSession, err := requireAgentSessionFromArgs(args)
	if err != nil {
		return marshalBrowserResult(false, err.Error(), nil)
	}
	name := strArg(args, "name", "")
	owner := ""
	if strings.TrimSpace(name) != "" {
		// A missing owner is a save failure. The script still runs, and the
		// batch records why the name was not stored.
		owner, _ = pageScriptToolOwner(agentSession, args)
	}
	result, err := RunPageScriptBatch(agentSession, strArg(args, "script", ""), name, owner, "")
	return marshalActionResult(agentSession, result, err, ExpectSpec{})
}

func browserSkillListTool(args map[string]interface{}) string {
	agentSession, err := requireAgentSessionFromArgs(args)
	if err != nil {
		return marshalBrowserResult(false, err.Error(), nil)
	}
	owner, err := pageScriptToolOwner(agentSession, args)
	if err != nil {
		return marshalBrowserResult(false, err.Error(), nil)
	}
	listed, err := ListPageScriptSkills("", owner)
	if err != nil {
		return marshalBrowserResult(false, err.Error(), nil)
	}
	return marshalBrowserResult(true, listed, nil)
}

func browserSkillRunTool(args map[string]interface{}) string {
	agentSession, err := requireAgentSessionFromArgs(args)
	if err != nil {
		return marshalBrowserResult(false, err.Error(), nil)
	}
	owner, err := pageScriptToolOwner(agentSession, args)
	if err != nil {
		return marshalBrowserResult(false, err.Error(), nil)
	}
	result, err := RunSavedPageScript(agentSession, owner, strArg(args, "name", ""), "")
	return marshalActionResult(agentSession, result, err, ExpectSpec{})
}
