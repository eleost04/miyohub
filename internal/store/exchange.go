package store

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

func validatePlan(p model.ExchangePlan) error {
	if p.AccountID == "" || p.GoodsID == "" {
		return errors.New("请选择账号和商品")
	}
	for _, v := range []string{p.GoodsID, p.AccountID, p.GoodsName, p.UID, p.Region, p.GameBiz, p.AddressID} {
		if len(v) > 512 || strings.ContainsAny(v, "\r\n\x00") {
			return errors.New("兑换计划字段无效")
		}
	}
	if p.Auto && (p.ExchangeAt <= time.Now().Unix() || p.ExchangeAt > time.Now().AddDate(1, 0, 0).Unix()) {
		return errors.New("自动兑换时间须在未来一年内")
	}
	if p.ExchangeAt < 0 {
		return errors.New("兑换时间无效")
	}
	return nil
}
func (s *Store) CreateExchangePlan(userID string, admin bool, p model.ExchangePlan) (model.ExchangePlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkExchangeAccessLocked(userID, admin, p.AccountID); err != nil {
		return model.ExchangePlan{}, err
	}
	if err := validatePlan(p); err != nil {
		return model.ExchangePlan{}, err
	}
	p.ID = randomID("plan_")
	p.State = "pending"
	p.Phase = ""
	p.Revision = 1
	p.Attempt = 0
	p.AttemptKey = ""
	p.LastRun = ""
	p.LastResult = ""
	p.DeviceFP = ""
	s.data.Config.Shop.Plans = append(s.data.Config.Shop.Plans, p)
	return p, s.saveLocked()
}
func (s *Store) UpdateExchangePlan(userID string, admin bool, p model.ExchangePlan) (model.ExchangePlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, old := range s.data.Config.Shop.Plans {
		if old.ID != p.ID {
			continue
		}
		if !s.accountOwnedLocked(userID, admin, old.AccountID) || !s.accountOwnedLocked(userID, admin, p.AccountID) {
			return model.ExchangePlan{}, errors.New("兑换计划不存在或无权访问")
		}
		if err := s.checkExchangeAccessLocked(userID, admin, old.AccountID); err != nil {
			return model.ExchangePlan{}, err
		}
		if err := s.checkExchangeAccessLocked(userID, admin, p.AccountID); err != nil {
			return model.ExchangePlan{}, err
		}
		if old.State != "pending" || old.Revision != p.Revision {
			return model.ExchangePlan{}, errors.New("计划已执行或被修改，请刷新；再次兑换请新建计划")
		}
		if err := validatePlan(p); err != nil {
			return model.ExchangePlan{}, err
		}
		p.State = old.State
		p.Phase = old.Phase
		p.Revision = old.Revision + 1
		p.Attempt = old.Attempt
		p.AttemptKey = old.AttemptKey
		p.LastRun = old.LastRun
		p.LastResult = old.LastResult
		p.DeviceFP = ""
		s.data.Config.Shop.Plans[i] = p
		return p, s.saveLocked()
	}
	return model.ExchangePlan{}, errors.New("兑换计划不存在或无权访问")
}
func (s *Store) DeleteExchangePlan(userID string, admin bool, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.data.Config.Shop.Plans {
		if p.ID != id || !s.accountOwnedLocked(userID, admin, p.AccountID) {
			continue
		}
		if p.State == "running" {
			return errors.New("请先停止执行中的计划")
		}
		s.data.Config.Shop.Plans = append(s.data.Config.Shop.Plans[:i], s.data.Config.Shop.Plans[i+1:]...)
		return s.saveLocked()
	}
	return errors.New("兑换计划不存在或无权访问")
}
func (s *Store) accountRunnableLocked(id string) (model.Account, bool) {
	for _, a := range s.data.Config.Accounts {
		if a.ID == id && !a.Disabled {
			for _, u := range s.data.Users {
				if u.ID == a.UserID && u.Status == "active" {
					return clone(a), true
				}
			}
		}
	}
	return model.Account{}, false
}

// Claim is persisted before any external exchange request. A plan runs once;
// even an interrupted request must be reviewed before creating another plan.
func (s *Store) ClaimExchangePlan(userID string, admin bool, id string, scheduled bool) (model.ExchangePlan, model.Account, error) {
	return s.ClaimExchangePlanAt(userID, admin, id, scheduled, time.Now(), 0)
}

// Scheduled execution may reserve a plan ahead of time for preparation only.
func (s *Store) ClaimExchangePlanAt(userID string, admin bool, id string, scheduled bool, now time.Time, lead time.Duration) (model.ExchangePlan, model.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fail := func(msg string) (model.ExchangePlan, model.Account, error) {
		return model.ExchangePlan{}, model.Account{}, errors.New(msg)
	}
	if !s.data.Config.Shop.Enabled {
		return fail("商品兑换已停用")
	}
	for i, p := range s.data.Config.Shop.Plans {
		if p.ID != id || !s.accountOwnedLocked(userID, admin, p.AccountID) {
			continue
		}
		if !p.Enabled || p.State != "pending" {
			return fail("计划已停用、执行中或已经执行，请刷新状态")
		}
		if err := s.checkExchangeAccessLocked(userID, admin, p.AccountID); err != nil {
			return model.ExchangePlan{}, model.Account{}, err
		}
		a, ok := s.accountRunnableLocked(p.AccountID)
		if !ok {
			return fail("账号或所属用户已停用")
		}
		if scheduled {
			if !p.Auto || p.ExchangeAt <= 0 || p.ExchangeAt > now.Add(lead).Unix() {
				return fail("计划未到执行时间")
			}
			if !now.Before(time.Unix(p.ExchangeAt, 0).Add(s.data.Config.Shop.DispatchWindow())) {
				p.State = "missed"
				p.LastResult = "已到达兑换窗口截止时间，请重新创建计划"
				s.data.Config.Shop.Plans[i] = p
				if err := s.saveLocked(); err != nil {
					return model.ExchangePlan{}, model.Account{}, err
				}
				return fail(p.LastResult)
			}
		}
		for _, other := range s.data.Config.Shop.Plans {
			if other.AccountID == p.AccountID && other.GoodsID == p.GoodsID && other.State == "running" {
				return fail("该账号已有同一商品的兑换正在执行")
			}
		}
		p.State = "running"
		p.Phase = "preparing"
		p.Attempt = 0
		p.AttemptKey = randomID("run_")
		p.LastRun = time.Now().Format(time.RFC3339)
		p.LastResult = "准备兑换"
		p.Revision++
		s.data.Config.Shop.Plans[i] = p
		return p, a, s.saveLocked()
	}
	return fail("兑换计划不存在或无权访问")
}
func (s *Store) ExchangeProgress(id, key string, attempt int, message string) error {
	return s.exchangeResult(id, key, "running", attempt, message)
}
func (s *Store) FinishExchange(id, key, state string, attempt int, message string) error {
	switch state {
	case "success", "failed", "unknown", "cancelled", "pending", "missed":
	default:
		return errors.New("无效执行状态")
	}
	return s.exchangeResult(id, key, state, attempt, message)
}
func (s *Store) exchangeResult(id, key, state string, attempt int, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.data.Config.Shop.Plans {
		if p.ID == id && p.AttemptKey == key && p.State == "running" {
			if attempt < p.Attempt || attempt < 0 {
				return errors.New("兑换请求次数不能回退")
			}
			if state == "pending" && (!p.Auto || attempt != 0 || p.Attempt != 0 || (p.Phase != "preparing" && p.Phase != "waiting")) {
				return errors.New("仅未发送请求的定时计划可以恢复等待")
			}
			p.State = state
			if state != "running" {
				p.Phase = ""
			} else if attempt > 0 {
				p.Phase = "exchanging"
			}
			p.Attempt = attempt
			p.LastResult = message
			s.data.Config.Shop.Plans[i] = p
			return s.saveLocked()
		}
	}
	return errors.New("兑换执行状态已变更")
}

func (s *Store) ExchangePhase(id, key, phase, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.data.Config.Shop.Plans {
		if p.ID == id && p.AttemptKey == key && p.State == "running" && p.Attempt == 0 {
			p.Phase, p.LastResult = phase, message
			s.data.Config.Shop.Plans[i] = p
			return s.saveLocked()
		}
	}
	return errors.New("兑换计划状态已变化")
}
func (s *Store) SetAccountDeviceFP(id, deviceID, fp string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, a := range s.data.Config.Accounts {
		if a.ID == id && a.Device.ID == deviceID {
			if a.ShopDeviceFP == fp {
				return nil
			}
			s.data.Config.Accounts[i].ShopDeviceFP = fp
			return s.saveLocked()
		}
	}
	return errors.New("账号设备已变化，请重试")
}
func recoverExchangePlans(state *model.State) {
	for i, p := range state.Config.Shop.Plans {
		if p.Revision == 0 {
			p.Revision = 1
		}
		if p.State == "running" && p.Attempt == 0 && (p.Phase == "preparing" || p.Phase == "waiting") && p.Auto {
			p.State = "pending"
			p.LastResult = "服务已恢复，等待重新准备兑换"
		} else if p.State == "running" || (p.State == "" && p.LastRun != "") {
			p.State = "unknown"
			p.LastResult = "服务中断或旧版执行结果待确认，请先在米游社查看兑换记录"
		} else if p.State == "" {
			p.State = "pending"
		}
		p.DeviceFP = ""
		p.Phase = ""
		state.Config.Shop.Plans[i] = p
	}
}
func (s *Store) ExchangeFailureLog(p model.ExchangePlan, userID string, err error) {
	_ = s.AddLogForUser(userID, "exchange", fmt.Sprintf("%s: 执行结果保存失败 (%s)，禁止重试，请检查存储", p.GoodsName, err))
}
