package botmgmt

import (
	"context"
	"errors"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

type settleStopDesk struct {
	stopped []string
}

func (d *settleStopDesk) Open(context.Context, string, string) (string, error) { return "", nil }
func (d *settleStopDesk) Stop(_ context.Context, _, userID string) error {
	d.stopped = append(d.stopped, userID)
	return nil
}

type settleFailSettings struct{}

func (settleFailSettings) Get(context.Context, string) (string, error) {
	return "", errors.New("settings unreadable")
}
func (settleFailSettings) Set(context.Context, string, string) error {
	return errors.New("settings unreadable")
}

func TestSettleKeepsTheAnswerWhenSettingsFail(t *testing.T) {
	desk := &settleStopDesk{}
	svc := NewService(settleFailSettings{})
	svc.Desktop = desk
	carried := desktop.BotDesktopUser("alice", "bot_1")
	reply, err := svc.settleDesktopCommand("tenant-a", "alice", "bot_1", "inst", "", true, desktopCommandBody{
		CallErr:     errors.New("run failed"),
		Text:        "训练结束了",
		Metadata:    map[string]string{"send_to_user": "训练完成，f1 0.81。"},
		Waiting:     true,
		DesktopUser: carried,
	})
	if err == nil || err.Error() != "run failed" || reply.SendToUser != "训练完成，f1 0.81。" {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
	if len(desk.stopped) != 1 || desk.stopped[0] != carried {
		t.Fatalf("desktop release %v", desk.stopped)
	}
}

func TestSettleKeepsTheAnswerWhenTheBotIsGone(t *testing.T) {
	desk := &settleStopDesk{}
	svc := NewService(&memSettings{m: map[string]string{}})
	svc.Desktop = desk
	carried := desktop.BotDesktopUser("alice", "bot_1")
	reply, err := svc.settleDesktopCommand("tenant-a", "alice", "bot_1", "inst", "", true, desktopCommandBody{
		CallErr:     errors.New("run failed"),
		Text:        "训练结束了",
		Metadata:    map[string]string{"send_to_user": "没做成，需要换数据集。"},
		Waiting:     true,
		DesktopUser: carried,
	})
	if err == nil || err.Error() != "run failed" || reply.SendToUser != "没做成，需要换数据集。" {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
	if len(desk.stopped) != 1 || desk.stopped[0] != carried {
		t.Fatalf("desktop release %v", desk.stopped)
	}
}
