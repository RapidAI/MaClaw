package httpapi

import (
	"errors"
	"strings"
)

// registrationEmailLogIdentity redacts a contact email for logs.
func registrationEmailLogIdentity(email string) string {
	email = strings.TrimSpace(strings.ToLower(email))
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return "***"
	}
	local := email[:at]
	if len(local) > 2 {
		local = local[:2]
	}
	return local + "***" + email[at:]
}

func registrationEmailDomainLog(email string) string {
	email = strings.TrimSpace(strings.ToLower(email))
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return email[at+1:]
}

// registrationPhoneLogIdentity redacts a phone number for logs.
func registrationPhoneLogIdentity(phoneNumber string) string {
	phoneNumber = normalizePhoneNumber(phoneNumber)
	// Keep the redacted form stable for Chinese mobile numbers regardless of
	// whether the caller supplied the common +86 country prefix. This exposes
	// no additional digits while making the same account recognizable in logs.
	if len(phoneNumber) == 13 && strings.HasPrefix(phoneNumber, "86") && phoneNumber[2] == '1' {
		phoneNumber = phoneNumber[2:]
	}
	if phoneNumber == "" {
		return "***"
	}
	if len(phoneNumber) <= 4 {
		return "***" + phoneNumber
	}
	if len(phoneNumber) <= 7 {
		return phoneNumber[:2] + "***" + phoneNumber[len(phoneNumber)-2:]
	}
	return phoneNumber[:3] + "***" + phoneNumber[len(phoneNumber)-4:]
}

// registrationAccountLogIdentity redacts email or phone: identity values used at enroll time.
func registrationAccountLogIdentity(account string) string {
	account = strings.TrimSpace(account)
	if account == "" {
		return "***"
	}
	// Case-insensitive "phone:" prefix; strip by fixed ASCII length (6).
	if len(account) >= 6 && strings.EqualFold(account[:6], "phone:") {
		return "phone:" + registrationPhoneLogIdentity(account[6:])
	}
	if strings.Contains(account, "@") {
		return registrationEmailLogIdentity(account)
	}
	if len(account) <= 4 {
		return "***"
	}
	if len(account) <= 12 {
		return account[:2] + "***"
	}
	return account[:4] + "***"
}

func registrationPhoneRejectCode(err error) string {
	if err == nil {
		return ""
	}
	var already errPhoneAlreadyRegistered
	if errors.As(err, &already) {
		return "PHONE_ALREADY_REGISTERED"
	}
	var route errPhoneRegistrationRouteCheck
	if errors.As(err, &route) {
		return "PHONE_REGISTRATION_ROUTE_CHECK_FAILED"
	}
	var lookup errPhoneRegistrationLookup
	if errors.As(err, &lookup) {
		return "PHONE_REGISTRATION_LOOKUP_FAILED"
	}
	return "PHONE_REGISTRATION_LOOKUP_FAILED"
}
