package botmgmt

import (
	"context"
	"strings"
	"time"
)

// deliverCloudMessage routes a no-desktop bot's task to a cloud_agents
// instance. It is the cloud twin of deliverDesktopMessage's srv /messages
// send: classify the text into session or coding, ensure the bot has a live
// instance of that kind (reusing one it already owns), then post the turn.
// The desktop path stays untouched for a bot that still has a computer — only
// a computer-less bot with a wired CloudRouter comes here.
func (s *Service) deliverCloudMessage(ctx context.Context, tenantID, userID, botID, botName, content, clientMessageID string) (BotAdmission, error) {
	inst, err := s.ensureCloudInstance(ctx, tenantID, userID, botID, botName, ClassifyBotTask(content))
	if err != nil {
		return BotAdmission{}, err
	}
	if err := s.CloudRouter.SendMessage(ctx, tenantID, userID, inst.ID, clientMessageID, content); err != nil {
		return BotAdmission{}, err
	}
	return BotAdmission{
		Accepted:        true,
		ClientMessageID: clientMessageID,
		// The cloud run is asynchronous like the desktop admit: the turn was
		// accepted, its request id lives in cloud_agent_turns, and the reply is
		// read back by the run-result poll. There is no settled text yet, so
		// the admission mirrors a desktop run id rather than a finished reply.
		Phase:  "execute",
		Kind:   "new",
		Ack:    desktopStartedAck(content),
		RunID:  clientMessageID,
		Session: botID,
	}, nil
}

// ensureCloudInstance returns the instance the bot should run this task on.
// kind is "session" or "coding" from ClassifyBotTask. Reuse prefers the newest
// live instance this bot owns of that kind; a stopped one is resumed first,
// and a Resume that fails drops the row and creates fresh — a stopped instance
// that will not wake is treated as unusable rather than retried forever.
// deleted and provision_failed rows never come back from FindReusable, so they
// can never swallow a message.
func (s *Service) ensureCloudInstance(ctx context.Context, tenantID, userID, botID, botName, kind string) (CloudInstance, error) {
	if inst, found, err := s.CloudRouter.FindReusable(ctx, tenantID, userID, botID, kind); err != nil {
		return CloudInstance{}, err
	} else if found {
		if inst.Status == "stopped" {
			if resumed, rerr := s.CloudRouter.Resume(ctx, tenantID, userID, inst.ID); rerr == nil {
				return resumed, nil
			}
			// Resume failed: fall through to Create a fresh instance.
		} else {
			return inst, nil
		}
	}
	return s.CloudRouter.Create(ctx, tenantID, userID, botID, cloudInstanceName(botName, kind), kind)
}

func cloudInstanceName(botName, kind string) string {
	botName = strings.TrimSpace(botName)
	if botName == "" {
		botName = "bot"
	}
	if kind == KindCoding {
		return "Bot " + botName + " 编程"
	}
	return "Bot " + botName + " 会话"
}

var _ = time.Now // keep time import when asserts are trimmed
