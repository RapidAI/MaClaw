package docgen

import (
	"os"
	"strconv"
	"sync"
)

// imageSizeCacheKey identifies an image file by path plus the stat identity
// that survives within a session: size and modification time (nanosecond
// resolution where supported). Caching on path alone would serve a stale
// size if the file is rewritten between two exports — the paper figure
// pipeline does rewrite its assets directory.
//
// ok is false when the file cannot be stat'ed, in which case callers must not
// cache anything.
func imageSizeCacheKey(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	return path + "|" + strconv.FormatInt(info.Size(), 36) + "|" + strconv.FormatInt(info.ModTime().UnixNano(), 36), true
}

// markdownImageSizeCache memoizes fitted sizes keyed by file identity. It is
// safe for concurrent use, and bounded: see maxMarkdownImageCacheEntries.
type markdownImageSizeCache struct {
	mu      sync.Mutex
	entries map[string]markdownImageSize
	order   []string
}

// maxMarkdownImageCacheEntries bounds memory for pathological documents that
// reference thousands of distinct images.
const maxMarkdownImageCacheEntries = 512

func newMarkdownImageSizeCache() *markdownImageSizeCache {
	return &markdownImageSizeCache{entries: make(map[string]markdownImageSize)}
}

func (c *markdownImageSizeCache) get(key string) (markdownImageSize, bool) {
	if c == nil {
		return markdownImageSize{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	got, ok := c.entries[key]
	return got, ok
}

func (c *markdownImageSizeCache) put(key string, size markdownImageSize) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; exists {
		return
	}
	c.entries[key] = size
	c.order = append(c.order, key)
	for len(c.order) > maxMarkdownImageCacheEntries {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
}
