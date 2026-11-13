package main

import (
	"log"
	"net/smtp"
	"os"
)

// Mailer sends one plain-text email. The recipient is validated before it reaches a mailer.
type Mailer interface {
	Send(to, subject, body string) error
}

// smtpMailer sends through any SMTP server (for example a transactional email provider).
type smtpMailer struct {
	host, port, user, pass, from string
}

func (m smtpMailer) Send(to, subject, body string) error {
	msg := []byte("From: " + m.from + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
		body + "\r\n")
	var auth smtp.Auth
	if m.user != "" {
		auth = smtp.PlainAuth("", m.user, m.pass, m.host)
	}
	return smtp.SendMail(m.host+":"+m.port, auth, m.from, []string{to}, msg)
}

// logMailer prints messages to the server log. It is for local development only, because the
// log then holds live sign-in codes.
type logMailer struct{}

func (logMailer) Send(to, subject, body string) error {
	log.Printf("DEV MAIL to=%s subject=%q body=%q", to, subject, body)
	return nil
}

// mailerFromEnv picks a mailer from the environment:
//   - ARENA_SMTP_HOST, ARENA_SMTP_PORT, ARENA_SMTP_USER, ARENA_SMTP_PASS, ARENA_SMTP_FROM: SMTP
//   - ARENA_DEV_MAIL=1: print codes to the log (development only)
//   - neither set: email sign-in is disabled and the codes are not sent anywhere
func mailerFromEnv() Mailer {
	if host := os.Getenv("ARENA_SMTP_HOST"); host != "" {
		port := os.Getenv("ARENA_SMTP_PORT")
		if port == "" {
			port = "587"
		}
		return smtpMailer{
			host: host,
			port: port,
			user: os.Getenv("ARENA_SMTP_USER"),
			pass: os.Getenv("ARENA_SMTP_PASS"),
			from: os.Getenv("ARENA_SMTP_FROM"),
		}
	}
	if os.Getenv("ARENA_DEV_MAIL") == "1" {
		return logMailer{}
	}
	return nil
}
