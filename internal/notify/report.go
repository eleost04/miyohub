package notify

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/eleost04/miyohub/internal/model"
)

const TaskEventKind = "task"
const ExchangeEventKind = "exchange"

// Event contains display text only. Never put accounts, cookies or addresses in the queue.
type Event struct {
	Kind, UserID, AccountID string
	Title, Message          string
	Success                 bool
	revision                int
}

func TaskEvent(cfg model.Config, account model.Account, results map[string]model.TaskSummary, cancelled bool, at time.Time, stopReason ...string) Event {
	passed, failed, skipped := 0, 0, 0
	var details []string
	balanceWarning := false
	for _, family := range []struct{ key, label string }{{"games", "游戏签到"}, {"cloud", "云游戏签到"}, {"bbs", "米游币任务"}} {
		r, exists := results[family.key]
		if !exists {
			continue
		}
		passed += r.Success
		failed += r.Failed
		skipped += r.Skipped
		if family.key == "bbs" && r.Status == "already_complete" && r.Success == 0 && r.Failed == 0 {
			details = append(details, "\n米游币任务：今日无待领取奖励（本次仅检查）")
		} else {
			details = append(details, fmt.Sprintf("\n%s：成功 %d · 失败 %d · 跳过 %d", family.label, r.Success, r.Failed, r.Skipped))
		}
		for _, detail := range r.Details {
			if strings.Contains(detail, "余额不足") && (strings.Contains(detail, "打码") || strings.Contains(detail, "验证码")) {
				balanceWarning = true
			}
		}
		selected, omitted := taskReportDetails(r)
		if omitted > 0 {
			details = append(details, fmt.Sprintf("（另有 %d 条明细，完整记录见站内日志）", omitted))
		}
		for _, detail := range selected {
			details = append(details, "· "+truncate(sanitize(detail, cfg, account), 220))
		}
	}
	state := "签到完成"
	if passed == 0 {
		state = "签到检查完成"
	}
	if failed > 0 {
		state = "签到有未完成项"
	}
	if cancelled {
		state = "签到已停止"
	}
	if balanceWarning {
		state = "验证码服务余额不足"
	}
	header := fmt.Sprintf("账号：%s\n时间：%s\n本次操作：成功 %d · 失败 %d · 跳过 %d", sanitize(account.Name, cfg, account), reportTime(cfg, at), passed, failed, skipped)
	if passed == 0 && failed == 0 && !cancelled {
		header += "\n本次没有新增成功操作，具体原因见下方。"
	}
	if cancelled {
		header += "\n任务已停止"
		if len(stopReason) > 0 && stopReason[0] != "" {
			header += "：" + truncate(sanitize(stopReason[0], cfg, account), 150)
		}
		header += "。以下为已执行部分的结果。"
	}
	if balanceWarning {
		header += "\n请充值验证码服务后再重试相关签到。"
	}
	return Event{Kind: TaskEventKind, UserID: account.UserID, AccountID: account.ID, Title: "MiyoHub · " + state,
		Message: header + strings.Join(details, "\n"), Success: failed == 0 && !cancelled && !balanceWarning}
}

// Completion notifications describe outcomes, not pending retry instructions.
// The original details (including backoff and per-family totals) remain in the
// account result and scoped run log. Keep errors ahead of routine success lines
// when the notification needs truncation.
func taskReportDetails(result model.TaskSummary) ([]string, int) {
	var candidates []string
	for _, detail := range result.Details {
		if strings.HasPrefix(detail, "米游币任务状态查询遇到网络异常，") ||
			strings.HasPrefix(detail, "游戏签到汇总：") ||
			strings.HasPrefix(detail, "云游戏签到汇总：") ||
			strings.HasPrefix(detail, "米游币操作汇总：") {
			continue
		}
		if result.Failed > 0 && strings.HasPrefix(detail, "米游币任务状态查询已恢复（") {
			continue
		}
		candidates = append(candidates, detail)
	}
	if result.Reason != "" {
		found := false
		for _, detail := range candidates {
			found = found || detail == result.Reason
		}
		if !found {
			candidates = append(candidates, result.Reason)
		}
	}
	if len(candidates) <= 5 {
		return candidates, 0
	}
	chosen := make([]bool, len(candidates))
	count := 0
	for _, importantOnly := range []bool{true, false} {
		for i := len(candidates) - 1; i >= 0 && count < 5; i-- {
			if chosen[i] {
				continue
			}
			important := candidates[i] == result.Reason
			for _, keyword := range []string{"失败", "未完成", "未确认", "尚未确认", "余额不足", "失效", "仍需验证码", "手动签到"} {
				important = important || strings.Contains(candidates[i], keyword)
			}
			if !importantOnly || important {
				chosen[i] = true
				count++
			}
		}
	}
	selected := make([]string, 0, count)
	for i, detail := range candidates {
		if chosen[i] {
			selected = append(selected, detail)
		}
	}
	return selected, len(candidates) - len(selected)
}

func ExchangeEvent(cfg model.Config, account model.Account, plan model.ExchangePlan, state, message string, attempt int, at time.Time) Event {
	status, detail := "兑换未完成", exchangeReason(message)
	switch state {
	case "success":
		status, detail = "兑换成功", "请在米游社查看兑换记录及发放情况。"
	case "unknown":
		status, detail = "兑换结果待确认", "请求已发出，但未确认结果。请先检查米游社兑换记录与余额，勿直接重复兑换。"
	case "missed":
		status, detail = "兑换计划已错过", "已错过兑换时间，未发送兑换请求。请检查服务运行状态并重新创建计划。"
	case "cancelled":
		status, detail = "兑换已停止", "已停止后续请求；已发送请求的结果请在米游社核对。"
		if attempt == 0 {
			detail = "已取消，未发送兑换请求。"
		}
	}
	text := fmt.Sprintf("账号：%s\n商品：%s\n时间：%s\n请求次数：%d\n\n%s", sanitize(account.Name, cfg, account), truncate(sanitize(plan.GoodsName, cfg, account), 100), reportTime(cfg, at), attempt, detail)
	return Event{Kind: ExchangeEventKind, UserID: account.UserID, AccountID: account.ID, Title: "MiyoHub · " + status, Message: text, Success: state == "success"}
}

// Do not forward arbitrary mall errors: upstream can echo a delivery address or phone number.
func exchangeReason(message string) string {
	for _, item := range []struct {
		keywords []string
		reason   string
	}{
		{[]string{"余额", "米游币不足"}, "米游币余额不足或无法确认，请在站内检查。"},
		{[]string{"价格"}, "商品价格已变化，请重新确认后创建计划。"},
		{[]string{"地址", "收货"}, "收货地址校验未通过，请在站内检查。"},
		{[]string{"角色", "区服"}, "游戏角色校验未通过，请在站内检查。"},
		{[]string{"登录", "凭据", "Cookie", "Stoken", "SToken"}, "登录凭据失效或缺失，请重新绑定账号。"},
		{[]string{"库存", "售罄"}, "商品库存不足，请查看商品状态。"},
		{[]string{"设备"}, "设备校验未通过，请在站内查看详情。"},
		{[]string{"错过", "结束", "未开始"}, "当前不在可兑换时间内，请重新检查计划。"},
	} {
		for _, keyword := range item.keywords {
			if strings.Contains(message, keyword) {
				return item.reason
			}
		}
	}
	return "本次兑换未完成，详细原因请在 MiyoHub 的兑换记录中查看。"
}

func reportTime(cfg model.Config, at time.Time) string {
	location, err := time.LoadLocation(cfg.Schedule.Timezone)
	if err != nil || cfg.Schedule.Timezone == "" {
		location, _ = time.LoadLocation(model.DefaultTimezone)
	}
	if location == nil {
		location = time.UTC
	}
	return at.In(location).Format("2006-01-02 15:04:05 -07:00")
}

var reportURL = regexp.MustCompile(`(?i)https?://[^\s<>"']+`)
var reportHeader = regexp.MustCompile(`(?im)(?:authorization|cookie)\s*[:=][^\n]+`)
var reportCredential = regexp.MustCompile(`(?i)(?:cookie|authorization|stoken|cookie_token(?:_v2)?|ltoken(?:_v2)?|token|userkey|secret|password)\s*[:=]\s*[^\s;,]+`)
var reportPhone = regexp.MustCompile(`\b(?:\+?86[- ]?)?1[3-9][0-9]{9}\b`)

func sanitize(text string, cfg model.Config, account model.Account) string {
	if len(text) > 65536 {
		return "详情过长，请在站内查看。"
	}
	values := []string{account.Cookie, account.Stoken, account.Stuid, account.Mid, account.Device.ID, account.Device.FP, account.ShopDeviceFP}
	for _, part := range strings.Split(account.Cookie, ";") {
		if _, value, ok := strings.Cut(strings.TrimSpace(part), "="); ok {
			values = append(values, value)
		}
	}
	for _, value := range account.CloudTokens {
		values = append(values, value)
	}
	for _, channel := range cfg.Captcha.Channels {
		values = append(values, channel.UserKey, channel.Token)
	}
	for _, channel := range cfg.Push.Channels {
		for _, value := range SecretFields(&channel) {
			values = append(values, *value)
		}
	}
	return Redact(text, values)
}

// Redact is shared with the authenticated log viewer and read-only CLI.
func Redact(text string, secrets []string) string {
	if len(text) > 65536 {
		return "详情过长，请在站内查看。"
	}
	values := append([]string(nil), secrets...)
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	for _, value := range values {
		if value == "" {
			continue
		}
		text = strings.ReplaceAll(text, value, "[已隐藏]")
		text = strings.ReplaceAll(text, url.QueryEscape(value), "[已隐藏]")
		if decoded, err := url.QueryUnescape(value); err == nil && decoded != "" {
			text = strings.ReplaceAll(text, decoded, "[已隐藏]")
		}
	}
	text = reportURL.ReplaceAllString(text, "[服务地址已隐藏]")
	text = reportHeader.ReplaceAllString(text, "[凭据已隐藏]")
	text = reportCredential.ReplaceAllString(text, "[凭据已隐藏]")
	text = reportPhone.ReplaceAllString(text, "[手机号已隐藏]")
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' {
			return -1
		}
		return r
	}, text)
}
