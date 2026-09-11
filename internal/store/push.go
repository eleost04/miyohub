package store

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/notify"
)

type PushChannelView struct {
	model.PushChannel
	Configured []string `json:"configured"`
}
type PushSettings struct {
	Enabled   bool              `json:"enable"`
	Tasks     bool              `json:"tasks"`
	Exchange  bool              `json:"exchange"`
	ErrorOnly bool              `json:"error_only"`
	Revision  int               `json:"revision"`
	Channels  []PushChannelView `json:"channels"`
}
type PushChannelPatch struct {
	model.PushChannel
	ClearFields []string `json:"clear_fields"`
}
type PushSettingsPatch struct {
	Enabled   bool               `json:"enable"`
	Tasks     bool               `json:"tasks"`
	Exchange  bool               `json:"exchange"`
	ErrorOnly bool               `json:"error_only"`
	Revision  int                `json:"revision"`
	Channels  []PushChannelPatch `json:"channels"`
}

var ErrPushConflict = errors.New("推送配置已被修改，请重新加载后再保存或测试")

func (s *Store) pushConfigLocked(userID string) model.PushConfig {
	if p, ok := s.data.UserPush[userID]; ok {
		return clone(p)
	}
	return model.PushConfig{Revision: 1, Tasks: true, Exchange: true, Channels: []model.PushChannel{}}
}

func (s *Store) PushConfigForUser(userID string) model.PushConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pushConfigLocked(userID)
}

// PrivatePushConfigs is for background workers only; never serialize it to an API.
func (s *Store) PrivatePushConfigs() map[string]model.PushConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := map[string]model.PushConfig{}
	for _, user := range s.data.Users {
		if user.Status == "active" {
			result[user.ID] = s.pushConfigLocked(user.ID)
		}
	}
	return result
}

func (s *Store) BindPushChannel(userID, channelID string, revision int, channel model.PushChannel) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.userIndexLocked(userID); i < 0 || s.data.Users[i].Status != "active" {
		return "", errors.New("用户未激活或已删除")
	}
	p := s.pushConfigLocked(userID)
	if p.Revision != revision {
		return "", ErrPushConflict
	}
	index := -1
	for i, existing := range p.Channels {
		if existing.ID == channelID && existing.Provider == channel.Provider {
			index = i
			break
		}
	}
	if channelID != "" && index < 0 {
		return "", errors.New("绑定渠道已删除或类型已变化")
	}
	if index < 0 && len(p.Channels) >= 10 {
		return "", errors.New("最多配置 10 个渠道")
	}
	for otherUser, config := range s.data.UserPush {
		for _, existing := range config.Channels {
			if otherUser == userID && existing.ID == channelID {
				continue
			}
			if existing.Provider == channel.Provider && (channel.Provider == "qqbot" && existing.AppID == channel.AppID || channel.Provider == "wechat_claw" && existing.BotID == channel.BotID) {
				return "", errors.New("此机器人已绑定到一个渠道，请使用原渠道重新绑定或先解绑")
			}
		}
	}
	if channel.Name == "" {
		channel.Name = notify.ProviderName(channel.Provider)
	}
	channel.Enabled = channel.OpenID != ""
	if err := notify.ValidateChannel(channel, channel.Enabled); err != nil {
		return "", err
	}
	if index >= 0 {
		channel.ID, channel.Name = p.Channels[index].ID, p.Channels[index].Name
		p.Channels[index] = channel
	} else {
		channel.ID = randomID("push_")
		p.Channels = append(p.Channels, channel)
	}
	p.Revision++
	s.data.UserPush[userID] = p
	if err := s.saveLocked(); err != nil {
		return "", err
	}
	return channel.ID, nil
}

func (s *Store) UpdateWeixinSession(userID string, expected model.PushChannel, cursor, contextToken, state, detail string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.userIndexLocked(userID); i < 0 || s.data.Users[i].Status != "active" {
		return false
	}
	p := s.pushConfigLocked(userID)
	for i, channel := range p.Channels {
		if channel.ID != expected.ID || !channel.Enabled || channel.Token != expected.Token || channel.OpenID != expected.OpenID || channel.APIURL != expected.APIURL {
			continue
		}
		if cursor == channel.SyncCursor && (contextToken == "" || contextToken == channel.ContextToken) && state == channel.BindingState && detail == channel.BindingError {
			return true
		}
		if state != channel.BindingState || detail != channel.BindingError {
			message := detail
			if state == "ready" {
				message = "微信通知会话已就绪"
			}
			if state == "waiting_message" {
				message = "微信接收连接已启动，请向机器人发送一条消息以建立通知会话"
			}
			s.data.Logs = append(s.data.Logs, model.LogEntry{At: time.Now(), Component: "push", UserID: userID, Message: channel.Name + "：" + message})
			if len(s.data.Logs) > 500 {
				s.data.Logs = s.data.Logs[len(s.data.Logs)-500:]
			}
		}
		channel.SyncCursor = cursor
		if contextToken != "" {
			channel.ContextToken = contextToken
		}
		channel.BindingState = state
		channel.BindingError = detail
		p.Channels[i] = channel
		s.data.UserPush[userID] = p
		return s.saveLocked() == nil
	}
	return false
}

func normalizePush(p *model.PushConfig) {
	if p.Revision < 1 {
		p.Revision = 1
	}
	if p.Channels == nil {
		p.Channels = []model.PushChannel{}
	}
	seen := map[string]bool{}
	for i := range p.Channels {
		c := &p.Channels[i]
		c.Provider = strings.ToLower(strings.TrimSpace(c.Provider))
		if c.ID == "" || seen[c.ID] {
			c.ID = randomID("push_")
		}
		seen[c.ID] = true
		if c.Name == "" {
			c.Name = notify.ProviderName(c.Provider)
		}
		if c.Provider == "pushplus" && c.Topic == "" {
			c.Topic, c.Secret = c.Secret, ""
		}
		if c.Provider == "email" && c.SMTPPort == 0 {
			c.SMTPPort = 465
			c.SMTPSSL = true
		}
		if c.Mode != "ilink" && c.Webhook == "" && (c.Provider == "webhook" || c.Provider == "wechat_claw" || c.Provider == "feishubot") {
			c.Webhook, c.APIURL = c.APIURL, ""
		}
	}
}

func (s *Store) PushSettings(userID string) PushSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pushSettingsLocked(userID)
}
func (s *Store) pushSettingsLocked(userID string) PushSettings {
	p := s.pushConfigLocked(userID)
	result := PushSettings{Enabled: p.Enabled, Tasks: p.Tasks, Exchange: p.Exchange, ErrorOnly: p.ErrorOnly, Revision: p.Revision, Channels: []PushChannelView{}}
	for _, c := range p.Channels {
		view := PushChannelView{PushChannel: c, Configured: []string{}}
		for key, value := range notify.SecretFields(&view.PushChannel) {
			if *value != "" {
				view.Configured = append(view.Configured, key)
			}
			*value = ""
		}
		sort.Strings(view.Configured)
		result.Channels = append(result.Channels, view)
	}
	return result
}

func (s *Store) UpdatePushSettings(userID string, p PushSettingsPatch) (PushSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index := s.userIndexLocked(userID); index < 0 || s.data.Users[index].Status != "active" {
		return PushSettings{}, errors.New("用户未激活或已删除")
	}
	oldConfig := s.pushConfigLocked(userID)
	if p.Revision != oldConfig.Revision {
		return PushSettings{}, ErrPushConflict
	}
	if len(p.Channels) > 10 {
		return PushSettings{}, errors.New("最多配置 10 个推送渠道")
	}
	next := model.PushConfig{Enabled: p.Enabled, Tasks: p.Tasks, Exchange: p.Exchange, ErrorOnly: p.ErrorOnly, Revision: p.Revision + 1, Channels: []model.PushChannel{}}
	seen := map[string]bool{}
	for _, patch := range p.Channels {
		c := patch.PushChannel
		c.ContextToken, c.SyncCursor, c.BindingState, c.BindingError = "", "", "", ""
		c.Name = strings.TrimSpace(c.Name)
		c.Provider = strings.ToLower(strings.TrimSpace(c.Provider))
		if c.ID == "" {
			c.ID = randomID("push_")
		} else {
			var old *model.PushChannel
			for _, existing := range oldConfig.Channels {
				if existing.ID == c.ID {
					copy := existing
					old = &copy
					break
				}
			}
			if old == nil || old.Provider != c.Provider {
				return PushSettings{}, errors.New("渠道不存在或类型已变化；更换渠道类型请删除后新建")
			}
			c.ContextToken, c.SyncCursor, c.BindingState = old.ContextToken, old.SyncCursor, old.BindingState
			c.BindingError = old.BindingError
			if c.OpenID != old.OpenID || c.BotID != old.BotID || c.Mode != old.Mode || c.Token != "" && c.Token != old.Token || c.APIURL != "" && c.APIURL != old.APIURL {
				c.ContextToken, c.SyncCursor, c.BindingState, c.BindingError = "", "", "", ""
			}
			previous := notify.SecretFields(old)
			for key, value := range notify.SecretFields(&c) {
				if key == "context_token" || key == "sync_cursor" {
					continue
				}
				if *value == "" {
					*value = *previous[key]
				}
			}
		}
		if seen[c.ID] {
			return PushSettings{}, errors.New("推送渠道重复")
		}
		seen[c.ID] = true
		secrets := notify.SecretFields(&c)
		for _, key := range patch.ClearFields {
			if key == "context_token" || key == "sync_cursor" {
				return PushSettings{}, errors.New("绑定会话由服务器维护，请重新扫码")
			}
			value, ok := secrets[key]
			if !ok {
				return PushSettings{}, errors.New("不能清除未知的密钥字段")
			}
			*value = ""
		}
		if err := notify.ValidateChannel(c, c.Enabled); err != nil {
			return PushSettings{}, err
		}
		next.Channels = append(next.Channels, c)
	}
	if p.Enabled {
		anyEnabled := false
		for _, c := range next.Channels {
			anyEnabled = anyEnabled || c.Enabled
		}
		if !anyEnabled {
			return PushSettings{}, errors.New("开启推送前请至少启用一个渠道")
		}
	}
	s.data.UserPush[userID] = next
	if err := s.saveLocked(); err != nil {
		return PushSettings{}, err
	}
	return s.pushSettingsLocked(userID), nil
}
