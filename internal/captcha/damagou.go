package captcha

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
)

const DamagouURL = "https://api.damagou.top/apiv1/jiyanRecognize.html"
const BalanceInsufficient = "打码狗余额不足，请充值后重试"

var ErrBalanceInsufficient = errors.New(BalanceInsufficient)
var ErrUnavailable = errors.New("请在「打码服务」配置自己的渠道，或使用管理员授权的站点打码服务")

type Solution struct {
	Validate  string
	Challenge string
}

type Solver struct {
	HTTP *http.Client
	URL  string
}

func (s Solver) Solve(ctx context.Context, userkey, gt, challenge, captchaType string, timeout time.Duration) (Solution, error) {
	return s.solve(ctx, userkey, gt, challenge, captchaType, timeout, nil)
}

func (s Solver) solve(ctx context.Context, userkey, gt, challenge, captchaType string, timeout time.Duration, success *int) (Solution, error) {
	if strings.TrimSpace(userkey) == "" {
		return Solution{}, errors.New("未配置打码狗 userkey")
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	endpoint := s.URL
	if endpoint == "" {
		endpoint = DamagouURL
	}
	query := url.Values{"userkey": {userkey}, "gt": {gt}, "challenge": {challenge}, "isJson": {"2"}}
	if success != nil && *success == 0 {
		query.Set("success", "0")
	}
	if captchaType != "" {
		query.Set("type", captchaType)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return Solution{}, err
	}
	copyClient := *client
	// Captcha inference may take longer than the ordinary upstream client's
	// 30-second deadline. The channel timeout owns this request's budget.
	copyClient.Timeout = timeout
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := copyClient.Do(request)
	if err != nil {
		return Solution{}, mihoyo.SafeNetworkError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Solution{}, fmt.Errorf("打码狗 HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return Solution{}, mihoyo.SafeNetworkError(err)
	}
	var payload map[string]any
	if len(raw) > 1<<20 || json.Unmarshal(raw, &payload) != nil || payload == nil {
		return Solution{}, errors.New("打码狗返回格式异常")
	}
	if fmt.Sprint(payload["status"]) != "0" && strings.Contains(fmt.Sprint(payload), "余额") {
		return Solution{}, ErrBalanceInsufficient
	}
	if fmt.Sprint(payload["status"]) != "0" {
		return Solution{}, errors.New("验证码服务识别失败，请检查渠道配置")
	}
	parts := strings.SplitN(fmt.Sprint(payload["data"]), "|", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return Solution{}, errors.New("打码狗返回格式异常")
	}
	return Solution{Challenge: strings.TrimSpace(parts[0]), Validate: strings.TrimSpace(parts[1])}, nil
}
