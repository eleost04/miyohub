package shop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

var ErrUncertain = errors.New("兑换结果无法确认，请先在米游社查看兑换记录，勿重复兑换")

func (s Service) device() model.Device {
	if s.Account.Device.ID != "" {
		return s.Account.Device
	}
	return s.Config.Device
}

// Prepare checks account-owned delivery details against fresh upstream data.
// It never consumes points or sends an exchange request.
func (s Service) Prepare(ctx context.Context, p model.ExchangePlan, immediate bool) (model.ExchangePlan, error) {
	return s.prepare(ctx, p, immediate, false)
}

// PrepareScheduled validates an already claimed reservation. A catalog rollover
// must not be confused with a user booking before the original sale opened.
func (s Service) PrepareScheduled(ctx context.Context, p model.ExchangePlan) (model.ExchangePlan, error) {
	return s.prepare(ctx, p, false, true)
}

func (s Service) prepare(ctx context.Context, p model.ExchangePlan, immediate, executing bool) (model.ExchangePlan, error) {
	if s.Account.Cookie == "" {
		return p, errors.New("账号缺少 Cookie，请重新绑定")
	}
	good, err := s.GoodDetail(ctx, p.GoodsID)
	if err != nil {
		return p, err
	}
	if text(good["goods_id"], "") != p.GoodsID {
		return p, errors.New("商品详情不完整")
	}
	p.GoodsName = text(good["goods_name"], "")
	p.GameBiz = text(good["game_biz"], "")
	price := intValue(good["price"])
	if p.Price > 0 && p.Price != price {
		return p, errors.New("商品价格已变化，请重新查看商品并创建计划")
	}
	p.Price = price
	p.GoodsType = intValue(good["type"])
	if p.GoodsType != 1 && p.GoodsType != 2 {
		return p, errors.New("商品类型不支持，请在米游社中兑换")
	}
	if err := validateAvailability(good, p, immediate, executing); err != nil {
		return p, err
	}
	if p.GoodsType == 1 {
		addresses, err := s.Addresses(ctx)
		if err != nil {
			return p, err
		}
		found := false
		for _, a := range addresses {
			if a["id"] == p.AddressID && p.AddressID != "" {
				found = true
			}
		}
		if !found {
			return p, errors.New("请选择该账号有效的收货地址")
		}
		p.UID = ""
		p.Region = ""
		p.GameBiz = ""
	} else {
		p.AddressID = ""
		if p.GameBiz != "" {
			roles, err := s.Roles(ctx, p.GameBiz)
			if err != nil {
				return p, err
			}
			found := false
			for _, r := range roles {
				if r["uid"] == p.UID && r["region"] == p.Region && p.UID != "" && p.Region != "" {
					found = true
				}
			}
			if !found {
				return p, errors.New("请选择该账号对应游戏的有效角色")
			}
		} else {
			p.UID = ""
			p.Region = ""
		}
	}
	points, err := s.Points(ctx)
	if err != nil {
		return p, err
	}
	balance, ok := points["points"]
	if !ok {
		balance, ok = points["point"]
	}
	if !ok {
		return p, errors.New("接口未返回米游币余额")
	}
	if intValue(balance) < p.Price {
		return p, fmt.Errorf("米游币不足，需要 %d", p.Price)
	}
	p.DeviceFP = s.Account.ShopDeviceFP
	if p.DeviceFP == "" {
		p.DeviceFP, err = s.DeviceFP(ctx)
	}
	return p, err
}

func validateAvailability(good map[string]any, p model.ExchangePlan, immediate, executing bool) error {
	if immediate && good["display_status"] != "online" && good["display_status"] != "always" {
		return errors.New("商品暂不可兑换，请检查开放时间和库存")
	}
	if !immediate && good["display_status"] == "ended" {
		return errors.New("本期商品已结束兑换，米哈游尚未公布下一期开售时间")
	}
	opening := int64(intValue(good["exchange_timestamp"]))
	if !p.Auto || opening <= p.ExchangeAt {
		return nil
	}
	openingText := time.Unix(opening, 0).In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05 +08:00")
	if executing {
		if int64(intValue(good["now_time"])) >= p.ExchangeAt && boolValue(good["sold_out"]) && int64(intValue(good["sale_start_time"])) <= p.ExchangeAt {
			return fmt.Errorf("本期库存已兑完，下一期开售时间为 %s；本次预约未自动改期", openingText)
		}
		return fmt.Errorf("商品可兑换时间已变为 %s，请确认后重新预约", openingText)
	}
	return fmt.Errorf("计划时间早于商品开放时间（米哈游：%s）", openingText)
}

// Repeat explicit temporary/generic rejections within a bounded window.
// A valid nonzero retcode confirms rejection; transport errors and malformed
// replies do not, and may follow a committed exchange. Never replay those.
func retryable(result map[string]any) bool {
	switch retcode(result) {
	case 0, -999999, -100, -101, -10001, -10002, -110:
		return false
	}
	message := strings.ToLower(text(result["message"], ""))
	for _, word := range []string{"不足", "限购", "上限", "已兑换", "已经兑换", "售罄", "兑完", "库存", "验证", "安全", "登录", "过期", "失效", "cookie", "token", "签名", "地址", "角色", "区服", "参数", "无效", "权限", "禁止", "封禁", "invalid", "insufficient", "sold out", "already", "limit exceeded"} {
		if strings.Contains(message, word) {
			return false
		}
	}
	switch message {
	case "兑换失败", "兑换失败。", "兑换失败，请稍后重试", "兑换失败，请稍后再试", "系统错误", "服务器错误", "内部错误", "fail", "failed":
		return true
	}
	for _, word := range []string{"未开始", "尚未开始", "稍后重试", "稍后再试", "繁忙", "排队", "频繁", "too many requests", "busy", "try again", "temporarily"} {
		if strings.Contains(message, word) {
			return true
		}
	}
	return false
}

func exchangeResultText(result map[string]any) string {
	message := text(result["message"], "兑换失败")
	if code := retcode(result); code != 0 {
		message = fmt.Sprintf("%s（错误码 %d）", message, code)
	}
	if reason := text(result["retry_stop"], ""); reason != "" {
		message += "；" + reason
	}
	return message
}

const maxExchangeAttempts = 60

func (s Service) ExchangeWithRetry(ctx context.Context, p model.ExchangePlan, progress func(int, string) error) (map[string]any, error) {
	window := time.Duration(max(0, min(120, s.Config.Shop.RetrySeconds)) * float64(time.Second))
	deadline := time.Now().Add(window)
	interval := time.Duration(max(0.2, min(30, s.Config.Shop.RetryInterval)) * float64(time.Second))
	var last map[string]any
	finish := func(reason string) (map[string]any, error) { last["retry_stop"] = reason; return last, nil }
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if last != nil && !time.Now().Before(deadline) {
			return finish("已到达重试时限")
		}
		// Only waiting/starting is bounded by the retry window. Do not abort
		// an in-flight request at its boundary and turn a known result unknown.
		queueDeadline := deadline
		if window == 0 {
			queueDeadline = time.Now().Add(15 * time.Second)
		}
		queueCtx, queueCancel := context.WithDeadline(ctx, queueDeadline)
		release := func() {}
		if s.AcquireExchange != nil {
			var err error
			release, err = s.AcquireExchange(queueCtx)
			if err != nil {
				queueCancel()
				if last != nil && ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
					return finish("已到达重试时限")
				}
				return nil, err
			}
		}
		queueCancel()
		if err := ctx.Err(); err != nil {
			release()
			return nil, err
		}
		if last != nil && !time.Now().Before(deadline) {
			release()
			return finish("已到达重试时限")
		}
		if progress != nil {
			if err := progress(attempt, "正在发送兑换请求"); err != nil {
				release()
				return nil, err
			}
		}
		requestCtx, requestCancel := context.WithTimeout(ctx, 15*time.Second)
		result, err := s.Exchange(requestCtx, p)
		requestCancel()
		release()
		if err != nil {
			return nil, ErrUncertain
		}
		result["attempt"] = attempt
		last = result
		if boolValue(result["ok"]) {
			return result, nil
		}
		if s.Emit != nil {
			s.Emit(fmt.Sprintf("第 %d 次请求：%s", attempt, exchangeResultText(result)))
		}
		if window == 0 {
			return finish("未启用自动重试")
		}
		if !retryable(result) {
			return finish("此类拒绝不自动重试，请检查原因")
		}
		if attempt >= maxExchangeAttempts {
			return finish("已达到 60 次请求上限")
		}
		if !time.Now().Before(deadline) {
			return finish("已到达重试时限")
		}
		delay := interval
		message := strings.ToLower(text(result["message"], ""))
		if strings.Contains(message, "频繁") || strings.Contains(message, "too many") {
			delay = max(delay, 2*time.Second)
		}
		if progress != nil {
			if err := progress(attempt, exchangeResultText(result)+fmt.Sprintf("；%.1f 秒后自动重试", delay.Seconds())); err != nil {
				return nil, err
			}
		}
		timer := time.NewTimer(min(delay, time.Until(deadline)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
