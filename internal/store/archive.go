package store

import (
	"errors"
	"strings"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

func (s *Store) ExportAccountArchive(userID, password string) (model.AccountArchive, error) {
	s.mu.RLock()
	index := s.userIndexLocked(userID)
	hash := ""
	if index >= 0 && s.data.Users[index].Status == "active" {
		hash = s.data.Users[index].PasswordHash
	}
	s.mu.RUnlock()
	if hash == "" || !verifyPassword(password, hash) {
		return model.AccountArchive{}, errors.New("站点登录密码错误或用户已停用")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	index = s.userIndexLocked(userID)
	if index < 0 || s.data.Users[index].Status != "active" || s.data.Users[index].PasswordHash != hash {
		return model.AccountArchive{}, errors.New("账号状态已变化，请重新验证")
	}
	archive := model.AccountArchive{Version: 1, CreatedAt: time.Now().UTC(), Accounts: []model.PortableAccount{}}
	for _, a := range s.data.Config.Accounts {
		if a.UserID != userID {
			continue
		}
		p := model.PortableAccount{Name: a.Name, Group: a.Group, UID: a.Stuid, Cookie: a.Cookie, SToken: a.Stoken, Mid: a.Mid, Device: a.Device, CloudTokens: clone(a.CloudTokens)}
		if a.TaskSettings != nil {
			p.TaskSettings = clone(*a.TaskSettings)
		}
		archive.Accounts = append(archive.Accounts, p)
	}
	if _, err := prepareArchive(archive); err != nil {
		return model.AccountArchive{}, err
	}
	return archive, nil
}

func prepareArchive(archive model.AccountArchive) ([]model.Account, error) {
	if archive.Version != 1 || len(archive.Accounts) < 1 || len(archive.Accounts) > 100 {
		return nil, errors.New("迁移文件须包含 1–100 个账号且版本受支持")
	}
	accounts := make([]model.Account, 0, len(archive.Accounts))
	uids, names := map[string]bool{}, map[string]bool{}
	for _, p := range archive.Accounts {
		a := model.Account{Name: strings.TrimSpace(p.Name), Group: strings.TrimSpace(p.Group), Stuid: p.UID, Cookie: p.Cookie, Stoken: p.SToken, Mid: p.Mid, Device: p.Device, CloudTokens: clone(p.CloudTokens), Disabled: true, Status: "unchecked"}
		if err := cookieFields(&a); err != nil {
			return nil, err
		}
		if !uidPattern.MatchString(a.Stuid) {
			return nil, errors.New("迁移账号缺少可识别的米游社 UID，请先检查绑定")
		}
		if err := validateAccount(a); err != nil {
			return nil, err
		}
		for _, value := range []string{a.Device.ID, a.Device.FP, a.Device.Name, a.Device.Model} {
			if len(value) > 128 || strings.ContainsAny(value, "\r\n\x00") {
				return nil, errors.New("设备参数格式无效")
			}
		}
		settings := clone(p.TaskSettings)
		settings.Automatic, settings.Revision = false, 1
		if err := validateTaskSettings(settings); err != nil {
			return nil, err
		}
		a.TaskSettings = &settings
		if uids[a.Stuid] || names[a.Name] {
			return nil, errors.New("迁移文件包含重复的账号 UID 或名称")
		}
		uids[a.Stuid], names[a.Name] = true, true
		accounts = append(accounts, a)
	}
	return accounts, nil
}

func (s *Store) archiveCandidatesLocked(userID string, accounts []model.Account) ([]model.Account, model.ArchivePreview, error) {
	if i := s.userIndexLocked(userID); i < 0 || s.data.Users[i].Status != "active" {
		return nil, model.ArchivePreview{}, errors.New("用户已停用")
	}
	preview := model.ArchivePreview{Names: []string{}}
	selected := []model.Account{}
	for _, a := range accounts {
		duplicate := false
		for _, old := range s.data.Config.Accounts {
			if a.Stuid == old.Stuid || old.UserID == userID && a.Name == old.Name {
				duplicate = true
				break
			}
		}
		if duplicate {
			preview.Skipped++
			continue
		}
		preview.Names = append(preview.Names, a.Name)
		selected = append(selected, a)
	}
	preview.Add = len(selected)
	return selected, preview, nil
}

func (s *Store) PreviewAccountArchive(userID string, archive model.AccountArchive) (model.ArchivePreview, error) {
	accounts, err := prepareArchive(archive)
	if err != nil {
		return model.ArchivePreview{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, preview, err := s.archiveCandidatesLocked(userID, accounts)
	return preview, err
}

func (s *Store) ImportAccountArchive(userID string, archive model.AccountArchive) (model.ArchivePreview, error) {
	accounts, err := prepareArchive(archive)
	if err != nil {
		return model.ArchivePreview{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	accounts, preview, err := s.archiveCandidatesLocked(userID, accounts)
	if err != nil || len(accounts) == 0 {
		return preview, err
	}
	for _, a := range accounts {
		a.ID, a.UserID = randomID("acc_"), userID
		if a.Device.ID == "" {
			a.Device = model.Device{ID: mihoyo.DeviceID(), FP: mihoyo.DeviceFP(), Name: "MiyoHub", Model: "MiyoHub"}
		}
		s.data.Config.Accounts = append(s.data.Config.Accounts, a)
	}
	if err := s.saveLocked(); err != nil {
		return model.ArchivePreview{}, err
	}
	return preview, nil
}
