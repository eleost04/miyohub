package store

import (
	"strings"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/notify"
)

// RedactedLogsForUser never exports configured authentication material, even
// when an upstream error has accidentally echoed a credential into a log.
func (s *Store) RedactedLogsForUser(userID string, admin bool) []model.LogEntry {
	config := s.Config()
	secrets := []string{config.Network.Proxy.URL, config.Network.Proxy.Username, config.Network.Proxy.Password}
	for _, a := range config.Accounts {
		secrets = append(secrets, a.Cookie, a.Stoken, a.Mid)
		for _, part := range strings.Split(a.Cookie, ";") {
			if _, value, ok := strings.Cut(strings.TrimSpace(part), "="); ok {
				secrets = append(secrets, value)
			}
		}
		for _, token := range a.CloudTokens {
			secrets = append(secrets, token)
		}
	}
	for _, c := range config.Push.Channels {
		for _, value := range notify.SecretFields(&c) {
			secrets = append(secrets, *value)
		}
	}
	s.mu.RLock()
	for _, cfg := range s.data.UserCaptcha {
		for _, c := range cfg.Channels {
			secrets = append(secrets, c.UserKey, c.Token)
		}
	}
	for _, cfg := range s.data.UserPush {
		for _, c := range cfg.Channels {
			for _, value := range notify.SecretFields(&c) {
				secrets = append(secrets, *value)
			}
		}
	}
	s.mu.RUnlock()
	for _, c := range config.Captcha.Channels {
		secrets = append(secrets, c.UserKey, c.Token)
	}
	entries := s.LogsForUser(userID, admin)
	for i := range entries {
		entries[i].Message = notify.Redact(entries[i].Message, secrets)
	}
	return entries
}
