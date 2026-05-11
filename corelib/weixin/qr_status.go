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
	switch QRLoginStatus(strings.TrimSpace(string(status))) {
	case QRLoginStatusWait:
		return QRLoginStatusWait
	case QRLoginStatusScanned:
		return QRLoginStatusScanned
	case QRLoginStatusConfirmed:
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
