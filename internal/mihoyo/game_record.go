package mihoyo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

const recordAPI = "https://api-takumi-record.mihoyo.com"

// RecordError contains only allowlisted messages, never raw upstream JSON,
// challenge URLs, role identifiers, cookies or network proxy credentials.
type RecordError struct {
	Kind     string
	Message  string
	Cooldown time.Duration
}

func (e *RecordError) Error() string { return e.Message }
func recordFailure(kind, message string, cooldown time.Duration) error {
	return &RecordError{Kind: kind, Message: message, Cooldown: cooldown}
}
func RecordGame(game string) bool { return game == "genshin" || game == "starrail" || game == "zzz" }

var recordUID = regexp.MustCompile(`^[0-9]{1,20}$`)
var recordRegion = regexp.MustCompile(`^[a-zA-Z0-9_]{1,40}$`)

func RecordRoleParameters(uid, region string) bool {
	return recordUID.MatchString(uid) && recordRegion.MatchString(region)
}

func (c *Client) recordRequest(ctx context.Context, a model.Account, game, method, path string, query url.Values, body any) (recordObject, error) {
	if !RecordGame(game) {
		return nil, recordFailure("unsupported", "此游戏暂未接入便笺", 0)
	}
	if a.Cookie == "" {
		return nil, recordFailure("credentials", "请先重新绑定米游社账号", 15*time.Minute)
	}
	headers := http.Header{"Cookie": {a.Cookie}, "X-Rpc-App_version": {"2.106.2"}, "X-Rpc-Client_type": {"5"}, "X-Rpc-Device_id": {a.Device.ID}, "X-Rpc-Device_fp": {a.Device.FP}, "Referer": {"https://webstatic.mihoyo.com/"}, "Origin": {"https://webstatic.mihoyo.com"}}
	if game == "zzz" {
		headers.Set("Referer", "https://act.mihoyo.com/")
		headers.Set("Origin", "https://act.mihoyo.com")
	}
	if strings.HasPrefix(path, recordAPI) && game != "zzz" {
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		headers.Set("DS", DSX4(query.Encode(), string(raw)))
	}
	if c.BaseURL != "" { // local protocol fixtures use exactly the same path/query/body.
		u, _ := url.Parse(path)
		path = u.RequestURI()
	}
	var response recordObject
	err := c.JSON(ctx, method, path, query, body, headers, &response)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var status *HTTPStatusError
		if errors.As(err, &status) && (status.StatusCode == 429 || status.StatusCode == 403) {
			delay := 15 * time.Minute
			if seconds, err := strconv.Atoi(status.RetryAfter); err == nil && seconds > 0 {
				delay = max(delay, time.Duration(min(seconds, 86400))*time.Second)
			} else if at, err := http.ParseTime(status.RetryAfter); err == nil {
				delay = max(delay, min(time.Until(at), 24*time.Hour))
			}
			return nil, recordFailure("limited", "上游限制查询，冷却期间不会重复请求", delay)
		}
		return nil, recordFailure("unavailable", "便笺查询失败，稍后可手动重试；未执行签到或互动", 2*time.Minute)
	}
	code := recordInt(response["retcode"], -1<<31, 1<<31-1)
	if code == nil {
		return nil, recordFailure("invalid", "上游响应缺少有效状态码，未将其视为成功", 5*time.Minute)
	}
	switch *code {
	case 0:
	case 1034, 10035, 5003:
		return nil, recordFailure("verification", "上游要求安全验证，请在米游社官方客户端处理；本账号便笺与日历查询暂停 6 小时", 6*time.Hour)
	case -100, 10001, 10002:
		return nil, recordFailure("credentials", "米游社登录状态失效，请重新绑定账号", 15*time.Minute)
	case 10102:
		return nil, recordFailure("private", "请在米游社隐私设置中开启游戏战绩与实时便笺", 15*time.Minute)
	default:
		return nil, recordFailure("unavailable", "上游暂未提供此数据，请检查官方客户端的便笺与隐私设置", 5*time.Minute)
	}
	data := recordMap(response["data"])
	if data == nil {
		return nil, recordFailure("invalid", "上游数据结构无效，未将缺失值视为零", 5*time.Minute)
	}
	return data, nil
}

func (c *Client) RecordRoles(ctx context.Context, a model.Account, game string) ([]model.RecordRole, error) {
	if !RecordGame(game) {
		return nil, recordFailure("unsupported", "此游戏暂未接入便笺", 0)
	}
	d, err := c.recordRequest(ctx, a, game, http.MethodGet, TakumiAPI+AccountRolesPath, url.Values{"game_biz": {Games[game].GameBiz}}, nil)
	if err != nil {
		return nil, err
	}
	var list []recordObject
	if json.Unmarshal(d["list"], &list) != nil || list == nil || len(list) > 100 {
		return nil, recordFailure("invalid", "上游角色列表格式无效", 5*time.Minute)
	}
	roles := []model.RecordRole{}
	seen := map[string]bool{}
	for _, r := range list {
		if biz := recordText(r["game_biz"], 40); biz != "" && biz != Games[game].GameBiz {
			continue
		}
		uid := recordText(r["game_uid"], 20)
		if uid == "" {
			if n := recordInt(r["game_uid"], 1, 1<<53-1); n != nil {
				uid = strconv.FormatInt(*n, 10)
			}
		}
		region := recordText(r["region"], 40)
		if !RecordRoleParameters(uid, region) || seen[uid+":"+region] {
			continue
		}
		seen[uid+":"+region] = true
		roles = append(roles, model.RecordRole{UID: uid, Region: region, Nickname: recordText(r["nickname"], 64), RegionName: recordText(r["region_name"], 64)})
	}
	if len(list) > 0 && len(roles) == 0 {
		return nil, recordFailure("invalid", "上游未返回可识别的游戏角色", 5*time.Minute)
	}
	return roles, nil
}

func (c *Client) RecordNote(ctx context.Context, a model.Account, game string, role model.RecordRole) (model.GameNote, error) {
	if !RecordRoleParameters(role.UID, role.Region) {
		return model.GameNote{}, recordFailure("invalid", "游戏角色参数无效", 0)
	}
	path := map[string]string{"genshin": "/game_record/app/genshin/api/dailyNote", "starrail": "/game_record/app/hkrpg/api/note", "zzz": "/event/game_record_zzz/api/zzz/note"}[game]
	d, err := c.recordRequest(ctx, a, game, http.MethodGet, recordAPI+path, url.Values{"role_id": {role.UID}, "server": {role.Region}}, nil)
	if err != nil {
		return model.GameNote{}, err
	}
	return parseRecordNote(game, d)
}

func parseRecordNote(game string, d recordObject) (model.GameNote, error) {
	note := model.GameNote{Metrics: []model.RecordMetric{}, Flags: []model.RecordFlag{}}
	metric := func(key, label string, source recordObject, current, maxKey, recovery string) {
		note.Metrics = append(note.Metrics, model.RecordMetric{Key: key, Label: label, Current: recordInt(source[current], 0, 1e9), Max: recordInt(source[maxKey], 1, 1e9), RecoverySeconds: recordInt(source[recovery], 0, 30*86400)})
	}
	flag := func(key, label string, value *bool) {
		note.Flags = append(note.Flags, model.RecordFlag{Key: key, Label: label, Value: value})
	}
	switch game {
	case "genshin":
		metric("energy", "原粹树脂", d, "current_resin", "max_resin", "resin_recovery_time")
		metric("daily", "每日委托", d, "finished_task_num", "total_task_num", "")
		metric("weekly", "周本优惠剩余", d, "remain_resin_discount_num", "resin_discount_num_limit", "")
		metric("expeditions", "探索派遣", d, "current_expedition_num", "max_expedition_num", "")
		metric("home", "洞天宝钱", d, "current_home_coin", "max_home_coin", "home_coin_recovery_time")
		flag("daily_reward", "委托额外奖励已领取", recordBool(d["is_extra_task_reward_received"]))
	case "starrail":
		metric("energy", "开拓力", d, "current_stamina", "max_stamina", "stamina_recover_time")
		metric("reserve", "后备开拓力", d, "current_reserve_stamina", "", "")
		metric("daily", "每日实训", d, "current_train_score", "max_train_score", "")
		metric("weekly", "历战余响已用次数", d, "weekly_cocoon_cnt", "weekly_cocoon_limit", "")
		metric("period", "宇宙每周积分", d, "period_score", "period_max_score", "")
		metric("expeditions", "委托派遣", d, "accepted_epedition_num", "total_expedition_num", "")
	case "zzz":
		energy := recordMap(d["energy"])
		metric("energy", "电量", recordMap(energy["progress"]), "current", "max", "")
		note.Metrics[0].RecoverySeconds = recordInt(energy["restore"], 0, 30*86400)
		metric("daily", "每日活跃度", recordMap(d["vitality"]), "current", "max", "")
		var signed *bool
		if value := recordText(d["card_sign"], 40); value == "CardSignDone" || value == "CardSignNo" {
			b := value == "CardSignDone"
			signed = &b
		}
		flag("card", "刮刮卡已完成", signed)
		var sale *bool
		if value := recordText(recordMap(d["vhs_sale"])["sale_state"], 40); value == "SaleStateDoing" || value == "SaleStateNo" || value == "SaleStateDone" {
			b := value != "SaleStateNo"
			sale = &b
		}
		flag("store", "录像店已开始经营", sale)
	}
	known := false
	for _, metric := range note.Metrics {
		known = known || metric.Current != nil
	}
	if !known {
		return model.GameNote{}, recordFailure("invalid", "上游未提供可识别的便笺数值，未将缺失值视为零", 5*time.Minute)
	}
	return note, nil
}
