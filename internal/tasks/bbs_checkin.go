package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

var errCredentials = errors.New("登录凭据失效，请重新绑定账号")

type BBSCheckin struct {
	Client  *mihoyo.Client
	Config  model.Config
	Account model.Account
	Emit    func(string)
}

type mission struct {
	id        int
	label     string
	enabled   bool
	remaining int
	present   bool
}

func missions(state map[string]any, cfg model.BBSConfig) []mission {
	result := []mission{{58, "社区签到", cfg.Checkin, 1, false}, {59, "看帖", cfg.Read, 3, false}, {60, "点赞", cfg.Like, 5, false}, {61, "分享", cfg.Share, 1, false}}
	for _, raw := range maps(state["states"]) {
		for i := range result {
			m := &result[i]
			if intValue(raw["mission_id"]) != m.id {
				continue
			}
			m.present = true
			if boolValue(raw["is_get_award"]) {
				m.remaining = 0
			} else {
				m.remaining = max(0, m.remaining-intValue(raw["happened_times"]))
			}
		}
	}
	// New rules omit daily community sign-in from this list (some accounts
	// return only onboarding missions 62/64). Its absence is NOT completion.
	// Retired interaction missions, however, must not trigger unsolicited likes.
	if state["states"] != nil {
		for i := range result {
			found := false
			for _, raw := range maps(state["states"]) {
				if intValue(raw["mission_id"]) == result[i].id {
					found = true
					break
				}
			}
			if !found && result[i].id != 58 {
				result[i].remaining = 0
			}
		}
	}
	return result
}

func (b BBSCheckin) Run(ctx context.Context) model.TaskSummary {
	summary := model.TaskSummary{}
	defer func() {
		b.add(fmt.Sprintf("米游币操作汇总：成功 %d，失败 %d，跳过 %d", summary.Success, summary.Failed, summary.Skipped))
	}()
	state, err := b.stateWithRetry(ctx)
	if err != nil {
		b.add("米游币任务状态获取失败: " + err.Error())
		summary.Failed++
		return summary
	}
	initial := intValue(state["already_received_points"])
	b.add(fmt.Sprintf("今日已得 %d，还可获得 %d，余额 %d", initial, intValue(state["can_get_points"]), intValue(state["total_points"])))
	if state["can_get_points"] != nil && intValue(state["can_get_points"]) == 0 {
		summary.Skipped++
		summary.Status = "already_complete"
		summary.Reason = fmt.Sprintf("今日已领取 %d 米游币，剩余可领取 0；本次仅检查状态，未重复执行任务。", initial)
		b.add(summary.Reason)
		return summary
	}
	var posts []map[string]any
	if b.Config.BBS.Checkin && !missions(state, b.Config.BBS)[0].present {
		b.add("任务接口未列出社区签到项；仍按已选社区执行签到，不将缺失任务误判为已完成")
	}
	for _, m := range missions(state, b.Config.BBS) {
		if ctx.Err() != nil {
			summary.Failed++
			return summary
		}
		if !m.enabled || m.remaining == 0 {
			summary.Skipped++
			switch {
			case !m.enabled:
				b.add(m.label + "未启用，跳过")
			case !m.present:
				b.add(m.label + "：当前上游未提供该互动任务，跳过")
			default:
				b.add(m.label + "已完成，跳过")
			}
			continue
		}
		if m.id == 58 {
			if len(b.Config.BBS.Forums) == 0 {
				summary.Skipped++
				b.add("未选择任何社区，无法执行社区签到；请在任务设置中选择分区")
			}
			for _, forumID := range b.Config.BBS.Forums {
				forum, ok := mihoyo.BBSForums[forumID]
				if !ok {
					continue
				}
				_, err := b.request(ctx, http.MethodPost, mihoyo.BBSSignPath, nil, map[string]any{"gids": forum.ID}, true)
				if err != nil && (strings.Contains(err.Error(), "已签到") || strings.Contains(err.Error(), "已经签到")) {
					summary.Skipped++
					b.add(forum.Name + "社区今日已签到")
				} else {
					b.record(&summary, forum.Name+"社区签到", err)
				}
				if errors.Is(err, errCredentials) {
					return summary
				}
				if !taskDelay(ctx, b.Config.BBS.DelaySeconds) {
					return summary
				}
			}
			continue
		}
		if posts == nil {
			posts, err = b.posts(ctx)
			if err != nil {
				b.record(&summary, "获取帖子列表", err)
				break
			}
		}
		count := min(m.remaining, len(posts))
		if count == 0 {
			b.record(&summary, m.label, fmt.Errorf("没有可用帖子"))
			continue
		}
		for _, post := range posts[:count] {
			id := text(post["post_id"], "")
			title := text(post["subject"], id)
			switch m.id {
			case 59:
				_, err = b.request(ctx, http.MethodGet, "/post/api/getPostFull", url.Values{"post_id": {id}}, nil, false)
			case 60:
				body := map[string]any{"post_id": id, "is_cancel": false, "gids": text(post["gids"], "")}
				_, err = b.request(ctx, http.MethodPost, "/post/api/post/upvote", nil, body, true)
				if err == nil && b.Config.BBS.CancelLike {
					b.record(&summary, "点赞 "+title, nil)
					if !taskDelay(ctx, b.Config.BBS.DelaySeconds) {
						return summary
					}
					body["is_cancel"] = true
					_, err = b.request(ctx, http.MethodPost, "/post/api/post/upvote", nil, body, true)
					b.record(&summary, "取消点赞 "+title, err)
					if errors.Is(err, errCredentials) {
						return summary
					}
					if !taskDelay(ctx, b.Config.BBS.DelaySeconds) {
						return summary
					}
					continue
				}
			case 61:
				_, err = b.request(ctx, http.MethodGet, "/apihub/api/getShareConf", url.Values{"entity_id": {id}, "entity_type": {"1"}}, nil, false)
			}
			b.record(&summary, m.label+" "+title, err)
			if errors.Is(err, errCredentials) {
				return summary
			}
			if !taskDelay(ctx, b.Config.BBS.DelaySeconds) {
				return summary
			}
		}
	}
	final, err := b.stateWithRetry(ctx)
	if err != nil {
		b.record(&summary, "任务完成状态确认", err)
	} else {
		b.add(fmt.Sprintf("米游币本次新增 %d，今日已得 %d，剩余可得 %d，余额 %d", max(0, intValue(final["already_received_points"])-initial), intValue(final["already_received_points"]), intValue(final["can_get_points"]), intValue(final["total_points"])))
		for _, m := range missions(final, b.Config.BBS) {
			if m.enabled && m.present && m.remaining > 0 {
				summary.Failed++
				b.add(fmt.Sprintf("%s仍有 %d 项未完成", m.label, m.remaining))
			}
		}
		if intValue(final["can_get_points"]) > 0 && intValue(final["already_received_points"]) <= initial && summary.Failed == 0 {
			summary.Failed++
			b.add("增币尚未确认：仍有可得米游币但本次未增加。请在米游社核对当日规则或稍后查看，不能将操作成功视为奖励到账")
		}
	}
	return summary
}

func (b BBSCheckin) state(ctx context.Context) (map[string]any, error) {
	return b.request(ctx, http.MethodGet, "/apihub/wapi/getUserMissionsState", url.Values{"point_sn": {"myb"}}, nil, false)
}

// Retry only the read-only mission-state query. Some BBS GET endpoints mutate
// progress (e.g. sharing), so applying retries to every GET is not safe.
// InspectState intentionally keeps its single-request diagnostic contract.
func (b BBSCheckin) stateWithRetry(ctx context.Context) (map[string]any, error) {
	retries := b.Config.Network.StateRetries()
	for attempt := 0; ; attempt++ {
		state, err := b.state(ctx)
		if err == nil {
			if attempt > 0 {
				b.add(fmt.Sprintf("米游币任务状态查询已恢复（已自动重试 %d 次）", attempt))
			}
			return state, nil
		}
		if ctx.Err() != nil || !retryableStateError(err) {
			return state, err
		}
		if attempt >= retries {
			return nil, fmt.Errorf("米游币状态查询在 %d 次请求后仍失败（自动重试 %d 次；这不是验证码错误，请检查服务器网络或代理）: %w", attempt+1, attempt, err)
		}
		delay := time.Duration(1<<min(attempt, 3)) * time.Second
		var status *mihoyo.HTTPStatusError
		if errors.As(err, &status) && status.StatusCode == http.StatusTooManyRequests {
			delay = max(delay, 30*time.Second)
		}
		if errors.As(err, &status) && status.RetryAfter != "" {
			retryAfter := time.Duration(0)
			if seconds, parseErr := strconv.Atoi(status.RetryAfter); parseErr == nil && seconds > 0 {
				retryAfter = time.Duration(min(seconds, 86400)) * time.Second
			} else if at, parseErr := http.ParseTime(status.RetryAfter); parseErr == nil {
				retryAfter = time.Until(at)
			}
			if retryAfter > time.Minute {
				return nil, fmt.Errorf("上游要求暂停查询超过一分钟，本次停止自动重试，请稍后查看任务状态: %w", err)
			}
			delay = max(delay, retryAfter)
		}
		b.add(fmt.Sprintf("米游币任务状态查询遇到网络异常，%.0f 秒后进行第 %d/%d 次重试（仅查询状态，不重发任务操作）", delay.Seconds(), attempt+1, retries))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func retryableStateError(err error) bool {
	var networkError net.Error
	var status *mihoyo.HTTPStatusError
	if errors.As(err, &status) {
		switch status.StatusCode {
		case 408, 429, 500, 502, 503, 504:
			return true
		}
	}
	return errors.As(err, &networkError) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

func (b BBSCheckin) posts(ctx context.Context) ([]map[string]any, error) {
	result := make([]map[string]any, 0)
	seen := map[string]bool{}
	for _, id := range b.Config.BBS.Forums {
		forum, ok := mihoyo.BBSForums[id]
		if !ok {
			continue
		}
		raw, err := b.request(ctx, http.MethodGet, "/post/api/getForumPostList", url.Values{"forum_id": {forum.ForumID}, "is_good": {"false"}, "is_hot": {"false"}, "page_size": {"20"}, "sort_type": {"1"}}, nil, false)
		if err != nil {
			return nil, err
		}
		for _, item := range maps(raw["list"]) {
			post := asMap(item["post"])
			pid := text(post["post_id"], "")
			if pid == "" || seen[pid] {
				continue
			}
			seen[pid] = true
			post["gids"] = forum.ID
			result = append(result, post)
		}
	}
	rand.Shuffle(len(result), func(i, j int) { result[i], result[j] = result[j], result[i] })
	return result[:min(len(result), max(1, min(20, b.Config.BBS.PostLimit)))], nil
}
func (b BBSCheckin) request(ctx context.Context, method, path string, query url.Values, body any, solve bool) (map[string]any, error) {
	challenge := ""
	for attempt := 0; attempt <= max(0, min(10, b.Config.Captcha.MaxRetries)); attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		headers := b.headers(body)
		if strings.Contains(path, "wapi") || strings.Contains(path, "ShareConf") {
			headers.Set("Cookie", b.Account.Cookie)
		}
		if challenge != "" {
			headers.Set("x-rpc-challenge", challenge)
		}
		var result map[string]any
		if err := b.Client.JSON(ctx, method, mihoyo.BBSAPI+path, query, body, headers, &result); err != nil {
			return nil, err
		}
		code := retcode(result)
		if code == 0 {
			return dataMap(result), nil
		}
		if code == -100 {
			return nil, errCredentials
		}
		if code != 1034 || !solve || attempt >= b.Config.Captcha.MaxRetries {
			return nil, fmt.Errorf("%s (%d)", text(result["message"], "接口响应异常"), code)
		}
		var err error
		challenge, err = b.passCaptcha(ctx)
		if err != nil {
			return nil, err
		}
		b.add(fmt.Sprintf("验证码校验完成，重试 %d/%d", attempt+1, b.Config.Captcha.MaxRetries))
	}
	return nil, fmt.Errorf("验证码重试已耗尽")
}
func (b BBSCheckin) passCaptcha(ctx context.Context) (string, error) {
	raw, err := b.request(ctx, http.MethodGet, "/misc/api/createVerification", url.Values{"is_high": {"true"}}, nil, false)
	if err != nil {
		return "", err
	}
	solution, err := (GameCheckin{Client: b.Client, Config: b.Config}).solve(ctx, raw)
	if err != nil {
		return "", err
	}
	verified, err := b.request(ctx, http.MethodPost, "/misc/api/verifyVerification", nil, map[string]any{"geetest_challenge": solution.Challenge, "geetest_validate": solution.Validate, "geetest_seccode": solution.Validate + "|jordan"}, false)
	if err != nil {
		return "", err
	}
	challenge := text(verified["challenge"], "")
	if challenge == "" {
		return "", fmt.Errorf("验证码校验缺少 challenge")
	}
	return challenge, nil
}
func (b BBSCheckin) headers(body any) http.Header {
	cookie := b.Account.Cookie
	if b.Account.Stuid != "" && b.Account.Stoken != "" {
		cookie = "stuid=" + b.Account.Stuid + ";stoken=" + b.Account.Stoken + ";mid=" + b.Account.Mid
	}
	headers := http.Header{"Accept": {"application/json"}, "Content-Type": {"application/json; charset=UTF-8"}, "User-Agent": {"okhttp/4.9.3"}, "Cookie": {cookie}, "X-Rpc-Device_id": {b.Config.Device.ID}, "X-Rpc-App_version": {"2.106.2"}, "X-Rpc-Client_type": {"2"}, "X-Rpc-Channel": {"miyousheluodi"}, "Referer": {"https://app.mihoyo.com"}, "Ds": {mihoyo.DS(false)}}
	headers.Set("x-rpc-device_name", b.Config.Device.Name)
	headers.Set("x-rpc-device_model", b.Config.Device.Model)
	headers.Set("x-rpc-sys_version", "12")
	headers.Set("x-rpc-verify_key", "bll8iq97cem8")
	headers.Set("x-rpc-csm_source", "home")
	if body != nil {
		headers.Set("DS", mihoyo.DSX6("", compactJSON(body)))
	}
	if b.Config.Device.FP != "" {
		headers.Set("x-rpc-device_fp", b.Config.Device.FP)
	}
	return headers
}
func (b BBSCheckin) record(s *model.TaskSummary, label string, err error) {
	if err != nil {
		s.Failed++
		b.add(label + "失败: " + err.Error())
	} else {
		s.Success++
		b.add(label + "成功")
	}
}
func (b BBSCheckin) add(message string) {
	if b.Emit != nil {
		b.Emit(message)
	}
}
func taskDelay(ctx context.Context, span []int) bool {
	low, high := 1, 3
	if len(span) == 2 {
		low = max(0, min(60, span[0]))
		high = max(low, min(60, span[1]))
	}
	seconds := float64(low) + rand.Float64()*float64(high-low)
	timer := time.NewTimer(time.Duration(seconds * float64(time.Second)))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
