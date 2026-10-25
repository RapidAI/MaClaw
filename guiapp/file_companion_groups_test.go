package guiapp

import (
	"errors"
	"net/http"
	"testing"
)

func TestFileCompanionGroupCallErrorKeepsTheHubMessage(t *testing.T) {
	hubMsg := &fileCompanionShareStatus{status: http.StatusNotFound, message: "没有找到入群邀请"}
	if got := fileCompanionGroupCallError(hubMsg); got.Error() != "没有找到入群邀请" {
		t.Fatalf("hub message = %q", got.Error())
	}
	empty404 := &fileCompanionShareStatus{status: http.StatusNotFound}
	if got := fileCompanionGroupCallError(empty404); got.Error() != "没有找到入群邀请" {
		t.Fatalf("empty 404 = %q", got.Error())
	}
	if got := empty404.Error(); got != "云端还没有文档分享，需要更新 Hub" {
		t.Fatalf("share status = %q", got)
	}
	empty502 := &fileCompanionShareStatus{status: http.StatusBadGateway}
	if got := fileCompanionGroupCallError(empty502); got.Error() != "云端群组无法读取（HTTP 502）" {
		t.Fatalf("empty 502 = %q", got.Error())
	}
	unauthorized := &fileCompanionShareStatus{status: http.StatusUnauthorized}
	if got := fileCompanionGroupCallError(unauthorized); got.Error() != "请先登录 MaClaw Hub" {
		t.Fatalf("empty 401 = %q", got.Error())
	}
	network := errors.New("暂时无法连接云端")
	if fileCompanionGroupCallError(network) != network {
		t.Fatal("network error should pass through")
	}
}

func TestFileCompanionInviteListIsOldHubOnlyForAnEmpty404(t *testing.T) {
	if !fileCompanionInviteListIsOldHub(&fileCompanionShareStatus{status: http.StatusNotFound}) {
		t.Fatal("empty 404 should be an old hub")
	}
	blank := &fileCompanionShareStatus{status: http.StatusNotFound, message: " \t "}
	if !fileCompanionInviteListIsOldHub(blank) {
		t.Fatal("a blank 404 message should be an old hub")
	}
	withMessage := &fileCompanionShareStatus{status: http.StatusNotFound, message: "邀请列表已关闭"}
	if fileCompanionInviteListIsOldHub(withMessage) {
		t.Fatal("a 404 with a hub message is not an old hub")
	}
	if got := fileCompanionGroupCallError(withMessage); got == nil || got.Error() != "邀请列表已关闭" {
		t.Fatalf("hub 404 message = %v", got)
	}
	if fileCompanionInviteListIsOldHub(&fileCompanionShareStatus{status: http.StatusBadGateway}) {
		t.Fatal("a non-404 status is not an old hub")
	}
	if fileCompanionInviteListIsOldHub(errors.New("暂时无法连接云端")) {
		t.Fatal("a network error is not an old hub")
	}
	if fileCompanionInviteListIsOldHub(nil) {
		t.Fatal("nil is not an old hub")
	}
}
