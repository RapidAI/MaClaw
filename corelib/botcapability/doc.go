// Package botcapability is the MCP servers and skills shared by every desktop
// bot of one account. The document lives on Hub and is copied into that
// owner's MaClawSrv config. HTTP MCP is called from the cloud. Stdio MCP runs
// in the account's cloud desktop, so the person's own computer can be off.
// The command, args, and env are stored here. Views redact secret values.
package botcapability

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/mcp"
)

const (
	maxServers    = 32
	maxSkills     = 64
	maxSkillRunes = 24000
	maxNameRunes  = 80
	maxArgs       = 40
	maxArgRunes   = 1000
	maxEnv        = 40
	maxEnvRunes   = 4000
	maxBotIDs     = 32
	maxReadTools  = 40
)

// Server is one MCP server the bots may call.
// Transport "http" is called from the cloud and may carry a secret.
// Transport "stdio" is a command run in the account's cloud desktop. Env is
// stored with the command so the bot can start it while the person's
// computer is off. Views do not return env values.
type Server struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Transport  string            `json:"transport"`
	URL        string            `json:"url,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	AuthSecret string            `json:"auth_secret,omitempty"`
	Command    string            `json:"command,omitempty"`
	Args       []string          `json:"args,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Disabled   bool              `json:"disabled,omitempty"`
	// Bots limits the server to these hub bot ids or runtime instance ids.
	// Empty means every bot of this owner. A non-empty list is the only
	// authorization; a discovered tool cannot add itself.
	Bots []string `json:"bots,omitempty"`
	// ReadTools are tool names the owner marked safe to call without a
	// fresh confirmation. Names that send, publish, pay, or delete still
	// wait. MCP readOnlyHint is not read and is not an entry in this list.
	ReadTools []string `json:"read_tools,omitempty"`
}

// Skill is one instruction the bots can read with action=cap_skill.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Body        string `json:"body"`
}

// Document is the account-wide set.
type Document struct {
	MCP    []Server `json:"mcp,omitempty"`
	Skills []Skill  `json:"skills,omitempty"`
}

// Update is one settings change. A blank auth secret or header value on an
// upsert keeps the stored secret. EnableSkills is filled in by the GUI before
// the document is saved; this package only sees UpsertSkills.
type Update struct {
	UpsertMCP    []Server        `json:"upsert_mcp,omitempty"`
	RemoveMCP    []string        `json:"remove_mcp,omitempty"`
	SetDisabled  map[string]bool `json:"set_disabled,omitempty"`
	UpsertSkills []Skill         `json:"upsert_skills,omitempty"`
	RemoveSkills []string        `json:"remove_skills,omitempty"`
}

// ServerView is one server with the secret removed.
type ServerView struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Transport string   `json:"transport"`
	URL       string   `json:"url,omitempty"`
	Command   string   `json:"command,omitempty"`
	Args      []string `json:"args,omitempty"`
	Disabled  bool     `json:"disabled,omitempty"`
	SecretSet bool     `json:"secret_set,omitempty"`
	Bots      []string `json:"bots,omitempty"`
	ReadTools []string `json:"read_tools,omitempty"`
}

// SkillView is one skill without its body.
type SkillView struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// View is what the settings screen and a GET return.
type View struct {
	MCP     []ServerView `json:"mcp"`
	Skills  []SkillView  `json:"skills"`
	Warning string       `json:"warning,omitempty"`
}

// Empty reports a document with nothing enabled or stored.
func (d Document) Empty() bool {
	return len(d.MCP) == 0 && len(d.Skills) == 0
}

// Parse reads a stored document. Invalid JSON is an error. Nil is empty.
func Parse(raw json.RawMessage) (Document, error) {
	if len(strings.TrimSpace(string(raw))) == 0 || string(raw) == "null" {
		return Document{}, nil
	}
	var doc Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Document{}, fmt.Errorf("bot capabilities: %w", err)
	}
	return Normalize(doc)
}

// Stamp is a hash of the stored document, secrets included, so a secret
// change is pushed again.
func Stamp(doc Document) string {
	raw, err := json.Marshal(doc)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ViewOf is the redacted form.
func ViewOf(doc Document) View {
	out := View{MCP: []ServerView{}, Skills: []SkillView{}}
	for _, s := range doc.MCP {
		out.MCP = append(out.MCP, ServerView{
			ID: s.ID, Name: s.Name, Transport: s.Transport, URL: s.URL,
			Command: s.Command, Args: append([]string(nil), s.Args...),
			Disabled: s.Disabled, SecretSet: secretSet(s),
			Bots: append([]string(nil), s.Bots...), ReadTools: append([]string(nil), s.ReadTools...),
		})
	}
	for _, s := range doc.Skills {
		out.Skills = append(out.Skills, SkillView{Name: s.Name, Description: s.Description})
	}
	return out
}

func secretSet(s Server) bool {
	if strings.TrimSpace(s.AuthSecret) != "" {
		return true
	}
	for _, v := range s.Headers {
		if strings.TrimSpace(v) != "" {
			return true
		}
	}
	for _, v := range s.Env {
		if strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

// Normalize checks one document and returns a copy safe to store.
func Normalize(doc Document) (Document, error) {
	if len(doc.MCP) > maxServers {
		return Document{}, fmt.Errorf("at most %d MCP servers", maxServers)
	}
	if len(doc.Skills) > maxSkills {
		return Document{}, fmt.Errorf("at most %d skills", maxSkills)
	}
	out := Document{}
	seenMCP := map[string]bool{}
	for _, s := range doc.MCP {
		next, err := cleanServer(s)
		if err != nil {
			return Document{}, err
		}
		if seenMCP[next.ID] {
			return Document{}, fmt.Errorf("duplicate MCP id %s", next.ID)
		}
		seenMCP[next.ID] = true
		out.MCP = append(out.MCP, next)
	}
	seenSkill := map[string]bool{}
	for _, s := range doc.Skills {
		next, err := cleanSkill(s)
		if err != nil {
			return Document{}, err
		}
		key := strings.ToLower(next.Name)
		if seenSkill[key] {
			return Document{}, fmt.Errorf("duplicate skill %s", next.Name)
		}
		seenSkill[key] = true
		out.Skills = append(out.Skills, next)
	}
	return out, nil
}

// Apply merges one settings change into doc. Blank secrets keep the previous
// value for the same id.
func Apply(doc Document, up Update) (Document, error) {
	next := doc
	next.MCP = append([]Server(nil), doc.MCP...)
	next.Skills = append([]Skill(nil), doc.Skills...)
	byID := map[string]int{}
	for i, s := range next.MCP {
		byID[s.ID] = i
	}
	for _, s := range up.UpsertMCP {
		cleaned, err := cleanServer(s)
		if err != nil {
			return Document{}, err
		}
		if i, ok := byID[cleaned.ID]; ok {
			cleaned = mergeSecret(next.MCP[i], cleaned)
			next.MCP[i] = cleaned
			continue
		}
		if len(next.MCP) >= maxServers {
			return Document{}, fmt.Errorf("at most %d MCP servers", maxServers)
		}
		byID[cleaned.ID] = len(next.MCP)
		next.MCP = append(next.MCP, cleaned)
	}
	if len(up.RemoveMCP) > 0 {
		drop := map[string]bool{}
		for _, id := range up.RemoveMCP {
			drop[strings.TrimSpace(id)] = true
		}
		kept := next.MCP[:0]
		for _, s := range next.MCP {
			if drop[s.ID] {
				continue
			}
			kept = append(kept, s)
		}
		next.MCP = kept
	}
	for id, disabled := range up.SetDisabled {
		id = strings.TrimSpace(id)
		for i := range next.MCP {
			if next.MCP[i].ID == id {
				next.MCP[i].Disabled = disabled
			}
		}
	}
	bySkill := map[string]int{}
	for i, s := range next.Skills {
		bySkill[strings.ToLower(s.Name)] = i
	}
	for _, s := range up.UpsertSkills {
		cleaned, err := cleanSkill(s)
		if err != nil {
			return Document{}, err
		}
		key := strings.ToLower(cleaned.Name)
		if i, ok := bySkill[key]; ok {
			next.Skills[i] = cleaned
			continue
		}
		if len(next.Skills) >= maxSkills {
			return Document{}, fmt.Errorf("at most %d skills", maxSkills)
		}
		bySkill[key] = len(next.Skills)
		next.Skills = append(next.Skills, cleaned)
	}
	if len(up.RemoveSkills) > 0 {
		drop := map[string]bool{}
		for _, name := range up.RemoveSkills {
			if n, err := CleanSkillName(name); err == nil {
				drop[strings.ToLower(n)] = true
			}
		}
		kept := next.Skills[:0]
		for _, s := range next.Skills {
			if drop[strings.ToLower(s.Name)] {
				continue
			}
			kept = append(kept, s)
		}
		next.Skills = kept
	}
	return Normalize(next)
}

func mergeSecret(prev, next Server) Server {
	if strings.TrimSpace(next.AuthSecret) == "" {
		next.AuthSecret = prev.AuthSecret
	}
	next.Headers = mergeStringMap(prev.Headers, next.Headers)
	next.Env = mergeStringMap(prev.Env, next.Env)
	return next
}

// mergeStringMap keeps a stored value when the update omits the map or sends
// a blank value for that key.
func mergeStringMap(prev, next map[string]string) map[string]string {
	if next == nil {
		if len(prev) == 0 {
			return nil
		}
		return prev
	}
	merged := map[string]string{}
	for k, v := range next {
		if strings.TrimSpace(v) == "" {
			if old, ok := prev[k]; ok {
				v = old
			}
		}
		if strings.TrimSpace(v) != "" {
			merged[k] = v
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func cleanServer(s Server) (Server, error) {
	s.Name = strings.TrimSpace(s.Name)
	s.Transport = strings.ToLower(strings.TrimSpace(s.Transport))
	if s.Name == "" || utf8.RuneCountInString(s.Name) > maxNameRunes || strings.ContainsAny(s.Name, "\r\n") {
		return Server{}, fmt.Errorf("MCP name is required")
	}
	s.ID = idFor(s.ID, s.Name)
	switch s.Transport {
	case "http":
		u, err := url.Parse(strings.TrimSpace(s.URL))
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return Server{}, fmt.Errorf("MCP %s needs an http(s) url", s.Name)
		}
		if mcp.IsAlwaysBlockedMCPHost(u.Hostname()) {
			return Server{}, fmt.Errorf("MCP %s url is not allowed", s.Name)
		}
		s.URL = u.String()
		s.Command, s.Args, s.Env = "", nil, nil
		s.Headers = cleanMap(s.Headers)
		s.AuthSecret = strings.TrimSpace(s.AuthSecret)
		if strings.ContainsAny(s.AuthSecret, "\r\n") || len(s.AuthSecret) > 4096 {
			return Server{}, fmt.Errorf("MCP %s secret is invalid", s.Name)
		}
	case "stdio":
		s.Command = strings.TrimSpace(s.Command)
		if s.Command == "" || strings.ContainsAny(s.Command, "\r\n") || utf8.RuneCountInString(s.Command) > 300 {
			return Server{}, fmt.Errorf("MCP %s needs a command", s.Name)
		}
		if len(s.Args) > maxArgs {
			return Server{}, fmt.Errorf("MCP %s has too many arguments", s.Name)
		}
		args := make([]string, 0, len(s.Args))
		for _, a := range s.Args {
			a = strings.TrimSpace(a)
			if a == "" || strings.ContainsAny(a, "\r\n") || utf8.RuneCountInString(a) > maxArgRunes {
				return Server{}, fmt.Errorf("MCP %s has an invalid argument", s.Name)
			}
			args = append(args, a)
		}
		s.Args = args
		s.URL, s.Headers, s.AuthSecret = "", nil, ""
		env, err := cleanEnv(s.Env)
		if err != nil {
			return Server{}, fmt.Errorf("MCP %s: %w", s.Name, err)
		}
		s.Env = env
	default:
		return Server{}, fmt.Errorf("MCP %s transport must be http or stdio", s.Name)
	}
	bots, err := cleanIDList(s.Bots)
	if err != nil {
		return Server{}, fmt.Errorf("MCP %s: %w", s.Name, err)
	}
	reads, err := cleanToolNames(s.ReadTools)
	if err != nil {
		return Server{}, fmt.Errorf("MCP %s: %w", s.Name, err)
	}
	s.Bots, s.ReadTools = bots, reads
	return s, nil
}

// Allows reports whether this server is turned on for the bot. instanceID
// is the runtime instance. botID is the hub bot id from the turn. An empty
// Bots list is every bot of the owner. Either id matching an entry is enough.
func (s Server) Allows(instanceID, botID string) bool {
	if len(s.Bots) == 0 {
		return true
	}
	instanceID = strings.TrimSpace(instanceID)
	botID = strings.TrimSpace(botID)
	for _, id := range s.Bots {
		if id != "" && (id == instanceID || id == botID) {
			return true
		}
	}
	return false
}

// ToolNeedsApproval is false only for a name the owner listed in ReadTools
// that is not a send, publish, pay, or delete. Anything else, including a
// tool the server annotated as read-only, waits for one confirmation.
func ToolNeedsApproval(name string, readTools []string) bool {
	if effectTool(name) {
		return true
	}
	name = strings.TrimSpace(name)
	for _, allowed := range readTools {
		if strings.TrimSpace(allowed) == name {
			return false
		}
	}
	return true
}

func effectTool(name string) bool {
	for _, word := range []string{"发布", "支付", "付款", "删除", "发送", "转账", "转发"} {
		if strings.Contains(name, word) {
			return true
		}
	}
	parts := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	for _, part := range parts {
		switch part {
		case "send", "publish", "post", "pay", "charge", "delete", "drop", "email", "transfer", "mail":
			return true
		}
	}
	return false
}

// ConfigStamp changes when the destination, command, secret, or the bot
// and read-tool scope changes. It is a hash. The secret is not recoverable
// from it.
func ConfigStamp(s Server) string {
	return hashJSON(map[string]any{
		"id": s.ID, "transport": s.Transport, "url": s.URL,
		"command": s.Command, "args": s.Args,
		"headers": s.Headers, "secret": s.AuthSecret, "env": s.Env,
		"disabled": s.Disabled, "bots": s.Bots, "read": s.ReadTools,
	})
}

// BindingStamp binds one call: the server config, the tool, its schema, and
// the arguments. A config or schema change is a different stamp, so an older
// confirmation does not apply.
func BindingStamp(s Server, tool string, schema map[string]any, args map[string]any) (string, error) {
	schemaRaw, err := json.Marshal(schema)
	if err != nil {
		return "", err
	}
	argsRaw, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	sum := hashJSON(map[string]any{
		"config": ConfigStamp(s),
		"tool":   strings.TrimSpace(tool),
		"schema": string(schemaRaw),
		"args":   string(argsRaw),
	})
	if sum == "" {
		return "", fmt.Errorf("mcp approval stamp failed")
	}
	return sum, nil
}

func hashJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func cleanIDList(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > maxBotIDs {
		return nil, fmt.Errorf("too many bots")
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, id := range in {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if !idPattern(id) {
			return nil, fmt.Errorf("bot id %s is invalid", id)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func cleanToolNames(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > maxReadTools {
		return nil, fmt.Errorf("too many read tools")
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, name := range in {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if utf8.RuneCountInString(name) > maxNameRunes || strings.ContainsAny(name, " \r\n\t") {
			return nil, fmt.Errorf("read tool %s is invalid", name)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func cleanEnv(in map[string]string) (map[string]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > maxEnv {
		return nil, fmt.Errorf("too many env values")
	}
	out := map[string]string{}
	for k, v := range in {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		if strings.ContainsAny(k, "\r\n=") || strings.ContainsAny(v, "\r\n") || utf8.RuneCountInString(k) > 80 || utf8.RuneCountInString(v) > maxEnvRunes {
			return nil, fmt.Errorf("env %s is invalid", k)
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func cleanMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, v := range in {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || strings.ContainsAny(k, "\r\n") || strings.ContainsAny(v, "\r\n") {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func idFor(id, name string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		id = strings.TrimSpace(name)
	}
	if idPattern(id) {
		return id
	}
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(name))))
	return "mcp-" + hex.EncodeToString(sum[:4])
}

func idPattern(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i, r := range id {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.'
		if !ok || (i == 0 && (r == '-' || r == '_' || r == '.')) {
			return false
		}
	}
	return true
}

// CleanSkillName is the name used as a directory and as the bot's lookup key.
func CleanSkillName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxNameRunes {
		return "", fmt.Errorf("skill name is required")
	}
	if strings.ContainsAny(name, "/\\\r\n\x00") || name == "." || name == ".." || strings.Contains(name, "..") {
		return "", fmt.Errorf("skill name %q is not allowed", name)
	}
	return name, nil
}

func cleanSkill(s Skill) (Skill, error) {
	var err error
	s.Name, err = CleanSkillName(s.Name)
	if err != nil {
		return Skill{}, err
	}
	s.Description = strings.TrimSpace(s.Description)
	if utf8.RuneCountInString(s.Description) > 400 {
		s.Description = string([]rune(s.Description)[:400])
	}
	s.Body = strings.TrimSpace(s.Body)
	if s.Body == "" {
		return Skill{}, fmt.Errorf("skill %s has no text", s.Name)
	}
	if utf8.RuneCountInString(s.Body) > maxSkillRunes {
		return Skill{}, fmt.Errorf("skill %s is too long", s.Name)
	}
	return s, nil
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Catalog is the text a bot reads with action=cap_list.
func Catalog(doc Document) string {
	var b strings.Builder
	n := 0
	for _, s := range doc.MCP {
		if s.Disabled {
			continue
		}
		if n == 0 {
			b.WriteString("MCP servers:\n")
		}
		n++
		fmt.Fprintf(&b, "- %s (%s). Call with action=cap_mcp mcp_server=%s\n", oneLine(s.Name), s.Transport, s.ID)
	}
	if n == 0 {
		b.WriteString("MCP servers: none.\n")
	}
	if len(doc.Skills) == 0 {
		b.WriteString("Skills: none.")
		return b.String()
	}
	b.WriteString("Skills:\n")
	for _, s := range doc.Skills {
		fmt.Fprintf(&b, "- %s", oneLine(s.Name))
		if s.Description != "" {
			fmt.Fprintf(&b, ": %s", oneLine(s.Description))
		}
		b.WriteByte('\n')
	}
	b.WriteString("Read one with action=cap_skill name=<skill> and follow that text.")
	return b.String()
}

// LookupMCP finds an enabled server by id or name.
func LookupMCP(doc Document, ref string) (Server, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Server{}, fmt.Errorf("mcp_server is required")
	}
	var found *Server
	for i := range doc.MCP {
		s := &doc.MCP[i]
		if strings.EqualFold(s.ID, ref) || strings.EqualFold(s.Name, ref) {
			if found != nil {
				return Server{}, fmt.Errorf("more than one MCP matches %s", ref)
			}
			found = s
		}
	}
	if found == nil {
		return Server{}, fmt.Errorf("no MCP named %s; action=cap_list shows the ones that are on", ref)
	}
	if found.Disabled {
		return Server{}, fmt.Errorf("MCP %s is turned off", found.Name)
	}
	return *found, nil
}

// LookupSkill returns one skill body.
func LookupSkill(doc Document, name string) (Skill, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Skill{}, fmt.Errorf("skill name is required")
	}
	for _, s := range doc.Skills {
		if strings.EqualFold(s.Name, name) {
			return s, nil
		}
	}
	return Skill{}, fmt.Errorf("no skill named %s; action=cap_list shows them", name)
}
