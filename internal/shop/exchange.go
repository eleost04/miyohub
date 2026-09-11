package shop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

type Service struct {
	Client  *mihoyo.Client
	Config  model.Config
	Account model.Account
	Emit    func(string)
	// AcquireExchange serializes actual requests, not preparation or retry waits.
	AcquireExchange func(context.Context) (release func(), err error)
}

func (s Service) Goods(ctx context.Context, game string) (map[string]any, error) {
	game = strings.TrimSpace(game)
	// The upstream advertises an "all" key but responds with an empty list
	// for game=all. Its actual unfiltered catalog uses the empty value.
	if game == "all" {
		game = ""
	}
	goods := make([]map[string]any, 0)
	var games []map[string]string
	seen := map[string]bool{}
	for page := 1; page <= 50; page++ {
		var result map[string]any
		query := url.Values{"app_id": {"1"}, "point_sn": {"myb"}, "page_size": {"20"}, "page": {strconv.Itoa(page)}, "game": {game}}
		if err := s.Client.JSON(ctx, http.MethodGet, mihoyo.TakumiAPI+mihoyo.MallGoodsPath, query, nil, s.goodsHeaders(), &result); err != nil {
			return nil, err
		}
		if retcode(result) != 0 {
			return nil, errors.New(text(result["message"], "商品列表获取失败"))
		}
		data := dataMap(result)
		if page == 1 {
			games = normalizeGames(data["games"])
		}
		items := maps(data["list"])
		if len(items) == 0 {
			break
		}
		added := 0
		for _, item := range items {
			id := text(item["goods_id"], "")
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			goods = append(goods, normalizeGood(item))
			added++
		}
		if added == 0 || len(items) < 20 || data["has_more"] == false {
			break
		}
	}
	sort.SliceStable(goods, func(i, j int) bool {
		left, right := goods[i], goods[j]
		leftSold, rightSold := boolValue(left["sold_out"]), boolValue(right["sold_out"])
		if leftSold != rightSold {
			return !leftSold
		}
		leftOnline, rightOnline := left["display_status"] == "online", right["display_status"] == "online"
		if leftOnline != rightOnline {
			return leftOnline
		}
		leftTime, rightTime := intValue(left["exchange_timestamp"]), intValue(right["exchange_timestamp"])
		if leftTime == 0 {
			leftTime = 1<<31 - 1
		}
		if rightTime == 0 {
			rightTime = 1<<31 - 1
		}
		if leftTime != rightTime {
			return leftTime < rightTime
		}
		return text(left["goods_id"], "") < text(right["goods_id"], "")
	})
	return map[string]any{"games": games, "goods": goods}, nil
}

func (s Service) GoodDetail(ctx context.Context, goodsID string) (map[string]any, error) {
	goodsID = strings.TrimSpace(goodsID)
	if goodsID == "" {
		return nil, errors.New("缺少 goods_id")
	}
	var result map[string]any
	query := url.Values{"app_id": {"1"}, "point_sn": {"myb"}, "goods_id": {goodsID}}
	if err := s.Client.JSON(ctx, http.MethodGet, mihoyo.TakumiAPI+mihoyo.MallDetailPath, query, nil, s.goodsHeaders(), &result); err != nil {
		return nil, err
	}
	if retcode(result) != 0 {
		return nil, errors.New(text(result["message"], "商品详情获取失败"))
	}
	return normalizeGood(dataMap(result)), nil
}

func (s Service) Exchange(ctx context.Context, plan model.ExchangePlan) (map[string]any, error) {
	if strings.TrimSpace(plan.GoodsID) == "" {
		return nil, errors.New("缺少 goods_id")
	}
	if strings.TrimSpace(plan.DeviceFP) == "" {
		return nil, errors.New("兑换计划缺少 device_fp")
	}
	body := map[string]any{"app_id": 1, "point_sn": "myb", "goods_id": plan.GoodsID, "exchange_num": 1}
	if plan.AddressID != "" {
		body["address_id"] = plan.AddressID
	}
	if plan.UID != "" && plan.Region != "" && plan.GameBiz != "" {
		body["uid"], body["region"], body["game_biz"] = plan.UID, plan.Region, plan.GameBiz
	}
	var result map[string]any
	if err := s.Client.JSON(ctx, http.MethodPost, mihoyo.MallExchangePath, nil, body, s.exchangeHeaders(plan), &result); err != nil {
		return nil, err
	}
	code := retcode(result)
	if code == -999999 {
		return nil, ErrUncertain
	}
	result = map[string]any{"retcode": code, "message": text(result["message"], "")}
	result["ok"] = code == 0
	result["message"] = text(result["message"], map[bool]string{true: "兑换成功", false: "兑换失败"}[code == 0])
	result["retcode"] = code
	return result, nil
}

func (s Service) Points(ctx context.Context) (map[string]any, error) {
	query := url.Values{"app_id": {"1"}, "point_sn": {"myb"}}
	var result map[string]any
	if err := s.Client.JSON(ctx, http.MethodGet, mihoyo.TakumiAPI+mihoyo.MallPointPath, query, nil, s.accountHeaders("api-takumi.mihoyo.com", "https://webstatic.mihoyo.com"), &result); err != nil {
		return nil, err
	}
	if retcode(result) != 0 {
		return nil, errors.New(text(result["message"], "米游币余额获取失败"))
	}
	return dataMap(result), nil
}

func (s Service) Addresses(ctx context.Context) ([]map[string]string, error) {
	query := url.Values{"t": {strconv.FormatInt(time.Now().UnixMilli(), 10)}}
	var result map[string]any
	if err := s.Client.JSON(ctx, http.MethodGet, mihoyo.TakumiAPI+mihoyo.MallAddressPath, query, nil, s.accountHeaders("api-takumi.mihoyo.com", "https://user.mihoyo.com"), &result); err != nil {
		return nil, err
	}
	if retcode(result) != 0 {
		return nil, errors.New(text(result["message"], "收货地址获取失败"))
	}
	items := maps(dataMap(result)["list"])
	addresses := make([]map[string]string, 0, len(items))
	for _, item := range items {
		addresses = append(addresses, map[string]string{
			"id":      text(item["id"], ""),
			"name":    text(item["connect_name"], text(item["name"], "")),
			"phone":   text(item["connect_areacode"], "") + text(item["connect_mobile"], ""),
			"address": text(item["addr_ext"], text(item["address"], "")),
		})
	}
	return addresses, nil
}

func (s Service) Roles(ctx context.Context, gameBiz string) ([]map[string]string, error) {
	gameBiz = strings.TrimSpace(gameBiz)
	if gameBiz == "" {
		return []map[string]string{}, nil
	}
	query := url.Values{"game_biz": {gameBiz}}
	var result map[string]any
	if err := s.Client.JSON(ctx, http.MethodGet, mihoyo.TakumiAPI+mihoyo.AccountRolesPath, query, nil, s.accountHeaders("api-takumi.mihoyo.com", "https://webstatic.mihoyo.com"), &result); err != nil {
		return nil, err
	}
	if retcode(result) != 0 {
		return nil, errors.New(text(result["message"], "角色获取失败"))
	}
	items := maps(dataMap(result)["list"])
	roles := make([]map[string]string, 0, len(items))
	for _, item := range items {
		roles = append(roles, map[string]string{
			"uid":         text(item["game_uid"], ""),
			"region":      text(item["region"], ""),
			"nickname":    text(item["nickname"], ""),
			"level":       text(item["level"], ""),
			"region_name": text(item["region_name"], ""),
		})
	}
	return roles, nil
}

func (s Service) DeviceFP(ctx context.Context) (string, error) {
	// Keep the platform-5 shop profile separate from app login fingerprints.
	// MiyoQian's exchange flow supplies this web environment as a JSON string.
	fields, err := json.Marshal(map[string]any{
		"userAgent": mihoyo.DefaultMobileUA, "browserScreenSize": 243750, "maxTouchPoints": 5,
		"isTouchSupported": true, "browserLanguage": "zh-CN", "browserPlat": "iPhone",
		"browserTimeZone": "Asia/Shanghai", "webGlRender": "Apple GPU", "webGlVendor": "Apple Inc.",
		"numOfPlugins": 0, "listOfPlugins": "unknown", "screenRatio": 3, "deviceMemory": "unknown",
		"hardwareConcurrency": "4", "cpuClass": "unknown", "ifNotTrack": "unknown", "ifAdBlock": 0,
		"hasLiedResolution": 1, "hasLiedOs": 0, "hasLiedBrowser": 0,
	})
	if err != nil {
		return "", errors.New("无法生成兑换设备环境")
	}
	body := map[string]any{"seed_id": mihoyo.DeviceFP()[:8], "device_id": strings.ToLower(s.device().ID), "platform": "5", "seed_time": strconv.FormatInt(time.Now().UnixMilli(), 10), "app_name": "account_cn", "device_fp": mihoyo.DeviceFP(), "ext_fields": string(fields)}
	var result map[string]any
	if err := s.Client.JSON(ctx, http.MethodPost, mihoyo.DeviceFPURL, nil, body, http.Header{"User-Agent": {mihoyo.DefaultMobileUA}}, &result); err != nil {
		return "", err
	}
	if retcode(result) != 0 {
		return "", errors.New(text(result["message"], "device_fp 获取失败"))
	}
	value := text(dataMap(result)["device_fp"], "")
	if value == "" {
		return "", errors.New("device_fp 获取失败")
	}
	return value, nil
}

func (s Service) goodsHeaders() http.Header {
	return http.Header{"Accept": {"application/json, text/plain, */*"}, "Accept-Language": {"zh-CN,zh-Hans;q=0.9"}, "Origin": {"https://user.mihoyo.com"}, "Referer": {"https://user.mihoyo.com/"}, "User-Agent": {mihoyo.DefaultMobileUA}, "X-Rpc-Device_id": {s.device().ID}, "X-Rpc-Client_type": {"5"}, "Cookie": {s.Account.Cookie}}
}

func (s Service) exchangeHeaders(plan model.ExchangePlan) http.Header {
	return http.Header{"Accept": {"application/json, text/plain, */*"}, "Accept-Language": {"zh-CN,zh-Hans;q=0.9"}, "Content-Type": {"application/json;charset=utf-8"}, "Origin": {"https://webstatic.miyoushe.com"}, "Referer": {"https://webstatic.miyoushe.com/"}, "User-Agent": {mihoyo.DefaultMobileUA}, "X-Rpc-App_version": {"2.106.2"}, "X-Rpc-Channel": {"appstore"}, "X-Rpc-Client_type": {"1"}, "X-Rpc-Verify_key": {"bll8iq97cem8"}, "X-Rpc-Device_fp": {plan.DeviceFP}, "X-Rpc-Device_id": {s.device().ID}, "X-Rpc-Device_model": {s.device().Model}, "X-Rpc-Device_name": {s.device().Name}, "X-Rpc-Sys_version": {"12"}, "Cookie": {s.Account.Cookie}}
}

func (s Service) accountHeaders(host, origin string) http.Header {
	return http.Header{"Accept": {"application/json, text/plain, */*"}, "Accept-Language": {"zh-CN,zh-Hans;q=0.9"}, "Origin": {origin}, "Referer": {origin + "/"}, "User-Agent": {mihoyo.DefaultMobileUA}, "X-Rpc-Device_id": {s.device().ID}, "X-Rpc-Client_type": {"5"}, "Cookie": {s.Account.Cookie}}
}

func retcode(value map[string]any) int {
	if n, ok := value["retcode"].(int); ok {
		return n
	}
	if n, ok := value["retcode"].(float64); ok && n == float64(int(n)) {
		return int(n)
	}
	return -999999
}
func dataMap(value map[string]any) map[string]any {
	if data, ok := value["data"].(map[string]any); ok {
		return data
	}
	return map[string]any{}
}
func text(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	valueText := strings.TrimSpace(fmt.Sprint(value))
	if valueText == "" {
		return fallback
	}
	return valueText
}

func normalizeGames(value any) []map[string]string {
	items, ok := value.([]any)
	if !ok {
		return []map[string]string{}
	}
	result := make([]map[string]string, 0, len(items))
	for _, item := range items {
		data, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key := text(data["key"], "")
		if key == "" {
			key = text(data["game"], "")
		}
		if key != "" {
			result = append(result, map[string]string{"key": key, "name": text(data["name"], key)})
		}
	}
	return result
}

func normalizeGood(raw map[string]any) map[string]any {
	goodsID := text(raw["goods_id"], "")
	status := text(raw["status"], "")
	unlimit := boolValue(raw["unlimit"])
	total := intValue(raw["total"])
	nextNum := intValue(raw["next_num"])
	soldOut := !unlimit && ((raw["total"] != nil && total <= 0) || (raw["total"] == nil && nextNum <= 0))
	nextTime := intValue(raw["next_time"])
	saleStart := intValue(raw["sale_start_time"])
	now := intValue(raw["now_time"])
	if now == 0 {
		now = intValue(time.Now().Unix())
	}
	exchangeAt := 0
	if soldOut {
		exchangeAt = nextTime
	} else if status != "online" && saleStart > now {
		exchangeAt = saleStart
	} else if status != "online" {
		exchangeAt = nextTime
	}
	displayStatus := status
	if exchangeAt > 0 && soldOut {
		displayStatus = "sold_out_with_next"
	} else if soldOut {
		displayStatus = "ended"
	} else if unlimit {
		displayStatus = "always"
	} else if status == "online" {
		displayStatus = "online"
	} else if exchangeAt > 0 {
		displayStatus = "scheduled"
	}
	stock := "未知"
	if unlimit {
		stock = "不限"
	} else if raw["total"] != nil {
		stock = strconv.Itoa(total)
	} else if raw["next_num"] != nil {
		stock = strconv.Itoa(nextNum)
	}
	exchangeTime := "未公布或已结束"
	if displayStatus == "always" {
		exchangeTime = "任何时间"
	} else if displayStatus == "online" {
		exchangeTime = "正在兑换"
	} else if displayStatus == "ended" {
		exchangeTime = "已结束"
	} else if exchangeAt > 0 {
		exchangeTime = time.Unix(int64(exchangeAt), 0).In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05")
	}
	return map[string]any{
		"goods_id":           goodsID,
		"type":               intValue(raw["type"]),
		"requires_address":   intValue(raw["type"]) == 1,
		"requires_role":      intValue(raw["type"]) == 2 && text(raw["game_biz"], "") != "",
		"goods_name":         text(raw["goods_name"], text(raw["name"], "未命名商品")),
		"price":              intValue(raw["price"]),
		"icon":               text(raw["icon"], ""),
		"game_biz":           text(raw["game_biz"], ""),
		"status":             status,
		"unlimit":            unlimit,
		"total":              raw["total"],
		"next_num":           raw["next_num"],
		"stock":              stock,
		"sold_out":           soldOut,
		"sale_start_time":    saleStart,
		"next_time":          nextTime,
		"now_time":           now,
		"exchange_timestamp": exchangeAt,
		"exchange_time":      exchangeTime,
		"display_status":     displayStatus,
		"limit":              formatLimit(raw),
	}
}

func formatLimit(raw map[string]any) string {
	limit := intValue(raw["account_cycle_limit"])
	if text(raw["account_cycle_type"], "") == "not_limit" || limit <= 0 {
		return "不限购"
	}
	cycle := map[string]string{"forever": "永久", "month": "每月"}[text(raw["account_cycle_type"], "")]
	if cycle == "" {
		cycle = "周期"
	}
	return fmt.Sprintf("%s %d/%d", cycle, intValue(raw["account_exchange_num"]), limit)
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed != 0
	case int:
		return typed != 0
	case string:
		return typed == "1" || strings.EqualFold(typed, "true")
	default:
		return false
	}
}

func intValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	case string:
		n, _ := strconv.Atoi(typed)
		return n
	default:
		return 0
	}
}

func maps(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if data, ok := item.(map[string]any); ok {
			result = append(result, data)
		}
	}
	return result
}
