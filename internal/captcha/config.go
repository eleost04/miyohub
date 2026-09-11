package captcha

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

// Site services may use private endpoints. Personal services additionally use
// ValidatePersonalChannel and a DNS-at-dial-time public-only transport.
func ValidateChannel(channel model.CaptchaChannel) error {
	if channel.Provider != "damagou" && channel.Provider != "custom" {
		return errors.New("验证码渠道仅支持打码狗或自建 / 自定义服务")
	}
	if channel.Timeout < 1 || channel.Timeout > 120 {
		return errors.New("验证码超时应为 1–120 秒")
	}
	for _, value := range []string{channel.ID, channel.UserKey, channel.Type, channel.Endpoint, channel.Token} {
		if len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00") {
			return errors.New("验证码配置过长或包含非法字符")
		}
	}
	if channel.Provider == "damagou" {
		if channel.Enabled && strings.TrimSpace(channel.UserKey) == "" {
			return errors.New("启用打码狗前请填写 userkey")
		}
		return nil
	}
	if channel.Endpoint == "" && !channel.Enabled {
		return nil
	}
	u, err := url.Parse(channel.Endpoint)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return errors.New("自建打码地址须为完整的 http(s) 接口 URL，不能包含用户名密码、查询参数或片段；密钥请单独填写")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return errors.New("自建打码服务端口无效")
		}
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && (ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()) {
		return errors.New("自建打码地址不能是未指定、组播或链路本地地址")
	}
	return nil
}

func SolveConfigured(ctx context.Context, client *http.Client, config model.CaptchaConfig, gt, challenge string, success *int) (Solution, error) {
	if gt == "" || challenge == "" {
		return Solution{}, errors.New("接口未返回完整验证码参数")
	}
	if config.Allowed != nil && !config.Allowed() {
		return Solution{}, ErrAuthorization
	}
	copyClient := http.Client{}
	if client != nil {
		copyClient = *client
	}
	copyClient.Jar = nil
	base := copyClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	if config.PublicOnly {
		base = personalTransport
	}
	copyClient.Transport = authorizedTransport{base: base, allowed: config.Allowed}
	client = &copyClient
	failures := []error{}
	for _, channel := range config.Channels {
		if !channel.Enabled {
			continue
		}
		if channel.Timeout == 0 {
			channel.Timeout = 60 // Imported legacy configurations omitted this field.
		}
		if err := ctx.Err(); err != nil {
			return Solution{}, err
		}
		if config.Allowed != nil && !config.Allowed() {
			return Solution{}, ErrAuthorization
		}
		if config.PublicOnly {
			if err := ValidatePersonalChannel(channel); err != nil {
				failures = append(failures, err)
				continue
			}
		}
		if err := ValidateChannel(channel); err != nil {
			failures = append(failures, err)
			continue
		}
		var solution Solution
		var err error
		started := time.Now()
		if channel.Provider == "custom" {
			solution, err = solveCustom(ctx, client, channel, gt, challenge)
		} else {
			solution, err = (Solver{HTTP: client}).solve(ctx, channel.UserKey, gt, challenge, channel.Type, time.Duration(channel.Timeout)*time.Second, success)
		}
		if config.Observe != nil {
			kind := config.Purpose
			if kind == "" {
				kind = "task"
			}
			code := "ok"
			if err != nil {
				code = "failed"
				if errors.Is(err, ErrBalanceInsufficient) {
					code = "balance"
				}
				if errors.Is(err, ErrAuthorization) {
					code = "permission"
				}
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					code = "interrupted"
				}
			}
			config.Observe(model.CaptchaAttempt{At: started, ChannelID: channel.ID, Provider: channel.Provider, Kind: kind, OK: err == nil, Code: code, DurationMS: time.Since(started).Milliseconds()})
		}
		if err == nil {
			return solution, nil
		}
		name := "打码狗"
		if channel.Provider == "custom" {
			name = "自建打码服务"
		}
		failures = append(failures, fmt.Errorf("%s: %w", name, err))
	}
	if len(failures) > 0 {
		return Solution{}, errors.Join(failures...)
	}
	return Solution{}, ErrUnavailable
}
