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
	passed, failed := 0, 0
	var details, issues []string
	omittedIssues := false
	balanceWarning := false
	for _, family := range []struct{ key, label string }{{"games", "游戏签到"}, {"cloud", "云游戏签到"}, {"bbs", "米游币"}} {
		r, exists := results[family.key]
		if !exists {
			continue
		}
		passed += r.Success
		failed += r.Failed
		outcome := taskReportCounts(r)
		if family.key == "bbs" {
			if points := taskReportCoins(r); points != "" {
				outcome = points
			}
		}
		details = append(details, family.label+"："+truncate(sanitize(outcome, cfg, account), 220))
		for _, detail := range r.Details {
			if strings.Contains(detail, "余额不足") && (strings.Contains(detail, "打码") || strings.Contains(detail, "验证码")) {
				balanceWarning = true
			}
		}
		selected, omitted := taskReportDetails(r)
		omittedIssues = omittedIssues || omitted > 0
		for _, detail := range selected {
			if len(issues) < 3 {
				issues = append(issues, "· "+truncate(sanitize(detail, cfg, account), 180))
			} else {
				omittedIssues = true
			}
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
	header := fmt.Sprintf("账号：%s\n时间：%s", sanitize(account.Name, cfg, account), reportTime(cfg, at))
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
	details = append(details, issues...)
	if omittedIssues {
		details = append(details, "其余异常请在站内查看。")
	}
	return Event{Kind: TaskEventKind, UserID: account.UserID, AccountID: account.ID, Title: "MiyoHub · " + state,
		Message: header + "\n\n" + strings.Join(details, "\n"), Success: failed == 0 && !cancelled && !balanceWarning}
}

func taskReportCounts(result model.TaskSummary) string {
	if result.Success == 0 && result.Failed == 0 {
		already := 0
		for _, detail := range result.Details {
			if strings.Contains(detail, "今日已签到") {
				already++
			}
		}
		if already > 0 && already == result.Skipped {
			return fmt.Sprintf("今日已签到 %d", already)
		}
		if result.Skipped > 0 {
			return fmt.Sprintf("未执行（跳过 %d 项）", result.Skipped)
		}
		return "未执行"
	}
	parts := []string{}
	for _, item := range []struct {
		label string
		count int
	}{{"成功", result.Success}, {"失败", result.Failed}, {"跳过", result.Skipped}} {
		if item.count > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", item.label, item.count))
		}
	}
	return strings.Join(parts, " · ")
}

var reportCoinOutcome = regexp.MustCompile(`^米游币本次新增 (\d+)，今日已得 (\d+)，剩余可得 (\d+)，余额 (-?\d+)$`)
var reportCoinSnapshot = regexp.MustCompile(`^今日已得 (\d+)，还可获得 (\d+)，余额 (-?\d+)$`)

// Use the final recorded balance, never infer rewards from successful request
// counts. An initial snapshot is usable only for an already-complete check.
func taskReportCoins(result model.TaskSummary) string {
	for i := len(result.Details) - 1; i >= 0; i-- {
		if match := reportCoinOutcome.FindStringSubmatch(result.Details[i]); match != nil {
			line := fmt.Sprintf("本次新增 %s · 今日已得 %s · 剩余可得 %s · 余额 %s", match[1], match[2], match[3], match[4])
			if result.Failed > 0 {
				line += fmt.Sprintf(" · 失败 %d", result.Failed)
			}
			return line
		}
	}
	if result.Status == "already_complete" && result.Success == 0 && result.Failed == 0 {
		for i := len(result.Details) - 1; i >= 0; i-- {
			if match := reportCoinSnapshot.FindStringSubmatch(result.Details[i]); match != nil {
				return fmt.Sprintf("今日已得 %s · 剩余可得 %s · 余额 %s（本次仅检查）", match[1], match[2], match[3])
			}
		}
		return "今日无待领取奖励（本次仅检查）"
	}
	return ""
}

// Keep troubleshooting reasons, not the full chronological log. Nothing here
// mutates the stored task result or per-user execution log.
func taskReportDetails(result model.TaskSummary) ([]string, int) {
	var candidates, fallback []string
	seen := map[string]bool{}
	add := func(detail string) {
		if detail != "" && !seen[detail] {
			seen[detail] = true
			candidates = append(candidates, detail)
		}
	}
	for _, detail := range result.Details {
		if strings.HasPrefix(detail, "米游币任务状态查询遇到网络异常，") ||
			strings.HasPrefix(detail, "米游币任务状态查询已恢复（") ||
			strings.HasPrefix(detail, "米游币任务列表：") ||
			strings.HasPrefix(detail, "米游币任务设置：") ||
			strings.HasPrefix(detail, "米游币执行方式：") ||
			strings.HasPrefix(detail, "游戏签到汇总：") ||
			strings.HasPrefix(detail, "云游戏签到汇总：") ||
			strings.HasPrefix(detail, "米游币操作汇总：") ||
			reportCoinOutcome.MatchString(detail) || reportCoinSnapshot.MatchString(detail) {
			continue
		}
		important := false
		for _, keyword := range []string{"失败", "未完成", "未确认", "尚未确认", "余额不足", "失效", "仍需验证码", "手动签到", "未绑定角色", "未配置", "无法", "已停止"} {
			important = important || strings.Contains(detail, keyword)
		}
		if important {
			add(detail)
		} else {
			fallback = append(fallback, detail)
		}
	}
	if !(result.Status == "already_complete" && strings.HasPrefix(result.Reason, "今日已领取 ") && strings.Contains(result.Reason, "本次仅检查状态")) {
		add(result.Reason)
	}
	if len(candidates) == 0 && result.Failed > 0 && len(fallback) > 0 {
		add(fallback[len(fallback)-1])
	}
	if len(candidates) > 3 {
		return candidates[:3], len(candidates) - 3
	}
	return candidates, 0
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
	values = append(values, cfg.Network.Proxy.URL, cfg.Network.Proxy.Username, cfg.Network.Proxy.Password)
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
