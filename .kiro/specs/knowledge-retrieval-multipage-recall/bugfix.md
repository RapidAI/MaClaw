# Bugfix Requirements Document

## Introduction

Multi-page PDF documents imported into the knowledge base fail to be fully recalled during auto-recall. When a user asks about content located on specific pages (e.g., book publication info on page 2 of a 4-page resume), the system frequently misses the relevant page due to five compounding issues: snippet truncation (200 chars), injection limit (max 3 results), FTS OR-mode dilution of key terms, lack of source-level page expansion, and premature skipping of embedding search when FTS topScore >= 2.0. The user sees "未提及" (not mentioned) for information that clearly exists in the imported PDF.

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN auto-recall injects search results into the system prompt THEN the system truncates each result snippet to 200 rune characters, causing critical information in the latter half of a page to be lost before the LLM sees it

1.2 WHEN auto-recall determines the number of results to inject based on topScore THEN the system injects at most 2-3 results (maxInject), causing pages with lower FTS scores to be dropped even when they contain the answer

1.3 WHEN a Chinese query (e.g., "马勇博士出版过书籍吗") is tokenized for FTS search THEN the system builds an OR query where high-frequency generic terms ("马勇", "博士") appear on all pages with low IDF, while the key distinguishing term ("书籍") may not exactly match the target page's text ("出版社", "译著"), causing irrelevant pages to rank higher than the page with actual book info

1.4 WHEN one page of a multi-page PDF matches the FTS query THEN the system does not expand results to include sibling pages from the same source document, causing each page to compete independently in FTS ranking with no document-level association

1.5 WHEN FTS returns results with topScore >= 2.0 (from generic term frequency across multiple pages) THEN the system completely skips embedding/vector search, causing semantic matches (e.g., "书籍" ≈ "出版社"+"译著") to never be evaluated

### Expected Behavior (Correct)

2.1 WHEN auto-recall injects search results into the system prompt THEN the system SHALL provide sufficient snippet length (or full page content within budget) so that critical information on a page is not lost due to truncation

2.2 WHEN auto-recall determines the number of results to inject THEN the system SHALL inject enough results to cover all relevant pages of a multi-page document, or SHALL use source-level grouping to ensure sibling pages are considered together

2.3 WHEN a Chinese query is tokenized for FTS search against a multi-page PDF THEN the system SHALL weight distinguishing terms higher or use a ranking strategy that does not dilute key query terms across pages that only match generic terms

2.4 WHEN one page of a multi-page PDF matches a search query THEN the system SHALL expand the result set to include sibling pages from the same source (source-level expansion), ensuring multi-page context is available for the LLM to answer correctly

2.5 WHEN FTS returns high-scoring results from generic term frequency THEN the system SHALL still execute embedding/vector search as a complementary signal, using RRF (Reciprocal Rank Fusion) or similar to combine FTS and semantic similarity scores, so that semantically relevant pages are not excluded

### Unchanged Behavior (Regression Prevention)

3.1 WHEN a query matches a single-page document or a card/fact result THEN the system SHALL CONTINUE TO return that result with correct ranking and snippet content

3.2 WHEN a query has no relevant matches in the knowledge base THEN the system SHALL CONTINUE TO return no results (or results below threshold) without injecting irrelevant content

3.3 WHEN the knowledge base contains non-PDF sources (web pages, notes, markdown files) THEN the system SHALL CONTINUE TO search and rank those sources correctly using the existing FTS + embedding pipeline

3.4 WHEN a query is purely ASCII/Latin text against English documents THEN the system SHALL CONTINUE TO use the existing FTS5 unicode61 tokenizer path without regression

3.5 WHEN the auto-recall search completes within the 3-second timeout THEN the system SHALL CONTINUE TO respect the timeout boundary and not introduce latency beyond acceptable limits for the additional source expansion or embedding search

3.6 WHEN ContextPack is called with explicit search options THEN the system SHALL CONTINUE TO respect the caller's maxItems and maxChars budget constraints
