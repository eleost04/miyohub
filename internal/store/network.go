package store

import (
	"strings"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

func (s *Store) NetworkConfig() model.NetworkConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.data.Config.Network)
}

func validateNetwork(next *model.NetworkConfig, previous model.NetworkConfig) error {
	p := &next.Proxy
	p.URL, p.Username = strings.TrimSpace(p.URL), strings.TrimSpace(p.Username)
	if p.ClearPassword {
		p.Password = ""
	} else if p.Password == "" && p.URL == previous.Proxy.URL && p.Username == previous.Proxy.Username {
		p.Password = previous.Proxy.Password
	}
	p.ClearPassword, p.HasPassword = false, false
	return mihoyo.ValidateProxy(*p)
}

func PublicNetwork(n model.NetworkConfig) model.NetworkConfig {
	n.Proxy.HasPassword = n.Proxy.Password != ""
	n.Proxy.Password, n.Proxy.ClearPassword = "", false
	return n
}
