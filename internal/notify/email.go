package notify

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

func (s Sender) email(ctx context.Context, channel model.PushChannel, title, message string) error {
	from := channel.MailFrom
	if from == "" {
		from = channel.SMTPUser
	}
	sender, err := mail.ParseAddress(from)
	if err != nil {
		return errors.New("发件人邮箱格式无效")
	}
	recipients, err := mail.ParseAddressList(channel.MailTo)
	if err != nil {
		return errors.New("收件人邮箱格式无效")
	}
	dial := s.dialContext
	if dial == nil {
		dial = safeDial
	}
	conn, err := dial(ctx, "tcp", net.JoinHostPort(channel.SMTPHost, strconv.Itoa(channel.SMTPPort)))
	if err != nil {
		if errors.Is(err, ErrPrivateNetwork) {
			return ErrPrivateNetwork
		}
		return deliveryError(ctx, "无法连接 SMTP 服务，请检查主机和端口")
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: channel.SMTPHost}
	if s.tlsConfig != nil {
		tlsConfig = s.tlsConfig.Clone()
		tlsConfig.ServerName = channel.SMTPHost
		tlsConfig.MinVersion = max(tls.VersionTLS12, tlsConfig.MinVersion)
	}
	var smtpConn net.Conn = conn
	if channel.SMTPSSL {
		secure := tls.Client(conn, tlsConfig)
		if err := secure.HandshakeContext(ctx); err != nil {
			return deliveryError(ctx, "SMTP TLS 握手失败，请核对端口和服务器证书")
		}
		smtpConn = secure
	}
	client, err := smtp.NewClient(smtpConn, channel.SMTPHost)
	if err != nil {
		return deliveryError(ctx, "SMTP 服务握手失败")
	}
	defer client.Close()
	if !channel.SMTPSSL {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP 服务未提供 STARTTLS，已拒绝明文发送")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return deliveryError(ctx, "SMTP STARTTLS 失败，请核对服务器证书")
		}
	}
	if channel.SMTPUser != "" {
		var auth smtp.Auth = smtp.PlainAuth("", channel.SMTPUser, channel.SMTPPassword, channel.SMTPHost)
		_, methods := client.Extension("AUTH")
		if !strings.Contains(strings.ToUpper(methods), "PLAIN") && strings.Contains(strings.ToUpper(methods), "LOGIN") {
			auth = &loginAuth{username: channel.SMTPUser, password: channel.SMTPPassword}
		}
		if err := client.Auth(auth); err != nil {
			return deliveryError(ctx, "SMTP 认证失败，请检查用户名和授权码")
		}
	}
	if err := client.Mail(sender.Address); err != nil {
		return deliveryError(ctx, "SMTP 拒绝发件人，请检查发件邮箱")
	}
	to := make([]string, 0, len(recipients))
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient.Address); err != nil {
			return deliveryError(ctx, "SMTP 拒绝收件人，请检查接收权限")
		}
		to = append(to, recipient.String())
	}
	w, err := client.Data()
	if err != nil {
		return deliveryError(ctx, "SMTP 未允许发送正文")
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(message))
	var body strings.Builder
	for len(encoded) > 76 {
		body.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	body.WriteString(encoded + "\r\n")
	header := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n", sender.String(), strings.Join(to, ", "), mime.QEncoding.Encode("UTF-8", strings.ReplaceAll(strings.ReplaceAll(title, "\r", " "), "\n", " ")), time.Now().Format(time.RFC1123Z))
	if _, err := w.Write([]byte(header + body.String())); err != nil {
		return deliveryError(ctx, "SMTP 正文发送中断，送达状态未确认；未自动重试")
	}
	if err := w.Close(); err != nil {
		return deliveryError(ctx, "SMTP 未确认接收，送达状态未确认；未自动重试")
	}
	// A successful DATA response means accepted; a failed QUIT must not trigger a resend.
	_ = client.Quit()
	return nil
}

type loginAuth struct {
	username, password string
	step               int
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS {
		return "", nil, errors.New("SMTP authentication requires TLS")
	}
	return "LOGIN", nil, nil
}
func (a *loginAuth) Next(_ []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	a.step++
	switch a.step {
	case 1:
		return []byte(a.username), nil
	case 2:
		return []byte(a.password), nil
	default:
		return nil, errors.New("unexpected SMTP authentication challenge")
	}
}
