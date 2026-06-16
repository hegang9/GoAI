// Package email 是邮件适配层：基于 SMTP 实现 domain/user.Mailer 端口。
package email

import (
	domainuser "GopherAI/internal/domain/user"
	"GopherAI/pkg/logger"

	"gopkg.in/gomail.v2"
)

// 邮件正文前缀常量，供应用层在发送验证码/账号编号时复用。
const (
	CaptchaPrefix   = "GopherAI验证码如下(验证码仅限于2分钟有效): "
	AccountNoPrefix = "GopherAI的内部账号编号如下，仅用于账号识别和问题排查，请妥善保管 "
)

// smtpHost / smtpPort 为 QQ 邮箱 SMTP 配置（587 为 STARTTLS 端口）。
const (
	smtpHost = "smtp.qq.com"
	smtpPort = 587
)

// Mailer 基于 QQ 邮箱 SMTP 实现 domain/user.Mailer 端口。
type Mailer struct {
	from     string
	authCode string
}

// NewMailer 创建邮件发送器。from 为发件邮箱，authCode 为 SMTP 授权码。
func NewMailer(from, authCode string) *Mailer {
	return &Mailer{from: from, authCode: authCode}
}

// 编译期断言：Mailer 必须满足领域端口。
var _ domainuser.Mailer = (*Mailer)(nil)

// Send 向指定邮箱发送一段以 prefix 为前缀、附带 content 的邮件。
func (m *Mailer) Send(email, content, prefix string) error {
	msg := gomail.NewMessage()
	msg.SetHeader("From", m.from)
	msg.SetHeader("To", email)
	msg.SetHeader("Subject", "来自GopherAI的信息")
	msg.SetBody("text/plain", prefix+" "+content)

	d := gomail.NewDialer(smtpHost, smtpPort, m.from, m.authCode)
	if err := d.DialAndSend(msg); err != nil {
		logger.Error("send mail failed", "to", email, "err", err)
		return err
	}
	logger.Info("send mail success", "to", email)
	return nil
}
