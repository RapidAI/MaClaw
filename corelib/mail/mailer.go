// Package mail provides a shared SMTP mail sender for hub and hubcenter.
package mail

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// DefaultSendTimeout is the default context timeout applied when the caller
// does not set a deadline.
const DefaultSendTimeout = 12 * time.Second

// Config holds SMTP connection parameters. It is intentionally free of any
// hub- or hubcenter-specific config dependency so both can use it.
type Config struct {
	SMTPHost   string
	SMTPPort   int
	Encryption string // "ssl", "starttls", "plain", "auto"
	Username   string
	Password   string
	FromName   string
	FromEmail  string
}

// Send delivers an email via SMTP. It adds RFC 5322 compliant Message-ID and
// Date headers that strict mail servers (e.g. Gmail) require.
func Send(ctx context.Context, cfg Config, to []string, subject, body string) error {
	if len(to) == 0 {
		return fmt.Errorf("mail recipient is required")
	}
	cfg = normalizeConfig(cfg)
	if strings.TrimSpace(cfg.SMTPHost) == "" {
		return fmt.Errorf("mail delivery is not configured")
	}
	if cfg.SMTPPort <= 0 {
		return fmt.Errorf("smtp port is required")
	}

	fromEmail := cfg.FromEmail
	if fromEmail == "" {
		fromEmail = cfg.Username
	}
	if fromEmail == "" {
		return fmt.Errorf("mail sender is not configured")
	}

	message := buildMessage(cfg.FromName, fromEmail, to, subject, body)
	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)
	if useImplicitTLS(cfg) {
		return sendWithImplicitTLS(ctx, addr, cfg, fromEmail, to, message)
	}
	return sendWithSMTP(ctx, addr, cfg, fromEmail, to, message)
}

// buildMessage constructs an RFC 5322 compliant email with Message-ID and Date.
func buildMessage(fromName, fromEmail string, to []string, subject, body string) []byte {
	domain := "localhost"
	if parts := strings.SplitN(fromEmail, "@", 2); len(parts) == 2 && parts[1] != "" {
		domain = parts[1]
	}
	msgID := generateMessageID(domain)

	headers := []string{
		"From: " + formatFrom(fromName, fromEmail),
		"To: " + strings.Join(to, ", "),
		"Subject: " + subject,
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"Message-ID: " + msgID,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
	}
	return []byte(strings.Join(headers, "\r\n") + "\r\n" + body)
}

func generateMessageID(domain string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("<%x.%x@%s>", b[:8], b[8:], domain)
}

func sendWithSMTP(ctx context.Context, addr string, cfg Config, fromEmail string, to []string, message []byte) error {
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return wrapMailError(err)
	}
	return sendWithConn(ctx, conn, cfg, fromEmail, to, message, false)
}

func sendWithImplicitTLS(ctx context.Context, addr string, cfg Config, fromEmail string, to []string, message []byte) error {
	dialer := &net.Dialer{}
	rawConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return wrapMailError(err)
	}
	tlsConn := tls.Client(rawConn, &tls.Config{
		ServerName:         cfg.SMTPHost,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: false,
	})
	if deadline, ok := ctx.Deadline(); ok {
		_ = tlsConn.SetDeadline(deadline)
	}
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = tlsConn.Close()
		return wrapMailError(err)
	}
	return sendWithConn(ctx, tlsConn, cfg, fromEmail, to, message, true)
}

func sendWithConn(ctx context.Context, conn net.Conn, cfg Config, fromEmail string, to []string, message []byte, implicitTLS bool) error {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, cfg.SMTPHost)
	if err != nil {
		return wrapMailError(err)
	}
	defer client.Close()

	if !implicitTLS && shouldStartTLS(cfg) {
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			if cfg.Encryption == "starttls" {
				return fmt.Errorf("smtp server does not support STARTTLS")
			}
		} else if err := client.StartTLS(&tls.Config{
			ServerName:         cfg.SMTPHost,
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: false,
		}); err != nil {
			return wrapMailError(err)
		}
	}

	if cfg.Username != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.SMTPHost)); err != nil {
				return wrapMailError(err)
			}
		}
	}
	if err := client.Mail(fromEmail); err != nil {
		return wrapMailError(err)
	}
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return wrapMailError(err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return wrapMailError(err)
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return wrapMailError(err)
	}
	if err := writer.Close(); err != nil {
		return wrapMailError(err)
	}
	return wrapMailError(client.Quit())
}

func useImplicitTLS(cfg Config) bool {
	return cfg.Encryption == "ssl" || (cfg.Encryption == "auto" && cfg.SMTPPort == 465)
}

func shouldStartTLS(cfg Config) bool {
	if cfg.Encryption == "plain" {
		return false
	}
	return cfg.Encryption == "starttls" || (cfg.Encryption == "auto" && cfg.SMTPPort != 465)
}

// NormalizeEncryption normalises an encryption string to one of the known values.
func NormalizeEncryption(v string) string {
	return normalizeEncryption(v)
}

func normalizeConfig(cfg Config) Config {
	cfg.SMTPHost = strings.TrimSpace(cfg.SMTPHost)
	cfg.SMTPPort = defaultPort(cfg.SMTPPort)
	cfg.Encryption = normalizeEncryption(cfg.Encryption)
	cfg.Username = strings.TrimSpace(cfg.Username)
	cfg.FromName = strings.TrimSpace(cfg.FromName)
	cfg.FromEmail = strings.TrimSpace(cfg.FromEmail)
	return cfg
}

func normalizeEncryption(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "ssl":
		return "ssl"
	case "starttls":
		return "starttls"
	case "plain":
		return "plain"
	default:
		return "auto"
	}
}

func defaultPort(port int) int {
	if port > 0 {
		return port
	}
	return 587
}

func formatFrom(name, email string) string {
	if strings.TrimSpace(name) == "" {
		return email
	}
	return fmt.Sprintf("%s <%s>", name, email)
}

func wrapMailError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("mail delivery timed out: %w", err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("mail delivery timed out: %w", err)
	}
	return err
}
