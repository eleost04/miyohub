package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

type Result struct {
	ChannelID string `json:"channel_id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	Uncertain bool   `json:"uncertain,omitempty"`
}

type Deliverer interface {
	Send(context.Context, model.PushConfig, string, string, bool) []Result
}

// Sender deliberately does not retry: a timeout may occur after delivery.
// The injectable transports are for local tests, never user-facing TLS options.
type Sender struct {
	HTTP        *http.Client
	Timeout     time.Duration
	dialContext func(context.Context, string, string) (net.Conn, error)
	tlsConfig   *tls.Config
}

func (s Sender) Send(ctx context.Context, config model.PushConfig, title, message string, success bool) []Result {
	results := []Result{}
	if !config.Enabled || config.ErrorOnly && success {
		return results
	}
	for _, channel := range config.Channels {
		if !channel.Enabled {
			continue
		}
		result := Result{ChannelID: channel.ID, Name: channel.Name, Provider: channel.Provider}
		timeout := s.Timeout
		if timeout <= 0 {
			timeout = 20 * time.Second
		}
		sendCtx, cancel := context.WithTimeout(ctx, timeout)
		err := ValidateChannel(channel, true)
		if err == nil {
			err = s.sendOne(sendCtx, channel, truncate(title, 100), truncate(message, 6000))
		}
		if err != nil {
			result.Error = err.Error()
			// A provider rejection ("渠道未确认成功") is a failure, not an
			// ambiguous delivery. Only transport/response uncertainty has this marker.
			result.Uncertain = strings.Contains(result.Error, "送达状态未确认")
		} else {
			result.OK = true
		}
		cancel()
		results = append(results, result)
	}
	return results
}

func (s Sender) sendOne(ctx context.Context, c model.PushChannel, title, message string) error {
	text := title + "\n\n" + message
	var endpoint string
	var payload any
	headers := map[string]string{}
	switch c.Provider {
	case "pushplus":
		endpoint = "https://www.pushplus.plus/send"
		payload = map[string]any{"token": c.Token, "title": title, "content": truncate(message, 4000), "template": "txt", "topic": c.Topic}
	case "telegram":
		base := strings.TrimRight(c.APIURL, "/")
		if base == "" {
			base = "https://api.telegram.org"
		}
		endpoint = base + "/bot" + c.Token + "/sendMessage"
		payload = map[string]any{"chat_id": c.ChatID, "text": truncate(text, 2000), "disable_web_page_preview": true}
	case "wxpusher":
		endpoint = "https://wxpusher.zjiecode.com/api/send/message"
		payload = map[string]any{"appToken": c.Token, "content": "<h3>" + html.EscapeString(title) + "</h3><p>" + strings.ReplaceAll(html.EscapeString(truncate(message, 3000)), "\n", "<br>") + "</p>", "summary": title, "contentType": 2, "uids": []string{c.OpenID}}
	case "dingrobot":
		u, _ := url.Parse(c.Webhook) // Validated before sendOne.
		if c.Secret != "" {
			ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
			signature := hmac.New(sha256.New, []byte(c.Secret))
			_, _ = signature.Write([]byte(ts + "\n" + c.Secret))
			q := u.Query()
			q.Set("timestamp", ts)
			q.Set("sign", base64.StdEncoding.EncodeToString(signature.Sum(nil)))
			u.RawQuery = q.Encode()
		}
		endpoint = u.String()
		payload = map[string]any{"msgtype": "text", "text": map[string]string{"content": truncate(text, 1500)}}
	case "feishubot":
		endpoint = c.Webhook
		body := map[string]any{"msg_type": "text", "content": map[string]string{"text": truncate(text, 4000)}}
		if c.Secret != "" {
			ts := strconv.FormatInt(time.Now().Unix(), 10)
			signature := hmac.New(sha256.New, []byte(ts+"\n"+c.Secret))
			body["timestamp"], body["sign"] = ts, base64.StdEncoding.EncodeToString(signature.Sum(nil))
		}
		payload = body
	case "qqbot":
		// A fresh token avoids retaining app credentials or cross-channel cache state.
		raw, err := s.post(ctx, "https://bots.qq.com/app/getAppAccessToken", map[string]string{"appId": c.AppID, "clientSecret": c.ClientSecret}, nil)
		if err != nil {
			return err
		}
		var token struct {
			AccessToken string `json:"access_token"`
		}
		if json.Unmarshal(raw, &token) != nil || token.AccessToken == "" || len(token.AccessToken) > 4096 || strings.ContainsAny(token.AccessToken, "\r\n") {
			return errors.New("QQ 授权失败，请检查 AppID 与 ClientSecret")
		}
		endpoint = "https://api.sgroup.qq.com/v2/users/" + url.PathEscape(c.OpenID) + "/messages"
		headers["Authorization"] = "QQBot " + token.AccessToken
		headers["X-Union-Appid"] = c.AppID
		payload = map[string]any{"content": truncate(text, 1500), "msg_type": 0}
	case "qq":
		endpoint = strings.TrimRight(c.PushURL, "/") + "/send_msg"
		headers["Authorization"] = "Bearer " + c.AccessToken
		body := map[string]any{"message_type": c.MsgType, "message": truncate(text, 4000), "auto_escape": true}
		if c.MsgType == "group" {
			body["group_id"] = c.SendID
		} else {
			body["user_id"] = c.SendID
		}
		payload = body
	case "wechat_claw":
		if c.Mode == "ilink" || c.Webhook == "" {
			return s.sendWeixin(ctx, c, text)
		}
		fallthrough
	case "webhook":
		endpoint = c.Webhook
		if c.Token != "" {
			headers["Authorization"] = "Bearer " + c.Token
		}
		payload = map[string]any{"title": title, "content": message, "text": text, "markdown": text, "msg_type": "text"}
	case "email":
		return s.email(ctx, c, title, message)
	default:
		return errors.New("不支持的推送渠道")
	}
	raw, err := s.post(ctx, endpoint, payload, headers)
	if err != nil {
		return err
	}
	return validateResponse(c.Provider, raw)
}

func (s Sender) post(ctx context.Context, endpoint string, payload any, headers map[string]string) ([]byte, error) {
	return s.request(ctx, http.MethodPost, endpoint, payload, headers)
}

func (s Sender) request(ctx context.Context, method, endpoint string, payload any, headers map[string]string) ([]byte, error) {
	if err := ValidateEndpoint(endpoint, false); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.New("无法编码推送消息")
	}
	if payload == nil {
		raw = nil
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("推送地址无效")
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	client := *NewHTTPClient()
	if s.HTTP != nil {
		client = *s.HTTP
	}
	// Never forward webhook tokens or Authorization to a redirect destination.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, ErrPrivateNetwork) {
			return nil, ErrPrivateNetwork
		}
		return nil, &requestError{message: deliveryError(ctx, "连接推送服务失败，送达状态未确认；未自动重试，请检查网络和接收端").Error(), cause: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("推送服务返回 HTTP %d，请检查权限、地址或服务状态", resp.StatusCode)
	}
	const maxResponse = 128 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return nil, &requestError{message: deliveryError(ctx, "读取推送响应失败，送达状态未确认；未自动重试").Error(), cause: err}
	}
	if len(body) > maxResponse {
		return nil, errors.New("推送响应过大，送达状态未确认")
	}
	return body, nil
}

// Preserve error classification for the receiver without exposing URLs/tokens
// in user-visible errors or changing message delivery's no-retry semantics.
type requestError struct {
	message string
	cause   error
}

func (e *requestError) Error() string { return e.message }
func (e *requestError) Unwrap() error { return e.cause }

func deliveryError(ctx context.Context, fallback string) error {
	deadline, hasDeadline := ctx.Deadline()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || hasDeadline && !time.Now().Before(deadline) {
		return errors.New("推送超时，送达状态未确认；未自动重试")
	}
	if ctx.Err() != nil {
		return errors.New("推送已停止，送达状态未确认")
	}
	// Do not expose net/url errors: they may contain tokens and private URLs.
	return errors.New(fallback)
}

func validateResponse(provider string, raw []byte) error {
	var body map[string]json.RawMessage
	err := json.Unmarshal(raw, &body)
	custom := provider == "webhook" || provider == "wechat_claw"
	if err != nil || body == nil {
		if custom {
			return nil
		} // Custom endpoints may return plain 2xx or 204.
		return errors.New("推送响应格式不正确，送达状态未确认")
	}
	code := func(key string, expected int) bool {
		var value *int
		return json.Unmarshal(body[key], &value) == nil && value != nil && *value == expected
	}
	flag := func(key string, expected bool) bool {
		var value *bool
		return json.Unmarshal(body[key], &value) == nil && value != nil && *value == expected
	}
	ok := false
	switch provider {
	case "pushplus":
		ok = code("code", 200)
	case "telegram":
		ok = flag("ok", true)
	case "wxpusher":
		ok = code("code", 1000) && !flag("success", false)
		var entries []struct {
			Code int `json:"code"`
		}
		if json.Unmarshal(body["data"], &entries) == nil {
			for _, entry := range entries {
				if entry.Code != 1000 {
					ok = false
				}
			}
		}
	case "dingrobot":
		ok = code("errcode", 0)
	case "feishubot":
		if body["code"] != nil {
			ok = code("code", 0)
		} else {
			ok = code("StatusCode", 0)
		}
	case "qqbot":
		var id string
		ok = json.Unmarshal(body["id"], &id) == nil && id != "" && (body["code"] == nil || code("code", 0))
	case "qq":
		var status string
		ok = code("retcode", 0) && json.Unmarshal(body["status"], &status) == nil && status == "ok"
	default:
		ok = !flag("ok", false) && !flag("success", false)
		for _, key := range []string{"code", "errcode", "retcode"} {
			if body[key] != nil && !code(key, 0) && !code(key, 200) {
				ok = false
			}
		}
		var status string
		if json.Unmarshal(body["status"], &status) == nil && (status == "failed" || status == "error") {
			ok = false
		}
	}
	if ok {
		return nil
	}
	// Only numeric error codes are safe to include; upstream messages can echo secrets.
	for _, key := range []string{"code", "errcode", "error_code", "retcode", "StatusCode"} {
		var value int
		if json.Unmarshal(body[key], &value) == nil {
			return fmt.Errorf("渠道未确认成功（错误码 %d），请检查配置、额度或接收权限", value)
		}
	}
	return errors.New("渠道未确认成功，请检查配置、额度或接收权限")
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:max(0, limit-1)]) + "…"
}
