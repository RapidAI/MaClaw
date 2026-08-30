package intent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// TestRealModelLiveDataDocumentCompositeRegression exercises the installed
// production embedding model against the failure class that previously relied
// on a remote classifier response.  It deliberately checks semantic labels and
// their declared dependency rather than matching words in the user message.
func TestRealModelLiveDataDocumentCompositeRegression(t *testing.T) {
	if testing.Short() {
		t.Skip("real-model regression skipped in short mode")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("resolve home directory: %v", err)
	}
	modelPath := filepath.Join(home, ".maclaw", "models", embedding.DefaultModelFilename)
	if _, err := os.Stat(modelPath); err != nil {
		t.Skipf("embedding model not installed: %v", err)
	}
	emb, err := embedding.NewGemmaEmbedder(modelPath, embedding.DefaultEmbeddingDim)
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()

	uic := New(Config{Embedder: emb})
	deadline := time.Now().Add(2 * time.Minute)
	for !uic.Ready() {
		if time.Now().After(deadline) {
			t.Fatal("anchor warmup timed out")
		}
		time.Sleep(50 * time.Millisecond)
	}

	for _, text := range []string{
		"北京天气，输出格式化pdf报告",
		"天津天气，输出格式化pdf报告",
		"东莞天气，输出 格式化pdf报告",
		"输出东莞天气PDF报告",
		"比特币当前价格，输出格式化pdf报告",
		"今天的新闻热点整理成PDF报告",
		"查询任意城市的实时天气并导出为格式化PDF报告",
		"把某地当前天气信息整理成PDF文件",
	} {
		result := uic.ClassifyEmbeddingOnly(MessageContext{Text: text})
		t.Logf("%q -> primary=%s secondary=%v confidence=%.3f reason=%q scores=%v", text, result.Primary, result.Secondary, result.Confidence, result.Reason, uic.DiagnoseScores(text))
		NormalizeDeclaredComposite(&result)
		if result.Primary != LabelLiveData || len(result.Secondary) != 1 || result.Secondary[0] != LabelDocumentGenerate {
			t.Fatalf("%q -> %+v, want declared live_data -> document_generate composite", text, result)
		}
		if result.Confidence < EmbeddingCompositePrimaryMinScore || result.Degraded {
			t.Fatalf("%q -> %+v, want independently verified executable composite", text, result)
		}
	}

	for _, text := range []string{
		"把这份会议纪要排版成PDF文件",
		"将用户提供的材料导出为PDF",
		"将附件里的内容排版并导出为PDF报告",
		"把项目总结制作成PDF文档",
		"将现有数据保存为PDF文件",
	} {
		result := uic.ClassifyEmbeddingOnly(MessageContext{Text: text})
		t.Logf("negative %q -> primary=%s secondary=%v confidence=%.3f reason=%q scores=%v", text, result.Primary, result.Secondary, result.Confidence, result.Reason, uic.DiagnoseScores(text))
		if isLookupIntentLabel(result.Primary) || containsLookupIntentLabel(result.Secondary) {
			t.Fatalf("%q -> %+v, must not synthesize an external-data prerequisite", text, result)
		}
	}
}

func containsIntentLabel(labels []IntentLabel, want IntentLabel) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

func containsLookupIntentLabel(labels []IntentLabel) bool {
	for _, label := range labels {
		if isLookupIntentLabel(label) {
			return true
		}
	}
	return false
}
