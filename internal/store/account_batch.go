package store

import (
	"errors"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/eleost04/miyohub/internal/model"
)

// Batch edits are one transaction. Owner-local labels do not grant access or
// introduce another scheduling or parallelism layer.
type AccountBatchPatch struct {
	AccountIDs    []string               `json:"account_ids"`
	Group         *string                `json:"group,omitempty"`
	Disabled      *bool                  `json:"disabled,omitempty"`
	Automatic     *bool                  `json:"automatic,omitempty"`
	Schedule      *model.AccountSchedule `json:"schedule,omitempty"`
	FollowDefault bool                   `json:"follow_default,omitempty"`
	Revisions     map[string]int         `json:"revisions,omitempty"`
}

func validateAccountGroup(group string) error {
	if utf8.RuneCountInString(group) > 32 || strings.TrimSpace(group) != group {
		return errors.New("分组名称最多 32 个字符，不能以空格开头或结尾")
	}
	for _, ch := range group {
		if unicode.IsControl(ch) || !unicode.IsPrint(ch) {
			return errors.New("分组名称包含非法字符")
		}
	}
	return nil
}

func (s *Store) BatchUpdateAccounts(userID string, patch AccountBatchPatch) ([]model.Account, error) {
	if len(patch.AccountIDs) < 1 || len(patch.AccountIDs) > 50 {
		return nil, errors.New("每次请选择 1–50 个账号")
	}
	scheduleEdit := patch.Schedule != nil || patch.FollowDefault || patch.Automatic != nil
	if patch.Group == nil && patch.Disabled == nil && !scheduleEdit {
		return nil, errors.New("请选择批量操作")
	}
	if patch.Schedule != nil && patch.FollowDefault {
		return nil, errors.New("个人时间与站点默认时间不能同时选择")
	}
	if patch.Schedule != nil {
		if err := model.ValidateAccountSchedule(*patch.Schedule); err != nil {
			return nil, err
		}
	}
	group := ""
	if patch.Group != nil {
		group = strings.TrimSpace(*patch.Group)
		if err := validateAccountGroup(group); err != nil {
			return nil, err
		}
	}
	selected := map[string]bool{}
	for _, id := range patch.AccountIDs {
		if id == "" || selected[id] {
			return nil, errors.New("账号选择为空或重复")
		}
		selected[id] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if index := s.userIndexLocked(userID); index < 0 || s.data.Users[index].Status != "active" {
		return nil, errors.New("用户已停用")
	}
	next := clone(s.data.Config.Accounts)
	result := []model.Account{}
	for i, a := range next {
		if !selected[a.ID] || a.UserID != userID {
			continue
		}
		if scheduleEdit && (a.TaskSettings == nil || patch.Revisions[a.ID] != a.TaskSettings.Revision) {
			return nil, errors.New("所选账号的签到设置已变化，请关闭弹窗后重新选择")
		}
		if patch.Group != nil {
			a.Group = group
		}
		if patch.Disabled != nil {
			a.Disabled = *patch.Disabled
		}
		if scheduleEdit {
			before := clone(*a.TaskSettings)
			if patch.Automatic != nil {
				a.TaskSettings.Automatic = *patch.Automatic
			}
			if patch.Schedule != nil {
				value := *patch.Schedule
				a.TaskSettings.Schedule = &value
			} else if patch.FollowDefault {
				a.TaskSettings.Schedule = nil
			}
			if !reflect.DeepEqual(before, *a.TaskSettings) {
				a.TaskSettings.Revision++
			}
		}
		next[i] = a
		result = append(result, a)
	}
	if len(result) != len(selected) {
		return nil, errors.New("所选账号已删除或无权访问；未修改任何账号")
	}
	if reflect.DeepEqual(next, s.data.Config.Accounts) {
		return clone(result), nil
	}
	s.data.Config.Accounts = next
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return clone(result), nil
}
