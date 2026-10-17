package mailbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-imap"
	imapclient "github.com/emersion/go-imap/client"
	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
)

// snippetFetchBytes is the TEXT prefix taken for a list row. It has to cover
// a multipart preamble plus the start of the note; 240 bytes is only headers.
const snippetFetchBytes = 2048

func (d *NetDialer) List(ctx context.Context, account Account, secret, box string, limit int) ([]MessageSummary, error) {
	return d.fetchSummaries(ctx, account, secret, box, limit, nil, false)
}

func (d *NetDialer) Search(ctx context.Context, account Account, secret, box, query string, limit int) ([]MessageSummary, error) {
	return d.fetchSummaries(ctx, account, secret, box, limit, messageSearchCriteria(query), true)
}

// messageSearchCriteria matches a subject, sender, or recipient substring, or
// the same text anywhere IMAP TEXT covers. One TEXT term misses mail whose
// only hit is the subject on servers that do not fold headers into TEXT.
func messageSearchCriteria(query string) *imap.SearchCriteria {
	term := func(header string) *imap.SearchCriteria {
		criteria := imap.NewSearchCriteria()
		if header == "" {
			criteria.Text = []string{query}
			return criteria
		}
		criteria.Header.Add(header, query)
		return criteria
	}
	parts := []*imap.SearchCriteria{
		term("Subject"),
		term("From"),
		term("To"),
		term("Cc"),
		term(""),
	}
	cur := parts[0]
	for _, next := range parts[1:] {
		parent := imap.NewSearchCriteria()
		parent.Or = [][2]*imap.SearchCriteria{{cur, next}}
		cur = parent
	}
	return cur
}

func (d *NetDialer) Read(ctx context.Context, account Account, secret, box string, uid uint32, _ int) (Body, error) {
	c, err := d.openBox(ctx, account, secret, mailboxBox(account, box))
	if err != nil {
		return Body{}, err
	}
	defer c.Logout()
	section := &imap.BodySectionName{Peek: true}
	seq := new(imap.SeqSet)
	seq.AddNum(uid)
	msgs, err := uidFetch(c, seq, []imap.FetchItem{imap.FetchEnvelope, imap.FetchFlags, imap.FetchBodyStructure, imap.FetchUid, section.FetchItem()})
	if err != nil {
		return Body{}, mapIMAP(err, "read")
	}
	if len(msgs) == 0 || msgs[0] == nil {
		return Body{}, classErr("not_found", MsgNotFound)
	}
	msg := msgs[0]
	body := Body{MessageSummary: summaryFromMessage(msg, "")}
	if literal := msg.GetBody(section); literal != nil {
		raw, readErr := io.ReadAll(io.LimitReader(literal, 2*AttachmentCapBytes))
		if readErr != nil {
			return Body{}, ErrConnect
		}
		fillParsedBody(&body, raw)
	}
	// Download indexes attachmentParts. The list returned to the model has
	// to be that same list, otherwise an inline picture takes the file's index.
	if msg.BodyStructure != nil {
		body.Attachments = metasFromStructure(msg.BodyStructure)
		body.HasAttachment = len(body.Attachments) > 0
	}
	return body, nil
}

func (d *NetDialer) Attachment(ctx context.Context, account Account, secret, box string, uid uint32, index int) (Attachment, error) {
	c, err := d.openBox(ctx, account, secret, mailboxBox(account, box))
	if err != nil {
		return Attachment{}, err
	}
	defer c.Logout()
	seq := new(imap.SeqSet)
	seq.AddNum(uid)
	msgs, err := uidFetch(c, seq, []imap.FetchItem{imap.FetchBodyStructure, imap.FetchUid})
	if err != nil {
		return Attachment{}, mapIMAP(err, "read")
	}
	if len(msgs) == 0 || msgs[0] == nil || msgs[0].BodyStructure == nil {
		return Attachment{}, classErr("not_found", MsgNotFound)
	}
	parts := attachmentParts(msgs[0].BodyStructure)
	if index < 0 || index >= len(parts) {
		return Attachment{}, classErr("not_found", MsgNotFound)
	}
	part := parts[index]
	section, err := imap.ParseBodySectionName(imap.FetchItem("BODY.PEEK[" + part.path + "]"))
	if err != nil {
		return Attachment{}, ErrConnect
	}
	fetched, err := uidFetch(c, seq, []imap.FetchItem{section.FetchItem()})
	if err != nil {
		return Attachment{}, mapIMAP(err, "read")
	}
	if len(fetched) == 0 || fetched[0] == nil {
		return Attachment{}, classErr("not_found", MsgNotFound)
	}
	literal := fetched[0].GetBody(section)
	if literal == nil {
		return Attachment{}, classErr("not_found", MsgNotFound)
	}
	// BODY[n] is still transfer-encoded. A 10MB file is about 4/3 that on
	// the wire, plus the CRLF MIME inserts every 76 base64 characters.
	limit := encodedAttachmentCap()
	raw, err := io.ReadAll(io.LimitReader(literal, int64(limit)+1))
	if err != nil {
		return Attachment{}, ErrConnect
	}
	if len(raw) > limit {
		return Attachment{}, classErr("too_large", "附件超过 10MB，没有保存")
	}
	decoded, err := partBytes(part, raw)
	if err != nil {
		return Attachment{}, err
	}
	return Attachment{Name: part.name, Bytes: decoded}, nil
}

func (d *NetDialer) Send(ctx context.Context, account Account, secret string, msg OutboundMessage) error {
	if account.Outbound == nil {
		return classErr("no_outbound", MsgNoOutbound(account.Name))
	}
	c, err := d.smtpClient(ctx, account, secret)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := mapSMTP(c.Mail(msg.From, nil), "mail"); err != nil {
		return err
	}
	for _, rcpt := range append(append(append([]string{}, msg.To...), msg.Cc...), msg.Bcc...) {
		if err := mapSMTP(c.Rcpt(rcpt, nil), "rcpt"); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return mapSMTP(err, "mail")
	}
	if _, err := w.Write(rfc822(msg)); err != nil {
		_ = w.Close()
		return ErrUncertain
	}
	if err := w.Close(); err != nil {
		return ErrUncertain
	}
	_ = c.Quit()
	return nil
}

func (d *NetDialer) fetchSummaries(ctx context.Context, account Account, secret, box string, limit int, criteria *imap.SearchCriteria, withSnippet bool) ([]MessageSummary, error) {
	if account.Inbound == nil {
		return nil, classErr("no_inbound", MsgNoInbound(account.Name))
	}
	if limit <= 0 {
		limit = ListDefaultLimit
	}
	c, err := d.openBox(ctx, account, secret, mailboxBox(account, box))
	if err != nil {
		return nil, err
	}
	defer c.Logout()
	// List rows are envelope fields only. Search is what shows a snippet, so only
	// search pays for a TEXT section.
	items, snippet := summaryFetchItems(withSnippet)
	var msgs []*imap.Message
	if criteria == nil {
		msgs, err = fetchRecent(c, limit, items)
	} else {
		msgs, err = fetchSearch(c, criteria, limit, items)
	}
	if err != nil {
		return nil, err
	}
	out := make([]MessageSummary, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		text := ""
		if snippet != nil {
			text = readSnippet(msg.GetBody(snippet), msg.BodyStructure)
		}
		out = append(out, summaryFromMessage(msg, text))
	}
	if len(out) == 0 {
		return nil, classErr("empty", MsgEmpty)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UID > out[j].UID })
	return out, nil
}

// summaryFetchItems is the IMAP FETCH set for a page of mail. List does not
// ask for TEXT: those bytes were downloaded and then dropped. Search still
// takes a short TEXT prefix so the model can tell messages apart.
func summaryFetchItems(withSnippet bool) ([]imap.FetchItem, *imap.BodySectionName) {
	items := []imap.FetchItem{imap.FetchEnvelope, imap.FetchFlags, imap.FetchBodyStructure, imap.FetchUid}
	if !withSnippet {
		return items, nil
	}
	snippet := &imap.BodySectionName{
		BodyPartName: imap.BodyPartName{Specifier: imap.TextSpecifier},
		Peek:         true,
		Partial:      []int{0, snippetFetchBytes},
	}
	return append(items, snippet.FetchItem()), snippet
}

// bracketMsgID restores the RFC 5322 form. go-message returns the id without
// angle brackets, and a reply header written that way does not thread.
func bracketMsgID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if strings.HasPrefix(id, "<") && strings.HasSuffix(id, ">") && len(id) > 2 {
		return id
	}
	return "<" + strings.Trim(id, "<>") + ">"
}

// joinMsgIDs keeps the first occurrence of each id, in order, in angle brackets.
func joinMsgIDs(ids []string) string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = bracketMsgID(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return strings.Join(out, " ")
}

// recentSequence is the inclusive sequence window of the newest limit messages.
// Listing must not SEARCH the whole mailbox just to throw away every UID but
// the last page.
func recentSequence(messages uint32, limit int) (start, end uint32, ok bool) {
	if messages == 0 || limit <= 0 {
		return 0, 0, false
	}
	end = messages
	start = 1
	if uint64(messages) > uint64(limit) {
		start = messages - uint32(limit) + 1
	}
	return start, end, true
}

func fetchRecent(c *imapclient.Client, limit int, items []imap.FetchItem) ([]*imap.Message, error) {
	count := uint32(0)
	if mbox := c.Mailbox(); mbox != nil {
		count = mbox.Messages
	}
	start, end, ok := recentSequence(count, limit)
	if !ok {
		return nil, classErr("empty", MsgEmpty)
	}
	seq := new(imap.SeqSet)
	seq.AddRange(start, end)
	msgs, err := seqFetch(c, seq, items)
	if err != nil {
		return nil, mapIMAP(err, "read")
	}
	return msgs, nil
}

func fetchSearch(c *imapclient.Client, criteria *imap.SearchCriteria, limit int, items []imap.FetchItem) ([]*imap.Message, error) {
	uids, err := c.UidSearch(criteria)
	if err != nil {
		return nil, mapIMAP(err, "read")
	}
	if len(uids) == 0 {
		return nil, classErr("empty", MsgEmpty)
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	if len(uids) > limit {
		uids = uids[len(uids)-limit:]
	}
	seq := new(imap.SeqSet)
	seq.AddNum(uids...)
	msgs, err := uidFetch(c, seq, items)
	if err != nil {
		return nil, mapIMAP(err, "read")
	}
	return msgs, nil
}

func mailboxBox(account Account, box string) string {
	if strings.TrimSpace(box) != "" {
		return strings.TrimSpace(box)
	}
	if account.Inbound != nil && strings.TrimSpace(account.Inbound.Box) != "" {
		return account.Inbound.Box
	}
	return "INBOX"
}

func (d *NetDialer) openBox(ctx context.Context, account Account, secret, box string) (*imapclient.Client, error) {
	if account.Inbound == nil {
		return nil, classErr("no_inbound", MsgNoInbound(account.Name))
	}
	c, err := d.imapClient(ctx, account, secret)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(box) == "" {
		box = account.Inbound.Box
	}
	if strings.TrimSpace(box) == "" {
		box = "INBOX"
	}
	if _, err := c.Select(box, true); err != nil {
		_ = c.Logout()
		return nil, mapIMAP(err, "select")
	}
	return c, nil
}

func uidFetch(c *imapclient.Client, seq *imap.SeqSet, items []imap.FetchItem) ([]*imap.Message, error) {
	return collectFetch(func(ch chan *imap.Message) error { return c.UidFetch(seq, items, ch) })
}

func seqFetch(c *imapclient.Client, seq *imap.SeqSet, items []imap.FetchItem) ([]*imap.Message, error) {
	return collectFetch(func(ch chan *imap.Message) error { return c.Fetch(seq, items, ch) })
}

func collectFetch(fetch func(chan *imap.Message) error) ([]*imap.Message, error) {
	ch := make(chan *imap.Message, 32)
	errCh := make(chan error, 1)
	go func() { errCh <- fetch(ch) }()
	var out []*imap.Message
	for msg := range ch {
		out = append(out, msg)
	}
	return out, <-errCh
}

func summaryFromMessage(msg *imap.Message, snippet string) MessageSummary {
	sum := MessageSummary{UID: msg.Uid, Snippet: snippet}
	for _, flag := range msg.Flags {
		if strings.EqualFold(flag, imap.SeenFlag) {
			sum.Seen = true
		}
	}
	if msg.BodyStructure != nil {
		sum.HasAttachment = len(attachmentParts(msg.BodyStructure)) > 0
	}
	if msg.Envelope == nil {
		return sum
	}
	env := msg.Envelope
	sum.Subject = decodeMIMEHeader(env.Subject)
	sum.From = formatAddressList(env.From)
	sum.To = formatAddressList(env.To)
	sum.FromAddr = firstAddress(env.From)
	sum.ToAddrs = addressList(env.To)
	sum.CcAddrs = addressList(env.Cc)
	sum.MessageID = env.MessageId
	if len(env.ReplyTo) > 0 {
		sum.ReplyTo = firstAddress(env.ReplyTo)
	}
	if !env.Date.IsZero() {
		sum.Date = env.Date.UTC().Format(time.RFC3339)
	}
	return sum
}

func formatAddressList(list []*imap.Address) string {
	parts := make([]string, 0, len(list))
	for _, addr := range list {
		if addr == nil || addr.MailboxName == "" {
			continue
		}
		mailAddr := addr.Address()
		if name := strings.TrimSpace(decodeMIMEHeader(addr.PersonalName)); name != "" {
			parts = append(parts, name+" <"+mailAddr+">")
			continue
		}
		parts = append(parts, mailAddr)
	}
	return strings.Join(parts, ", ")
}

func addressList(list []*imap.Address) []string {
	out := make([]string, 0, len(list))
	for _, addr := range list {
		if addr == nil || addr.MailboxName == "" {
			continue
		}
		out = append(out, addr.Address())
	}
	return out
}

func firstAddress(list []*imap.Address) string {
	items := addressList(list)
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

type mimePart struct {
	path     string
	name     string
	size     int64
	ctype    string
	encoding string
}

func attachmentParts(bs *imap.BodyStructure) []mimePart {
	var out []mimePart
	walkAttachment(bs, "", &out)
	return out
}

func walkAttachment(bs *imap.BodyStructure, prefix string, out *[]mimePart) {
	if bs == nil {
		return
	}
	if len(bs.Parts) > 0 {
		for i, part := range bs.Parts {
			path := strconvI(i + 1)
			if prefix != "" {
				path = prefix + "." + path
			}
			walkAttachment(part, path, out)
		}
		return
	}
	path := prefix
	if path == "" {
		path = "1"
	}
	disp := strings.ToLower(strings.TrimSpace(bs.Disposition))
	isText := strings.EqualFold(bs.MIMEType, "text") || bs.MIMEType == ""
	// Inline parts are pictures inside the HTML. Counting them shifts the
	// index of the real file away from the index download uses.
	if disp == "inline" || (disp != "attachment" && isText) {
		return
	}
	name := ""
	if bs.DispositionParams != nil {
		name = bs.DispositionParams["filename"]
	}
	if name == "" && bs.Params != nil {
		name = bs.Params["name"]
	}
	if name == "" {
		name = "attachment"
	}
	ctype := strings.ToLower(bs.MIMEType + "/" + bs.MIMESubType)
	*out = append(*out, mimePart{
		path: path, name: decodeMIMEHeader(name), size: int64(bs.Size), ctype: ctype, encoding: bs.Encoding,
	})
}

// encodedAttachmentCap is the largest BODY[n] literal that can still decode
// to AttachmentCapBytes. Base64 grows by 4/3 and MIME wraps every 76 characters.
func encodedAttachmentCap() int {
	n := AttachmentCapBytes/3*4 + 8
	return n + (n/76)*2 + 4
}

func partBytes(part mimePart, raw []byte) ([]byte, error) {
	decoded, err := decodeTransfer(part.encoding, raw)
	if err != nil {
		return nil, classErr("read", "附件内容无法解码")
	}
	if len(decoded) > AttachmentCapBytes {
		return nil, classErr("too_large", "附件超过 10MB，没有保存")
	}
	return decoded, nil
}

func decodeTransfer(encoding string, raw []byte) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "7bit", "8bit", "binary":
		return raw, nil
	case "base64":
		cleaned := make([]byte, 0, len(raw))
		for _, c := range raw {
			switch c {
			case '\r', '\n', ' ', '\t':
				continue
			default:
				cleaned = append(cleaned, c)
			}
		}
		if n := len(cleaned) % 4; n != 0 {
			cleaned = append(cleaned, bytes.Repeat([]byte{'='}, 4-n)...)
		}
		return base64.StdEncoding.DecodeString(string(cleaned))
	case "quoted-printable":
		return io.ReadAll(quotedprintable.NewReader(bytes.NewReader(raw)))
	default:
		return raw, nil
	}
}

func strconvI(n int) string { return fmt.Sprintf("%d", n) }

func metasFromStructure(bs *imap.BodyStructure) []AttachmentMeta {
	parts := attachmentParts(bs)
	out := make([]AttachmentMeta, 0, len(parts))
	for i, part := range parts {
		out = append(out, AttachmentMeta{
			Index: i, Name: part.name, Size: part.size, ContentType: part.ctype,
		})
	}
	return out
}

func fillParsedBody(body *Body, raw []byte) {
	reader, err := mail.CreateReader(bytes.NewReader(raw))
	// Unknown charset still returns a reader. Dropping it makes the model
	// see the MIME headers instead of the note.
	if (err != nil && !message.IsUnknownCharset(err)) || reader == nil {
		body.Text = string(raw)
		return
	}
	if subject, err := reader.Header.Subject(); err == nil && body.Subject == "" {
		body.Subject = subject
	}
	// MessageID() strips the angle brackets. The envelope value already has
	// them, and a missing header must not wipe that value.
	if id, err := reader.Header.MessageID(); err == nil {
		if id = bracketMsgID(id); id != "" {
			body.MessageID = id
		}
	}
	if refs, _ := reader.Header.MsgIDList("References"); len(refs) > 0 {
		body.References = joinMsgIDs(refs)
	}
	index := 0
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil && !message.IsUnknownCharset(err) {
			break
		}
		if part == nil {
			break
		}
		switch header := part.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := header.ContentType()
			buf, _ := io.ReadAll(io.LimitReader(part.Body, AttachmentCapBytes))
			if strings.HasPrefix(strings.ToLower(ct), "text/html") && body.HTML == "" {
				body.HTML = string(buf)
			} else if body.Text == "" {
				body.Text = string(buf)
			}
		case *mail.AttachmentHeader:
			name, _ := header.Filename()
			ct, _, _ := header.ContentType()
			body.Attachments = append(body.Attachments, AttachmentMeta{Index: index, Name: name, ContentType: ct})
			body.HasAttachment = true
			index++
		}
	}
	if strings.TrimSpace(body.Text) == "" && strings.TrimSpace(body.HTML) == "" {
		body.Text = string(raw)
	}
}

func decodeMIMEHeader(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || !strings.Contains(s, "=?") {
		return s
	}
	dec := mime.WordDecoder{CharsetReader: message.CharsetReader}
	out, err := dec.DecodeHeader(s)
	if err != nil || strings.TrimSpace(out) == "" {
		return s
	}
	return out
}

func readSnippet(r io.Reader, bs *imap.BodyStructure) string {
	if r == nil {
		return ""
	}
	raw, _ := io.ReadAll(io.LimitReader(r, snippetFetchBytes))
	return snippetFromMessage(bs, raw)
}

// snippetFromMessage turns the raw TEXT section into a short note. The
// structure carries the charset, transfer encoding, and multipart boundary
// that TEXT itself does not include, so a GBK or base64 body is decoded
// before it is shown.
func snippetFromMessage(bs *imap.BodyStructure, raw []byte) string {
	if decoded := decodeStructuredSnippet(bs, raw); decoded != "" {
		return clipSnippet(decoded)
	}
	return snippetText(raw)
}

func decodeStructuredSnippet(bs *imap.BodyStructure, raw []byte) string {
	header := structurePrefix(bs)
	if header == "" || len(raw) == 0 {
		return ""
	}
	wrapped := make([]byte, 0, len(header)+len(raw))
	wrapped = append(wrapped, header...)
	wrapped = append(wrapped, raw...)
	var body Body
	fillParsedBody(&body, wrapped)
	if body.Text == string(wrapped) {
		return ""
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		text = strings.TrimSpace(htmlToText(body.HTML))
	}
	return text
}

func structurePrefix(bs *imap.BodyStructure) string {
	if bs == nil {
		return ""
	}
	if strings.EqualFold(bs.MIMEType, "multipart") || len(bs.Parts) > 0 {
		boundary := strings.TrimSpace(paramFold(bs.Params, "boundary"))
		if boundary == "" {
			return ""
		}
		sub := bs.MIMESubType
		if sub == "" {
			sub = "mixed"
		}
		return "MIME-Version: 1.0\r\nContent-Type: multipart/" + sub + "; boundary=" + strconv.Quote(boundary) + "\r\n\r\n"
	}
	major := bs.MIMEType
	if major == "" {
		major = "text"
	}
	sub := bs.MIMESubType
	if sub == "" {
		sub = "plain"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\nContent-Type: %s/%s", major, sub)
	if cs := strings.TrimSpace(paramFold(bs.Params, "charset")); cs != "" {
		fmt.Fprintf(&b, "; charset=%s", strconv.Quote(cs))
	}
	b.WriteString("\r\n")
	if enc := strings.TrimSpace(bs.Encoding); enc != "" {
		fmt.Fprintf(&b, "Content-Transfer-Encoding: %s\r\n", enc)
	}
	b.WriteString("\r\n")
	return b.String()
}

func paramFold(params map[string]string, key string) string {
	for k, v := range params {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

func snippetText(raw []byte) string {
	text := strings.TrimSpace(mimeSnippet(raw))
	if strings.Contains(strings.ToLower(text), "<html") {
		text = htmlToText(text)
	}
	return clipSnippet(text)
}

func clipSnippet(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > 140 {
		return string(runes[:140])
	}
	return text
}

// mimeSnippet drops a leading MIME header block from a TEXT section. The first
// bytes of a multipart body are the boundary and part headers, not the note.
func mimeSnippet(raw []byte) string {
	s := string(raw)
	lower := strings.ToLower(s)
	if !strings.Contains(lower, "content-type:") && !strings.HasPrefix(strings.TrimSpace(s), "--") {
		return s
	}
	normalized := strings.ReplaceAll(s, "\r\n", "\n")
	chunks := strings.Split(normalized, "\n\n")
	for i, chunk := range chunks {
		if i == 0 && !strings.Contains(strings.ToLower(chunk), "content-type:") && !strings.HasPrefix(strings.TrimSpace(chunk), "--") {
			return s
		}
		if strings.Contains(strings.ToLower(chunk), "content-type:") || strings.HasPrefix(strings.TrimSpace(chunk), "--") {
			continue
		}
		body := strings.TrimSpace(chunk)
		if idx := strings.Index(body, "\n--"); idx >= 0 {
			body = strings.TrimSpace(body[:idx])
		}
		if body != "" && !strings.HasPrefix(body, "--") {
			return body
		}
	}
	return s
}

func rfc822(msg OutboundMessage) []byte {
	var b strings.Builder
	writeHeader(&b, "From", msg.From)
	writeHeader(&b, "To", strings.Join(msg.To, ", "))
	if len(msg.Cc) > 0 {
		writeHeader(&b, "Cc", strings.Join(msg.Cc, ", "))
	}
	writeHeader(&b, "Subject", subjectHeader(msg.Subject))
	writeHeader(&b, "Date", time.Now().UTC().Format(time.RFC1123Z))
	if msg.InReplyTo != "" {
		writeHeader(&b, "In-Reply-To", msg.InReplyTo)
	}
	if msg.References != "" {
		writeHeader(&b, "References", msg.References)
	}
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	writeTextBody(&b, msg.Text)
	return []byte(b.String())
}

// writeTextBody keeps the payload 7-bit. A server that did not advertise
// 8BITMIME will corrupt or refuse a raw UTF-8 body.
func writeTextBody(b *strings.Builder, text string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if sevenBit(text) {
		b.WriteString("Content-Transfer-Encoding: 7bit\r\n\r\n")
		b.WriteString(strings.ReplaceAll(text, "\n", "\r\n"))
		return
	}
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	var buf bytes.Buffer
	w := quotedprintable.NewWriter(&buf)
	_, _ = io.WriteString(w, text)
	_ = w.Close()
	b.Write(buf.Bytes())
}

func sevenBit(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if len(line) > 998 {
			return false
		}
	}
	for _, r := range text {
		if r == '\n' || r == '\t' {
			continue
		}
		if r < 32 || r > 126 {
			return false
		}
	}
	return true
}

func stripHeader(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, s)
}

// oneLine keeps a confirmation-card field on a single line. A subject or
// mailbox name that contains a newline would otherwise look like another
// recipient line.
func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\r' || r == '\n' || r == '\t':
			return ' '
		case r < 32:
			return -1
		default:
			return r
		}
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// writeHeader folds a value that would exceed the SMTP 998-octet line limit.
// Short values stay on one line. Folding happens only at spaces, which is
// where an encoded-word or an address list can be broken without changing it.
func writeHeader(b *strings.Builder, name, value string) {
	value = stripHeader(value)
	if len(name)+2+len(value) <= 900 {
		fmt.Fprintf(b, "%s: %s\r\n", name, value)
		return
	}
	words := strings.Fields(value)
	if len(words) == 0 {
		fmt.Fprintf(b, "%s:\r\n", name)
		return
	}
	line := name + ": " + words[0]
	for _, word := range words[1:] {
		if len(line)+1+len(word) > 78 {
			b.WriteString(line)
			b.WriteString("\r\n")
			line = " " + word
			continue
		}
		line += " " + word
	}
	b.WriteString(line)
	b.WriteString("\r\n")
}

func subjectHeader(subject string) string {
	subject = oneLine(subject)
	encoded := mime.QEncoding.Encode("utf-8", subject)
	if len("Subject: "+encoded) <= 900 {
		return encoded
	}
	if strings.Contains(encoded, " ") {
		return encoded
	}
	return strings.Join(qEncodeWords(subject), " ")
}

// qEncodeWords splits a value the standard encoder left as one long ASCII
// line. Whitespace between encoded-words is not part of the decoded text.
func qEncodeWords(s string) []string {
	const budget = 75 - len("=?utf-8?q?") - len("?=")
	var words []string
	var cur strings.Builder
	used := 0
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		words = append(words, "=?utf-8?q?"+cur.String()+"?=")
		cur.Reset()
		used = 0
	}
	for i := 0; i < len(s); {
		_, size := utf8.DecodeRuneInString(s[i:])
		if size <= 0 {
			break
		}
		enc := qBytes(s[i : i+size])
		if used > 0 && used+len(enc) > budget {
			flush()
		}
		cur.WriteString(enc)
		used += len(enc)
		i += size
	}
	flush()
	if len(words) == 0 {
		return []string{""}
	}
	return words
}

func qBytes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ':
			b.WriteByte('_')
		case c >= '!' && c <= '~' && c != '=' && c != '?' && c != '_':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "=%02X", c)
		}
	}
	return b.String()
}
