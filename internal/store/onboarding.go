package store

import (
	"errors"
	"github.com/eleost04/miyohub/internal/model"
)

// Existing users with an empty status are not forced through a migration guide.
// Only the signed-in user's dismissal/completion can be changed by this path.
func (s *Store) UpdateOnboarding(userID, status string) (model.User, error) {
	if status != "dismissed" && status != "complete" {
		return model.User{}, errors.New("引导状态无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.userIndexLocked(userID)
	if i < 0 || s.data.Users[i].Status != "active" {
		return model.User{}, errors.New("用户不存在或已停用")
	}
	s.data.Users[i].OnboardingStatus = status
	user := publicUser(s.data.Users[i])
	if err := s.saveLocked(); err != nil {
		return model.User{}, err
	}
	return user, nil
}
