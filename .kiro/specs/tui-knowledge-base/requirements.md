# Requirements Document

## Introduction

This feature adds knowledge base capabilities to the TUI version of MaClaw. The GUI already has a full knowledge base system (`corelib/knowledge/` SQLite store with FTS, cards, facts, source nodes, and auto-recall). The TUI needs two capabilities: (1) automatic RAG retrieval during chat conversations to enhance agent responses with relevant context from imported documents, and (2) a CLI command for importing files or directories into the knowledge base.

Both capabilities share the same underlying `corelib/knowledge.SQLiteStore` and `~/.maclaw/knowledge.db` database that the GUI uses. The TUI does not need the full GUI management UI (source links, quality maintenance, topic relevance graphs, etc.) — it needs the core import and retrieval paths.

## Glossary

- **Knowledge_Store**: The `corelib/knowledge.SQLiteStore` — a SQLite database at `~/.maclaw/knowledge.db` storing parsed document sources, nodes, cards, facts, and FTS indexes.
- **Auto_Recall**: The mechanism that automatically searches the Knowledge_Store for every user message and injects relevant snippets into the LLM system prompt before the agent loop runs.
- **Context_Pack**: A compact, citation-backed bundle of ranked cards, facts, and source nodes under a character budget, built from Knowledge_Store search results.
- **Import_Pipeline**: The process of scanning files, parsing supported formats (PDF, DOCX, XLSX, CSV, Markdown, TXT), chunking into nodes, and optionally distilling into cards/facts.
- **TUI_Agent_Loop**: The `corelib/agent.RunLoop` execution with `LoopCallbacks` provided by the TUI's `pipeCallbacks` or interactive chat callbacks.
- **Knowledge_CLI**: The `maclaw-tui knowledge` subcommand group for managing the knowledge base from the terminal.

## Requirements

### Requirement 1: Knowledge Base Auto-Recall in TUI Agent Loop

**User Story:** As a TUI user, I want the agent to automatically search my knowledge base when I ask questions, so that responses are enriched with relevant context from my imported documents without me needing to manually invoke search tools.

#### Acceptance Criteria

1. WHEN a user message is received in the TUI agent loop, THE Auto_Recall SHALL search the Knowledge_Store using the user message text and inject relevant snippets into the system prompt.
2. WHILE the Knowledge_Store contains at least one source, THE Auto_Recall SHALL execute a search with a 3-second timeout before each agent loop iteration.
3. THE Auto_Recall SHALL truncate the user message to 200 characters for the FTS query to avoid noisy tokens from long pastes.
4. WHEN search results exceed a score threshold of 0.3, THE Auto_Recall SHALL inject up to 3 snippets into the system prompt under a "知识库参考（自动检索）" section.
5. WHEN the Knowledge_Store has zero sources, THE Auto_Recall SHALL skip the search entirely with no performance overhead.
6. THE Auto_Recall SHALL reuse a cached Knowledge_Store connection across messages to avoid repeated open/close overhead.
7. IF the Knowledge_Store database does not exist at `~/.maclaw/knowledge.db`, THEN THE Auto_Recall SHALL gracefully skip without error.

### Requirement 2: Knowledge Tools Registration in TUI

**User Story:** As a TUI user, I want the agent to have access to knowledge search and save tools, so that the LLM can perform deeper knowledge retrieval or save information when I explicitly ask.

#### Acceptance Criteria

1. THE TUI_Agent_Loop SHALL register `knowledge_search`, `knowledge_context_pack`, `knowledge_save_text`, and `knowledge_save_url` tools in the CoreToolRegistry.
2. WHEN the LLM calls `knowledge_search`, THE TUI_Agent_Loop SHALL execute a local FTS search against the Knowledge_Store and return ranked results.
3. WHEN the LLM calls `knowledge_context_pack`, THE TUI_Agent_Loop SHALL build a citation-backed context bundle under the specified character budget.
4. WHEN the LLM calls `knowledge_save_text`, THE TUI_Agent_Loop SHALL persist the provided text as a new source in the Knowledge_Store.
5. WHEN the LLM calls `knowledge_save_url`, THE TUI_Agent_Loop SHALL fetch the URL content and persist it as a new source in the Knowledge_Store.
6. IF the Knowledge_Store cannot be opened, THEN THE knowledge tools SHALL return a descriptive error message to the LLM.

### Requirement 3: Knowledge Base Import CLI — File Import

**User Story:** As a TUI user, I want to import individual files into the knowledge base from the command line, so that I can add documents without needing the GUI.

#### Acceptance Criteria

1. WHEN `maclaw-tui knowledge import <file-path>` is executed, THE Knowledge_CLI SHALL parse the file and add it to the Knowledge_Store.
2. THE Knowledge_CLI SHALL support the following file formats: PDF, DOCX, XLSX, CSV, Markdown (.md), plain text (.txt), and legacy DOC/XLS when LibreOffice is available.
3. WHEN the import succeeds, THE Knowledge_CLI SHALL print a summary including: file name, number of nodes created, and source ID.
4. IF the file does not exist, THEN THE Knowledge_CLI SHALL print a descriptive error and exit with a non-zero exit code.
5. IF the file format is unsupported, THEN THE Knowledge_CLI SHALL print a descriptive error listing supported formats and exit with a non-zero exit code.
6. WHEN multiple file paths are provided (`maclaw-tui knowledge import file1.pdf file2.md`), THE Knowledge_CLI SHALL import all files in sequence and report per-file results.

### Requirement 4: Knowledge Base Import CLI — Directory Import

**User Story:** As a TUI user, I want to import an entire directory of documents into the knowledge base, so that I can batch-import a folder of reference materials.

#### Acceptance Criteria

1. WHEN `maclaw-tui knowledge import <directory-path>` is executed, THE Knowledge_CLI SHALL recursively scan the directory for supported files and import them.
2. THE Knowledge_CLI SHALL filter files by the default include extensions list from `knowledge.Capabilities()` (PDF, DOCX, XLSX, CSV, MD, TXT, DOC, XLS).
3. WHEN the import completes, THE Knowledge_CLI SHALL print a summary including: total files found, files imported, files skipped (duplicates), and files failed.
4. THE Knowledge_CLI SHALL skip files that have already been imported (content hash deduplication) and report them as "skipped (duplicate)".
5. IF the directory does not exist, THEN THE Knowledge_CLI SHALL print a descriptive error and exit with a non-zero exit code.
6. WHERE the `--dry-run` flag is provided, THE Knowledge_CLI SHALL scan and report what would be imported without actually writing to the Knowledge_Store.

### Requirement 5: Knowledge Base Import CLI — Options and Configuration

**User Story:** As a TUI user, I want to control import behavior through CLI flags, so that I can customize how documents are processed.

#### Acceptance Criteria

1. WHERE the `--project <path>` flag is provided, THE Knowledge_CLI SHALL associate imported sources with the specified project path.
2. WHERE the `--labels <label1,label2>` flag is provided, THE Knowledge_CLI SHALL attach the specified labels to all imported sources.
3. WHERE the `--scope <project|personal|local_only>` flag is provided, THE Knowledge_CLI SHALL set the save scope for imported sources (default: project).
4. WHERE the `--include-exts <.pdf,.md,.txt>` flag is provided, THE Knowledge_CLI SHALL override the default extension filter for directory imports.
5. THE Knowledge_CLI SHALL use the shared `~/.maclaw/knowledge.db` database path, consistent with the GUI.
6. WHEN the `--json` flag is provided, THE Knowledge_CLI SHALL output results in JSON format for scripting integration.

### Requirement 6: Knowledge Base Management CLI Commands

**User Story:** As a TUI user, I want basic knowledge base management commands, so that I can list sources, search, and clear the knowledge base from the terminal.

#### Acceptance Criteria

1. WHEN `maclaw-tui knowledge list` is executed, THE Knowledge_CLI SHALL print a table of all sources with columns: ID, Kind, Title/Path, Status, Nodes, Cards, Updated.
2. WHEN `maclaw-tui knowledge search <query>` is executed, THE Knowledge_CLI SHALL perform an FTS search and print ranked results with source, snippet, and score.
3. WHEN `maclaw-tui knowledge status` is executed, THE Knowledge_CLI SHALL print knowledge base statistics: total sources, total nodes, total cards, total facts, database size.
4. WHEN `maclaw-tui knowledge delete <source-id>` is executed, THE Knowledge_CLI SHALL remove the specified source and all its nodes/cards/facts from the Knowledge_Store.
5. IF `maclaw-tui knowledge delete` is called without `--force`, THEN THE Knowledge_CLI SHALL prompt for confirmation before deletion.
6. WHEN `maclaw-tui knowledge clear` is executed with `--force`, THE Knowledge_CLI SHALL remove all sources from the Knowledge_Store.

### Requirement 7: System Prompt Knowledge Base Rules in TUI

**User Story:** As a TUI user, I want the system prompt to include knowledge base usage guidance, so that the LLM knows how to leverage the knowledge tools effectively.

#### Acceptance Criteria

1. WHEN the Knowledge_Store has at least one source, THE TUI system prompt builder SHALL set `HasKnowledgeBase: true` in `SystemPromptDeps`.
2. THE system prompt SHALL include rules instructing the LLM to use `knowledge_search` or `knowledge_context_pack` for deeper retrieval when auto-recall snippets are insufficient.
3. THE system prompt SHALL instruct the LLM to use `knowledge_save_text` or `knowledge_save_url` only when the user explicitly asks to save information to the knowledge base.
