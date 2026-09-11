package notify

import (
	"errors"
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/eleost04/miyohub/internal/model"
)

// Endpoint and credential fields are write-only in the configuration API.
func SecretFields(c *model.PushChannel) map[string]*string {
	return map[string]*string{
		"token": &c.Token, "webhook": &c.Webhook, "secret": &c.Secret,
		"api_url": &c.APIURL, "client_secret": &c.ClientSecret,
		"push_url": &c.PushURL, "access_token": &c.AccessToken, "smtp_password": &c.SMTPPassword,
		"context_token": &c.ContextToken, "sync_cursor": &c.SyncCursor,
	}
}

var providerNames = map[string]string{
	"pushplus": "PushPlus", "telegram": "Telegram", "wxpusher": "WxPusher",
	"dingrobot": "钉钉机器人", "feishubot": "飞书机器人", "qqbot": "QQ 官方机器人",
	"qq": "QQ · OneBot", "email": "电子邮件", "wechat_claw": "微信龙虾", "webhook": "通用 Webhook",
}

func ProviderName(provider string) string {
	if name := providerNames[provider]; name != "" {
		return name
	}
	return provider
}

func ValidateEndpoint(raw string, base bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" || strings.ContainsAny(raw, "\r\n\x00") {
		return errors.New("地址须为完整的 http(s) URL，不能含用户密码或片段")
	}
	if base && (u.RawQuery != "" || u.ForceQuery) {
		return errors.New("API 基础地址不能包含查询参数")
	}
	if u.Port() != "" {
		if _, err := net.LookupPort("tcp", u.Port()); err != nil {
			return errors.New("URL 端口无效")
		}
	}
	return nil
}

var botToken = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`)
var numericID = regexp.MustCompile(`^[0-9]{1,20}$`)
var opaqueID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Disabled drafts may omit required fields, but any provided URL must be valid.
func ValidateChannel(c model.PushChannel, require bool) error {
	if providerNames[c.Provider] == "" {
		return errors.New("不支持的推送渠道")
	}
	if strings.TrimSpace(c.Name) == "" || utf8.RuneCountInString(c.Name) > 64 {
		return errors.New("渠道名称应为 1–64 个字符")
	}
	fields := []string{c.Name, c.Token, c.Webhook, c.Secret, c.ChatID, c.APIURL, c.AppID, c.ClientSecret, c.OpenID, c.Topic, c.PushURL, c.AccessToken, c.SendID, c.MsgType, c.SMTPHost, c.SMTPUser, c.SMTPPassword, c.MailFrom, c.MailTo, c.Mode, c.BotID}
	for _, field := range fields {
		if len(field) > 4096 || strings.ContainsAny(field, "\r\n\x00") {
			return errors.New("渠道字段过长或包含非法字符")
		}
	}
	for _, item := range []struct {
		value string
		base  bool
	}{{c.Webhook, false}, {c.APIURL, true}, {c.PushURL, true}} {
		if item.value != "" {
			if err := ValidateEndpoint(item.value, item.base); err != nil {
				return err
			}
		}
	}
	required := func(values ...string) error {
		if !require {
			return nil
		}
		for _, v := range values {
			if strings.TrimSpace(v) == "" {
				return errors.New("请补齐该渠道的必填配置后再启用或测试")
			}
		}
		return nil
	}
	switch c.Provider {
	case "pushplus":
		return required(c.Token)
	case "telegram":
		if c.Token != "" && !botToken.MatchString(c.Token) {
			return errors.New("Telegram Token 格式应为机器人编号:密钥")
		}
		return required(c.Token, c.ChatID)
	case "wxpusher":
		return required(c.Token, c.OpenID)
	case "dingrobot", "feishubot", "webhook":
		return required(c.Webhook)
	case "wechat_claw":
		if c.Mode != "ilink" && c.Webhook != "" {
			return required(c.Webhook)
		}
		if c.APIURL != "" && !WeixinEndpoint(c.APIURL) {
			return errors.New("微信 iLink 地址必须使用官方 HTTPS 域名")
		}
		return required(c.Token, c.OpenID, c.APIURL)
	case "qqbot":
		if c.AppID != "" && !opaqueID.MatchString(c.AppID) || c.OpenID != "" && !opaqueID.MatchString(c.OpenID) {
			return errors.New("QQ 机器人 AppID 或 OpenID 格式无效")
		}
		return required(c.AppID, c.ClientSecret, c.OpenID)
	case "qq":
		if c.SendID != "" && !numericID.MatchString(c.SendID) {
			return errors.New("QQ 接收用户或群号须为数字")
		}
		if c.MsgType != "" && c.MsgType != "private" && c.MsgType != "group" {
			return errors.New("OneBot 消息类型应为 private 或 group")
		}
		return required(c.PushURL, c.AccessToken, c.SendID, c.MsgType)
	case "email":
		if c.SMTPPort < 1 || c.SMTPPort > 65535 {
			return errors.New("SMTP 端口应在 1–65535 之间")
		}
		if c.SMTPHost != "" && (strings.ContainsAny(c.SMTPHost, "/@?# \\[]") || strings.Contains(c.SMTPHost, ":") && net.ParseIP(c.SMTPHost) == nil) {
			return errors.New("SMTP 主机只填写域名或 IP，不含协议和端口")
		}
		from := c.MailFrom
		if from == "" {
			from = c.SMTPUser
		}
		if from != "" {
			if _, err := mail.ParseAddress(from); err != nil {
				return errors.New("发件人邮箱格式无效")
			}
		}
		if c.MailTo != "" {
			to, err := mail.ParseAddressList(c.MailTo)
			if err != nil || len(to) == 0 || len(to) > 10 {
				return errors.New("收件邮箱格式无效，最多 10 个，以英文逗号分隔")
			}
		}
		if (c.SMTPUser == "") != (c.SMTPPassword == "") {
			return errors.New("SMTP 用户名与授权码需要同时填写，或同时留空使用免认证中继")
		}
		return required(c.SMTPHost, from, c.MailTo)
	}
	return errors.New("渠道配置无效")
}
