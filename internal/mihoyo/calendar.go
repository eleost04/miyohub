package mihoyo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

// Upstream calendar timestamps can be seconds or milliseconds. Other units,
// durations, fractions and out-of-range dates are not guessed into dates.
func recordTime(raw json.RawMessage) *time.Time {
	n := recordInt(raw, 946684800, 4102444800000)
	if n == nil {
		return nil
	}
	var t time.Time
	switch {
	case *n <= 4102444800:
		t = time.Unix(*n, 0)
	case *n >= 946684800000:
		t = time.UnixMilli(*n)
	default:
		return nil
	}
	t = t.UTC()
	return &t
}

func (c *Client) RecordCalendar(ctx context.Context, a model.Account, game string, role model.RecordRole) (model.GameCalendar, error) {
	if game != "genshin" && game != "starrail" {
		return model.GameCalendar{}, recordFailure("unsupported", "此游戏的官方活动日历暂未接入，可添加自定义日程", 0)
	}
	if !RecordRoleParameters(role.UID, role.Region) {
		return model.GameCalendar{}, recordFailure("invalid", "游戏角色参数无效", 0)
	}
	method, path := http.MethodGet, recordAPI+"/game_record/app/hkrpg/api/get_act_calender"
	query := url.Values{"role_id": {role.UID}, "server": {role.Region}}
	var body any
	if game == "genshin" {
		method, path = http.MethodPost, recordAPI+"/game_record/app/genshin/api/act_calendar"
		body = map[string]string{"role_id": role.UID, "server": role.Region}
		query = nil
	}
	d, err := c.recordRequest(ctx, a, game, method, path, query, body)
	if err != nil {
		return model.GameCalendar{}, err
	}
	return parseRecordCalendar(game, d, time.Now())
}

func parseRecordCalendar(game string, d recordObject, now time.Time) (model.GameCalendar, error) {
	result := model.GameCalendar{Events: []model.CalendarEvent{}}
	if serverTime := recordTime(d["now"]); serverTime != nil && serverTime.After(now.Add(-24*time.Hour)) && serverTime.Before(now.Add(24*time.Hour)) {
		result.ServerTime = serverTime
		now = *serverTime
	}
	type listKind struct{ key, kind, finished string }
	lists := []listKind{{"avatar_card_pool_list", "pool", ""}, {"act_list", "activity", "is_finished"}}
	if game == "genshin" {
		lists = append(lists, listKind{"weapon_card_pool_list", "pool", ""}, listKind{"mixed_card_pool_list", "pool", ""}, listKind{"fixed_act_list", "activity", "is_finished"})
	} else {
		lists[1].finished = "all_finished"
		lists = append(lists, listKind{"equip_card_pool_list", "pool", ""}, listKind{"challenge_list", "challenge", ""})
	}
	known := false
	seen := map[string]bool{}
	for _, list := range lists {
		raw, exists := d[list.key]
		if !exists {
			continue
		}
		var items []recordObject
		if json.Unmarshal(raw, &items) != nil || items == nil {
			result.Skipped++
			continue
		}
		known = true
		if len(items) > 200 {
			return model.GameCalendar{}, recordFailure("invalid", "活动列表超出数量限制", 5*time.Minute)
		}
		for _, item := range items {
			timing := recordMap(item["time_info"])
			start, end := recordTime(timing["start_ts"]), recordTime(timing["end_ts"])
			title := ""
			for _, key := range []string{"pool_name", "name", "title", "act_name", "name_mi18n"} {
				if title = recordText(item[key], 160); title != "" {
					break
				}
			}
			if title == "" || end == nil || start != nil && !end.After(*start) {
				result.Skipped++
				continue
			}
			if !end.After(now) || end.After(now.Add(366*24*time.Hour)) {
				continue
			}
			event := model.CalendarEvent{Title: title, Kind: list.kind, Source: "official", StartAt: start, EndAt: end, Finished: recordBool(item[list.finished])}
			if list.kind == "challenge" {
				status := recordText(item["status"], 50)
				if status == "challengeStatusFinish" {
					b := true
					event.Finished = &b
				}
			}
			// Stable within a published time window; no private role identifiers.
			key, _ := json.Marshal([]any{game, list.kind, title, start, end})
			hash := sha256.Sum256(key)
			event.ID = "official_" + hex.EncodeToString(hash[:12])
			if !seen[event.ID] {
				if len(result.Events) >= 200 {
					return model.GameCalendar{}, recordFailure("invalid", "活动总数超出查询限制", 5*time.Minute)
				}
				result.Events = append(result.Events, event)
				seen[event.ID] = true
			}
		}
	}
	if !known {
		return model.GameCalendar{}, recordFailure("invalid", "上游未提供可识别的活动列表，不能确认日历为空", 5*time.Minute)
	}
	sort.SliceStable(result.Events, func(i, j int) bool { return result.Events[i].EndAt.Before(*result.Events[j].EndAt) })
	return result, nil
}

// CalendarEventValid also protects user-supplied manual schedules. Text is
// rendered as plain text; URLs and HTML are not fetched or executed.
func CalendarEventValid(event model.CalendarEvent, now time.Time) bool {
	if strings.TrimSpace(event.Title) == "" || recordText(mustRecordJSON(event.Title), 160) == "" {
		return false
	}
	if event.Kind != "pool" && event.Kind != "activity" && event.Kind != "challenge" && event.Kind != "version" {
		return false
	}
	if event.StartAt == nil && event.EndAt == nil {
		return false
	}
	for _, at := range []*time.Time{event.StartAt, event.EndAt} {
		if at != nil && (at.Before(now.Add(-366*24*time.Hour)) || at.After(now.Add(366*24*time.Hour))) {
			return false
		}
	}
	return event.StartAt == nil || event.EndAt == nil || event.EndAt.After(*event.StartAt)
}
func mustRecordJSON(value string) json.RawMessage { raw, _ := json.Marshal(value); return raw }
