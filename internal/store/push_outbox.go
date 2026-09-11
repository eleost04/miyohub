package store

import (
	"errors"
	"time"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/notify"
)

func (s *Store) pushAllowedLocked(userID, accountID string) bool {
	i := s.userIndexLocked(userID)
	if i < 0 || s.data.Users[i].Status != "active" {
		return false
	}
	if accountID == "" {
		return true
	}
	for _, a := range s.data.Config.Accounts {
		if a.ID == accountID {
			return a.UserID == userID && !a.Disabled
		}
	}
	return false
}
func pushEventEnabled(config model.PushConfig, kind string, success bool) bool {
	return config.Enabled && (!config.ErrorOnly || !success) && (kind == notify.TaskEventKind && config.Tasks || kind == notify.ExchangeEventKind && config.Exchange)
}
func (s *Store) QueuePushEvent(event notify.Event) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.pushConfigLocked(event.UserID)
	if !s.pushAllowedLocked(event.UserID, event.AccountID) || !pushEventEnabled(cfg, event.Kind, event.Success) {
		return false, nil
	}
	channels := []model.PushChannel{}
	for _, c := range cfg.Channels {
		if c.Enabled {
			channels = append(channels, c)
		}
	}
	if len(channels) == 0 {
		return false, nil
	}
	active := 0
	for _, entry := range s.data.PushDeliveries {
		if entry.Status == "pending" || entry.Status == "sending" {
			active++
		}
	}
	if active+len(channels) > 500 {
		return false, errors.New("推送队列已满，任务结果仍已保存")
	}
	now := time.Now()
	for _, c := range channels {
		s.data.PushDeliveries = append(s.data.PushDeliveries, model.PushDelivery{ID: randomID("delivery_"), UserID: event.UserID, AccountID: event.AccountID, ChannelID: c.ID, ChannelName: c.Name, Provider: c.Provider, Kind: event.Kind, Revision: cfg.Revision, Title: event.Title, Message: event.Message, Success: event.Success, Status: "pending", CreatedAt: now, UpdatedAt: now})
	}
	s.trimPushLocked()
	return true, s.saveLocked()
}
func (s *Store) trimPushLocked() {
	if len(s.data.PushDeliveries) <= 500 {
		return
	}
	excess := len(s.data.PushDeliveries) - 500
	result := make([]model.PushDelivery, 0, 500)
	for _, entry := range s.data.PushDeliveries {
		if excess > 0 && entry.Status != "pending" && entry.Status != "sending" {
			excess--
			continue
		}
		result = append(result, entry)
	}
	s.data.PushDeliveries = result
}
func (s *Store) ClaimPushDelivery() (model.PushDelivery, model.PushChannel, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for i, entry := range s.data.PushDeliveries {
		if entry.Status != "pending" {
			continue
		}
		cfg := s.pushConfigLocked(entry.UserID)
		valid := s.pushAllowedLocked(entry.UserID, entry.AccountID) && cfg.Revision == entry.Revision && pushEventEnabled(cfg, entry.Kind, entry.Success)
		var selected model.PushChannel
		for _, c := range cfg.Channels {
			if c.ID == entry.ChannelID && c.Enabled {
				selected = c
			}
		}
		if !valid || selected.ID == "" {
			s.data.PushDeliveries[i].Status = "skipped"
			s.data.PushDeliveries[i].Error = "配置、账号或用户状态已变化，未发送"
			s.data.PushDeliveries[i].UpdatedAt = time.Now()
			changed = true
			continue
		}
		s.data.PushDeliveries[i].Status = "sending"
		s.data.PushDeliveries[i].UpdatedAt = time.Now()
		if err := s.saveLocked(); err != nil {
			return model.PushDelivery{}, model.PushChannel{}, false, err
		}
		return entry, selected, true, nil
	}
	if changed {
		if err := s.saveLocked(); err != nil {
			return model.PushDelivery{}, model.PushChannel{}, false, err
		}
	}
	return model.PushDelivery{}, model.PushChannel{}, false, nil
}
func (s *Store) FinishPushDelivery(id string, result notify.Result) error {
	s.mu.Lock()
	userID := ""
	message := ""
	for i, entry := range s.data.PushDeliveries {
		if entry.ID == id && entry.Status == "sending" {
			status := "failed"
			message = result.Error
			if result.Uncertain {
				status = "unknown"
			}
			if result.OK {
				status = "accepted"
				message = "推送服务已接收通知"
			}
			s.data.PushDeliveries[i].Status = status
			s.data.PushDeliveries[i].Error = result.Error
			s.data.PushDeliveries[i].UpdatedAt = time.Now()
			userID = entry.UserID
			message = notify.ProviderName(entry.Provider) + "：" + message
			break
		}
	}
	err := s.saveLocked()
	s.mu.Unlock()
	if err == nil && userID != "" {
		_ = s.AddLogForUser(userID, "push", message)
	}
	return err
}
func (s *Store) RecoverPushDeliveries() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for i := range s.data.PushDeliveries {
		if s.data.PushDeliveries[i].Status == "sending" {
			s.data.PushDeliveries[i].Status = "unknown"
			s.data.PushDeliveries[i].Error = "服务中断，送达状态未确认；为避免重复消息，未自动重发"
			s.data.PushDeliveries[i].UpdatedAt = time.Now()
			changed = true
		}
	}
	if changed {
		return s.saveLocked()
	}
	return nil
}
func (s *Store) PushHistory(userID string) []model.PushDelivery {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []model.PushDelivery{}
	for i := len(s.data.PushDeliveries) - 1; i >= 0 && len(result) < 100; i-- {
		entry := s.data.PushDeliveries[i]
		if entry.UserID != userID {
			continue
		}
		entry.Message = ""
		result = append(result, entry)
	}
	return result
}
func (s *Store) RecordPushTest(userID string, channel model.PushChannel, result notify.Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.pushAllowedLocked(userID, "") {
		return errors.New("用户已停用")
	}
	status := "failed"
	if result.Uncertain {
		status = "unknown"
	}
	if result.OK {
		status = "accepted"
	}
	now := time.Now()
	s.data.PushDeliveries = append(s.data.PushDeliveries, model.PushDelivery{ID: randomID("delivery_"), UserID: userID, ChannelID: channel.ID, ChannelName: channel.Name, Provider: channel.Provider, Kind: "test", Title: "推送测试", Status: status, Error: result.Error, CreatedAt: now, UpdatedAt: now})
	s.trimPushLocked()
	return s.saveLocked()
}
