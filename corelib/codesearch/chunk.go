package codesearch

import (
	"regexp"
	"strings"
)

const (
	maxChunkBytes = 8192
	windowLines   = 60
	windowStride  = 40
	maxSnippetLn  = 40
	maxSnippetRun = 1500
)

var (
	goSymbol = regexp.MustCompile(`^func(?:\s+\([^)]*\))?\s+([A-Za-z_][A-Za-z0-9_]*)`)
	goValue  = regexp.MustCompile(`^(?:type|var|const)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	pySymbol = regexp.MustCompile(`^(?:async\s+def|def|class)\s+([A-Za-z_][A-Za-z0-9_]*)`)
)

type rawChunk struct {
	path   string
	start  int
	end    int
	symbol string
	text   string
}

func chunkSource(rel, src string) []rawChunk {
	// A leading UTF-8 BOM is not source. Left on the first line it hides a
	// "//" banner, and a parenthesis in that banner becomes a fake declaration.
	src = strings.TrimPrefix(src, "\uFEFF")
	switch ext := strings.ToLower(extOf(rel)); ext {
	case ".go":
		return chunkDecls(rel, src, isGoDecl, goDeclSymbol, false)
	case ".py", ".pyi":
		return chunkDecls(rel, src, isPyDecl, pyDeclSymbol, true)
	default:
		// C and C++ have no indent structure to follow. A column-0 definition
		// is the same split Go uses, so a 60-line window starts at the
		// signature instead of cutting the body off the end.
		if cLikeExt(ext) {
			return chunkDecls(rel, src, isCDecl, cDeclSymbol, false)
		}
		return explode(rel, lineGroup{start: 1, lines: splitLines(src)})
	}
}

func cLikeExt(ext string) bool {
	switch ext {
	case ".c", ".h", ".hh", ".hpp", ".cc", ".cpp", ".cxx":
		return true
	default:
		return false
	}
}

func extOf(rel string) string {
	slash := strings.ReplaceAll(rel, "\\", "/")
	name := slash
	if i := strings.LastIndex(slash, "/"); i >= 0 {
		name = slash[i+1:]
	}
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i:]
	}
	return ""
}

func isGoDecl(line string) bool {
	return strings.HasPrefix(line, "func ") || strings.HasPrefix(line, "type ") || strings.HasPrefix(line, "var ") || strings.HasPrefix(line, "const ")
}

func goDeclSymbol(line string) string {
	if m := goSymbol.FindStringSubmatch(line); len(m) == 2 {
		return m[1]
	}
	if m := goValue.FindStringSubmatch(line); len(m) == 2 {
		return m[1]
	}
	return ""
}

func isPyDecl(line string) bool {
	line = strings.TrimLeft(line, " \t")
	return strings.HasPrefix(line, "def ") || strings.HasPrefix(line, "class ") || strings.HasPrefix(line, "async def ")
}

func pyDeclSymbol(line string) string {
	line = strings.TrimLeft(line, " \t")
	if m := pySymbol.FindStringSubmatch(line); len(m) == 2 {
		return m[1]
	}
	return ""
}

// isCDecl reports a column-0 function or function-pointer declaration. The line
// needs a return type and a name before '(', so a call or an if/for does not
// split the body. A pointer declarator such as
// typedef HANDLE (WINAPI *ImmAssociateContextFn)(...) is named after the '*':
// the token before '(' is the return type. '=' or ';' before the paren is an
// initializer or a comment, not a definition. A file-scope variable stays in
// the surrounding group: giving each one its own chunk makes a short comment
// outrank the function that implements it.
func isCDecl(line string) bool {
	_, ok := cDeclName(line)
	return ok
}

func cDeclSymbol(line string) string {
	name, _ := cDeclName(line)
	return name
}

func cDeclName(line string) (string, bool) {
	if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' || line[0] == '/' || line[0] == '*' || line[0] == '{' || line[0] == '}' {
		return "", false
	}
	paren := strings.IndexByte(line, '(')
	if paren <= 0 {
		return "", false
	}
	head := line[:paren]
	if strings.ContainsAny(head, "=;{}") {
		return "", false
	}
	fields := strings.Fields(head)
	if len(fields) == 0 || cStmtWord(fields[0]) {
		return "", false
	}
	if name, ok := cPointerDeclName(line[paren:]); ok {
		return name, true
	}
	if len(fields) < 2 {
		return "", false
	}
	name := strings.TrimRight(fields[len(fields)-1], "*&")
	if i := strings.LastIndex(name, "::"); i >= 0 {
		name = name[i+2:]
	}
	name = strings.TrimLeft(name, "~*&")
	if !cIdent(name) {
		return "", false
	}
	return name, true
}

// cPointerDeclName reads a function-pointer declarator that opens at fromParen.
// (WINAPI *Name)(args) and (*Name)(args) both name Name. A parameter list
// such as (int *x) does not: after the name there is no second '('.
func cPointerDeclName(fromParen string) (string, bool) {
	if len(fromParen) < 4 || fromParen[0] != '(' {
		return "", false
	}
	i := 1
	for {
		i = cSkipSpace(fromParen, i)
		if i >= len(fromParen) {
			return "", false
		}
		if fromParen[i] == '*' {
			break
		}
		ident, next := cReadIdent(fromParen, i)
		if ident == "" {
			return "", false
		}
		i = next
	}
	sawStar := false
	for {
		i = cSkipSpace(fromParen, i)
		if i >= len(fromParen) || fromParen[i] != '*' {
			break
		}
		sawStar = true
		i++
	}
	if !sawStar {
		return "", false
	}
	i = cSkipSpace(fromParen, i)
	name, next := cReadIdent(fromParen, i)
	if !cIdent(name) {
		return "", false
	}
	i = cSkipSpace(fromParen, next)
	if i >= len(fromParen) || fromParen[i] != ')' {
		return "", false
	}
	i = cSkipSpace(fromParen, i+1)
	if i >= len(fromParen) || fromParen[i] != '(' {
		return "", false
	}
	return name, true
}

func cSkipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

func cReadIdent(s string, i int) (string, int) {
	start := i
	for i < len(s) {
		c := s[i]
		ok := c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
		if i > start {
			ok = ok || (c >= '0' && c <= '9')
		}
		if !ok {
			break
		}
		i++
	}
	if i == start {
		return "", start
	}
	return s[start:i], i
}

func cIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
		if i > 0 {
			ok = ok || (c >= '0' && c <= '9')
		}
		if !ok {
			return false
		}
	}
	return true
}

func cStmtWord(w string) bool {
	switch w {
	case "if", "for", "while", "switch", "catch", "else", "do", "return",
		"case", "goto", "sizeof", "alignof", "decltype", "typeof", "typeid",
		"static_assert", "assert", "_Static_assert", "new", "delete",
		"co_await", "co_return", "co_yield", "throw", "and", "or", "not", "xor":
		return true
	default:
		return false
	}
}

// cScope is the brace scope of a C or C++ file. A method indented inside a
// class or struct is a declaration. A line indented inside a function is not:
// a wrapped call or a string that contains '(' would otherwise become a symbol.
type cScope struct {
	stack []byte // 'c' class or struct, 'f' function, 'b' other block
	await bool   // a declaration is waiting for its body
	paren int
}

func (s *cScope) inClass() bool {
	return len(s.stack) > 0 && s.stack[len(s.stack)-1] == 'c'
}

func (s *cScope) inFunction() bool {
	for _, k := range s.stack {
		if k == 'f' {
			return true
		}
	}
	return false
}

func (s *cScope) feed(line string, openedDecl bool) {
	if openedDecl {
		s.await = true
		s.paren = 0
	}
	paren := 0
	if s.await {
		paren = s.paren
	}
	classLine := !openedDecl && cLineOpensClass(line)
	classUsed := false
	mode := 0 // 0 code, 2 block comment, 3 string, 4 char
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch mode {
		case 2:
			if c == '*' && i+1 < len(line) && line[i+1] == '/' {
				mode = 0
				i++
			}
			continue
		case 3, 4:
			if c == '\\' && i+1 < len(line) {
				i++
				continue
			}
			q := byte('"')
			if mode == 4 {
				q = '\''
			}
			if c == q {
				mode = 0
			}
			continue
		}
		if c == '/' && i+1 < len(line) && line[i+1] == '/' {
			break
		}
		if c == '/' && i+1 < len(line) && line[i+1] == '*' {
			mode = 2
			i++
			continue
		}
		if c == '"' {
			mode = 3
			continue
		}
		if c == '\'' {
			mode = 4
			continue
		}
		switch c {
		case '(':
			paren++
		case ')':
			if paren > 0 {
				paren--
			}
		case ';':
			if s.await && paren == 0 {
				s.await = false
			}
		case '{':
			if paren != 0 {
				continue
			}
			if s.await {
				s.stack = append(s.stack, 'f')
				s.await = false
				paren = 0
				continue
			}
			kind := byte('b')
			if classLine && !classUsed && !s.inFunction() {
				kind = 'c'
				classUsed = true
			}
			s.stack = append(s.stack, kind)
		case '}':
			if paren != 0 {
				continue
			}
			if len(s.stack) > 0 {
				s.stack = s.stack[:len(s.stack)-1]
			}
		}
	}
	if s.await {
		s.paren = paren
	}
}

func cLineOpensClass(line string) bool {
	trim := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trim, "typedef") {
		trim = strings.TrimSpace(strings.TrimPrefix(trim, "typedef"))
	}
	return strings.HasPrefix(trim, "class ") || strings.HasPrefix(trim, "class\t") || strings.HasPrefix(trim, "class{") ||
		strings.HasPrefix(trim, "struct ") || strings.HasPrefix(trim, "struct\t") || strings.HasPrefix(trim, "struct{")
}

type lineGroup struct {
	start  int
	symbol string
	lines  []string
}

func chunkDecls(rel, src string, isDecl func(string) bool, symbol func(string) string, decorators bool) []rawChunk {
	if decorators {
		return chunkPython(rel, src, isDecl, symbol)
	}
	lines := splitLines(src)
	var groups []lineGroup
	var cur *lineGroup
	var decor []string
	decorAt := 0
	decorDepth := 0
	var decorQuote quoteState
	clearDecor := func() {
		decor = nil
		decorAt = 0
		decorDepth = 0
		decorQuote = quoteState{}
	}
	flush := func() {
		if cur == nil {
			return
		}
		groups = append(groups, *cur)
		cur = nil
	}
	// A column-0 func or def inside a comment or a multi-line string is not
	// a declaration. Treating it as one ranks the rest of the enclosing body
	// under a symbol that does not exist.
	nest := srcNest{lang: strings.ToLower(extOf(rel))}
	var scope cScope
	for i, line := range lines {
		n := i + 1
		inside := nest.inside()
		// Column 0 is the file and namespace scope. Inside a class or struct
		// the method is indented, and the brace scope is what admits it. The
		// same shape inside a function is a wrapped call, not a declaration.
		startDecl := false
		declSymbol := ""
		if decorDepth == 0 && !inside {
			if isDecl(line) {
				startDecl = true
				declSymbol = symbol(line)
			} else if cLikeExt(nest.lang) && scope.inClass() {
				trim := strings.TrimLeft(line, " \t")
				if isCDecl(trim) {
					startDecl = true
					declSymbol = cDeclSymbol(trim)
				}
			}
		}
		switch {
		case decorDepth == 0 && inside:
			if cur == nil {
				cur = &lineGroup{start: n}
			}
			cur.lines = append(cur.lines, line)
		case decorators && (decorDepth > 0 || strings.HasPrefix(strings.TrimLeft(line, " \t"), "@")):
			// An unclosed @dec( keeps its argument lines, including the text
			// inside the call. Flushing those lines onto the previous def
			// ranks that text under the wrong symbol.
			if decorAt == 0 {
				decorAt = n
			}
			decor = append(decor, line)
			decorDepth, decorQuote = scanCallDepth(line, decorDepth, decorQuote)
		case startDecl:
			// C preprocessor lines are not hash comments. Peeling #include
			// onto the next function indexes the directive under that symbol.
			doc, docAt := peelDoc(cur, !cLikeExt(nest.lang))
			if cur != nil && len(cur.lines) == 0 {
				cur = nil
			}
			flush()
			cur = &lineGroup{start: n, symbol: declSymbol, lines: []string{line}}
			if len(decor) > 0 {
				cur.lines = append(append([]string{}, decor...), cur.lines...)
				cur.start = decorAt
				clearDecor()
			}
			if len(doc) > 0 {
				cur.lines = append(append([]string{}, doc...), cur.lines...)
				if docAt < cur.start {
					cur.start = docAt
				}
			}
		default:
			// A comment between a decorator and the next def still belongs to
			// that def. Flushing it onto the previous group ranks the comment
			// under the wrong symbol.
			if len(decor) > 0 && isLineComment(line) {
				decor = append(decor, line)
				break
			}
			if len(decor) > 0 {
				if cur == nil {
					cur = &lineGroup{start: decorAt}
				}
				cur.lines = append(cur.lines, decor...)
				clearDecor()
			}
			if cur == nil {
				cur = &lineGroup{start: n}
			}
			cur.lines = append(cur.lines, line)
		}
		if cLikeExt(nest.lang) && !inside {
			scope.feed(line, startDecl)
		}
		nest.feed(line)
	}
	if len(decor) > 0 {
		if cur == nil {
			cur = &lineGroup{start: decorAt}
		}
		cur.lines = append(cur.lines, decor...)
	}
	flush()
	var out []rawChunk
	for _, g := range groups {
		if len(g.lines) == 0 {
			continue
		}
		out = append(out, explode(rel, g)...)
	}
	return out
}

// chunkPython keeps a stack of open blocks. A suite ends when a line dedents
// to the declaration that opened it, so a later assignment is not indexed
// under that function. A comment that sits above the next def stays with
// that def; a comment deeper than the suite stays on the suite.
func chunkPython(rel, src string, isDecl func(string) bool, symbol func(string) string) []rawChunk {
	lines := splitLines(src)
	var groups []lineGroup
	var stack []pyFrame
	var decor []string
	decorAt := 0
	decorDepth := 0
	var decorQuote quoteState
	var pending []string
	pendingAt := 0
	nest := srcNest{lang: ".py"}
	clearDecor := func() {
		decor = nil
		decorAt = 0
		decorDepth = 0
		decorQuote = quoteState{}
	}
	addDoc := func(doc []string, at int) {
		if len(doc) == 0 {
			return
		}
		if pendingAt == 0 || (at != 0 && at < pendingAt) {
			pendingAt = at
		}
		pending = append(pending, doc...)
	}
	takePending := func() ([]string, int) {
		doc, at := pending, pendingAt
		pending = nil
		pendingAt = 0
		return doc, at
	}
	flushFrame := func(f pyFrame) {
		if f.g == nil || len(f.g.lines) == 0 {
			return
		}
		groups = append(groups, *f.g)
	}
	// attach prepends a doc and a decorator onto a new declaration. The doc
	// is first because it appears earlier in the file.
	attach := func(g *lineGroup) {
		if len(decor) > 0 {
			g.lines = append(append([]string{}, decor...), g.lines...)
			if g.start == 0 || decorAt < g.start {
				g.start = decorAt
			}
			clearDecor()
		}
		doc, docAt := takePending()
		if len(doc) > 0 {
			g.lines = append(append([]string{}, doc...), g.lines...)
			if g.start == 0 || docAt < g.start {
				g.start = docAt
			}
		}
	}
	// placeTail appends a doc or a decorator that belongs after the lines a
	// block already has. Prepending would put them above the def or class.
	placeTail := func(g *lineGroup) {
		if len(pending) > 0 {
			doc, _ := takePending()
			g.lines = append(g.lines, doc...)
		}
		if len(decor) > 0 {
			g.lines = append(g.lines, decor...)
			clearDecor()
		}
	}
	closeFrames := func(indent int, asDecl, keepDoc bool) {
		for len(stack) > 0 && pyShouldClose(stack[len(stack)-1], indent, asDecl) {
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if keepDoc {
				doc, docAt := peelPyDoc(f.g, indent)
				addDoc(doc, docAt)
			}
			flushFrame(f)
		}
	}
	for i, line := range lines {
		n := i + 1
		indent, blank := lineIndent(line)
		inside := nest.inside()
		isAt := strings.HasPrefix(strings.TrimLeft(line, " \t"), "@")
		switch {
		case inside:
			appendPyLine(&stack, line, n, 0)
		case blank && decorDepth == 0:
			// A blank stops a doc from jumping across it onto the next def.
			if len(pending) > 0 {
				doc, at := takePending()
				if len(stack) == 0 {
					ind, _ := lineIndent(doc[0])
					stack = append(stack, pyFrame{g: &lineGroup{start: at, lines: doc}, indent: ind})
				} else {
					stack[len(stack)-1].g.lines = append(stack[len(stack)-1].g.lines, doc...)
				}
			}
			if len(stack) > 0 {
				appendPyLine(&stack, line, n, stack[len(stack)-1].indent)
			}
		case decorDepth > 0:
			decor = append(decor, line)
			decorDepth, decorQuote = scanCallDepth(line, decorDepth, decorQuote)
		case len(decor) > 0 && isLineComment(line):
			decor = append(decor, line)
		case isLineComment(line):
			closeFrames(indent, false, true)
			if len(stack) == 0 {
				addDoc([]string{line}, n)
			} else {
				top := stack[len(stack)-1].g
				placeTail(top)
				top.lines = append(top.lines, line)
			}
		case isAt:
			closeFrames(indent, true, true)
			if decorAt == 0 {
				decorAt = n
			}
			decor = append(decor, line)
			decorDepth, decorQuote = scanCallDepth(line, decorDepth, decorQuote)
		case isDecl(line):
			closeFrames(indent, true, true)
			if len(stack) > 0 {
				doc, docAt := peelPyDoc(stack[len(stack)-1].g, indent)
				addDoc(doc, docAt)
			}
			g := &lineGroup{start: n, symbol: symbol(line), lines: []string{line}}
			attach(g)
			stack = append(stack, pyFrame{g: g, indent: indent})
		default:
			closeFrames(indent, false, false)
			if len(stack) > 0 {
				top := stack[len(stack)-1].g
				placeTail(top)
				top.lines = append(top.lines, line)
			} else {
				g := &lineGroup{start: n, lines: []string{line}}
				attach(g)
				stack = append(stack, pyFrame{g: g, indent: indent})
			}
		}
		nest.feed(line)
	}
	if len(decor) > 0 || len(pending) > 0 {
		if len(stack) == 0 {
			start := decorAt
			if start == 0 || (pendingAt != 0 && pendingAt < start) {
				start = pendingAt
			}
			g := &lineGroup{start: start}
			attach(g)
			stack = append(stack, pyFrame{g: g, indent: 0})
		} else {
			placeTail(stack[len(stack)-1].g)
		}
	}
	for _, f := range stack {
		flushFrame(f)
	}
	var out []rawChunk
	for _, g := range groups {
		if len(g.lines) == 0 {
			continue
		}
		out = append(out, explode(rel, g)...)
	}
	return out
}

// pyShouldClose reports whether lineIndent ends the open frame. A nested
// suite stays open while the line is deeper. A declaration's suite also ends
// at the same indent. A symbol-less run of statements stays together until a
// declaration starts.
func pyShouldClose(f pyFrame, indent int, asDecl bool) bool {
	if f.g == nil || f.indent > indent {
		return true
	}
	if f.indent < indent {
		return false
	}
	if f.g.symbol != "" {
		return true
	}
	return asDecl
}

type pyFrame struct {
	g      *lineGroup
	indent int
}

func appendPyLine(stack *[]pyFrame, line string, n, indent int) {
	if len(*stack) == 0 {
		*stack = append(*stack, pyFrame{g: &lineGroup{start: n}, indent: indent})
	}
	top := (*stack)[len(*stack)-1].g
	top.lines = append(top.lines, line)
}

func lineIndent(line string) (int, bool) {
	if strings.TrimSpace(line) == "" {
		return 0, true
	}
	n := 0
	for _, r := range line {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 8
		default:
			return n, false
		}
	}
	return n, true
}

func peelPyDoc(g *lineGroup, maxIndent int) ([]string, int) {
	if g == nil || len(g.lines) == 0 {
		return nil, 0
	}
	i := len(g.lines)
	for i > 0 {
		line := g.lines[i-1]
		indent, blank := lineIndent(line)
		if blank || !isLineComment(line) || indent > maxIndent {
			break
		}
		i--
	}
	if i == len(g.lines) {
		return nil, 0
	}
	doc := append([]string{}, g.lines[i:]...)
	g.lines = g.lines[:i]
	return doc, g.start + i
}

// peelDoc moves the comment block that ends the current group onto the next
// declaration. A column-0 comment belongs to the following func, and leaving
// it on the previous one ranks that comment under the wrong symbol.
func peelDoc(cur *lineGroup, hash bool) ([]string, int) {
	if cur == nil || len(cur.lines) == 0 {
		return nil, 0
	}
	at := docStart(cur.lines, hash)
	if at >= len(cur.lines) {
		return nil, 0
	}
	doc := append([]string{}, cur.lines[at:]...)
	cur.lines = cur.lines[:at]
	return doc, cur.start + at
}

// docStart is the index of the comment suffix that belongs to the next
// declaration. A blank line or a code line stops the walk. A line comment
// may be indented, as a Python method doc is. A block comment may indent
// its closer and its star lines.
func docStart(lines []string, hash bool) int {
	i := len(lines)
	for i > 0 {
		line := lines[i-1]
		switch {
		case isDocLine(line, hash):
			i--
		case oneLineBlock(line):
			i--
		case blockCloser(line):
			open := blockOpen(lines[:i-1])
			if open < 0 {
				return i
			}
			i = open
		default:
			return i
		}
	}
	return i
}

func isLineComment(line string) bool {
	return isDocLine(line, true)
}

func isDocLine(line string, hash bool) bool {
	trim := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trim, "//") {
		return true
	}
	return hash && strings.HasPrefix(trim, "#")
}

func oneLineBlock(line string) bool {
	trim := strings.TrimSpace(line)
	if !strings.HasPrefix(trim, "/*") {
		return false
	}
	rest := trim[2:]
	k := strings.Index(rest, "*/")
	return k >= 0 && strings.TrimSpace(rest[k+2:]) == ""
}

func blockCloser(line string) bool {
	return strings.TrimSpace(line) == "*/"
}

func blockOpen(lines []string) int {
	for j := len(lines) - 1; j >= 0; j-- {
		// The body between /* and */ is arbitrary text, including blank
		// lines and star prefixes. The nearest opener is the one that owns
		// this closer.
		if strings.HasPrefix(strings.TrimSpace(lines[j]), "/*") && !oneLineBlock(lines[j]) {
			return j
		}
	}
	return -1
}

// quoteState is the Python string the decorator scanner is inside.
// Parens in that string do not close the @dec( call.
type quoteState struct {
	kind   byte
	triple bool
	raw    bool
	escape bool
}

func scanCallDepth(line string, depth int, st quoteState) (int, quoteState) {
	for i := 0; i < len(line); i++ {
		c := line[i]
		if st.kind != 0 {
			if st.escape && !st.raw {
				st.escape = false
				continue
			}
			if !st.raw && c == '\\' {
				st.escape = true
				continue
			}
			if st.triple {
				if i+2 < len(line) && line[i] == st.kind && line[i+1] == st.kind && line[i+2] == st.kind {
					st = quoteState{}
					i += 2
				}
				continue
			}
			if c == st.kind {
				st = quoteState{}
			}
			continue
		}
		switch c {
		case '#':
			return depth, st
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case '\'', '"':
			raw := rawStringPrefix(line, i)
			if i+2 < len(line) && line[i+1] == c && line[i+2] == c {
				st = quoteState{kind: c, triple: true, raw: raw}
				i += 2
				continue
			}
			st = quoteState{kind: c, raw: raw}
		}
	}
	if st.escape && !st.triple {
		st.escape = false
		return depth, st
	}
	if !st.triple {
		st = quoteState{}
	}
	return depth, st
}

func rawStringPrefix(line string, quote int) bool {
	j := quote
	raw := false
	for j > 0 {
		c := line[j-1]
		switch c {
		case 'r', 'R':
			raw = true
			j--
		case 'f', 'F', 'b', 'B', 'u', 'U':
			j--
		default:
			if j < quote && isIdentByte(c) {
				return false
			}
			return raw
		}
	}
	return raw
}

func isIdentByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

const (
	nestNone = iota
	nestBlock
	nestRaw
	nestString
	nestRune
)

// srcNest is the comment or string that is still open at a line boundary.
type srcNest struct {
	lang string
	mode int
	esc  bool
	py   quoteState
}

func (n *srcNest) inside() bool {
	switch n.lang {
	case ".go":
		return n.mode != nestNone
	case ".py", ".pyi":
		return n.py.kind != 0
	default:
		if cLikeExt(n.lang) {
			return n.mode != nestNone
		}
		return false
	}
}

func (n *srcNest) feed(line string) {
	switch {
	case n.lang == ".go":
		n.feedGo(line, true)
	case cLikeExt(n.lang):
		n.feedGo(line, false)
	case n.lang == ".py" || n.lang == ".pyi":
		_, n.py = scanCallDepth(line, 0, n.py)
	}
}

func (n *srcNest) feedGo(line string, raw bool) {
	i := 0
	switch n.mode {
	case nestRaw:
		k := strings.IndexByte(line, '`')
		if k < 0 {
			return
		}
		n.mode = nestNone
		i = k + 1
	case nestBlock:
		k := strings.Index(line, "*/")
		if k < 0 {
			return
		}
		n.mode = nestNone
		i = k + 2
	case nestString, nestRune:
		q := byte('"')
		if n.mode == nestRune {
			q = '\''
		}
		// The backslash that continued this literal was consumed by the newline.
		// It must not escape the first character of this line.
		closed, next, esc := consumeQuoted(line, 0, q, false)
		if !closed {
			if esc {
				n.esc = false
				return
			}
			n.mode = nestNone
			n.esc = false
			return
		}
		n.mode = nestNone
		n.esc = false
		i = next
	}
	for i < len(line) {
		c := line[i]
		if c == '/' && i+1 < len(line) && (line[i+1] == '/' || line[i+1] == '*') {
			if line[i+1] == '/' {
				return
			}
			k := strings.Index(line[i+2:], "*/")
			if k < 0 {
				n.mode = nestBlock
				return
			}
			i += 2 + k + 2
			continue
		}
		if raw && c == '`' {
			k := strings.IndexByte(line[i+1:], '`')
			if k < 0 {
				n.mode = nestRaw
				return
			}
			i += 1 + k + 1
			continue
		}
		if c == '"' || c == '\'' {
			closed, next, esc := consumeQuoted(line, i+1, c, false)
			if !closed {
				// A raw newline is not inside a Go string. Only a trailing
				// backslash continues the literal, and that backslash is the
				// escaped newline, not an escape of the next line's first byte.
				if !esc {
					return
				}
				if c == '"' {
					n.mode = nestString
				} else {
					n.mode = nestRune
				}
				n.esc = false
				return
			}
			i = next
			continue
		}
		i++
	}
}

func consumeQuoted(line string, i int, quote byte, esc bool) (closed bool, next int, escOut bool) {
	for i < len(line) {
		if esc {
			esc = false
			i++
			continue
		}
		if line[i] == '\\' {
			esc = true
			i++
			continue
		}
		if line[i] == quote {
			return true, i + 1, false
		}
		i++
	}
	return false, len(line), esc
}

func explode(rel string, g lineGroup) []rawChunk {
	if len(g.lines) == 0 {
		return nil
	}
	text := strings.Join(g.lines, "\n")
	if len(text) <= maxChunkBytes && len(g.lines) <= windowLines {
		return []rawChunk{{
			path:   rel,
			start:  g.start,
			end:    g.start + len(g.lines) - 1,
			symbol: g.symbol,
			text:   text,
		}}
	}
	var out []rawChunk
	for off := 0; off < len(g.lines); {
		end := off + windowLines
		if end > len(g.lines) {
			end = len(g.lines)
		}
		// A fat window used to keep the first 8192 runes and then stride past
		// the rest, so the lines in between were never indexed.
		for end > off+1 && snippetRunes(g.lines[off:end]) > maxChunkBytes {
			end--
		}
		if snippetRunes(g.lines[off:end]) > maxChunkBytes {
			out = append(out, splitLongLine(rel, g, off)...)
			off++
			continue
		}
		sym := ""
		if off == 0 {
			sym = g.symbol
		}
		out = append(out, rawChunk{
			path:   rel,
			start:  g.start + off,
			end:    g.start + end - 1,
			symbol: sym,
			text:   strings.Join(g.lines[off:end], "\n"),
		})
		if end == len(g.lines) {
			break
		}
		step := windowStride
		if step > end-off {
			step = end - off
		}
		if step < 1 {
			step = 1
		}
		off += step
	}
	return out
}

func splitLongLine(rel string, g lineGroup, off int) []rawChunk {
	runes := []rune(g.lines[off])
	if len(runes) == 0 {
		return nil
	}
	var out []rawChunk
	for i := 0; i < len(runes); {
		j := i + maxChunkBytes
		if j >= len(runes) {
			j = len(runes)
		} else {
			j = cutBeforeToken(runes, i, j)
		}
		sym := ""
		if off == 0 && i == 0 {
			sym = g.symbol
		}
		out = append(out, rawChunk{
			path:   rel,
			start:  g.start + off,
			end:    g.start + off,
			symbol: sym,
			text:   string(runes[i:j]),
		})
		if j >= len(runes) {
			break
		}
		if j <= i {
			j = i + maxChunkBytes
			if j > len(runes) {
				j = len(runes)
			}
		}
		i = j
	}
	return out
}

// cutBeforeToken keeps an identifier on one side of a long-line split.
// A cut through saveWidget indexes neither piece for that name.
func cutBeforeToken(runes []rune, start, end int) int {
	if end <= start || end >= len(runes) {
		return end
	}
	if !identCont(runes[end-1]) || !identCont(runes[end]) {
		return end
	}
	k := end
	for k > start && identCont(runes[k-1]) {
		k--
	}
	if k <= start {
		return end
	}
	return k
}

func splitLines(src string) []string {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.TrimSuffix(src, "\n")
	if src == "" {
		return nil
	}
	return strings.Split(src, "\n")
}

func snippetOf(text string) string {
	lines := splitLines(text)
	if len(lines) > maxSnippetLn {
		lines = lines[:maxSnippetLn]
	}
	return cutRunes(strings.Join(lines, "\n"), maxSnippetRun)
}

func cutRunes(s string, n int) string {
	if n <= 0 || s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
