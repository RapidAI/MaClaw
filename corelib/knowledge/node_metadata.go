package knowledge

import "strings"

// sanitizeDocumentNodeMetadata removes fields which are valid only during
// local parsing or which can disclose a host filesystem location. It is used
// by every persistence and snapshot boundary, rather than relying solely on
// individual extractors to remember the policy.
func sanitizeDocumentNodeMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return metadata
	}
	out := make(map[string]string, len(metadata))
	for key, value := range metadata {
		key = strings.TrimSpace(key)
		if key == "" || key == "_image_bytes_key" || key == MetaImageAssetPath {
			continue
		}
		// image_asset_id is persisted input at this low-level boundary. Keep
		// only a canonical opaque ID; otherwise later retrieval, cleanup, and
		// diagnostics could see a malformed token that no presentation layer is
		// allowed to resolve.
		if key == MetaImageAssetID && !IsSafeImageAssetID(value) {
			continue
		}
		out[key] = value
	}
	return out
}

// sanitizeSnapshotDocumentNode retains portable knowledge text/metadata while
// dropping obsolete asset-path fields from both legacy database rows and input
// snapshots. Image assets themselves are intentionally not snapshot payloads;
// the surviving opaque ID is only a local managed-asset lookup key.
func sanitizeSnapshotDocumentNode(node DocumentNode) DocumentNode {
	node.Metadata = sanitizeDocumentNodeMetadata(node.Metadata)
	return node
}
