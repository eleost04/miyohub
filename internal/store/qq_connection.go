package store

import (
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

// Runtime state never changes the user's notification preferences/revision.
// Credentials and ownership are checked again so a removed/rebound bot cannot
// receive a late status update from an old gateway worker.
func (s *Store) UpdateQQConnection(userID string, expected model.PushChannel, state, detail string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.userIndexLocked(userID); i < 0 || s.data.Users[i].Status != "active" {
		return false
	}
	switch state {
	case "connecting", "ready", "reconnecting", "disconnected", "error":
	default:
		return false
	}
	p := s.pushConfigLocked(userID)
	for i, channel := range p.Channels {
		if channel.ID != expected.ID || channel.Provider != "qqbot" || channel.AppID != expected.AppID || channel.ClientSecret != expected.ClientSecret {
			continue
		}
		if channel.BindingState == state && channel.BindingError == detail {
			return true
		}
		channel.BindingState, channel.BindingError = state, detail
		p.Channels[i] = channel
		s.data.UserPush[userID] = p
		message := detail
		if state == "ready" {
			message = "QQ 机器人已连接官方网关；通知发送仍由个人推送开关控制"
		}
		s.data.Logs = append(s.data.Logs, model.LogEntry{At: time.Now(), Component: "push", UserID: userID, Message: "QQ 官方机器人：" + message})
		if len(s.data.Logs) > 500 {
			s.data.Logs = s.data.Logs[len(s.data.Logs)-500:]
		}
		return s.saveLocked() == nil
	}
	return false
}
