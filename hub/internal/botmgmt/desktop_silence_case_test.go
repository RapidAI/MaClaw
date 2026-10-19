package botmgmt

import (
	"fmt"
	"strings"
	"testing"
)

func TestDesktopBotSilenceCases(t *testing.T) {
	var s *Service
	reply, err := s.settleDesktopCommand("tenant", "user", "bot", "instance", "", false, desktopCommandBody{})
	if err != nil || reply.Text != "" || reply.SendToUser != "" {
		t.Fatalf("unattended empty = %+v err=%v", reply, err)
	}
	_, err = s.settleDesktopCommand("tenant", "user", "bot", "instance", "", false, desktopCommandBody{Waiting: true})
	if err == nil || err.Error() != "run ended without a result" {
		t.Fatalf("waiting empty = %v", err)
	}
	reply, err = s.settleDesktopCommand("tenant", "user", "bot", "instance", "", false, desktopCommandBody{
		Waiting:  true,
		Metadata: map[string]string{"send_to_user": "首页没打开。", "text": "无变化"},
		Text:     "无变化",
	})
	if err != nil || reply.SendToUser != "首页没打开。" || strings.Contains(reply.SendToUser, "无变化") {
		t.Fatalf("sentence = %+v err=%v", reply, err)
	}
	reply, err = s.settleDesktopCommand("tenant", "user", "bot", "instance", "", false, desktopCommandBody{
		Text: "无变化",
	})
	if err != nil || reply.Text != "无变化" || reply.SendToUser != "" {
		t.Fatalf("filler prose = %+v err=%v", reply, err)
	}
	reply, err = s.settleDesktopCommand("tenant", "user", "bot", "instance", "", false, desktopCommandBody{
		Waiting: true,
		Text:    "无变化",
	})
	if err != nil || reply.Text != "无变化" || reply.SendToUser != "" {
		t.Fatalf("waiting filler prose = %+v err=%v", reply, err)
	}
	reply, err = s.settleDesktopCommand("tenant", "user", "bot", "instance", "", false, desktopCommandBody{
		CallErr: fmt.Errorf("browser broke"),
		Text:    "安装过程写在这里",
		Metadata: map[string]string{
			"ask_user_question":     "下一步？",
			"ask_user_input_type":   "secret",
			"ask_user_secret_name":  "SITE_PASSWORD",
			"ask_user_options_json": `["继续"]`,
		},
	})
	if err == nil || err.Error() != "browser broke" || reply.Text != "" || reply.SendToUser != "" || reply.AskUserQuestion != "下一步？" || reply.AskUserSecretName != "SITE_PASSWORD" || reply.AskUserInputType != "secret" || reply.AskUserOptionsJSON != `["继续"]` {
		t.Fatalf("failed ask = %+v err=%v", reply, err)
	}
	if strings.Contains(reply.Text, "安装过程") {
		t.Fatalf("failed reply kept prose: %+v", reply)
	}
	reply, err = s.settleDesktopCommand("tenant", "user", "bot", "instance", "", false, desktopCommandBody{
		CallErr:   fmt.Errorf("browser broke"),
		Handoff:   true,
		Attention: "login_wall",
		Text:      "安装过程写在这里",
		Metadata:  map[string]string{"ask_user_question": "请输入密码。", "ask_user_secret_name": "SITE_PASSWORD"},
	})
	if err == nil || err.Error() != "browser broke" || !reply.Handoff || reply.AttentionReason != "login_wall" || reply.Text != "" || reply.SendToUser != "" || reply.AskUserQuestion != "请输入密码。" || reply.AskUserSecretName != "SITE_PASSWORD" {
		t.Fatalf("failed handoff = %+v err=%v", reply, err)
	}
	if strings.Contains(reply.Text, "安装过程") {
		t.Fatalf("failed handoff kept prose: %+v", reply)
	}
	reply, err = s.settleDesktopCommand("tenant", "user", "bot", "instance", "", false, desktopCommandBody{
		Waiting: true,
		Metadata: map[string]string{
			"ask_user_question":     " ",
			"ask_user_options_json": `["继续","停下"]`,
		},
	})
	if err != nil || reply.AskUserQuestion != "" || reply.AskUserOptionsJSON != `["继续","停下"]` || reply.SendToUser != "" {
		t.Fatalf("blank question with choices = %+v err=%v", reply, err)
	}
	_, err = s.settleDesktopCommand("tenant", "user", "bot", "instance", "", false, desktopCommandBody{
		Waiting: true,
		Metadata: map[string]string{
			"ask_user_question":     " ",
			"ask_user_options_json": `["无变化", " "]`,
		},
	})
	if err == nil || err.Error() != "run ended without a result" {
		t.Fatalf("placeholder labels kept an empty run: %v", err)
	}
	reply, err = s.settleDesktopCommand("tenant", "user", "bot", "instance", "", false, desktopCommandBody{
		CallErr:   fmt.Errorf("browser broke"),
		Attention: "payment_confirm",
		Text:      "安装过程写在这里",
	})
	if err == nil || err.Error() != "browser broke" || reply.Handoff || reply.AttentionReason != "payment_confirm" || reply.Text != "" || reply.NovncURL != "" {
		t.Fatalf("failed attention = %+v err=%v", reply, err)
	}
}
