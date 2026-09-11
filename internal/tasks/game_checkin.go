package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/eleost04/miyohub/internal/captcha"
	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

type GameCheckin struct {
	Client  *mihoyo.Client
	Config  model.Config
	Account model.Account
	Emit    func(string)
}

func (g GameCheckin) Run(ctx context.Context) model.TaskSummary {
	summary := model.TaskSummary{}
	defer func() {
		g.add(fmt.Sprintf("游戏签到汇总：成功 %d，失败 %d，跳过 %d", summary.Success, summary.Failed, summary.Skipped))
	}()
	for _, key := range g.Config.Games.Enabled {
		if ctx.Err() != nil {
			summary.Failed++
			return summary
		}
		game, ok := mihoyo.Games[key]
		if !ok {
			summary.Skipped++
			continue
		}
		roles, err := g.roles(ctx, game)
		if err != nil {
			summary.Failed++
			g.add(game.Name + "角色获取失败: " + err.Error())
			if errors.Is(err, errCredentials) {
				return summary
			}
			continue
		}
		if len(roles) == 0 {
			summary.Skipped++
			g.add(game.Name + "未绑定角色")
			continue
		}
		awards, err := g.awards(ctx, game)
		if err != nil {
			g.add(game.Name + "奖励列表暂时不可用")
		}
		for index, role := range roles {
			if index > 0 && len(g.Config.BBS.DelaySeconds) == 2 && !taskDelay(ctx, g.Config.BBS.DelaySeconds) {
				return summary
			}
			uid := text(role["game_uid"], "")
			label := game.Name + " " + text(role["nickname"], uid) + "(" + uid + ")"
			blocked := false
			for _, item := range g.Config.Games.Blacklist[key] {
				if item == uid {
					blocked = true
					break
				}
			}
			if blocked {
				summary.Skipped++
				g.add(label + "在黑名单中，跳过")
				continue
			}
			info, err := g.info(ctx, game, role)
			if err != nil {
				summary.Failed++
				g.add(label + "状态查询失败: " + err.Error())
				if errors.Is(err, errCredentials) {
					return summary
				}
				continue
			}
			if boolValue(info["first_bind"]) {
				summary.Skipped++
				g.add(label + "首次绑定，请在米游社手动签到一次")
				continue
			}
			if boolValue(info["is_sign"]) {
				summary.Skipped++
				g.add(label + "今日已签到，奖励 " + award(awards, intValue(info["total_sign_day"])-1))
				continue
			}
			result, err := g.sign(ctx, game, role, nil)
			for attempt := 0; err == nil && boolValue(dataMap(result)["success"]) && attempt < max(0, min(10, g.Config.Captcha.MaxRetries)); attempt++ {
				var solution captcha.Solution
				solution, err = g.solve(ctx, dataMap(result))
				if err != nil {
					g.add(fmt.Sprintf("%s 验证码第 %d 次处理失败: %s", label, attempt+1, err))
					if errors.Is(err, captcha.ErrUnavailable) || errors.Is(err, captcha.ErrBalanceInsufficient) {
						break
					}
					if attempt+1 < g.Config.Captcha.MaxRetries {
						err = nil
						continue
					}
					break
				}
				result, err = g.sign(ctx, game, role, &solution)
			}
			if err != nil {
				summary.Failed++
				g.add(label + "签到失败: " + err.Error())
				continue
			}
			if retcode(result) == -5003 {
				summary.Skipped++
				g.add(label + "今日已签到")
				continue
			}
			if retcode(result) == 0 && !boolValue(dataMap(result)["success"]) {
				summary.Success++
				g.add(label + "签到成功，奖励 " + award(awards, intValue(info["total_sign_day"])))
				continue
			}
			summary.Failed++
			if retcode(result) == -100 {
				g.add(label + "登录凭据失效，请重新绑定账号")
				return summary
			} else if boolValue(dataMap(result)["success"]) {
				g.add(label + "仍需验证码，请检查验证码渠道或手动签到")
			} else {
				g.add(fmt.Sprintf("%s 签到失败: %s (%d)", label, text(result["message"], "接口响应异常"), retcode(result)))
			}
		}
	}
	return summary
}

func (g GameCheckin) roles(ctx context.Context, game mihoyo.Game) ([]map[string]any, error) {
	var result map[string]any
	query := url.Values{"game_biz": {game.GameBiz}}
	if err := g.Client.JSON(ctx, http.MethodGet, mihoyo.TakumiAPI+mihoyo.AccountRolesPath, query, nil, g.headers(game, true), &result); err != nil {
		return nil, err
	}
	if retcode(result) == -100 {
		return nil, errCredentials
	}
	if retcode(result) != 0 {
		return nil, fmt.Errorf("%s(%d)", text(result["message"], "接口错误"), retcode(result))
	}
	return maps(dataMap(result)["list"]), nil
}

func (g GameCheckin) awards(ctx context.Context, game mihoyo.Game) ([]map[string]any, error) {
	var result map[string]any
	query := url.Values{"act_id": {game.ActID}}
	if err := g.Client.JSON(ctx, http.MethodGet, game.HomeURL, query, nil, g.headers(game, true), &result); err != nil {
		return nil, err
	}
	if retcode(result) != 0 {
		return nil, fmt.Errorf("奖励列表请求失败 (%d)", retcode(result))
	}
	return maps(dataMap(result)["awards"]), nil
}

func (g GameCheckin) info(ctx context.Context, game mihoyo.Game, role map[string]any) (map[string]any, error) {
	var result map[string]any
	query := url.Values{"act_id": {game.ActID}, "region": {text(role["region"], "")}, "uid": {text(role["game_uid"], "")}}
	if err := g.Client.JSON(ctx, http.MethodGet, game.InfoURL, query, nil, g.headers(game, true), &result); err != nil {
		return nil, err
	}
	if retcode(result) == -100 {
		return nil, errCredentials
	}
	if retcode(result) != 0 {
		return nil, fmt.Errorf("%s(%d)", text(result["message"], "接口错误"), retcode(result))
	}
	info := dataMap(result)
	if info["is_sign"] == nil && !boolValue(info["first_bind"]) {
		return nil, errors.New("接口没有返回签到状态")
	}
	return info, nil
}

func (g GameCheckin) sign(ctx context.Context, game mihoyo.Game, role map[string]any, solution *captcha.Solution) (map[string]any, error) {
	body := map[string]any{"act_id": game.ActID, "region": text(role["region"], ""), "uid": text(role["game_uid"], "")}
	headers := g.headers(game, true)
	if game.SignGame != "" {
		headers.Set("x-rpc-signgame", game.SignGame)
	}
	if solution != nil {
		headers.Set("x-rpc-challenge", solution.Challenge)
		headers.Set("x-rpc-validate", solution.Validate)
		headers.Set("x-rpc-seccode", solution.Validate+"|jordan")
	}
	var result map[string]any
	if err := g.Client.JSON(ctx, http.MethodPost, game.SignURL, nil, body, headers, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (g GameCheckin) solve(ctx context.Context, payload map[string]any) (captcha.Solution, error) {
	gt, challenge := text(payload["gt"], ""), text(payload["challenge"], "")
	if gt == "" || challenge == "" {
		return captcha.Solution{}, fmt.Errorf("接口未返回验证码参数")
	}
	var success *int
	if payload["success"] != nil {
		value := intValue(payload["success"])
		success = &value
	}
	return captcha.SolveConfigured(ctx, g.Client.HTTP, g.Config.Captcha, gt, challenge, success)
}

func (g GameCheckin) headers(game mihoyo.Game, web bool) http.Header {
	headers := http.Header{"Accept": {"application/json, text/plain, */*"}, "User-Agent": {mihoyo.DefaultMobileUA}, "Cookie": {g.Account.Cookie}, "x-rpc-device_id": {g.Config.Device.ID}, "x-rpc-app_version": {"2.106.2"}, "x-rpc-client_type": {"5"}, "x-rpc-channel": {"miyousheluodi"}, "Origin": {"https://act.mihoyo.com"}, "Referer": {"https://act.mihoyo.com/"}}
	headers.Set("DS", mihoyo.DS(web))
	if game.SignGame != "" {
		headers.Set("x-rpc-signgame", game.SignGame)
	}
	if g.Config.Device.FP != "" {
		headers.Set("x-rpc-device_fp", g.Config.Device.FP)
	}
	return headers
}

func (g GameCheckin) add(message string) {
	if g.Emit != nil {
		g.Emit(message)
	}
}

func award(items []map[string]any, index int) string {
	if len(items) == 0 {
		return "未知"
	}
	if index < 0 {
		index = 0
	}
	if index >= len(items) {
		index = len(items) - 1
	}
	item := items[index]
	return fmt.Sprintf("%s x%s", text(item["name"], "未知"), text(item["cnt"], "?"))
}

func retcode(value map[string]any) int {
	switch v := value["retcode"].(type) {
	case float64:
		if v == float64(int(v)) {
			return int(v)
		}
	case int:
		return v
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return -999999
}
func dataMap(value map[string]any) map[string]any {
	if data, ok := value["data"].(map[string]any); ok {
		return data
	}
	return map[string]any{}
}
func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed != 0
	case int:
		return typed != 0
	default:
		return typed == "1" || typed == "true"
	}
}
func intValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		n, _ := strconv.Atoi(typed)
		return n
	}
	return 0
}
func text(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	result := strings.TrimSpace(fmt.Sprint(value))
	if result == "" || result == "<nil>" {
		return fallback
	}
	return result
}
func maps(value any) []map[string]any {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if typed, ok := item.(map[string]any); ok {
			result = append(result, typed)
		}
	}
	return result
}
func compactJSON(value any) string { raw, _ := json.Marshal(value); return string(raw) }
