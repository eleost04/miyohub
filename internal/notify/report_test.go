package notify

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

func TestTaskReportAggregatesAndRedactsCredentials(t *testing.T) {
	account := model.Account{ID: "account", UserID: "owner", Name: "测试账号", Cookie: "cookie_token=cookie-private;other=another-private", Stoken: "stoken-private", Mid: "mid-private", CloudTokens: map[string]string{"genshin": "cloud-private"}, Device: model.Device{ID: "device-private"}}
	cfg := model.Config{Schedule: model.Schedule{Timezone: "Asia/Shanghai"}, Captcha: model.CaptchaConfig{Channels: []model.CaptchaChannel{{UserKey: "captcha-private"}}}}
	results := map[string]model.TaskSummary{
		"games": {Success: 2, Failed: 1, Details: []string{"cookie-private stoken-private another-private", "mid-private cloud-private device-private captcha-private", "打码狗余额不足，请充值后重试", "https://private.test/send?token=hidden 13812345678", "Authorization: Bearer unknown-auth-secret"}},
		"cloud": {Success: 1, Skipped: 1},
		"bbs":   {Success: 3},
	}
	event := TaskEvent(cfg, account, results, false, time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC))
	encoded, _ := json.Marshal(event)
	for _, secret := range []string{"cookie-private", "another-private", "stoken-private", "mid-private", "cloud-private", "device-private", "captcha-private", "private.test", "hidden", "13812345678", "unknown-auth-secret"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("report leaked", secret)
		}
	}
	if event.Success || event.UserID != account.UserID || event.AccountID != account.ID || !strings.Contains(event.Title, "余额不足") || !strings.Contains(event.Message, "游戏签到：成功 2 · 失败 1") || !strings.Contains(event.Message, "2026-09-10 09:00:00 +08:00") {
		t.Fatalf("invalid aggregate report: %+v", event)
	}
	cancelled := TaskEvent(cfg, account, map[string]model.TaskSummary{"games": {Success: 1}}, true, time.Now())
	if cancelled.Success || !strings.Contains(cancelled.Title, "停止") || !strings.Contains(cancelled.Message, "已执行部分") {
		t.Fatal("cancelled task reported as completed")
	}
	withReason := TaskEvent(cfg, account, results, true, time.Now(), "用户主动停止任务 cookie-private")
	if !strings.Contains(withReason.Message, "用户主动停止任务") || strings.Contains(withReason.Message, "cookie-private") {
		t.Fatal("stop reason was missing or leaked a credential")
	}
	complete := TaskEvent(cfg, account, map[string]model.TaskSummary{"games": {Success: 1}}, false, time.Now())
	if !complete.Success {
		t.Fatal("successful task reported as a failure")
	}
}

func TestExchangeReportsDoNotLeakDeliveryDataOrMislabelUnknownResults(t *testing.T) {
	account := model.Account{ID: "account", UserID: "owner", Name: "测试账号", Cookie: "private-cookie"}
	plan := model.ExchangePlan{GoodsName: "测试商品", AddressID: "private-address", UID: "123456789", Region: "private-region"}
	for _, state := range []string{"success", "failed", "unknown", "cancelled"} {
		event := ExchangeEvent(model.Config{}, account, plan, state, "收货地址 private-address private-name 13812345678 private-street", 1, time.Now())
		if event.Success != (state == "success") {
			t.Fatal("wrong success flag", state)
		}
		for _, secret := range []string{"private-cookie", "private-address", "private-region", "123456789", "private-name", "13812345678", "private-street"} {
			if strings.Contains(event.Title+event.Message, secret) {
				t.Fatal("delivery data leaked", secret)
			}
		}
		if state == "unknown" && (!strings.Contains(event.Title, "待确认") || !strings.Contains(event.Message, "勿直接重复兑换") || strings.Contains(event.Message, "兑换失败")) {
			t.Fatal("uncertain delivery mislabelled", event)
		}
	}
	unsent := ExchangeEvent(model.Config{}, account, plan, "cancelled", "", 0, time.Now())
	if !strings.Contains(unsent.Message, "未发送兑换请求") {
		t.Fatal("unsent cancellation unclear")
	}
}

func TestTaskReportShowsFinalCompletionWithoutRetryChatter(t *testing.T) {
	const reason = "今日已领取 50 米游币，剩余可领取 0；本次仅检查状态，未重复执行任务。"
	results := map[string]model.TaskSummary{
		"games": {Skipped: 3, Details: []string{
			"原神 测试角色(10001)今日已签到，奖励 原石 x20",
			"星穹铁道 测试角色(10002)今日已签到，奖励 遗失碎金 x2",
			"绝区零 测试角色(10003)今日已签到，奖励 丁尼 x3000",
			"游戏签到汇总：成功 0，失败 0，跳过 3",
		}},
		"bbs": {Skipped: 1, Status: "already_complete", Reason: reason, Details: []string{
			"米游币任务状态查询遇到网络异常，1 秒后进行第 1/2 次重试（不重复签到或兑换）",
			"米游币任务状态查询已恢复（已自动重试 1 次）",
			"今日已得 50，还可获得 0，余额 44054",
			reason,
			"米游币操作汇总：成功 0，失败 0，跳过 1",
		}},
	}
	before, _ := json.Marshal(results)
	event := TaskEvent(model.Config{}, model.Account{Name: "测试账号"}, results, false, time.Now())
	if !event.Success || event.Title != "MiyoHub · 签到检查完成" {
		t.Fatal("completed status query was mislabelled", event)
	}
	for _, want := range []string{"游戏签到：今日已签到 3", "米游币：今日已得 50", "剩余可得 0", "余额 44054", "本次仅检查"} {
		if !strings.Contains(event.Message, want) {
			t.Fatal("missing final outcome", want, event.Message)
		}
	}
	for _, unwanted := range []string{"本次操作", "查询已恢复", "秒后进行", "操作汇总", "签到汇总", "本次新增 50", "原石", reason} {
		if strings.Contains(event.Message, unwanted) {
			t.Fatal("notification included pending/redundant or inaccurate text", unwanted)
		}
	}
	after, _ := json.Marshal(results)
	if string(before) != string(after) {
		t.Fatal("notification filtering modified full diagnostic details")
	}
}

func TestTaskReportKeepsFailuresAndDoesNotTreatAllSkipsAsCompleted(t *testing.T) {
	for _, tc := range []struct {
		name      string
		result    model.TaskSummary
		cancelled bool
		want      string
	}{
		{"query-exhausted", model.TaskSummary{Failed: 1, Details: []string{"米游币任务状态查询遇到网络异常，2 秒后进行第 2/2 次重试（不重复签到或兑换）", "米游币任务状态获取失败: 网络连接或响应异常，任务状态查询重试后仍失败（这不是验证码错误）"}}, false, "查询重试后仍失败"},
		{"later-query-failed", model.TaskSummary{Failed: 1, Details: []string{"米游币任务状态查询已恢复（已自动重试 1 次）", "任务完成状态确认失败: 网络异常"}}, false, "任务完成状态确认失败"},
		{"cancelled", model.TaskSummary{Failed: 1, Details: []string{"米游币任务状态查询遇到网络异常，1 秒后进行第 1/2 次重试（不重复签到或兑换）", "米游币任务状态获取失败: context canceled"}}, true, "任务已停止"},
		{"missing-role", model.TaskSummary{Skipped: 1, Details: []string{"原神未绑定角色"}}, false, "未绑定角色"},
		{"disabled", model.TaskSummary{Skipped: 1, Details: []string{"社区签到未启用，跳过"}}, false, "未执行"},
		{"contradictory-status", model.TaskSummary{Status: "already_complete", Failed: 1, Details: []string{"状态查询失败"}}, false, "状态查询失败"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := TaskEvent(model.Config{}, model.Account{}, map[string]model.TaskSummary{"bbs": tc.result}, tc.cancelled, time.Now())
			if event.Success != (tc.result.Failed == 0 && !tc.cancelled) || !strings.Contains(event.Message, tc.want) {
				t.Fatal("final result was hidden or misreported", event)
			}
			for _, unwanted := range []string{"秒后进行", "查询已恢复", "今日无待领取奖励"} {
				if strings.Contains(event.Message, unwanted) {
					t.Fatal("misleading result", event)
				}
			}
		})
	}
	failure := "原神签到失败: 仍需验证码，请检查验证码渠道"
	event := TaskEvent(model.Config{}, model.Account{}, map[string]model.TaskSummary{"games": {Success: 6, Failed: 1, Details: []string{failure, "角色 1 签到成功", "角色 2 签到成功", "角色 3 签到成功", "角色 4 签到成功", "角色 5 签到成功", "角色 6 签到成功", "游戏签到汇总：成功 6，失败 1，跳过 0"}}}, false, time.Now())
	if event.Success || !strings.Contains(event.Message, failure) || strings.Contains(event.Message, "另有") || strings.Contains(event.Message, "角色 6 签到成功") {
		t.Fatal("later successes buried a real failure", event)
	}
	redacted := TaskEvent(model.Config{}, model.Account{Stoken: "PRIVATE_REASON_SECRET"}, map[string]model.TaskSummary{"bbs": {Skipped: 1, Status: "already_complete", Reason: "PRIVATE_REASON_SECRET"}}, false, time.Now())
	if strings.Contains(redacted.Message, "PRIVATE_REASON_SECRET") || !strings.Contains(redacted.Message, "[已隐藏]") {
		t.Fatal("structured reason bypassed redaction", redacted)
	}
}

func TestTaskReportKeepsCoinOutcomeInsteadOfRoutineDetails(t *testing.T) {
	results := map[string]model.TaskSummary{
		"games": {Success: 3, Details: []string{"原神 模拟角色(10001)签到成功，奖励 炸萝卜丸子 x3", "星穹铁道 模拟角色(10002)签到成功", "绝区零 模拟角色(10003)签到成功", "游戏签到汇总：成功 3，失败 0，跳过 0"}},
		"bbs": {Success: 4, Skipped: 3, Details: []string{
			"今日已得 0，还可获得 50，余额 2000",
			"任务接口未列出社区签到项；仍按已选社区执行签到，不将缺失任务误判为已完成",
			"大别野社区签到成功", "原神社区签到成功", "星穹铁道社区签到成功", "绝区零社区签到成功",
			"看帖：当前上游未提供该互动任务，跳过", "点赞：当前上游未提供该互动任务，跳过", "分享未启用，跳过",
			"米游币本次新增 50，今日已得 50，剩余可得 0，余额 2050", "米游币操作汇总：成功 4，失败 0，跳过 3",
		}},
	}
	before, _ := json.Marshal(results)
	event := TaskEvent(model.Config{}, model.Account{Name: "模拟账号"}, results, false, time.Now())
	for _, want := range []string{"游戏签到：成功 3", "米游币：本次新增 50 · 今日已得 50 · 剩余可得 0 · 余额 2050"} {
		if !strings.Contains(event.Message, want) {
			t.Fatal("missing concise final outcome", want, event.Message)
		}
	}
	for _, unwanted := range []string{"本次操作", "失败 0", "跳过", "另有", "社区签到成功", "米游币任务：成功", "10001", "炸萝卜丸子", "余额 2000"} {
		if strings.Contains(event.Message, unwanted) {
			t.Fatal("routine details crowded the notification", unwanted, event.Message)
		}
	}
	if !event.Success || len([]rune(event.Message)) > 180 {
		t.Fatal("normal report is not compact", event)
	}
	after, _ := json.Marshal(results)
	if string(before) != string(after) {
		t.Fatal("compaction changed full stored results")
	}
}

func TestTaskReportDoesNotInventCoinGainAfterFailedConfirmation(t *testing.T) {
	for _, result := range []model.TaskSummary{
		{Success: 2, Failed: 1, Details: []string{"今日已得 0，还可获得 50，余额 2000", "原神社区签到成功", "任务完成状态确认失败: 网络异常"}},
		{Success: 2, Failed: 1, Details: []string{"米游币本次新增 0，今日已得 0，剩余可得 50，余额 2000", "增币尚未确认：仍有可得米游币但本次未增加"}},
	} {
		event := TaskEvent(model.Config{}, model.Account{}, map[string]model.TaskSummary{"bbs": result}, false, time.Now())
		if event.Success || !strings.Contains(event.Message, "失败 1") || strings.Contains(event.Message, "本次新增 50") || !strings.Contains(event.Message, "确认") {
			t.Fatal("uncertain coin outcome was hidden or invented", event)
		}
	}
}

func TestTaskReportBoundsFailuresWithoutHidingTheFailureState(t *testing.T) {
	details := []string{}
	for i := 0; i < 20; i++ {
		details = append(details, "模拟任务执行失败: 网络异常，完整过程应保留在站内")
	}
	event := TaskEvent(model.Config{}, model.Account{}, map[string]model.TaskSummary{"bbs": {Failed: 20, Details: details}}, false, time.Now())
	if event.Success || !strings.Contains(event.Message, "失败 20") || strings.Count(event.Message, "模拟任务执行失败") > 3 || len([]rune(event.Message)) > 800 {
		t.Fatal("failure report is unbounded or mislabelled", event)
	}
}

func TestTaskReportCoinSummaryRetainsRedaction(t *testing.T) {
	account := model.Account{Stoken: "2050"}
	event := TaskEvent(model.Config{}, account, map[string]model.TaskSummary{"bbs": {Success: 1, Details: []string{"米游币本次新增 50，今日已得 50，剩余可得 0，余额 2050"}}}, false, time.Now())
	if strings.Contains(event.Message, "2050") || !strings.Contains(event.Message, "[已隐藏]") {
		t.Fatal("compact coin summary bypassed redaction", event)
	}
}
