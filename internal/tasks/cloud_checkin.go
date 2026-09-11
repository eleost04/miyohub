package tasks

import (
	"context"
	"fmt"
	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
	"net/http"
	"strings"
)

type CloudCheckin struct {
	Client  *mihoyo.Client
	Config  model.Config
	Account model.Account
	Emit    func(string)
}

var cloudURLs = map[string]string{"genshin": "https://api-cloudgame.mihoyo.com/hk4e_cg_cn/wallet/wallet/get", "zzz": "https://api-cloudgame.mihoyo.com/nap_cg_cn/wallet/wallet/get"}
var cloudNames = map[string]string{"genshin": "云·原神", "zzz": "云·绝区零"}

func (c CloudCheckin) Run(ctx context.Context) model.TaskSummary {
	summary := model.TaskSummary{}
	defer func() {
		c.add(fmt.Sprintf("云游戏签到汇总：成功 %d，失败 %d，跳过 %d", summary.Success, summary.Failed, summary.Skipped))
	}()
	for _, key := range c.Config.CloudGames.Enabled {
		name := cloudNames[key]
		if name == "" {
			name = key
		}
		if ctx.Err() != nil {
			summary.Failed++
			return summary
		}
		if cloudURLs[key] == "" || strings.TrimSpace(c.Account.CloudTokens[key]) == "" {
			summary.Skipped++
			c.add(name + " 未配置云游戏凭据，跳过")
			continue
		}
		wallet, err := c.wallet(ctx, key)
		if err != nil {
			summary.Failed++
			c.add(name + " 签到失败: " + err.Error())
			continue
		}
		initial := asMap(wallet["free_time"])
		free, gained := intValue(initial["free_time"]), intValue(initial["send_freetime"])
		if gained <= 0 && free < 600 {
			if !taskDelay(ctx, c.Config.BBS.DelaySeconds) {
				return summary
			}
			confirmed, err := c.wallet(ctx, key)
			if err != nil {
				summary.Failed++
				c.add(name + " 签到后状态确认失败: " + err.Error())
				continue
			}
			wallet = confirmed
			next := asMap(confirmed["free_time"])
			gained = max(intValue(next["send_freetime"]), intValue(next["free_time"])-free)
			free = intValue(next["free_time"])
		}
		if gained > 0 {
			summary.Success++
			c.add(fmt.Sprintf("%s 签到成功，获得 %d 分钟，当前免费时长 %d 分钟", name, gained, free))
		} else {
			summary.Skipped++
			c.add(fmt.Sprintf("%s 今日已签到或时长已达上限，当前免费时长 %d 分钟", name, free))
		}
		c.add(fmt.Sprintf("%s 畅玩卡：%s，云游戏币：%d", name, text(asMap(wallet["play_card"])["short_msg"], "未开通"), intValue(asMap(wallet["coin"])["coin_num"])))
	}
	return summary
}
func (c CloudCheckin) wallet(ctx context.Context, key string) (map[string]any, error) {
	var result map[string]any
	referer := map[string]string{"genshin": "https://ys.mihoyo.com/", "zzz": "https://zzz.mihoyo.com/"}[key]
	err := c.Client.JSON(ctx, http.MethodGet, cloudURLs[key], nil, nil, http.Header{"x-rpc-combo_token": {c.Account.CloudTokens[key]}, "Referer": {referer}, "User-Agent": {"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36"}}, &result)
	if err != nil {
		return nil, err
	}
	if retcode(result) == -100 {
		return nil, fmt.Errorf("云游戏 token 失效，请更新凭据")
	}
	if retcode(result) != 0 {
		return nil, fmt.Errorf("%s (%d)", text(result["message"], "接口响应异常"), retcode(result))
	}
	wallet := dataMap(result)
	if asMap(wallet["free_time"])["free_time"] == nil {
		return nil, fmt.Errorf("接口缺少免费时长数据")
	}
	return wallet, nil
}
func (c CloudCheckin) add(message string) {
	if c.Emit != nil {
		c.Emit(message)
	}
}
func asMap(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return map[string]any{}
}
