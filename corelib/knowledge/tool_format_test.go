package knowledge

import (
	"strings"
	"testing"
)

func TestFormatSearchResultsForLLMMarksImageEvidence(t *testing.T) {
	out := FormatSearchResultsForLLM([]SearchResult{{
		ResultType: "node",
		NodeType:   NodeTypeImage,
		NodeTitle:  "Architecture",
		Source:     Source{ID: "source-image", Kind: SourceKindImage, Title: "Architecture image"},
	}})
	if !strings.Contains(out, "**Image**") {
		t.Fatalf("formatted image result did not identify image evidence: %q", out)
	}
}

func TestFormatSearchResultsForLLMDoesNotExposeImageFileURI(t *testing.T) {
	privatePath := `C:\private\knowledge_assets\diagram.png`
	out := FormatSearchResultsForLLM([]SearchResult{{
		ResultType: "node",
		NodeType:   NodeTypeImage,
		NodeTitle:  "Gateway diagram",
		Source:     Source{ID: "image-source", Kind: SourceKindImage, URI: privatePath},
	}})
	if strings.Contains(out, privatePath) || strings.Contains(out, "knowledge_assets") {
		t.Fatalf("image tool result leaked local URI: %q", out)
	}
	if !strings.Contains(out, "image-source") || !strings.Contains(out, "Gateway diagram") {
		t.Fatalf("image tool result lost safe evidence: %q", out)
	}
}

func TestFormatSourceAndCitationLabelsDoNotExposeImageFileURI(t *testing.T) {
	privatePath := `C:\private\knowledge_assets\diagram.png`
	r := SearchResult{
		NodeType:  NodeTypeImage,
		NodeTitle: "Gateway diagram",
		Citation:  privatePath,
		Source: Source{
			ID:           "image-source",
			Kind:         SourceKindImage,
			URI:          privatePath,
			CanonicalURI: "file://" + privatePath,
			RelativePath: privatePath,
		},
	}
	for name, label := range map[string]string{
		"source":   FormatSourceLabel(r),
		"citation": FormatCitationLabel(r),
	} {
		if strings.Contains(label, privatePath) || strings.Contains(label, "file://") {
			t.Fatalf("%s label leaked image path: %q", name, label)
		}
	}
	if got := FormatSourceLabel(r); got != "image-source" {
		t.Fatalf("FormatSourceLabel(image) = %q, want opaque source ID", got)
	}
}

func TestFormatImageLabelsRejectAbsolutePathMetadataAcrossPlatforms(t *testing.T) {
	for _, privatePath := range []string{
		`C:\\private\\knowledge_assets\\diagram.png`,
		`/private/knowledge_assets/diagram.png`,
		`\\\\server\\share\\diagram.png`,
	} {
		r := SearchResult{
			NodeType:  NodeTypeImage,
			NodeTitle: privatePath,
			Source: Source{
				ID:           "safe-image-id",
				Kind:         SourceKindImage,
				Title:        privatePath,
				RelativePath: privatePath,
			},
		}
		if got := FormatImageSourceLabel(r); got != "safe-image-id" {
			t.Fatalf("FormatImageSourceLabel(%q) = %q, want safe source ID", privatePath, got)
		}
		if got := FormatImageCitationLabel(r); got != "image evidence" {
			t.Fatalf("FormatImageCitationLabel(%q) = %q, want path-free fallback", privatePath, got)
		}
		out := FormatSearchResultsForLLM([]SearchResult{r})
		if strings.Contains(out, privatePath) {
			t.Fatalf("formatted image result leaked path %q: %s", privatePath, out)
		}
	}
}

func TestSafeImageDisplayTextRejectsEmbeddedHostPaths(t *testing.T) {
	for _, value := range []string{
		"imported from C:\\private\\knowledge_assets\\diagram.png",
		"source=/private/knowledge_assets/diagram.png",
		"hosted at \\\\server\\share\\diagram.png",
		"file://C:/private/knowledge_assets/diagram.png",
	} {
		if got := SafeImageDisplayText(value); got != "" {
			t.Fatalf("SafeImageDisplayText(%q) = %q, want empty", value, got)
		}
	}
	if got := SafeImageDisplayText("Gateway architecture diagram"); got != "Gateway architecture diagram" {
		t.Fatalf("safe display text changed: %q", got)
	}
}

func TestProjectImageExplainForToolRemovesPathBearingResultAndCitationFields(t *testing.T) {
	privatePath := `C:\\private\\knowledge_assets\\diagram.png`
	result := SearchResult{
		ResultType: "node",
		NodeType:   NodeTypeImage,
		NodeID:     "image-node",
		NodeTitle:  privatePath,
		Source: Source{
			ID:           "safe-image-id",
			Kind:         SourceKindImage,
			URI:          privatePath,
			CanonicalURI: "file://" + privatePath,
			RelativePath: privatePath,
			Title:        privatePath,
			ProjectPath:  privatePath,
			ErrorMessage: privatePath,
		},
		Media: &SearchResultMedia{
			AssetID:      "safe-image-asset",
			ThumbnailURL: "file://" + privatePath + "/thumbnail",
			PreviewURL:   privatePath + "/preview",
			OriginalURL:  privatePath + "/original",
			Alt:          privatePath,
			Caption:      privatePath,
		},
	}
	explain := ProjectImageExplainForTool(ExplainResult{
		Results:   []SearchResult{result},
		Citations: []Citation{citationFromResult(result)},
	})
	if len(explain.Results) != 1 || len(explain.Citations) != 1 {
		t.Fatalf("projection result = %#v", explain)
	}
	projected, citation := explain.Results[0], explain.Citations[0]
	if projected.Source.ID != "safe-image-id" || citation.SourceID != "safe-image-id" {
		t.Fatalf("projection lost safe image identity: result=%#v citation=%#v", projected, citation)
	}
	if projected.Media == nil || projected.Media.AssetID != "safe-image-asset" {
		t.Fatalf("projection lost safe managed asset identity: %#v", projected.Media)
	}
	for _, value := range []string{
		projected.Source.URI, projected.Source.CanonicalURI, projected.Source.RelativePath, projected.Source.ProjectPath, projected.Source.ErrorMessage,
		projected.Source.Title, projected.NodeTitle, projected.Citation,
		projected.Media.ThumbnailURL, projected.Media.PreviewURL, projected.Media.OriginalURL, projected.Media.Alt, projected.Media.Caption,
		citation.SourceTitle, citation.URI, citation.RelativePath, citation.Label,
	} {
		if strings.Contains(value, privatePath) || strings.Contains(value, "file://") {
			t.Fatalf("image explain projection leaked path in %q", value)
		}
	}
}

func TestProjectImageSearchResultForToolDropsInvalidOrPathBearingMedia(t *testing.T) {
	privatePath := `C:\\private\\knowledge_assets\\diagram.png`
	for _, assetID := range []string{"../private-image", " safe-image-id", "safe-image-id "} {
		result := ProjectImageSearchResultForTool(SearchResult{
			NodeType: NodeTypeImage,
			Source:   Source{ID: "safe-image-id", Kind: SourceKindImage},
			Media: &SearchResultMedia{
				AssetID:      assetID,
				ThumbnailURL: privatePath,
				Caption:      "file://" + privatePath,
			},
		})
		if result.Media != nil {
			t.Fatalf("unsafe image media %q survived tool projection: %#v", assetID, result.Media)
		}
	}
}

func TestProjectImageSearchResultForToolDropsCrossSourceAssetID(t *testing.T) {
	result := ProjectImageSearchResultForTool(SearchResult{
		NodeType: NodeTypeImage,
		Source:   Source{ID: "document-source", Kind: SourceKindDOCX},
		Media:    &SearchResultMedia{AssetID: "other-source_embedded-image"},
	})
	if result.Media != nil {
		t.Fatalf("cross-source image asset survived tool projection: %#v", result.Media)
	}
}

func TestProjectImageDocumentNodesForToolDropsWhitespacePaddedAssetID(t *testing.T) {
	nodes := ProjectImageDocumentNodesForTool([]DocumentNode{{
		Type:     NodeTypeImage,
		Title:    "Architecture diagram",
		Metadata: map[string]string{MetaImageAssetID: " safe-image-id "},
	}})
	if len(nodes) != 1 || nodes[0].Metadata != nil {
		t.Fatalf("whitespace-padded asset ID survived document-node projection: %#v", nodes)
	}
}

func TestImageSourceProjectionsRemovePathBearingSourceModels(t *testing.T) {
	privatePath := `C:\private\knowledge_assets\diagram.png`
	imageSource := Source{
		ID:           "image-source",
		Kind:         SourceKindImage,
		Title:        "imported from " + privatePath,
		URI:          privatePath,
		CanonicalURI: "file://" + privatePath,
		RelativePath: privatePath,
		ProjectPath:  privatePath,
		ErrorMessage: "failed to inspect " + privatePath,
	}
	assertPathFree := func(value string) {
		t.Helper()
		if strings.Contains(value, privatePath) || strings.Contains(value, "file://") {
			t.Fatalf("image source projection leaked path in %q", value)
		}
	}

	projectedSource := ProjectImageSourceForTool(imageSource)
	for _, value := range []string{projectedSource.Title, projectedSource.URI, projectedSource.CanonicalURI, projectedSource.RelativePath, projectedSource.ProjectPath, projectedSource.ErrorMessage} {
		assertPathFree(value)
	}

	graph := ProjectImageSourceGraphForTool(SourceGraphResult{Nodes: []SourceGraphNode{{
		ID: "image-source", Kind: SourceKindImage, Label: privatePath, URI: privatePath, RelativePath: privatePath, ProjectPath: privatePath,
	}}, Edges: []SourceGraphEdge{{SourceID: "image-source", RelatedSourceID: "text-source", Evidence: []string{"from " + privatePath}}}})
	if graph.Nodes[0].Label != "image-source" || graph.Nodes[0].URI != "" || graph.Nodes[0].RelativePath != "" || graph.Nodes[0].ProjectPath != "" || len(graph.Edges[0].Evidence) != 0 {
		t.Fatalf("image graph projection = %#v", graph)
	}

	digest := ProjectImageSourceDigestForTool(SourceDigestResult{
		Source: imageSource,
		Title:  privatePath,
		Nodes: []DocumentNode{{
			Type: NodeTypeImage, Title: privatePath,
			Metadata: map[string]string{MetaImageAssetID: "safe-asset", "import_path": privatePath},
		}},
	})
	if digest.Title != "image-source" || digest.Nodes[0].Title != "" || len(digest.Nodes[0].Metadata) != 1 || digest.Nodes[0].Metadata[MetaImageAssetID] != "safe-asset" {
		t.Fatalf("image digest projection = %#v", digest)
	}

	timeline := ProjectImageSourceTimelineForTool(SourceTimelineResult{Source: imageSource, Events: []SourceTimelineEvent{{Title: privatePath, Detail: "at " + privatePath}}})
	assertPathFree(timeline.Events[0].Title)
	assertPathFree(timeline.Events[0].Detail)

	quality := ProjectImageSourceQualityForTool(SourceQualityReport{Items: []SourceQualityItem{{Source: imageSource}}})
	assertPathFree(quality.Items[0].Source.URI)

	preview := ProjectImageSourceChangePreviewForTool(SourceChangePreview{Source: imageSource, NextSource: imageSource, Samples: []SourceChangeSample{{Title: privatePath, Snippet: "from " + privatePath}}})
	assertPathFree(preview.Source.URI)
	assertPathFree(preview.NextSource.URI)
	assertPathFree(preview.Samples[0].Title)
	assertPathFree(preview.Samples[0].Snippet)

	links := ProjectImageSourceLinksForTool([]SourceLink{{RelatedSource: imageSource, Evidence: []string{"from " + privatePath}}})
	assertPathFree(links[0].RelatedSource.URI)
	if len(links[0].Evidence) != 0 {
		t.Fatalf("image source link retained path-bearing evidence: %#v", links[0])
	}
	topicLinks := ProjectImageSourceTopicLinksForTool(SourceTopicLinkBuildResult{Links: links})
	if len(topicLinks.Links[0].Evidence) != 0 {
		t.Fatalf("topic link projection retained path-bearing evidence: %#v", topicLinks)
	}
	events := ProjectImageSourceLinkEventsForParent([]SourceLinkEvent{{Note: privatePath, Evidence: []string{"from " + privatePath}}}, true)
	assertPathFree(events[0].Note)
	if len(events[0].Evidence) != 0 {
		t.Fatalf("link event projection retained path-bearing evidence: %#v", events[0])
	}

	refresh := ProjectImageSourceRefreshForTool(SourceRefreshResult{
		Sources:  []Source{imageSource},
		Failures: []SourceRefreshFailure{{Error: "failed at " + privatePath}},
		Warnings: []string{"warn " + privatePath},
	})
	assertPathFree(refresh.Sources[0].URI)

	rebuild := ProjectImageSourceRebuildForTool(SourceRebuildResult{Sources: []Source{imageSource}, Failures: []SourceRebuildFailure{{Error: privatePath}}})
	assertPathFree(rebuild.Sources[0].URI)

	status := ProjectImageSourceStatusUpdateForTool(SourceStatusUpdateResult{Sources: []Source{imageSource}, Failures: []SourceStatusUpdateFailure{{Error: privatePath}}})
	assertPathFree(status.Sources[0].URI)

	versions := ProjectImageSourceVersionsForTool([]SourceVersion{{Kind: SourceKindImage, URI: privatePath, CanonicalURI: "file://" + privatePath, Title: "from " + privatePath, Reason: privatePath}})
	assertPathFree(versions[0].URI)
	assertPathFree(versions[0].CanonicalURI)
	assertPathFree(versions[0].Title)
	assertPathFree(versions[0].Reason)
}

func TestBestContentText_CardPrefersClaim(t *testing.T) {
	r := SearchResult{
		ResultType: "card",
		Claim:      "api2 服务器: api2.maclaw.top, 用户名 root, 密码 sunion123",
		Summary:    "api2 服务器信息",
		Snippet:    "api2...服务器",
	}
	got := BestContentText(r)
	if got != r.Claim {
		t.Fatalf("card with all fields: expected Claim, got %q", got)
	}
}

func TestBestContentText_CardFallsToSummaryWhenClaimEmpty(t *testing.T) {
	r := SearchResult{
		ResultType: "card",
		Summary:    "服务器登录信息汇总",
		Snippet:    "api2...服务器",
	}
	got := BestContentText(r)
	if got != r.Summary {
		t.Fatalf("card without claim: expected Summary, got %q", got)
	}
}

func TestBestContentText_CardFallsToSnippetWhenClaimAndSummaryEmpty(t *testing.T) {
	r := SearchResult{
		ResultType: "card",
		Snippet:    "api2...服务器",
	}
	got := BestContentText(r)
	if got != r.Snippet {
		t.Fatalf("card with only snippet: expected Snippet, got %q", got)
	}
}

func TestBestContentText_FactPrefersClaim(t *testing.T) {
	r := SearchResult{
		ResultType: "fact",
		Claim:      "马勇博士共有 3 项发明专利。",
		Summary:    "专利信息",
		Subject:    "马勇",
		Predicate:  "拥有",
		Object:     "3项专利",
	}
	got := BestContentText(r)
	if got != r.Claim {
		t.Fatalf("fact with claim: expected Claim, got %q", got)
	}
}

func TestBestContentText_FactFallsToTriple(t *testing.T) {
	r := SearchResult{
		ResultType: "fact",
		Subject:    "api2",
		Predicate:  "密码是",
		Object:     "sunion123",
	}
	got := BestContentText(r)
	expected := "api2 密码是 sunion123"
	if got != expected {
		t.Fatalf("fact with only triple: expected %q, got %q", expected, got)
	}
}

func TestBestContentText_NodePrefersSnippet(t *testing.T) {
	r := SearchResult{
		ResultType: "node",
		Snippet:    "...部署在 api2.maclaw.top 上运行的 OmniRoute 容器...",
		Claim:      "完整的部署文档内容，很长很长",
		Summary:    "部署文档摘要",
	}
	got := BestContentText(r)
	if got != r.Snippet {
		t.Fatalf("node with all fields: expected Snippet (FTS highlight), got %q", got)
	}
}

func TestBestContentText_NodeFallsToSummaryWhenSnippetEmpty(t *testing.T) {
	r := SearchResult{
		ResultType: "node",
		Claim:      "完整内容",
		Summary:    "文档摘要",
	}
	got := BestContentText(r)
	if got != r.Summary {
		t.Fatalf("node without snippet: expected Summary, got %q", got)
	}
}

func TestBestContentText_EmptyResultTypeDefaultsToCardBehavior(t *testing.T) {
	// ResultType="" (not set) should behave like card — Claim first.
	r := SearchResult{
		Claim:   "完整内容",
		Snippet: "短片段",
	}
	got := BestContentText(r)
	if got != r.Claim {
		t.Fatalf("empty ResultType: expected Claim (card default), got %q", got)
	}
}

func TestBestContentText_AllEmpty(t *testing.T) {
	r := SearchResult{ResultType: "card"}
	got := BestContentText(r)
	if got != "" {
		t.Fatalf("all empty: expected empty string, got %q", got)
	}
}

func TestBestContentText_SnippetNotUsedWhenClaimAvailableForCard(t *testing.T) {
	// This is the exact bug scenario: FTS snippet is 14 chars, Claim has full content.
	r := SearchResult{
		ResultType: "card",
		Claim:      "api1 服务器: api1.maclaw.top, 用户名 root, 密码 sunion123\napi2 服务器: api2.maclaw.top, 用户名 root, 密码 sunion123",
		Snippet:    "api1/api2 服务器", // 14 chars — the bug would return this instead of full Claim
	}
	got := BestContentText(r)
	if got != r.Claim {
		t.Fatalf("bug scenario: expected full Claim with passwords, got %q (len=%d)", got, len([]rune(got)))
	}
	if got == r.Snippet {
		t.Fatal("bug scenario: returned short Snippet instead of full Claim — priority is wrong!")
	}
}
