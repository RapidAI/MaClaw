package weixin

import "strings"

type QRLoginStatus string

const (
	QRLoginStatusUnknown   QRLoginStatus = ""
	QRLoginStatusWait      QRLoginStatus = "wait"
	QRLoginStatusScanned   QRLoginStatus = "scaned"
	QRLoginStatusConfirmed QRLoginStatus = "confirmed"
	QRLoginStatusExpired   QRLoginStatus = "expired"
)

func NormalizeQRLoginStatus(status QRLoginStatus) QRLoginStatus {
	switch QRLoginStatus(strings.ToLower(strings.TrimSpace(string(status)))) {
	case QRLoginStatusWait, "waiting", "pending", "polling", "timeout":
		return QRLoginStatusWait
	case QRLoginStatusScanned, "scanned", "scan":
		return QRLoginStatusScanned
	case QRLoginStatusConfirmed, "confirm", "success", "succeeded", "connected", "done", "ok":
		return QRLoginStatusConfirmed
	case QRLoginStatusExpired, "expire":
		return QRLoginStatusExpired
	default:
		return QRLoginStatusUnknown
	}
}

func IsQRLoginWaitMessage(message string) bool {
	switch strings.ToLower(strings.TrimSpace(message)) {
	case "timeout":
		return true
	default:
		return false
	}
}

func (status QRLoginStatus) String() string {
	return string(status)
}
