package remotecontrol

import (
	"context"
	"crypto/subtle"
	"fmt"
	"strings"
	"time"
)

func (s *Service) notifyControllerResult(ctx context.Context, grant Grant, title, content, sourceID string) {
	if grant.ControllerHub == "" {
		s.notifyResult(ctx, grant.ControllerUserID, title, content, sourceID)
	}
}

func (s *Service) notifyTargetResult(ctx context.Context, grant Grant, title, content, sourceID string) {
	if grant.TargetHub == "" {
		s.notifyResult(ctx, grant.TargetUserID, title, content, sourceID)
	}
}

func federationSecretMatches(stored, provided string) bool {
	stored = strings.TrimSpace(stored)
	provided = strings.TrimSpace(provided)
	return len(stored) >= 32 && len(stored) == len(provided) && subtle.ConstantTimeCompare([]byte(stored), []byte(provided)) == 1
}

func (s *Service) InviteFederated(ctx context.Context, tenantID, userID, controllerMachineID, account, permission, controllerHub, targetHub, secret string) (Grant, error) {
	if err := s.ready(); err != nil {
		return Grant{}, err
	}
	permission, err := normalizePermission(permission)
	if err != nil {
		return Grant{}, err
	}
	controller, err := s.requireOwnMachine(ctx, userID, controllerMachineID)
	if err != nil {
		return Grant{}, err
	}
	account = strings.TrimSpace(account)
	controllerHub = strings.TrimRight(strings.TrimSpace(controllerHub), "/")
	targetHub = strings.TrimRight(strings.TrimSpace(targetHub), "/")
	if account == "" || controllerHub == "" || targetHub == "" || len(strings.TrimSpace(secret)) < 32 {
		return Grant{}, invalid("跨 Hub 远控请求无效")
	}
	row := s.db.QueryRowContext(ctx, grantSelect+` WHERE tenant_id = ? AND controller_user_id = ? AND controller_machine_id = ? AND target_account = ? AND target_hub = ? AND status = ? AND target_ignored = 0`, strings.TrimSpace(tenantID), userID, controller.ID, account, targetHub, StatusPending)
	if existing, scanErr := scanGrant(row); scanErr == nil {
		return existing, nil
	}
	now := time.Now().UTC()
	grant := Grant{
		ID: newID(), TenantID: strings.TrimSpace(tenantID), ControllerUserID: userID,
		ControllerMachineID: controller.ID, ControllerMachineName: displayMachine(controller),
		TargetAccount: account, ControllerHub: controllerHub, TargetHub: targetHub, FederationSecret: strings.TrimSpace(secret),
		Permission: permission, Status: StatusPending, CreatedAt: now, ExpiresAt: now.Add(inviteTTL), UpdatedAt: now,
	}
	if err := s.insert(ctx, grant); err != nil {
		return Grant{}, err
	}
	return grant, nil
}

func (s *Service) ImportFederatedInvite(ctx context.Context, grant Grant) error {
	if err := s.ready(); err != nil {
		return err
	}
	grant.ID = strings.TrimSpace(grant.ID)
	grant.TargetUserID = strings.TrimSpace(grant.TargetUserID)
	grant.ControllerHub = strings.TrimRight(strings.TrimSpace(grant.ControllerHub), "/")
	grant.TargetHub = strings.TrimRight(strings.TrimSpace(grant.TargetHub), "/")
	grant.FederationSecret = strings.TrimSpace(grant.FederationSecret)
	if grant.ID == "" || grant.ControllerUserID == "" || grant.ControllerMachineID == "" || grant.TargetUserID == "" || grant.TargetAccount == "" || grant.ControllerHub == "" || grant.TargetHub == "" || len(grant.FederationSecret) < 32 {
		return invalid("跨 Hub 远控邀请无效")
	}
	if _, err := normalizePermission(grant.Permission); err != nil {
		return err
	}
	if existing, err := s.load(ctx, grant.ID); err == nil {
		if federationSecretMatches(existing.FederationSecret, grant.FederationSecret) && existing.ControllerHub == grant.ControllerHub {
			return nil
		}
		return denied("远控邀请标识冲突")
	}
	now := time.Now().UTC()
	grant.Status = StatusPending
	grant.SameAccount = false
	grant.TargetIgnored = false
	grant.TargetMachineID = ""
	grant.TargetMachineName = ""
	grant.InviteNotificationID = ""
	grant.NoticeNotificationID = ""
	grant.CreatedAt = now
	grant.UpdatedAt = now
	if grant.ExpiresAt.IsZero() || now.Add(inviteTTL).Before(grant.ExpiresAt) {
		grant.ExpiresAt = now.Add(inviteTTL)
	}
	if err := s.insert(ctx, grant); err != nil {
		return err
	}
	s.notifyInvite(ctx, grant)
	return nil
}

func (s *Service) FederationGrant(ctx context.Context, grantID, secret string) (Grant, error) {
	grant, err := s.load(ctx, grantID)
	if err != nil || !federationSecretMatches(grant.FederationSecret, secret) {
		return Grant{}, ErrNotFound
	}
	return grant, nil
}

func (s *Service) FederatedGrantForTarget(ctx context.Context, userID, grantID string) (Grant, error) {
	grant, err := s.load(ctx, grantID)
	if err != nil || grant.TargetUserID != userID || grant.ControllerHub == "" {
		return Grant{}, ErrNotFound
	}
	return grant, nil
}

func (s *Service) ControllerGrant(ctx context.Context, tenantID, userID, machineID, grantID string) (Grant, error) {
	grant, err := s.load(ctx, grantID)
	if err != nil {
		return Grant{}, err
	}
	if grant.TenantID != strings.TrimSpace(tenantID) || grant.ControllerUserID != userID || grant.ControllerMachineID != strings.TrimSpace(machineID) {
		return Grant{}, denied("不能把远控转给其它机器")
	}
	return grant, nil
}

func (s *Service) AcceptFederated(ctx context.Context, grantID, secret, targetMachineID, targetMachineName string) (Grant, error) {
	grant, err := s.FederationGrant(ctx, grantID, secret)
	if err != nil {
		return Grant{}, err
	}
	targetMachineID = strings.TrimSpace(targetMachineID)
	if grant.Status == StatusActive && grant.TargetMachineID == targetMachineID {
		return grant, nil
	}
	if grant.Status != StatusPending || (!grant.ExpiresAt.IsZero() && time.Now().UTC().After(grant.ExpiresAt)) {
		return Grant{}, ErrNotFound
	}
	if targetMachineID == "" {
		return Grant{}, invalid("被控机器无效")
	}
	now := time.Now().UTC()
	grant.TargetMachineID = targetMachineID
	grant.TargetMachineName = strings.TrimSpace(targetMachineName)
	grant.Status = StatusActive
	grant.TargetIgnored = false
	grant.DecidedAt = now
	grant.UpdatedAt = now
	if err := s.update(ctx, grant); err != nil {
		return Grant{}, err
	}
	s.revokeNotice(ctx, grant.InviteNotificationID)
	s.notifyResult(ctx, grant.ControllerUserID, "远控已接受", fmt.Sprintf("%s 已接受远控，权限：%s。任务列表里已出现这台机器。", grant.TargetMachineName, permissionLabel(grant.Permission)), grant.ID)
	return grant, nil
}

func (s *Service) RejectFederated(ctx context.Context, grantID, secret string) error {
	grant, err := s.FederationGrant(ctx, grantID, secret)
	if err != nil {
		return err
	}
	if grant.Status == StatusRejected {
		return nil
	}
	if grant.Status != StatusPending {
		return ErrNotFound
	}
	now := time.Now().UTC()
	grant.Status = StatusRejected
	grant.DecidedAt = now
	grant.UpdatedAt = now
	if err := s.update(ctx, grant); err != nil {
		return err
	}
	s.revokeNotice(ctx, grant.InviteNotificationID)
	s.notifyResult(ctx, grant.ControllerUserID, "远控已拒绝", grant.TargetAccount+" 已拒绝远控。", grant.ID)
	return nil
}

func (s *Service) RevokeFederated(ctx context.Context, grantID, secret string, fromController bool) error {
	grant, err := s.FederationGrant(ctx, grantID, secret)
	if err != nil {
		return err
	}
	if grant.Status == StatusRevoked {
		return nil
	}
	if grant.Status != StatusActive && grant.Status != StatusPending {
		return ErrNotFound
	}
	previous := grant.Status
	now := time.Now().UTC()
	grant.Status = StatusRevoked
	grant.DecidedAt = now
	grant.UpdatedAt = now
	if err := s.update(ctx, grant); err != nil {
		return err
	}
	s.revokeNotice(ctx, grant.InviteNotificationID)
	s.revokeNotice(ctx, grant.NoticeNotificationID)
	if previous == StatusActive {
		if fromController {
			s.notifyResult(ctx, grant.TargetUserID, "远控已结束", "对方已结束对这台电脑的远控。", grant.ID)
		} else {
			s.notifyResult(ctx, grant.ControllerUserID, "远控已结束", grant.TargetMachineName+" 已结束远控。", grant.ID)
		}
	}
	return nil
}
