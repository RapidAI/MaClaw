/**
 * Lightweight syntax highlighting module — pure regex-based tokenization.
 *
 * Provides language detection from file extensions and line-level tokenization
 * for supported languages. No external dependencies (no Prism.js / highlight.js).
 *
 * Supported languages: Go, TypeScript, JavaScript, Python, Rust, Java,
 * C, C++, HTML, CSS, JSON, YAML, Shell/Bash, Markdown.
 */

/** A single token in a highlighted line. */
export interface HighlightToken {
    text: string;
    type:
        | 'keyword'
        | 'string'
        | 'comment'
        | 'number'
        | 'operator'
        | 'function'
        | 'type'
        | 'plain';
}

// ── Language Detection ──

/** Map of file extension (with dot) to language identifier. */
const extensionMap: Record<string, string> = {
    '.go': 'go',
    '.ts': 'typescript',
    '.tsx': 'typescript',
    '.js': 'javascript',
    '.jsx': 'javascript',
    '.py': 'python',
    '.rs': 'rust',
    '.java': 'java',
    '.c': 'c',
    '.h': 'c',
    '.cpp': 'cpp',
    '.cc': 'cpp',
    '.hpp': 'cpp',
    '.html': 'html',
    '.htm': 'html',
    '.css': 'css',
    '.json': 'json',
    '.yaml': 'yaml',
    '.yml': 'yaml',
    '.md': 'markdown',
    '.sh': 'shell',
    '.bash': 'shell',
};

/**
 * Detect language from a file name based on its extension.
 *
 * @param fileName - File name or path (e.g. "main.go", "src/index.ts")
 * @returns Language identifier string, or "plaintext" for unknown extensions
 */
export function detectLanguage(fileName: string): string {
    if (!fileName) return 'plaintext';

    // Extract extension: find the last dot in the basename
    const lastSlash = Math.max(fileName.lastIndexOf('/'), fileName.lastIndexOf('\\'));
    const basename = lastSlash >= 0 ? fileName.substring(lastSlash + 1) : fileName;
    const dotIndex = basename.lastIndexOf('.');

    if (dotIndex < 0) return 'plaintext';

    const ext = basename.substring(dotIndex).toLowerCase();
    return extensionMap[ext] ?? 'plaintext';
}

// ── Tokenization Rules ──

/** A single tokenization rule: regex pattern mapped to token type. */
interface TokenRule {
    pattern: RegExp;
    type: HighlightToken['type'];
}

/** Build a keyword regex that matches whole words from a list. */
function keywordRegex(keywords: string[]): RegExp {
    return new RegExp(`^\\b(${keywords.join('|')})\\b`);
}

// ── Language keyword sets ──

const goKeywords = [
    'break', 'case', 'chan', 'const', 'continue', 'default', 'defer',
    'else', 'fallthrough', 'for', 'func', 'go', 'goto', 'if', 'import',
    'interface', 'map', 'package', 'range', 'return', 'select', 'struct',
    'switch', 'type', 'var',
];

const goTypes = [
    'bool', 'byte', 'complex64', 'complex128', 'error', 'float32',
    'float64', 'int', 'int8', 'int16', 'int32', 'int64', 'rune',
    'string', 'uint', 'uint8', 'uint16', 'uint32', 'uint64', 'uintptr',
    'nil', 'true', 'false', 'iota',
];

const tsKeywords = [
    'abstract', 'as', 'async', 'await', 'break', 'case', 'catch', 'class',
    'const', 'continue', 'debugger', 'declare', 'default', 'delete', 'do',
    'else', 'enum', 'export', 'extends', 'finally', 'for', 'from',
    'function', 'if', 'implements', 'import', 'in', 'instanceof',
    'interface', 'let', 'module', 'namespace', 'new', 'of', 'package',
    'private', 'protected', 'public', 'readonly', 'return', 'static',
    'super', 'switch', 'this', 'throw', 'try', 'type', 'typeof', 'var',
    'void', 'while', 'with', 'yield',
];

const tsTypes = [
    'any', 'boolean', 'never', 'null', 'number', 'object', 'string',
    'symbol', 'undefined', 'unknown', 'bigint', 'true', 'false',
];

const jsKeywords = [
    'async', 'await', 'break', 'case', 'catch', 'class', 'const',
    'continue', 'debugger', 'default', 'delete', 'do', 'else', 'export',
    'extends', 'finally', 'for', 'from', 'function', 'if', 'import',
    'in', 'instanceof', 'let', 'new', 'of', 'return', 'static', 'super',
    'switch', 'this', 'throw', 'try', 'typeof', 'var', 'void', 'while',
    'with', 'yield',
];

const jsTypes = [
    'null', 'undefined', 'true', 'false', 'NaN', 'Infinity',
];

const pythonKeywords = [
    'and', 'as', 'assert', 'async', 'await', 'break', 'class',
    'continue', 'def', 'del', 'elif', 'else', 'except', 'finally',
    'for', 'from', 'global', 'if', 'import', 'in', 'is', 'lambda',
    'nonlocal', 'not', 'or', 'pass', 'raise', 'return', 'try', 'while',
    'with', 'yield',
];

const pythonTypes = [
    'True', 'False', 'None', 'int', 'float', 'str', 'bool', 'list',
    'dict', 'tuple', 'set', 'bytes', 'type', 'object',
];

const rustKeywords = [
    'as', 'async', 'await', 'break', 'const', 'continue', 'crate',
    'dyn', 'else', 'enum', 'extern', 'fn', 'for', 'if', 'impl', 'in',
    'let', 'loop', 'match', 'mod', 'move', 'mut', 'pub', 'ref',
    'return', 'self', 'static', 'struct', 'super', 'trait', 'type',
    'unsafe', 'use', 'where', 'while',
];

const rustTypes = [
    'bool', 'char', 'f32', 'f64', 'i8', 'i16', 'i32', 'i64', 'i128',
    'isize', 'str', 'u8', 'u16', 'u32', 'u64', 'u128', 'usize',
    'Self', 'true', 'false', 'None', 'Some', 'Ok', 'Err',
    'String', 'Vec', 'Box', 'Option', 'Result',
];

const javaKeywords = [
    'abstract', 'assert', 'break', 'case', 'catch', 'class', 'const',
    'continue', 'default', 'do', 'else', 'enum', 'extends', 'final',
    'finally', 'for', 'goto', 'if', 'implements', 'import', 'instanceof',
    'interface', 'native', 'new', 'package', 'private', 'protected',
    'public', 'return', 'static', 'strictfp', 'super', 'switch',
    'synchronized', 'this', 'throw', 'throws', 'transient', 'try',
    'void', 'volatile', 'while',
];

const javaTypes = [
    'boolean', 'byte', 'char', 'double', 'float', 'int', 'long',
    'short', 'null', 'true', 'false', 'String', 'Integer', 'Boolean',
    'Long', 'Double', 'Float', 'Object', 'List', 'Map', 'Set',
];

const cKeywords = [
    'auto', 'break', 'case', 'const', 'continue', 'default', 'do',
    'else', 'enum', 'extern', 'for', 'goto', 'if', 'inline', 'register',
    'restrict', 'return', 'sizeof', 'static', 'struct', 'switch',
    'typedef', 'union', 'volatile', 'while',
];

const cTypes = [
    'char', 'double', 'float', 'int', 'long', 'short', 'signed',
    'unsigned', 'void', 'NULL', 'size_t', 'bool', 'true', 'false',
];

const cppKeywords = [
    ...cKeywords,
    'catch', 'class', 'constexpr', 'delete', 'dynamic_cast', 'explicit',
    'export', 'friend', 'mutable', 'namespace', 'new', 'noexcept',
    'operator', 'override', 'private', 'protected', 'public',
    'reinterpret_cast', 'static_assert', 'static_cast', 'template',
    'this', 'throw', 'try', 'typeid', 'typename', 'using', 'virtual',
];

const cppTypes = [
    ...cTypes,
    'nullptr', 'string', 'wchar_t', 'char16_t', 'char32_t',
    'auto', 'decltype',
];

const shellKeywords = [
    'if', 'then', 'else', 'elif', 'fi', 'for', 'while', 'do', 'done',
    'case', 'esac', 'in', 'function', 'return', 'exit', 'local',
    'export', 'source', 'alias', 'unalias', 'set', 'unset', 'shift',
    'break', 'continue', 'eval', 'exec', 'trap', 'readonly',
    'declare', 'typeset', 'select', 'until',
];

// ── Rule builders ──

/** Common rules for C-family languages (// comments, strings, numbers, operators). */
function cFamilyCommonRules(): TokenRule[] {
    return [
        // Single-line comment
        { pattern: /^\/\/.*/, type: 'comment' },
        // Multi-line comment opening (we only handle single-line view)
        { pattern: /^\/\*.*?\*\//, type: 'comment' },
        // Double-quoted string
        { pattern: /^"(?:[^"\\]|\\.)*"/, type: 'string' },
        // Single-quoted string (char literal in C-family)
        { pattern: /^'(?:[^'\\]|\\.)*'/, type: 'string' },
        // Backtick string (template literal)
        { pattern: /^`(?:[^`\\]|\\.)*`/, type: 'string' },
        // Numbers (hex, float, int)
        { pattern: /^0[xX][0-9a-fA-F]+(?:_[0-9a-fA-F]+)*/, type: 'number' },
        { pattern: /^\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/, type: 'number' },
        // Function call (identifier followed by open paren)
        { pattern: /^[a-zA-Z_]\w*(?=\s*\()/, type: 'function' },
        // Operators
        { pattern: /^(?:=>|===|!==|==|!=|<=|>=|&&|\|\||<<|>>|\+\+|--|[+\-*/%&|^~<>!=])/, type: 'operator' },
    ];
}

/** Build rules for a language with C-family syntax. */
function buildCFamilyRules(keywords: string[], types: string[]): TokenRule[] {
    return [
        ...cFamilyCommonRules(),
        { pattern: keywordRegex(keywords), type: 'keyword' },
        { pattern: keywordRegex(types), type: 'type' },
    ];
}

/** Build rules for Python. */
function buildPythonRules(): TokenRule[] {
    return [
        // Comment
        { pattern: /^#.*/, type: 'comment' },
        // Triple-quoted strings
        { pattern: /^"""[\s\S]*?"""/, type: 'string' },
        { pattern: /^'''[\s\S]*?'''/, type: 'string' },
        // Strings
        { pattern: /^f?"(?:[^"\\]|\\.)*"/, type: 'string' },
        { pattern: /^f?'(?:[^'\\]|\\.)*'/, type: 'string' },
        // Numbers
        { pattern: /^0[xX][0-9a-fA-F]+(?:_[0-9a-fA-F]+)*/, type: 'number' },
        { pattern: /^\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/, type: 'number' },
        // Decorator
        { pattern: /^@\w+/, type: 'keyword' },
        // Function call
        { pattern: /^[a-zA-Z_]\w*(?=\s*\()/, type: 'function' },
        // Operators
        { pattern: /^(?:==|!=|<=|>=|<<|>>|\*\*|\/\/|->|[+\-*/%&|^~<>!=])/, type: 'operator' },
        // Keywords and types
        { pattern: keywordRegex(pythonKeywords), type: 'keyword' },
        { pattern: keywordRegex(pythonTypes), type: 'type' },
    ];
}

/** Build rules for Shell/Bash. */
function buildShellRules(): TokenRule[] {
    return [
        // Comment
        { pattern: /^#.*/, type: 'comment' },
        // Double-quoted string
        { pattern: /^"(?:[^"\\]|\\.)*"/, type: 'string' },
        // Single-quoted string (no escapes in bash single quotes)
        { pattern: /^'[^']*'/, type: 'string' },
        // Numbers
        { pattern: /^\d+/, type: 'number' },
        // Variable
        { pattern: /^\$\{?\w+\}?/, type: 'type' },
        // Function call
        { pattern: /^[a-zA-Z_]\w*(?=\s*\()/, type: 'function' },
        // Operators
        { pattern: /^(?:\|\||&&|;;|[|&;><])/, type: 'operator' },
        // Keywords
        { pattern: keywordRegex(shellKeywords), type: 'keyword' },
    ];
}

/** Build rules for HTML. */
function buildHTMLRules(): TokenRule[] {
    return [
        // Comment
        { pattern: /^<!--[\s\S]*?-->/, type: 'comment' },
        // Tag name (opening/closing)
        { pattern: /^<\/?[a-zA-Z][\w-]*/, type: 'keyword' },
        // Closing bracket
        { pattern: /^\/?>/, type: 'keyword' },
        // Attribute value
        { pattern: /^"(?:[^"\\]|\\.)*"/, type: 'string' },
        { pattern: /^'(?:[^'\\]|\\.)*'/, type: 'string' },
        // Attribute name
        { pattern: /^[a-zA-Z][\w-]*(?=\s*=)/, type: 'type' },
        // Entities
        { pattern: /^&\w+;/, type: 'number' },
        // Operators
        { pattern: /^=/, type: 'operator' },
    ];
}

/** Build rules for CSS. */
function buildCSSRules(): TokenRule[] {
    return [
        // Comment
        { pattern: /^\/\*[\s\S]*?\*\//, type: 'comment' },
        // String
        { pattern: /^"(?:[^"\\]|\\.)*"/, type: 'string' },
        { pattern: /^'(?:[^'\\]|\\.)*'/, type: 'string' },
        // Numbers with units
        { pattern: /^\d+(?:\.\d+)?(?:px|em|rem|%|vh|vw|s|ms|deg|fr)?/, type: 'number' },
        // Color hex
        { pattern: /^#[0-9a-fA-F]{3,8}/, type: 'number' },
        // Property name (before colon)
        { pattern: /^[a-zA-Z-]+(?=\s*:)/, type: 'type' },
        // Selector class/id
        { pattern: /^[.#][a-zA-Z][\w-]*/, type: 'keyword' },
        // At-rules
        { pattern: /^@[a-zA-Z][\w-]*/, type: 'keyword' },
        // Pseudo-selectors
        { pattern: /^:{1,2}[a-zA-Z][\w-]*/, type: 'keyword' },
        // Function call (e.g. rgb(), calc())
        { pattern: /^[a-zA-Z][\w-]*(?=\()/, type: 'function' },
        // Operators
        { pattern: /^[{}:;,>+~]/, type: 'operator' },
    ];
}

/** Build rules for JSON. */
function buildJSONRules(): TokenRule[] {
    return [
        // String (key or value)
        { pattern: /^"(?:[^"\\]|\\.)*"(?=\s*:)/, type: 'type' },
        { pattern: /^"(?:[^"\\]|\\.)*"/, type: 'string' },
        // Numbers
        { pattern: /^-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/, type: 'number' },
        // Boolean / null
        { pattern: /^(?:true|false|null)\b/, type: 'keyword' },
        // Structural
        { pattern: /^[{}[\]:,]/, type: 'operator' },
    ];
}

/** Build rules for YAML. */
function buildYAMLRules(): TokenRule[] {
    return [
        // Comment
        { pattern: /^#.*/, type: 'comment' },
        // Key (word followed by colon)
        { pattern: /^[\w][\w.-]*(?=\s*:)/, type: 'type' },
        // String
        { pattern: /^"(?:[^"\\]|\\.)*"/, type: 'string' },
        { pattern: /^'(?:[^'\\]|\\.)*'/, type: 'string' },
        // Boolean / null
        { pattern: /^(?:true|false|yes|no|null|~)\b/, type: 'keyword' },
        // Numbers
        { pattern: /^-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/, type: 'number' },
        // Anchors and aliases
        { pattern: /^[&*]\w+/, type: 'keyword' },
        // Operators
        { pattern: /^[:\-|>]/, type: 'operator' },
    ];
}

// ── Rule cache ──

const rulesCache = new Map<string, TokenRule[]>();

/** Get tokenization rules for a language. Returns undefined for unsupported. */
function getRules(language: string): TokenRule[] | undefined {
    if (rulesCache.has(language)) return rulesCache.get(language);

    let rules: TokenRule[] | undefined;

    switch (language) {
        case 'go':
            rules = buildCFamilyRules(goKeywords, goTypes);
            break;
        case 'typescript':
            rules = buildCFamilyRules(tsKeywords, tsTypes);
            break;
        case 'javascript':
            rules = buildCFamilyRules(jsKeywords, jsTypes);
            break;
        case 'python':
            rules = buildPythonRules();
            break;
        case 'rust':
            rules = buildCFamilyRules(rustKeywords, rustTypes);
            break;
        case 'java':
            rules = buildCFamilyRules(javaKeywords, javaTypes);
            break;
        case 'c':
            rules = buildCFamilyRules(cKeywords, cTypes);
            break;
        case 'cpp':
            rules = buildCFamilyRules(cppKeywords, cppTypes);
            break;
        case 'html':
            rules = buildHTMLRules();
            break;
        case 'css':
            rules = buildCSSRules();
            break;
        case 'json':
            rules = buildJSONRules();
            break;
        case 'yaml':
            rules = buildYAMLRules();
            break;
        case 'shell':
            rules = buildShellRules();
            break;
        default:
            return undefined;
    }

    rulesCache.set(language, rules);
    return rules;
}

// ── Tokenizer ──

/**
 * Tokenize a single line of code based on language.
 *
 * Processes the line left-to-right, matching the first applicable rule
 * at each position. Unmatched text becomes 'plain' tokens.
 *
 * For unsupported languages (including "plaintext" and "markdown"),
 * returns the entire line as a single 'plain' token.
 *
 * @param line - A single line of code (no newline characters)
 * @param language - Language identifier (from detectLanguage)
 * @returns Array of HighlightToken covering the entire line
 */
export function tokenizeLine(line: string, language: string): HighlightToken[] {
    if (line === '') return [];

    const rules = getRules(language);
    if (!rules) {
        // Unsupported language — return entire line as plain
        return [{ text: line, type: 'plain' }];
    }

    const tokens: HighlightToken[] = [];
    let pos = 0;
    let plainStart = pos;

    while (pos < line.length) {
        // Skip whitespace — accumulate into plain
        if (line[pos] === ' ' || line[pos] === '\t') {
            pos++;
            continue;
        }

        const remaining = line.substring(pos);
        let matched = false;

        for (const rule of rules) {
            const m = remaining.match(rule.pattern);
            if (m && m[0].length > 0) {
                // Flush accumulated plain text before this match
                if (plainStart < pos) {
                    tokens.push({ text: line.substring(plainStart, pos), type: 'plain' });
                }

                tokens.push({ text: m[0], type: rule.type });
                pos += m[0].length;
                plainStart = pos;
                matched = true;
                break;
            }
        }

        if (!matched) {
            // No rule matched — advance one character (will be part of plain text)
            pos++;
        }
    }

    // Flush remaining plain text
    if (plainStart < pos) {
        tokens.push({ text: line.substring(plainStart, pos), type: 'plain' });
    }

    return tokens;
}
