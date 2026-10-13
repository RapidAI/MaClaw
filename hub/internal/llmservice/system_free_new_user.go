package llmservice

// RegistryIgnoringSystemFreeNewUserLimit returns a billing view where this
// owner's system-free new-user limit card does not constrain the request.
// The new-user allowance on system-free applies only to the MaClaw desktop
// app. Bot and other non-desktop system-free calls use this view so they
// neither consume nor get blocked by that card. The returned registry is a
// copy; saving it would drop the real card.
func RegistryIgnoringSystemFreeNewUserLimit(reg *Registry, userID, email string) *Registry {
	if reg == nil {
		return nil
	}
	clone := *reg
	clone.Grants = append([]Grant(nil), reg.Grants...)
	clone.BillingReservations = append([]BillingReservation(nil), reg.BillingReservations...)
	owner := newUserAccountRef(userID, email)
	if owner.empty() {
		return &clone
	}
	for i := range clone.Grants {
		grant := clone.Grants[i]
		if grant.Frozen || !isNewUserLimitCardSource(grant.Source) || !IsSystemFreeServiceGroup(grant.ServiceGroupID) {
			continue
		}
		if !grantMatchesUser(grant, owner) {
			continue
		}
		clone.Grants[i].Frozen = true
	}
	return &clone
}
