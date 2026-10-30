package mailbox

import "strings"

// QueryWantsMessageSend reports a request to send through a configured
// mailbox now. An IM destination such as 微信, a local file delete, and a
// mail delete stay on their own labels. A PDF that is only being generated,
// with no send, is not this request.
func QueryWantsMessageSend(userMessage string) bool {
	q := strings.ToLower(strings.TrimSpace(userMessage))
	if q == "" || QueryWantsMessageDelete(userMessage) || QuerySchedulesMessageDelete(userMessage) || queryAsksAboutMailDelete(q) {
		return false
	}
	if queryNamesIMDelivery(q) && !queryHasEmailAddress(q) {
		return false
	}
	if queryHasExplicitMailSend(q) {
		return true
	}
	if queryHasEmailAddress(q) && queryHasOutboundVerb(q) {
		return true
	}
	return queryWantsResend(q) && (strings.Contains(q, "pdf") || strings.Contains(q, "邮件") || strings.Contains(q, "附件") || strings.Contains(q, "email"))
}

// QueryWantsNewPDF reports that this send also has to produce a new PDF.
// Mentioning a PDF that already exists ("带PDF附件", "附加刚生成的") is not
// a create request. "生成" after 刚 or 已, and "生成的", name that file.
func QueryWantsNewPDF(userMessage string) bool {
	q := strings.ToLower(strings.TrimSpace(userMessage))
	if q == "" || !strings.Contains(q, "pdf") {
		return false
	}
	for _, spoken := range []string{"刚生成", "已生成", "生成的", "生成好"} {
		q = strings.ReplaceAll(q, spoken, "")
	}
	return strings.Contains(q, "生成") || strings.Contains(q, "做成") || strings.Contains(q, "generate")
}

func queryHasExplicitMailSend(q string) bool {
	for _, phrase := range []string{
		"发邮件", "发送邮件", "写邮件", "发一封邮件",
		"邮件发给",
		"send an email", "send email", "email this", "email the", "email it",
	} {
		if strings.Contains(q, phrase) {
			return true
		}
	}
	return false
}

func queryHasOutboundVerb(q string) bool {
	if strings.Contains(q, "发送") || strings.Contains(q, "发给") || strings.Contains(q, "发到") {
		return true
	}
	return asciiToken(q, "send")
}

func queryWantsResend(q string) bool {
	for _, phrase := range []string{"再次发送", "重新发送", "后再发送", "后再次发送", "然后再发"} {
		if strings.Contains(q, phrase) {
			return true
		}
	}
	return false
}

func queryNamesIMDelivery(q string) bool {
	for _, phrase := range []string{"微信", "weixin", "wechat", "蓝信", "lansenger"} {
		if strings.Contains(q, phrase) {
			return true
		}
	}
	return false
}

func queryHasEmailAddress(q string) bool {
	at := strings.IndexByte(q, '@')
	if at <= 0 || at+1 >= len(q) {
		return false
	}
	local := q[:at]
	domain := q[at+1:]
	if i := strings.LastIndexAny(local, " \t\r\n,，;；:：<>\"'()[]"); i >= 0 {
		local = local[i+1:]
	}
	if i := strings.IndexAny(domain, " \t\r\n,，;；:：<>\"'()[]"); i >= 0 {
		domain = domain[:i]
	}
	if local == "" || !strings.Contains(domain, ".") {
		return false
	}
	return true
}
