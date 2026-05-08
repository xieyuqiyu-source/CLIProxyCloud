package services

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"

	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/config"
)

type EmailService struct {
	cfg config.EmailConfig
}

func NewEmailService(cfg config.EmailConfig) *EmailService {
	return &EmailService{cfg: cfg}
}

func (s *EmailService) Configured() bool {
	return strings.TrimSpace(s.cfg.Host) != "" && strings.TrimSpace(s.cfg.From) != ""
}

func (s *EmailService) SendLoginCode(to string, code string, deviceName string) error {
	if !s.Configured() {
		return fmt.Errorf("smtp is not configured")
	}

	subject := "CLIProxyCloud 登录验证码"
	body := fmt.Sprintf(
		"您好，\r\n\r\n您的登录验证码是：%s\r\n\r\n设备：%s\r\n有效期：10 分钟\r\n如果这不是您本人操作，请忽略此邮件。\r\n",
		code,
		deviceName,
	)

	fromHeader := s.cfg.From
	if strings.TrimSpace(s.cfg.FromName) != "" {
		fromHeader = fmt.Sprintf("%s <%s>", s.cfg.FromName, s.cfg.From)
	}

	message := strings.Join([]string{
		fmt.Sprintf("From: %s", fromHeader),
		fmt.Sprintf("To: %s", to),
		fmt.Sprintf("Subject: %s", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n")

	hostPort := fmt.Sprintf("%s:%s", s.cfg.Host, s.cfg.Port)
	auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)

	if !s.cfg.RequireTLS {
		return smtp.SendMail(hostPort, auth, s.cfg.From, []string{to}, []byte(message))
	}

	conn, err := tls.Dial("tcp", hostPort, &tls.Config{
		ServerName: s.cfg.Host,
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return err
	}
	defer client.Quit()

	if s.cfg.Username != "" || s.cfg.Password != "" {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(s.cfg.From); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write([]byte(message)); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}
