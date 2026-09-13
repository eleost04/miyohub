package store

import (
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

func tasksFromConfig(cfg model.Config) *model.AccountTaskSettings {
	return &model.AccountTaskSettings{Revision: 1, Automatic: true, Features: cfg.Features,
		Games: clone(cfg.Games), CloudGames: clone(cfg.CloudGames), BBS: clone(cfg.BBS)}
}

func validateTaskSettings(p model.AccountTaskSettings) error {
	if p.Schedule != nil {
		if err := model.ValidateAccountSchedule(*p.Schedule); err != nil {
			return err
		}
	}
	if len(p.Games.Enabled) > len(mihoyo.Games) || len(p.CloudGames.Enabled) > 2 || len(p.BBS.Forums) > len(mihoyo.BBSForums) {
		return errors.New("任务选择数量无效")
	}
	for _, key := range p.Games.Enabled {
		if _, ok := mihoyo.Games[key]; !ok {
			return errors.New("包含不支持的游戏")
		}
	}
	for key, uids := range p.Games.Blacklist {
		if _, ok := mihoyo.Games[key]; !ok {
			return errors.New("UID 排除列表包含不支持的游戏")
		}
		if len(uids) > 100 {
			return errors.New("每个游戏最多排除 100 个 UID")
		}
		for _, uid := range uids {
			if !uidPattern.MatchString(strings.TrimSpace(uid)) {
				return errors.New("排除的角色 UID 应为 1–20 位数字")
			}
		}
	}
	for _, key := range p.CloudGames.Enabled {
		if key != "genshin" && key != "zzz" {
			return errors.New("包含不支持的云游戏")
		}
	}
	for _, id := range p.BBS.Forums {
		if _, ok := mihoyo.BBSForums[id]; !ok {
			return errors.New("包含不支持的社区")
		}
	}
	if p.BBS.PostLimit < 1 || p.BBS.PostLimit > 20 {
		return errors.New("帖子数量应在 1–20 之间")
	}
	if len(p.BBS.DelaySeconds) != 2 || p.BBS.DelaySeconds[0] < 0 || p.BBS.DelaySeconds[1] < p.BBS.DelaySeconds[0] || p.BBS.DelaySeconds[1] > 60 {
		return errors.New("任务间隔应为 0–60 秒内的最小值和最大值")
	}
	return nil
}

// Claim before reserving automatic work. Both the fallback scheduler and
// personal schedules share this durable per-account, per-local-day guard.
func (s *Store) ClaimAutomaticRun(id string, revision int, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accountRunnableLocked(id)
	if !ok || !s.data.Config.Enabled || a.TaskSettings == nil || !a.TaskSettings.Automatic || a.TaskSettings.Revision != revision || a.AutomaticDoneToday(s.data.Config.Schedule, now) {
		return false, nil
	}
	for i := range s.data.Config.Accounts {
		if s.data.Config.Accounts[i].ID != id {
			continue
		}
		previous := s.data.Config.Accounts[i].LastAutomaticAt
		s.data.Config.Accounts[i].LastAutomaticAt = now
		if err := s.saveLocked(); err != nil {
			s.data.Config.Accounts[i].LastAutomaticAt = previous
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (s *Store) UpdateAccountTasks(user model.User, id string, p model.AccountTaskSettings) (model.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.userIndexLocked(user.ID)
	if index < 0 || s.data.Users[index].Status != "active" {
		return model.Account{}, errors.New("用户已停用")
	}
	user = s.data.Users[index]
	for i, a := range s.data.Config.Accounts {
		if a.ID != id || a.UserID != user.ID {
			continue
		}
		if a.TaskSettings == nil || p.Revision != a.TaskSettings.Revision {
			return model.Account{}, errors.New("签到设置已被修改，请刷新账号后重试")
		}
		if err := validateTaskSettings(p); err != nil {
			return model.Account{}, err
		}
		if p.SameWork(a.TaskSettings) && p.Automatic == a.TaskSettings.Automatic && reflect.DeepEqual(p.Schedule, a.TaskSettings.Schedule) {
			return clone(a), nil
		}
		p.Revision++
		p = clone(p)
		if p.Games.Enabled == nil {
			p.Games.Enabled = []string{}
		}
		if p.Games.Blacklist == nil {
			p.Games.Blacklist = map[string][]string{}
		}
		if p.CloudGames.Enabled == nil {
			p.CloudGames.Enabled = []string{}
		}
		if p.BBS.Forums == nil {
			p.BBS.Forums = []int{}
		}
		a.TaskSettings = &p
		s.data.Config.Accounts[i] = a
		return clone(a), s.saveLocked()
	}
	return model.Account{}, errors.New("账号不存在或无权访问")
}

// ConfigForAccount is the only task-runner configuration source. Global task
// fields remain solely for importing legacy state and no longer override users.
func (s *Store) ConfigForAccount(id string) (model.Config, bool) {
	s.mu.RLock()
	a, ok := s.accountRunnableLocked(id)
	if !ok {
		s.mu.RUnlock()
		return model.Config{}, false
	}
	cfg := clone(s.data.Config)
	p := a.TaskSettings
	if p == nil {
		p = tasksFromConfig(cfg)
	}
	cfg.Features, cfg.Games, cfg.CloudGames, cfg.BBS = p.Features, clone(p.Games), clone(p.CloudGames), clone(p.BBS)
	if a.Device.ID != "" {
		cfg.Device = a.Device
	}
	s.mu.RUnlock()
	cfg.Captcha = s.CaptchaForUser(a.UserID)
	return cfg, true
}
