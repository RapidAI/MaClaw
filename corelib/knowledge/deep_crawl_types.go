package knowledge

import "sync"

// DeepCrawlRequest 深度检索请求参数
type DeepCrawlRequest struct {
	SeedURL        string   `json:"seed_url"`
	MaxDepth       int      `json:"max_depth"`        // 1-5
	SameDomainOnly bool     `json:"same_domain_only"` // 默认 true
	SaveScope      string   `json:"save_scope,omitempty"`
	TopicHint      string   `json:"topic_hint,omitempty"`
	DistillMode    string   `json:"distill_mode,omitempty"`
	Labels         []string `json:"labels,omitempty"`
	AutoLabels     bool     `json:"auto_labels,omitempty"`
	PreviewOnly    bool     `json:"preview_only"` // true=仅预览不保存
	OwnerID        string   `json:"owner_id,omitempty"`
	ProjectPath    string   `json:"project_path,omitempty"`
	ClientRunID    string   `json:"client_run_id,omitempty"`
}

// DeepCrawlProgress 进度事件数据
type DeepCrawlProgress struct {
	JobID           string `json:"job_id"`
	Mode            string `json:"mode,omitempty"`          // preview/crawl
	ClientRunID     string `json:"client_run_id,omitempty"` // caller-provided UI run correlation
	Status          string `json:"status"`                  // discovering/crawling/completed/cancelled/failed
	CurrentDepth    int    `json:"current_depth"`
	MaxDepth        int    `json:"max_depth"`
	TotalDiscovered int    `json:"total_discovered"`
	Completed       int    `json:"completed"`
	Pending         int    `json:"pending"`
	Failed          int    `json:"failed"`
	Skipped         int    `json:"skipped"`
	CurrentURL      string `json:"current_url,omitempty"`
}

// DeepCrawlResult 抓取完成结果
type DeepCrawlResult struct {
	JobID           string                  `json:"job_id"`
	Status          string                  `json:"status"`
	TotalDiscovered int                     `json:"total_discovered"`
	TotalSaved      int                     `json:"total_saved"`
	Duplicates      int                     `json:"duplicates"`
	Failed          int                     `json:"failed"`
	Skipped         int                     `json:"skipped"`
	Items           []DeepCrawlItem         `json:"items,omitempty"`
	ByDepth         []DeepCrawlDepthSummary `json:"by_depth,omitempty"`
}

// DeepCrawlItem 单个 URL 的抓取结果
type DeepCrawlItem struct {
	URL      string `json:"url"`
	Depth    int    `json:"depth"`
	Status   string `json:"status"` // saved/duplicate/failed/skipped
	Title    string `json:"title,omitempty"`
	Error    string `json:"error,omitempty"`
	SourceID string `json:"source_id,omitempty"`
}

// DeepCrawlDepthSummary 按层级汇总
type DeepCrawlDepthSummary struct {
	Depth  int      `json:"depth"`
	Total  int      `json:"total"`
	Saved  int      `json:"saved"`
	Failed int      `json:"failed"`
	URLs   []string `json:"urls,omitempty"` // 仅预览模式填充
}

// bfsLevel 表示 BFS 中一层的 URL 集合
type bfsLevel struct {
	depth int
	urls  []string
}

// crawlState 抓取状态（引擎内部）
type crawlState struct {
	mu           sync.Mutex
	visited      map[string]struct{} // 已访问/已入队的 URL（normalized）
	results      []DeepCrawlItem
	totalQueued  int
	limitReached bool
	completed    int
	failed       int
	skipped      int
}
