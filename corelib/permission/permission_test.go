package permission

import (
	"os"
	"path/filepath"
	"testing"
)

func snap(t *testing.T, rules ...Rule) *Snapshot {
	t.Helper()
	s, err := Load(Options{ManagedRules: rules})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s
}

func kindPtr(k AccessKind) *AccessKind { return &k }

func TestDecideEffectPrecedence(t *testing.T) {
	tests := []struct {
		name  string
		rules []Rule
		want  Effect
	}{
		{
			name: "deny beats allow regardless of order (allow first)",
			rules: []Rule{
				{Tool: "bash", Effect: EffectAllow},
				{Tool: "bash", Effect: EffectDeny},
			},
			want: EffectDeny,
		},
		{
			name: "deny beats allow regardless of order (deny first)",
			rules: []Rule{
				{Tool: "bash", Effect: EffectDeny},
				{Tool: "bash", Effect: EffectAllow},
			},
			want: EffectDeny,
		},
		{
			name: "ask beats allow",
			rules: []Rule{
				{Tool: "bash", Effect: EffectAllow},
				{Tool: "bash", Effect: EffectAsk},
			},
			want: EffectAsk,
		},
		{
			name: "deny beats ask",
			rules: []Rule{
				{Tool: "bash", Effect: EffectAsk},
				{Tool: "bash", Effect: EffectDeny},
			},
			want: EffectDeny,
		},
		{
			name: "single allow matches",
			rules: []Rule{
				{Tool: "bash", Effect: EffectAllow},
			},
			want: EffectAllow,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := snap(t, tt.rules...).Decide("bash", KindExecute, nil)
			if d.Default || d.Effect != tt.want {
				t.Fatalf("Decide = %+v, want effect %q", d, tt.want)
			}
		})
	}
}

func TestDecideSpecificity(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		rules []Rule
		args  map[string]interface{}
		want  string // reason of the winning rule
	}{
		{
			name: "exact tool beats star within same effect",
			tool: "bash",
			rules: []Rule{
				{Tool: "*", Effect: EffectDeny, Reason: "star"},
				{Tool: "bash", Effect: EffectDeny, Reason: "exact"},
			},
			want: "exact",
		},
		{
			name: "when-rule beats no-when within same effect",
			tool: "database",
			rules: []Rule{
				{Tool: "database", Effect: EffectDeny, Reason: "plain"},
				{Tool: "database", Effect: EffectDeny, Reason: "when", When: &ArgsPredicate{Field: "op", Equals: "write"}},
			},
			args: map[string]interface{}{"op": "write"},
			want: "when",
		},
		{
			name: "when-rule loses when predicate does not match",
			tool: "database",
			rules: []Rule{
				{Tool: "database", Effect: EffectDeny, Reason: "plain"},
				{Tool: "database", Effect: EffectDeny, Reason: "when", When: &ArgsPredicate{Field: "op", Equals: "write"}},
			},
			args: map[string]interface{}{"op": "read"},
			want: "plain",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := snap(t, tt.rules...).Decide(tt.tool, "", tt.args)
			if d.Default || d.Rule == nil || d.Rule.Reason != tt.want {
				t.Fatalf("Decide = %+v, want rule reason %q", d, tt.want)
			}
		})
	}
}

func TestKindMatching(t *testing.T) {
	tests := []struct {
		name string
		rule Rule
		kind AccessKind
		want bool // matched?
	}{
		{
			name: "kind-qualified rule matches same kind",
			rule: Rule{Tool: "bash", Effect: EffectAllow, Kind: kindPtr(KindExecute)},
			kind: KindExecute,
			want: true,
		},
		{
			name: "kind-qualified rule does not match other kind",
			rule: Rule{Tool: "bash", Effect: EffectAllow, Kind: kindPtr(KindExecute)},
			kind: KindRead,
			want: false,
		},
		{
			name: "kind-qualified rule does not match unknown kind",
			rule: Rule{Tool: "bash", Effect: EffectAllow, Kind: kindPtr(KindExecute)},
			kind: "",
			want: false,
		},
		{
			name: "unqualified rule matches any kind",
			rule: Rule{Tool: "bash", Effect: EffectAllow},
			kind: KindRead,
			want: true,
		},
		{
			name: "unqualified rule matches unknown kind",
			rule: Rule{Tool: "bash", Effect: EffectAllow},
			kind: "",
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := snap(t, tt.rule).Decide("bash", tt.kind, nil)
			if got := !d.Default; got != tt.want {
				t.Fatalf("matched = %v, want %v (decision %+v)", got, tt.want, d)
			}
		})
	}
}

func TestArgsPredicate(t *testing.T) {
	tests := []struct {
		name string
		pred ArgsPredicate
		args map[string]interface{}
		want bool
	}{
		{"equals match", ArgsPredicate{Field: "op", Equals: "write"}, map[string]interface{}{"op": "write"}, true},
		{"equals mismatch", ArgsPredicate{Field: "op", Equals: "write"}, map[string]interface{}{"op": "read"}, false},
		{"in match", ArgsPredicate{Field: "op", In: []string{"read", "write"}}, map[string]interface{}{"op": "read"}, true},
		{"in mismatch", ArgsPredicate{Field: "op", In: []string{"read"}}, map[string]interface{}{"op": "write"}, false},
		{"missing key", ArgsPredicate{Field: "op", Equals: "write"}, map[string]interface{}{"other": "write"}, false},
		{"nil args", ArgsPredicate{Field: "op", Equals: "write"}, nil, false},
		{"empty field never matches", ArgsPredicate{Equals: "write"}, map[string]interface{}{"op": "write"}, false},
		{"empty field with in never matches", ArgsPredicate{In: []string{"write"}}, map[string]interface{}{"op": "write"}, false},
		{"equals empty means no equals arm", ArgsPredicate{Field: "op"}, map[string]interface{}{"op": "write"}, false},
		{"non-string value stringified", ArgsPredicate{Field: "n", Equals: "42"}, map[string]interface{}{"n": 42}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build the snapshot directly: empty-field predicates are
			// rejected by Load (fail-closed), but at evaluation time they
			// simply never match.
			s := &Snapshot{rules: []Rule{{Tool: "x", Effect: EffectDeny, When: &tt.pred}}}
			d := s.Decide("x", "", tt.args)
			if got := !d.Default; got != tt.want {
				t.Fatalf("matched = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecideNoMatchIsDefault(t *testing.T) {
	s := snap(t, Rule{Tool: "bash", Effect: EffectDeny})
	d := s.Decide("read_file", KindRead, nil)
	if !d.Default || d.Effect != "" || d.Rule != nil {
		t.Fatalf("Decide = %+v, want zero default decision", d)
	}
}

func TestDenyAllSnapshot(t *testing.T) {
	s := DenyAllSnapshot("rule source unreadable: /tmp/x.json")
	for _, tc := range []struct {
		tool string
		kind AccessKind
		args map[string]interface{}
	}{
		{"bash", KindExecute, map[string]interface{}{"cmd": "rm -rf /"}},
		{"read_file", KindRead, nil},
		{"anything", "", nil},
	} {
		d := s.Decide(tc.tool, tc.kind, tc.args)
		if d.Default || d.Effect != EffectDeny || d.Rule == nil {
			t.Fatalf("Decide(%q) = %+v, want deny", tc.tool, d)
		}
		if d.Rule.Reason != "rule source unreadable: /tmp/x.json" {
			t.Fatalf("reason = %q", d.Rule.Reason)
		}
	}
}

func TestBuiltinKind(t *testing.T) {
	tests := []struct {
		tool string
		want AccessKind
	}{
		{"bash", KindExecute},
		{"ssh", KindExecute},
		{"read_file", KindRead},
		{"grep", KindRead},
		{"edit_file", KindMutate},
		{"write_file", KindMutate},
		{"manage_skill", KindMutate},
		{"unknown_tool", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := BuiltinKind(tt.tool); got != tt.want {
			t.Errorf("BuiltinKind(%q) = %q, want %q", tt.tool, got, tt.want)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupProjectDir creates a project dir with .maclaw/permission.json and
// .claude/settings.json and returns the dir plus the user config path.
func setupProjectDir(t *testing.T) (projectDir, userConfigPath string) {
	t.Helper()
	base := t.TempDir()
	projectDir = filepath.Join(base, "proj")
	userConfigPath = filepath.Join(base, "user-config.json")

	writeFile(t, userConfigPath, `{
		"permission": {
			"rules": [
				{"tool": "bash", "effect": "allow"},
				{"tool": "web_fetch", "effect": "ask"},
				{"tool": "*", "effect": "allow"}
			]
		}
	}`)
	writeFile(t, filepath.Join(projectDir, ".maclaw", "permission.json"), `{
		"rules": [
			{"tool": "web_fetch", "effect": "allow"},
			{"tool": "memory", "effect": "deny", "reason": "no memory"}
		]
	}`)
	writeFile(t, filepath.Join(projectDir, ".claude", "settings.json"), `{
		"permissions": {
			"deny": ["ssh"],
			"ask": ["browser"],
			"allow": ["edit_file"]
		}
	}`)
	return projectDir, userConfigPath
}

func TestLoadPrecedenceAcrossSources(t *testing.T) {
	projectDir, userConfigPath := setupProjectDir(t)
	s, err := Load(Options{
		ManagedRules:   []Rule{{Tool: "bash", Effect: EffectDeny, Reason: "managed deny"}},
		UserConfigPath: userConfigPath,
		ProjectDir:     projectDir,
		TrustProject:   true,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	tests := []struct {
		name string
		tool string
		kind AccessKind
		want Effect
		src  string
	}{
		// Global effect priority across sources: user allow < managed deny.
		{"managed deny beats user allow for bash", "bash", KindExecute, EffectDeny, "managed"},
		// user ask beats project allow for web_fetch.
		{"user ask beats project allow", "web_fetch", KindRead, EffectAsk, "user-config"},
		// project deny applies.
		{"project deny", "memory", KindMutate, EffectDeny, "project"},
		// claude-fallback deny applies.
		{"claude deny", "ssh", KindExecute, EffectDeny, "claude-fallback"},
		// claude-fallback ask applies.
		{"claude ask", "browser", KindExecute, EffectAsk, "claude-fallback"},
		// user catch-all allow covers everything else.
		{"catch-all allow", "read_file", KindRead, EffectAllow, "user-config"},
		// project rule does not shadow unrelated decisions.
		{"claude allow", "edit_file", KindMutate, EffectAllow, "claude-fallback"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := s.Decide(tt.tool, tt.kind, nil)
			if d.Default || d.Effect != tt.want || d.Rule.Source != tt.src {
				t.Fatalf("Decide(%q) = %+v (source %q), want %q from %q",
					tt.tool, d, ruleSource(d), tt.want, tt.src)
			}
		})
	}
}

func ruleSource(d Decision) string {
	if d.Rule == nil {
		return ""
	}
	return d.Rule.Source
}

func TestLoadTrustProjectFalseSkipsProjectRules(t *testing.T) {
	projectDir, userConfigPath := setupProjectDir(t)
	s, err := Load(Options{
		UserConfigPath: userConfigPath,
		ProjectDir:     projectDir,
		TrustProject:   false,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// project deny on memory must not apply → falls to user catch-all allow.
	if d := s.Decide("memory", KindMutate, nil); d.Default || d.Effect != EffectAllow {
		t.Fatalf("memory = %+v, want allow (project rule skipped)", d)
	}
	// claude-fallback deny on ssh must not apply either.
	if d := s.Decide("ssh", KindExecute, nil); d.Default || d.Effect != EffectAllow {
		t.Fatalf("ssh = %+v, want allow (claude rule skipped)", d)
	}
}

func TestLoadYoloPinStripsCatchAllAllows(t *testing.T) {
	projectDir, userConfigPath := setupProjectDir(t)
	opts := Options{
		UserConfigPath: userConfigPath,
		ProjectDir:     projectDir,
		TrustProject:   true,
	}

	// Without pin: catch-all allow from user config applies.
	s, err := Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if d := s.Decide("read_file", KindRead, nil); d.Default || d.Effect != EffectAllow {
		t.Fatalf("unpinned read_file = %+v, want catch-all allow", d)
	}

	// With pin: catch-all allow stripped → no match → default. Specific
	// allows survive (bash from user config), specific denies survive.
	opts.ManagedYoloPin = true
	s, err = Load(opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if d := s.Decide("read_file", KindRead, nil); !d.Default {
		t.Fatalf("pinned read_file = %+v, want default (catch-all stripped)", d)
	}
	if d := s.Decide("bash", KindExecute, nil); d.Default || d.Effect != EffectAllow {
		t.Fatalf("pinned bash = %+v, want specific allow kept", d)
	}
	// A catch-all ask is not an allow and must survive the pin.
	if d := s.Decide("web_fetch", KindRead, nil); d.Default || d.Effect != EffectAsk {
		t.Fatalf("pinned web_fetch = %+v, want ask kept", d)
	}
	// Project/claude rules still apply.
	if d := s.Decide("memory", KindMutate, nil); d.Default || d.Effect != EffectDeny {
		t.Fatalf("pinned memory = %+v, want project deny", d)
	}
}

func TestLoadMalformedJSONFailsClosed(t *testing.T) {
	projectDir, userConfigPath := setupProjectDir(t)
	// Corrupt the user config.
	writeFile(t, userConfigPath, `{not json`)

	s, err := Load(Options{
		ManagedRules:   []Rule{{Tool: "bash", Effect: EffectAllow}},
		UserConfigPath: userConfigPath,
		ProjectDir:     projectDir,
		TrustProject:   true,
	})
	if err == nil {
		t.Fatal("Load: want error for malformed source")
	}
	if s == nil {
		t.Fatal("Load: want fail-closed snapshot, got nil")
	}
	d := s.Decide("bash", KindExecute, nil)
	if d.Default || d.Effect != EffectDeny {
		t.Fatalf("Decide = %+v, want deny", d)
	}
	wantReason := "rule source unreadable: " + userConfigPath
	if d.Rule == nil || d.Rule.Reason != wantReason {
		t.Fatalf("reason = %q, want %q", d.Rule.Reason, wantReason)
	}

	// Malformed project file also fails closed.
	writeFile(t, userConfigPath, `{"permission": {"rules": []}}`)
	writeFile(t, filepath.Join(projectDir, ".maclaw", "permission.json"), `]`)
	s, err = Load(Options{
		UserConfigPath: userConfigPath,
		ProjectDir:     projectDir,
		TrustProject:   true,
	})
	if err == nil {
		t.Fatal("Load: want error for malformed project source")
	}
	d = s.Decide("anything", "", nil)
	if d.Default || d.Effect != EffectDeny {
		t.Fatalf("Decide = %+v, want deny", d)
	}
}

func TestLoadSemanticallyInvalidRuleFailsClosed(t *testing.T) {
	projectDir, userConfigPath := setupProjectDir(t)
	// Unknown effect is a malformed source.
	writeFile(t, userConfigPath, `{"permission": {"rules": [{"tool": "bash", "effect": "maybe"}]}}`)
	s, err := Load(Options{UserConfigPath: userConfigPath, ProjectDir: projectDir, TrustProject: true})
	if err == nil {
		t.Fatal("Load: want error for invalid effect")
	}
	if d := s.Decide("bash", KindExecute, nil); d.Default || d.Effect != EffectDeny {
		t.Fatalf("Decide = %+v, want deny", d)
	}
}

func TestLoadMissingFilesNoError(t *testing.T) {
	base := t.TempDir()
	s, err := Load(Options{
		UserConfigPath: filepath.Join(base, "nope.json"),
		ProjectDir:     filepath.Join(base, "no-such-project"),
		TrustProject:   true,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// No rules at all: every decision is a default.
	if d := s.Decide("bash", KindExecute, nil); !d.Default {
		t.Fatalf("Decide = %+v, want default", d)
	}
	// Managed rules still work alongside absent files.
	s, err = Load(Options{
		ManagedRules:   []Rule{{Tool: "bash", Effect: EffectAsk}},
		UserConfigPath: filepath.Join(base, "nope.json"),
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if d := s.Decide("bash", KindExecute, nil); d.Default || d.Effect != EffectAsk {
		t.Fatalf("Decide = %+v, want ask", d)
	}
}

func TestClaudeFallbackParsing(t *testing.T) {
	projectDir := t.TempDir()
	writeFile(t, filepath.Join(projectDir, ".claude", "settings.json"), `{
		"permissions": {
			"deny": ["WebSearch", "bash"],
			"ask": ["mcp__docs__query"],
			"allow": ["read_file", "edit_file"]
		}
	}`)
	s, err := Load(Options{ProjectDir: projectDir, TrustProject: true})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	tests := []struct {
		tool string
		want Effect
	}{
		{"WebSearch", EffectDeny},
		{"bash", EffectDeny},
		{"mcp__docs__query", EffectAsk},
		{"read_file", EffectAllow},
		{"edit_file", EffectAllow},
		{"unlisted_tool", ""}, // no match → default
	}
	for _, tt := range tests {
		d := s.Decide(tt.tool, "", nil)
		if tt.want == "" {
			if !d.Default {
				t.Fatalf("Decide(%q) = %+v, want default", tt.tool, d)
			}
			continue
		}
		if d.Default || d.Effect != tt.want || d.Rule.Source != "claude-fallback" {
			t.Fatalf("Decide(%q) = %+v (source %q), want %q from claude-fallback",
				tt.tool, d, ruleSource(d), tt.want)
		}
	}
}

func TestLoadKindQualifiedRuleFromJSON(t *testing.T) {
	base := t.TempDir()
	userConfigPath := filepath.Join(base, "user-config.json")
	writeFile(t, userConfigPath, `{
		"permission": {
			"rules": [
				{"tool": "database", "effect": "deny", "kind": "mutate",
				 "when": {"field": "op", "in": ["drop", "delete"]},
				 "reason": "destructive op"}
			]
		}
	}`)
	s, err := Load(Options{UserConfigPath: userConfigPath})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	tests := []struct {
		name string
		kind AccessKind
		args map[string]interface{}
		want Effect // "" means default
	}{
		{"matching kind and op", KindMutate, map[string]interface{}{"op": "drop"}, EffectDeny},
		{"non-matching kind", KindRead, map[string]interface{}{"op": "drop"}, ""},
		{"matching kind non-matching op", KindMutate, map[string]interface{}{"op": "select"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := s.Decide("database", tt.kind, tt.args)
			if tt.want == "" {
				if !d.Default {
					t.Fatalf("Decide = %+v, want default", d)
				}
				return
			}
			if d.Default || d.Effect != tt.want || d.Rule.Reason != "destructive op" {
				t.Fatalf("Decide = %+v, want %q", d, tt.want)
			}
		})
	}
}

// TestLoadEmptyUserConfigIsAbsentSource: a 0-byte user config file must be
// treated like a missing one — managed rules load normally instead of the
// whole Load failing closed on an Unmarshal error.
func TestLoadEmptyUserConfigIsAbsentSource(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "config.json")
	if err := os.WriteFile(empty, []byte{}, 0o644); err != nil {
		t.Fatalf("write empty config: %v", err)
	}
	whitespace := filepath.Join(dir, "config-ws.json")
	if err := os.WriteFile(whitespace, []byte("  \n\t\n"), 0o644); err != nil {
		t.Fatalf("write whitespace config: %v", err)
	}
	for _, path := range []string{empty, whitespace} {
		s, err := Load(Options{
			ManagedRules:   []Rule{{Tool: "bash", Effect: EffectAsk, Reason: "managed"}},
			UserConfigPath: path,
		})
		if err != nil {
			t.Fatalf("Load(%s): %v", path, err)
		}
		d := s.Decide("bash", KindExecute, nil)
		if d.Default || d.Effect != EffectAsk || d.Rule.Reason != "managed" {
			t.Fatalf("Decide = %+v, want managed ask rule", d)
		}
	}
}

// TestLoadRejectsDeadPredicateRule: a When predicate with a field but no
// matchers is permanently dead; validateRules must fail the source closed
// rather than silently carrying an inert rule.
func TestLoadRejectsDeadPredicateRule(t *testing.T) {
	s, err := Load(Options{ManagedRules: []Rule{
		{Tool: "read_file", Effect: EffectDeny, When: &ArgsPredicate{Field: "path"}},
	}})
	if err == nil {
		t.Fatal("Load must reject a dead-predicate rule")
	}
	if s == nil {
		t.Fatal("fail-closed snapshot required")
	}
	d := s.Decide("read_file", KindRead, map[string]interface{}{"path": "/etc/passwd"})
	if d.Default || d.Effect != EffectDeny {
		t.Fatalf("fail-closed deny expected, got %+v", d)
	}
}

// TestDecideRuleReturnsCopy: mutating the Rule pointer in a Decision must
// not leak back into the snapshot.
func TestDecideRuleReturnsCopy(t *testing.T) {
	s := snap(t, Rule{Tool: "bash", Effect: EffectDeny, Reason: "original"})
	d1 := s.Decide("bash", KindExecute, nil)
	if d1.Rule == nil || d1.Rule.Reason != "original" {
		t.Fatalf("Decide = %+v", d1)
	}
	d1.Rule.Reason = "mutated"
	d1.Rule.Effect = EffectAllow
	d2 := s.Decide("bash", KindExecute, nil)
	if d2.Rule == nil || d2.Rule.Reason != "original" || d2.Rule.Effect != EffectDeny {
		t.Fatalf("snapshot rule was mutated through Decision.Rule: %+v", d2.Rule)
	}
}

// TestHasArgsRules reports whether the snapshot contains any When-predicate
// rule, letting callers skip per-call args parsing on the hot path.
func TestHasArgsRules(t *testing.T) {
	if snap(t, Rule{Tool: "bash", Effect: EffectDeny}).HasArgsRules() {
		t.Fatal("name-only snapshot must report no args rules")
	}
	if !snap(t, Rule{Tool: "bash", Effect: EffectDeny, When: &ArgsPredicate{Field: "path", Prefix: "/"}}).HasArgsRules() {
		t.Fatal("snapshot with a When rule must report args rules")
	}
	if !snap(t,
		Rule{Tool: "bash", Effect: EffectDeny},
		Rule{Tool: "read_file", Effect: EffectDeny, When: &ArgsPredicate{Field: "path", Prefix: "/"}},
	).HasArgsRules() {
		t.Fatal("mixed snapshot must report args rules")
	}
}
