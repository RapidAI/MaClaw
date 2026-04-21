package memory

import (
	"testing"
	"time"
)

// TestRecallDynamic_APIServerVsGPUServer verifies that when the query is
// "查看api服务器资源状", the API server entry ranks higher than the GPU server entry.
func TestRecallDynamic_APIServerVsGPUServer(t *testing.T) {
	store := newTestStore(t)

	now := time.Now()

	// GPU server entry — high access count (simulating real data)
	gpuEntry := Entry{
		ID:          "gpu-1",
		Content:     "GPU服务器→地址:home.rapidai.tech;端口:44;用户:znsoft;密码:sunion123;用途:实验/训练/推理等GPU计算;注意:GPU模型加载慢，勿因日志未刷新判失败，需查GPU使用+进程状态确认",
		CompactForm: "GPU服务器→地址:home.rapidai.tech;端口:44;用户:znsoft;密码:sunion123;用途:实验/训练/推理等GPU计算;注意:GPU模型加载慢，勿因日志未刷新判失败，需查GPU使用+进程状态确认",
		Category:    CategoryProjectKnowledge,
		Tags:        []string{"desktop-user", "extracted", "gpu-server", "infrastructure", "ssh"},
		AccessCount: 104,
		UpdatedAt:   now.Add(-2 * time.Hour),
		Status:      StatusActive,
	}

	// API server entry — low access count
	apiEntry := Entry{
		ID:          "api-1",
		Content:     "API服务器→地址:api.rapidai.tech;端口:22;用户:root;密码:sunion123;主机名:znsoftvps4;配置:Intel Xeon E5-2697 v2 10核/15GB内存/220GB磁盘;用途:API网关服务",
		Category:    CategoryProjectKnowledge,
		Tags:        []string{"服务器", "API", "SSH"},
		AccessCount: 1,
		UpdatedAt:   now.Add(-24 * time.Hour),
		Status:      StatusActive,
	}

	// Another API server entry with different tags
	apiEntry2 := Entry{
		ID:          "api-2",
		Content:     "API服务器: api.rapidai.tech, IP: 66.154.113.63, 用途: API服务。SSH端口/密码待确认。GPU服务器是另一台: home.rapidai.tech:44",
		Category:    CategoryProjectKnowledge,
		Tags:        []string{"api服务器", "rapidai"},
		AccessCount: 1,
		UpdatedAt:   now.Add(-48 * time.Hour),
		Status:      StatusActive,
	}

	// API server entry with api-server tag
	apiEntry3 := Entry{
		ID:       "api-3",
		Content:  "API服务器→地址:api.rapidai.tech;用户:root;密码:sunion123;用途:API服务",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"API服务器", "服务器", "ssh"},
		AccessCount: 1,
		UpdatedAt:   now.Add(-10 * time.Hour),
		Status:      StatusActive,
	}

	// Some noise entries to simulate real memory store
	noiseEntries := []Entry{
		{
			ID:          "noise-1",
			Content:     "深度研究报告制作流程要求：扩大搜索范围，验证资源真实性",
			Category:    CategoryInstruction,
			Tags:        []string{"深度研究", "报告", "工作流程"},
			AccessCount: 157,
			UpdatedAt:   now.Add(-1 * time.Hour),
			Status:      StatusActive,
		},
		{
			ID:          "noise-2",
			Content:     "PDF 生成优先使用 xh-md-to-pdf skill",
			Category:    CategoryPreference,
			Tags:        []string{"pdf", "skill"},
			AccessCount: 50,
			UpdatedAt:   now.Add(-3 * time.Hour),
			Status:      StatusActive,
		},
		{
			ID:          "noise-3",
			Content:     "LLM Harness Engineering 外部代码系统控制信息存储检索呈现",
			Category:    CategoryProjectKnowledge,
			Tags:        []string{"LLM", "harness"},
			AccessCount: 10,
			UpdatedAt:   now.Add(-5 * time.Hour),
			Status:      StatusActive,
		},
		{
			ID:          "noise-4",
			Content:     "Deep Research Skill 可复用结构化技术领域研究",
			Category:    CategoryProjectKnowledge,
			Tags:        []string{"research", "skill"},
			AccessCount: 20,
			UpdatedAt:   now.Add(-4 * time.Hour),
			Status:      StatusActive,
		},
	}

	// Save all entries
	for _, e := range []Entry{gpuEntry, apiEntry, apiEntry2, apiEntry3} {
		if err := store.Save(e); err != nil {
			t.Fatalf("Save %s: %v", e.ID, err)
		}
	}
	for _, e := range noiseEntries {
		if err := store.Save(e); err != nil {
			t.Fatalf("Save %s: %v", e.ID, err)
		}
	}

	// Query: "查看api服务器资源状"
	results := store.RecallDynamic("查看api服务器资源状", "", "")

	// Check that at least one API server entry is in the results
	foundAPI := false
	foundGPU := false
	apiRank := -1
	gpuRank := -1
	for i, e := range results {
		if e.ID == "api-1" || e.ID == "api-2" || e.ID == "api-3" {
			if !foundAPI {
				apiRank = i
			}
			foundAPI = true
		}
		if e.ID == "gpu-1" {
			gpuRank = i
			foundGPU = true
		}
	}

	t.Logf("Results (%d entries):", len(results))
	for i, e := range results {
		t.Logf("  [%d] ID=%s Category=%s Tags=%v AccessCount=%d Content=%.80s",
			i, e.ID, e.Category, e.Tags, e.AccessCount, e.Content)
	}

	if !foundAPI {
		t.Errorf("API server entry not found in recall results! GPU found=%v at rank %d", foundGPU, gpuRank)
	}
	if foundAPI && foundGPU && apiRank > gpuRank {
		t.Errorf("API server (rank %d) should rank higher than GPU server (rank %d) for query 'api服务器'", apiRank, gpuRank)
	}
}
