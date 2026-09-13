package store

import (
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

type Store struct {
	readOnly  bool
	cipher    cipher.AEAD
	mu        sync.RWMutex
	path      string
	data      model.State
	committed model.State
}

func New(path string) (*Store, error) {
	s := &Store{path: path, data: defaultState()}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// OpenReadOnly reads summaries without migrating files or recovering plans.
func OpenReadOnly(path string) (*Store, error) {
	s := &Store{path: path, data: defaultState(), readOnly: true}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func defaultState() model.State {
	return model.State{
		Config: model.Config{
			Enabled:    true,
			Accounts:   []model.Account{},
			Features:   model.Features{GameCheckin: true},
			Games:      model.GamesConfig{Enabled: []string{"genshin", "starrail", "zzz"}, Blacklist: map[string][]string{"genshin": {}, "starrail": {}, "zzz": {}}},
			CloudGames: model.CloudGamesConfig{Enabled: []string{"genshin", "zzz"}},
			BBS:        model.BBSConfig{Forums: []int{5, 2}, Checkin: true, CancelLike: true, PostLimit: 5, DelaySeconds: []int{1, 3}},
			Captcha:    model.CaptchaConfig{MaxRetries: 3, Channels: []model.CaptchaChannel{{ID: randomID("captcha_"), Provider: "damagou", Timeout: 60}}},
			Schedule:   model.Schedule{Time: "09:00", Timezone: model.DefaultTimezone, JitterMins: 45},
			Push:       model.PushConfig{Channels: []model.PushChannel{}},
			Shop:       model.ShopConfig{Enabled: true, RetrySeconds: 20, RetryInterval: 0.4, Push: true, Plans: []model.ExchangePlan{}},
		},
		Users:            []model.User{},
		Sessions:         map[string]model.Session{},
		Logs:             []model.LogEntry{},
		InviteCodes:      []model.InviteCode{},
		RegistrationMode: "review",
		UserPush:         map[string]model.PushConfig{},
		UserCaptcha:      map[string]model.UserCaptchaConfig{},
		PushDeliveries:   []model.PushDelivery{},
	}
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) && !s.readOnly {
		return s.saveLocked()
	}
	if err != nil {
		return fmt.Errorf("read state: %w", err)
	}
	raw, err = s.decodeState(raw)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		return fmt.Errorf("parse state: %w", err)
	}
	if s.data.Sessions == nil {
		s.data.Sessions = map[string]model.Session{}
	}
	if s.data.Logs == nil {
		s.data.Logs = []model.LogEntry{}
	}
	if s.data.InviteCodes == nil {
		s.data.InviteCodes = []model.InviteCode{}
	}
	if s.data.RegistrationMode == "" {
		s.data.RegistrationMode = "review"
	}
	normalizeState(&s.data)
	if s.readOnly {
		s.committed = clone(s.data)
		return nil
	}
	recoverExchangePlans(&s.data)
	s.committed = clone(s.data)
	return s.saveLocked()
}

func normalizeState(state *model.State) {
	if state.CaptchaActivity == nil {
		state.CaptchaActivity = map[string][]model.CaptchaAttempt{}
	}
	if state.UserCaptcha == nil {
		state.UserCaptcha = map[string]model.UserCaptchaConfig{}
	}
	if state.UserPush == nil {
		state.UserPush = map[string]model.PushConfig{}
	}
	if state.PushDeliveries == nil {
		state.PushDeliveries = []model.PushDelivery{}
	}
	for userID, config := range state.UserPush {
		normalizePush(&config)
		state.UserPush[userID] = config
	}
	if state.Config.Schedule.Timezone == "" {
		state.Config.Schedule.Timezone = model.DefaultTimezone
	}
	if state.Config.Accounts == nil {
		state.Config.Accounts = []model.Account{}
	}
	if state.Config.Games.Enabled == nil {
		state.Config.Games.Enabled = []string{}
	}
	if state.Config.Games.Blacklist == nil {
		state.Config.Games.Blacklist = map[string][]string{}
	}
	if state.Config.CloudGames.Enabled == nil {
		state.Config.CloudGames.Enabled = []string{}
	}
	if state.Config.BBS.Forums == nil {
		state.Config.BBS.Forums = []int{}
	}
	if state.Config.BBS.DelaySeconds == nil {
		state.Config.BBS.DelaySeconds = []int{}
	}
	for i := range state.Config.Accounts {
		if state.Config.Accounts[i].TaskSettings == nil {
			state.Config.Accounts[i].TaskSettings = tasksFromConfig(state.Config)
		}
	}
	if state.Config.Captcha.Channels == nil {
		state.Config.Captcha.Channels = []model.CaptchaChannel{}
	}
	for i := range state.Config.Captcha.Channels {
		ch := &state.Config.Captcha.Channels[i]
		if ch.ID == "" {
			ch.ID = randomID("captcha_")
		}
		ch.Configured, ch.ClearToken = nil, false
	}
	if state.Config.Push.Channels == nil {
		state.Config.Push.Channels = []model.PushChannel{}
	}
	if state.Config.Shop.Plans == nil {
		state.Config.Shop.Plans = []model.ExchangePlan{}
	}
	if state.Users == nil {
		state.Users = []model.User{}
	}
	if state.InviteCodes == nil {
		state.InviteCodes = []model.InviteCode{}
	}
	if state.RegistrationMode == "" {
		state.RegistrationMode = "review"
	}
}

func (s *Store) saveLocked() (err error) {
	if s.readOnly {
		s.data = clone(s.committed)
		return errors.New("只读状态不能修改")
	}
	defer func() {
		if err != nil {
			s.data = clone(s.committed)
		}
	}()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	raw, err = s.encodeState(raw)
	if err != nil {
		return err
	}
	if err := atomicStateWrite(s.path, raw); err != nil {
		return err
	}
	s.committed = clone(s.data)
	return nil
}

func (s *Store) Config() model.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.data.Config)
}

func (s *Store) ReplaceConfig(config model.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	config = clone(config)
	preserveConfigSecrets(&s.data.Config, &config)
	s.data.Config = config
	normalizeState(&s.data)
	return s.saveLocked()
}

// preserveConfigSecrets keeps credentials that are intentionally redacted by
// the public API when a client submits an edited configuration.
func preserveConfigSecrets(old, next *model.Config) {
	for index := range next.Accounts {
		account := &next.Accounts[index]
		var previous *model.Account
		for oldIndex := range old.Accounts {
			candidate := &old.Accounts[oldIndex]
			if (account.ID != "" && candidate.ID == account.ID) || (account.ID == "" && account.Name != "" && candidate.Name == account.Name) {
				previous = candidate
				break
			}
		}
		if previous == nil {
			continue
		}
		if account.UserID == "" {
			account.UserID = previous.UserID
		}
		if account.Cookie == "" {
			account.Cookie = previous.Cookie
		}
		if account.Stuid == "" {
			account.Stuid = previous.Stuid
		}
		if account.Stoken == "" {
			account.Stoken = previous.Stoken
		}
		if account.Mid == "" {
			account.Mid = previous.Mid
		}
		if account.CloudTokens == nil && previous.CloudTokens != nil {
			account.CloudTokens = map[string]string{}
			for key, value := range previous.CloudTokens {
				account.CloudTokens[key] = value
			}
		}
	}
	for index := range next.Push.Channels {
		channel := &next.Push.Channels[index]
		var previous *model.PushChannel
		if index < len(old.Push.Channels) && old.Push.Channels[index].Provider == channel.Provider {
			previous = &old.Push.Channels[index]

		}
		if previous == nil {
			continue
		}
		if channel.Token == "" {
			channel.Token = previous.Token
		}
		if channel.Secret == "" {
			channel.Secret = previous.Secret
		}
		if channel.ClientSecret == "" {
			channel.ClientSecret = previous.ClientSecret
		}
	}
	for index := range next.Captcha.Channels {
		channel := &next.Captcha.Channels[index]
		if channel.ClearToken {
			channel.Token = ""
		}
		for _, previous := range old.Captcha.Channels {
			// Never match by position: reordering/deleting channels must not
			// move a credential to a different service.
			if channel.ID == "" || channel.ID != previous.ID || channel.Provider != previous.Provider {
				continue
			}
			if channel.UserKey == "" {
				channel.UserKey = previous.UserKey
			}
			if channel.ClearToken {
				channel.Token = ""
			} else if channel.Token == "" && strings.TrimSpace(channel.Endpoint) == previous.Endpoint {
				channel.Token = previous.Token
			}
			break
		}
	}
	if next.Device.ID == "" {
		next.Device.ID = old.Device.ID
	}
	if next.Device.FP == "" {
		next.Device.FP = old.Device.FP
	}
}

func (s *Store) AddAccount(account model.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addAccountLocked(account, "")
}

// AddAccountForUser adds or updates an account within one user's namespace.
// Account names are unique per user, and an account ID can never be claimed by
// another user.
func (s *Store) AddAccountForUser(userID string, account model.Account) error {
	if strings.TrimSpace(userID) == "" {
		return errors.New("账号缺少所属用户")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if index := s.userIndexLocked(userID); index < 0 || s.data.Users[index].Status != "active" {
		return errors.New("用户未激活或已删除")
	}
	account.UserID = userID
	return s.addAccountLocked(account, userID)
}

func (s *Store) AccountsForUser(userID string, admin bool) []model.Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	accounts := make([]model.Account, 0)
	for _, account := range s.data.Config.Accounts {
		if (admin && userID == "") || account.UserID == userID {
			accounts = append(accounts, clone(account))
		}
	}
	return accounts
}

func (s *Store) AccountForUser(userID string, admin bool, id string) (model.Account, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, account := range s.data.Config.Accounts {
		if account.ID == id && ((admin && userID == "") || account.UserID == userID) {
			return clone(account), true
		}
	}
	return model.Account{}, false
}

func (s *Store) HasAdmin() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, user := range s.data.Users {
		if user.Role == "admin" && user.Status == "active" {
			return true
		}
	}
	return false
}

func (s *Store) CreateAdmin(username, password string) (model.User, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.data.Users {
		if existing.Role == "admin" {
			return model.User{}, "", errors.New("系统已初始化管理员")
		}
	}
	user, err := newUser(username, password, "admin", "active")
	if err != nil {
		return model.User{}, "", err
	}
	s.data.Users = append(s.data.Users, user)
	for index := range s.data.Config.Accounts {
		if s.data.Config.Accounts[index].UserID == "" {
			s.data.Config.Accounts[index].UserID = user.ID
		}
	}
	token := s.createSessionLocked(user.ID)
	return publicUser(user), token, s.saveLocked()
}

func (s *Store) Register(username, password string) (model.User, string, error) {
	return s.RegisterWithInvite(username, password, "")
}

func (s *Store) RegisterWithInvite(username, password, inviteCode string) (model.User, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeAdminCountLocked() == 0 {
		return model.User{}, "", errors.New("请先初始化管理员")
	}
	username = strings.TrimSpace(username)
	inviteCode = strings.TrimSpace(inviteCode)
	for _, item := range s.data.Users {
		if strings.EqualFold(item.Username, username) {
			return model.User{}, "", errors.New("用户名已存在")
		}
	}
	status := "active"
	if inviteCode == "" {
		switch s.data.RegistrationMode {
		case "closed":
			return model.User{}, "", errors.New("仅允许使用邀请码注册")
		case "review", "":
			status = "pending"
		case "open":
		default:
			return model.User{}, "", errors.New("注册模式无效")
		}
	}
	user, err := newUser(username, password, "user", status)
	if err != nil {
		return model.User{}, "", err
	}
	before := clone(s.data)
	if inviteCode != "" {
		invite, err := s.consumeInviteCodeLocked(inviteCode)
		if err != nil {
			return model.User{}, "", errors.New("邀请码无效、已过期或已用尽")
		}
		user.Permissions = invite.Permissions
		if invite.UseSiteCaptcha && invite.Permissions.SiteCaptcha {
			if s.data.UserCaptcha == nil {
				s.data.UserCaptcha = map[string]model.UserCaptchaConfig{}
			}
			s.data.UserCaptcha[user.ID] = model.UserCaptchaConfig{Source: "site", Revision: 1, MaxRetries: 3, Channels: []model.CaptchaChannel{}}
		}
	}
	s.data.Users = append(s.data.Users, user)
	token := ""
	if status == "active" {
		token = s.createSessionLocked(user.ID)
	}
	if err := s.saveLocked(); err != nil {
		s.data = before
		return model.User{}, "", err
	}
	return publicUser(user), token, nil
}

func (s *Store) RegistrationMode() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.data.RegistrationMode == "" {
		return "review"
	}
	return s.data.RegistrationMode
}

func (s *Store) SetRegistrationMode(mode string) error {
	if mode != "open" && mode != "review" && mode != "closed" {
		return errors.New("注册模式只能是 open、review 或 closed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.RegistrationMode = mode
	return s.saveLocked()
}

func (s *Store) ListUsers() []model.User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	users := make([]model.User, 0, len(s.data.Users))
	for _, user := range s.data.Users {
		users = append(users, publicUser(user))
	}
	return users
}

func (s *Store) UpdateUserStatus(id, status string) error {
	if status != "active" && status != "pending" && status != "disabled" {
		return errors.New("用户状态只能是 active、pending 或 disabled")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.userIndexLocked(id)
	if index < 0 {
		return errors.New("用户不存在")
	}
	if s.data.Users[index].Role == "admin" && s.data.Users[index].Status == "active" && status != "active" && s.activeAdminCountLocked() <= 1 {
		return errors.New("不能停用最后一个管理员")
	}
	s.data.Users[index].Status = status
	if status != "active" {
		for token, session := range s.data.Sessions {
			if session.UserID == id {
				delete(s.data.Sessions, token)
			}
		}
	}
	return s.saveLocked()
}

func (s *Store) UpdateUserRole(id, role string) error {
	if role != "admin" && role != "user" {
		return errors.New("用户角色只能是 admin 或 user")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.userIndexLocked(id)
	if index < 0 {
		return errors.New("用户不存在")
	}
	if s.data.Users[index].Role == "admin" && role != "admin" && s.activeAdminCountLocked() <= 1 && s.data.Users[index].Status == "active" {
		return errors.New("不能降级最后一个管理员")
	}
	s.data.Users[index].Role = role
	return s.saveLocked()
}

func (s *Store) ResetUserPassword(id, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.userIndexLocked(id)
	if index < 0 {
		return errors.New("用户不存在")
	}
	s.data.Users[index].PasswordHash = hashPassword(password)
	s.revokeSessionsLocked(id)
	return s.saveLocked()
}

func (s *Store) DeleteUser(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.userIndexLocked(id)
	if index < 0 {
		return errors.New("用户不存在")
	}
	if s.data.Users[index].Role == "admin" && s.data.Users[index].Status == "active" && s.activeAdminCountLocked() <= 1 {
		return errors.New("不能删除最后一个管理员")
	}
	for _, p := range s.data.Config.Shop.Plans {
		if p.State == "running" {
			for _, a := range s.data.Config.Accounts {
				if a.UserID == id && a.ID == p.AccountID {
					return errors.New("用户有兑换正在执行，请先停用并等待结果")
				}
			}
		}
	}
	s.data.Users = append(s.data.Users[:index], s.data.Users[index+1:]...)
	delete(s.data.UserPush, id)
	delete(s.data.UserCaptcha, id)
	delete(s.data.CaptchaActivity, id)
	delete(s.data.CaptchaProbes, id)
	deliveries := make([]model.PushDelivery, 0)
	for _, delivery := range s.data.PushDeliveries {
		if delivery.UserID != id {
			deliveries = append(deliveries, delivery)
		}
	}
	s.data.PushDeliveries = deliveries
	for token, session := range s.data.Sessions {
		if session.UserID == id {
			delete(s.data.Sessions, token)
		}
	}
	removed := map[string]bool{}
	accounts := make([]model.Account, 0)
	for _, a := range s.data.Config.Accounts {
		if a.UserID == id {
			removed[a.ID] = true
		} else {
			accounts = append(accounts, a)
		}
	}
	s.data.Config.Accounts = accounts
	plans := make([]model.ExchangePlan, 0)
	for _, p := range s.data.Config.Shop.Plans {
		if !removed[p.AccountID] {
			plans = append(plans, p)
		}
	}
	s.data.Config.Shop.Plans = plans
	logs := make([]model.LogEntry, 0)
	for _, entry := range s.data.Logs {
		if entry.UserID != id {
			logs = append(logs, entry)
		}
	}
	s.data.Logs = logs
	return s.saveLocked()
}

func (s *Store) userIndexLocked(id string) int {
	for index, user := range s.data.Users {
		if user.ID == id {
			return index
		}
	}
	return -1
}

func (s *Store) activeAdminCountLocked() int {
	count := 0
	for _, user := range s.data.Users {
		if user.Role == "admin" && user.Status == "active" {
			count++
		}
	}
	return count
}

type InviteGrant struct {
	Permissions    model.UserPermissions
	UseSiteCaptcha bool
}

func (s *Store) CreateInviteCode(code string, expiresAt time.Time, maxUses int, note, createdBy string, grants ...InviteGrant) (model.InviteCode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	creator := s.userIndexLocked(createdBy)
	if creator < 0 || !isAdmin(s.data.Users[creator]) {
		return model.InviteCode{}, errors.New("只有在职管理员可以创建邀请码")
	}
	grant := InviteGrant{}
	if len(grants) > 0 {
		grant = grants[0]
	}
	if grant.UseSiteCaptcha && !grant.Permissions.SiteCaptcha {
		return model.InviteCode{}, errors.New("默认使用站点打码前，必须授予站点打码权限")
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		raw := make([]byte, 24)
		if _, err := rand.Read(raw); err != nil {
			return model.InviteCode{}, err
		}
		code = "MYH-" + strings.ToUpper(hex.EncodeToString(raw))
	}
	if !invitePattern.MatchString(code) {
		return model.InviteCode{}, errors.New("邀请码长度需在 4-64 位之间")
	}
	if !expiresAt.IsZero() && !expiresAt.After(time.Now()) {
		return model.InviteCode{}, errors.New("失效时间必须晚于当前时间")
	}
	if len(note) > 512 {
		return model.InviteCode{}, errors.New("备注不能超过 512 字节")
	}
	if maxUses < 0 || maxUses > 100000 {
		return model.InviteCode{}, errors.New("使用次数不能为负数")
	}
	for _, item := range s.data.InviteCodes {
		if strings.EqualFold(item.Code, code) {
			return model.InviteCode{}, errors.New("邀请码已存在")
		}
	}
	item := model.InviteCode{Code: code, CreatedAt: time.Now(), ExpiresAt: expiresAt, MaxUses: maxUses, Note: strings.TrimSpace(note), CreatedBy: createdBy, Permissions: grant.Permissions, UseSiteCaptcha: grant.UseSiteCaptcha}
	s.data.InviteCodes = append(s.data.InviteCodes, item)
	if err := s.saveLocked(); err != nil {
		return model.InviteCode{}, err
	}
	return item, nil
}

func (s *Store) ListInviteCodes() []model.InviteCode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]model.InviteCode, len(s.data.InviteCodes))
	copy(items, s.data.InviteCodes)
	return items
}

func (s *Store) DeleteInviteCode(code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	code = strings.TrimSpace(code)
	for index, item := range s.data.InviteCodes {
		if strings.EqualFold(item.Code, code) {
			s.data.InviteCodes = append(s.data.InviteCodes[:index], s.data.InviteCodes[index+1:]...)
			return s.saveLocked()
		}
	}
	return errors.New("邀请码不存在")
}

func (s *Store) ConsumeInviteCode(code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.consumeInviteCodeLocked(code)
	if err != nil {
		return err
	}
	return s.saveLocked()
}

func (s *Store) consumeInviteCodeLocked(code string) (model.InviteCode, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return model.InviteCode{}, errors.New("邀请码不能为空")
	}
	for index := range s.data.InviteCodes {
		item := &s.data.InviteCodes[index]
		if !strings.EqualFold(item.Code, code) {
			continue
		}
		if item.Disabled {
			return model.InviteCode{}, errors.New("邀请码已失效")
		}
		creator := s.userIndexLocked(item.CreatedBy)
		if creator < 0 || !isAdmin(s.data.Users[creator]) {
			return model.InviteCode{}, errors.New("邀请码创建者已无管理权限")
		}
		if !item.ExpiresAt.IsZero() && !item.ExpiresAt.After(time.Now()) {
			return model.InviteCode{}, errors.New("邀请码已过期")
		}
		if item.MaxUses > 0 && item.UsedCount >= item.MaxUses {
			return model.InviteCode{}, errors.New("邀请码使用次数已达上限")
		}
		item.UsedCount++
		return *item, nil
	}
	return model.InviteCode{}, errors.New("邀请码不存在")
}

func (s *Store) ExchangePlansForUser(userID string, admin bool) []model.ExchangePlan {
	s.mu.RLock()
	defer s.mu.RUnlock()
	accounts := make(map[string]bool)
	for _, account := range s.data.Config.Accounts {
		if (admin && userID == "") || account.UserID == userID {
			accounts[account.ID] = true
		}
	}
	plans := make([]model.ExchangePlan, 0)
	for _, plan := range s.data.Config.Shop.Plans {
		if accounts[plan.AccountID] {
			plans = append(plans, plan)
		}
	}
	return plans
}

func (s *Store) ExchangePlanForUser(userID string, admin bool, id string) (model.ExchangePlan, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, plan := range s.data.Config.Shop.Plans {
		if plan.ID == id && s.accountOwnedLocked(userID, admin, plan.AccountID) {
			return plan, true
		}
	}
	return model.ExchangePlan{}, false
}

func (s *Store) accountOwnedLocked(userID string, admin bool, accountID string) bool {
	for _, account := range s.data.Config.Accounts {
		if account.ID == accountID && ((admin && userID == "") || account.UserID == userID) {
			return true
		}
	}
	return false
}

func (s *Store) Authenticate(username, password string) (model.User, string, error) {
	if len(password) > 128 {
		return model.User{}, "", errors.New("用户名或密码错误")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, user := range s.data.Users {
		if strings.EqualFold(user.Username, strings.TrimSpace(username)) {
			if !verifyPassword(password, user.PasswordHash) {
				return model.User{}, "", errors.New("用户名或密码错误")
			}
			if user.Status != "active" {
				return model.User{}, "", errors.New("用户尚未激活或已停用")
			}
			if !strings.HasPrefix(user.PasswordHash, "pbkdf2-sha256$") {
				s.data.Users[index].PasswordHash = hashPassword(password)
			}
			token := s.createSessionLocked(user.ID)
			return publicUser(user), token, s.saveLocked()
		}
	}
	dummyVerify(password)
	return model.User{}, "", errors.New("用户名或密码错误")
}

func (s *Store) UserBySession(token string) (model.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.data.Sessions[token]
	if !ok || session.ExpiresAt.Before(time.Now()) {
		return model.User{}, false
	}
	for _, user := range s.data.Users {
		if user.ID == session.UserID && user.Status == "active" {
			return publicUser(user), true
		}
	}
	return model.User{}, false
}

func (s *Store) DeleteSession(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data.Sessions, token)
	return s.saveLocked()
}

func (s *Store) AddLog(component, message string) error {
	return s.AddLogForUser("", component, message)
}

func (s *Store) AddLogForUser(userID, component, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if userID != "" && s.userIndexLocked(userID) < 0 {
		return nil
	}
	return s.appendLogLocked(model.LogEntry{At: time.Now(), Component: component, Message: message, UserID: userID})
}

// A display name is not an identity. Correlate only owned account runs, while
// leaving legacy entries untouched and preserving the existing scoped reader.
func (s *Store) AddTaskLogForUser(userID, accountID, runID, component, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if userID == "" || runID == "" || len(runID) > 96 || s.userIndexLocked(userID) < 0 || !s.accountOwnedLocked(userID, false, accountID) {
		return errors.New("任务日志归属或执行编号无效")
	}
	return s.appendLogLocked(model.LogEntry{At: time.Now(), Component: component, Message: message, UserID: userID, AccountID: accountID, RunID: runID})
}

func (s *Store) appendLogLocked(entry model.LogEntry) error {
	s.data.Logs = append(s.data.Logs, entry)
	if len(s.data.Logs) > 500 {
		s.data.Logs = s.data.Logs[len(s.data.Logs)-500:]
	}
	return s.saveLocked()
}

func (s *Store) Logs() []model.LogEntry {
	return s.LogsForUser("", true)
}

func (s *Store) LogsForUser(userID string, admin bool) []model.LogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]model.LogEntry, 0, len(s.data.Logs))
	for _, entry := range s.data.Logs {
		if (admin && (userID == "" || entry.UserID == "")) || entry.UserID == userID {
			result = append(result, entry)
		}
	}
	return result
}

func (s *Store) createSessionLocked(userID string) string {
	for token, session := range s.data.Sessions {
		if !session.ExpiresAt.After(time.Now()) {
			delete(s.data.Sessions, token)
		}
	}
	count := 0
	oldestToken := ""
	var oldest time.Time
	for token, session := range s.data.Sessions {
		if session.UserID == userID {
			count++
			if oldest.IsZero() || session.ExpiresAt.Before(oldest) {
				oldest = session.ExpiresAt
				oldestToken = token
			}
		}
	}
	if count >= 20 {
		delete(s.data.Sessions, oldestToken)
	}
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	token := hex.EncodeToString(raw)
	s.data.Sessions[token] = model.Session{UserID: userID, ExpiresAt: time.Now().Add(7 * 24 * time.Hour)}
	return token
}

func newUser(username, password, role, status string) (model.User, error) {
	if err := validateUser(username, password); err != nil {
		return model.User{}, err
	}
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return model.User{}, err
	}
	return model.User{ID: "usr_" + hex.EncodeToString(raw), Username: strings.TrimSpace(username), PasswordHash: hashPassword(password), Role: role, Status: status, CreatedAt: time.Now(), OnboardingStatus: "pending"}, nil
}

func publicUser(user model.User) model.User { user.PasswordHash = ""; return user }
