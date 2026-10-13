package mailbox

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// StoredSecret is a password the host must write to the maclaw keyring.
// It is never placed in the tool result.
type StoredSecret struct {
	ID     string
	Side   string
	Secret string
}

// ApplyConfigure turns the connection details a user told the agent into one
// mailbox record. The returned secrets are not copied into the account.
func ApplyConfigure(existing []Account, args map[string]interface{}, now time.Time) ([]Account, Account, []StoredSecret, string, error) {
	if err := rejectConfigureArgs(args); err != nil {
		return nil, Account{}, nil, "", err
	}
	if strings.TrimSpace(stringArg(args, "action")) != "save" {
		return nil, Account{}, nil, "", classErr("invalid", "配置邮箱只支持 save")
	}
	email := strings.TrimSpace(stringArg(args, "email"))
	name := strings.TrimSpace(stringArg(args, "name"))
	if name == "" {
		local, _, _ := strings.Cut(email, "@")
		name = strings.TrimSpace(local)
	}
	username := strings.TrimSpace(stringArg(args, "username"))
	inbound := endpointFromMap(args["inbound"], true, username, email)
	outbound := endpointFromMap(args["outbound"], false, username, email)
	if inbound == nil && outbound == nil {
		return nil, Account{}, nil, "", classErr("invalid", "至少要有收件或发件服务器")
	}
	var id string
	for _, account := range existing {
		if account.Name == name {
			id = account.ID
			break
		}
	}
	var makeDefault *bool
	if raw, ok := args["make_default"].(bool); ok {
		makeDefault = &raw
	}
	enabled := true
	next, saved, err := UpsertAccount(existing, SaveRequest{
		ID: id, Name: name, Address: email, Enabled: &enabled, MakeDefault: makeDefault,
		Inbound: inbound, Outbound: outbound,
	}, now)
	if err != nil {
		return nil, Account{}, nil, "", err
	}
	password := stringArg(args, "password")
	var secrets []StoredSecret
	if strings.TrimSpace(password) != "" {
		if saved.Inbound != nil {
			secrets = append(secrets, StoredSecret{ID: saved.ID, Side: SideInbound, Secret: password})
		}
		if saved.Outbound != nil {
			secrets = append(secrets, StoredSecret{ID: saved.ID, Side: SideOutbound, Secret: password})
		}
	}
	if len(secrets) > 0 {
		return next, saved, secrets, fmt.Sprintf(MsgConfigured, saved.Name), nil
	}
	return next, saved, nil, fmt.Sprintf(MsgConfiguredNoSecret, saved.Name), nil
}

func endpointFromMap(raw interface{}, inbound bool, username, email string) *Endpoint {
	fields, ok := raw.(map[string]interface{})
	if !ok || len(fields) == 0 {
		return nil
	}
	host := strings.TrimSpace(stringArg(fields, "host"))
	if host == "" {
		return nil
	}
	user := strings.TrimSpace(stringArg(fields, "username"))
	if user == "" {
		user = username
	}
	ep := &Endpoint{
		Host: host, Port: intFrom(fields["port"]), Security: normalizeSecurity(stringArg(fields, "security")),
		Username: user, Box: strings.TrimSpace(stringArg(fields, "box")),
	}
	if !inbound {
		from := strings.TrimSpace(stringArg(fields, "from_address"))
		if from == "" {
			from = email
		}
		ep.FromAddress = from
	}
	return ep
}

func normalizeSecurity(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "tls", "ssl", "imaps", "smtps":
		return SecurityTLS
	case "starttls":
		return SecuritySTARTTLS
	case "none", "plain":
		return SecurityNone
	default:
		return strings.TrimSpace(raw)
	}
}

func intFrom(raw interface{}) int {
	switch n := raw.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

var configureArgs = map[string]bool{
	"action": true, "name": true, "email": true, "password": true, "username": true,
	"inbound": true, "outbound": true, "make_default": true,
}

func ConfigureProperties() map[string]interface{} {
	return schemaProperties(configureSchema())
}

func ConfigureInvocationSchema() map[string]interface{} { return configureSchema() }

func ToolDescriptionConfigure() string {
	return "根据用户提供的接入信息保存邮箱。action 只能是 save。邮箱名必须填写：用户提供了就用用户的，没提供就由你生成一个简短名字（可用地址本地部分），不要留空；重名则另起一个。同时填写 email、username、password，以及 inbound 和 outbound（host、port、security 用 tls、starttls 或 none，收件还可填 box）。IMAP 写入 inbound，SMTP 写入 outbound。密码只写入系统密钥环，不要把密码复述到回复里。这个工具不发送邮件。"
}

func configureSchema() map[string]interface{} {
	endpoint := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"host":         map[string]interface{}{"type": "string", "description": "服务器主机名"},
			"port":         map[string]interface{}{"type": "integer"},
			"security":     map[string]interface{}{"type": "string", "description": "tls、starttls 或 none。ssl 按 tls 保存。"},
			"username":     map[string]interface{}{"type": "string"},
			"box":          map[string]interface{}{"type": "string", "description": "IMAP 文件夹，默认 INBOX。"},
			"from_address": map[string]interface{}{"type": "string", "description": "发件人地址。不填则用 email。"},
		},
		"additionalProperties": false,
	}
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action":       map[string]interface{}{"type": "string", "enum": []string{"save"}},
			"name":         map[string]interface{}{"type": "string", "description": "邮箱名。用户提供了就填用户的；没提供就生成一个简短名字（可用地址本地部分）。不要留空。重名则另起。"},
			"email":        map[string]interface{}{"type": "string", "description": "邮箱地址"},
			"username":     map[string]interface{}{"type": "string"},
			"password":     map[string]interface{}{"type": "string", "description": "密码或授权码。只写入系统密钥环，不要写入回复。"},
			"inbound":      endpoint,
			"outbound":     endpoint,
			"make_default": map[string]interface{}{"type": "boolean"},
		},
		"required":             []string{"action", "email", "name"},
		"additionalProperties": false,
	}
}
