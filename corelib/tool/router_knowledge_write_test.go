package tool

import "testing"

func TestKnowledgeWriteToolsForPayload_TextOnly(t *testing.T) {
	tools := knowledgeWriteToolsForPayload("save this note into the knowledge base")
	if !tools["knowledge_save_text"] || len(tools) != 1 {
		t.Fatalf("expected text-only save tool, got %v", tools)
	}
}

func TestKnowledgeWriteToolsForPayload_URLDoesNotLookLikeLocalPath(t *testing.T) {
	tools := knowledgeWriteToolsForPayload("Archive https://example.com/research into the knowledge base")
	if !tools["knowledge_save_url"] || !tools["knowledge_save_urls"] {
		t.Fatalf("expected URL save tools, got %v", tools)
	}
	if tools["knowledge_import_files"] || tools["knowledge_import_directory"] || tools["knowledge_save_text"] {
		t.Fatalf("URL payload routed unrelated knowledge writers: %v", tools)
	}
}

func TestKnowledgeWriteToolsForPayload_WindowsFileWithSpaces(t *testing.T) {
	tools := knowledgeWriteToolsForPayload(`Archive D:\case files\evidence final.pdf into the knowledge base`)
	if !tools["knowledge_import_files"] {
		t.Fatalf("expected file import for Windows path containing spaces, got %v", tools)
	}
	if tools["knowledge_import_directory"] || tools["knowledge_save_text"] {
		t.Fatalf("file payload routed unrelated knowledge writers: %v", tools)
	}
}

func TestKnowledgeWriteToolsForPayload_WindowsUnknownExtensionWithSpaces(t *testing.T) {
	tools := knowledgeWriteToolsForPayload("Archive this selected path:\nD:\\case files\\evidence final.pages")
	if !tools["knowledge_import_files"] {
		t.Fatalf("expected file import for selected Windows file path containing spaces, got %v", tools)
	}
	if tools["knowledge_import_directory"] || tools["knowledge_save_text"] {
		t.Fatalf("selected file payload routed unrelated knowledge writers: %v", tools)
	}
}

func TestKnowledgeWriteToolsForPayload_MixedURLAndFile(t *testing.T) {
	tools := knowledgeWriteToolsForPayload(`Archive https://example.com/research and D:\cases\evidence.pdf into the knowledge base`)
	if !tools["knowledge_save_url"] || !tools["knowledge_import_files"] {
		t.Fatalf("expected URL and file knowledge writers, got %v", tools)
	}
	if tools["knowledge_save_text"] || tools["knowledge_import_directory"] {
		t.Fatalf("mixed URL/file payload routed unrelated knowledge writers: %v", tools)
	}
}
