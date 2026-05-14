package memory

import (
	"testing"
)

func TestStabilityMeta_InitialState(t *testing.T) {
	var m *StabilityMeta
	if m.StabilityBoost() != 0.0 {
		t.Errorf("nil boost should be 0, got %.1f", m.StabilityBoost())
	}
	m = &StabilityMeta{}
	if m.Level != StabilityUnverified {
		t.Errorf("initial level should be unverified, got %s", m.Level)
	}
	if m.StabilityBoost() != 0.0 {
		t.Errorf("unverified boost should be 0, got %.1f", m.StabilityBoost())
	}
}

func TestStabilityMeta_BecomesStableAfter3Confirms(t *testing.T) {
	m := &StabilityMeta{}
	m.RecordConfirmation()
	m.RecordConfirmation()
	if m.Level == StabilityStable {
		t.Error("should not be stable after only 2 confirms")
	}
	m.RecordConfirmation()
	if m.Level != StabilityStable {
		t.Errorf("should be stable after 3 confirms, got %s", m.Level)
	}
	if m.StabilityBoost() != 2.0 {
		t.Errorf("stable boost should be 2.0, got %.1f", m.StabilityBoost())
	}
}

func TestStabilityMeta_BecomesVolatileOnContradiction(t *testing.T) {
	m := &StabilityMeta{}
	m.RecordConfirmation()
	m.RecordConfirmation()
	m.RecordConfirmation()
	if m.Level != StabilityStable {
		t.Fatal("should be stable first")
	}

	m.RecordContradiction()
	if m.Level != StabilityVolatile {
		t.Errorf("should become volatile after contradiction, got %s", m.Level)
	}
	if m.StabilityBoost() != -1.0 {
		t.Errorf("volatile boost should be -1.0, got %.1f", m.StabilityBoost())
	}
}

func TestStabilityMeta_Reset(t *testing.T) {
	m := &StabilityMeta{}
	m.RecordConfirmation()
	m.RecordConfirmation()
	m.RecordConfirmation()
	m.Reset()
	if m.Level != StabilityUnverified {
		t.Errorf("after reset should be unverified, got %s", m.Level)
	}
	if m.ConfirmCount != 0 || m.ContradictCount != 0 {
		t.Error("counts should be 0 after reset")
	}
}

func TestDetectContradiction_NegationPair(t *testing.T) {
	existing := "SSH 服务器 api.rapidai.tech 支持密钥认证"
	new_ := "SSH 服务器 api.rapidai.tech 不支持密钥认证"
	if !DetectContradiction(existing, new_) {
		t.Error("should detect negation contradiction")
	}
}

func TestDetectContradiction_CorrectionSignal(t *testing.T) {
	existing := "项目使用 Python 3.8"
	new_ := "项目使用 Python 3.11，之前说 3.8 是错了"
	if !DetectContradiction(existing, new_) {
		t.Error("should detect correction signal")
	}
}

func TestDetectContradiction_DifferentTopics(t *testing.T) {
	existing := "用户喜欢深色主题"
	new_ := "服务器 IP 是 192.168.1.1"
	if DetectContradiction(existing, new_) {
		t.Error("different topics should not be contradictions")
	}
}

func TestDetectContradiction_SameTopicNoConflict(t *testing.T) {
	existing := "项目使用 Go 语言开发"
	new_ := "项目使用 Go 语言，版本 1.21"
	if DetectContradiction(existing, new_) {
		t.Error("supplementary info should not be contradiction")
	}
}

func TestDetectContradiction_BooleanFlip(t *testing.T) {
	existing := "feature flag is enabled"
	new_ := "feature flag is disabled now"
	if !DetectContradiction(existing, new_) {
		t.Error("should detect boolean flip")
	}
}

func TestDetectContradiction_EnglishCorrection(t *testing.T) {
	existing := "The API endpoint is /v1/users"
	new_ := "Actually the API endpoint is /v2/users, the previous one was incorrect"
	if !DetectContradiction(existing, new_) {
		t.Error("should detect English correction signal")
	}
}
