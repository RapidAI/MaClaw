package messenger

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// IncomingMessage is one user message or postback worth an agent turn.
// EditNum is 0 for the original delivery. An edit reuses MessageID, so the
// count keeps that turn distinct from the message Meta already delivered.
// ReplyToMID is the message.reply_to.mid Meta sent. It is not part of Text
// and not part of the dedupe key. The quoted body is not in the webhook.
// ReplyQuote is the body this process already stored for that mid. Parsing
// leaves it empty; dispatch fills it before the new mid can evict the parent.
type IncomingMessage struct {
	PSID       string
	PageID     string
	Text       string
	MessageID  string
	ReplyToMID string
	ReplyQuote string
	Timestamp  int64
	EditNum    int
}

type webhookPayload struct {
	Object string         `json:"object"`
	Entry  []webhookEntry `json:"entry"`
}

type webhookEntry struct {
	ID        string           `json:"id"`
	Messaging []messagingEvent `json:"messaging"`
}

type messagingEvent struct {
	Sender struct {
		ID string `json:"id"`
	} `json:"sender"`
	Recipient struct {
		ID string `json:"id"`
	} `json:"recipient"`
	Timestamp int64 `json:"timestamp"`
	Message   *struct {
		MID        string `json:"mid"`
		Text       string `json:"text"`
		IsEcho     bool   `json:"is_echo"`
		QuickReply *struct {
			Payload flexString `json:"payload"`
		} `json:"quick_reply"`
		// reply_to names the message the user quoted. The body is not included.
		ReplyTo *struct {
			MID         string `json:"mid"`
			IsSelfReply bool   `json:"is_self_reply"`
		} `json:"reply_to"`
		Attachments []messageAttachment `json:"attachments"`
		// A shops or click-to-Messenger ad attaches this to the user's text.
		Referral *messagingReferral `json:"referral"`
	} `json:"message"`
	MessageEdit *struct {
		MID     string  `json:"mid"`
		Text    string  `json:"text"`
		NumEdit flexInt `json:"num_edit"`
	} `json:"message_edit"`
	Postback *struct {
		MID      string             `json:"mid"`
		Title    string             `json:"title"`
		Payload  flexString         `json:"payload"`
		Referral *messagingReferral `json:"referral"`
	} `json:"postback"`
	// referral is its own webhook event when an m.me link or ad opens an
	// existing thread. There is no message body, and the 24-hour window opens.
	Referral *messagingReferral `json:"referral"`
	Lead     *messagingLead     `json:"lead"`
	// hop_context marks a lead-ad summary. That summary is not a thread message.
	HopContext *struct {
		Metadata string `json:"metadata"`
	} `json:"hop_context"`
	Delivery json.RawMessage `json:"delivery"`
	Read     json.RawMessage `json:"read"`
}

// messagingReferral is an ad, m.me, or shops handoff. Ids are flexString
// because Meta sends them as either JSON strings or numbers.
type messagingReferral struct {
	Ref        flexString `json:"ref"`
	Source     string     `json:"source"`
	Type       string     `json:"type"`
	AdID       flexString `json:"ad_id"`
	RefererURI string     `json:"referer_uri"`
	Product    *struct {
		ID flexString `json:"id"`
	} `json:"product"`
	AdsContext *struct {
		AdTitle   string     `json:"ad_title"`
		PhotoURL  string     `json:"photo_url"`
		VideoURL  string     `json:"video_url"`
		PostID    flexString `json:"post_id"`
		ProductID flexString `json:"product_id"`
	} `json:"ads_context_data"`
}

type messagingLead struct {
	Data []struct {
		Question string     `json:"question"`
		Answer   flexString `json:"answer"`
	} `json:"data"`
}

type messageAttachment struct {
	Type    string            `json:"type"`
	Payload attachmentPayload `json:"payload"`
}

type attachmentPayload struct {
	URL         string     `json:"url"`
	Title       string     `json:"title"`
	ID          flexString `json:"id"`
	StickerID   flexString `json:"sticker_id"`
	ReelVideoID flexString `json:"reel_video_id"`
	Coordinates *struct {
		Lat  float64 `json:"lat"`
		Long float64 `json:"long"`
	} `json:"coordinates"`
	BookingID string `json:"booking_id"`
	Status    string `json:"status"`
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
	Timezone  string `json:"timezone"`
	Product   *struct {
		Elements []productElement `json:"elements"`
	} `json:"product"`
}

type productElement struct {
	ID         flexString `json:"id"`
	RetailerID flexString `json:"retailer_id"`
	ImageURL   string     `json:"image_url"`
	Title      string     `json:"title"`
	Subtitle   string     `json:"subtitle"`
}

// flexString accepts a JSON string or number. Quick-reply payloads, postback
// payloads, sticker ids, reel video ids, and referral ids are documented as
// either, and a number must not reject the rest of the webhook.
type flexString string

func (s *flexString) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*s = flexString(value)
		return nil
	}
	if data[0] == '{' || data[0] == '[' {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err != nil {
		return nil
	}
	*s = flexString(number.String())
	return nil
}

type flexInt int

func (n *flexInt) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return nil
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return nil
		}
		*n = flexInt(parsed)
		return nil
	}
	var parsed int
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil
	}
	*n = flexInt(parsed)
	return nil
}

// ParseIncoming turns a verified webhook body into agent turns.
// Echoes, delivery receipts, and read receipts are acknowledged by the caller
// and do not appear here.
func ParseIncoming(body []byte) (object string, messages []IncomingMessage, err error) {
	var payload webhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", nil, err
	}
	for _, entry := range payload.Entry {
		for _, event := range entry.Messaging {
			msg, ok := eventMessage(entry.ID, event)
			if !ok {
				continue
			}
			messages = append(messages, msg)
		}
	}
	return payload.Object, messages, nil
}

func eventMessage(pageID string, event messagingEvent) (IncomingMessage, bool) {
	if event.Message != nil && event.Message.IsEcho {
		return IncomingMessage{}, false
	}
	if event.Message == nil && event.Postback == nil && event.MessageEdit == nil && event.Referral == nil && event.Lead == nil {
		return IncomingMessage{}, false
	}
	text := eventText(event)
	if text == "" {
		return IncomingMessage{}, false
	}
	sender := NormalizePSID(event.Sender.ID)
	page := strings.TrimSpace(pageID)
	if sender == "" || (page != "" && strings.EqualFold(sender, page)) {
		return IncomingMessage{}, false
	}
	msg := IncomingMessage{
		PSID:      sender,
		PageID:    strings.TrimSpace(pageID),
		Text:      text,
		Timestamp: event.Timestamp,
	}
	if event.Message != nil {
		msg.MessageID = strings.TrimSpace(event.Message.MID)
	}
	if msg.MessageID == "" && event.Postback != nil {
		msg.MessageID = strings.TrimSpace(event.Postback.MID)
	}
	if event.MessageEdit != nil {
		msg.MessageID = strings.TrimSpace(event.MessageEdit.MID)
		msg.EditNum = int(event.MessageEdit.NumEdit)
		if msg.EditNum < 1 {
			msg.EditNum = 1
		}
	}
	// Lead-ad summaries borrow a mid that never appears in the thread.
	// Quoting it makes Graph reject the follow-up.
	if event.HopContext != nil && syntheticLeadSummary(event.HopContext.Metadata) {
		msg.MessageID = ""
	}
	if event.Message != nil && event.Message.ReplyTo != nil {
		msg.ReplyToMID = strings.TrimSpace(event.Message.ReplyTo.MID)
	}
	if msg.PageID == "" {
		msg.PageID = NormalizePSID(event.Recipient.ID)
	}
	return msg, true
}

// TextWithReplyQuote prefixes a quote this process has already seen.
// Meta's reply_to carries only a message id. An unknown id leaves the user
// text unchanged. Slash commands are matched on the raw text before this.
func TextWithReplyQuote(userText, quote string) string {
	userText = strings.TrimSpace(userText)
	quote = strings.Join(strings.Fields(quote), " ")
	if userText == "" || quote == "" {
		return userText
	}
	return "Reply to: " + quote + "\n" + userText
}

func eventText(event messagingEvent) string {
	lines := make([]string, 0, 4)
	if event.Message != nil {
		text := strings.TrimSpace(event.Message.Text)
		if text != "" {
			lines = append(lines, text)
		}
		for _, attachment := range event.Message.Attachments {
			if line := attachmentLine(attachment); line != "" {
				lines = append(lines, line)
			}
		}
		if event.Message.QuickReply != nil {
			payload := strings.TrimSpace(string(event.Message.QuickReply.Payload))
			if payload != "" && payload != text {
				lines = append(lines, "Quick reply: "+payload)
			}
		}
		lines = append(lines, referralLines(event.Message.Referral)...)
	}
	if len(lines) == 0 && event.MessageEdit != nil {
		if text := strings.TrimSpace(event.MessageEdit.Text); text != "" {
			lines = append(lines, "Edited: "+text)
		}
	}
	if len(lines) == 0 && event.Postback != nil {
		title := strings.TrimSpace(event.Postback.Title)
		if title == "" {
			title = "untitled"
		}
		line := "Postback: " + title
		if payload := strings.TrimSpace(string(event.Postback.Payload)); payload != "" {
			line += "\nPayload: " + payload
		}
		lines = append(lines, line)
		lines = append(lines, referralLines(event.Postback.Referral)...)
	}
	// A message body already carries its own referral. The top-level referral
	// and lead belong to the standalone messaging_referrals event.
	if event.Message == nil {
		lines = append(lines, referralLines(event.Referral)...)
		lines = append(lines, leadLines(event.Lead)...)
	}
	return strings.Join(lines, "\n")
}

func syntheticLeadSummary(metadata string) bool {
	switch strings.TrimSpace(metadata) {
	case "messenger_lead_gen_complete", "messenger_lead_gen_incomplete":
		return true
	default:
		return false
	}
}

func referralLines(ref *messagingReferral) []string {
	if ref == nil {
		return nil
	}
	var lines []string
	source := strings.TrimSpace(ref.Source)
	kind := strings.TrimSpace(ref.Type)
	switch {
	case source != "" && kind != "":
		lines = append(lines, "Referral: "+source+" "+kind)
	case source != "":
		lines = append(lines, "Referral: "+source)
	case kind != "":
		lines = append(lines, "Referral: "+kind)
	}
	if refCode := strings.TrimSpace(string(ref.Ref)); refCode != "" {
		lines = append(lines, "Ref: "+refCode)
	}
	if id := strings.TrimSpace(string(ref.AdID)); id != "" {
		lines = append(lines, "Ad id: "+id)
	}
	if ref.Product != nil {
		if id := strings.TrimSpace(string(ref.Product.ID)); id != "" {
			lines = append(lines, "Product: "+id)
		}
	}
	if ads := ref.AdsContext; ads != nil {
		if title := strings.TrimSpace(ads.AdTitle); title != "" {
			lines = append(lines, "Ad: "+title)
		}
		if id := strings.TrimSpace(string(ads.ProductID)); id != "" {
			lines = append(lines, "Product: "+id)
		}
		if url := strings.TrimSpace(ads.PhotoURL); url != "" {
			lines = append(lines, "Photo: "+url)
		}
		if url := strings.TrimSpace(ads.VideoURL); url != "" {
			lines = append(lines, "Video: "+url)
		}
		if id := strings.TrimSpace(string(ads.PostID)); id != "" {
			lines = append(lines, "Post: "+id)
		}
	}
	if uri := strings.TrimSpace(ref.RefererURI); uri != "" {
		lines = append(lines, "Referrer: "+uri)
	}
	return lines
}

func leadLines(lead *messagingLead) []string {
	if lead == nil {
		return nil
	}
	lines := make([]string, 0, len(lead.Data))
	for _, row := range lead.Data {
		question := strings.TrimSpace(row.Question)
		answer := strings.TrimSpace(string(row.Answer))
		switch {
		case question != "" && answer != "":
			lines = append(lines, "Lead: "+question+" "+answer)
		case question != "":
			lines = append(lines, "Lead: "+question)
		case answer != "":
			lines = append(lines, "Lead: "+answer)
		}
	}
	return lines
}

func attachmentLine(attachment messageAttachment) string {
	kind := strings.TrimSpace(attachment.Type)
	if kind == "" {
		kind = "attachment"
	}
	payload := attachment.Payload
	if payload.Product != nil && len(payload.Product.Elements) > 0 {
		return productAttachmentLines(payload.Product.Elements)
	}
	// Meta sends the CDN URL together with the persistent id. Keeping only the
	// first one drops sticker_id, reel_video_id, or the post id.
	parts := make([]string, 0, 6)
	url := strings.TrimSpace(payload.URL)
	title := strings.TrimSpace(payload.Title)
	if title != "" && title != url {
		parts = append(parts, title)
	}
	if url != "" {
		parts = append(parts, url)
	}
	if id := strings.TrimSpace(string(payload.StickerID)); id != "" {
		parts = append(parts, id)
	}
	if id := strings.TrimSpace(string(payload.ReelVideoID)); id != "" {
		parts = append(parts, id)
	}
	if payload.Coordinates != nil {
		parts = append(parts, strconv.FormatFloat(payload.Coordinates.Lat, 'f', -1, 64)+","+strconv.FormatFloat(payload.Coordinates.Long, 'f', -1, 64))
	}
	if status := strings.TrimSpace(payload.Status); status != "" {
		parts = append(parts, status)
	}
	if payload.StartTime != 0 || payload.EndTime != 0 {
		when := strconv.FormatInt(payload.StartTime, 10)
		if payload.EndTime != 0 {
			when += "-" + strconv.FormatInt(payload.EndTime, 10)
		}
		parts = append(parts, when)
	}
	if tz := strings.TrimSpace(payload.Timezone); tz != "" {
		parts = append(parts, tz)
	}
	if id := strings.TrimSpace(payload.BookingID); id != "" {
		parts = append(parts, id)
	}
	if id := strings.TrimSpace(string(payload.ID)); id != "" {
		parts = append(parts, id)
	}
	if len(parts) == 0 {
		return "[Messenger " + kind + " attachment]"
	}
	return "[Messenger " + kind + " attachment: " + strings.Join(parts, " ") + "]"
}

func productAttachmentLines(elements []productElement) string {
	lines := make([]string, 0, len(elements))
	for _, element := range elements {
		parts := make([]string, 0, 5)
		if title := strings.TrimSpace(element.Title); title != "" {
			parts = append(parts, title)
		}
		if subtitle := strings.TrimSpace(element.Subtitle); subtitle != "" {
			parts = append(parts, subtitle)
		}
		if image := strings.TrimSpace(element.ImageURL); image != "" {
			parts = append(parts, image)
		}
		if id := strings.TrimSpace(string(element.ID)); id != "" {
			parts = append(parts, id)
		}
		if id := strings.TrimSpace(string(element.RetailerID)); id != "" {
			parts = append(parts, id)
		}
		if len(parts) == 0 {
			lines = append(lines, "[Messenger template attachment]")
			continue
		}
		lines = append(lines, "[Messenger template attachment: "+strings.Join(parts, " ")+"]")
	}
	return strings.Join(lines, "\n")
}
