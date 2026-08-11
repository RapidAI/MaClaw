package main

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

const knowledgeToolSourceKindsDescription = "Optional source kinds: url, pdf, doc, docx, ppt, pptx, xls, xlsx, csv, markdown, text, conversation, workflow_artifact"

func registerKnowledgeTools(registry *ToolRegistry, app *App) {
	if registry == nil || app == nil {
		return
	}
	registry.Register(RegisteredTool{
		Name:        "knowledge_search",
		Description: knowledge.KnowledgeSearchToolDescription,
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "memory", "search", "local", "brain", "recall"},
		Priority:    10,
		Status:      RegToolAvailable,
		Required:    []string{"query"},
		InputSchema: map[string]interface{}{
			"query":        map[string]string{"type": "string", "description": "Search query"},
			"search_scope": map[string]string{"type": "string", "description": "all | project | personal. Default all."},
			"topic_hint":   map[string]string{"type": "string", "description": "Optional current topic hint for local re-ranking. Does not call an LLM."},
			"context_terms": map[string]interface{}{
				"type":        "array",
				"items":       map[string]string{"type": "string"},
				"description": "Optional current conversation/project terms for local re-ranking.",
			},
			"result_types": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional result types: card, fact, node"},
			"source_kinds": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": knowledgeToolSourceKindsDescription},
			"source_ids":   knowledgeSourceIDsSchema("Optional exact source IDs to search within."),
			"ids":          knowledgeSourceIDsAliasSchema(),
			"labels":       map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections. Sources must have every supplied label."},
			"domain":       map[string]string{"type": "string", "description": "Optional URL source domain filter, for example example.com. Includes subdomains."},
			"project_path": map[string]string{"type": "string", "description": "Optional explicit project path for project scope"},
			"limit":        map[string]string{"type": "integer", "description": "Max results, default 8, max 50"},
			"include_disabled": map[string]string{
				"type":        "boolean",
				"description": "Include disabled sources. Default false.",
			},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeSearch(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_image_search",
		Description: "Search only imported knowledge-base images by OCR text, visual description, filename, and surrounding document context. Use when the user asks to find, show, view, select, or compare stored images. Results include safe display markers; when the user asks to see an image, copy its exact marker unchanged onto its own line in the final answer.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "image", "search", "local", "recall"},
		Priority:    10,
		Status:      RegToolAvailable,
		Required:    []string{"query"},
		InputSchema: map[string]interface{}{
			"query":        map[string]string{"type": "string", "description": "Search image OCR/caption/context"},
			"search_scope": map[string]string{"type": "string", "description": "all | project | personal. Default all."},
			"topic_hint":   map[string]string{"type": "string", "description": "Optional current topic hint for local re-ranking."},
			"context_terms": map[string]interface{}{
				"type":        "array",
				"items":       map[string]string{"type": "string"},
				"description": "Optional conversation/project terms for local re-ranking.",
			},
			"source_kinds":     map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source kind filter."},
			"source_ids":       knowledgeSourceIDsSchema("Optional exact source IDs to search within."),
			"ids":              knowledgeSourceIDsAliasSchema(),
			"labels":           map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections."},
			"domain":           map[string]string{"type": "string", "description": "Optional URL source domain filter."},
			"project_path":     map[string]string{"type": "string", "description": "Optional explicit project path for project scope."},
			"limit":            map[string]string{"type": "integer", "description": "Max image results, default 8, max 50."},
			"include_disabled": map[string]string{"type": "boolean", "description": "Include disabled sources. Default false."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeImageSearch(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_explain",
		Description: "Explain local knowledge recall without calling an LLM. Returns ranked card/fact/node hits plus citations with source URL/path, page, sheet, and row/column hints when available. Use before answering from stored knowledge when the user needs sources or asks why something was recalled.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "explain", "citation", "local", "brain"},
		Priority:    6,
		Status:      RegToolAvailable,
		Required:    []string{"query"},
		InputSchema: map[string]interface{}{
			"query":        map[string]string{"type": "string", "description": "Search query to explain"},
			"search_scope": map[string]string{"type": "string", "description": "all | project | personal. Default all."},
			"topic_hint":   map[string]string{"type": "string", "description": "Optional current topic hint for local re-ranking. Does not call an LLM."},
			"context_terms": map[string]interface{}{
				"type":        "array",
				"items":       map[string]string{"type": "string"},
				"description": "Optional current conversation/project terms for local re-ranking.",
			},
			"result_types": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional result types: card, fact, node"},
			"source_kinds": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": knowledgeToolSourceKindsDescription},
			"source_ids":   knowledgeSourceIDsSchema("Optional exact source IDs to search within."),
			"ids":          knowledgeSourceIDsAliasSchema(),
			"labels":       map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections. Sources must have every supplied label."},
			"domain":       map[string]string{"type": "string", "description": "Optional URL source domain filter, for example example.com. Includes subdomains."},
			"project_path": map[string]string{"type": "string", "description": "Optional explicit project path for project scope"},
			"limit":        map[string]string{"type": "integer", "description": "Max results, default 8, max 50"},
			"include_disabled": map[string]string{
				"type":        "boolean",
				"description": "Include disabled sources. Default false.",
			},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeExplain(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_context_pack",
		Description: knowledge.KnowledgeContextPackToolDescription,
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "context", "citation", "local", "brain", "recall"},
		Priority:    10,
		Status:      RegToolAvailable,
		Required:    []string{"query"},
		InputSchema: map[string]interface{}{
			"query":        map[string]string{"type": "string", "description": "Search query for the context pack"},
			"search_scope": map[string]string{"type": "string", "description": "all | project | personal. Default all."},
			"topic_hint":   map[string]string{"type": "string", "description": "Optional current topic hint for local re-ranking. Does not call an LLM."},
			"context_terms": map[string]interface{}{
				"type":        "array",
				"items":       map[string]string{"type": "string"},
				"description": "Optional current conversation/project terms for local re-ranking.",
			},
			"result_types":     map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional result types: card, fact, node"},
			"source_kinds":     map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": knowledgeToolSourceKindsDescription},
			"source_ids":       knowledgeSourceIDsSchema("Optional exact source IDs to search within."),
			"ids":              knowledgeSourceIDsAliasSchema(),
			"labels":           map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections. Sources must have every supplied label."},
			"domain":           map[string]string{"type": "string", "description": "Optional URL source domain filter, for example example.com. Includes subdomains."},
			"project_path":     map[string]string{"type": "string", "description": "Optional explicit project path for project scope"},
			"max_items":        map[string]string{"type": "integer", "description": "Max context items, default 8, max 30"},
			"max_chars":        map[string]string{"type": "integer", "description": "Max total context characters, default 6000, max 20000"},
			"include_disabled": map[string]string{"type": "boolean", "description": "Include disabled sources. Default false."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeContextPack(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_search_facets",
		Description: "Summarize local knowledge search hits into facets without calling an LLM. Use to narrow a broad recall by result type, source type, domain, source, entity, or predicate before answering from a large knowledge base.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "facets", "search", "filter", "local", "brain"},
		Priority:    7,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"query":        map[string]string{"type": "string", "description": "Optional search query to facet. Empty browses local knowledge distribution."},
			"search_scope": map[string]string{"type": "string", "description": "all | project | personal. Default all."},
			"topic_hint":   map[string]string{"type": "string", "description": "Optional current topic hint for local re-ranking. Does not call an LLM."},
			"context_terms": map[string]interface{}{
				"type":        "array",
				"items":       map[string]string{"type": "string"},
				"description": "Optional current conversation/project terms for local re-ranking.",
			},
			"result_types":     map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional result types: card, fact, node"},
			"source_kinds":     map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": knowledgeToolSourceKindsDescription},
			"source_ids":       knowledgeSourceIDsSchema("Optional exact source IDs to search within."),
			"ids":              knowledgeSourceIDsAliasSchema(),
			"labels":           map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections. Sources must have every supplied label."},
			"domain":           map[string]string{"type": "string", "description": "Optional URL source domain filter, for example example.com. Includes subdomains."},
			"project_path":     map[string]string{"type": "string", "description": "Optional explicit project path for project scope"},
			"limit":            map[string]string{"type": "integer", "description": "Max search hits to facet, default 80, max 100"},
			"include_disabled": map[string]string{"type": "boolean", "description": "Include disabled sources. Default false."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeSearchFacets(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_topic_relevance",
		Description: "Find sources related to the current topic using only local metadata, labels, cards, facts, and document nodes. Use before saving or recalling knowledge when the user wants topic-linked storage or wants to see which saved sources are relevant to the current conversation. Does not call an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "topic", "relevance", "source", "local", "brain"},
		Priority:    7,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"topic_hint":       map[string]string{"type": "string", "description": "Current topic hint. If omitted, query is used as the topic text."},
			"query":            map[string]string{"type": "string", "description": "Optional fallback topic text and source filter context."},
			"context_terms":    map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional current conversation/project terms."},
			"search_scope":     map[string]string{"type": "string", "description": "all | project | personal. Default all."},
			"source_kinds":     map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source kinds."},
			"source_ids":       knowledgeSourceIDsSchema("Optional exact source IDs to inspect."),
			"ids":              knowledgeSourceIDsAliasSchema(),
			"labels":           map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections."},
			"domain":           map[string]string{"type": "string", "description": "Optional URL source domain filter."},
			"project_path":     map[string]string{"type": "string", "description": "Optional explicit project path for project scope."},
			"limit":            map[string]string{"type": "integer", "description": "Max related sources, default 20, max 100."},
			"include_disabled": map[string]string{"type": "boolean", "description": "Include disabled sources. Default false."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeTopicRelevance(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_fact_graph",
		Description: "Build a local fact graph from stored knowledge facts without calling an LLM. Use when the user asks for relationships, entities, dependencies, decisions, or a structured map of saved knowledge. Returns nodes and grounded edges with citations.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "graph", "facts", "local", "brain", "relations"},
		Priority:    7,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"query":        map[string]string{"type": "string", "description": "Optional entity, relation, or topic query. Empty returns recent/high-confidence facts."},
			"entity":       map[string]string{"type": "string", "description": "Optional subject/object entity drilldown filter."},
			"predicate":    map[string]string{"type": "string", "description": "Optional predicate/relation filter, for example uses, topic, mentions."},
			"search_scope": map[string]string{"type": "string", "description": "all | project | personal. Default all."},
			"source_kinds": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": knowledgeToolSourceKindsDescription},
			"source_ids":   knowledgeSourceIDsSchema("Optional exact source IDs to search within."),
			"ids":          knowledgeSourceIDsAliasSchema(),
			"labels":       map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections. Sources must have every supplied label."},
			"domain":       map[string]string{"type": "string", "description": "Optional URL source domain filter, for example example.com. Includes subdomains."},
			"project_path": map[string]string{"type": "string", "description": "Optional explicit project path for project scope"},
			"limit":        map[string]string{"type": "integer", "description": "Max fact edges, default 40, max 100"},
			"include_disabled": map[string]string{
				"type":        "boolean",
				"description": "Include disabled sources. Default false.",
			},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeFactGraph(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_fact_index",
		Description: "List the most frequent local knowledge entities or relations derived from stored structured facts without calling an LLM. Use to inspect what the knowledge base knows before drilling into a fact graph.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "index", "entities", "relations", "facts", "local"},
		Priority:    7,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"query":        map[string]string{"type": "string", "description": "Optional text filter applied to labels, predicates, and examples."},
			"kind":         map[string]string{"type": "string", "description": "entity | predicate | subject | object. Default entity."},
			"search_scope": map[string]string{"type": "string", "description": "all | project | personal. Default all."},
			"source_kinds": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": knowledgeToolSourceKindsDescription},
			"source_ids":   knowledgeSourceIDsSchema("Optional exact source IDs to search within."),
			"ids":          knowledgeSourceIDsAliasSchema(),
			"labels":       map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections. Sources must have every supplied label."},
			"domain":       map[string]string{"type": "string", "description": "Optional URL source domain filter, for example example.com. Includes subdomains."},
			"project_path": map[string]string{"type": "string", "description": "Optional explicit project path for project scope"},
			"limit":        map[string]string{"type": "integer", "description": "Max index items, default 60, max 200"},
			"include_disabled": map[string]string{
				"type":        "boolean",
				"description": "Include disabled sources. Default false.",
			},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeFactIndex(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_entity_profile",
		Description: "Build a local entity profile from stored structured facts without calling an LLM. Returns fact edges, related entities, predicates, and citations for one entity.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "entity", "profile", "facts", "relations", "local"},
		Priority:    7,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"entity":       map[string]string{"type": "string", "description": "Entity label to inspect."},
			"query":        map[string]string{"type": "string", "description": "Optional fallback entity label when entity is omitted."},
			"predicate":    map[string]string{"type": "string", "description": "Optional predicate/relation filter."},
			"search_scope": map[string]string{"type": "string", "description": "all | project | personal. Default all."},
			"source_kinds": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": knowledgeToolSourceKindsDescription},
			"source_ids":   knowledgeSourceIDsSchema("Optional exact source IDs to search within."),
			"ids":          knowledgeSourceIDsAliasSchema(),
			"labels":       map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections. Sources must have every supplied label."},
			"domain":       map[string]string{"type": "string", "description": "Optional URL source domain filter, for example example.com. Includes subdomains."},
			"project_path": map[string]string{"type": "string", "description": "Optional explicit project path for project scope"},
			"limit":        map[string]string{"type": "integer", "description": "Max fact edges, default 60, max 100"},
			"include_disabled": map[string]string{
				"type":        "boolean",
				"description": "Include disabled sources. Default false.",
			},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeEntityProfile(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_suggest",
		Description: "Suggest local knowledge entities, predicates, sources, URL domains, source types, and source labels without calling an LLM. Use to help narrow large knowledge bases before searching or building a fact graph.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "suggest", "autocomplete", "entity", "domain", "label", "local"},
		Priority:    7,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"query":        map[string]string{"type": "string", "description": "Optional prefix or contains query for suggestions."},
			"kinds":        map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional suggestion kinds: entity, predicate, source, domain, source_kind, label"},
			"search_scope": map[string]string{"type": "string", "description": "all | project | personal. Default all."},
			"source_kinds": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source kind filter"},
			"source_ids":   knowledgeSourceIDsSchema("Optional exact source IDs to suggest within."),
			"ids":          knowledgeSourceIDsAliasSchema(),
			"labels":       map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections. Sources must have every supplied label."},
			"domain":       map[string]string{"type": "string", "description": "Optional URL source domain filter, for example example.com. Includes subdomains."},
			"project_path": map[string]string{"type": "string", "description": "Optional explicit project path for project scope"},
			"limit":        map[string]string{"type": "integer", "description": "Max suggestions, default 30, max 80"},
			"include_disabled": map[string]string{
				"type":        "boolean",
				"description": "Include disabled sources. Default false.",
			},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeSuggest(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_save_url",
		Description: "Save a public HTTP(S) URL into MaClaw knowledge base. Only use when the user explicitly asks to save, remember, archive, or add a web page to the knowledge base. Fetching has public-network safety checks; write-time LLM distillation may be used if configured, but querying later does not require LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "url", "link", "web", "save", "archive", "brain", "知识库", "保存", "网址", "链接", "网页", "外脑"},
		Priority:    5,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"url":        map[string]string{"type": "string", "description": "Public HTTP(S) URL to save"},
			"link":       map[string]string{"type": "string", "description": "Alias for url."},
			"href":       map[string]string{"type": "string", "description": "Alias for url."},
			"uri":        map[string]string{"type": "string", "description": "Alias for url."},
			"target":     map[string]string{"type": "string", "description": "Alias for url."},
			"save_scope": map[string]string{"type": "string", "description": "project | personal | local_only. Default project."},
			"topic_hint": map[string]string{"type": "string", "description": "Optional topic hint to improve write-time structure"},
			"distill_mode": map[string]string{
				"type":        "string",
				"description": "Optional write-time structuring mode: auto, rules_only, llm_if_available. Default auto.",
			},
			"labels": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections to add during save."},
			"auto_labels": map[string]string{
				"type":        "boolean",
				"description": "When true, add local rule-based labels such as kind:url, domain:example.com, and scope:project. Default true.",
			},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeSaveURL(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_save_urls",
		Description: "Save multiple public HTTP(S) URLs into MaClaw knowledge base. Only use when the user explicitly asks to save, remember, archive, or add a list of web pages to the knowledge base. Each URL is fetched with public-network safety checks; failures are returned per URL and do not stop the whole batch.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "url", "link", "bulk", "save", "archive", "brain", "知识库", "保存", "网址", "链接", "网页", "批量", "外脑"},
		Priority:    5,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"urls":             map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Public HTTP(S) URLs to save"},
			"links":            map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Alias for urls."},
			"hrefs":            map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Alias for urls."},
			"text":             map[string]string{"type": "string", "description": "Optional newline/comma separated URL list, or messy text/HTML when discover_urls is true"},
			"url_list":         map[string]string{"type": "string", "description": "Alias for text when passing a pasted URL list."},
			"link_list":        map[string]string{"type": "string", "description": "Alias for text when passing a pasted URL list."},
			"html":             map[string]string{"type": "string", "description": "Optional HTML or sitemap XML when discover_urls is true."},
			"discover_urls":    map[string]string{"type": "boolean", "description": "When true, locally discover URL candidates from text/html/sitemap XML before saving."},
			"base_url":         map[string]string{"type": "string", "description": "Optional public base URL for resolving relative links when discover_urls is true."},
			"same_domain_only": map[string]string{"type": "boolean", "description": "When true with discover_urls, keep only base domain and subdomain URLs."},
			"save_scope":       map[string]string{"type": "string", "description": "project | personal | local_only. Default project."},
			"topic_hint":       map[string]string{"type": "string", "description": "Optional topic hint to improve write-time structure"},
			"distill_mode": map[string]string{
				"type":        "string",
				"description": "Optional write-time structuring mode: auto, rules_only, llm_if_available. Default auto.",
			},
			"labels": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections to add to every saved URL."},
			"auto_labels": map[string]string{
				"type":        "boolean",
				"description": "When true, add local rule-based labels such as kind:url, domain:example.com, and scope:project. Default true.",
			},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeSaveURLs(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_discover_urls",
		Description: "Discover public HTTP(S) URL candidates from pasted text, HTML, sitemap XML, or mixed notes without fetching pages or writing the knowledge base. Use before knowledge_save_urls when the user provides a messy page/list and wants to save web information.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "url", "link", "discover", "sitemap", "bulk", "web", "知识库", "发现", "网址", "链接", "网页"},
		Priority:    5,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"text":             map[string]string{"type": "string", "description": "Pasted text, HTML, sitemap XML, or mixed URL list to parse locally"},
			"html":             map[string]string{"type": "string", "description": "Alias for text when passing HTML or sitemap XML."},
			"url_list":         map[string]string{"type": "string", "description": "Alias for text when passing a pasted URL list."},
			"link_list":        map[string]string{"type": "string", "description": "Alias for text when passing a pasted URL list."},
			"base_url":         map[string]string{"type": "string", "description": "Optional public base URL for resolving relative links"},
			"same_domain_only": map[string]string{"type": "boolean", "description": "When true, keep only base domain and subdomain URLs. Default false."},
			"limit":            map[string]string{"type": "integer", "description": "Max candidate URLs, default 100, max 1000"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeDiscoverURLs(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_save_text",
		Description: "Save explicit user-approved text, notes, current-topic conclusions, or workflow artifacts into MaClaw knowledge base. Only use when the user asks to save, remember, add to knowledge base, or persist a specific piece of text. Write-time LLM distillation may be used if configured; later recall is local SQLite/FTS and does not require LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "text", "note", "save", "conversation", "brain"},
		Priority:    5,
		Status:      RegToolAvailable,
		Required:    []string{"text"},
		InputSchema: map[string]interface{}{
			"text":       map[string]string{"type": "string", "description": "Text or note to save"},
			"title":      map[string]string{"type": "string", "description": "Optional source title"},
			"kind":       map[string]string{"type": "string", "description": "Optional kind: conversation, workflow_artifact, text. Default conversation."},
			"save_scope": map[string]string{"type": "string", "description": "project | personal | local_only. Default project."},
			"topic_hint": map[string]string{"type": "string", "description": "Optional topic hint to improve write-time structure"},
			"distill_mode": map[string]string{
				"type":        "string",
				"description": "Optional write-time structuring mode: auto, rules_only, llm_if_available. Default auto.",
			},
			"labels": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections to add during save."},
			"auto_labels": map[string]string{
				"type":        "boolean",
				"description": "When true, add local rule-based labels such as kind:conversation and scope:project. Default true.",
			},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeSaveText(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_import_directory",
		Description: "Scan or import a local directory of documents into MaClaw knowledge base or saved local corpus. Only use after the user explicitly provides or approves the directory. Supported: doc, docx, ppt, pptx, xls, xlsx, pdf, csv, markdown, txt. Knowledge imports use native fallback parsers by default; OfficeRead structured Markdown/images are an explicit opt-in, and legacy .ppt requires that opt-in. Action scan is dry-run; action import starts an async import by default.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "document", "import", "directory", "folder", "brain", "知识库", "导入", "目录", "文件夹", "文档", "外脑"},
		Priority:    10,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"root_path":     map[string]string{"type": "string", "description": "Directory containing documents"},
			"path":          map[string]string{"type": "string", "description": "Alias for root_path."},
			"dir":           map[string]string{"type": "string", "description": "Alias for root_path."},
			"directory":     map[string]string{"type": "string", "description": "Alias for root_path."},
			"folder":        map[string]string{"type": "string", "description": "Alias for root_path."},
			"root":          map[string]string{"type": "string", "description": "Alias for root_path."},
			"action":        map[string]interface{}{"type": "string", "enum": []string{"scan", "import"}, "description": "scan | import. Default import."},
			"save_scope":    map[string]string{"type": "string", "description": "project | personal | local_only. Default project."},
			"topic_hint":    map[string]string{"type": "string", "description": "Optional topic hint"},
			"distill_mode":  map[string]string{"type": "string", "description": "Optional write-time structuring mode: auto, rules_only, llm_if_available. Default auto."},
			"labels":        map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections to add to imported files."},
			"auto_labels":   map[string]string{"type": "boolean", "description": "When true, add local rule-based labels such as kind:pdf, folder:contracts, and scope:project. Default true."},
			"recursive":     map[string]string{"type": "boolean", "description": "Include subdirectories, default true"},
			"include_exts":  map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Extensions to include, e.g. .doc,.docx,.ppt,.pptx,.xls,.xlsx,.pdf,.md"},
			"exclude_globs": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Glob patterns to exclude, e.g. vendor/** or *.tmp"},
			"max_file_mb":   map[string]string{"type": "integer", "description": "Max file size in MB, default 100"},
			"start_async":   map[string]string{"type": "boolean", "description": "For import action, start async job. Default true."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeImportDirectory(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_import_files",
		Description: "Scan or import explicitly provided local document file paths into MaClaw knowledge base or saved local corpus. Use for importing files/documents/PDFs into the knowledge base / external brain. Only use after the user explicitly provides or approves the file paths. Supported: doc, docx, ppt, pptx, xls, xlsx, pdf, csv, markdown, txt. Knowledge imports use native fallback parsers by default; OfficeRead structured Markdown/images are an explicit opt-in, and legacy .ppt requires that opt-in.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "document", "import", "file", "files", "pdf", "brain", "知识库", "导入", "文件", "文档", "外脑"},
		Priority:    10,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"file_paths":    map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Explicit local document file paths to scan or import"},
			"paths":         map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Alias for file_paths."},
			"files":         map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Alias for file_paths."},
			"file_path":     map[string]string{"type": "string", "description": "Alias for a single file_paths item."},
			"path":          map[string]string{"type": "string", "description": "Alias for a single file_paths item."},
			"action":        map[string]interface{}{"type": "string", "enum": []string{"scan", "import"}, "description": "scan | import. Default import."},
			"save_scope":    map[string]string{"type": "string", "description": "project | personal | local_only. Default project."},
			"topic_hint":    map[string]string{"type": "string", "description": "Optional topic hint"},
			"distill_mode":  map[string]string{"type": "string", "description": "Optional write-time structuring mode: auto, rules_only, llm_if_available. Default auto."},
			"labels":        map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections to add to imported files."},
			"auto_labels":   map[string]string{"type": "boolean", "description": "When true, add local rule-based labels such as kind:pdf, folder:contracts, and scope:project. Default true."},
			"include_exts":  map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Extensions to include, e.g. .doc,.docx,.ppt,.pptx,.xls,.xlsx,.pdf,.md"},
			"exclude_globs": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Glob patterns to exclude, e.g. vendor/** or *.tmp"},
			"max_file_mb":   map[string]string{"type": "integer", "description": "Max file size in MB, default 100"},
			"start_async":   map[string]string{"type": "boolean", "description": "For import action, start async job. Default true."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeImportFiles(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_import_status",
		Description: "Check status for a knowledge_import_directory or knowledge_import_files async job.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "import", "status", "job"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"job_id": map[string]string{"type": "string", "description": "Job ID returned by knowledge_import_directory or knowledge_import_files"},
			"id":     knowledgeStringAliasSchema("Alias for job_id."),
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeImportStatus(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_stats",
		Description: "Return MaClaw knowledge base counters without calling an LLM. Use to inspect local external-brain health, source/node/card/fact totals, and import batch count.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "stats", "local", "brain", "diagnostics"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{},
		Source:      "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeStats(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_doctor",
		Description: "Run local MaClaw knowledge base diagnostics without calling an LLM. Returns health status, score, stats, and actionable findings for failed sources, pending imports, unsupported file types, stale sources, source graph fragmentation, isolated sources, and other maintenance signals.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "doctor", "diagnostics", "health", "local", "brain"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{},
		Source:      "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeDoctor(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_health",
		Description: "Return a compact MaClaw knowledge health overview without calling an LLM. Combines doctor score, source quality grades/signals, and recommended maintenance actions into one operational summary.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "health", "quality", "doctor", "diagnostics", "summary", "local"},
		Priority:    5,
		Status:      RegToolAvailable,
		InputSchema: mergeKnowledgeSchemas(knowledgeQualityFilterSchema("Max sources to inspect for quality summary, default 100, max 500. When explicit source_ids are provided, execution automatically raises the effective health limit to cover those sources, up to 5000."), map[string]interface{}{
			"include_plan": map[string]string{"type": "boolean", "description": "Include recommended maintenance actions. Default true."},
		}),
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeHealth(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_source_quality",
		Description: "Return a local per-source quality report for MaClaw knowledge without calling an LLM. Scores sources by parser/card/fact coverage, labels, status, trust, duplicate claims, and sensitive-content signals. Use to decide what to refresh, rebuild, label, suppress, disable, or keep.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "quality", "source", "diagnostics", "health", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"query":             map[string]string{"type": "string", "description": "Optional source keyword filter"},
			"kind":              map[string]string{"type": "string", "description": "Optional source kind filter"},
			"source_kinds":      map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source kind filters"},
			"status":            map[string]string{"type": "string", "description": "Optional source status filter"},
			"coverage_filter":   map[string]string{"type": "string", "description": "Optional coverage filter such as missing_nodes, missing_cards, missing_facts, missing_links, missing_labels, complete"},
			"labels":            map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional labels that sources must have"},
			"source_ids":        knowledgeSourceIDsSchema("Optional source IDs to score"),
			"ids":               knowledgeSourceIDsAliasSchema(),
			"quality_grade":     map[string]string{"type": "string", "description": "Optional quality grade filter: excellent, good, needs_attention, poor"},
			"quality_grades":    map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional quality grade filters"},
			"min_quality_score": map[string]string{"type": "integer", "description": "Optional minimum local quality score"},
			"max_quality_score": map[string]string{"type": "integer", "description": "Optional maximum local quality score"},
			"limit":             map[string]string{"type": "integer", "description": "Max sources to score, default 100, max 1000. When explicit source_ids are provided, execution automatically raises the effective scoring limit to cover those sources, up to 5000."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeSourceQuality(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_quality_maintenance_plan",
		Description: "Build a local no-write MaClaw knowledge quality maintenance plan from the source quality report. Returns ordered actions, affected source IDs, and recommended knowledge tools for sensitive isolation, refresh/reimport, derived rebuild, topic-link refresh, duplicate suppression, and label backfill. Does not call an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "quality", "plan", "maintenance", "diagnostics", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: knowledgeQualityFilterSchema("Max sources to inspect, default 100, max 500. When explicit source_ids are provided, execution automatically raises the effective planning limit to cover those sources, up to 5000."),
		Source:      "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeQualityMaintenancePlan(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_quality_maintenance_policies",
		Description: "List built-in MaClaw knowledge quality maintenance policy presets. Presets define safe action sets such as conservative local cleanup, balanced duplicate suppression, LLM-assisted storage enrichment, and strict sensitive-source isolation. Query remains local and does not require an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "quality", "policy", "maintenance", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{},
		Source:      "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeQualityMaintenancePolicies(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_execute_quality_maintenance_plan",
		Description: "Execute selected local MaClaw knowledge quality maintenance actions from the quality plan. Supports dry_run preview, sensitive-source disabling, missing-node source refresh/reimport, derived card/fact rebuild, topic-link refresh, auto-label backfill, and reversible duplicate suppression. Does not call an LLM unless distill_mode=llm_if_available is explicitly used for rebuild.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "quality", "plan", "execute", "maintenance", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: mergeKnowledgeSchemas(knowledgeQualityFilterSchema("Max sources to inspect, default 100, max 500. When explicit source_ids are provided, execution automatically raises the effective planning limit to cover those sources."), map[string]interface{}{
			"policy":                      map[string]string{"type": "string", "description": "Optional built-in policy preset: conservative, balanced, enriched, strict. Used when actions/distill/limits are omitted."},
			"actions":                     map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional action kinds to execute: disable_sensitive_sources, refresh_or_reimport_missing_nodes, rebuild_derived_gaps, refresh_topic_links, backfill_labels, suppress_duplicate_groups. Omit to execute all executable plan actions."},
			"dry_run":                     map[string]string{"type": "boolean", "description": "Preview actions without writing. Default true."},
			"distill_mode":                map[string]string{"type": "string", "description": "Optional rebuild structuring mode: auto, rules_only, llm_if_available. Default auto."},
			"max_sources_per_action":      map[string]string{"type": "integer", "description": "Safety limit per action. Default 100, max 5000. When explicit source_ids are provided, execution automatically raises the effective per-action limit to cover those sources."},
			"allow_sensitive_disable":     map[string]string{"type": "boolean", "description": "Required to actually disable sensitive sources when dry_run is false."},
			"allow_duplicate_suppression": map[string]string{"type": "boolean", "description": "Required to actually suppress duplicate card groups when dry_run is false."},
		}),
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeExecuteQualityMaintenancePlan(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_rebuild_quality_gaps",
		Description: "Inspect the local source quality report, select sources with missing_cards or missing_facts, then rebuild their derived cards/facts from already parsed document nodes. Does not refetch URLs, reread files, or require an LLM for selection.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "quality", "rebuild", "repair", "local", "automation"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"query":             map[string]string{"type": "string", "description": "Optional source keyword filter"},
			"kind":              map[string]string{"type": "string", "description": "Optional source kind filter"},
			"source_kinds":      map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source kind filters"},
			"status":            map[string]string{"type": "string", "description": "Optional source status filter"},
			"coverage_filter":   map[string]string{"type": "string", "description": "Optional coverage filter before quality scoring"},
			"labels":            map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional labels that sources must have"},
			"source_ids":        knowledgeSourceIDsSchema("Optional source IDs to inspect"),
			"ids":               knowledgeSourceIDsAliasSchema(),
			"quality_grade":     map[string]string{"type": "string", "description": "Optional quality grade filter: excellent, good, needs_attention, poor"},
			"quality_grades":    map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional quality grade filters"},
			"min_quality_score": map[string]string{"type": "integer", "description": "Optional minimum local quality score"},
			"max_quality_score": map[string]string{"type": "integer", "description": "Optional maximum local quality score"},
			"limit":             map[string]string{"type": "integer", "description": "Max sources to inspect, default 100, max 5000. When explicit source_ids are provided, execution automatically raises the effective inspection limit to cover those sources."},
			"distill_mode":      map[string]string{"type": "string", "description": "Optional structuring mode: auto, rules_only, llm_if_available. Default auto."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeRebuildQualityGaps(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_capabilities",
		Description: "Return MaClaw knowledge base supported input formats, parser status, default import extensions, and local search/storage capabilities. Does not call an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "capabilities", "format", "parser", "local", "brain"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{},
		Source:      "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeCapabilities(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_url_domain_policies",
		Description: "List or replace local URL domain allow/block policies for MaClaw knowledge URL saving. Does not call an LLM. Use action=list to inspect policies, action=replace to set allow_domains/block_domains, or action=check to test a URL before saving.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "url", "domain", "policy", "allowlist", "blocklist", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"action":        map[string]string{"type": "string", "description": "list | replace | check. Default list."},
			"url":           map[string]string{"type": "string", "description": "URL to check when action=check."},
			"allow_domains": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Domains allowed for URL saves. When non-empty, other domains are denied."},
			"block_domains": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Domains blocked for URL saves. Block rules take priority."},
			"reason":        map[string]string{"type": "string", "description": "Optional reason stored with policies."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeURLDomainPolicies(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_maintain",
		Description: "Run local knowledge database maintenance: SQLite integrity check, FTS optimize, WAL checkpoint, and optional VACUUM. Transient checkpoint/VACUUM lock conflicts are returned as warnings. Does not call an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "maintenance", "sqlite", "fts", "optimize", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"vacuum": map[string]string{"type": "boolean", "description": "Also run VACUUM after checkpointing. Default false."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeMaintain(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_export_snapshot",
		Description: "Export a local MaClaw knowledge snapshot as JSONL for backup, audit, or migration. Redacts sensitive patterns by default and does not call an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "export", "backup", "audit", "jsonl", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"output_path":      map[string]string{"type": "string", "description": "Optional local JSONL output path. Defaults to the MaClaw data directory."},
			"redact_sensitive": map[string]string{"type": "boolean", "description": "Redact sensitive patterns in exported text. Default true."},
			"source_ids":       knowledgeSourceIDsSchema("Optional source IDs to export. When set, only these sources and their nodes/cards/facts are included."),
			"ids":              knowledgeSourceIDsAliasSchema(),
			"query":            map[string]string{"type": "string", "description": "Optional source filter query used to select source IDs before export."},
			"kind":             map[string]string{"type": "string", "description": "Optional source kind filter used to select source IDs before export."},
			"source_kinds":     map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source kind list used to select source IDs before export."},
			"status":           map[string]string{"type": "string", "description": "Optional source status filter used to select source IDs before export."},
			"domain":           map[string]string{"type": "string", "description": "Optional URL domain filter used to select source IDs before export, e.g. example.com."},
			"coverage_filter":  map[string]string{"type": "string", "description": "Optional coverage filter used to select source IDs before export."},
			"project_path":     map[string]string{"type": "string", "description": "Optional project path filter used to select source IDs before export."},
			"owner_id":         map[string]string{"type": "string", "description": "Optional owner/user filter used to select source IDs before export."},
			"tenant_id":        map[string]string{"type": "string", "description": "Optional tenant filter used to select source IDs before export."},
			"limit":            map[string]string{"type": "integer", "description": "Max filtered sources to export, default 500, max 500."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeExportSnapshot(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_import_snapshot",
		Description: "Import or dry-run a MaClaw knowledge JSONL snapshot exported by knowledge_export_snapshot. Defaults to dry_run=true and overwrite=false to avoid changing existing knowledge unless explicitly requested.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "import", "restore", "migration", "jsonl", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"input_path":           map[string]string{"type": "string", "description": "Local JSONL snapshot path to import."},
			"path":                 knowledgeStringAliasSchema("Alias for input_path."),
			"dry_run":              map[string]string{"type": "boolean", "description": "Validate and count records without writing. Default true."},
			"overwrite":            map[string]string{"type": "boolean", "description": "Overwrite existing records with the same IDs. Default false."},
			"skip_safety_backup":   map[string]string{"type": "boolean", "description": "For real restore only, skip the automatic pre-restore local backup. Default false."},
			"safety_backup_path":   map[string]string{"type": "string", "description": "Optional output path for the automatic pre-restore backup."},
			"safety_backup_redact": map[string]string{"type": "boolean", "description": "Redact sensitive text in the automatic pre-restore backup. Default false so the backup can fully restore local state."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeImportSnapshot(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_share_to_hub",
		Description: "Share selected local Maclaw GUI knowledge sources to Hub as an editable knowledge package. Returns share_url, agent_import, content_sources, and warnings so agents can copy the link and verify import completeness. Requires a user-provided description; ttl defaults to 7d.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "hub", "share", "export", "link", "agent-import", "知识库", "分享", "导出"},
		Priority:    5,
		Status:      RegToolAvailable,
		Required:    []string{"description"},
		InputSchema: map[string]interface{}{
			"hub_url":          map[string]string{"type": "string", "description": "Optional Hub base URL. Defaults to configured RemoteHubURL."},
			"hub_token":        map[string]string{"type": "string", "description": "Optional Hub token. Defaults to configured RemoteViewerToken."},
			"title":            map[string]string{"type": "string", "description": "Optional knowledge share title."},
			"description":      map[string]string{"type": "string", "description": "Required user-facing knowledge description shown in Hub browsing/management pages."},
			"visibility_scope": map[string]string{"type": "string", "description": "Visibility scope: public, tenant, hub, private, or users. Default hub."},
			"visibility_users": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "User IDs/emails allowed when visibility_scope=users."},
			"ttl":              map[string]string{"type": "string", "description": "Share lifetime: 7d, month, year, permanent. Default 7d."},
			"source_ids":       knowledgeSourceIDsSchema("Optional exact source IDs to share. If omitted, shares all active sources selected by filters."),
			"ids":              knowledgeSourceIDsAliasSchema(),
			"query":            map[string]string{"type": "string", "description": "Optional source filter query used to select source IDs before sharing."},
			"kind":             map[string]string{"type": "string", "description": "Optional source kind filter used to select source IDs before sharing."},
			"source_kinds":     map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source kind list used to select source IDs before sharing."},
			"status":           map[string]string{"type": "string", "description": "Optional source status filter used to select source IDs before sharing."},
			"domain":           map[string]string{"type": "string", "description": "Optional URL domain filter used to select source IDs before sharing."},
			"coverage_filter":  map[string]string{"type": "string", "description": "Optional coverage filter used to select source IDs before sharing."},
			"project_path":     map[string]string{"type": "string", "description": "Optional project path filter used to select source IDs before sharing."},
			"owner_id":         map[string]string{"type": "string", "description": "Optional owner/user filter used to select source IDs before sharing."},
			"tenant_id":        map[string]string{"type": "string", "description": "Optional tenant filter used to select source IDs before sharing."},
			"limit":            map[string]string{"type": "integer", "description": "Max filtered sources to share, default 500, max 500."},
			"redact_sensitive": map[string]string{"type": "boolean", "description": "Redact sensitive URI/path fields and sensitive text snippets in the shared package. Default true."},
			"include_disabled": map[string]string{"type": "boolean", "description": "Include disabled sources when explicit source IDs or filters match. Default false."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeShareToHub(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_import_hub_share",
		Description: "Dry-run or import a Hub knowledge share by human share link, package link, agent import link, or knowledge_id. Defaults to dry_run=true so agents can inspect imported/skipped counts and warnings before writing local knowledge.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "hub", "share", "import", "restore", "link", "知识库", "导入", "分享"},
		Priority:    5,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"share_link":   map[string]string{"type": "string", "description": "Human share URL, package URL, or agent import URL."},
			"link":         knowledgeStringAliasSchema("Alias for share_link."),
			"url":          knowledgeStringAliasSchema("Alias for share_link."),
			"knowledge_id": map[string]string{"type": "string", "description": "Knowledge ID to resolve on the configured or supplied Hub."},
			"id":           knowledgeStringAliasSchema("Alias for knowledge_id."),
			"hub_url":      map[string]string{"type": "string", "description": "Optional Hub base URL. Required when only knowledge_id is provided and no default Hub is configured."},
			"hub_token":    map[string]string{"type": "string", "description": "Optional Hub token. Defaults to configured RemoteViewerToken."},
			"dry_run":      map[string]string{"type": "boolean", "description": "Preview importable/skipped items without writing. Default true."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeImportHubShare(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_list_sources",
		Description: "List saved MaClaw knowledge sources from the local store without calling an LLM. Use when the user asks what has been saved, imported, indexed, failed, grouped by label, or recently updated. Returns per-source node/card/fact counts and supports query, kind/source_kinds, status, domain, label/labels, coverage_filter, project_path, owner_id, tenant_id, and limit filters.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "list", "local", "brain"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"status":           map[string]string{"type": "string", "description": "Optional source status filter: parsed, distilled, failed, stale, disabled"},
			"include_disabled": map[string]string{"type": "boolean", "description": "Include disabled sources. Default false."},
			"domain":           map[string]string{"type": "string", "description": "Optional URL domain filter, e.g. example.com."},
			"label":            map[string]string{"type": "string", "description": "Optional single source label/collection filter."},
			"labels":           map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional labels/collections filter. Sources must have every supplied label."},
			"source_ids":       knowledgeSourceIDsSchema("Optional exact source IDs to list."),
			"ids":              knowledgeSourceIDsAliasSchema(),
			"coverage_filter": map[string]string{
				"type":        "string",
				"description": "Optional coverage filter: missing_nodes, missing_cards, missing_facts, missing_links, missing_labels, pdf_ocr_needed, complete, has_nodes, has_cards, has_facts, has_links",
			},
			"project_path": map[string]string{"type": "string", "description": "Optional project path filter"},
			"owner_id":     map[string]string{"type": "string", "description": "Optional owner/user filter"},
			"tenant_id":    map[string]string{"type": "string", "description": "Optional tenant filter"},
			"limit":        map[string]string{"type": "integer", "description": "Max sources, default 50, max 500"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeListSources(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_list_source_labels",
		Description: "List source labels/collections from the local MaClaw knowledge store, with counts and sample sources. Supports the same source filters as knowledge_list_sources and does not call an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "label", "collection", "list", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"query":           map[string]string{"type": "string", "description": "Optional source text filter"},
			"kind":            map[string]string{"type": "string", "description": "Optional source kind filter"},
			"source_kinds":    map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source kind filters"},
			"status":          map[string]string{"type": "string", "description": "Optional source status filter"},
			"domain":          map[string]string{"type": "string", "description": "Optional URL domain filter"},
			"label":           map[string]string{"type": "string", "description": "Optional label filter"},
			"labels":          map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional labels filter"},
			"source_ids":      knowledgeSourceIDsSchema("Optional exact source IDs to aggregate labels for."),
			"ids":             knowledgeSourceIDsAliasSchema(),
			"coverage_filter": map[string]string{"type": "string", "description": "Optional coverage filter"},
			"project_path":    map[string]string{"type": "string", "description": "Optional project path filter"},
			"owner_id":        map[string]string{"type": "string", "description": "Optional owner/user filter"},
			"tenant_id":       map[string]string{"type": "string", "description": "Optional tenant filter"},
			"limit":           map[string]string{"type": "integer", "description": "Max sources sampled for label aggregation, default 1000, max 5000"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeListSourceLabels(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_update_source_labels",
		Description: "Bulk add, remove, replace, rename, or clear labels for saved MaClaw knowledge sources. Can target explicit source_ids or the same filters used by knowledge_list_sources. If rename_from/rename_to is provided without filters, it targets sources with rename_from. Does not modify content and does not call an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "label", "collection", "bulk", "governance"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_ids":      knowledgeSourceIDsSchema("Optional explicit source IDs. If omitted, filters are used."),
			"ids":             knowledgeSourceIDsAliasSchema(),
			"add_labels":      map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Labels to add"},
			"remove_labels":   map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Labels to remove"},
			"replace_labels":  map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Replacement labels for each matched source"},
			"rename_from":     map[string]string{"type": "string", "description": "Existing label to rename/merge from"},
			"rename_to":       map[string]string{"type": "string", "description": "New label to rename/merge into"},
			"clear_labels":    map[string]string{"type": "boolean", "description": "Remove all labels from each matched source"},
			"dry_run":         map[string]string{"type": "boolean", "description": "Preview label changes without writing"},
			"query":           map[string]string{"type": "string", "description": "Optional source text filter"},
			"kind":            map[string]string{"type": "string", "description": "Optional source kind filter"},
			"source_kinds":    map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source kind filters"},
			"status":          map[string]string{"type": "string", "description": "Optional source status filter"},
			"domain":          map[string]string{"type": "string", "description": "Optional URL domain filter"},
			"label":           map[string]string{"type": "string", "description": "Optional existing label filter"},
			"labels":          map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional existing labels filter"},
			"coverage_filter": map[string]string{"type": "string", "description": "Optional coverage filter"},
			"project_path":    map[string]string{"type": "string", "description": "Optional project path filter"},
			"owner_id":        map[string]string{"type": "string", "description": "Optional owner/user filter"},
			"tenant_id":       map[string]string{"type": "string", "description": "Optional tenant filter"},
			"limit":           map[string]string{"type": "integer", "description": "Max sources to update, default 1000, max 5000"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeUpdateSourceLabels(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_backfill_source_auto_labels",
		Description: "Backfill local rule-based labels for existing MaClaw knowledge sources selected by source_ids or source filters. Adds missing kind/scope/domain/folder labels without removing manual labels and does not call an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "label", "auto-label", "bulk", "governance"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_ids":      knowledgeSourceIDsSchema("Optional explicit source IDs. If omitted, filters are used."),
			"ids":             knowledgeSourceIDsAliasSchema(),
			"dry_run":         map[string]string{"type": "boolean", "description": "Preview auto-label changes without writing. Default false."},
			"query":           map[string]string{"type": "string", "description": "Optional source text filter"},
			"kind":            map[string]string{"type": "string", "description": "Optional source kind filter"},
			"source_kinds":    map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source kind filters"},
			"status":          map[string]string{"type": "string", "description": "Optional source status filter"},
			"domain":          map[string]string{"type": "string", "description": "Optional URL domain filter"},
			"label":           map[string]string{"type": "string", "description": "Optional existing label filter"},
			"labels":          map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional existing labels filter"},
			"coverage_filter": map[string]string{"type": "string", "description": "Optional coverage filter, e.g. missing_labels"},
			"project_path":    map[string]string{"type": "string", "description": "Optional project path filter"},
			"owner_id":        map[string]string{"type": "string", "description": "Optional owner/user filter"},
			"tenant_id":       map[string]string{"type": "string", "description": "Optional tenant filter"},
			"limit":           map[string]string{"type": "integer", "description": "Max sources to backfill, default 1000, max 5000"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeBackfillSourceAutoLabels(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_backfill_quality_labels",
		Description: "Inspect the local source quality report, select sources with missing_labels, then backfill local rule-based labels for that quality slice. Does not modify content and does not call an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "quality", "label", "auto-label", "bulk", "governance"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"query":             map[string]string{"type": "string", "description": "Optional source keyword filter"},
			"kind":              map[string]string{"type": "string", "description": "Optional source kind filter"},
			"source_kinds":      map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source kind filters"},
			"status":            map[string]string{"type": "string", "description": "Optional source status filter"},
			"coverage_filter":   map[string]string{"type": "string", "description": "Optional coverage filter before quality scoring"},
			"labels":            map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional labels that sources must have"},
			"source_ids":        knowledgeSourceIDsSchema("Optional source IDs to inspect"),
			"ids":               knowledgeSourceIDsAliasSchema(),
			"quality_grade":     map[string]string{"type": "string", "description": "Optional quality grade filter: excellent, good, needs_attention, poor"},
			"quality_grades":    map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional quality grade filters"},
			"min_quality_score": map[string]string{"type": "integer", "description": "Optional minimum local quality score"},
			"max_quality_score": map[string]string{"type": "integer", "description": "Optional maximum local quality score"},
			"dry_run":           map[string]string{"type": "boolean", "description": "Preview auto-label changes without writing. Default false."},
			"limit":             map[string]string{"type": "integer", "description": "Max sources to inspect, default 100, max 5000. When explicit source_ids are provided, execution automatically raises the effective inspection limit to cover those sources."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeBackfillQualityLabels(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_disable_quality_sensitive_sources",
		Description: "Inspect the local source quality report, select sources with possible_sensitive_content, then disable those sources without deleting them. Selection is local and does not call an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "quality", "sensitive", "disable", "bulk", "governance"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: knowledgeQualityFilterSchema("Max sources to inspect, default 100, max 5000. When explicit source_ids are provided, execution automatically raises the effective inspection limit to cover those sources."),
		Source:      "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeDisableQualitySensitiveSources(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_suppress_quality_duplicate_groups",
		Description: "Inspect the local source quality report, select sources with duplicate_card_claims, then suppress duplicate card groups that touch those sources. Suppression is reversible and does not delete sources, nodes, or cards.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "quality", "duplicate", "suppress", "bulk", "governance"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: mergeKnowledgeSchemas(knowledgeQualityFilterSchema("Max sources to inspect, default 100, max 5000. When explicit source_ids are provided, execution automatically raises the effective inspection limit to cover those sources."), map[string]interface{}{
			"duplicate_limit": map[string]string{"type": "integer", "description": "Max duplicate groups to inspect, default 100, max 1000"},
			"reason":          map[string]string{"type": "string", "description": "Optional suppression reason. Default quality_duplicate_card_claim_bulk."},
		}),
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeSuppressQualityDuplicateGroups(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_list_import_batches",
		Description: "List recent MaClaw knowledge import batches from the local store without calling an LLM. Use when the user asks about recent directory/file imports, import history, or import success/failure totals.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "import", "batch", "list", "diagnostics"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"limit": map[string]string{"type": "integer", "description": "Max batches, default 50, max 200"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeListImportBatches(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_list_import_items",
		Description: "List files in a MaClaw knowledge import batch from the local store without calling an LLM. Use to inspect which files imported, skipped, duplicated, or failed after the user asks about a specific batch.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "import", "item", "file", "diagnostics"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"batch_id": map[string]string{"type": "string", "description": "Import batch ID"},
			"id":       knowledgeStringAliasSchema("Alias for batch_id."),
			"limit":    map[string]string{"type": "integer", "description": "Max items, default 200, max 1000"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeListImportItems(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_retry_import_batch",
		Description: "Retry failed or skipped files from an existing MaClaw knowledge import batch. Creates a new import batch for traceability and reuses source-path idempotency, so successful retries update existing sources instead of duplicating them.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "import", "retry", "batch", "document", "brain"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"batch_id":        map[string]string{"type": "string", "description": "Import batch ID to retry"},
			"id":              knowledgeStringAliasSchema("Alias for batch_id."),
			"item_ids":        map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional specific import item IDs to retry"},
			"statuses":        map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional statuses to retry. Default failed; include skipped types by setting include_skipped."},
			"include_skipped": map[string]string{"type": "boolean", "description": "Also retry skipped_unsupported_type, skipped_too_large, and skipped_duplicate items. Default false."},
			"include_exts":    map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional extension override for retry"},
			"max_file_mb":     map[string]string{"type": "integer", "description": "Optional max file size override in MB"},
			"topic_hint":      map[string]string{"type": "string", "description": "Optional topic hint override"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeRetryImportBatch(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_source_detail",
		Description: "Inspect parsed source nodes, distilled cards, facts, recent source versions, source digest, source timeline, topic-related source links, recent link audit events, and the focused local source-neighborhood graph for a specific MaClaw knowledge source without calling an LLM. Use when the user asks what knowledge was stored for a saved URL or imported document source.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "card", "fact", "version", "digest", "timeline", "detail", "graph", "neighborhood", "audit"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_id":            map[string]string{"type": "string", "description": "Knowledge source ID"},
			"id":                   knowledgeSourceIDAliasSchema(),
			"nodes_limit":          map[string]string{"type": "integer", "description": "Max source nodes, default 50, max 1000"},
			"cards_limit":          map[string]string{"type": "integer", "description": "Max cards, default 50, max 500"},
			"facts_limit":          map[string]string{"type": "integer", "description": "Max facts, default 100, max 1000"},
			"versions_limit":       map[string]string{"type": "integer", "description": "Max source versions, default 20, max 200"},
			"timeline_limit":       map[string]string{"type": "integer", "description": "Max source timeline events, default 50, max 500"},
			"links_limit":          map[string]string{"type": "integer", "description": "Max related source links, default 20, max 200"},
			"link_events_limit":    map[string]string{"type": "integer", "description": "Max source-link audit events, default 20, max 200"},
			"include_neighborhood": map[string]string{"type": "boolean", "description": "Include focused source-neighborhood graph. Default true."},
			"neighborhood_depth":   map[string]string{"type": "integer", "description": "Neighborhood link depth, default 2, max 3"},
			"neighborhood_limit":   map[string]string{"type": "integer", "description": "Max neighborhood graph nodes, default 40, max 200"},
			"edge_limit":           map[string]string{"type": "integer", "description": "Max neighborhood graph edges, default 200, max 2000"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeSourceDetail(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_list_source_links",
		Description: "List persisted topic-related source links for a MaClaw knowledge source from the local store without calling an LLM. Use to inspect how saved knowledge is connected to the current topic or nearby sources.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "links", "topic", "graph", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_id": map[string]string{"type": "string", "description": "Knowledge source ID"},
			"id":        knowledgeSourceIDAliasSchema(),
			"limit":     map[string]string{"type": "integer", "description": "Max links, default 20, max 200"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeListSourceLinks(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_source_graph",
		Description: "Return a local source graph for MaClaw knowledge from persisted topic-related links. Includes graph nodes, collapsed source-link edges, and isolates for a filtered source slice. Does not call an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "graph", "links", "topic", "diagnostics", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: mergeKnowledgeSchemas(knowledgeQualityFilterSchema("Max graph nodes, default 100, max 500"), map[string]interface{}{
			"edge_limit": map[string]string{"type": "integer", "description": "Max graph edges, default 500, max 2000"},
		}),
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeSourceGraph(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_source_neighborhood",
		Description: "Return a local focused source-neighborhood graph around one MaClaw knowledge source by walking persisted topic-related links. Includes nodes, edges, components, and isolates. Does not call an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "graph", "neighborhood", "links", "topic", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_id":  map[string]string{"type": "string", "description": "Focus knowledge source ID"},
			"id":         knowledgeSourceIDAliasSchema(),
			"depth":      map[string]string{"type": "integer", "description": "Link depth, default 1, max 3"},
			"limit":      map[string]string{"type": "integer", "description": "Max graph nodes, default 50, max 200"},
			"edge_limit": map[string]string{"type": "integer", "description": "Max graph edges, default 500, max 2000"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeSourceNeighborhood(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_source_path",
		Description: "Find the shortest local path between two MaClaw knowledge sources by walking persisted topic-related source links. Returns ordered path nodes and edge evidence without calling an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "graph", "path", "links", "topic", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"from_source_id":   map[string]string{"type": "string", "description": "Start knowledge source ID"},
			"source_id":        knowledgeStringAliasSchema("Alias for from_source_id."),
			"from":             knowledgeStringAliasSchema("Alias for from_source_id."),
			"to_source_id":     map[string]string{"type": "string", "description": "Target knowledge source ID"},
			"target_source_id": knowledgeStringAliasSchema("Alias for to_source_id."),
			"to":               knowledgeStringAliasSchema("Alias for to_source_id."),
			"max_depth":        map[string]string{"type": "integer", "description": "Max link depth, default 4, max 6"},
			"edge_limit":       map[string]string{"type": "integer", "description": "Max candidate edges per expansion, default 1000, max 5000"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeSourcePath(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_preview_topic_links",
		Description: "Preview candidate topic-related source links for one MaClaw knowledge source using local topic relevance, without writing graph edges and without calling an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "links", "topic", "graph", "preview", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_id": map[string]string{"type": "string", "description": "Knowledge source ID to preview links for"},
			"id":        knowledgeSourceIDAliasSchema(),
			"limit":     map[string]string{"type": "integer", "description": "Max candidate links, default 8, max 50"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgePreviewTopicLinks(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_link_sources",
		Description: "Persist a bidirectional topic-related link between two MaClaw knowledge sources in the local source graph. Use only when the user explicitly wants to connect the sources.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "links", "topic", "graph", "write", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_id":         map[string]string{"type": "string", "description": "Start knowledge source ID"},
			"id":                knowledgeSourceIDAliasSchema(),
			"from_source_id":    knowledgeStringAliasSchema("Alias for source_id."),
			"from":              knowledgeStringAliasSchema("Alias for source_id."),
			"related_source_id": map[string]string{"type": "string", "description": "Related knowledge source ID"},
			"to_source_id":      knowledgeStringAliasSchema("Alias for related_source_id."),
			"to":                knowledgeStringAliasSchema("Alias for related_source_id."),
			"relation":          map[string]string{"type": "string", "description": "Relation name, default topic_related"},
			"score":             map[string]string{"type": "number", "description": "Optional link score, default 1"},
			"terms":             knowledgeStringArrayAliasSchema("Optional matched terms"),
			"evidence":          knowledgeStringArrayAliasSchema("Optional evidence summaries"),
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeLinkSources(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_unlink_sources",
		Description: "Remove a bidirectional topic-related link between two MaClaw knowledge sources from the local source graph. Use only when the user explicitly wants to remove that source relationship.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "links", "topic", "graph", "delete", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_id":         map[string]string{"type": "string", "description": "Start knowledge source ID"},
			"id":                knowledgeSourceIDAliasSchema(),
			"from_source_id":    knowledgeStringAliasSchema("Alias for source_id."),
			"from":              knowledgeStringAliasSchema("Alias for source_id."),
			"related_source_id": map[string]string{"type": "string", "description": "Related knowledge source ID"},
			"to_source_id":      knowledgeStringAliasSchema("Alias for related_source_id."),
			"to":                knowledgeStringAliasSchema("Alias for related_source_id."),
			"relation":          map[string]string{"type": "string", "description": "Relation name, default topic_related"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeUnlinkSources(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_list_source_link_events",
		Description: "List recent manual source-link governance events for a MaClaw knowledge source, including link and unlink actions. Does not call an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "links", "audit", "graph", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_id": map[string]string{"type": "string", "description": "Knowledge source ID"},
			"id":        knowledgeSourceIDAliasSchema(),
			"limit":     map[string]string{"type": "integer", "description": "Max events, default 20, max 200"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeListSourceLinkEvents(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_source_timeline",
		Description: "Return a local chronological timeline for one MaClaw knowledge source by merging source lifecycle, saved/refreshed versions, and source-link audit events. Does not call an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "timeline", "version", "audit", "history", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_id": map[string]string{"type": "string", "description": "Knowledge source ID"},
			"id":        knowledgeSourceIDAliasSchema(),
			"limit":     map[string]string{"type": "integer", "description": "Max timeline events, default 50, max 500"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeSourceTimeline(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_source_digest",
		Description: "Return a compact local digest for one MaClaw knowledge source, including source metadata, labels, top parsed nodes, cards, facts, related source links, and timeline events. Does not call an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "digest", "summary", "card", "fact", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_id":   map[string]string{"type": "string", "description": "Knowledge source ID"},
			"id":          knowledgeSourceIDAliasSchema(),
			"nodes_limit": map[string]string{"type": "integer", "description": "Max source nodes, default 8, max 50"},
			"cards_limit": map[string]string{"type": "integer", "description": "Max cards, default 8, max 100"},
			"facts_limit": map[string]string{"type": "integer", "description": "Max facts, default 12, max 200"},
			"links_limit": map[string]string{"type": "integer", "description": "Max source links, default 8, max 100"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeSourceDigest(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_refresh_topic_links",
		Description: "Build or refresh persisted topic-related source links using local topic relevance. Does not call an LLM. Use after large imports or metadata edits to connect sources into a navigable external-brain graph.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "links", "topic", "graph", "maintenance", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: mergeKnowledgeSchemas(knowledgeQualityFilterSchema("Max sources to link when source_id is omitted, default 100, max 500"), map[string]interface{}{
			"source_id":        map[string]string{"type": "string", "description": "Optional single source ID to refresh. If omitted, the current source filter is used."},
			"id":               knowledgeSourceIDAliasSchema(),
			"limit_per_source": map[string]string{"type": "integer", "description": "Max links per source, default 8, max 50."},
		}),
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeRefreshTopicLinks(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_list_source_versions",
		Description: "List recent stored versions for a MaClaw knowledge source from the local store without calling an LLM. Use to audit when a URL, file, or text source was saved/refreshed and how many nodes/cards/facts were produced.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "version", "history", "audit"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_id": map[string]string{"type": "string", "description": "Knowledge source ID"},
			"id":        knowledgeSourceIDAliasSchema(),
			"limit":     map[string]string{"type": "integer", "description": "Max versions, default 20, max 200"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeListSourceVersions(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_list_duplicate_cards",
		Description: "List repeated knowledge-card claims from the local MaClaw knowledge base without calling an LLM. Use when the user asks about duplicate knowledge, import quality, repeated conclusions, or Doctor reports duplicate_card_claims.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "quality", "duplicate", "card", "diagnostics"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"limit": map[string]string{"type": "integer", "description": "Max duplicate groups, default 50, max 1000"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeListDuplicateCards(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_suppress_duplicate_cards",
		Description: "Suppress repeated knowledge cards from local recall without deleting sources or original content. Use when the user asks to resolve duplicate_card_claims or hide duplicate conclusions. Keeps the best card unless keep_card_id is provided; reversible with knowledge_restore_suppressed_cards.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "quality", "duplicate", "suppress", "card"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"key":          map[string]string{"type": "string", "description": "Duplicate group key or raw repeated claim"},
			"claim":        knowledgeStringAliasSchema("Alias for key when passing a raw repeated claim."),
			"keep_card_id": map[string]string{"type": "string", "description": "Optional card ID to keep active"},
			"project_path": map[string]string{"type": "string", "description": "Optional project scope filter"},
			"owner_id":     map[string]string{"type": "string", "description": "Optional owner scope filter"},
			"tenant_id":    map[string]string{"type": "string", "description": "Optional tenant scope filter"},
			"reason":       map[string]string{"type": "string", "description": "Optional suppression reason"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeSuppressDuplicateCards(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_suppress_duplicate_groups",
		Description: "Bulk suppress currently detected duplicate knowledge-card groups from local recall without deleting sources or original content. Use when the user asks to resolve duplicate_card_claims in bulk after inspecting duplicates. Keeps the best card in each group and is reversible with knowledge_restore_suppressed_cards.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "quality", "duplicate", "bulk", "suppress", "card"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"limit":        map[string]string{"type": "integer", "description": "Max duplicate groups to suppress, default 50, max 1000"},
			"project_path": map[string]string{"type": "string", "description": "Optional project scope filter"},
			"owner_id":     map[string]string{"type": "string", "description": "Optional owner scope filter"},
			"tenant_id":    map[string]string{"type": "string", "description": "Optional tenant scope filter"},
			"reason":       map[string]string{"type": "string", "description": "Optional suppression reason"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeSuppressDuplicateGroups(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_list_suppressed_cards",
		Description: "List knowledge cards suppressed from local recall. Suppressed cards are not deleted and can be restored.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "quality", "suppressed", "card"},
		Priority:    3,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"limit": map[string]string{"type": "integer", "description": "Max suppressed cards, default 100, max 1000"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeListSuppressedCards(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_scan_sensitive",
		Description: "Run a local redacted scan for possible secrets, tokens, passwords, private keys, and other sensitive content stored in MaClaw knowledge. Does not call an LLM and does not reveal full secret values.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "security", "sensitive", "secrets", "diagnostics", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"limit": map[string]string{"type": "integer", "description": "Max redacted findings, default 100, max 1000"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeScanSensitive(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_disable_sensitive_sources",
		Description: "Disable all currently enabled knowledge sources with redacted sensitive-content findings. This excludes affected sources from local recall without deleting data. Use after knowledge_scan_sensitive or knowledge_doctor reports possible_sensitive_content.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "security", "sensitive", "secrets", "disable", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"limit": map[string]string{"type": "integer", "description": "Max redacted findings to scan before disabling unique affected sources, default 100, max 1000"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeDisableSensitiveSources(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_restore_suppressed_cards",
		Description: "Restore previously suppressed knowledge cards so they participate in local recall again.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "quality", "restore", "card"},
		Priority:    3,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"card_ids": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Card IDs to restore"},
			"ids":      knowledgeStringArrayAliasSchema("Alias for card_ids."),
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeRestoreSuppressedCards(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_restore_suppressed_cards_bulk",
		Description: "Bulk restore suppressed knowledge cards so they participate in local recall again. Use when the user asks to undo duplicate-card suppression in bulk.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "quality", "restore", "bulk", "card"},
		Priority:    3,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"limit":           map[string]string{"type": "integer", "description": "Max suppressed cards to restore, default 100, max 1000"},
			"reason_contains": map[string]string{"type": "string", "description": "Optional substring filter against suppression reason"},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeRestoreSuppressedCardsBulk(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_update_source_metadata",
		Description: "Update editable metadata for a saved MaClaw knowledge source: title, topic_hint, source_trust, and labels. Use labels to group sources into local collections. Does not modify original content and does not call an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "metadata", "trust", "topic", "label", "brain"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_id":    map[string]string{"type": "string", "description": "Knowledge source ID"},
			"id":           knowledgeSourceIDAliasSchema(),
			"title":        map[string]string{"type": "string", "description": "New display title"},
			"topic_hint":   map[string]string{"type": "string", "description": "New topic hint used by local re-ranking"},
			"source_trust": map[string]string{"type": "number", "description": "Trust weight from 0 to 1"},
			"labels":       map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Replacement source labels/collections. Omit to keep existing labels."},
			"clear_labels": map[string]string{"type": "boolean", "description": "Set true to remove all labels from the source."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeUpdateSourceMetadata(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_refresh_source",
		Description: "Refresh an existing URL/HTML or imported local document knowledge source by re-fetching/re-reading it and rebuilding local nodes/cards/facts. Only use when the user explicitly asks to refresh a specific saved source.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "refresh", "url", "document", "brain"},
		Priority:    3,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_id": map[string]string{"type": "string", "description": "Knowledge source ID to refresh. Must be an existing URL/HTML or imported local document source."},
			"id":        knowledgeSourceIDAliasSchema(),
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeRefreshSource(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_preview_source_refresh",
		Description: "Preview whether an existing URL/HTML or imported local document source has changed before refreshing. This is read-only: it re-fetches/re-reads the source, compares local hashes and parsed nodes, and does not rebuild or call an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "preview", "refresh", "diff", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_id": map[string]string{"type": "string", "description": "Knowledge source ID to preview."},
			"id":        knowledgeSourceIDAliasSchema(),
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgePreviewSourceRefresh(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_preview_sources_refresh",
		Description: "Preview refresh changes for multiple saved URL/HTML or local document sources by source ID. This is read-only and does not rebuild or call an LLM.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "preview", "refresh", "bulk", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_ids": knowledgeSourceIDsSchema("Knowledge source IDs to preview."),
			"ids":        knowledgeSourceIDsAliasSchema(),
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgePreviewSourcesRefresh(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_preview_sources_refresh_by_filter",
		Description: "Preview refresh changes for saved sources selected by local source filters. This is read-only and useful before refreshing a large filtered set.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "preview", "refresh", "filter", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"query":           map[string]string{"type": "string", "description": "Optional source title/path/url/topic query filter"},
			"kind":            map[string]string{"type": "string", "description": "Optional source kind filter"},
			"source_kinds":    map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source kind list"},
			"status":          map[string]string{"type": "string", "description": "Optional source status filter"},
			"domain":          map[string]string{"type": "string", "description": "Optional URL domain filter, e.g. example.com."},
			"label":           map[string]string{"type": "string", "description": "Optional single source label/collection filter."},
			"labels":          map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections. Sources must have every supplied label."},
			"source_ids":      knowledgeSourceIDsSchema("Optional exact source IDs to preview."),
			"ids":             knowledgeSourceIDsAliasSchema(),
			"coverage_filter": map[string]string{"type": "string", "description": "Optional coverage filter: missing_nodes, missing_cards, missing_facts, missing_links, missing_labels, pdf_ocr_needed, complete, has_nodes, has_cards, has_facts, has_links"},
			"project_path":    map[string]string{"type": "string", "description": "Optional project path filter"},
			"owner_id":        map[string]string{"type": "string", "description": "Optional owner filter"},
			"tenant_id":       map[string]string{"type": "string", "description": "Optional tenant filter"},
			"limit":           map[string]string{"type": "integer", "description": "Max sources to preview, default 100, max 500. When explicit source_ids are provided, execution automatically raises the effective filter limit to cover those sources, up to 5000."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgePreviewSourcesRefreshByFilter(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_refresh_changed_sources",
		Description: "Preview multiple saved sources and refresh only the ones whose content or parsed nodes changed. This avoids rebuilding unchanged local knowledge. Does not use an LLM during preview; refresh may use configured write-time distillation mode defaults.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "refresh", "changed", "bulk", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_ids": knowledgeSourceIDsSchema("Knowledge source IDs to preview and refresh if changed."),
			"ids":        knowledgeSourceIDsAliasSchema(),
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeRefreshChangedSources(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_refresh_changed_sources_by_filter",
		Description: "Preview saved sources selected by local filters and refresh only sources that actually changed. Use for efficient periodic or bulk maintenance of large URL/document knowledge sets.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "refresh", "changed", "filter", "local"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"query":           map[string]string{"type": "string", "description": "Optional source title/path/url/topic query filter"},
			"kind":            map[string]string{"type": "string", "description": "Optional source kind filter"},
			"source_kinds":    map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source kind list"},
			"status":          map[string]string{"type": "string", "description": "Optional source status filter"},
			"domain":          map[string]string{"type": "string", "description": "Optional URL domain filter, e.g. example.com."},
			"label":           map[string]string{"type": "string", "description": "Optional single source label/collection filter."},
			"labels":          map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections. Sources must have every supplied label."},
			"source_ids":      knowledgeSourceIDsSchema("Optional exact source IDs to preview and refresh if changed."),
			"ids":             knowledgeSourceIDsAliasSchema(),
			"coverage_filter": map[string]string{"type": "string", "description": "Optional coverage filter: missing_nodes, missing_cards, missing_facts, missing_links, missing_labels, pdf_ocr_needed, complete, has_nodes, has_cards, has_facts, has_links"},
			"project_path":    map[string]string{"type": "string", "description": "Optional project path filter"},
			"owner_id":        map[string]string{"type": "string", "description": "Optional owner filter"},
			"tenant_id":       map[string]string{"type": "string", "description": "Optional tenant filter"},
			"limit":           map[string]string{"type": "integer", "description": "Max sources to preview and refresh, default 100, max 500. When explicit source_ids are provided, execution automatically raises the effective filter limit to cover those sources, up to 5000."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeRefreshChangedSourcesByFilter(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_refresh_sources",
		Description: "Refresh multiple existing URL/HTML or imported local document knowledge sources by source ID, rebuilding local nodes/cards/facts. Useful after knowledge_doctor reports changed_local_files with source_ids.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "refresh", "bulk", "document", "brain"},
		Priority:    3,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_ids": knowledgeSourceIDsSchema("Knowledge source IDs to refresh."),
			"ids":        knowledgeSourceIDsAliasSchema(),
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeRefreshSources(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_refresh_sources_by_filter",
		Description: "Refresh saved URL/HTML or imported local document knowledge sources selected by local source filters, rebuilding nodes/cards/facts. Use when the user asks to refresh a filtered group such as stale sources, missing_cards, changed documents, a kind, or a query match.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "refresh", "bulk", "filter", "brain"},
		Priority:    3,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"query":           map[string]string{"type": "string", "description": "Optional source title/path/url/topic query filter"},
			"kind":            map[string]string{"type": "string", "description": "Optional source kind filter"},
			"source_kinds":    map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source kind list"},
			"status":          map[string]string{"type": "string", "description": "Optional source status filter"},
			"domain":          map[string]string{"type": "string", "description": "Optional URL domain filter, e.g. example.com."},
			"label":           map[string]string{"type": "string", "description": "Optional single source label/collection filter."},
			"labels":          map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections. Sources must have every supplied label."},
			"source_ids":      knowledgeSourceIDsSchema("Optional exact source IDs to refresh."),
			"ids":             knowledgeSourceIDsAliasSchema(),
			"coverage_filter": map[string]string{"type": "string", "description": "Optional coverage filter: missing_nodes, missing_cards, missing_facts, missing_links, missing_labels, pdf_ocr_needed, complete, has_nodes, has_cards, has_facts, has_links"},
			"project_path":    map[string]string{"type": "string", "description": "Optional project path filter"},
			"owner_id":        map[string]string{"type": "string", "description": "Optional owner filter"},
			"tenant_id":       map[string]string{"type": "string", "description": "Optional tenant filter"},
			"limit":           map[string]string{"type": "integer", "description": "Max sources to refresh, default 100, max 500. When explicit source_ids are provided, execution automatically raises the effective filter limit to cover those sources, up to 5000."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeRefreshSourcesByFilter(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_rebuild_source_derived",
		Description: "Rebuild distilled cards and facts for one existing knowledge source from its already parsed local document nodes. Does not refetch URLs, reread files, or change source nodes. Use to repair missing_cards/missing_facts or rerun write-time structuring after distillation improvements.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "rebuild", "card", "fact", "repair"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_id":    map[string]string{"type": "string", "description": "Knowledge source ID to rebuild from existing nodes."},
			"id":           knowledgeSourceIDAliasSchema(),
			"distill_mode": map[string]string{"type": "string", "description": "Optional structuring mode: auto, rules_only, llm_if_available. Default auto."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeRebuildSourceDerived(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_rebuild_sources_derived",
		Description: "Rebuild distilled cards and facts for multiple existing knowledge sources from their already parsed local document nodes. Does not refetch URLs or reread files.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "rebuild", "bulk", "repair"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_ids":   knowledgeSourceIDsSchema("Knowledge source IDs to rebuild."),
			"ids":          knowledgeSourceIDsAliasSchema(),
			"distill_mode": map[string]string{"type": "string", "description": "Optional structuring mode: auto, rules_only, llm_if_available. Default auto."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeRebuildSourcesDerived(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_rebuild_sources_derived_by_filter",
		Description: "Rebuild cards and facts from existing parsed nodes for sources selected by local filters. Useful for repairing Doctor findings such as sources_without_cards or sources_without_facts without refetching or rereading originals.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "rebuild", "bulk", "filter", "repair"},
		Priority:    4,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"query":           map[string]string{"type": "string", "description": "Optional source title/path/url/topic query filter"},
			"kind":            map[string]string{"type": "string", "description": "Optional source kind filter"},
			"source_kinds":    map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source kind list"},
			"status":          map[string]string{"type": "string", "description": "Optional source status filter"},
			"domain":          map[string]string{"type": "string", "description": "Optional URL domain filter, e.g. example.com."},
			"label":           map[string]string{"type": "string", "description": "Optional single source label/collection filter."},
			"labels":          map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections. Sources must have every supplied label."},
			"source_ids":      knowledgeSourceIDsSchema("Optional exact source IDs to rebuild."),
			"ids":             knowledgeSourceIDsAliasSchema(),
			"coverage_filter": map[string]string{"type": "string", "description": "Optional coverage filter: missing_cards, missing_facts, has_nodes, complete, etc."},
			"project_path":    map[string]string{"type": "string", "description": "Optional project path filter"},
			"owner_id":        map[string]string{"type": "string", "description": "Optional owner filter"},
			"tenant_id":       map[string]string{"type": "string", "description": "Optional tenant filter"},
			"limit":           map[string]string{"type": "integer", "description": "Max sources to rebuild, default 100, max 500. When explicit source_ids are provided, execution automatically raises the effective filter limit to cover those sources, up to 5000."},
			"distill_mode":    map[string]string{"type": "string", "description": "Optional structuring mode: auto, rules_only, llm_if_available. Default auto."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeRebuildSourcesDerivedByFilter(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_disable_source",
		Description: "Disable a knowledge source without deleting it. Disabled sources stay stored but are excluded from default local search. Only use when the user explicitly asks to disable, hide, pause, or exclude a specific source ID.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "disable", "brain"},
		Priority:    3,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_id": map[string]string{"type": "string", "description": "Knowledge source ID to disable."},
			"id":        knowledgeSourceIDAliasSchema(),
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeDisableSource(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_disable_sources_by_filter",
		Description: "Disable multiple knowledge sources selected by local source filters without deleting them. Disabled sources stay stored but are excluded from default local search. Only use when the user explicitly asks to disable, hide, pause, or exclude a filtered group.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "disable", "bulk", "filter", "brain"},
		Priority:    3,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"query":           map[string]string{"type": "string", "description": "Optional source title/path/url/topic query filter"},
			"kind":            map[string]string{"type": "string", "description": "Optional source kind filter"},
			"source_kinds":    map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source kind list"},
			"status":          map[string]string{"type": "string", "description": "Optional source status filter"},
			"domain":          map[string]string{"type": "string", "description": "Optional URL domain filter, e.g. example.com."},
			"label":           map[string]string{"type": "string", "description": "Optional single source label/collection filter."},
			"labels":          map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections. Sources must have every supplied label."},
			"source_ids":      knowledgeSourceIDsSchema("Optional exact source IDs to disable."),
			"ids":             knowledgeSourceIDsAliasSchema(),
			"coverage_filter": map[string]string{"type": "string", "description": "Optional coverage filter: missing_nodes, missing_cards, missing_facts, missing_links, missing_labels, pdf_ocr_needed, complete, has_nodes, has_cards, has_facts, has_links"},
			"project_path":    map[string]string{"type": "string", "description": "Optional project path filter"},
			"owner_id":        map[string]string{"type": "string", "description": "Optional owner filter"},
			"tenant_id":       map[string]string{"type": "string", "description": "Optional tenant filter"},
			"limit":           map[string]string{"type": "integer", "description": "Max sources to disable, default 100, max 500. When explicit source_ids are provided, execution automatically raises the effective filter limit to cover those sources, up to 5000."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeDisableSourcesByFilter(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_enable_source",
		Description: "Re-enable a disabled knowledge source so it participates in default local search again. Only use when the user explicitly asks to enable, restore, or include a specific source ID.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "enable", "brain"},
		Priority:    3,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_id": map[string]string{"type": "string", "description": "Knowledge source ID to enable."},
			"id":        knowledgeSourceIDAliasSchema(),
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeEnableSource(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_enable_sources_by_filter",
		Description: "Re-enable multiple disabled knowledge sources selected by local source filters. Only use when the user explicitly asks to enable, restore, or include a filtered group.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "enable", "bulk", "filter", "brain"},
		Priority:    3,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"query":           map[string]string{"type": "string", "description": "Optional source title/path/url/topic query filter"},
			"kind":            map[string]string{"type": "string", "description": "Optional source kind filter"},
			"source_kinds":    map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source kind list"},
			"status":          map[string]string{"type": "string", "description": "Optional source status filter. Defaults can include disabled sources when passed as disabled."},
			"domain":          map[string]string{"type": "string", "description": "Optional URL domain filter, e.g. example.com."},
			"label":           map[string]string{"type": "string", "description": "Optional single source label/collection filter."},
			"labels":          map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source labels/collections. Sources must have every supplied label."},
			"source_ids":      knowledgeSourceIDsSchema("Optional exact source IDs to enable."),
			"ids":             knowledgeSourceIDsAliasSchema(),
			"coverage_filter": map[string]string{"type": "string", "description": "Optional coverage filter: missing_nodes, missing_cards, missing_facts, missing_links, missing_labels, pdf_ocr_needed, complete, has_nodes, has_cards, has_facts, has_links"},
			"project_path":    map[string]string{"type": "string", "description": "Optional project path filter"},
			"owner_id":        map[string]string{"type": "string", "description": "Optional owner filter"},
			"tenant_id":       map[string]string{"type": "string", "description": "Optional tenant filter"},
			"limit":           map[string]string{"type": "integer", "description": "Max sources to enable, default 100, max 500. When explicit source_ids are provided, execution automatically raises the effective filter limit to cover those sources, up to 5000."},
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeEnableSourcesByFilter(args)
		},
	})
	registry.Register(RegisteredTool{
		Name:        "knowledge_delete_source",
		Description: "Delete a knowledge source and all derived local nodes/cards/facts. This is destructive; only use when the user explicitly asks to delete a specific source ID.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"knowledge", "source", "delete", "brain"},
		Priority:    2,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"source_id": map[string]string{"type": "string", "description": "Knowledge source ID to delete."},
			"id":        knowledgeSourceIDAliasSchema(),
		},
		Source: "builtin:knowledge",
		Handler: func(args map[string]interface{}) string {
			return app.toolKnowledgeDeleteSource(args)
		},
	})

}

func (a *App) toolKnowledgeSearch(args map[string]interface{}) string {
	query := knowledgeToolStringArg(args, "query")
	if query == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing query argument"))
	}
	limit := knowledgeToolIntArg(args, "limit", 8)
	if limit <= 0 {
		limit = 8
	}
	if limit > 50 {
		limit = 50
	}
	opts := knowledge.SearchOptions{
		Query:           query,
		SearchScope:     knowledgeToolStringArg(args, "search_scope"),
		ProjectPath:     knowledgeToolStringArg(args, "project_path"),
		TopicHint:       knowledgeToolStringArg(args, "topic_hint"),
		ContextTerms:    knowledgeToolStringSlice(args["context_terms"]),
		ResultTypes:     knowledgeToolStringSlice(args["result_types"]),
		SourceKinds:     knowledgeToolStringSlice(args["source_kinds"]),
		SourceIDs:       knowledgeToolSourceIDs(args),
		Labels:          knowledgeToolStringSlice(args["labels"]),
		Domain:          knowledgeToolStringArg(args, "domain"),
		Limit:           limit,
		IncludeDisabled: knowledgeToolBoolArg(args, "include_disabled", false),
	}
	results, err := a.KnowledgeSearch(opts)
	results = knowledge.ProjectImageSearchResultsForTool(results)

	// General knowledge search may incidentally return image nodes, but it must
	// not inject image bytes into the model context. Use knowledge_image_search
	// when the user explicitly asks to find or display image evidence.
	response := map[string]interface{}{"query": query, "count": len(results), "results": results}
	if len(results) == 0 && err == nil {
		response["guidance"] = knowledge.EmptySearchResultMessage
	} else if len(results) > 0 {
		response["guidance"] = knowledge.SearchResultsHeader
	}
	return knowledgeToolJSON(response, err)
}

// toolKnowledgeImageSearch is the GUI agent's dedicated text-to-image route.
// Keeping it separate from knowledge_search ensures unrelated cards and text
// nodes cannot crowd image evidence out of the result window.
func (a *App) toolKnowledgeImageSearch(args map[string]interface{}) string {
	query := knowledgeToolStringArg(args, "query")
	if query == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing query argument"))
	}
	limit := knowledgeToolIntArg(args, "limit", 8)
	if limit <= 0 {
		limit = 8
	}
	if limit > 50 {
		limit = 50
	}
	results, err := a.KnowledgeSearchImages(knowledge.ImageSearchOptions{SearchOptions: knowledge.SearchOptions{
		Query:           query,
		SearchScope:     knowledgeToolStringArg(args, "search_scope"),
		ProjectPath:     knowledgeToolStringArg(args, "project_path"),
		TopicHint:       knowledgeToolStringArg(args, "topic_hint"),
		ContextTerms:    knowledgeToolStringSlice(args["context_terms"]),
		SourceKinds:     knowledgeToolStringSlice(args["source_kinds"]),
		SourceIDs:       knowledgeToolSourceIDs(args),
		Labels:          knowledgeToolStringSlice(args["labels"]),
		Domain:          knowledgeToolStringArg(args, "domain"),
		Limit:           limit,
		IncludeDisabled: knowledgeToolBoolArg(args, "include_disabled", false),
	}})
	enrichedResults := a.enrichKnowledgeImageResults(results)
	response := map[string]interface{}{"query": query, "count": len(enrichedResults), "results": enrichedResults, "mode": "text_to_image"}
	if len(enrichedResults) == 0 && err == nil {
		response["guidance"] = knowledge.EmptySearchResultMessage
	} else if len(enrichedResults) > 0 {
		response["guidance"] = knowledge.SearchResultsHeader
	}
	return knowledgeToolJSON(response, err)
}

// enrichKnowledgeImageResults converts image search hits to the narrow display
// contract used by agents. Source is intentionally projected instead of
// serialized wholesale: imported images can retain absolute local paths in URI,
// CanonicalURI, RelativePath, ProjectPath, or ErrorMessage. Those fields are
// useful to the importer but must never enter a model/tool payload.
func (a *App) enrichKnowledgeImageResults(results []knowledge.SearchResult) []interface{} {
	assetBaseDir := filepath.Join(a.GetDataDir(), "knowledge_assets")
	enriched := make([]interface{}, 0, len(results))
	for _, r := range results {
		if r.NodeType != knowledge.NodeTypeImage && r.Source.Kind != knowledge.SourceKindImage {
			// This endpoint is image-only. Never surface an unexpected general
			// search hit, because that raw result has a broader source contract.
			continue
		}

		// Always use the safe projection, including when the binary asset or its
		// thumbnail is unavailable. Falling back to r here would reintroduce
		// host-path leakage on a common degraded-path response.
		item := map[string]interface{}{
			"source": map[string]interface{}{
				"id":           r.Source.ID,
				"kind":         r.Source.Kind,
				"display_name": a.knowledgeImageDisplayName(r),
			},
			"result_type": r.ResultType,
			"node_id":     r.NodeID,
			"node_title":  a.knowledgeImageDisplayText(r.NodeTitle, "image evidence"),
			"node_type":   r.NodeType,
			"page":        r.Page,
			"sheet_name":  r.SheetName,
			"row_range":   r.RowRange,
			"col_range":   r.ColRange,
			"card_id":     r.CardID,
			"card_title":  a.knowledgeImageDisplayText(r.CardTitle, ""),
			"claim":       r.Claim,
			"summary":     r.Summary,
			"snippet":     r.Snippet,
			"citation":    knowledge.FormatImageCitationLabel(r),
			"score":       r.Score,
		}
		if embed := knowledge.EmbedImageThumbForSearchResult(r, assetBaseDir); embed != nil {
			item["media"] = map[string]interface{}{
				"asset_id":           embed.AssetID,
				"thumbnail_data_url": embed.DataURL,
				"alt":                a.knowledgeImageDisplayText(r.NodeTitle, "image evidence"),
			}
			item["display_marker"] = knowledge.FormatKBImageMarker(&knowledge.SearchResultImageEmbed{
				AssetID: embed.AssetID,
				DataURL: embed.DataURL,
			})
		}
		enriched = append(enriched, item)
	}
	return enriched
}

func (a *App) knowledgeImageDisplayName(r knowledge.SearchResult) string {
	return a.knowledgeImageDisplayText(r.Source.Title, "knowledge image")
}

func (a *App) knowledgeImageDisplayText(value, fallback string) string {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if value == "" || filepath.IsAbs(value) || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "\\") || isWindowsAbsoluteKnowledgePath(value) || strings.HasPrefix(lower, "file://") {
		return fallback
	}
	dataDir := filepath.Clean(a.GetDataDir())
	if dataDir != "." && strings.Contains(filepath.Clean(value), dataDir) {
		return fallback
	}
	return value
}

func isWindowsAbsoluteKnowledgePath(value string) bool {
	return strings.HasPrefix(value, `\\`) || strings.HasPrefix(value, `//`) ||
		(len(value) >= 3 && ((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')) && value[1] == ':' && (value[2] == '\\' || value[2] == '/'))
}

func (a *App) toolKnowledgeExplain(args map[string]interface{}) string {
	query := knowledgeToolStringArg(args, "query")
	if query == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing query argument"))
	}
	limit := knowledgeToolIntArg(args, "limit", 8)
	if limit <= 0 {
		limit = 8
	}
	if limit > 50 {
		limit = 50
	}
	opts := knowledge.SearchOptions{
		Query:           query,
		SearchScope:     knowledgeToolStringArg(args, "search_scope"),
		ProjectPath:     knowledgeToolStringArg(args, "project_path"),
		TopicHint:       knowledgeToolStringArg(args, "topic_hint"),
		ContextTerms:    knowledgeToolStringSlice(args["context_terms"]),
		ResultTypes:     knowledgeToolStringSlice(args["result_types"]),
		SourceKinds:     knowledgeToolStringSlice(args["source_kinds"]),
		SourceIDs:       knowledgeToolSourceIDs(args),
		Labels:          knowledgeToolStringSlice(args["labels"]),
		Domain:          knowledgeToolStringArg(args, "domain"),
		Limit:           limit,
		IncludeDisabled: knowledgeToolBoolArg(args, "include_disabled", false),
	}
	explain, err := a.KnowledgeExplain(opts)
	explain = knowledge.ProjectImageExplainForTool(explain)
	return knowledgeToolJSON(map[string]interface{}{"explain": explain}, err)
}

func (a *App) toolKnowledgeSearchFacets(args map[string]interface{}) string {
	query := knowledgeToolStringArg(args, "query")
	limit := knowledgeToolIntArg(args, "limit", 80)
	if limit <= 0 {
		limit = 80
	}
	if limit > 100 {
		limit = 100
	}
	opts := knowledge.SearchOptions{
		Query:           query,
		SearchScope:     knowledgeToolStringArg(args, "search_scope"),
		ProjectPath:     knowledgeToolStringArg(args, "project_path"),
		TopicHint:       knowledgeToolStringArg(args, "topic_hint"),
		ContextTerms:    knowledgeToolStringSlice(args["context_terms"]),
		ResultTypes:     knowledgeToolStringSlice(args["result_types"]),
		SourceKinds:     knowledgeToolStringSlice(args["source_kinds"]),
		SourceIDs:       knowledgeToolSourceIDs(args),
		Labels:          knowledgeToolStringSlice(args["labels"]),
		Domain:          knowledgeToolStringArg(args, "domain"),
		Limit:           limit,
		IncludeDisabled: knowledgeToolBoolArg(args, "include_disabled", false),
	}
	facets, err := a.KnowledgeSearchFacets(opts)
	return knowledgeToolJSON(map[string]interface{}{"facets": facets}, err)
}

func (a *App) toolKnowledgeTopicRelevance(args map[string]interface{}) string {
	query := knowledgeToolStringArg(args, "query")
	topicHint := knowledgeToolStringArg(args, "topic_hint")
	if topicHint == "" && query == "" && len(knowledgeToolStringSlice(args["context_terms"])) == 0 {
		return knowledgeToolJSON(nil, fmt.Errorf("missing topic_hint or query"))
	}
	limit := knowledgeToolIntArg(args, "limit", 20)
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	opts := knowledge.SearchOptions{
		Query:           query,
		SearchScope:     knowledgeToolStringArg(args, "search_scope"),
		ProjectPath:     knowledgeToolStringArg(args, "project_path"),
		TopicHint:       topicHint,
		ContextTerms:    knowledgeToolStringSlice(args["context_terms"]),
		SourceKinds:     knowledgeToolStringSlice(args["source_kinds"]),
		SourceIDs:       knowledgeToolSourceIDs(args),
		Labels:          knowledgeToolStringSlice(args["labels"]),
		Domain:          knowledgeToolStringArg(args, "domain"),
		Limit:           limit,
		IncludeDisabled: knowledgeToolBoolArg(args, "include_disabled", false),
	}
	report, err := a.KnowledgeTopicRelevance(opts)
	report = knowledge.ProjectImageTopicRelevanceForTool(report)
	return knowledgeToolJSON(map[string]interface{}{"topic_relevance": report}, err)
}

func (a *App) toolKnowledgeContextPack(args map[string]interface{}) string {
	query := knowledgeToolStringArg(args, "query")
	if query == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing query argument"))
	}
	maxItems := knowledgeToolIntArg(args, "max_items", 8)
	if maxItems <= 0 {
		maxItems = 8
	}
	if maxItems > 30 {
		maxItems = 30
	}
	maxChars := knowledgeToolIntArg(args, "max_chars", 6000)
	if maxChars <= 0 {
		maxChars = 6000
	}
	if maxChars > 20000 {
		maxChars = 20000
	}
	opts := knowledge.ContextPackOptions{
		SearchOptions: knowledge.SearchOptions{
			Query:           query,
			SearchScope:     knowledgeToolStringArg(args, "search_scope"),
			ProjectPath:     knowledgeToolStringArg(args, "project_path"),
			TopicHint:       knowledgeToolStringArg(args, "topic_hint"),
			ContextTerms:    knowledgeToolStringSlice(args["context_terms"]),
			ResultTypes:     knowledgeToolStringSlice(args["result_types"]),
			SourceKinds:     knowledgeToolStringSlice(args["source_kinds"]),
			SourceIDs:       knowledgeToolSourceIDs(args),
			Labels:          knowledgeToolStringSlice(args["labels"]),
			Domain:          knowledgeToolStringArg(args, "domain"),
			Limit:           maxItems * 2,
			IncludeDisabled: knowledgeToolBoolArg(args, "include_disabled", false),
		},
		MaxItems: maxItems,
		MaxChars: maxChars,
	}
	pack, err := a.KnowledgeContextPack(opts)
	response := map[string]interface{}{"context_pack": pack}
	if err == nil && len(pack.Items) == 0 {
		response["guidance"] = knowledge.EmptyContextPackMessage
	}
	return knowledgeToolJSON(response, err)
}

func (a *App) toolKnowledgeFactGraph(args map[string]interface{}) string {
	limit := knowledgeToolIntArg(args, "limit", 40)
	if limit <= 0 {
		limit = 40
	}
	if limit > 100 {
		limit = 100
	}
	opts := knowledge.SearchOptions{
		Query:           knowledgeToolStringArg(args, "query"),
		SearchScope:     knowledgeToolStringArg(args, "search_scope"),
		ProjectPath:     knowledgeToolStringArg(args, "project_path"),
		SourceKinds:     knowledgeToolStringSlice(args["source_kinds"]),
		SourceIDs:       knowledgeToolSourceIDs(args),
		Labels:          knowledgeToolStringSlice(args["labels"]),
		Domain:          knowledgeToolStringArg(args, "domain"),
		Entity:          knowledgeToolStringArg(args, "entity"),
		Predicate:       knowledgeToolStringArg(args, "predicate"),
		Limit:           limit,
		IncludeDisabled: knowledgeToolBoolArg(args, "include_disabled", false),
	}
	graph, err := a.KnowledgeFactGraph(opts)
	return knowledgeToolJSON(map[string]interface{}{"fact_graph": graph}, err)
}

func (a *App) toolKnowledgeFactIndex(args map[string]interface{}) string {
	limit := knowledgeToolIntArg(args, "limit", 60)
	if limit <= 0 {
		limit = 60
	}
	if limit > 200 {
		limit = 200
	}
	opts := knowledge.FactIndexOptions{
		SearchOptions: knowledge.SearchOptions{
			Query:           knowledgeToolStringArg(args, "query"),
			SearchScope:     knowledgeToolStringArg(args, "search_scope"),
			ProjectPath:     knowledgeToolStringArg(args, "project_path"),
			SourceKinds:     knowledgeToolStringSlice(args["source_kinds"]),
			SourceIDs:       knowledgeToolSourceIDs(args),
			Labels:          knowledgeToolStringSlice(args["labels"]),
			Domain:          knowledgeToolStringArg(args, "domain"),
			Limit:           limit,
			IncludeDisabled: knowledgeToolBoolArg(args, "include_disabled", false),
		},
		Kind: knowledgeToolStringArg(args, "kind"),
	}
	index, err := a.KnowledgeFactIndex(opts)
	return knowledgeToolJSON(map[string]interface{}{"fact_index": index}, err)
}

func (a *App) toolKnowledgeEntityProfile(args map[string]interface{}) string {
	limit := knowledgeToolIntArg(args, "limit", 60)
	if limit <= 0 {
		limit = 60
	}
	if limit > 100 {
		limit = 100
	}
	opts := knowledge.SearchOptions{
		Query:           knowledgeToolStringArg(args, "query"),
		SearchScope:     knowledgeToolStringArg(args, "search_scope"),
		ProjectPath:     knowledgeToolStringArg(args, "project_path"),
		SourceKinds:     knowledgeToolStringSlice(args["source_kinds"]),
		SourceIDs:       knowledgeToolSourceIDs(args),
		Labels:          knowledgeToolStringSlice(args["labels"]),
		Domain:          knowledgeToolStringArg(args, "domain"),
		Entity:          knowledgeToolStringArg(args, "entity"),
		Predicate:       knowledgeToolStringArg(args, "predicate"),
		Limit:           limit,
		IncludeDisabled: knowledgeToolBoolArg(args, "include_disabled", false),
	}
	profile, err := a.KnowledgeEntityProfile(opts)
	return knowledgeToolJSON(map[string]interface{}{"entity_profile": profile}, err)
}

func (a *App) toolKnowledgeSuggest(args map[string]interface{}) string {
	limit := knowledgeToolIntArg(args, "limit", 30)
	if limit <= 0 {
		limit = 30
	}
	if limit > 80 {
		limit = 80
	}
	opts := knowledge.KnowledgeSuggestOptions{
		SearchOptions: knowledge.SearchOptions{
			Query:           knowledgeToolStringArg(args, "query"),
			SearchScope:     knowledgeToolStringArg(args, "search_scope"),
			ProjectPath:     knowledgeToolStringArg(args, "project_path"),
			SourceKinds:     knowledgeToolStringSlice(args["source_kinds"]),
			SourceIDs:       knowledgeToolSourceIDs(args),
			Labels:          knowledgeToolStringSlice(args["labels"]),
			Domain:          knowledgeToolStringArg(args, "domain"),
			Limit:           limit,
			IncludeDisabled: knowledgeToolBoolArg(args, "include_disabled", false),
		},
		Kinds: knowledgeToolStringSlice(args["kinds"]),
	}
	result, err := a.KnowledgeSuggest(opts)
	return knowledgeToolJSON(map[string]interface{}{"suggestions": result}, err)
}

func (a *App) toolKnowledgeSaveURL(args map[string]interface{}) string {
	rawURL := knowledgeToolFirstString(args, "url", "link", "href", "uri", "target")
	if rawURL == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing url argument (aliases: link, href, uri, target)"))
	}
	source, err := a.KnowledgeSaveURL(rawURL, knowledgeToolStringArg(args, "save_scope"), knowledgeToolStringArg(args, "topic_hint"), knowledgeToolStringArg(args, "distill_mode"), knowledgeToolStringSlice(args["labels"]), knowledgeToolBoolArg(args, "auto_labels", true))
	source = knowledge.ProjectImageSourceForTool(source)
	return knowledgeToolJSON(map[string]interface{}{"source": source}, err)
}

func (a *App) toolKnowledgeSaveURLs(args map[string]interface{}) string {
	urls := knowledgeToolURLList(args["urls"])
	urls = append(urls, knowledgeToolURLList(args["links"])...)
	urls = append(urls, knowledgeToolURLList(args["hrefs"])...)
	urls = append(urls, knowledgeToolURLList(args["text"])...)
	urls = append(urls, knowledgeToolURLList(args["url_list"])...)
	urls = append(urls, knowledgeToolURLList(args["link_list"])...)
	var discovery knowledge.URLDiscoveryResult
	var discoveryErr error
	if knowledgeToolBoolArg(args, "discover_urls", false) {
		discovery, discoveryErr = a.KnowledgeDiscoverURLs(knowledge.URLDiscoveryRequest{
			Text:           knowledgeToolFirstString(args, "text", "html", "url_list", "link_list"),
			BaseURL:        knowledgeToolFirstString(args, "base_url"),
			SameDomainOnly: knowledgeToolBoolArg(args, "same_domain_only", false),
			Limit:          knowledgeToolIntArg(args, "limit", 100),
		})
		if discoveryErr != nil {
			return knowledgeToolJSON(nil, discoveryErr)
		}
		urls = append(urls, discovery.URLs...)
	}
	urls = knowledgeToolUniqueStrings(urls)
	if len(urls) == 0 {
		return knowledgeToolJSON(nil, fmt.Errorf("missing urls argument"))
	}
	result, err := a.KnowledgeSaveURLs(urls, knowledgeToolStringArg(args, "save_scope"), knowledgeToolStringArg(args, "topic_hint"), knowledgeToolStringArg(args, "distill_mode"), knowledgeToolStringSlice(args["labels"]), knowledgeToolBoolArg(args, "auto_labels", true))
	result = knowledge.ProjectImageURLBatchSaveForTool(result)
	return knowledgeToolJSON(map[string]interface{}{"result": result, "discovery": discovery}, err)
}

func (a *App) toolKnowledgeDiscoverURLs(args map[string]interface{}) string {
	text := knowledgeToolFirstString(args, "text", "html", "url_list", "link_list")
	if text == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing text argument"))
	}
	result, err := a.KnowledgeDiscoverURLs(knowledge.URLDiscoveryRequest{
		Text:           text,
		BaseURL:        knowledgeToolFirstString(args, "base_url"),
		SameDomainOnly: knowledgeToolBoolArg(args, "same_domain_only", false),
		Limit:          knowledgeToolIntArg(args, "limit", 100),
	})
	return knowledgeToolJSON(map[string]interface{}{"result": result, "urls": result.URLs}, err)
}

func (a *App) toolKnowledgeSaveText(args map[string]interface{}) string {
	text := knowledgeToolFirstString(args, "text")
	if text == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing text argument"))
	}
	source, err := a.KnowledgeSaveText(knowledge.TextSaveRequest{
		Text:        text,
		Title:       knowledgeToolStringArg(args, "title"),
		Kind:        knowledgeToolStringArg(args, "kind"),
		SaveScope:   knowledgeToolStringArg(args, "save_scope"),
		TopicHint:   knowledgeToolStringArg(args, "topic_hint"),
		DistillMode: knowledgeToolStringArg(args, "distill_mode"),
		Labels:      knowledgeToolStringSlice(args["labels"]),
		AutoLabels:  knowledgeToolBoolArg(args, "auto_labels", true),
	})
	return knowledgeToolJSON(map[string]interface{}{"source": source}, err)
}

func (a *App) toolKnowledgeImportDirectory(args map[string]interface{}) string {
	rootPath := knowledgeToolFirstString(args, "root_path", "path", "dir", "directory", "folder", "root")
	if rootPath == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing root_path argument (aliases: path, dir, directory, folder, root)"))
	}
	maxFileMB := knowledgeToolIntArg(args, "max_file_mb", 100)
	if maxFileMB <= 0 {
		maxFileMB = 100
	}
	req := knowledge.DirectoryImportRequest{
		RootPath:     rootPath,
		SaveScope:    knowledgeToolStringArg(args, "save_scope"),
		TopicHint:    knowledgeToolStringArg(args, "topic_hint"),
		DistillMode:  knowledgeToolStringArg(args, "distill_mode"),
		Labels:       knowledgeToolStringSlice(args["labels"]),
		AutoLabels:   knowledgeToolBoolArg(args, "auto_labels", true),
		Recursive:    knowledgeToolBoolArg(args, "recursive", true),
		IncludeExts:  knowledgeToolStringSlice(args["include_exts"]),
		ExcludeGlobs: knowledgeToolStringSlice(args["exclude_globs"]),
		MaxFileBytes: int64(maxFileMB) * 1024 * 1024,
	}
	switch normalizeKnowledgeToolActionKind(knowledgeToolStringArg(args, "action")) {
	case knowledgeToolActionScan:
		result, err := a.KnowledgeScanDirectory(req)
		return knowledgeToolJSON(map[string]interface{}{"scan": result}, err)
	default:
		if knowledgeToolBoolArg(args, "start_async", true) {
			job, err := a.KnowledgeStartImportDirectory(req)
			return knowledgeToolJSON(map[string]interface{}{"job": job}, err)
		}
		result, err := a.KnowledgeImportDirectory(req)
		return knowledgeToolJSON(map[string]interface{}{"import": result}, err)
	}
}

func (a *App) toolKnowledgeImportFiles(args map[string]interface{}) string {
	filePaths := knowledgeToolFilePathSlice(args["file_paths"])
	if len(filePaths) == 0 {
		filePaths = knowledgeToolFilePathSlice(args["paths"])
	}
	if len(filePaths) == 0 {
		filePaths = knowledgeToolFilePathSlice(args["files"])
	}
	if len(filePaths) == 0 {
		filePaths = knowledgeToolFilePathSlice(args["file_path"])
	}
	if len(filePaths) == 0 {
		filePaths = knowledgeToolFilePathSlice(args["path"])
	}
	if len(filePaths) == 0 {
		return knowledgeToolJSON(nil, fmt.Errorf("missing file_paths argument (aliases: paths, files, file_path, path)"))
	}
	maxFileMB := knowledgeToolIntArg(args, "max_file_mb", 100)
	if maxFileMB <= 0 {
		maxFileMB = 100
	}
	req := knowledge.DirectoryImportRequest{
		SaveScope:    knowledgeToolStringArg(args, "save_scope"),
		TopicHint:    knowledgeToolStringArg(args, "topic_hint"),
		DistillMode:  knowledgeToolStringArg(args, "distill_mode"),
		Labels:       knowledgeToolStringSlice(args["labels"]),
		AutoLabels:   knowledgeToolBoolArg(args, "auto_labels", true),
		IncludeExts:  knowledgeToolStringSlice(args["include_exts"]),
		ExcludeGlobs: knowledgeToolStringSlice(args["exclude_globs"]),
		MaxFileBytes: int64(maxFileMB) * 1024 * 1024,
	}
	switch normalizeKnowledgeToolActionKind(knowledgeToolStringArg(args, "action")) {
	case knowledgeToolActionScan:
		result, err := a.KnowledgeScanFiles(req, filePaths)
		return knowledgeToolJSON(map[string]interface{}{"scan": result}, err)
	default:
		if knowledgeToolBoolArg(args, "start_async", true) {
			job, err := a.KnowledgeStartImportFiles(req, filePaths)
			return knowledgeToolJSON(map[string]interface{}{"job": job}, err)
		}
		result, err := a.KnowledgeImportFiles(req, filePaths)
		return knowledgeToolJSON(map[string]interface{}{"import": result}, err)
	}
}

func (a *App) toolKnowledgeImportStatus(args map[string]interface{}) string {
	jobID := knowledgeToolFirstString(args, "job_id", "id")
	if jobID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing job_id argument"))
	}
	job, err := a.KnowledgeImportJobStatus(jobID)
	return knowledgeToolJSON(map[string]interface{}{"job": job}, err)
}

func (a *App) toolKnowledgeStats(args map[string]interface{}) string {
	stats, err := a.KnowledgeStats()
	return knowledgeToolJSON(map[string]interface{}{"stats": stats}, err)
}

func (a *App) toolKnowledgeDoctor(args map[string]interface{}) string {
	doctor, err := a.KnowledgeDoctor()
	return knowledgeToolJSON(map[string]interface{}{"doctor": doctor}, err)
}

func (a *App) toolKnowledgeHealth(args map[string]interface{}) string {
	health, err := a.KnowledgeHealth(args)
	return knowledgeToolJSON(map[string]interface{}{"health": health}, err)
}

func (a *App) KnowledgeHealth(args map[string]interface{}) (map[string]interface{}, error) {
	limit := knowledgeToolSourceFilterLimit(args, 100, 500, 5000)
	doctor, err := a.KnowledgeDoctor()
	if err != nil {
		return nil, err
	}
	quality, err := a.KnowledgeSourceQualityReport(knowledgeToolListSourcesOptions(args, limit))
	if err != nil {
		return nil, err
	}
	quality = knowledge.ProjectImageSourceQualityForTool(quality)
	score := doctor.Score
	if quality.Count > 0 {
		score = int(math.Round(float64(doctor.Score)*0.6 + quality.AverageScore*0.4))
	}
	health := map[string]interface{}{
		"score":              score,
		"status":             knowledgeToolHealthStatus(score, doctor.Status),
		"doctor_score":       doctor.Score,
		"doctor_status":      doctor.Status,
		"source_count":       doctor.Stats.Sources,
		"quality_count":      quality.Count,
		"quality_avg_score":  quality.AverageScore,
		"quality_grades":     quality.Grades,
		"quality_signals":    quality.Signals,
		"doctor_findings":    knowledgeToolDoctorFindingSummary(doctor.Findings),
		"top_findings":       knowledgeToolTopDoctorFindings(doctor.Findings, 5),
		"notes":              []string{"local_knowledge_health_no_llm", "combines_doctor_and_source_quality"},
		"recommended_tools":  knowledgeToolHealthRecommendedTools(doctor.Findings),
		"maintenance_needed": len(doctor.Findings) > 0 || knowledgeToolQualityNeedsMaintenance(quality),
	}
	if knowledgeToolBoolArg(args, "include_plan", true) {
		plan, err := a.KnowledgeSourceQualityMaintenancePlan(knowledgeToolListSourcesOptions(args, limit))
		if err != nil {
			return nil, err
		}
		health["maintenance_actions"] = knowledgeToolMaintenanceActionSummary(plan.Actions)
		health["maintenance_action_count"] = len(plan.Actions)
	}
	return health, nil
}

func (a *App) toolKnowledgeSourceQuality(args map[string]interface{}) string {
	limit := knowledgeToolSourceFilterLimit(args, 100, 1000, 5000)
	report, err := a.KnowledgeSourceQualityReport(knowledgeToolListSourcesOptions(args, limit))
	report = knowledge.ProjectImageSourceQualityForTool(report)
	return knowledgeToolJSON(map[string]interface{}{"quality": report}, err)
}

func (a *App) toolKnowledgeQualityMaintenancePlan(args map[string]interface{}) string {
	limit := knowledgeToolSourceFilterLimit(args, 100, 500, 5000)
	plan, err := a.KnowledgeSourceQualityMaintenancePlan(knowledgeToolListSourcesOptions(args, limit))
	plan.Quality = knowledge.ProjectImageSourceQualityForTool(plan.Quality)
	return knowledgeToolJSON(map[string]interface{}{"plan": plan}, err)
}

func (a *App) toolKnowledgeQualityMaintenancePolicies(args map[string]interface{}) string {
	return knowledgeToolJSON(map[string]interface{}{"policies": a.KnowledgeQualityMaintenancePolicies()}, nil)
}

func (a *App) toolKnowledgeExecuteQualityMaintenancePlan(args map[string]interface{}) string {
	limit := knowledgeToolSourceFilterLimit(args, 100, 500, 5000)
	result, err := a.KnowledgeExecuteSourceQualityMaintenancePlan(knowledge.SourceQualityMaintenanceExecuteRequest{
		Filter:                    knowledgeToolListSourcesOptions(args, limit),
		Policy:                    knowledgeToolStringArg(args, "policy"),
		Actions:                   knowledgeToolStringSlice(args["actions"]),
		DryRun:                    knowledgeToolBoolArg(args, "dry_run", true),
		DistillMode:               knowledgeToolStringArg(args, "distill_mode"),
		MaxSourcesPerAction:       knowledgeToolIntArg(args, "max_sources_per_action", 0),
		AllowSensitiveDisable:     knowledgeToolBoolArg(args, "allow_sensitive_disable", false),
		AllowDuplicateSuppression: knowledgeToolBoolArg(args, "allow_duplicate_suppression", false),
	})
	result = knowledge.ProjectImageQualityMaintenanceExecutionForTool(result)
	return knowledgeToolJSON(map[string]interface{}{"execution": result}, err)
}

func (a *App) toolKnowledgeRebuildQualityGaps(args map[string]interface{}) string {
	limit := knowledgeToolIntArg(args, "limit", 100)
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	report, err := a.KnowledgeSourceQualityReport(knowledgeToolListSourcesOptions(args, limit))
	if err != nil {
		return knowledgeToolJSON(nil, err)
	}
	report = knowledge.ProjectImageSourceQualityForTool(report)
	ids := make([]string, 0, len(report.Items))
	for _, item := range report.Items {
		if item.Source.ID == "" {
			continue
		}
		if knowledgeToolStringSliceContains(item.Signals, "missing_cards") || knowledgeToolStringSliceContains(item.Signals, "missing_facts") {
			ids = append(ids, item.Source.ID)
		}
	}
	if len(ids) == 0 {
		return knowledgeToolJSON(map[string]interface{}{
			"quality":            report,
			"candidate_count":    0,
			"rebuilt":            false,
			"rebuild_source_ids": []string{},
			"reason":             "no_missing_cards_or_facts_in_quality_slice",
		}, nil)
	}
	result, err := a.KnowledgeRebuildSourcesDerived(ids, knowledgeToolStringArg(args, "distill_mode"))
	result = knowledge.ProjectImageSourceRebuildForTool(result)
	return knowledgeToolJSON(map[string]interface{}{
		"quality":            report,
		"candidate_count":    len(ids),
		"rebuilt":            err == nil,
		"rebuild_source_ids": ids,
		"result":             result,
	}, err)
}

func (a *App) toolKnowledgeCapabilities(args map[string]interface{}) string {
	return knowledgeToolJSON(map[string]interface{}{"capabilities": a.KnowledgeCapabilities()}, nil)
}

func (a *App) toolKnowledgeURLDomainPolicies(args map[string]interface{}) string {
	switch normalizeKnowledgeToolActionKind(knowledgeToolStringArg(args, "action")) {
	case knowledgeToolActionReplace:
		result, err := a.KnowledgeUpdateURLDomainPolicies(knowledge.URLDomainPolicyUpdateRequest{
			AllowDomains: knowledgeToolStringSlice(args["allow_domains"]),
			BlockDomains: knowledgeToolStringSlice(args["block_domains"]),
			Replace:      true,
			Reason:       knowledgeToolStringArg(args, "reason"),
		})
		return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
	case knowledgeToolActionCheck:
		rawURL := knowledgeToolFirstString(args, "url")
		if rawURL == "" {
			return knowledgeToolJSON(nil, fmt.Errorf("missing url argument"))
		}
		result, err := a.KnowledgeCheckURLDomainPolicy(rawURL)
		return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
	default:
		policies, err := a.KnowledgeListURLDomainPolicies()
		return knowledgeToolJSON(map[string]interface{}{"count": len(policies), "policies": policies}, err)
	}
}

func (a *App) toolKnowledgeMaintain(args map[string]interface{}) string {
	result, err := a.KnowledgeMaintain(knowledgeToolBoolArg(args, "vacuum", false))
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgeExportSnapshot(args map[string]interface{}) string {
	redact := knowledgeToolBoolArg(args, "redact_sensitive", true)
	outputPath := knowledgeToolFirstString(args, "output_path", "path")
	sourceIDs := knowledgeToolSourceIDs(args)
	if len(sourceIDs) == 0 && hasKnowledgeSourceFilterArgs(args) {
		limit := knowledgeToolIntArg(args, "limit", 500)
		if limit <= 0 || limit > 500 {
			limit = 500
		}
		sources, err := a.KnowledgeListSources(knowledgeToolListSourcesOptions(args, limit))
		if err != nil {
			return knowledgeToolJSON(nil, err)
		}
		sourceIDs = make([]string, 0, len(sources))
		for _, source := range sources {
			if strings.TrimSpace(source.ID) != "" {
				sourceIDs = append(sourceIDs, source.ID)
			}
		}
	}
	result, err := a.KnowledgeExportSnapshotWithOptions(knowledge.ExportOptions{OutputPath: outputPath, RedactSensitive: redact, SourceIDs: sourceIDs})
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgeImportSnapshot(args map[string]interface{}) string {
	inputPath := knowledgeToolFirstString(args, "input_path", "path")
	if inputPath == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing input_path argument"))
	}
	result, err := a.KnowledgeImportSnapshot(knowledge.SnapshotImportOptions{
		InputPath:          inputPath,
		DryRun:             knowledgeToolBoolArg(args, "dry_run", true),
		Overwrite:          knowledgeToolBoolArg(args, "overwrite", false),
		SkipSafetyBackup:   knowledgeToolBoolArg(args, "skip_safety_backup", false),
		SafetyBackupPath:   knowledgeToolStringArg(args, "safety_backup_path"),
		SafetyBackupRedact: knowledgeToolBoolArg(args, "safety_backup_redact", false),
	})
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgeShareToHub(args map[string]interface{}) string {
	sourceIDs := knowledgeToolSourceIDs(args)
	if len(sourceIDs) == 0 && hasKnowledgeSourceFilterArgs(args) {
		limit := knowledgeToolIntArg(args, "limit", 500)
		if limit <= 0 || limit > 500 {
			limit = 500
		}
		sources, err := a.KnowledgeListSources(knowledgeToolListSourcesOptions(args, limit))
		if err != nil {
			return knowledgeToolJSON(nil, err)
		}
		sourceIDs = make([]string, 0, len(sources))
		for _, source := range sources {
			if strings.TrimSpace(source.ID) != "" {
				sourceIDs = append(sourceIDs, source.ID)
			}
		}
	}
	result, err := a.KnowledgeShareToHub(KnowledgeHubShareRequest{
		HubURL:          knowledgeToolStringArg(args, "hub_url"),
		HubToken:        knowledgeToolStringArg(args, "hub_token"),
		Title:           knowledgeToolStringArg(args, "title"),
		Description:     knowledgeToolStringArg(args, "description"),
		VisibilityScope: knowledgeToolStringArg(args, "visibility_scope"),
		VisibilityUsers: knowledgeToolStringSlice(args["visibility_users"]),
		TTL:             knowledgeToolStringArg(args, "ttl"),
		SourceIDs:       sourceIDs,
		RedactSensitive: knowledgeToolBoolArg(args, "redact_sensitive", true),
		IncludeDisabled: knowledgeToolBoolArg(args, "include_disabled", false),
	})
	return knowledgeToolJSON(map[string]interface{}{"share": result}, err)
}

func (a *App) toolKnowledgeImportHubShare(args map[string]interface{}) string {
	result, err := a.KnowledgeImportHubShare(KnowledgeHubShareImportRequest{
		HubURL:      knowledgeToolStringArg(args, "hub_url"),
		HubToken:    knowledgeToolStringArg(args, "hub_token"),
		KnowledgeID: knowledgeToolFirstString(args, "knowledge_id", "id"),
		ShareLink:   knowledgeToolFirstString(args, "share_link", "link", "url"),
		DryRun:      knowledgeToolBoolArg(args, "dry_run", true),
	})
	return knowledgeToolJSON(map[string]interface{}{"import": result}, err)
}
func (a *App) toolKnowledgeListSources(args map[string]interface{}) string {
	limit := knowledgeToolIntArg(args, "limit", 50)
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	opts := knowledgeToolListSourcesOptions(args, limit)
	sources, err := a.KnowledgeListSources(opts)
	sources = knowledge.ProjectImageSourcesForTool(sources)
	return knowledgeToolJSON(map[string]interface{}{"count": len(sources), "sources": sources}, err)
}

func (a *App) toolKnowledgeListSourceLabels(args map[string]interface{}) string {
	limit := knowledgeToolIntArg(args, "limit", 1000)
	if limit <= 0 {
		limit = 1000
	}
	if limit > 5000 {
		limit = 5000
	}
	labels, err := a.KnowledgeListSourceLabels(knowledgeToolListSourcesOptions(args, limit))
	return knowledgeToolJSON(map[string]interface{}{"count": len(labels), "labels": labels}, err)
}

func (a *App) toolKnowledgeUpdateSourceLabels(args map[string]interface{}) string {
	limit := knowledgeToolIntArg(args, "limit", 1000)
	if limit <= 0 {
		limit = 1000
	}
	if limit > 5000 {
		limit = 5000
	}
	req := knowledge.SourceLabelUpdateRequest{
		SourceIDs:     knowledgeToolSourceIDs(args),
		Filter:        knowledgeToolListSourcesOptions(args, limit),
		AddLabels:     knowledgeToolStringSlice(args["add_labels"]),
		RemoveLabels:  knowledgeToolStringSlice(args["remove_labels"]),
		ReplaceLabels: knowledgeToolStringSlice(args["replace_labels"]),
		RenameFrom:    knowledgeToolStringArg(args, "rename_from"),
		RenameTo:      knowledgeToolStringArg(args, "rename_to"),
		ClearLabels:   knowledgeToolBoolArg(args, "clear_labels", false),
		DryRun:        knowledgeToolBoolArg(args, "dry_run", false),
		Limit:         limit,
	}
	if len(req.SourceIDs) == 0 && !hasKnowledgeSourceFilterArgs(args) && strings.TrimSpace(req.RenameFrom) == "" && len(req.RemoveLabels) == 0 {
		return knowledgeToolJSON(nil, fmt.Errorf("provide source_ids or at least one source filter before bulk label updates"))
	}
	if strings.TrimSpace(req.RenameFrom) != "" && strings.TrimSpace(req.RenameTo) == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("provide rename_to when rename_from is set"))
	}
	if len(req.AddLabels) == 0 && len(req.RemoveLabels) == 0 && len(req.ReplaceLabels) == 0 && strings.TrimSpace(req.RenameFrom) == "" && !req.ClearLabels {
		return knowledgeToolJSON(nil, fmt.Errorf("provide add_labels, remove_labels, replace_labels, rename_from/rename_to, or clear_labels"))
	}
	result, err := a.KnowledgeUpdateSourceLabels(req)
	result = knowledge.ProjectImageSourceLabelUpdateForTool(result)
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgeBackfillSourceAutoLabels(args map[string]interface{}) string {
	limit := knowledgeToolIntArg(args, "limit", 1000)
	req := knowledge.SourceAutoLabelBackfillRequest{
		SourceIDs: knowledgeToolSourceIDs(args),
		Filter:    knowledgeToolListSourcesOptions(args, limit),
		DryRun:    knowledgeToolBoolArg(args, "dry_run", false),
		Limit:     limit,
	}
	result, err := a.KnowledgeBackfillSourceAutoLabels(req)
	result = knowledge.ProjectImageSourceLabelUpdateForTool(result)
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgeBackfillQualityLabels(args map[string]interface{}) string {
	limit := knowledgeToolIntArg(args, "limit", 100)
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	report, err := a.KnowledgeSourceQualityReport(knowledgeToolListSourcesOptions(args, limit))
	if err != nil {
		return knowledgeToolJSON(nil, err)
	}
	report = knowledge.ProjectImageSourceQualityForTool(report)
	ids := make([]string, 0, len(report.Items))
	for _, item := range report.Items {
		if item.Source.ID == "" {
			continue
		}
		if knowledgeToolStringSliceContains(item.Signals, "missing_labels") {
			ids = append(ids, item.Source.ID)
		}
	}
	if len(ids) == 0 {
		return knowledgeToolJSON(map[string]interface{}{
			"quality":         report,
			"candidate_count": 0,
			"backfilled":      false,
			"source_ids":      []string{},
			"reason":          "no_missing_labels_in_quality_slice",
		}, nil)
	}
	result, err := a.KnowledgeBackfillSourceAutoLabels(knowledge.SourceAutoLabelBackfillRequest{
		SourceIDs: ids,
		DryRun:    knowledgeToolBoolArg(args, "dry_run", false),
		Limit:     len(ids),
	})
	result = knowledge.ProjectImageSourceLabelUpdateForTool(result)
	return knowledgeToolJSON(map[string]interface{}{
		"quality":         report,
		"candidate_count": len(ids),
		"backfilled":      err == nil && !knowledgeToolBoolArg(args, "dry_run", false),
		"source_ids":      ids,
		"result":          result,
	}, err)
}

func (a *App) toolKnowledgeDisableQualitySensitiveSources(args map[string]interface{}) string {
	report, ids, err := a.knowledgeQualitySourceIDsWithSignal(args, "possible_sensitive_content")
	if err != nil {
		return knowledgeToolJSON(nil, err)
	}
	if len(ids) == 0 {
		return knowledgeToolJSON(map[string]interface{}{
			"quality":         report,
			"candidate_count": 0,
			"disabled":        false,
			"source_ids":      []string{},
			"reason":          "no_possible_sensitive_content_in_quality_slice",
		}, nil)
	}
	result, err := a.KnowledgeDisableSources(ids)
	return knowledgeToolJSON(map[string]interface{}{
		"quality":         report,
		"candidate_count": len(ids),
		"disabled":        err == nil,
		"source_ids":      ids,
		"result":          result,
	}, err)
}

func (a *App) toolKnowledgeSuppressQualityDuplicateGroups(args map[string]interface{}) string {
	report, ids, err := a.knowledgeQualitySourceIDsWithSignal(args, "duplicate_card_claims")
	if err != nil {
		return knowledgeToolJSON(nil, err)
	}
	if len(ids) == 0 {
		return knowledgeToolJSON(map[string]interface{}{
			"quality":          report,
			"candidate_count":  0,
			"processed_groups": 0,
			"suppressed":       0,
			"source_ids":       []string{},
			"reason":           "no_duplicate_card_claims_in_quality_slice",
		}, nil)
	}
	duplicateLimit := knowledgeToolIntArg(args, "duplicate_limit", 100)
	if duplicateLimit <= 0 {
		duplicateLimit = 100
	}
	if duplicateLimit > 1000 {
		duplicateLimit = 1000
	}
	reason := knowledgeToolStringArg(args, "reason")
	if reason == "" {
		reason = "quality_duplicate_card_claim_bulk"
	}
	groups, err := a.KnowledgeListDuplicateCards(duplicateLimit)
	if err != nil {
		return knowledgeToolJSON(nil, err)
	}
	idSet := knowledgeToolStringSet(ids)
	type suppressedGroupResult struct {
		Key        string `json:"key"`
		Claim      string `json:"claim,omitempty"`
		Suppressed int    `json:"suppressed"`
		KeptCardID string `json:"kept_card_id,omitempty"`
		Error      string `json:"error,omitempty"`
	}
	results := make([]suppressedGroupResult, 0)
	processed := 0
	skipped := 0
	totalSuppressed := 0
	errors := make([]string, 0)
	for _, group := range groups {
		if !knowledgeToolStringSetIntersects(idSet, group.SourceIDs) {
			skipped++
			continue
		}
		result, suppressErr := a.KnowledgeSuppressDuplicateCards(knowledge.DuplicateCardSuppressionRequest{
			Key:         group.Key,
			OwnerID:     group.OwnerID,
			TenantID:    group.TenantID,
			ProjectPath: group.ProjectPath,
			Reason:      reason,
		})
		processed++
		item := suppressedGroupResult{Key: group.Key, Claim: group.Claim, Suppressed: result.Suppressed, KeptCardID: result.KeptCardID}
		if suppressErr != nil {
			item.Error = suppressErr.Error()
			errors = append(errors, fmt.Sprintf("%s: %v", group.Key, suppressErr))
		} else {
			totalSuppressed += result.Suppressed
		}
		results = append(results, item)
	}
	return knowledgeToolJSON(map[string]interface{}{
		"quality":          report,
		"candidate_count":  len(ids),
		"source_ids":       ids,
		"requested_groups": len(groups),
		"processed_groups": processed,
		"skipped_groups":   skipped,
		"suppressed":       totalSuppressed,
		"errors":           errors,
		"results":          results,
	}, nil)
}

func (a *App) knowledgeQualitySourceIDsWithSignal(args map[string]interface{}, signal string) (knowledge.SourceQualityReport, []string, error) {
	limit := knowledgeToolQualityInspectionLimit(args)
	report, err := a.KnowledgeSourceQualityReport(knowledgeToolListSourcesOptions(args, limit))
	if err != nil {
		return knowledge.SourceQualityReport{}, nil, err
	}
	report = knowledge.ProjectImageSourceQualityForTool(report)
	ids := make([]string, 0, len(report.Items))
	for _, item := range report.Items {
		if item.Source.ID == "" {
			continue
		}
		if knowledgeToolStringSliceContains(item.Signals, signal) {
			ids = append(ids, item.Source.ID)
		}
	}
	return report, ids, nil
}

func knowledgeToolQualityInspectionLimit(args map[string]interface{}) int {
	return knowledgeToolSourceFilterLimit(args, 100, 5000, 5000)
}

func (a *App) toolKnowledgeListImportBatches(args map[string]interface{}) string {
	limit := knowledgeToolIntArg(args, "limit", 50)
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	batches, err := a.KnowledgeListImportBatches(limit)
	return knowledgeToolJSON(map[string]interface{}{"count": len(batches), "batches": batches}, err)
}

func (a *App) toolKnowledgeListImportItems(args map[string]interface{}) string {
	batchID := knowledgeToolStringArg(args, "batch_id")
	if batchID == "" {
		batchID = knowledgeToolStringArg(args, "id")
	}
	if batchID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing batch_id argument"))
	}
	limit := knowledgeToolIntArg(args, "limit", 200)
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	items, err := a.KnowledgeListImportItems(batchID, limit)
	return knowledgeToolJSON(map[string]interface{}{"batch_id": batchID, "count": len(items), "items": items}, err)
}

func (a *App) toolKnowledgeRetryImportBatch(args map[string]interface{}) string {
	batchID := knowledgeToolStringArg(args, "batch_id")
	if batchID == "" {
		batchID = knowledgeToolStringArg(args, "id")
	}
	if batchID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing batch_id argument"))
	}
	maxFileMB := knowledgeToolIntArg(args, "max_file_mb", 0)
	req := knowledge.ImportRetryRequest{
		BatchID:        batchID,
		ItemIDs:        knowledgeToolStringSlice(args["item_ids"]),
		Statuses:       knowledgeToolStringSlice(args["statuses"]),
		IncludeSkipped: knowledgeToolBoolArg(args, "include_skipped", false),
		IncludeExts:    knowledgeToolStringSlice(args["include_exts"]),
		TopicHint:      knowledgeToolStringArg(args, "topic_hint"),
		DistillMode:    knowledgeToolStringArg(args, "distill_mode"),
	}
	if maxFileMB > 0 {
		req.MaxFileBytes = int64(maxFileMB) * 1024 * 1024
	}
	result, err := a.KnowledgeRetryImportBatch(req)
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgeSourceDetail(args map[string]interface{}) string {
	sourceID := knowledgeToolStringArg(args, "source_id")
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "id")
	}
	if sourceID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_id argument"))
	}
	nodesLimit := knowledgeToolIntArg(args, "nodes_limit", knowledgeToolIntArg(args, "limit", 50))
	if nodesLimit <= 0 {
		nodesLimit = 50
	}
	if nodesLimit > 1000 {
		nodesLimit = 1000
	}
	cardsLimit := knowledgeToolIntArg(args, "cards_limit", knowledgeToolIntArg(args, "limit", 50))
	if cardsLimit <= 0 {
		cardsLimit = 50
	}
	if cardsLimit > 500 {
		cardsLimit = 500
	}
	factsLimit := knowledgeToolIntArg(args, "facts_limit", knowledgeToolIntArg(args, "limit", 100))
	if factsLimit <= 0 {
		factsLimit = 100
	}
	if factsLimit > 1000 {
		factsLimit = 1000
	}
	versionsLimit := knowledgeToolIntArg(args, "versions_limit", knowledgeToolIntArg(args, "limit", 20))
	if versionsLimit <= 0 {
		versionsLimit = 20
	}
	if versionsLimit > 200 {
		versionsLimit = 200
	}
	timelineLimit := knowledgeToolIntArg(args, "timeline_limit", knowledgeToolIntArg(args, "limit", 50))
	if timelineLimit <= 0 {
		timelineLimit = 50
	}
	if timelineLimit > 500 {
		timelineLimit = 500
	}
	linksLimit := knowledgeToolIntArg(args, "links_limit", knowledgeToolIntArg(args, "limit", 20))
	if linksLimit <= 0 {
		linksLimit = 20
	}
	if linksLimit > 200 {
		linksLimit = 200
	}
	linkEventsLimit := knowledgeToolIntArg(args, "link_events_limit", knowledgeToolIntArg(args, "limit", 20))
	if linkEventsLimit <= 0 {
		linkEventsLimit = 20
	}
	if linkEventsLimit > 200 {
		linkEventsLimit = 200
	}
	includeNeighborhood := knowledgeToolBoolArg(args, "include_neighborhood", true)
	neighborhoodDepth := knowledgeToolIntArg(args, "neighborhood_depth", 2)
	if neighborhoodDepth <= 0 {
		neighborhoodDepth = 1
	}
	if neighborhoodDepth > 3 {
		neighborhoodDepth = 3
	}
	neighborhoodLimit := knowledgeToolIntArg(args, "neighborhood_limit", 40)
	if neighborhoodLimit <= 0 {
		neighborhoodLimit = 40
	}
	if neighborhoodLimit > 200 {
		neighborhoodLimit = 200
	}
	edgeLimit := knowledgeToolIntArg(args, "edge_limit", 200)
	if edgeLimit <= 0 {
		edgeLimit = 200
	}
	if edgeLimit > 2000 {
		edgeLimit = 2000
	}
	source, err := a.KnowledgeGetSource(sourceID)
	if err != nil {
		return knowledgeToolJSON(nil, err)
	}
	nodes, err := a.KnowledgeListNodesBySource(sourceID, nodesLimit)
	if err != nil {
		return knowledgeToolJSON(nil, err)
	}
	cards, err := a.KnowledgeListCardsBySource(sourceID, cardsLimit)
	if err != nil {
		return knowledgeToolJSON(nil, err)
	}
	digest, err := a.KnowledgeSourceDigest(sourceID, nodesLimit, cardsLimit, factsLimit, linksLimit)
	if err != nil {
		return knowledgeToolJSON(nil, err)
	}
	versions, err := a.KnowledgeListSourceVersions(sourceID, versionsLimit)
	if err != nil {
		return knowledgeToolJSON(nil, err)
	}
	timeline, err := a.KnowledgeSourceTimeline(sourceID, timelineLimit)
	if err != nil {
		return knowledgeToolJSON(nil, err)
	}
	facts, err := a.KnowledgeListFactsBySource(sourceID, factsLimit)
	if err != nil {
		return knowledgeToolJSON(nil, err)
	}
	links, err := a.KnowledgeListSourceLinks(sourceID, linksLimit)
	if err != nil {
		return knowledgeToolJSON(nil, err)
	}
	linkEvents, err := a.KnowledgeListSourceLinkEvents(sourceID, linkEventsLimit)
	if err != nil {
		return knowledgeToolJSON(nil, err)
	}
	var neighborhood *knowledge.SourceGraphResult
	if includeNeighborhood {
		graph, err := a.KnowledgeSourceNeighborhood(sourceID, neighborhoodDepth, neighborhoodLimit, edgeLimit)
		if err != nil {
			return knowledgeToolJSON(nil, err)
		}
		neighborhood = &graph
	}
	source = knowledge.ProjectImageSourceForTool(source)
	nodes = knowledge.ProjectImageDocumentNodesForTool(nodes)
	versions = knowledge.ProjectImageSourceVersionsForTool(versions)
	digest = knowledge.ProjectImageSourceDigestForTool(digest)
	timeline = knowledge.ProjectImageSourceTimelineForTool(timeline)
	if neighborhood != nil {
		projected := knowledge.ProjectImageSourceGraphForTool(*neighborhood)
		neighborhood = &projected
	}
	links = knowledge.ProjectImageSourceLinksForParent(links, source.Kind == knowledge.SourceKindImage)
	linkEvents = knowledge.ProjectImageSourceLinkEventsForParent(linkEvents, source.Kind == knowledge.SourceKindImage)
	return knowledgeToolJSON(map[string]interface{}{
		"source_id":          sourceID,
		"source":             source,
		"digest":             digest,
		"node_count":         len(nodes),
		"card_count":         len(cards),
		"fact_count":         len(facts),
		"version_count":      len(versions),
		"timeline_count":     timeline.Count,
		"link_count":         len(links),
		"link_event_count":   len(linkEvents),
		"neighborhood_count": graphCount(neighborhood),
		"nodes":              nodes,
		"cards":              cards,
		"facts":              facts,
		"versions":           versions,
		"timeline":           timeline,
		"links":              links,
		"link_events":        linkEvents,
		"neighborhood":       neighborhood,
	}, err)
}

func graphCount(graph *knowledge.SourceGraphResult) int {
	if graph == nil {
		return 0
	}
	return graph.Count
}

func (a *App) toolKnowledgeListSourceLinks(args map[string]interface{}) string {
	sourceID := knowledgeToolStringArg(args, "source_id")
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "id")
	}
	if sourceID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_id argument"))
	}
	limit := knowledgeToolIntArg(args, "limit", 20)
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	links, err := a.KnowledgeListSourceLinks(sourceID, limit)
	if source, sourceErr := a.KnowledgeGetSource(sourceID); sourceErr == nil {
		links = knowledge.ProjectImageSourceLinksForParent(links, source.Kind == knowledge.SourceKindImage)
	}
	return knowledgeToolJSON(map[string]interface{}{
		"source_id":  sourceID,
		"link_count": len(links),
		"links":      links,
	}, err)
}

func (a *App) toolKnowledgeSourceGraph(args map[string]interface{}) string {
	limit := knowledgeToolIntArg(args, "limit", 100)
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	edgeLimit := knowledgeToolIntArg(args, "edge_limit", 500)
	if edgeLimit <= 0 {
		edgeLimit = 500
	}
	if edgeLimit > 2000 {
		edgeLimit = 2000
	}
	graph, err := a.KnowledgeSourceGraph(knowledgeToolListSourcesOptions(args, limit), edgeLimit)
	graph = knowledge.ProjectImageSourceGraphForTool(graph)
	return knowledgeToolJSON(map[string]interface{}{
		"graph": graph,
	}, err)
}

func (a *App) toolKnowledgeSourceNeighborhood(args map[string]interface{}) string {
	sourceID := knowledgeToolStringArg(args, "source_id")
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "id")
	}
	if sourceID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_id argument"))
	}
	depth := knowledgeToolIntArg(args, "depth", 1)
	if depth <= 0 {
		depth = 1
	}
	if depth > 3 {
		depth = 3
	}
	limit := knowledgeToolIntArg(args, "limit", 50)
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	edgeLimit := knowledgeToolIntArg(args, "edge_limit", 500)
	if edgeLimit <= 0 {
		edgeLimit = 500
	}
	if edgeLimit > 2000 {
		edgeLimit = 2000
	}
	graph, err := a.KnowledgeSourceNeighborhood(sourceID, depth, limit, edgeLimit)
	graph = knowledge.ProjectImageSourceGraphForTool(graph)
	return knowledgeToolJSON(map[string]interface{}{
		"source_id": sourceID,
		"graph":     graph,
	}, err)
}

func (a *App) toolKnowledgeSourcePath(args map[string]interface{}) string {
	fromSourceID := knowledgeToolStringArg(args, "from_source_id")
	if fromSourceID == "" {
		fromSourceID = knowledgeToolStringArg(args, "source_id")
	}
	if fromSourceID == "" {
		fromSourceID = knowledgeToolStringArg(args, "from")
	}
	toSourceID := knowledgeToolStringArg(args, "to_source_id")
	if toSourceID == "" {
		toSourceID = knowledgeToolStringArg(args, "target_source_id")
	}
	if toSourceID == "" {
		toSourceID = knowledgeToolStringArg(args, "to")
	}
	if fromSourceID == "" || toSourceID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing from_source_id or to_source_id argument"))
	}
	maxDepth := knowledgeToolIntArg(args, "max_depth", 4)
	if maxDepth <= 0 {
		maxDepth = 4
	}
	if maxDepth > 6 {
		maxDepth = 6
	}
	edgeLimit := knowledgeToolIntArg(args, "edge_limit", 1000)
	if edgeLimit <= 0 {
		edgeLimit = 1000
	}
	if edgeLimit > 5000 {
		edgeLimit = 5000
	}
	path, err := a.KnowledgeSourcePath(fromSourceID, toSourceID, maxDepth, edgeLimit)
	path = knowledge.ProjectImageSourcePathForTool(path)
	return knowledgeToolJSON(map[string]interface{}{
		"from_source_id": fromSourceID,
		"to_source_id":   toSourceID,
		"path":           path,
	}, err)
}

func (a *App) toolKnowledgePreviewTopicLinks(args map[string]interface{}) string {
	sourceID := knowledgeToolStringArg(args, "source_id")
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "id")
	}
	if sourceID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_id argument"))
	}
	limit := knowledgeToolIntArg(args, "limit", 8)
	if limit <= 0 {
		limit = 8
	}
	if limit > 50 {
		limit = 50
	}
	result, err := a.KnowledgePreviewSourceTopicLinks(sourceID, limit)
	result = knowledge.ProjectImageSourceTopicLinksForTool(result)
	return knowledgeToolJSON(map[string]interface{}{
		"source_id": sourceID,
		"preview":   result,
	}, err)
}

func (a *App) toolKnowledgeLinkSources(args map[string]interface{}) string {
	sourceID := knowledgeToolStringArg(args, "source_id")
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "id")
	}
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "from_source_id")
	}
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "from")
	}
	relatedSourceID := knowledgeToolStringArg(args, "related_source_id")
	if relatedSourceID == "" {
		relatedSourceID = knowledgeToolStringArg(args, "to_source_id")
	}
	if relatedSourceID == "" {
		relatedSourceID = knowledgeToolStringArg(args, "to")
	}
	if sourceID == "" || relatedSourceID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_id or related_source_id argument"))
	}
	link := knowledge.SourceLink{
		SourceID:        sourceID,
		RelatedSourceID: relatedSourceID,
		Relation:        knowledgeToolStringArg(args, "relation"),
		Score:           knowledgeToolFloatArg(args, "score", 1),
		Terms:           knowledgeToolStringSlice(args["terms"]),
		Evidence:        knowledgeToolStringSlice(args["evidence"]),
	}
	result, err := a.KnowledgeLinkSources(link)
	return knowledgeToolJSON(map[string]interface{}{
		"source_id":         sourceID,
		"related_source_id": relatedSourceID,
		"link":              result,
	}, err)
}

func (a *App) toolKnowledgeUnlinkSources(args map[string]interface{}) string {
	sourceID := knowledgeToolStringArg(args, "source_id")
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "id")
	}
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "from_source_id")
	}
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "from")
	}
	relatedSourceID := knowledgeToolStringArg(args, "related_source_id")
	if relatedSourceID == "" {
		relatedSourceID = knowledgeToolStringArg(args, "to_source_id")
	}
	if relatedSourceID == "" {
		relatedSourceID = knowledgeToolStringArg(args, "to")
	}
	if sourceID == "" || relatedSourceID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_id or related_source_id argument"))
	}
	relation := knowledgeToolStringArg(args, "relation")
	result, err := a.KnowledgeUnlinkSources(sourceID, relatedSourceID, relation)
	return knowledgeToolJSON(map[string]interface{}{
		"source_id":         sourceID,
		"related_source_id": relatedSourceID,
		"result":            result,
	}, err)
}

func (a *App) toolKnowledgeListSourceLinkEvents(args map[string]interface{}) string {
	sourceID := knowledgeToolStringArg(args, "source_id")
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "id")
	}
	if sourceID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_id argument"))
	}
	limit := knowledgeToolIntArg(args, "limit", 20)
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	events, err := a.KnowledgeListSourceLinkEvents(sourceID, limit)
	if source, sourceErr := a.KnowledgeGetSource(sourceID); sourceErr == nil {
		events = knowledge.ProjectImageSourceLinkEventsForParent(events, source.Kind == knowledge.SourceKindImage)
	}
	return knowledgeToolJSON(map[string]interface{}{
		"source_id":   sourceID,
		"event_count": len(events),
		"events":      events,
	}, err)
}

func (a *App) toolKnowledgeSourceTimeline(args map[string]interface{}) string {
	sourceID := knowledgeToolStringArg(args, "source_id")
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "id")
	}
	if sourceID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_id argument"))
	}
	limit := knowledgeToolIntArg(args, "limit", 50)
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	timeline, err := a.KnowledgeSourceTimeline(sourceID, limit)
	timeline = knowledge.ProjectImageSourceTimelineForTool(timeline)
	return knowledgeToolJSON(map[string]interface{}{
		"timeline": timeline,
	}, err)
}

func (a *App) toolKnowledgeSourceDigest(args map[string]interface{}) string {
	sourceID := knowledgeToolStringArg(args, "source_id")
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "id")
	}
	if sourceID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_id argument"))
	}
	nodeLimit := knowledgeToolIntArg(args, "nodes_limit", knowledgeToolIntArg(args, "limit", 8))
	cardLimit := knowledgeToolIntArg(args, "cards_limit", knowledgeToolIntArg(args, "limit", 8))
	factLimit := knowledgeToolIntArg(args, "facts_limit", knowledgeToolIntArg(args, "limit", 12))
	linkLimit := knowledgeToolIntArg(args, "links_limit", knowledgeToolIntArg(args, "limit", 8))
	digest, err := a.KnowledgeSourceDigest(sourceID, nodeLimit, cardLimit, factLimit, linkLimit)
	digest = knowledge.ProjectImageSourceDigestForTool(digest)
	return knowledgeToolJSON(map[string]interface{}{
		"digest": digest,
	}, err)
}

func (a *App) toolKnowledgeRefreshTopicLinks(args map[string]interface{}) string {
	limitPerSource := knowledgeToolIntArg(args, "limit_per_source", 8)
	if limitPerSource <= 0 {
		limitPerSource = 8
	}
	if limitPerSource > 50 {
		limitPerSource = 50
	}
	sourceID := knowledgeToolStringArg(args, "source_id")
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "id")
	}
	if sourceID != "" {
		result, err := a.KnowledgeRefreshSourceTopicLinks(sourceID, limitPerSource)
		result = knowledge.ProjectImageSourceTopicLinksForTool(result)
		return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
	}
	limit := knowledgeToolIntArg(args, "limit", 100)
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	result, err := a.KnowledgeRefreshSourceTopicLinksByFilter(knowledgeToolListSourcesOptions(args, limit), limitPerSource)
	result = knowledge.ProjectImageSourceTopicLinksForTool(result)
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgeListSourceVersions(args map[string]interface{}) string {
	sourceID := knowledgeToolStringArg(args, "source_id")
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "id")
	}
	if sourceID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_id argument"))
	}
	limit := knowledgeToolIntArg(args, "limit", 20)
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	versions, err := a.KnowledgeListSourceVersions(sourceID, limit)
	versions = knowledge.ProjectImageSourceVersionsForTool(versions)
	return knowledgeToolJSON(map[string]interface{}{
		"source_id":     sourceID,
		"version_count": len(versions),
		"versions":      versions,
	}, err)
}

func (a *App) toolKnowledgeListDuplicateCards(args map[string]interface{}) string {
	limit := knowledgeToolIntArg(args, "limit", 50)
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}
	groups, err := a.KnowledgeListDuplicateCards(limit)
	return knowledgeToolJSON(map[string]interface{}{"count": len(groups), "groups": groups}, err)
}

func (a *App) toolKnowledgeSuppressDuplicateCards(args map[string]interface{}) string {
	key := knowledgeToolStringArg(args, "key")
	if key == "" {
		key = knowledgeToolStringArg(args, "claim")
	}
	if key == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing key argument"))
	}
	result, err := a.KnowledgeSuppressDuplicateCards(knowledge.DuplicateCardSuppressionRequest{
		Key:         key,
		KeepCardID:  knowledgeToolStringArg(args, "keep_card_id"),
		OwnerID:     knowledgeToolStringArg(args, "owner_id"),
		TenantID:    knowledgeToolStringArg(args, "tenant_id"),
		ProjectPath: knowledgeToolStringArg(args, "project_path"),
		Reason:      knowledgeToolStringArg(args, "reason"),
	})
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgeSuppressDuplicateGroups(args map[string]interface{}) string {
	limit := knowledgeToolIntArg(args, "limit", 50)
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}
	projectPath := knowledgeToolStringArg(args, "project_path")
	ownerID := knowledgeToolStringArg(args, "owner_id")
	tenantID := knowledgeToolStringArg(args, "tenant_id")
	reason := knowledgeToolStringArg(args, "reason")
	if reason == "" {
		reason = "duplicate_card_claim_bulk"
	}
	groups, err := a.KnowledgeListDuplicateCards(limit)
	if err != nil {
		return knowledgeToolJSON(nil, err)
	}
	type suppressedGroupResult struct {
		Key        string `json:"key"`
		Claim      string `json:"claim,omitempty"`
		Suppressed int    `json:"suppressed"`
		KeptCardID string `json:"kept_card_id,omitempty"`
		Error      string `json:"error,omitempty"`
	}
	results := make([]suppressedGroupResult, 0, len(groups))
	processed := 0
	skipped := 0
	totalSuppressed := 0
	errors := make([]string, 0)
	for _, group := range groups {
		if projectPath != "" && group.ProjectPath != projectPath {
			skipped++
			continue
		}
		if ownerID != "" && group.OwnerID != ownerID {
			skipped++
			continue
		}
		if tenantID != "" && group.TenantID != tenantID {
			skipped++
			continue
		}
		result, suppressErr := a.KnowledgeSuppressDuplicateCards(knowledge.DuplicateCardSuppressionRequest{
			Key:         group.Key,
			OwnerID:     firstNonEmpty(ownerID, group.OwnerID),
			TenantID:    firstNonEmpty(tenantID, group.TenantID),
			ProjectPath: firstNonEmpty(projectPath, group.ProjectPath),
			Reason:      reason,
		})
		processed++
		item := suppressedGroupResult{Key: group.Key, Claim: group.Claim, Suppressed: result.Suppressed, KeptCardID: result.KeptCardID}
		if suppressErr != nil {
			item.Error = suppressErr.Error()
			errors = append(errors, fmt.Sprintf("%s: %v", group.Key, suppressErr))
		} else {
			totalSuppressed += result.Suppressed
		}
		results = append(results, item)
	}
	return knowledgeToolJSON(map[string]interface{}{
		"requested_groups": len(groups),
		"processed_groups": processed,
		"skipped_groups":   skipped,
		"suppressed":       totalSuppressed,
		"errors":           errors,
		"results":          results,
	}, nil)
}

func (a *App) toolKnowledgeListSuppressedCards(args map[string]interface{}) string {
	limit := knowledgeToolIntArg(args, "limit", 100)
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	items, err := a.KnowledgeListSuppressedCards(limit)
	return knowledgeToolJSON(map[string]interface{}{"count": len(items), "items": items}, err)
}

func (a *App) toolKnowledgeScanSensitive(args map[string]interface{}) string {
	limit := knowledgeToolIntArg(args, "limit", 100)
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	result, err := a.KnowledgeScanSensitiveContent(limit)
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgeDisableSensitiveSources(args map[string]interface{}) string {
	limit := knowledgeToolIntArg(args, "limit", 100)
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	result, err := a.KnowledgeDisableSensitiveSources(limit)
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgeRestoreSuppressedCards(args map[string]interface{}) string {
	cardIDs := knowledgeToolStringSlice(args["card_ids"])
	if len(cardIDs) == 0 {
		cardIDs = knowledgeToolStringSlice(args["ids"])
	}
	if len(cardIDs) == 0 {
		return knowledgeToolJSON(nil, fmt.Errorf("missing card_ids argument"))
	}
	result, err := a.KnowledgeRestoreSuppressedCards(cardIDs)
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgeRestoreSuppressedCardsBulk(args map[string]interface{}) string {
	limit := knowledgeToolIntArg(args, "limit", 100)
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	reasonContains := strings.ToLower(knowledgeToolStringArg(args, "reason_contains"))
	items, err := a.KnowledgeListSuppressedCards(limit)
	if err != nil {
		return knowledgeToolJSON(nil, err)
	}
	cardIDs := make([]string, 0, len(items))
	for _, item := range items {
		if reasonContains != "" && !strings.Contains(strings.ToLower(item.Reason), reasonContains) {
			continue
		}
		cardIDs = append(cardIDs, item.CardID)
	}
	result, err := a.KnowledgeRestoreSuppressedCards(cardIDs)
	return knowledgeToolJSON(map[string]interface{}{
		"loaded":    len(items),
		"requested": len(cardIDs),
		"result":    result,
	}, err)
}

func (a *App) toolKnowledgeUpdateSourceMetadata(args map[string]interface{}) string {
	sourceID := knowledgeToolStringArg(args, "source_id")
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "id")
	}
	if sourceID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_id argument"))
	}
	labels := knowledgeToolStringSlice(args["labels"])
	if knowledgeToolBoolArg(args, "clear_labels", false) {
		labels = []string{}
	}
	source, err := a.KnowledgeUpdateSourceMetadata(knowledge.SourceUpdateRequest{
		ID:          sourceID,
		Title:       knowledgeToolStringArg(args, "title"),
		TopicHint:   knowledgeToolStringArg(args, "topic_hint"),
		SourceTrust: knowledgeToolFloatArg(args, "source_trust", -1),
		Labels:      labels,
	})
	source = knowledge.ProjectImageSourceForTool(source)
	return knowledgeToolJSON(map[string]interface{}{"source": source}, err)
}

func (a *App) toolKnowledgeRefreshSource(args map[string]interface{}) string {
	sourceID := knowledgeToolStringArg(args, "source_id")
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "id")
	}
	if sourceID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_id argument"))
	}
	source, err := a.KnowledgeRefreshSource(sourceID)
	source = knowledge.ProjectImageSourceForTool(source)
	return knowledgeToolJSON(map[string]interface{}{"source": source}, err)
}

func (a *App) toolKnowledgePreviewSourceRefresh(args map[string]interface{}) string {
	sourceID := knowledgeToolStringArg(args, "source_id")
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "id")
	}
	if sourceID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_id argument"))
	}
	preview, err := a.KnowledgePreviewSourceRefresh(sourceID)
	preview = knowledge.ProjectImageSourceChangePreviewForTool(preview)
	return knowledgeToolJSON(map[string]interface{}{"preview": preview}, err)
}

func (a *App) toolKnowledgePreviewSourcesRefresh(args map[string]interface{}) string {
	ids := knowledgeToolStringSlice(args["source_ids"])
	if len(ids) == 0 {
		ids = knowledgeToolStringSlice(args["ids"])
	}
	if len(ids) == 0 {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_ids argument"))
	}
	result, err := a.KnowledgePreviewSourcesRefresh(ids)
	result = knowledge.ProjectImageSourceChangePreviewsForTool(result)
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgePreviewSourcesRefreshByFilter(args map[string]interface{}) string {
	limit := knowledgeToolSourceFilterLimit(args, 100, 500, 5000)
	result, err := a.KnowledgePreviewSourcesRefreshByFilter(knowledgeToolListSourcesOptions(args, limit))
	result = knowledge.ProjectImageSourceChangePreviewsForTool(result)
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgeRefreshChangedSources(args map[string]interface{}) string {
	ids := knowledgeToolStringSlice(args["source_ids"])
	if len(ids) == 0 {
		ids = knowledgeToolStringSlice(args["ids"])
	}
	if len(ids) == 0 {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_ids argument"))
	}
	result, err := a.KnowledgeRefreshChangedSources(ids)
	result.Preview = knowledge.ProjectImageSourceChangePreviewsForTool(result.Preview)
	result.Refresh = knowledge.ProjectImageSourceRefreshForTool(result.Refresh)
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgeRefreshChangedSourcesByFilter(args map[string]interface{}) string {
	limit := knowledgeToolSourceFilterLimit(args, 100, 500, 5000)
	result, err := a.KnowledgeRefreshChangedSourcesByFilter(knowledgeToolListSourcesOptions(args, limit))
	result.Preview = knowledge.ProjectImageSourceChangePreviewsForTool(result.Preview)
	result.Refresh = knowledge.ProjectImageSourceRefreshForTool(result.Refresh)
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgeRefreshSources(args map[string]interface{}) string {
	ids := knowledgeToolStringSlice(args["source_ids"])
	if len(ids) == 0 {
		ids = knowledgeToolStringSlice(args["ids"])
	}
	if len(ids) == 0 {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_ids argument"))
	}
	result, err := a.KnowledgeRefreshSources(ids)
	result = knowledge.ProjectImageSourceRefreshForTool(result)
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgeRefreshSourcesByFilter(args map[string]interface{}) string {
	limit := knowledgeToolSourceFilterLimit(args, 100, 500, 5000)
	result, err := a.KnowledgeRefreshSourcesByFilter(knowledgeToolListSourcesOptions(args, limit))
	result = knowledge.ProjectImageSourceRefreshForTool(result)
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgeRebuildSourceDerived(args map[string]interface{}) string {
	sourceID := knowledgeToolStringArg(args, "source_id")
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "id")
	}
	if sourceID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_id argument"))
	}
	source, err := a.KnowledgeRebuildSourceDerived(sourceID, knowledgeToolStringArg(args, "distill_mode"))
	source = knowledge.ProjectImageSourceForTool(source)
	return knowledgeToolJSON(map[string]interface{}{"source": source}, err)
}

func (a *App) toolKnowledgeRebuildSourcesDerived(args map[string]interface{}) string {
	ids := knowledgeToolStringSlice(args["source_ids"])
	if len(ids) == 0 {
		ids = knowledgeToolStringSlice(args["ids"])
	}
	if len(ids) == 0 {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_ids argument"))
	}
	result, err := a.KnowledgeRebuildSourcesDerived(ids, knowledgeToolStringArg(args, "distill_mode"))
	result = knowledge.ProjectImageSourceRebuildForTool(result)
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgeRebuildSourcesDerivedByFilter(args map[string]interface{}) string {
	limit := knowledgeToolSourceFilterLimit(args, 100, 500, 5000)
	result, err := a.KnowledgeRebuildSourcesDerivedByFilter(knowledgeToolListSourcesOptions(args, limit), knowledgeToolStringArg(args, "distill_mode"))
	result = knowledge.ProjectImageSourceRebuildForTool(result)
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgeDisableSourcesByFilter(args map[string]interface{}) string {
	limit := knowledgeToolSourceFilterLimit(args, 100, 500, 5000)
	result, err := a.KnowledgeDisableSourcesByFilter(knowledgeToolListSourcesOptions(args, limit))
	result = knowledge.ProjectImageSourceStatusUpdateForTool(result)
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgeEnableSourcesByFilter(args map[string]interface{}) string {
	limit := knowledgeToolSourceFilterLimit(args, 100, 500, 5000)
	result, err := a.KnowledgeEnableSourcesByFilter(knowledgeToolListSourcesOptions(args, limit))
	result = knowledge.ProjectImageSourceStatusUpdateForTool(result)
	return knowledgeToolJSON(map[string]interface{}{"result": result}, err)
}

func (a *App) toolKnowledgeDisableSource(args map[string]interface{}) string {
	sourceID := knowledgeToolStringArg(args, "source_id")
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "id")
	}
	if sourceID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_id argument"))
	}
	source, err := a.KnowledgeDisableSource(sourceID)
	source = knowledge.ProjectImageSourceForTool(source)
	return knowledgeToolJSON(map[string]interface{}{"source": source, "disabled": err == nil}, err)
}

func (a *App) toolKnowledgeEnableSource(args map[string]interface{}) string {
	sourceID := knowledgeToolStringArg(args, "source_id")
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "id")
	}
	if sourceID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_id argument"))
	}
	source, err := a.KnowledgeEnableSource(sourceID)
	source = knowledge.ProjectImageSourceForTool(source)
	return knowledgeToolJSON(map[string]interface{}{"source": source, "enabled": err == nil}, err)
}

func (a *App) toolKnowledgeDeleteSource(args map[string]interface{}) string {
	sourceID := knowledgeToolStringArg(args, "source_id")
	if sourceID == "" {
		sourceID = knowledgeToolStringArg(args, "id")
	}
	if sourceID == "" {
		return knowledgeToolJSON(nil, fmt.Errorf("missing source_id argument"))
	}
	err := a.KnowledgeDeleteSource(sourceID)
	return knowledgeToolJSON(map[string]interface{}{"deleted": err == nil, "source_id": sourceID}, err)
}

func knowledgeToolStringSlice(value interface{}) []string {
	unique := func(items []string) []string {
		result := make([]string, 0, len(items))
		seen := map[string]struct{}{}
		for _, item := range items {
			for _, part := range strings.FieldsFunc(item, isKnowledgeToolListSeparator) {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				if _, ok := seen[part]; ok {
					continue
				}
				seen[part] = struct{}{}
				result = append(result, part)
			}
		}
		return result
	}
	switch typed := value.(type) {
	case []string:
		return unique(typed)
	case []interface{}:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				items = append(items, text)
			}
		}
		return unique(items)
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		parts := strings.FieldsFunc(typed, isKnowledgeToolListSeparator)
		return unique(parts)
	default:
		return nil
	}
}

func isKnowledgeToolListSeparator(r rune) bool {
	switch r {
	case ',', ';', '\n', '\r', '\t', '\uFF0C', '\uFF1B', '\u3001':
		return true
	default:
		return false
	}
}

func knowledgeToolFilePathSlice(value interface{}) []string {
	unique := func(items []string) []string {
		result := make([]string, 0, len(items))
		seen := map[string]struct{}{}
		for _, item := range items {
			for _, part := range strings.FieldsFunc(item, isKnowledgeToolFilePathSeparator) {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				if _, ok := seen[part]; ok {
					continue
				}
				seen[part] = struct{}{}
				result = append(result, part)
			}
		}
		return result
	}
	switch typed := value.(type) {
	case []string:
		return unique(typed)
	case []interface{}:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				items = append(items, text)
			}
		}
		return unique(items)
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return unique([]string{typed})
	default:
		return nil
	}
}

func isKnowledgeToolFilePathSeparator(r rune) bool {
	return r == '\n' || r == '\r' || r == '\t'
}

func knowledgeToolFirstString(args map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if args == nil {
			return ""
		}
		switch typed := args[key].(type) {
		case string:
			if value := strings.TrimSpace(typed); value != "" {
				return value
			}
		case []string:
			for _, item := range typed {
				if value := strings.TrimSpace(item); value != "" {
					return value
				}
			}
		case []interface{}:
			for _, item := range typed {
				if text, ok := item.(string); ok {
					if value := strings.TrimSpace(text); value != "" {
						return value
					}
				}
			}
		}
	}
	return ""
}

func knowledgeToolStringArg(args map[string]interface{}, key string) string {
	return knowledgeToolFirstString(args, key)
}

func knowledgeToolFirstScalarValue(value interface{}) (interface{}, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, false
	case []interface{}:
		for _, item := range typed {
			if value, ok := knowledgeToolFirstScalarValue(item); ok {
				return value, true
			}
		}
	case []string:
		for _, item := range typed {
			if value := strings.TrimSpace(item); value != "" {
				return value, true
			}
		}
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil, false
		}
		return typed, true
	default:
		return typed, true
	}
	return nil, false
}

func knowledgeToolBoolArg(args map[string]interface{}, key string, fallback bool) bool {
	if args == nil {
		return fallback
	}
	value, ok := knowledgeToolFirstScalarValue(args[key])
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		if value, ok := coerceToolBoolToken(typed); ok {
			return value
		}
	}
	return fallback
}

func knowledgeToolIntArg(args map[string]interface{}, key string, fallback int) int {
	if args == nil {
		return fallback
	}
	value, ok := knowledgeToolFirstScalarValue(args[key])
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed)
		}
		if parsed, err := typed.Float64(); err == nil {
			return int(parsed)
		}
	case string:
		if parsed, err := parseCraftInt(typed); err == nil {
			return parsed
		}
	}
	return fallback
}

func knowledgeToolURLList(value interface{}) []string {
	raw := knowledgeToolStringSlice(value)
	result := make([]string, 0, len(raw))
	seen := map[string]struct{}{}
	for _, item := range raw {
		for _, part := range strings.FieldsFunc(item, isKnowledgeToolListSeparator) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, ok := seen[part]; ok {
				continue
			}
			seen[part] = struct{}{}
			result = append(result, part)
		}
	}
	return result
}

func knowledgeToolUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func knowledgeToolSourceIDs(args map[string]interface{}) []string {
	sourceIDs := knowledgeToolStringSlice(args["source_ids"])
	if len(sourceIDs) == 0 {
		sourceIDs = knowledgeToolStringSlice(args["ids"])
	}
	return sourceIDs
}

func knowledgeToolSourceFilterLimit(args map[string]interface{}, fallback int, max int, explicitSourceIDsMax int) int {
	limit := knowledgeToolIntArg(args, "limit", fallback)
	if limit <= 0 {
		limit = fallback
	}
	if max > 0 && limit > max {
		limit = max
	}
	if explicitCount := len(knowledgeToolSourceIDs(args)); explicitCount > limit {
		limit = explicitCount
		if explicitSourceIDsMax > 0 && limit > explicitSourceIDsMax {
			limit = explicitSourceIDsMax
		}
	}
	return limit
}

func knowledgeToolStringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func knowledgeToolStringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}

func knowledgeToolStringSetIntersects(set map[string]struct{}, values []string) bool {
	for _, value := range values {
		if _, ok := set[strings.TrimSpace(value)]; ok {
			return true
		}
	}
	return false
}

func knowledgeQualityFilterSchema(limitDescription string) map[string]interface{} {
	return map[string]interface{}{
		"query":             map[string]string{"type": "string", "description": "Optional source keyword filter"},
		"search_scope":      map[string]string{"type": "string", "description": "all | project | personal. Default all."},
		"kind":              map[string]string{"type": "string", "description": "Optional source kind filter"},
		"source_kinds":      map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional source kind filters"},
		"status":            map[string]string{"type": "string", "description": "Optional source status filter"},
		"coverage_filter":   map[string]string{"type": "string", "description": "Optional coverage filter before quality scoring"},
		"labels":            map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional labels that sources must have"},
		"source_ids":        knowledgeSourceIDsSchema("Optional source IDs to inspect"),
		"ids":               knowledgeSourceIDsAliasSchema(),
		"quality_grade":     map[string]string{"type": "string", "description": "Optional quality grade filter: excellent, good, needs_attention, poor"},
		"quality_grades":    map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional quality grade filters"},
		"min_quality_score": map[string]string{"type": "integer", "description": "Optional minimum local quality score"},
		"max_quality_score": map[string]string{"type": "integer", "description": "Optional maximum local quality score"},
		"limit":             map[string]string{"type": "integer", "description": limitDescription},
	}
}

func knowledgeSourceIDsSchema(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "array",
		"items":       map[string]string{"type": "string"},
		"description": description,
	}
}

func knowledgeSourceIDsAliasSchema() map[string]interface{} {
	return knowledgeSourceIDsSchema("Alias for source_ids.")
}

func knowledgeSourceIDAliasSchema() map[string]string {
	return map[string]string{"type": "string", "description": "Alias for source_id."}
}

func knowledgeStringAliasSchema(description string) map[string]string {
	return map[string]string{"type": "string", "description": description}
}

func knowledgeStringArrayAliasSchema(description string) map[string]interface{} {
	return map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": description}
}

func mergeKnowledgeSchemas(base map[string]interface{}, extra map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(base)+len(extra))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range extra {
		result[key] = value
	}
	return result
}

func knowledgeToolListSourcesOptions(args map[string]interface{}, limit int) knowledge.ListSourcesOptions {
	return knowledge.ListSourcesOptions{
		OwnerID:         knowledgeToolStringArg(args, "owner_id"),
		TenantID:        knowledgeToolStringArg(args, "tenant_id"),
		SearchScope:     knowledgeToolStringArg(args, "search_scope"),
		ProjectPath:     knowledgeToolStringArg(args, "project_path"),
		SourceIDs:       knowledgeToolSourceIDs(args),
		Status:          knowledgeToolStringArg(args, "status"),
		IncludeDisabled: knowledgeToolBoolArg(args, "include_disabled", false),
		Kind:            knowledgeToolStringArg(args, "kind"),
		SourceKinds:     knowledgeToolStringSlice(args["source_kinds"]),
		Domain:          knowledgeToolStringArg(args, "domain"),
		Label:           knowledgeToolStringArg(args, "label"),
		Labels:          knowledgeToolStringSlice(args["labels"]),
		Query:           knowledgeToolStringArg(args, "query"),
		CoverageFilter:  knowledgeToolStringArg(args, "coverage_filter"),
		QualityGrade:    knowledgeToolStringArg(args, "quality_grade"),
		QualityGrades:   knowledgeToolStringSlice(args["quality_grades"]),
		MinQualityScore: knowledgeToolIntArg(args, "min_quality_score", 0),
		MaxQualityScore: knowledgeToolIntArg(args, "max_quality_score", 0),
		Limit:           limit,
	}
}

func hasKnowledgeSourceFilterArgs(args map[string]interface{}) bool {
	for _, key := range []string{"query", "search_scope", "kind", "source_kinds", "source_ids", "ids", "status", "domain", "label", "labels", "coverage_filter", "quality_grade", "quality_grades", "min_quality_score", "max_quality_score", "project_path", "owner_id", "tenant_id"} {
		if _, ok := args[key]; ok {
			return true
		}
	}
	return false
}

func knowledgeToolHealthStatus(score int, fallback string) string {
	switch {
	case score >= 90:
		return "excellent"
	case score >= 75:
		return "good"
	case score >= 60:
		return "needs_attention"
	case score > 0:
		return "poor"
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return "empty"
}

func knowledgeToolDoctorFindingSummary(findings []knowledge.DoctorFinding) map[string]int {
	summary := map[string]int{}
	for _, finding := range findings {
		severity := strings.TrimSpace(finding.Severity)
		if severity == "" {
			severity = "info"
		}
		summary[severity]++
	}
	return summary
}

func knowledgeToolTopDoctorFindings(findings []knowledge.DoctorFinding, limit int) []map[string]interface{} {
	if limit <= 0 || len(findings) == 0 {
		return nil
	}
	if limit > len(findings) {
		limit = len(findings)
	}
	result := make([]map[string]interface{}, 0, limit)
	for _, finding := range findings[:limit] {
		item := map[string]interface{}{
			"code":     finding.Code,
			"severity": finding.Severity,
			"title":    finding.Title,
		}
		if finding.Count > 0 {
			item["count"] = finding.Count
		}
		if finding.Action != "" {
			item["action"] = finding.Action
		}
		if len(finding.SourceIDs) > 0 {
			item["source_ids"] = finding.SourceIDs
		}
		result = append(result, item)
	}
	return result
}

func knowledgeToolHealthRecommendedTools(findings []knowledge.DoctorFinding) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, finding := range findings {
		action := strings.TrimSpace(finding.Action)
		if action == "" {
			continue
		}
		if _, ok := seen[action]; ok {
			continue
		}
		seen[action] = struct{}{}
		result = append(result, action)
	}
	return result
}

func knowledgeToolQualityNeedsMaintenance(report knowledge.SourceQualityReport) bool {
	if report.Count == 0 {
		return false
	}
	for _, grade := range []string{"needs_attention", "poor"} {
		if report.Grades[grade] > 0 {
			return true
		}
	}
	return len(report.Actions) > 0
}

func knowledgeToolMaintenanceActionSummary(actions []knowledge.SourceQualityMaintenanceAction) []map[string]interface{} {
	if len(actions) == 0 {
		return nil
	}
	result := make([]map[string]interface{}, 0, len(actions))
	for _, action := range actions {
		item := map[string]interface{}{
			"kind":        action.Kind,
			"title":       action.Title,
			"description": action.Description,
			"severity":    action.Severity,
			"count":       action.Count,
			"tool":        action.Tool,
			"executable":  knowledgeToolMaintenanceActionExecutable(action.Kind),
		}
		if !knowledgeToolMaintenanceActionExecutable(action.Kind) {
			item["manual_reason"] = knowledgeToolMaintenanceActionManualReason(action.Kind)
		}
		if len(action.SourceIDs) > 0 {
			item["source_ids"] = action.SourceIDs
		}
		if len(action.Signals) > 0 {
			item["signals"] = action.Signals
		}
		result = append(result, item)
	}
	return result
}

func knowledgeToolMaintenanceActionExecutable(kind string) bool {
	return normalizeKnowledgeMaintenanceActionKind(kind).IsExecutable()
}

func knowledgeToolMaintenanceActionManualReason(kind string) string {
	return normalizeKnowledgeMaintenanceActionKind(kind).ManualReason()
}

func knowledgeToolFloatArg(args map[string]interface{}, key string, fallback float64) float64 {
	if args == nil {
		return fallback
	}
	value, ok := knowledgeToolFirstScalarValue(args[key])
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err == nil {
			return parsed
		}
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func knowledgeToolJSON(payload map[string]interface{}, err error) string {
	if payload == nil {
		payload = map[string]interface{}{}
	}
	if err != nil {
		payload["ok"] = false
		payload["error"] = err.Error()
	} else {
		payload["ok"] = true
	}
	data, marshalErr := json.MarshalIndent(payload, "", "  ")
	if marshalErr != nil {
		return fmt.Sprintf("{\"ok\":false,\"error\":%q}", marshalErr.Error())
	}
	return string(data)
}
