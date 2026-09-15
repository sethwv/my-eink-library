// Package mail sends outbound email (password reset, invites, the new-book
// digest) over SMTP using only the standard library. No vendored mail
// client is needed, since net/smtp plus a small amount of TLS-dialing glue
// is enough for a single outbound relay.
package mail

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Settings describes one outbound SMTP account. The zero value is disabled.
type Settings struct {
	Host        string
	Port        int
	Encryption  string // "tls" (implicit TLS, e.g. port 465), "starttls", or "none"
	Username    string
	Password    string
	FromName    string
	FromAddress string
}

// Enabled reports whether s has enough to attempt a send. Callers should
// skip sending (not error) when this is false, since SMTP is optional.
func (s Settings) Enabled() bool {
	return s.Host != "" && s.FromAddress != ""
}

// Send delivers a single plain-text email via s. Encryption == "tls" dials
// straight into TLS (the common case for port 465 "SSL/TLS" providers like
// Gmail); anything else goes through smtp.SendMail, which negotiates
// STARTTLS itself when the server offers it and falls back to plaintext
// otherwise.
func Send(s Settings, to, subject, body string) error {
	if !s.Enabled() {
		return fmt.Errorf("mail: not configured")
	}
	if err := validateHeaderValue(subject); err != nil {
		return fmt.Errorf("mail: invalid subject: %w", err)
	}
	if err := validateHeaderValue(s.FromName); err != nil {
		return fmt.Errorf("mail: invalid from name: %w", err)
	}

	toAddr, err := parseAddress(to)
	if err != nil {
		return fmt.Errorf("mail: invalid recipient address: %w", err)
	}
	fromAddr, err := parseAddress(s.FromAddress)
	if err != nil {
		return fmt.Errorf("mail: invalid sender address: %w", err)
	}

	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	msg, err := buildMessage(s, toAddr, subject, body)
	if err != nil {
		return err
	}

	var auth smtp.Auth
	if s.Username != "" {
		auth = smtp.PlainAuth("", s.Username, s.Password, s.Host)
	}

	if s.Encryption == "tls" {
		return sendImplicitTLS(addr, s.Host, auth, fromAddr, toAddr, msg)
	}
	return smtp.SendMail(addr, auth, fromAddr, []string{toAddr}, msg)
}

func buildMessage(s Settings, to, subject, body string) ([]byte, error) {
	if err := validateHeaderValue(subject); err != nil {
		return nil, fmt.Errorf("mail: invalid subject: %w", err)
	}
	if err := validateHeaderValue(s.FromName); err != nil {
		return nil, fmt.Errorf("mail: invalid from name: %w", err)
	}

	fromAddr, err := parseAddress(s.FromAddress)
	if err != nil {
		return nil, fmt.Errorf("mail: invalid sender address: %w", err)
	}
	toAddr, err := parseAddress(to)
	if err != nil {
		return nil, fmt.Errorf("mail: invalid recipient address: %w", err)
	}

	fromHeader := fromAddr
	if s.FromName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", s.FromName, fromAddr)
	}

	var b strings.Builder
	b.WriteString("From: " + fromHeader + "\r\n")
	b.WriteString("To: " + toAddr + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String()), nil
}

func validateHeaderValue(v string) error {
	if strings.ContainsAny(v, "\r\n") {
		return fmt.Errorf("contains newline characters")
	}
	return nil
}

func parseAddress(v string) (string, error) {
	if err := validateHeaderValue(v); err != nil {
		return "", err
	}
	addr, err := mail.ParseAddress(v)
	if err != nil {
		return "", err
	}
	return addr.Address, nil
}

func sendImplicitTLS(addr, host string, auth smtp.Auth, from, to string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtp handshake: %w", err)
	}
	defer c.Close()

	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
