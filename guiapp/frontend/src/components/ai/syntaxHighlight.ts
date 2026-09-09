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
    '.bat': 'batch',
    '.cmd': 'batch',
    '.ps1': 'powershell',
    '.psm1': 'powershell',
    '.psd1': 'powershell',
    '.rb': 'ruby',
    '.php': 'php',
    '.swift': 'swift',
    '.kt': 'kotlin',
    '.kts': 'kotlin',
    '.cs': 'csharp',
    '.sql': 'sql',
    '.r': 'r',
    '.lua': 'lua',
    '.toml': 'toml',
    '.xml': 'xml',
    '.xsl': 'xml',
    '.xsd': 'xml',
    '.svg': 'xml',
    '.tf': 'hcl',
    '.hcl': 'hcl',
    '.cmake': 'cmake',
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

/** Build rules for Windows Batch (.bat/.cmd) files. */
function buildBatchRules(): TokenRule[] {
    const batchKeywords = [
        'if', 'else', 'for', 'do', 'in', 'goto', 'call', 'exit', 'set', 'setlocal',
        'endlocal', 'echo', 'rem', 'pause', 'cls', 'title', 'color', 'pushd', 'popd',
        'not', 'exist', 'defined', 'equ', 'neq', 'lss', 'leq', 'gtr', 'geq',
        'errorlevel', 'cmdextversion', 'enabledelayedexpansion', 'off', 'on',
    ];
    return [
        // REM comment
        { pattern: /^(?:rem|REM)\b.*/, type: 'comment' },
        // :: comment
        { pattern: /^::.*/, type: 'comment' },
        // Labels
        { pattern: /^:\w+/, type: 'function' },
        // Double-quoted string
        { pattern: /^"(?:[^"\\]|\\.)*"/, type: 'string' },
        // Variables %var% and !var!
        { pattern: /^%~?[a-zA-Z0-9_*]+%/, type: 'type' },
        { pattern: /^![a-zA-Z0-9_]+!/, type: 'type' },
        // %1-%9 parameters
        { pattern: /^%~?[0-9]/, type: 'type' },
        // Numbers
        { pattern: /^\d+/, type: 'number' },
        // Operators
        { pattern: /^(?:>>|<<|&&|\|\||[|><&^])/, type: 'operator' },
        // @echo off pattern
        { pattern: /^@/, type: 'operator' },
        // Keywords (case-insensitive)
        { pattern: new RegExp(`^(?:${batchKeywords.join('|')})\\b`, 'i'), type: 'keyword' },
    ];
}

/** Build rules for PowerShell (.ps1/.psm1/.psd1) files. */
function buildPowerShellRules(): TokenRule[] {
    const psKeywords = [
        'if', 'else', 'elseif', 'switch', 'while', 'for', 'foreach', 'do', 'until',
        'break', 'continue', 'return', 'exit', 'throw', 'try', 'catch', 'finally',
        'trap', 'begin', 'process', 'end', 'function', 'filter', 'param', 'class',
        'enum', 'using', 'workflow', 'parallel', 'sequence', 'inlinescript',
    ];
    const psCmdlets = [
        'Write-Host', 'Write-Output', 'Write-Error', 'Write-Warning', 'Write-Verbose',
        'Get-Content', 'Set-Content', 'Get-Item', 'Set-Item', 'New-Item', 'Remove-Item',
        'Get-ChildItem', 'Test-Path', 'Invoke-Expression', 'Invoke-Command',
        'Start-Process', 'Stop-Process', 'Get-Process', 'Select-Object', 'Where-Object',
        'ForEach-Object', 'Sort-Object', 'Group-Object', 'Measure-Object',
    ];
    return [
        // Comment
        { pattern: /^#.*/, type: 'comment' },
        // Block comment <# ... #> (single-line portion)
        { pattern: /^<#.*?#>/, type: 'comment' },
        // Double-quoted string (with interpolation)
        { pattern: /^"(?:[^"\\`]|\\.|`[^])*"/, type: 'string' },
        // Single-quoted string
        { pattern: /^'[^']*'/, type: 'string' },
        // Here-string markers
        { pattern: /^@["']/, type: 'string' },
        // Variables
        { pattern: /^\$(?:\{[^}]+\}|\w+)/, type: 'type' },
        // Numbers
        { pattern: /^\d+(?:\.\d+)?/, type: 'number' },
        // Cmdlet-style names (Verb-Noun)
        { pattern: new RegExp(`^(?:${psCmdlets.join('|')})\\b`), type: 'function' },
        // General Verb-Noun cmdlet pattern
        { pattern: /^[A-Z][a-z]+-[A-Z]\w+/, type: 'function' },
        // Operators
        { pattern: /^(?:-eq|-ne|-lt|-gt|-le|-ge|-match|-notmatch|-like|-notlike|-contains|-in|-notin|-replace|-split|-join|-and|-or|-not|-band|-bor|-bnot|-shl|-shr|\||\+\+|--|[+\-*/%=!<>])/, type: 'operator' },
        // Type accelerators [type]
        { pattern: /^\[[a-zA-Z.]+\]/, type: 'type' },
        // Keywords
        { pattern: keywordRegex(psKeywords), type: 'keyword' },
    ];
}

/** Build rules for Ruby. */
function buildRubyRules(): TokenRule[] {
    const rubyKeywords = [
        'def', 'end', 'class', 'module', 'if', 'elsif', 'else', 'unless', 'while',
        'until', 'for', 'do', 'begin', 'rescue', 'ensure', 'raise', 'return', 'yield',
        'block_given?', 'self', 'super', 'nil', 'true', 'false', 'and', 'or', 'not',
        'then', 'when', 'case', 'require', 'require_relative', 'include', 'extend',
        'attr_accessor', 'attr_reader', 'attr_writer', 'puts', 'print', 'p',
    ];
    return [
        { pattern: /^#.*/, type: 'comment' },
        { pattern: /^"(?:[^"\\]|\\.)*"/, type: 'string' },
        { pattern: /^'(?:[^'\\]|\\.)*'/, type: 'string' },
        { pattern: /^\/(?:[^/\\]|\\.)*\/[gimxo]*/, type: 'string' },
        { pattern: /^:\w+/, type: 'string' }, // symbols
        { pattern: /^@{1,2}\w+/, type: 'type' }, // instance/class vars
        { pattern: /^\$\w+/, type: 'type' }, // global vars
        { pattern: /^\d+(?:\.\d+)?/, type: 'number' },
        { pattern: /^[A-Z]\w*/, type: 'type' }, // constants/class names
        { pattern: /^\w+[?!]?(?=\s*[({])/, type: 'function' },
        { pattern: /^(?:=>|->|<=>|&&|\|\||\.\.\.?|[+\-*/%=!<>&|^~])/, type: 'operator' },
        { pattern: keywordRegex(rubyKeywords), type: 'keyword' },
    ];
}

/** Build rules for PHP. */
function buildPHPRules(): TokenRule[] {
    const phpKeywords = [
        'if', 'else', 'elseif', 'while', 'for', 'foreach', 'do', 'switch', 'case',
        'break', 'continue', 'return', 'function', 'class', 'interface', 'trait',
        'extends', 'implements', 'new', 'public', 'private', 'protected', 'static',
        'abstract', 'final', 'const', 'var', 'use', 'namespace', 'require', 'include',
        'echo', 'print', 'throw', 'try', 'catch', 'finally', 'yield', 'match',
        'true', 'false', 'null', 'self', 'parent', 'fn',
    ];
    return [
        { pattern: /^\/\/.*/, type: 'comment' },
        { pattern: /^#.*/, type: 'comment' },
        { pattern: /^\/\*.*?\*\//, type: 'comment' },
        { pattern: /^"(?:[^"\\]|\\.)*"/, type: 'string' },
        { pattern: /^'(?:[^'\\]|\\.)*'/, type: 'string' },
        { pattern: /^\$\w+/, type: 'type' }, // variables
        { pattern: /^\d+(?:\.\d+)?/, type: 'number' },
        { pattern: /^[A-Z]\w*(?=\s*::|\s*\()/, type: 'type' },
        { pattern: /^\w+(?=\s*\()/, type: 'function' },
        { pattern: /^(?:=>|->|::|\?\?|\.\.\.|\.\.|[+\-*/%=!<>&|^~.])/, type: 'operator' },
        { pattern: keywordRegex(phpKeywords), type: 'keyword' },
    ];
}

/** Build rules for Swift. */
function buildSwiftRules(): TokenRule[] {
    const swiftKeywords = [
        'func', 'var', 'let', 'class', 'struct', 'enum', 'protocol', 'extension',
        'if', 'else', 'guard', 'switch', 'case', 'for', 'while', 'repeat', 'return',
        'break', 'continue', 'throw', 'throws', 'try', 'catch', 'defer', 'import',
        'self', 'Self', 'super', 'nil', 'true', 'false', 'init', 'deinit',
        'public', 'private', 'internal', 'fileprivate', 'open', 'static', 'override',
        'mutating', 'nonmutating', 'lazy', 'weak', 'unowned', 'async', 'await',
        'typealias', 'associatedtype', 'where', 'in', 'is', 'as',
    ];
    const swiftTypes = ['Int', 'String', 'Bool', 'Double', 'Float', 'Array', 'Dictionary', 'Set', 'Optional', 'Void', 'Any', 'AnyObject'];
    return buildCFamilyRules(swiftKeywords, swiftTypes);
}

/** Build rules for Kotlin. */
function buildKotlinRules(): TokenRule[] {
    const kotlinKeywords = [
        'fun', 'val', 'var', 'class', 'object', 'interface', 'enum', 'sealed',
        'if', 'else', 'when', 'for', 'while', 'do', 'return', 'break', 'continue',
        'throw', 'try', 'catch', 'finally', 'import', 'package', 'is', 'as', 'in',
        'null', 'true', 'false', 'this', 'super', 'override', 'open', 'abstract',
        'private', 'protected', 'internal', 'public', 'companion', 'data', 'inline',
        'suspend', 'lateinit', 'by', 'constructor', 'init', 'typealias',
    ];
    const kotlinTypes = ['Int', 'Long', 'Short', 'Byte', 'Float', 'Double', 'Boolean', 'Char', 'String', 'Unit', 'Any', 'Nothing', 'Array', 'List', 'Map', 'Set'];
    return buildCFamilyRules(kotlinKeywords, kotlinTypes);
}

/** Build rules for C#. */
function buildCSharpRules(): TokenRule[] {
    const csKeywords = [
        'if', 'else', 'switch', 'case', 'for', 'foreach', 'while', 'do', 'break',
        'continue', 'return', 'throw', 'try', 'catch', 'finally', 'using', 'namespace',
        'class', 'struct', 'interface', 'enum', 'delegate', 'event', 'new', 'this',
        'base', 'null', 'true', 'false', 'void', 'var', 'const', 'readonly', 'static',
        'public', 'private', 'protected', 'internal', 'abstract', 'virtual', 'override',
        'sealed', 'async', 'await', 'yield', 'ref', 'out', 'in', 'params', 'is', 'as',
        'typeof', 'sizeof', 'nameof', 'get', 'set', 'value', 'where', 'record',
    ];
    const csTypes = ['int', 'long', 'short', 'byte', 'float', 'double', 'decimal', 'bool', 'char', 'string', 'object', 'dynamic', 'Task', 'List', 'Dictionary', 'IEnumerable'];
    return buildCFamilyRules(csKeywords, csTypes);
}

/** Build rules for SQL. */
function buildSQLRules(): TokenRule[] {
    const sqlKeywords = [
        'SELECT', 'FROM', 'WHERE', 'AND', 'OR', 'NOT', 'IN', 'BETWEEN', 'LIKE',
        'INSERT', 'INTO', 'VALUES', 'UPDATE', 'SET', 'DELETE', 'CREATE', 'ALTER',
        'DROP', 'TABLE', 'INDEX', 'VIEW', 'DATABASE', 'SCHEMA', 'IF', 'EXISTS',
        'PRIMARY', 'KEY', 'FOREIGN', 'REFERENCES', 'UNIQUE', 'NULL', 'DEFAULT',
        'JOIN', 'LEFT', 'RIGHT', 'INNER', 'OUTER', 'FULL', 'CROSS', 'ON', 'AS',
        'ORDER', 'BY', 'GROUP', 'HAVING', 'LIMIT', 'OFFSET', 'UNION', 'ALL',
        'DISTINCT', 'COUNT', 'SUM', 'AVG', 'MIN', 'MAX', 'CASE', 'WHEN', 'THEN',
        'ELSE', 'END', 'BEGIN', 'COMMIT', 'ROLLBACK', 'TRANSACTION', 'GRANT', 'REVOKE',
    ];
    return [
        { pattern: /^--.*/, type: 'comment' },
        { pattern: /^\/\*.*?\*\//, type: 'comment' },
        { pattern: /^'(?:[^'\\]|\\.)*'/, type: 'string' },
        { pattern: /^"(?:[^"\\]|\\.)*"/, type: 'string' },
        { pattern: /^\d+(?:\.\d+)?/, type: 'number' },
        { pattern: /^(?:<=|>=|<>|!=|[+\-*/%=<>.,;()])/, type: 'operator' },
        { pattern: new RegExp(`^(?:${sqlKeywords.join('|')})\\b`, 'i'), type: 'keyword' },
        { pattern: /^[A-Z]\w*(?=\s*\()/, type: 'function' },
    ];
}

/** Build rules for Lua. */
function buildLuaRules(): TokenRule[] {
    const luaKeywords = [
        'and', 'break', 'do', 'else', 'elseif', 'end', 'false', 'for', 'function',
        'goto', 'if', 'in', 'local', 'nil', 'not', 'or', 'repeat', 'return', 'then',
        'true', 'until', 'while',
    ];
    return [
        { pattern: /^--\[\[.*/, type: 'comment' },
        { pattern: /^--.*/, type: 'comment' },
        { pattern: /^"(?:[^"\\]|\\.)*"/, type: 'string' },
        { pattern: /^'(?:[^'\\]|\\.)*'/, type: 'string' },
        { pattern: /^\[\[[\s\S]*?\]\]/, type: 'string' },
        { pattern: /^\d+(?:\.\d+)?(?:e[+-]?\d+)?/, type: 'number' },
        { pattern: /^\w+(?=\s*[({])/, type: 'function' },
        { pattern: /^(?:\.\.|\.\.\.|[+\-*/%^#=<>~])/, type: 'operator' },
        { pattern: keywordRegex(luaKeywords), type: 'keyword' },
    ];
}

/** Build rules for TOML. */
function buildTOMLRules(): TokenRule[] {
    return [
        { pattern: /^#.*/, type: 'comment' },
        { pattern: /^"""[\s\S]*?"""/, type: 'string' },
        { pattern: /^"(?:[^"\\]|\\.)*"/, type: 'string' },
        { pattern: /^'[^']*'/, type: 'string' },
        { pattern: /^\d{4}-\d{2}-\d{2}(?:T\d{2}:\d{2}:\d{2})?/, type: 'number' }, // datetime
        { pattern: /^[+-]?\d+(?:\.\d+)?(?:e[+-]?\d+)?/, type: 'number' },
        { pattern: /^(?:true|false)\b/, type: 'keyword' },
        { pattern: /^\[\[?[^\]]*\]\]?/, type: 'type' }, // table headers
        { pattern: /^\w[\w.-]*(?=\s*=)/, type: 'function' }, // keys
        { pattern: /^=/, type: 'operator' },
    ];
}

/** Build rules for XML/SVG. */
function buildXMLRules(): TokenRule[] {
    return [
        { pattern: /^<!--.*?-->/, type: 'comment' },
        { pattern: /^<!\[CDATA\[[\s\S]*?\]\]>/, type: 'string' },
        { pattern: /^<\/?[\w:.-]+/, type: 'keyword' }, // tag names
        { pattern: /^\/>|>/, type: 'keyword' },
        { pattern: /^"[^"]*"/, type: 'string' },
        { pattern: /^'[^']*'/, type: 'string' },
        { pattern: /^[\w:.-]+(?=\s*=)/, type: 'function' }, // attribute names
        { pattern: /^=/, type: 'operator' },
        { pattern: /^&\w+;/, type: 'number' }, // entities
    ];
}

/** Build rules for Dockerfile. */
function buildDockerfileRules(): TokenRule[] {
    const dockerKeywords = [
        'FROM', 'RUN', 'CMD', 'LABEL', 'MAINTAINER', 'EXPOSE', 'ENV', 'ADD', 'COPY',
        'ENTRYPOINT', 'VOLUME', 'USER', 'WORKDIR', 'ARG', 'ONBUILD', 'STOPSIGNAL',
        'HEALTHCHECK', 'SHELL', 'AS',
    ];
    return [
        { pattern: /^#.*/, type: 'comment' },
        { pattern: /^"(?:[^"\\]|\\.)*"/, type: 'string' },
        { pattern: /^'[^']*'/, type: 'string' },
        { pattern: /^\$\{?\w+\}?/, type: 'type' }, // variables
        { pattern: /^\d+/, type: 'number' },
        { pattern: new RegExp(`^(?:${dockerKeywords.join('|')})\\b`), type: 'keyword' },
        { pattern: /^(?:&&|\\|\|\|)/, type: 'operator' },
    ];
}

/** Build rules for Makefile. */
function buildMakefileRules(): TokenRule[] {
    return [
        { pattern: /^#.*/, type: 'comment' },
        { pattern: /^"(?:[^"\\]|\\.)*"/, type: 'string' },
        { pattern: /^'[^']*'/, type: 'string' },
        { pattern: /^\$[({]\w+[)}]/, type: 'type' }, // $(VAR) or ${VAR}
        { pattern: /^\$[@<^?*%]/, type: 'type' }, // automatic variables
        { pattern: /^[\w.-]+(?=\s*[:+?]?=)/, type: 'function' }, // variable assignments
        { pattern: /^[\w%./-]+(?=\s*:)/, type: 'keyword' }, // targets
        { pattern: /^\t/, type: 'plain' }, // recipe prefix
        { pattern: /^(?::=|\?=|\+=|::|[=:;|&\\])/, type: 'operator' },
        { pattern: keywordRegex(['ifeq', 'ifneq', 'ifdef', 'ifndef', 'else', 'endif', 'include', 'define', 'endef', 'override', 'export', 'unexport', 'vpath', '.PHONY', '.DEFAULT', '.PRECIOUS', '.SUFFIXES']), type: 'keyword' },
    ];
}

/** Build rules for HCL (Terraform). */
function buildHCLRules(): TokenRule[] {
    const hclKeywords = [
        'resource', 'data', 'variable', 'output', 'locals', 'module', 'provider',
        'terraform', 'backend', 'required_providers', 'required_version',
        'for_each', 'count', 'depends_on', 'lifecycle', 'dynamic', 'content',
        'true', 'false', 'null',
    ];
    return [
        { pattern: /^#.*/, type: 'comment' },
        { pattern: /^\/\/.*/, type: 'comment' },
        { pattern: /^\/\*.*?\*\//, type: 'comment' },
        { pattern: /^"(?:[^"\\]|\\.)*"/, type: 'string' },
        { pattern: /^\$\{[^}]*\}/, type: 'type' }, // interpolation
        { pattern: /^\d+(?:\.\d+)?/, type: 'number' },
        { pattern: /^\w+(?=\s*[({])/, type: 'function' },
        { pattern: /^(?:=>|[={}[\]])/, type: 'operator' },
        { pattern: keywordRegex(hclKeywords), type: 'keyword' },
    ];
}

/** Build rules for R. */
function buildRRules(): TokenRule[] {
    const rKeywords = [
        'if', 'else', 'for', 'while', 'repeat', 'in', 'next', 'break', 'function',
        'return', 'library', 'require', 'source', 'TRUE', 'FALSE', 'NULL', 'NA',
        'NA_integer_', 'NA_real_', 'NA_complex_', 'NA_character_', 'Inf', 'NaN',
    ];
    return [
        { pattern: /^#.*/, type: 'comment' },
        { pattern: /^"(?:[^"\\]|\\.)*"/, type: 'string' },
        { pattern: /^'(?:[^'\\]|\\.)*'/, type: 'string' },
        { pattern: /^\d+(?:\.\d+)?(?:e[+-]?\d+)?[Li]?/, type: 'number' },
        { pattern: /^\w+(?=\s*\()/, type: 'function' },
        { pattern: /^(?:<-|->|%%|%in%|%\*%|%>%|\|\||&&|[+\-*/%^~!<>=&|$@])/, type: 'operator' },
        { pattern: keywordRegex(rKeywords), type: 'keyword' },
    ];
}

/** Build rules for CMake (CMakeLists.txt / .cmake). */
function buildCMakeRules(): TokenRule[] {
    const cmakeKeywords = [
        'if', 'elseif', 'else', 'endif', 'foreach', 'endforeach', 'while', 'endwhile',
        'function', 'endfunction', 'macro', 'endmacro', 'return', 'break', 'continue',
        'set', 'unset', 'option', 'cmake_minimum_required', 'project', 'add_executable',
        'add_library', 'target_link_libraries', 'target_include_directories',
        'target_compile_definitions', 'target_compile_options', 'target_sources',
        'find_package', 'find_library', 'find_path', 'find_program', 'include',
        'include_directories', 'link_directories', 'add_subdirectory',
        'install', 'message', 'list', 'string', 'file', 'math', 'configure_file',
        'add_custom_command', 'add_custom_target', 'execute_process',
        'set_target_properties', 'get_target_property', 'add_definitions',
        'add_compile_options', 'cmake_policy', 'enable_testing', 'add_test',
    ];
    return [
        // Comment
        { pattern: /^#.*/, type: 'comment' },
        // Bracket comment #[[ ... ]]
        { pattern: /^#\[\[[\s\S]*?\]\]/, type: 'comment' },
        // Quoted string
        { pattern: /^"(?:[^"\\]|\\.)*"/, type: 'string' },
        // Variables ${VAR} and $ENV{VAR}
        { pattern: /^\$(?:ENV)?\{[^}]*\}/, type: 'type' },
        // Generator expressions $<...>
        { pattern: /^\$<[^>]*>/, type: 'type' },
        // Numbers
        { pattern: /^\d+(?:\.\d+)?/, type: 'number' },
        // Boolean/constants
        { pattern: /^(?:TRUE|FALSE|ON|OFF|YES|NO|NOTFOUND)\b/, type: 'number' },
        // Operators
        { pattern: /^(?:STREQUAL|STRLESS|STRGREATER|EQUAL|LESS|GREATER|AND|OR|NOT|MATCHES|VERSION_EQUAL|VERSION_LESS|VERSION_GREATER)\b/, type: 'operator' },
        // CMake commands (case-insensitive)
        { pattern: new RegExp(`^(?:${cmakeKeywords.join('|')})(?=\\s*\\()`, 'i'), type: 'keyword' },
        // Other function-like calls
        { pattern: /^[a-zA-Z_]\w*(?=\s*\()/, type: 'function' },
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
        case 'batch':
            rules = buildBatchRules();
            break;
        case 'powershell':
            rules = buildPowerShellRules();
            break;
        case 'ruby':
            rules = buildRubyRules();
            break;
        case 'php':
            rules = buildPHPRules();
            break;
        case 'swift':
            rules = buildSwiftRules();
            break;
        case 'kotlin':
            rules = buildKotlinRules();
            break;
        case 'csharp':
            rules = buildCSharpRules();
            break;
        case 'sql':
            rules = buildSQLRules();
            break;
        case 'lua':
            rules = buildLuaRules();
            break;
        case 'toml':
            rules = buildTOMLRules();
            break;
        case 'xml':
            rules = buildXMLRules();
            break;
        case 'dockerfile':
            rules = buildDockerfileRules();
            break;
        case 'makefile':
            rules = buildMakefileRules();
            break;
        case 'hcl':
            rules = buildHCLRules();
            break;
        case 'r':
            rules = buildRRules();
            break;
        case 'cmake':
            rules = buildCMakeRules();
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
