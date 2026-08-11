package knowledge

import (
	"encoding/base64"
	"fmt"
	"image"
	"io"
	"strings"
)

// KBImageMarkerPrefix is the special marker prefix for inline knowledge base images
// in tool results. The frontend parses this marker to render inline image previews.
// Format: [KB_IMAGE:assetID|dataURL]  (pipe-separated)
const KBImageMarkerPrefix = "[KB_IMAGE:"

// MaxKBImageMarkerDataBytes limits the decoded JPEG thumbnail that can cross
// the knowledge-search -> agent -> chat boundary. Asset-generated thumbnails
// are at most 120px on either edge, so this remains comfortably above normal
// output while preventing model-authored marker text from making a browser
// decode an arbitrarily large data URL.
const MaxKBImageMarkerDataBytes = 256 * 1024

// SearchResultImageEmbed holds image data for embedding in search results.
type SearchResultImageEmbed struct {
	AssetID string // identifier used to open the original image
	DataURL string // data:image/jpeg;base64,... (thumbnail)
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

	// Determine asset ID from the persisted search-result metadata. The caller
	// has already performed database authorization; this helper must not invent
	// another source/node-derived ID, because a stale or forged result could
	// otherwise render a different managed asset from the same local cache.
	sourceID := result.Source.ID
	if !IsSafeImageAssetID(sourceID) {
		return nil
	}

	assetID := sourceID
	if result.Media != nil {
		// An image-node result must carry the authoritative persisted asset ID.
		// If it is malformed, do not silently substitute the standalone source
		// asset: that changes which evidence the model is shown.
		if result.Media.AssetID == "" || !IsSafeImageAssetID(result.Media.AssetID) {
			return nil
		}
		assetID = result.Media.AssetID
	}
	if !ImageAssetIDBelongsToSourceID(assetID, sourceID) {
		return nil
	}
	thumbData, err := ReadKnowledgeImageThumbnail(assetBaseDir, assetID)
	if err != nil {
		return nil
	}
	return &SearchResultImageEmbed{
		AssetID: assetID,
		DataURL: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(thumbData),
	}
}

// FormatKBImageMarker generates the inline marker string for agent replies.
// Format: [KB_IMAGE:assetID|dataURL] (pipe-separated, since base64 never contains |).
// OriginalPath is deliberately excluded: markers cross the model boundary and
// must never disclose host filesystem paths.
func FormatKBImageMarker(embed *SearchResultImageEmbed) string {
	if embed == nil || !isSafeKBImageAssetID(embed.AssetID) || !isSafeKBImageDataURL(embed.DataURL) {
		return ""
	}
	// Use | as separator since base64 doesn't contain |.
	return fmt.Sprintf("[KB_IMAGE:%s|%s]", embed.AssetID, embed.DataURL)
}

// ParseKBImageMarker extracts a safe image embed from a KB_IMAGE marker string.
// Markers are model-visible data, so accept only the two-field format generated
// by FormatKBImageMarker. In particular, a caller must not be able to reattach
// an arbitrary local OriginalPath through an old three-field marker form.
func ParseKBImageMarker(marker string) *SearchResultImageEmbed {
	if !strings.HasPrefix(marker, "[KB_IMAGE:") || !strings.HasSuffix(marker, "]") {
		return nil
	}
	inner := marker[len("[KB_IMAGE:") : len(marker)-1]
	parts := strings.Split(inner, "|")
	if len(parts) != 2 || !isSafeKBImageAssetID(parts[0]) || !isSafeKBImageDataURL(parts[1]) {
		return nil
	}
	return &SearchResultImageEmbed{
		AssetID: parts[0],
		DataURL: parts[1],
	}
}

func isSafeKBImageAssetID(value string) bool {
	return IsSafeImageAssetID(value)
}

func isSafeKBImageDataURL(value string) bool {
	const prefix = "data:image/jpeg;base64,"
	if !strings.HasPrefix(strings.ToLower(value), prefix) {
		return false
	}
	encoded := value[len(prefix):]
	// Four base64 input bytes decode to at most three bytes. Check the encoded
	// length first so an attacker cannot force a large allocation in DecodeString.
	if encoded == "" || len(encoded)%4 != 0 || len(encoded) > ((MaxKBImageMarkerDataBytes+2)/3)*4 {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(decoded) == 0 || len(decoded) > MaxKBImageMarkerDataBytes {
		return false
	}
	return isSafeKBImageThumbnail(decoded)
}

// isSafeKBImageThumbnail verifies the producer/consumer contract, not merely
// the data-URL syntax. A marker may only carry the derived JPEG thumbnail that
// the knowledge asset manager creates; arbitrary base64 bytes and a full-size
// JPEG cannot be promoted into agent-rendered media.
func isSafeKBImageThumbnail(data []byte) bool {
	if len(data) == 0 || len(data) > MaxKBImageMarkerDataBytes {
		return false
	}
	config, format, err := image.DecodeConfig(&thumbnailBytesReader{data: data})
	if err != nil || format != "jpeg" || config.Width <= 0 || config.Height <= 0 {
		return false
	}
	return config.Width <= ThumbSize && config.Height <= ThumbSize
}

// IsSafeKnowledgeImageThumbnail verifies that data is a bounded JPEG thumbnail
// produced by the managed knowledge image pipeline. It is exported for desktop
// and HTTP presentation layers, which must never expose arbitrary bytes as an
// image data URL.
func IsSafeKnowledgeImageThumbnail(data []byte) bool {
	return isSafeKBImageThumbnail(data)
}

// thumbnailBytesReader avoids an additional allocation while image.DecodeConfig parses
// a thumbnail that is already held in memory.
type thumbnailBytesReader struct {
	data []byte
	pos  int
}

func (r *thumbnailBytesReader) Read(dst []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(dst, r.data[r.pos:])
	r.pos += n
	return n, nil
}
