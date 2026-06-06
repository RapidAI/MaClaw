package knowledge

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// KBImageMarkerPrefix is the special marker prefix for inline knowledge base images
// in tool results. The frontend parses this marker to render inline image previews.
// Format: [KB_IMAGE:assetID|dataURL|originalPath]  (pipe-separated)
const KBImageMarkerPrefix = "[KB_IMAGE:"

// SearchResultImageEmbed holds image data for embedding in search results.
type SearchResultImageEmbed struct {
	AssetID     string // identifier used to open the original image
	DataURL     string // data:image/jpeg;base64,... (thumbnail)
	OriginalPath string // absolute path for opening
}

// EmbedImageThumbForSearchResult generates the inline image embed data for a
// search result that represents an image node.
// Returns nil if the result is not an image or assets are not found.
//
// assetBaseDir is typically ~/.maclaw/knowledge_assets/
func EmbedImageThumbForSearchResult(result SearchResult, assetBaseDir string) *SearchResultImageEmbed {
	if result.NodeType != NodeTypeImage && (result.Source.Kind != SourceKindImage) {
		return nil
	}
	if assetBaseDir == "" {
		return nil
	}

	// Determine asset ID: for embedded images it's sourceID_nodeID, for standalone it's sourceID
	sourceID := result.Source.ID
	nodeID := result.NodeID
	if sourceID == "" {
		return nil
	}

	// Try sourceID_nodeID first (embedded images), then sourceID (standalone)
	candidates := []string{}
	if nodeID != "" {
		candidates = append(candidates, sourceID+"_"+nodeID)
	}
	candidates = append(candidates, sourceID)

	for _, assetID := range candidates {
		assetDir := filepath.Join(assetBaseDir, assetID)
		thumbPath := filepath.Join(assetDir, "thumb_120.jpg")
		thumbData, err := os.ReadFile(thumbPath)
		if err != nil || len(thumbData) == 0 {
			continue
		}

		// Find original path
		originalPath := ""
		entries, err := os.ReadDir(assetDir)
		if err == nil {
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), "original") {
					originalPath = filepath.Join(assetDir, entry.Name())
					break
				}
			}
		}

		return &SearchResultImageEmbed{
			AssetID:      assetID,
			DataURL:      "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(thumbData),
			OriginalPath: originalPath,
		}
	}
	return nil
}

// FormatKBImageMarker generates the inline marker string for embedding in tool results.
// Format: [KB_IMAGE:assetID|dataURL|originalPath]  (pipe-separated, since base64 never contains |)
func FormatKBImageMarker(embed *SearchResultImageEmbed) string {
	if embed == nil || embed.DataURL == "" {
		return ""
	}
	// Use | as separator since base64 doesn't contain |
	return fmt.Sprintf("[KB_IMAGE:%s|%s|%s]", embed.AssetID, embed.DataURL, embed.OriginalPath)
}

// ParseKBImageMarker extracts image embed data from a KB_IMAGE marker string.
// Returns nil if the string doesn't match the expected format.
func ParseKBImageMarker(marker string) *SearchResultImageEmbed {
	if !strings.HasPrefix(marker, "[KB_IMAGE:") || !strings.HasSuffix(marker, "]") {
		return nil
	}
	inner := marker[len("[KB_IMAGE:") : len(marker)-1]
	parts := strings.SplitN(inner, "|", 3)
	if len(parts) < 2 {
		return nil
	}
	result := &SearchResultImageEmbed{
		AssetID: parts[0],
		DataURL: parts[1],
	}
	if len(parts) >= 3 {
		result.OriginalPath = parts[2]
	}
	return result
}
