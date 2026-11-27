package httpapi

import (
	"context"

	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
	"github.com/RapidAI/CodeClaw/hub/internal/cloudagent"
)

// botCloudRouter adapts cloudagent.Service to botmgmt.CloudRouter. botmgmt
// cannot import cloudagent (cloudagent already imports botmgmt), so the
// dependency is inverted: this package owns both and bridges them here. Each
// method is a thin forward that renames cloudagent.Instance to the router's
// CloudInstance view.
type botCloudRouter struct {
	svc *cloudagent.Service
}

func cloudInstanceView(inst cloudagent.Instance) botmgmt.CloudInstance {
	return botmgmt.CloudInstance{ID: inst.ID, Kind: inst.Kind, Status: inst.Status}
}

func (r botCloudRouter) FindReusable(ctx context.Context, tenantID, userID, botID, kind string) (botmgmt.CloudInstance, bool, error) {
	inst, found, err := r.svc.FindReusable(ctx, tenantID, userID, botID, kind)
	if err != nil || !found {
		return botmgmt.CloudInstance{}, found, err
	}
	return cloudInstanceView(inst), true, nil
}

func (r botCloudRouter) Create(ctx context.Context, tenantID, userID, botID, name, kind string) (botmgmt.CloudInstance, error) {
	inst, err := r.svc.Create(ctx, tenantID, userID, cloudagent.CreateInput{
		Name:    name,
		Kind:    kind,
		LLMMode: cloudagent.LLMOfficial,
		BotID:   botID,
	})
	if err != nil {
		return botmgmt.CloudInstance{}, err
	}
	return cloudInstanceView(inst), nil
}

func (r botCloudRouter) Resume(ctx context.Context, tenantID, userID, instanceID string) (botmgmt.CloudInstance, error) {
	inst, err := r.svc.Resume(ctx, tenantID, userID, instanceID)
	if err != nil {
		return botmgmt.CloudInstance{}, err
	}
	return cloudInstanceView(inst), nil
}

func (r botCloudRouter) Delete(ctx context.Context, tenantID, userID, instanceID string) error {
	return r.svc.Delete(ctx, tenantID, userID, instanceID)
}

func (r botCloudRouter) SendMessage(ctx context.Context, tenantID, userID, instanceID, clientMessageID, text string) error {
	_, err := r.svc.Dispatcher().SendMessage(ctx, tenantID, userID, instanceID, clientMessageID, text)
	return err
}
