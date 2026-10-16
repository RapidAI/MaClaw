package mailbox

import "strings"

// Resolve picks one usable account. An empty name uses the single default.
// A name selects only that mailbox: a missing, paused, or wrong-direction
// mailbox fails and is not replaced by another account.
func Resolve(accounts []Account, name, direction string) (Account, error) {
	if len(accounts) == 0 {
		return Account{}, classErr("not_configured", MsgNotConfigured)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		for _, account := range accounts {
			if account.Enabled && account.Default {
				return requireDirection(account, direction)
			}
		}
		return Account{}, classErr("no_default", MsgNoDefault)
	}
	var found *Account
	for i := range accounts {
		if accounts[i].Name == name {
			cp := accounts[i]
			found = &cp
			break
		}
	}
	if found == nil {
		return Account{}, classErr("unknown_name", MsgUnknownName(name))
	}
	if !found.Enabled {
		return Account{}, classErr("disabled", MsgDisabled(name))
	}
	return requireDirection(*found, direction)
}

func requireDirection(account Account, direction string) (Account, error) {
	switch direction {
	case SideInbound:
		if account.Inbound == nil {
			return Account{}, classErr("no_inbound", MsgNoInbound(account.Name))
		}
	case SideOutbound:
		if account.Outbound == nil {
			return Account{}, classErr("no_outbound", MsgNoOutbound(account.Name))
		}
	}
	return account, nil
}
