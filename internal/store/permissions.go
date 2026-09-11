package store

import (
	"errors"

	"github.com/eleost04/miyohub/internal/model"
)

var ErrExchangePermission = errors.New("该账号所属用户尚未获得商品兑换权限，请联系管理员开通")
var ErrSiteCaptchaPermission = errors.New("尚未获得站点打码服务权限，请联系管理员开通，或配置自己的打码服务")

// UpdateUserAccess commits the management dialog atomically: a failed role or
// status change must not leave half-applied grants behind.
func (s *Store) UpdateUserAccess(actor model.User, userID, role, status string, permissions model.UserPermissions) (model.User, model.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fail := func(message string) (model.User, model.User, error) {
		return model.User{}, model.User{}, errors.New(message)
	}
	i := s.userIndexLocked(actor.ID)
	if i < 0 || !isAdmin(s.data.Users[i]) {
		return fail("只有管理员可以管理用户")
	}
	i = s.userIndexLocked(userID)
	if i < 0 {
		return fail("用户不存在")
	}
	before := s.data.Users[i]
	if userID == actor.ID {
		return fail("请由其他管理员调整你的角色、状态或权限")
	}
	if role != "user" && role != "admin" {
		return fail("用户角色无效")
	}
	if status != "active" && status != "pending" && status != "disabled" {
		return fail("用户状态无效")
	}
	if isAdmin(before) && (role != "admin" || status != "active") && s.activeAdminCountLocked() <= 1 {
		return fail("不能停用或降级最后一个管理员")
	}
	after := before
	after.Role, after.Status = role, status
	if role == "user" {
		after.Permissions = permissions
	}
	s.data.Users[i] = after
	if status != "active" {
		s.revokeSessionsLocked(userID)
	}
	err := s.saveLocked()
	return publicUser(before), publicUser(after), err
}

func (s *Store) UpdateUserPermissions(actor model.User, userID string, p model.UserPermissions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.userIndexLocked(actor.ID)
	if i < 0 || !isAdmin(s.data.Users[i]) {
		return errors.New("只有管理员可以分配权限")
	}
	i = s.userIndexLocked(userID)
	if i < 0 {
		return errors.New("用户不存在")
	}
	if s.data.Users[i].Role == "admin" {
		return errors.New("管理员已具备全部服务权限，无需单独授权")
	}
	s.data.Users[i].Permissions = p
	return s.saveLocked()
}

func (s *Store) UserCanExchange(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i := s.userIndexLocked(id)
	return i >= 0 && s.data.Users[i].CanExchange()
}

func (s *Store) accountCanExchangeLocked(id string) bool {
	a, ok := s.accountRunnableLocked(id)
	if !ok {
		return false
	}
	i := s.userIndexLocked(a.UserID)
	return i >= 0 && s.data.Users[i].CanExchange()
}

func (s *Store) AccountCanExchange(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.accountCanExchangeLocked(id)
}

func (s *Store) checkExchangeAccessLocked(userID string, admin bool, accountID string) error {
	if userID != "" {
		i := s.userIndexLocked(userID)
		if i < 0 || !s.data.Users[i].CanExchange() {
			return ErrExchangePermission
		}
		// A caller cannot turn its own admin flag into an ownership bypass.
		admin = admin && isAdmin(s.data.Users[i])
	} else if !admin {
		return ErrExchangePermission
	}
	if !s.accountOwnedLocked(userID, admin, accountID) {
		return errors.New("账号不存在或无权访问")
	}
	if !s.accountCanExchangeLocked(accountID) {
		return ErrExchangePermission
	}
	return nil
}

func (s *Store) CheckExchangeAccess(userID string, admin bool, accountID string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.checkExchangeAccessLocked(userID, admin, accountID)
}
