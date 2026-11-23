package toolresult

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

const (
	defaultSearchBeforeBytes = 256
	defaultSearchAfterBytes  = 512
	defaultSearchMaxMatches  = 5
	searchChunkBytes         = 64 * 1024
)

type searchMatch struct {
	start int
	end   int
	text  string
}

type searchCollector struct {
	opts       ReadOptions
	readRange  func(start, end int) (string, error)
	re         *regexp.Regexp
	matches    []searchMatch
	nextOffset int
	truncated  bool
	total      int
	err        error
}

func readSearchOpened(rootAbs, path string, file *os.File, size int64, opts ReadOptions) (ReadResult, error) {
	maxInt := int64(^uint(0) >> 1)
	if size < 0 || size > maxInt {
		return ReadResult{}, fmt.Errorf("toolresult: file is too large to address")
	}
	total := int(size)
	offset := min(max(opts.Offset, 0), total)
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return ReadResult{}, fmt.Errorf("toolresult: query is required for search")
	}
	if len(query) > 4096 {
		return ReadResult{}, fmt.Errorf("toolresult: query exceeds 4096 bytes")
	}
	re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(query))
	if err != nil {
		return ReadResult{}, fmt.Errorf("toolresult: invalid query: %w", err)
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultReadLimit
	}
	if limit > MaxReadLimit {
		limit = MaxReadLimit
	}
	opts.Limit = limit
	opts.Query = query
	readRange := func(start, end int) (string, error) {
		data := make([]byte, end-start)
		n, err := file.ReadAt(data, int64(start))
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("toolresult: read search window: %w", err)
		}
		if n != len(data) {
			return "", fmt.Errorf("toolresult: short search window: got %d bytes, want %d", n, len(data))
		}
		startAligned, endAligned := alignUTF8ReadWindow(data, 0, len(data))
		return string(data[startAligned:endAligned]), nil
	}
	collector := searchCollector{opts: opts, readRange: readRange, re: re, nextOffset: total, total: total}
	var plain []byte
	if strings.HasSuffix(path, encryptedSuffix) {
		plain, err = readStorePayload(rootAbs, path, file, size)
		if err != nil {
			return ReadResult{}, err
		}
		total = len(plain)
		offset = min(offset, total)
		collector.total = total
		collector.nextOffset = total
		collector.readRange = func(start, end int) (string, error) {
			data := plain[start:end]
			startAligned, endAligned := alignUTF8ReadWindow(data, 0, len(data))
			return string(data[startAligned:endAligned]), nil
		}
		collector.scan(plain, 0)
	} else {
		err = collector.scanFile(file, offset, total)
	}
	if err != nil {
		return ReadResult{}, err
	}
	if collector.err != nil {
		return ReadResult{}, collector.err
	}
	if !collector.truncated {
		collector.nextOffset = total
	}
	afterRead, err := file.Stat()
	if err != nil {
		return ReadResult{}, fmt.Errorf("toolresult: stat after search: %w", err)
	}
	if afterRead.Size() != size {
		return ReadResult{}, fmt.Errorf("toolresult: file changed during search")
	}
	content := collector.render()
	out := ReadResult{
		Content:       content,
		Path:          path,
		ID:            strings.TrimSpace(opts.ID),
		Offset:        offset,
		ReturnedBytes: len(content),
		TotalBytes:    total,
		Truncated:     collector.truncated,
		NextOffset:    collector.nextOffset,
		Query:         query,
		Matches:       len(collector.matches),
	}
	if base := filepathBase(path); strings.HasSuffix(base, ".txt") {
		out.ID = strings.TrimSuffix(base, ".txt")
	} else if strings.HasSuffix(base, encryptedSuffix) {
		out.ID = strings.TrimSuffix(base, encryptedSuffix)
	}
	return out, nil
}

func filepathBase(path string) string {
	if i := strings.LastIndexAny(path, `/\\`); i >= 0 {
		return path[i+1:]
	}
	return path
}

func (c *searchCollector) scanFile(file *os.File, offset, total int) error {
	carry := make([]byte, 0)
	for pos := offset; pos < total && !c.truncated; {
		n := min(searchChunkBytes, total-pos)
		chunk := make([]byte, n)
		read, err := file.ReadAt(chunk, int64(pos))
		if err != nil && err != io.EOF {
			return fmt.Errorf("toolresult: read search chunk: %w", err)
		}
		if read != n {
			return fmt.Errorf("toolresult: short search chunk: got %d bytes, want %d", read, n)
		}
		data := make([]byte, 0, len(carry)+len(chunk))
		data = append(data, carry...)
		data = append(data, chunk...)
		c.scan(data, pos-len(carry))
		keep := min(len(c.opts.Query)-1, len(data))
		carry = append(carry[:0], data[len(data)-keep:]...)
		pos += n
	}
	return nil
}

func (c *searchCollector) scan(data []byte, base int) {
	for _, loc := range c.re.FindAllIndex(data, -1) {
		if c.truncated {
			return
		}
		start := base + loc[0]
		end := base + loc[1]
		if start < c.opts.Offset {
			continue
		}
		before := c.opts.BeforeBytes
		if before <= 0 {
			before = defaultSearchBeforeBytes
		}
		after := c.opts.AfterBytes
		if after <= 0 {
			after = defaultSearchAfterBytes
		}
		contextStart := max(c.opts.Offset, start-before)
		contextEnd := min(c.total, end+after)
		text, err := c.readRange(contextStart, contextEnd)
		if err != nil {
			c.err = err
			c.truncated = true
			c.nextOffset = start
			return
		}
		if len(c.matches) > 0 && contextStart <= c.matches[len(c.matches)-1].end {
			last := &c.matches[len(c.matches)-1]
			if contextEnd <= last.end {
				c.nextOffset = end
				continue
			}
			merged, err := c.readRange(last.start, contextEnd)
			if err != nil {
				c.err = err
				c.truncated = true
				c.nextOffset = start
				return
			}
			last.end = contextEnd
			last.text = merged
			c.nextOffset = end
			continue
		}
		c.matches = append(c.matches, searchMatch{start: contextStart, end: contextEnd, text: text})
		c.nextOffset = end
		maxMatches := c.opts.MaxMatches
		if maxMatches <= 0 {
			maxMatches = defaultSearchMaxMatches
		}
		if len(c.matches) >= maxMatches || c.contentBytes() > c.opts.Limit {
			c.truncated = true
			return
		}
	}
}

func (c *searchCollector) contentBytes() int {
	total := 0
	for _, match := range c.matches {
		total += len(match.text) + 32
	}
	return total
}

func (c *searchCollector) render() string {
	var b strings.Builder
	for i, match := range c.matches {
		if i > 0 {
			b.WriteString("\n---\n")
		}
		fmt.Fprintf(&b, "[match_offset: %d]\n%s", match.start, match.text)
	}
	return b.String()
}
