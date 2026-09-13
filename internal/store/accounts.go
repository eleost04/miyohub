package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

func randomID(prefix string) string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(raw)
}
func cookieFields(a *model.Account) error {
	fields := map[string]string{}
	for _, part := range strings.Split(a.Cookie, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok {
			fields[k] = v
		}
	}
	uid := ""
	for _, key := range []string{"stuid", "ltuid", "ltuid_v2", "account_id", "account_id_v2"} {
		if fields[key] != "" {
			if uid != "" && uid != fields[key] {
				return errors.New("Cookie 包含不同账号的 UID")
			}
			uid = fields[key]
		}
	}
	if uid != "" {
		if a.Stuid != "" && a.Stuid != uid {
			return errors.New("UID 与 Cookie 不一致")
		}
		a.Stuid = uid
	}
	if a.Stoken == "" {
		a.Stoken = fields["stoken"]
	}
	if a.Mid == "" {
		a.Mid = fields["mid"]
	}
	return nil
}

var uidPattern = regexp.MustCompile(`^[0-9]{1,20}$`)

func validateAccount(a model.Account) error {
	if err := validateAccountGroup(a.Group); err != nil {
		return err
	}
	if a.Stuid != "" && !uidPattern.MatchString(a.Stuid) {
		return errors.New("UID 只能包含数字")
	}
	for _, v := range []string{a.Stuid, a.Stoken, a.Mid} {
		if strings.ContainsAny(v, ";\r\n\x00") {
			return errors.New("凭据字段包含非法字符")
		}
	}
	if strings.TrimSpace(a.Name) == "" || utf8.RuneCountInString(a.Name) > 64 {
		return errors.New("账号名称应为 1–64 个字符")
	}
	for _, v := range []string{a.Cookie, a.Stuid, a.Stoken, a.Mid, a.Name} {
		if len(v) > 16384 || strings.ContainsAny(v, "\r\n\x00") {
			return errors.New("账号字段过长或包含非法字符")
		}
	}
	for k, v := range a.CloudTokens {
		if k != "genshin" && k != "zzz" {
			return errors.New("不支持的云游戏")
		}
		if len(v) > 8192 || strings.ContainsAny(v, "\r\n\x00") {
			return errors.New("云游戏凭据格式无效")
		}
	}
	return nil
}
func (s *Store) addAccountLocked(a model.Account, userID string) error {
	a = clone(a)
	a.Name = strings.TrimSpace(a.Name)
	if err := cookieFields(&a); err != nil {
		return err
	}
	if err := validateAccount(a); err != nil {
		return err
	}
	target := -1
	for i, item := range s.data.Config.Accounts {
		sameID := a.ID != "" && a.ID == item.ID
		sameUID := a.Stuid != "" && a.Stuid == item.Stuid
		sameName := a.Name == item.Name && item.UserID == a.UserID
		if !sameID && !sameUID && !sameName {
			continue
		}
		if userID != "" && item.UserID != userID {
			return errors.New("该米游社账号已绑定，不能重复绑定")
		}
		if a.Stuid != "" && item.Stuid != "" && a.Stuid != item.Stuid {
			return errors.New("账号名称已被另一 UID 使用，请更换名称")
		}
		if target >= 0 && target != i {
			return errors.New("账号 UID 或名称重复")
		}
		target = i
	}
	if target >= 0 {
		old := s.data.Config.Accounts[target]
		for _, p := range s.data.Config.Shop.Plans {
			if p.AccountID == old.ID && p.State == "running" {
				return errors.New("该账号正在兑换，请等待结束后重新绑定")
			}
		}
		a.ID = old.ID
		a.UserID = old.UserID
		if a.CloudTokens == nil {
			a.CloudTokens = old.CloudTokens
		}
		if a.Cookie == "" {
			a.Cookie = old.Cookie
		}
		if a.Stoken == "" {
			a.Stoken = old.Stoken
		}
		if a.Stuid == "" {
			a.Stuid = old.Stuid
		}
		if a.Mid == "" {
			a.Mid = old.Mid
		}
		if a.Device.ID == "" {
			a.Device = old.Device
		}
		a.Disabled = old.Disabled
		a.TaskResults = old.TaskResults
		a.LastTaskAt = old.LastTaskAt
		a.TaskSettings = old.TaskSettings
		a.Group = old.Group
	} else {
		a.ID = randomID("acc_")
		if a.TaskSettings == nil {
			a.TaskSettings = tasksFromConfig(defaultState().Config)
		}
		a.TaskSettings.Revision = 1
		if err := validateTaskSettings(*a.TaskSettings); err != nil {
			return err
		}
	}
	if a.Device.ID == "" {
		a.Device = model.Device{ID: mihoyo.DeviceID(), FP: mihoyo.DeviceFP(), Name: "MiyoHub", Model: "MiyoHub"}
	}
	a.Status = "unchecked"
	if target >= 0 {
		s.data.Config.Accounts[target] = a
	} else {
		s.data.Config.Accounts = append(s.data.Config.Accounts, a)
	}
	return s.saveLocked()
}

type AccountPatch struct {
	ID               string            `json:"id"`
	Name             *string           `json:"name"`
	Group            *string           `json:"group"`
	Disabled         *bool             `json:"disabled"`
	Cookie           string            `json:"cookie"`
	Stoken           string            `json:"stoken"`
	Mid              string            `json:"mid"`
	CloudTokens      map[string]string `json:"cloud_tokens"`
	ClearCloudTokens []string          `json:"clear_cloud_tokens"`
}

func (s *Store) UpdateAccount(user model.User, p AccountPatch) (model.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, old := range s.data.Config.Accounts {
		if old.ID != p.ID || old.UserID != user.ID {
			continue
		}
		a := clone(old)
		if p.Name != nil {
			a.Name = strings.TrimSpace(*p.Name)
		}
		if p.Group != nil {
			a.Group = strings.TrimSpace(*p.Group)
		}
		if p.Disabled != nil {
			a.Disabled = *p.Disabled
		}
		if p.Cookie != "" {
			a.Cookie = p.Cookie
			a.Status = "unchecked"
			parsed := model.Account{Cookie: p.Cookie}
			if err := cookieFields(&parsed); err != nil {
				return model.Account{}, err
			}
			if parsed.Stoken != "" {
				a.Stoken = parsed.Stoken
			}
			if parsed.Mid != "" {
				a.Mid = parsed.Mid
			}
		}
		if p.Stoken != "" {
			a.Stoken = p.Stoken
		}
		if p.Mid != "" {
			a.Mid = p.Mid
		}
		if err := cookieFields(&a); err != nil {
			return model.Account{}, err
		}
		if a.CloudTokens == nil {
			a.CloudTokens = map[string]string{}
		}
		for key, v := range p.CloudTokens {
			if v != "" {
				a.CloudTokens[key] = v
			}
		}
		for _, key := range p.ClearCloudTokens {
			delete(a.CloudTokens, key)
		}
		if err := validateAccount(a); err != nil {
			return model.Account{}, err
		}
		for _, other := range s.data.Config.Accounts {
			if other.ID != a.ID && (other.UserID == a.UserID && other.Name == a.Name || a.Stuid != "" && a.Stuid == other.Stuid) {
				return model.Account{}, errors.New("账号名称或 UID 已存在")
			}
		}
		s.data.Config.Accounts[i] = a
		return clone(a), s.saveLocked()
	}
	return model.Account{}, errors.New("账号不存在或无权访问")
}
func isAdmin(u model.User) bool { return u.Role == "admin" && u.Status == "active" }
func (s *Store) DeleteAccount(user model.User, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, a := range s.data.Config.Accounts {
		if a.ID != id || a.UserID != user.ID {
			continue
		}
		for _, p := range s.data.Config.Shop.Plans {
			if p.AccountID == id && p.State == "running" {
				return errors.New("兑换正在结束，请等待执行结果后删除账号")
			}
		}
		s.data.Config.Accounts = append(s.data.Config.Accounts[:i], s.data.Config.Accounts[i+1:]...)
		plans := make([]model.ExchangePlan, 0)
		for _, p := range s.data.Config.Shop.Plans {
			if p.AccountID != id {
				plans = append(plans, p)
			}
		}
		s.data.Config.Shop.Plans = plans
		s.removeCalendarLocked("", id)
		return s.saveLocked()
	}
	return errors.New("账号不存在或无权访问")
}
func (s *Store) AccountRunnable(id string) (model.Account, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.data.Config.Accounts {
		if a.ID != id || a.Disabled {
			continue
		}
		for _, u := range s.data.Users {
			if u.ID == a.UserID && u.Status == "active" {
				return clone(a), true
			}
		}
	}
	return model.Account{}, false
}
func (s *Store) RecordAccountCheck(id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Config.Accounts {
		if s.data.Config.Accounts[i].ID == id {
			s.data.Config.Accounts[i].Status = status
			s.data.Config.Accounts[i].CheckedAt = time.Now()
			return s.saveLocked()
		}
	}
	return errors.New("账号已删除")
}
func (s *Store) RecordTasks(id string, results map[string]model.TaskSummary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Config.Accounts {
		if s.data.Config.Accounts[i].ID == id {
			s.data.Config.Accounts[i].TaskResults = clone(results)
			s.data.Config.Accounts[i].LastTaskAt = time.Now()
			return s.saveLocked()
		}
	}
	return errors.New("账号已删除")
}

// A delayed renewal must never overwrite credentials from a newer binding.
func (s *Store) RenewAccountCookie(before model.Account, cookie string) (model.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, a := range s.data.Config.Accounts {
		if a.ID != before.ID {
			continue
		}
		if a.Cookie != before.Cookie || a.Stoken != before.Stoken || a.Stuid != before.Stuid || a.Mid != before.Mid || a.Device.ID != before.Device.ID || a.Disabled {
			return a, errors.New("账号状态或凭据已变化")
		}
		if _, ok := s.accountRunnableLocked(a.ID); !ok {
			return a, errors.New("账号已停用")
		}
		a.Cookie = cookie
		if err := cookieFields(&a); err != nil {
			return a, err
		}
		if err := validateAccount(a); err != nil {
			return a, err
		}
		s.data.Config.Accounts[i] = a
		return clone(a), s.saveLocked()
	}
	return model.Account{}, errors.New("账号已删除")
}

func (s *Store) BindAccountForUser(userID, targetID string, account model.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.userIndexLocked(userID); i < 0 || s.data.Users[i].Status != "active" {
		return errors.New("用户未激活或已删除")
	}
	if targetID != "" {
		found := false
		for _, old := range s.data.Config.Accounts {
			if old.ID != targetID || old.UserID != userID {
				continue
			}
			if old.Stuid != "" && old.Stuid != account.Stuid {
				return errors.New("请使用原来绑定的米游社账号登录")
			}
			account.ID, account.Name = old.ID, old.Name
			found = true
			break
		}
		if !found {
			return errors.New("重新绑定的账号已删除或无权访问")
		}
	}
	account.UserID = userID
	return s.addAccountLocked(account, userID)
}
