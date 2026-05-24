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
	case QRLoginStatusWait:
		return QRLoginStatusWait
	case QRLoginStatusScanned, "scanned", "scan":
		return QRLoginStatusScanned
	case QRLoginStatusConfirmed, "confirm", "success", "succeeded", "connected", "done", "ok":
		return QRLoginStatusConfirmed
	case QRLoginStatusExpired:
		return QRLoginStatusExpired
	default:
		return QRLoginStatusUnknown
	}
}

func (status QRLoginStatus) String() string {
	return string(status)
}
