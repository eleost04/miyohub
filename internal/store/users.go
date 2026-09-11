package store

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/eleost04/miyohub/internal/model"
)

var invitePattern = regexp.MustCompile(`^[A-Z0-9_-]{4,64}$`)

func validatePassword(password string) error {
	if len(password) < 8 || len(password) > 128 {
		return errors.New("密码应为 8–128 字节")
	}
	return nil
}
func validateUser(username, password string) error {
	username = strings.TrimSpace(username)
	if utf8.RuneCountInString(username) < 1 || utf8.RuneCountInString(username) > 64 {
		return errors.New("用户名应为 1–64 个字符")
	}
	for _, r := range username {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("_.-", r) {
			return errors.New("用户名仅支持文字、数字、下划线、点和连字符")
		}
	}
	return validatePassword(password)
}
func (s *Store) revokeSessionsLocked(id string) {
	for token, session := range s.data.Sessions {
		if session.UserID == id {
			delete(s.data.Sessions, token)
		}
	}
}
func (s *Store) CreateUser(username, password, role string) (model.User, error) {
	if role != "user" && role != "admin" {
		return model.User{}, errors.New("用户角色无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.data.Users {
		if strings.EqualFold(u.Username, strings.TrimSpace(username)) {
			return model.User{}, errors.New("用户名已存在")
		}
	}
	u, err := newUser(username, password, role, "active")
	if err != nil {
		return model.User{}, err
	}
	s.data.Users = append(s.data.Users, u)
	return publicUser(u), s.saveLocked()
}
func (s *Store) DisableInvite(code string, disabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.data.InviteCodes {
		if strings.EqualFold(c.Code, strings.TrimSpace(code)) {
			s.data.InviteCodes[i].Disabled = disabled
			return s.saveLocked()
		}
	}
	return errors.New("邀请码不存在")
}
