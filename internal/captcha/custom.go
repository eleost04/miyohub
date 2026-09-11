package captcha

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

// test_nine-compatible API, shared by game and BBS verification. It receives
// only the challenge, never the account cookie or stoken.
func solveCustom(ctx context.Context, client *http.Client, channel model.CaptchaChannel, gt, challenge string) (Solution, error) {
	if err := ValidateChannel(channel); err != nil {
		return Solution{}, err
	}
	timeout := time.Duration(channel.Timeout) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	u, _ := url.Parse(channel.Endpoint)
	useV3 := channel.UseV3Model == nil || *channel.UseV3Model
	u.RawQuery = url.Values{"gt": {gt}, "challenge": {challenge}, "use_v3_model": {strconv.FormatBool(useV3)}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Solution{}, errors.New("自建打码请求配置无效")
	}
	req.Header.Set("Accept", "application/json")
	if channel.Token != "" {
		req.Header.Set("Authorization", "Bearer "+channel.Token)
	}
	copyClient := http.Client{}
	if client != nil {
		copyClient = *client
	}
	copyClient.Timeout = timeout
	// Never forward a challenge or bearer token to a redirect destination.
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := copyClient.Do(req)
	if err != nil {
		return Solution{}, mihoyo.SafeNetworkError(err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Solution{}, fmt.Errorf("HTTP %d，请检查服务状态和鉴权配置", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil {
		return Solution{}, mihoyo.SafeNetworkError(err)
	}
	var payload struct {
		Data struct {
			Result    string `json:"result"`
			Validate  string `json:"validate"`
			Challenge string `json:"challenge"`
		} `json:"data"`
	}
	if len(raw) > 1<<20 || json.Unmarshal(raw, &payload) != nil {
		return Solution{}, errors.New("返回格式异常，应为包含 data.result 和 data.validate 的 JSON")
	}
	validate := strings.TrimSpace(payload.Data.Validate)
	if payload.Data.Result != "success" || validate == "" {
		return Solution{}, errors.New("验证码识别未成功，请查看自建服务日志或调整渠道顺序")
	}
	if len(validate) > 4096 || strings.ContainsAny(validate, "\r\n\x00") {
		return Solution{}, errors.New("返回了无效的 validate")
	}
	// Most pass_nine deployments return only validate; keep the original
	// challenge unless the service explicitly supplies a replacement.
	if value := strings.TrimSpace(payload.Data.Challenge); value != "" {
		if len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00") {
			return Solution{}, errors.New("返回了无效的 challenge")
		}
		challenge = value
	}
	return Solution{Validate: validate, Challenge: challenge}, nil
}
