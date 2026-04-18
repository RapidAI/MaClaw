/**
 * Unit tests for syntaxHighlight module.
 *
 * Tests tokenization of each supported language and detectLanguage edge cases.
 */
import { describe, it, expect } from 'vitest';
import { detectLanguage, tokenizeLine, type HighlightToken } from './syntaxHighlight';

// ── Helper ──

/** Find the first token of a given type in the result. */
function findToken(tokens: HighlightToken[], type: HighlightToken['type']): HighlightToken | undefined {
    return tokens.find(t => t.type === type);
}

/** Check that concatenating all token texts reproduces the original line. */
function assertFullCoverage(tokens: HighlightToken[], line: string) {
    const reconstructed = tokens.map(t => t.text).join('');
    expect(reconstructed).toBe(line);
}

// ── detectLanguage ──

describe('detectLanguage', () => {
    it('maps known extensions correctly', () => {
        expect(detectLanguage('main.go')).toBe('go');
        expect(detectLanguage('index.ts')).toBe('typescript');
        expect(detectLanguage('App.tsx')).toBe('typescript');
        expect(detectLanguage('app.js')).toBe('javascript');
        expect(detectLanguage('Component.jsx')).toBe('javascript');
        expect(detectLanguage('script.py')).toBe('python');
        expect(detectLanguage('lib.rs')).toBe('rust');
        expect(detectLanguage('Main.java')).toBe('java');
        expect(detectLanguage('util.c')).toBe('c');
        expect(detectLanguage('header.h')).toBe('c');
        expect(detectLanguage('main.cpp')).toBe('cpp');
        expect(detectLanguage('util.cc')).toBe('cpp');
        expect(detectLanguage('types.hpp')).toBe('cpp');
        expect(detectLanguage('index.html')).toBe('html');
        expect(detectLanguage('page.htm')).toBe('html');
        expect(detectLanguage('style.css')).toBe('css');
        expect(detectLanguage('data.json')).toBe('json');
        expect(detectLanguage('config.yaml')).toBe('yaml');
        expect(detectLanguage('config.yml')).toBe('yaml');
        expect(detectLanguage('README.md')).toBe('markdown');
        expect(detectLanguage('deploy.sh')).toBe('shell');
        expect(detectLanguage('init.bash')).toBe('shell');
    });

    it('returns plaintext for unknown extensions', () => {
        expect(detectLanguage('file.xyz')).toBe('plaintext');
        expect(detectLanguage('data.csv')).toBe('plaintext');
        expect(detectLanguage('image.png')).toBe('plaintext');
    });

    it('returns plaintext for no extension', () => {
        expect(detectLanguage('Makefile')).toBe('plaintext');
        expect(detectLanguage('Dockerfile')).toBe('plaintext');
    });

    it('returns plaintext for empty string', () => {
        expect(detectLanguage('')).toBe('plaintext');
    });

    it('handles paths with directories', () => {
        expect(detectLanguage('src/components/App.tsx')).toBe('typescript');
        expect(detectLanguage('C:\\Users\\dev\\main.go')).toBe('go');
        expect(detectLanguage('/home/user/project/lib.rs')).toBe('rust');
    });

    it('is case-insensitive for extensions', () => {
        expect(detectLanguage('FILE.GO')).toBe('go');
        expect(detectLanguage('INDEX.TS')).toBe('typescript');
        expect(detectLanguage('STYLE.CSS')).toBe('css');
    });
});

// ── tokenizeLine — general behavior ──

describe('tokenizeLine — general', () => {
    it('returns empty array for empty line', () => {
        expect(tokenizeLine('', 'go')).toEqual([]);
    });

    it('returns single plain token for unsupported language', () => {
        const tokens = tokenizeLine('some code here', 'plaintext');
        expect(tokens).toEqual([{ text: 'some code here', type: 'plain' }]);
    });

    it('returns single plain token for markdown', () => {
        const tokens = tokenizeLine('# Hello World', 'markdown');
        expect(tokens).toEqual([{ text: '# Hello World', type: 'plain' }]);
    });

    it('token texts concatenate to original line', () => {
        const line = 'func main() { fmt.Println("hello") }';
        const tokens = tokenizeLine(line, 'go');
        assertFullCoverage(tokens, line);
    });
});

// ── tokenizeLine — Go ──

describe('tokenizeLine — Go', () => {
    it('highlights keywords', () => {
        const tokens = tokenizeLine('func main() {', 'go');
        expect(findToken(tokens, 'keyword')?.text).toBe('func');
    });

    it('highlights types', () => {
        const tokens = tokenizeLine('var x int = 42', 'go');
        expect(tokens.some(t => t.type === 'type' && t.text === 'int')).toBe(true);
    });

    it('highlights strings', () => {
        const tokens = tokenizeLine('s := "hello world"', 'go');
        expect(findToken(tokens, 'string')?.text).toBe('"hello world"');
    });

    it('highlights comments', () => {
        const tokens = tokenizeLine('// this is a comment', 'go');
        expect(tokens).toHaveLength(1);
        expect(tokens[0]).toEqual({ text: '// this is a comment', type: 'comment' });
    });

    it('highlights numbers', () => {
        const tokens = tokenizeLine('x := 42', 'go');
        expect(findToken(tokens, 'number')?.text).toBe('42');
    });

    it('highlights function calls', () => {
        const tokens = tokenizeLine('fmt.Println("hi")', 'go');
        expect(tokens.some(t => t.type === 'function' && t.text === 'Println')).toBe(true);
    });

    it('full coverage for complex line', () => {
        const line = 'if err != nil { return err }';
        const tokens = tokenizeLine(line, 'go');
        assertFullCoverage(tokens, line);
    });
});

// ── tokenizeLine — TypeScript ──

describe('tokenizeLine — TypeScript', () => {
    it('highlights keywords', () => {
        const tokens = tokenizeLine('const x: number = 5;', 'typescript');
        expect(findToken(tokens, 'keyword')?.text).toBe('const');
    });

    it('highlights types', () => {
        const tokens = tokenizeLine('let val: boolean = true;', 'typescript');
        expect(tokens.some(t => t.type === 'type' && t.text === 'boolean')).toBe(true);
    });

    it('highlights template literals', () => {
        const tokens = tokenizeLine('const s = `hello`;', 'typescript');
        expect(findToken(tokens, 'string')?.text).toBe('`hello`');
    });

    it('highlights arrow functions', () => {
        const tokens = tokenizeLine('const fn = () => {};', 'typescript');
        expect(tokens.some(t => t.type === 'operator' && t.text === '=>')).toBe(true);
    });

    it('full coverage', () => {
        const line = 'export interface Foo { bar: string; }';
        const tokens = tokenizeLine(line, 'typescript');
        assertFullCoverage(tokens, line);
    });
});

// ── tokenizeLine — JavaScript ──

describe('tokenizeLine — JavaScript', () => {
    it('highlights keywords', () => {
        const tokens = tokenizeLine('function hello() {}', 'javascript');
        expect(findToken(tokens, 'keyword')?.text).toBe('function');
    });

    it('highlights strings', () => {
        const tokens = tokenizeLine("const s = 'world';", 'javascript');
        expect(findToken(tokens, 'string')?.text).toBe("'world'");
    });

    it('highlights comments', () => {
        const tokens = tokenizeLine('// TODO: fix this', 'javascript');
        expect(tokens[0].type).toBe('comment');
    });

    it('full coverage', () => {
        const line = 'const arr = [1, 2, 3];';
        const tokens = tokenizeLine(line, 'javascript');
        assertFullCoverage(tokens, line);
    });
});

// ── tokenizeLine — Python ──

describe('tokenizeLine — Python', () => {
    it('highlights keywords', () => {
        const tokens = tokenizeLine('def hello():', 'python');
        expect(findToken(tokens, 'keyword')?.text).toBe('def');
    });

    it('highlights comments', () => {
        const tokens = tokenizeLine('# this is a comment', 'python');
        expect(tokens).toHaveLength(1);
        expect(tokens[0].type).toBe('comment');
    });

    it('highlights strings', () => {
        const tokens = tokenizeLine('x = "hello"', 'python');
        expect(findToken(tokens, 'string')?.text).toBe('"hello"');
    });

    it('highlights decorators', () => {
        const tokens = tokenizeLine('@staticmethod', 'python');
        expect(findToken(tokens, 'keyword')?.text).toBe('@staticmethod');
    });

    it('highlights types', () => {
        const tokens = tokenizeLine('x = None', 'python');
        expect(tokens.some(t => t.type === 'type' && t.text === 'None')).toBe(true);
    });

    it('highlights f-strings', () => {
        const tokens = tokenizeLine('s = f"hello {name}"', 'python');
        expect(findToken(tokens, 'string')?.text).toBe('f"hello {name}"');
    });

    it('full coverage', () => {
        const line = 'for i in range(10):';
        const tokens = tokenizeLine(line, 'python');
        assertFullCoverage(tokens, line);
    });
});

// ── tokenizeLine — Rust ──

describe('tokenizeLine — Rust', () => {
    it('highlights keywords', () => {
        const tokens = tokenizeLine('fn main() {', 'rust');
        expect(findToken(tokens, 'keyword')?.text).toBe('fn');
    });

    it('highlights types', () => {
        const tokens = tokenizeLine('let x: i32 = 5;', 'rust');
        expect(tokens.some(t => t.type === 'type' && t.text === 'i32')).toBe(true);
    });

    it('highlights strings', () => {
        const tokens = tokenizeLine('let s = "hello";', 'rust');
        expect(findToken(tokens, 'string')?.text).toBe('"hello"');
    });

    it('highlights comments', () => {
        const tokens = tokenizeLine('// rust comment', 'rust');
        expect(tokens[0].type).toBe('comment');
    });

    it('full coverage', () => {
        const line = 'let mut v: Vec<i32> = Vec::new();';
        const tokens = tokenizeLine(line, 'rust');
        assertFullCoverage(tokens, line);
    });
});

// ── tokenizeLine — Java ──

describe('tokenizeLine — Java', () => {
    it('highlights keywords', () => {
        const tokens = tokenizeLine('public class Main {', 'java');
        expect(findToken(tokens, 'keyword')?.text).toBe('public');
    });

    it('highlights types', () => {
        const tokens = tokenizeLine('String name = "test";', 'java');
        expect(tokens.some(t => t.type === 'type' && t.text === 'String')).toBe(true);
    });

    it('highlights strings', () => {
        const tokens = tokenizeLine('String s = "hello";', 'java');
        expect(findToken(tokens, 'string')?.text).toBe('"hello"');
    });

    it('full coverage', () => {
        const line = 'System.out.println("Hello World");';
        const tokens = tokenizeLine(line, 'java');
        assertFullCoverage(tokens, line);
    });
});

// ── tokenizeLine — C ──

describe('tokenizeLine — C', () => {
    it('highlights keywords', () => {
        const tokens = tokenizeLine('return 0;', 'c');
        expect(findToken(tokens, 'keyword')?.text).toBe('return');
    });

    it('highlights types', () => {
        const tokens = tokenizeLine('int main(void) {', 'c');
        expect(tokens.some(t => t.type === 'type' && t.text === 'int')).toBe(true);
    });

    it('highlights strings', () => {
        const tokens = tokenizeLine('char *s = "hello";', 'c');
        expect(findToken(tokens, 'string')?.text).toBe('"hello"');
    });

    it('highlights comments', () => {
        const tokens = tokenizeLine('/* block comment */', 'c');
        expect(tokens[0].type).toBe('comment');
    });

    it('full coverage', () => {
        const line = '#include <stdio.h>';
        const tokens = tokenizeLine(line, 'c');
        assertFullCoverage(tokens, line);
    });
});

// ── tokenizeLine — C++ ──

describe('tokenizeLine — C++', () => {
    it('highlights C++ specific keywords', () => {
        const tokens = tokenizeLine('class Foo : public Bar {', 'cpp');
        expect(findToken(tokens, 'keyword')?.text).toBe('class');
    });

    it('highlights C++ types', () => {
        const tokens = tokenizeLine('nullptr', 'cpp');
        expect(tokens.some(t => t.type === 'type' && t.text === 'nullptr')).toBe(true);
    });

    it('highlights namespace', () => {
        const tokens = tokenizeLine('namespace std {', 'cpp');
        expect(findToken(tokens, 'keyword')?.text).toBe('namespace');
    });

    it('full coverage', () => {
        const line = 'std::cout << "Hello" << std::endl;';
        const tokens = tokenizeLine(line, 'cpp');
        assertFullCoverage(tokens, line);
    });
});

// ── tokenizeLine — HTML ──

describe('tokenizeLine — HTML', () => {
    it('highlights tags', () => {
        const tokens = tokenizeLine('<div class="main">', 'html');
        expect(tokens.some(t => t.type === 'keyword' && t.text === '<div')).toBe(true);
    });

    it('highlights attribute values', () => {
        const tokens = tokenizeLine('<div class="main">', 'html');
        expect(findToken(tokens, 'string')?.text).toBe('"main"');
    });

    it('highlights attribute names', () => {
        const tokens = tokenizeLine('<div class="main">', 'html');
        expect(tokens.some(t => t.type === 'type' && t.text === 'class')).toBe(true);
    });

    it('highlights comments', () => {
        const tokens = tokenizeLine('<!-- comment -->', 'html');
        expect(tokens[0].type).toBe('comment');
    });

    it('full coverage', () => {
        const line = '<a href="https://example.com">Link</a>';
        const tokens = tokenizeLine(line, 'html');
        assertFullCoverage(tokens, line);
    });
});

// ── tokenizeLine — CSS ──

describe('tokenizeLine — CSS', () => {
    it('highlights selectors', () => {
        const tokens = tokenizeLine('.container {', 'css');
        expect(tokens.some(t => t.type === 'keyword' && t.text === '.container')).toBe(true);
    });

    it('highlights property names', () => {
        const tokens = tokenizeLine('  color: red;', 'css');
        expect(tokens.some(t => t.type === 'type' && t.text === 'color')).toBe(true);
    });

    it('highlights numbers with units', () => {
        const tokens = tokenizeLine('  width: 100px;', 'css');
        expect(tokens.some(t => t.type === 'number' && t.text === '100px')).toBe(true);
    });

    it('highlights hex colors', () => {
        const tokens = tokenizeLine('  color: #ff0000;', 'css');
        expect(tokens.some(t => t.type === 'number' && t.text === '#ff0000')).toBe(true);
    });

    it('highlights comments', () => {
        const tokens = tokenizeLine('/* css comment */', 'css');
        expect(tokens[0].type).toBe('comment');
    });

    it('full coverage', () => {
        const line = '.btn { display: flex; }';
        const tokens = tokenizeLine(line, 'css');
        assertFullCoverage(tokens, line);
    });
});

// ── tokenizeLine — JSON ──

describe('tokenizeLine — JSON', () => {
    it('highlights keys', () => {
        const tokens = tokenizeLine('  "name": "value",', 'json');
        expect(tokens.some(t => t.type === 'type' && t.text === '"name"')).toBe(true);
    });

    it('highlights string values', () => {
        const tokens = tokenizeLine('  "name": "value",', 'json');
        expect(tokens.some(t => t.type === 'string' && t.text === '"value"')).toBe(true);
    });

    it('highlights numbers', () => {
        const tokens = tokenizeLine('  "count": 42,', 'json');
        expect(findToken(tokens, 'number')?.text).toBe('42');
    });

    it('highlights booleans and null', () => {
        const tokens = tokenizeLine('  "active": true,', 'json');
        expect(tokens.some(t => t.type === 'keyword' && t.text === 'true')).toBe(true);
    });

    it('highlights structural characters', () => {
        const tokens = tokenizeLine('{', 'json');
        expect(tokens[0]).toEqual({ text: '{', type: 'operator' });
    });

    it('full coverage', () => {
        const line = '  "key": "value",';
        const tokens = tokenizeLine(line, 'json');
        assertFullCoverage(tokens, line);
    });
});

// ── tokenizeLine — YAML ──

describe('tokenizeLine — YAML', () => {
    it('highlights keys', () => {
        const tokens = tokenizeLine('name: value', 'yaml');
        expect(tokens.some(t => t.type === 'type' && t.text === 'name')).toBe(true);
    });

    it('highlights comments', () => {
        const tokens = tokenizeLine('# yaml comment', 'yaml');
        expect(tokens[0].type).toBe('comment');
    });

    it('highlights strings', () => {
        const tokens = tokenizeLine('key: "hello"', 'yaml');
        expect(findToken(tokens, 'string')?.text).toBe('"hello"');
    });

    it('highlights booleans', () => {
        const tokens = tokenizeLine('enabled: true', 'yaml');
        expect(tokens.some(t => t.type === 'keyword' && t.text === 'true')).toBe(true);
    });

    it('highlights numbers', () => {
        const tokens = tokenizeLine('port: 8080', 'yaml');
        expect(findToken(tokens, 'number')?.text).toBe('8080');
    });

    it('full coverage', () => {
        const line = 'server: "localhost"';
        const tokens = tokenizeLine(line, 'yaml');
        assertFullCoverage(tokens, line);
    });
});

// ── tokenizeLine — Shell ──

describe('tokenizeLine — Shell', () => {
    it('highlights keywords', () => {
        const tokens = tokenizeLine('if [ -f file ]; then', 'shell');
        expect(findToken(tokens, 'keyword')?.text).toBe('if');
    });

    it('highlights comments', () => {
        const tokens = tokenizeLine('# shell comment', 'shell');
        expect(tokens[0].type).toBe('comment');
    });

    it('highlights strings', () => {
        const tokens = tokenizeLine('echo "hello"', 'shell');
        expect(findToken(tokens, 'string')?.text).toBe('"hello"');
    });

    it('highlights variables', () => {
        const tokens = tokenizeLine('echo $HOME', 'shell');
        expect(tokens.some(t => t.type === 'type' && t.text === '$HOME')).toBe(true);
    });

    it('highlights braced variables', () => {
        const tokens = tokenizeLine('echo ${PATH}', 'shell');
        expect(tokens.some(t => t.type === 'type' && t.text === '${PATH}')).toBe(true);
    });

    it('highlights pipe operator', () => {
        const tokens = tokenizeLine('cat file | grep pattern', 'shell');
        expect(tokens.some(t => t.type === 'operator' && t.text === '|')).toBe(true);
    });

    it('full coverage', () => {
        const line = 'for f in *.txt; do echo "$f"; done';
        const tokens = tokenizeLine(line, 'shell');
        assertFullCoverage(tokens, line);
    });
});
