package mailbox

import "testing"

func TestQueryWantsMessageSend(t *testing.T) {
	yes := []string{
		"向 znsoft@163.com 发送一封带PDF附件的邮件，报告北京天气。",
		"向znsoft@163.com发邮件，报告北京天气。",
		"生成pdf后再次发送",
		"email the report to the client",
		"send an email to a@ex.com",
	}
	no := []string{
		"生成一份北京天气的pdf",
		"把这个文件发送给我",
		"把pdf发到微信",
		"删除邮箱中的所有邮件",
		"删除邮件.md",
		"请看带pdf附件的说明",
		"发送一封邀请",
	}
	for _, text := range yes {
		if !QueryWantsMessageSend(text) {
			t.Fatalf("want send: %q", text)
		}
	}
	for _, text := range no {
		if QueryWantsMessageSend(text) {
			t.Fatalf("want not send: %q", text)
		}
	}
	if !QueryWantsNewPDF("生成pdf后再次发送") {
		t.Fatal("generate-then-send must produce a PDF")
	}
	if QueryWantsNewPDF("向 znsoft@163.com 发送一封带PDF附件的邮件") {
		t.Fatal("an existing PDF attachment is not a new document")
	}
	if QueryWantsNewPDF("向 znsoft@163.com 发送一封带PDF附件的邮件，并附加刚生成的 PDF") {
		t.Fatal("a PDF that was just generated is not another create")
	}
}
