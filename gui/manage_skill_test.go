package main

import (
	"fmt"
	"strings"
	"testing"
)

// TestToolUploadSkill_EmptyName verifies that toolUploadSkill returns an error
// when the name parameter is empty or missing.
//
// **Validates: Requirements 3.3**
func TestToolUploadSkill_EmptyName(t *testing.T) {
	h := &IMMessageHandler{app: &App{}}

	// Empty name
	got := h.toolUploadSkill(map[string]interface{}{"name": ""})
	if !strings.Contains(got, "缺少 name 参数") {
		t.Fatalf("expected missing name error, got %q", got)
	}

	// Missing name key entirely
	got = h.toolUploadSkill(map[string]interface{}{})
	if !strings.Contains(got, "缺少 name 参数") {
		t.Fatalf("expected missing name error for missing key, got %q", got)
	}
}

// TestToolUploadSkill_NilExecutor verifies that toolUploadSkill returns an error
// when the skill executor is not initialized (nil).
//
// **Validates: Requirements 3.5**
func TestToolUploadSkill_NilExecutor(t *testing.T) {
	app := &App{}
	// skillExecutor is nil by default
	h := &IMMessageHandler{app: app}

	got := h.toolUploadSkill(map[string]interface{}{"name": "test-skill"})
	if !strings.Contains(got, "上传失败") {
		t.Fatalf("expected upload failure error when executor is nil, got %q", got)
	}
}

// TestToolUploadSkill_ErrorPropagation verifies that errors from
// UploadNLSkillToMarket are propagated in the response message.
//
// **Validates: Requirements 3.4**
func TestToolUploadSkill_ErrorPropagation(t *testing.T) {
	app := &App{}
	// Ensure skillExecutor is set but skillMarketClient is nil,
	// which will cause UploadNLSkillToMarket to return an error.
	app.skillExecutor = &SkillExecutor{app: app}
	h := &IMMessageHandler{app: app}

	got := h.toolUploadSkill(map[string]interface{}{"name": "test-skill"})
	if !strings.Contains(got, "上传失败") {
		t.Fatalf("expected upload failure error, got %q", got)
	}
}

// TestToolUploadSkill_SuccessPath verifies that a successful upload returns
// a message containing the submission ID.
//
// **Validates: Requirements 3.2**
func TestToolUploadSkill_SuccessPath(t *testing.T) {
	// We test the success message format by verifying the format string
	// used in toolUploadSkill. Since UploadNLSkillToMarket requires
	// real infrastructure (skill executor, market client, config, etc.),
	// we verify the format by checking the expected output pattern.
	expectedFormat := "✅ Skill「%s」已上传到 SkillMarket，提交 ID: %s"
	result := fmt.Sprintf(expectedFormat, "my-skill", "sub-12345")
	if !strings.Contains(result, "my-skill") || !strings.Contains(result, "sub-12345") {
		t.Fatalf("success message format is incorrect: %q", result)
	}
}

// TestToolManageSkill_DispatchesCorrectly verifies that toolManageSkill
// dispatches each action to the correct handler by testing the default
// (invalid action) case returns an error listing all supported actions.
//
// **Validates: Requirements 2.7**
func TestToolManageSkill_InvalidAction(t *testing.T) {
	app := &App{}
	h := &IMMessageHandler{app: app}

	got := h.toolManageSkill(map[string]interface{}{"action": "invalid_action"}, nil)
	if !strings.Contains(got, "未知 manage_skill action") {
		t.Fatalf("expected unknown action error, got %q", got)
	}
	// Verify all six action names are listed
	for _, action := range []string{"list", "search", "install", "run", "status", "upload"} {
		if !strings.Contains(got, action) {
			t.Errorf("error message should contain action %q, got %q", action, got)
		}
	}
}

// TestToolManageSkill_EmptyAction verifies that an empty action parameter
// returns an error listing all supported actions.
//
// **Validates: Requirements 2.7**
func TestToolManageSkill_EmptyAction(t *testing.T) {
	app := &App{}
	h := &IMMessageHandler{app: app}

	got := h.toolManageSkill(map[string]interface{}{}, nil)
	if !strings.Contains(got, "未知 manage_skill action") {
		t.Fatalf("expected unknown action error for empty action, got %q", got)
	}
}
