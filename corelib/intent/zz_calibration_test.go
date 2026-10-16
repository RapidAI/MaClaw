package intent

// LOCAL CALIBRATION HARNESS (local branch only, not for upstream).
// ZZ_CALIB=scores dumps per-utterance top-N label scores for:
//   - every EmbedTexts anchor (self-consistency: own label should be top-1)
//   - the utterance corpus below (expected-label checks incl. composites)
// Output: JSONL at ZZ_OUT with {"id","text","expected","top":[{label,score}]},
// plus summary records {"id":"_summary"...} for margin analysis.

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

type zzCalScore struct {
	Label string  `json:"label"`
	Score float64 `json:"score"`
}

func zzCalTopN(scores map[IntentLabel]float64, n int) []zzCalScore {
	var all []zzCalScore
	for l, s := range scores {
		all = append(all, zzCalScore{string(l), s})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Score > all[j].Score })
	if len(all) > n {
		all = all[:n]
	}
	return all
}

func TestZZCalibration(t *testing.T) {
	if os.Getenv("ZZ_CALIB") == "" {
		t.Skip("ZZ_CALIB not set")
	}
	modelPath := embedding.DefaultModelPath()
	if modelPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skipf("resolve home: %v", err)
		}
		modelPath = home + string(os.PathSeparator) + ".maclaw" + string(os.PathSeparator) + "models" + string(os.PathSeparator) + embedding.DefaultModelFilename
	}
	if _, err := os.Stat(modelPath); err != nil {
		t.Skipf("embedding model not installed: %v", err)
	}
	emb, err := embedding.NewEmbedderFromFile(modelPath, embedding.DefaultEmbeddingDim)
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

	out, err := os.Create(os.Getenv("ZZ_OUT"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)

	emit := func(id, text, expected string, scores map[IntentLabel]float64, res *ClassificationResult) {
		rec := map[string]any{
			"id":        id,
			"text":      text,
			"expected":  expected,
			"top":       zzCalTopN(scores, 6),
			"live_data": scores[LabelLiveData],
			"search":    scores[LabelSearch],
			"web_fetch": scores[LabelWebFetch],
			"mbox_read": scores[LabelMailboxRead],
			"mbox_send": scores[LabelMailboxSend],
			"mbox_cfg":  scores[LabelMailboxConfigure],
		}
		if res != nil {
			rec["primary"] = res.Primary
			rec["secondary"] = res.Secondary
			rec["confidence"] = res.Confidence
			rec["reason"] = res.Reason
			rec["degraded"] = res.Degraded
		}
		_ = enc.Encode(rec)
	}

	// 1. Anchor self-consistency: each anchor text should rank its own label first.
	for _, def := range DefaultDefinitions() {
		for i, text := range def.EmbedTexts {
			emit("anchor/"+string(def.Label)+"/"+itoa(i), text, string(def.Label), uic.DiagnoseScores(text), nil)
		}
	}

	// 2. Test-asserted utterances and composite controls.
	type utt struct {
		Text     string
		Expected string
	}
	for i, u := range []utt{
		// live_data -> document_generate composites (regression test corpus)
		{"北京天气，输出格式化pdf报告", "live_data+document_generate"},
		{"崇州天气，生成格式化pdf", "live_data+document_generate"},
		{"天津天气，输出格式化pdf报告", "live_data+document_generate"},
		{"东莞天气，输出 格式化pdf报告", "live_data+document_generate"},
		{"输出东莞天气PDF报告", "live_data+document_generate"},
		{"比特币当前价格，输出格式化pdf报告", "live_data+document_generate"},
		{"今天的新闻热点整理成PDF报告", "live_data+document_generate"},
		{"查询任意城市的实时天气并导出为格式化PDF报告", "live_data+document_generate"},
		{"把某地当前天气信息整理成PDF文件", "live_data+document_generate"},
		// extended live_data->PDF phrasings (calibration breadth)
		{"查一下上海明天的天气做成PDF", "live_data+document_generate"},
		{"美元兑人民币汇率，导出PDF报告", "live_data+document_generate"},
		{"今日股市行情整理成PDF文件", "live_data+document_generate"},
		{"帮我查航班动态并生成PDF", "live_data+document_generate"},
		{"最近的新闻做成PDF日报", "live_data+document_generate"},
		// PDF-only negatives
		{"把这份会议纪要排版成PDF文件", "document_generate"},
		{"将用户提供的材料导出为PDF", "document_generate"},
		{"将附件里的内容排版并导出为PDF报告", "document_generate"},
		{"把项目总结制作成PDF文档", "document_generate"},
		{"将现有数据保存为PDF文件", "document_generate"},
		{"把这段文字转换成PDF", "document_generate"},
		{"帮我生成一份PDF简历", "document_generate"},
		// office + lookup composites
		{"生成庆祝我家布偶宝宝5岁生日的ppt，没有照片，网上随便找一下布偶照片。", "office+lookup"},
		{"网上找几张布偶猫照片，做成生日PPT", "office+lookup"},
		// computer_use positives/guards
		{"打开word程序，编写一个你（maclaw）的简历", "computer_use"},
		{"帮我在电脑上打开Excel程序，把这一列求和", "computer_use"},
		{"点击窗口上的确定按钮", "computer_use"},
		{"帮我写一份word简历", "!computer_use"},
		{"在浏览器里打开知乎发帖", "!computer_use"},
		// mailbox positives
		{"查一下我的未读邮件", "mailbox_read"},
		{"看看收件箱里有没有新邮件", "mailbox_read"},
		{"搜一下上周老板发来的邮件", "mailbox_read"},
		{"把这封邮件的内容读给我听", "mailbox_read"},
		{"check my inbox for new mail", "mailbox_read"},
		{"给张三发一封邮件说明天开会", "mailbox_send"},
		{"回复刚才那封邮件", "mailbox_send"},
		{"把这封邮件转发给李四", "mailbox_send"},
		{"用我的工作邮箱发个通知", "mailbox_send"},
		{"email the report to the client", "document_delivery"}, // spec: file-to-mailbox = document_delivery
		{"帮我配置一下邮箱", "mailbox_configure"},
		{"添加公司邮箱账号", "mailbox_configure"},
		{"把SMTP服务器和密码存下来", "mailbox_configure"},
		// mailbox boundary guards (TreeText-declared boundaries)
		{"发条微信告诉他邮件收到了", "!mailbox"},
		{"在浏览器里打开QQ邮箱网页版", "!mailbox"},
		{"把文件发到他的邮箱", "!mailbox"}, // spec: document_delivery
		{"把周报导出成PDF发给同事", "!mailbox"},
		{"帮我写一封工作邮件的草稿", "ambiguous"}, // draft/send boundary: must escalate, not ship confident
	} {
		res := uic.ClassifyEmbeddingOnly(MessageContext{Text: u.Text})
		emit("utt/"+itoa(i), u.Text, u.Expected, uic.DiagnoseScores(u.Text), &res)
	}

	_ = enc.Encode(map[string]any{
		"id": "_thresholds",
		"thresholds": map[string]float64{
			"EmbeddingConfidentMinScore":     EmbeddingConfidentMinScore,
			"EmbeddingConfidentMinGap":       EmbeddingConfidentMinGap,
			"EmbeddingCompositeSecondaryMin": EmbeddingCompositeSecondaryMinScore,
			"EmbeddingCompositeLiveDataMin":  EmbeddingCompositeLiveDataMinScore,
			"EmbeddingLookupMinScore":        EmbeddingLookupMinScore,
			"EmbeddingLookupMinGap":          EmbeddingLookupMinGap,
			"EmbeddingLookupCompositeFloor":  EmbeddingLookupCompositeFloor,
			"verdictLocalLeaderFloor":        verdictLocalLeaderFloor,
		},
	})
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [8]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}
