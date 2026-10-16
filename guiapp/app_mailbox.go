package guiapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/mailbox"
	"github.com/zalando/go-keyring"
)

// Mailbox passwords use the same OS keyring service as database profiles.
// The settings DTO never carries them. Accounts are not pushed to Hub.
// A desktop bot uses a mailbox only when that account is already in the
// user config the executor loads, and resolves the password locally.
var (
	mailboxKeyringSet    = keyring.Set
	mailboxKeyringGet    = keyring.Get
	mailboxKeyringDelete = keyring.Delete
	mailboxProbeDialer   mailbox.Dialer
	mailboxToolReader    mailbox.MailReader
	mailboxToolSender    mailbox.MailSender
	guiMailboxDrafts     = mailbox.NewDraftStore()
)

type mailboxAccountDTO struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Address     string            `json:"address"`
	Enabled     *bool             `json:"enabled"`
	MakeDefault *bool             `json:"default"`
	Inbound     *mailbox.Endpoint `json:"inbound"`
	Outbound    *mailbox.Endpoint `json:"outbound"`
	SignatureID *string           `json:"signature_id"`
}

type mailboxSignatureDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	MakeDefault *bool  `json:"default"`
}

func (a *App) MailboxAccountsList() ([]map[string]interface{}, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(cfg.MailboxAccounts))
	for _, account := range cfg.MailboxAccounts {
		out = append(out, mailbox.Summary(account, a.mailboxHasSecret(account.ID, mailbox.SideInbound), a.mailboxHasSecret(account.ID, mailbox.SideOutbound)))
	}
	return out, nil
}

func (a *App) MailboxAccountSave(payload map[string]interface{}) (string, error) {
	if payload == nil {
		return "", errors.New("邮箱配置不能为空")
	}
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	if err := mailbox.RejectSecretPayload(keys); err != nil {
		return "", err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", errors.New("邮箱配置无法识别")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var in mailboxAccountDTO
	if err := dec.Decode(&in); err != nil {
		return "", errors.New("邮箱配置无法识别")
	}
	in.Inbound = blankEndpoint(in.Inbound)
	in.Outbound = blankEndpoint(in.Outbound)
	cfg, err := a.LoadConfig()
	if err != nil {
		return "", err
	}
	if in.SignatureID != nil && strings.TrimSpace(*in.SignatureID) != "" && !signatureExists(cfg.MailboxSignatures, *in.SignatureID) {
		return "", errors.New(mailbox.MsgSignatureMissing(strings.TrimSpace(in.Name)))
	}
	next, saved, err := mailbox.UpsertAccount(cfg.MailboxAccounts, mailbox.SaveRequest{
		ID: in.ID, Name: in.Name, Address: in.Address, Enabled: in.Enabled,
		MakeDefault: in.MakeDefault, Inbound: in.Inbound, Outbound: in.Outbound, SignatureID: in.SignatureID,
	}, time.Now())
	if err != nil {
		return "", err
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) { cfg.MailboxAccounts = next }); err != nil {
		return "", err
	}
	return saved.ID, nil
}

func (a *App) MailboxAccountSetSecret(id, side, secret string) error {
	item, err := mailbox.KeyringItem(strings.TrimSpace(id), strings.TrimSpace(side))
	if err != nil {
		return err
	}
	if strings.TrimSpace(secret) == "" {
		return errors.New(mailbox.MsgNoSecret)
	}
	if err := mailboxKeyringSet(mailbox.KeyringService, item, secret); err != nil {
		return errors.New(mailbox.MsgKeyringLong)
	}
	return nil
}

func (a *App) MailboxAccountSetEnabled(id string, enabled bool) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	next, err := mailbox.SetEnabled(cfg.MailboxAccounts, id, enabled)
	if err != nil {
		return err
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) { cfg.MailboxAccounts = next }); err != nil {
		return err
	}
	return nil
}

func (a *App) MailboxAccountSetDefault(id string) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	next, err := mailbox.SetDefault(cfg.MailboxAccounts, id)
	if err != nil {
		return err
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) { cfg.MailboxAccounts = next }); err != nil {
		return err
	}
	return nil
}

func (a *App) MailboxAccountDelete(id string) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	next, removed, err := mailbox.DeleteAccount(cfg.MailboxAccounts, id)
	if err != nil {
		return err
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) { cfg.MailboxAccounts = next }); err != nil {
		return err
	}
	for _, side := range []string{mailbox.SideInbound, mailbox.SideOutbound} {
		if item, err := mailbox.KeyringItem(removed.ID, side); err == nil {
			_ = mailboxKeyringDelete(mailbox.KeyringService, item)
		}
	}
	return nil
}

func (a *App) MailboxAccountTest(id, side string) (string, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return "", err
	}
	var account mailbox.Account
	found := false
	for _, item := range cfg.MailboxAccounts {
		if item.ID == strings.TrimSpace(id) {
			account = item
			found = true
			break
		}
	}
	if !found {
		return "", errors.New("没有这个邮箱")
	}
	side = strings.TrimSpace(side)
	secret, err := a.mailboxSecret(context.Background(), mailbox.SecretRef(account.ID, side))
	if err != nil {
		return "", mailboxCallErr(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), mailbox.SettingsProbeTimeout)
	defer cancel()
	dialer, _, _ := a.mailboxIO()
	switch side {
	case mailbox.SideInbound:
		return mailbox.TestReceive(ctx, dialer, account, secret)
	case mailbox.SideOutbound:
		return mailbox.TestSend(ctx, dialer, account, secret)
	default:
		return "", errors.New("邮箱标识无效")
	}
}

// mailboxDraftTestDTO carries the unsaved editor values for a pre-save
// connection probe. AccountID plus UseSavedSecret lets an existing account
// fall back to its keyring secret while the password box stays empty.
type mailboxDraftTestDTO struct {
	AccountID      string `json:"account_id"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Security       string `json:"security"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	Box            string `json:"box"`
	FromAddress    string `json:"from_address"`
	UseSavedSecret bool   `json:"use_saved_secret"`
}

// MailboxAccountTestDraft dials with the editor's uncommitted values and
// writes nothing to config, so a failing probe leaves no partial account.
func (a *App) MailboxAccountTestDraft(spec mailboxDraftTestDTO, side string) (string, error) {
	side = strings.TrimSpace(side)
	if side != mailbox.SideInbound && side != mailbox.SideOutbound {
		return "", errors.New("邮箱标识无效")
	}
	host := strings.TrimSpace(spec.Host)
	if host == "" {
		return "", errors.New(mailbox.MsgNoHost)
	}
	secret := spec.Password
	if strings.TrimSpace(secret) == "" && spec.UseSavedSecret {
		saved, err := a.mailboxSecret(context.Background(), mailbox.SecretRef(strings.TrimSpace(spec.AccountID), side))
		if err != nil {
			return "", mailboxCallErr(err)
		}
		secret = saved
	}
	if strings.TrimSpace(secret) == "" {
		return "", errors.New(mailbox.MsgNoSecret)
	}
	endpoint := mailbox.Endpoint{
		Host:        host,
		Port:        spec.Port,
		Security:    strings.TrimSpace(spec.Security),
		Username:    strings.TrimSpace(spec.Username),
		Box:         strings.TrimSpace(spec.Box),
		FromAddress: strings.TrimSpace(spec.FromAddress),
	}
	account := mailbox.Account{Address: endpoint.FromAddress}
	if side == mailbox.SideInbound {
		account.Inbound = &endpoint
	} else {
		account.Outbound = &endpoint
	}
	ctx, cancel := context.WithTimeout(context.Background(), mailbox.SettingsProbeTimeout)
	defer cancel()
	dialer, _, _ := a.mailboxIO()
	if side == mailbox.SideInbound {
		return mailbox.TestReceive(ctx, dialer, account, secret)
	}
	return mailbox.TestSend(ctx, dialer, account, secret)
}

func (a *App) MailboxSignaturesList() ([]map[string]interface{}, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(cfg.MailboxSignatures))
	for _, sig := range cfg.MailboxSignatures {
		out = append(out, mailbox.SignatureSummary(sig))
	}
	return out, nil
}

func (a *App) MailboxSignatureSave(payload map[string]interface{}) (string, error) {
	if payload == nil {
		return "", errors.New("签名不能为空")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", errors.New("签名无法识别")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var in mailboxSignatureDTO
	if err := dec.Decode(&in); err != nil {
		return "", errors.New("签名无法识别")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return "", err
	}
	next, saved, err := mailbox.UpsertSignature(cfg.MailboxSignatures, mailbox.SignatureSave{
		ID: in.ID, Name: in.Name, Body: in.Body, MakeDefault: in.MakeDefault,
	}, time.Now())
	if err != nil {
		return "", err
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) { cfg.MailboxSignatures = next }); err != nil {
		return "", err
	}
	return saved.ID, nil
}

func (a *App) MailboxSignatureDelete(id string) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	next, accounts, err := mailbox.DeleteSignature(cfg.MailboxSignatures, cfg.MailboxAccounts, id)
	if err != nil {
		return err
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.MailboxSignatures = next
		cfg.MailboxAccounts = accounts
	}); err != nil {
		return err
	}
	return nil
}

func (a *App) MailboxSignatureSetDefault(id string) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	next, err := mailbox.SetDefaultSignature(cfg.MailboxSignatures, id)
	if err != nil {
		return err
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) { cfg.MailboxSignatures = next }); err != nil {
		return err
	}
	return nil
}

// MailboxToolHost is the GUI agent runtime. It uses the saved accounts,
// signatures, and keyring secrets. The desktop bot uses the accounts already
// in the user config it loads, and resolves passwords from its local store.
func (a *App) MailboxToolHost(principal, session, workingDir string, budget *mailbox.TurnBudget) *mailbox.Host {
	cfg, err := a.LoadConfig()
	if err != nil {
		cfg = corelib.AppConfig{}
	}
	if strings.TrimSpace(workingDir) == "" {
		workingDir = cfg.WorkingDirectory
	}
	_, reader, sender := a.mailboxIO()
	return &mailbox.Host{
		Accounts:   cfg.MailboxAccounts,
		Signatures: cfg.MailboxSignatures,
		Secrets:    a.mailboxSecret,
		Reader:     reader,
		Sender:     sender,
		WorkingDir: workingDir,
		Budget:     budget,
		Drafts:     guiMailboxDrafts,
		Principal:  principal,
		Session:    session,
	}
}

func (a *App) mailboxIO() (mailbox.Dialer, mailbox.MailReader, mailbox.MailSender) {
	dialer := mailboxProbeDialer
	var netDialer *mailbox.NetDialer
	if dialer == nil {
		netDialer = mailbox.NewNetDialer()
		dialer = netDialer
	}
	reader := mailboxToolReader
	sender := mailboxToolSender
	if reader == nil {
		if netDialer != nil {
			reader = netDialer
		} else if asReader, ok := dialer.(mailbox.MailReader); ok {
			reader = asReader
		}
	}
	if sender == nil {
		if netDialer != nil {
			sender = netDialer
		} else if asSender, ok := dialer.(mailbox.MailSender); ok {
			sender = asSender
		}
	}
	return dialer, reader, sender
}

func (a *App) mailboxSecret(ctx context.Context, ref string) (string, error) {
	item, err := mailbox.ParseSecretRef(ref)
	if err != nil {
		return "", mailbox.ErrKeyring
	}
	secret, err := mailboxKeyringGet(mailbox.KeyringService, item)
	if err != nil {
		if mailboxSecretMissing(err) {
			return "", errors.New("not found")
		}
		return "", mailbox.ErrKeyring
	}
	current, updated, err := mailbox.MaterializeSecret(ctx, secret, time.Now())
	if err != nil {
		return "", err
	}
	if updated != "" {
		if err := writeMailboxSecretBothSides(mailboxIDFromRef(ref), updated); err != nil {
			return "", err
		}
	}
	return current, nil
}

func mailboxIDFromRef(ref string) string {
	item, err := mailbox.ParseSecretRef(ref)
	if err != nil {
		return ""
	}
	id, _, ok := strings.Cut(strings.TrimPrefix(item, "mailbox/"), "/")
	if !ok {
		return ""
	}
	return id
}

func writeMailboxSecretBothSides(id, secret string) error {
	if strings.TrimSpace(secret) == "" {
		return errors.New(mailbox.MsgNoSecret)
	}
	type prior struct {
		item   string
		value  string
		stored bool
	}
	prev := make([]prior, 0, 2)
	for _, side := range []string{mailbox.SideInbound, mailbox.SideOutbound} {
		item, err := mailbox.KeyringItem(id, side)
		if err != nil {
			return err
		}
		entry := prior{item: item}
		value, err := mailboxKeyringGet(mailbox.KeyringService, item)
		if err != nil && !mailboxSecretMissing(err) {
			return errors.New(mailbox.MsgKeyringLong)
		}
		if err == nil {
			entry.value = value
			entry.stored = true
		}
		prev = append(prev, entry)
	}
	for i, entry := range prev {
		if err := mailboxKeyringSet(mailbox.KeyringService, entry.item, secret); err != nil {
			for _, undone := range prev[:i] {
				if undone.stored {
					_ = mailboxKeyringSet(mailbox.KeyringService, undone.item, undone.value)
					continue
				}
				_ = mailboxKeyringDelete(mailbox.KeyringService, undone.item)
			}
			return errors.New(mailbox.MsgKeyringLong)
		}
	}
	return nil
}

func mailboxCallErr(err error) error {
	if err == nil {
		return nil
	}
	var class *mailbox.ClassError
	if errors.As(err, &class) && class != nil && class.Message != "" {
		return errors.New(class.Message)
	}
	if errors.Is(err, mailbox.ErrKeyring) {
		return errors.New(mailbox.MsgKeyringLong)
	}
	return errors.New(mailbox.MsgNoSecret)
}

func (a *App) mailboxHasSecret(id, side string) bool {
	item, err := mailbox.KeyringItem(id, side)
	if err != nil {
		return false
	}
	secret, err := mailboxKeyringGet(mailbox.KeyringService, item)
	return err == nil && strings.TrimSpace(secret) != ""
}

func blankEndpoint(ep *mailbox.Endpoint) *mailbox.Endpoint {
	if ep == nil || strings.TrimSpace(ep.Host) == "" {
		return nil
	}
	return ep
}

func signatureExists(sigs []mailbox.Signature, id string) bool {
	id = strings.TrimSpace(id)
	for _, sig := range sigs {
		if sig.ID == id {
			return true
		}
	}
	return false
}

func mailboxSecretMissing(err error) bool {
	if err == nil {
		return false
	}
	low := strings.ToLower(err.Error())
	return strings.Contains(low, "not found") || strings.Contains(low, "no such") || strings.Contains(low, "item not")
}
