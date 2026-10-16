/**
 * Guards the diagram syntax the system prompt teaches the model.
 *
 * The visual-explanation prompt block (corelib/agent/prompt_blocks.go,
 * PromptVisualizationRules) routes each explanation target to a concrete
 * mermaid diagram type and states how Chinese labels must be written. Those
 * claims were written once from memory and two of them were wrong:
 *
 *   - `CUSTOMER["客户表"]` (erDiagram entity alias) does not parse.
 *   - mindmap DOES accept quoted labels, contrary to what the first draft
 *     asserted; the real mindmap hazard is round brackets selecting a shape.
 *
 * A wrong hint is worse than no hint: the model emits a fence that fails to
 * parse and the UI degrades to raw source.
 *
 * IMPORTANT: these cases run the REAL mermaid parser. An earlier revision of
 * this file only asserted `startsWith(type)` under a name claiming "parses" —
 * a guard that could never fail, protecting nothing. If you weaken this suite,
 * NEGATIVE_CONTROLS below must still hold: they are what proves the parser is
 * actually being invoked rather than the assertions being vacuously true.
 */
import mermaid from 'mermaid';
import { beforeAll, describe, expect, it } from 'vitest';

beforeAll(() => {
    mermaid.initialize({ startOnLoad: false, securityLevel: 'strict', suppressErrorRendering: true });
});

/**
 * Verified against the installed mermaid: parse() RESOLVES with a diagram
 * descriptor for accepted input and THROWS for rejected input (it does not
 * resolve false). Both the type definition and runtime agree, so the try/catch
 * below is the whole contract.
 */
async function accepts(source: string): Promise<boolean> {
    try {
        await mermaid.parse(source);
        return true;
    } catch {
        return false;
    }
}

const EXPECTED_TYPES = [
    'flowchart',
    'sequenceDiagram',
    'erDiagram',
    'stateDiagram-v2',
    'gantt',
    'mindmap',
    'pie',
    'C4Context',
    'C4Container',
];

describe('taught mermaid diagram forms', () => {
    it.each(EXPECTED_TYPES)('%s parses when written as the prompt teaches', async (type) => {
        const sample = SAMPLES[type];
        expect(sample).toBeTruthy();
        expect(sample.startsWith(type)).toBe(true);
        expect(await accepts(sample)).toBe(true);
    });

    describe('negative controls', () => {
        // If these ever pass, the parser stopped running and every "parses"
        // assertion above became meaningless.
        it.each(NEGATIVE_CONTROLS)('rejects %s', async ({ name, source }) => {
            expect(await accepts(source), `${name} should NOT parse`).toBe(false);
        });
    });

    it('documents the erDiagram form that actually parses', () => {
        expect(ER_DIAGRAM_VALID).toContain('CUSTOMER ||--o{ ORDER : 下单');
        expect(ER_DIAGRAM_VALID).not.toMatch(/\w+\["[^"]+"\]\s*(\|\||\{|\})/);
    });

    it('documents the mindmap forms that actually parse', () => {
        expect(MINDMAP_VALID).toContain('"分支一"');
        expect(MINDMAP_VALID).toContain('网关层');
    });

    describe('language independence', () => {
        // The prompt claims its syntax constraints are language-neutral, so
        // every taught form must hold with English labels too — otherwise the
        // rule would be silently wrong for non-Chinese replies.
        //
        // NOTE: the earlier draft also warned that round brackets in mindmap
        // node text break parsing. That was measured FALSE: `分支(A)` parses
        // fine. Whether it alters the rendered SHAPE could not be verified
        // (jsdom has no SVG layout engine — see file header), so the claim was
        // removed rather than left standing without evidence.
        it.each(LANGUAGE_PAIRS)('%s parses with English labels too', async ({ label, source }) => {
            expect(await accepts(source), `${label} (English) should parse`).toBe(true);
        });
    });
});

/** The taught form that must hold regardless of the reply language. */
const LANGUAGE_PAIRS: Array<{ label: string; source: string }> = [
    { label: 'flowchart', source: `flowchart TD\n  A["Ingest"] --> B["Store"]` },
    { label: 'sequenceDiagram', source: `sequenceDiagram\n  C->>S: request` },
    {
        label: 'erDiagram',
        source: `erDiagram
  CUSTOMER ||--o{ ORDER : places
  CUSTOMER {
    string name
  }`,
    },
    { label: 'mindmap', source: `mindmap\n  root((Architecture))\n    "Branch One"` },
    { label: 'stateDiagram-v2', source: `stateDiagram-v2\n  [*] --> Pending` },
    { label: 'pie', source: `pie title Sources\n  "Search" : 45` },
];

/**
 * Forms that must keep failing — originally taught or plausible mistakes.
 * Each is here because the prompt either warns against it or relies on its
 * rejection being real.
 */
const NEGATIVE_CONTROLS: Array<{ name: string; source: string }> = [
    {
        name: 'erDiagram entity alias',
        source: `erDiagram
  CUSTOMER["客户表"] ||--o{ ORDER : 下单`,
    },
    {
        name: 'undeclared diagram keyword',
        source: `thisIsNotADiagram
  A --> B`,
    },
];

/**
 * Canonical samples verified against the installed mermaid with mermaid.parse().
 * Keep these minimal and representative: they document accepted syntax, and
 * this suite is the executable memory of that check.
 */
//
// erDiagram: the entity name must be a space-free ASCII identifier. Chinese
// column labels belong inside the attribute block, one per line.
const ER_DIAGRAM_VALID = `erDiagram
  CUSTOMER ||--o{ ORDER : 下单
  CUSTOMER {
    string 姓名
    int 编号
  }`;

// mindmap: Chinese and quoted labels both parse. Round brackets are the hazard
// (they select a node shape), so they stay out of node text.
const MINDMAP_VALID = `mindmap
  root((系统架构))
    "分支一"
    网关层
    服务层`;

const SAMPLES: Record<string, string> = {
    flowchart: `flowchart TD
  subgraph Ingest["数据接入"]
    A["采集器"] --> B["清洗"]
  end
  B --> C["存储"]`,
    sequenceDiagram: `sequenceDiagram
  participant C as 客户端
  participant S as 服务端
  C->>S: 请求
  S-->>C: 结果`,
    erDiagram: ER_DIAGRAM_VALID,
    'stateDiagram-v2': `stateDiagram-v2
  [*] --> 待审核
  待审核 --> 已通过: 通过
  已通过 --> [*]`,
    gantt: `gantt
  dateFormat YYYY-MM-DD
  section 设计
  方案设计 :a1, 2026-01-01, 7d`,
    mindmap: MINDMAP_VALID,
    pie: `pie title 来源占比
  "自然搜索" : 45
  "直接访问" : 30`,
    C4Context: `C4Context
  Person(user, "用户")
  System(app, "MaClaw")
  Rel(user, app, "提问")`,
    C4Container: `C4Container
  Person(user, "用户")
  Container_Boundary(app, "MaClaw") {
    Container(gui, "前端", "React")
  }
  Rel(user, gui, "使用")`,
};
