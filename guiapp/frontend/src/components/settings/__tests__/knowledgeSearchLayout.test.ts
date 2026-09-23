import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

const css = readFileSync(join(process.cwd(), 'src/App.css'), 'utf8');

describe('knowledge search layout', () => {
    it('keeps results and facets in non-overlapping columns', () => {
        expect(css).toMatch(/\.knowledge-two-column\s*\{[^}]*minmax\(0,\s*1fr\)/);
        expect(css).toMatch(/\.knowledge-two-column > \*\s*\{[^}]*min-width:\s*0/);
        expect(css).toMatch(/\.knowledge-two-column--search\s*\{[^}]*minmax\(0,\s*1fr\)\s+minmax\(200px,\s*280px\)/);
        expect(css).toMatch(/\.knowledge-search-results-only\s*\{[^}]*width:\s*100%/);
        expect(css).not.toMatch(/\.knowledge-two-column\s*\{[^}]*auto-fit/);
        expect(css).toMatch(/\.knowledge-stack\s*\{[^}]*container-type:\s*inline-size/);
        expect(css).toMatch(/@container \(max-width:\s*720px\)[\s\S]*\.knowledge-two-column--search/);
    });

    it('labels compact search filters and clamps result snippets', () => {
        expect(css).toMatch(/\.knowledge-search-filter\s*\{[^}]*flex-direction:\s*column/);
        expect(css).toMatch(/\.knowledge-search-filter--fixed\s*\{[^}]*max-width:\s*180px/);
        expect(css).toMatch(/\.knowledge-search-source-chip\s*\{[^}]*text-overflow:\s*ellipsis/);
        expect(css).toMatch(/\.knowledge-search-filter--limit\s*\{[^}]*96px/);
        expect(css).toMatch(/\.knowledge-result-snippet\s*\{[^}]*-webkit-line-clamp:\s*5/);
        expect(css).toMatch(/\.knowledge-row-main\s*\{[^}]*overflow-wrap:\s*anywhere/);
        expect(css).toMatch(/\.knowledge-chip\s*\{[^}]*overflow-wrap:\s*anywhere/);
        expect(css).toMatch(/button\.knowledge-chip\s*\{[^}]*cursor:\s*pointer/);
        expect(css).toMatch(/\.knowledge-row-delete\s*\{[^}]*white-space:\s*nowrap/);
        expect(css).toMatch(/\.knowledge-search-facets \.knowledge-block-body\s*\{[^}]*overflow:\s*auto/);
        expect(css).toMatch(/\.knowledge-facet-chip\s*\{[^}]*flex:\s*0 0 auto/);
        expect(css).toMatch(/\.knowledge-facet-chip-name\s*\{[^}]*text-overflow:\s*ellipsis/);
        expect(css).toMatch(/\.knowledge-facet-chip-count\s*\{[^}]*flex:\s*0 0 auto/);
    });
});
