package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

const (
	passportAPI     = "https://passport-api.mihoyo.com"
	qrFetchPath     = "/account/ma-cn-passport/app/createQRLogin"
	qrQueryPath     = "/account/ma-cn-passport/app/queryQRLoginStatus"
	ltokenPath      = "/account/auth/api/getLTokenBySToken"
	cookieTokenPath = "/account/auth/api/getCookieAccountInfoBySToken"
)

type QRManager struct {
	store    *store.Store
	client   *mihoyo.Client
	mu       sync.RWMutex
	sessions map[string]*qrSession
	stopped  bool
}

type qrSession struct {
	state  model.LoginState
	cancel context.CancelFunc
}

func NewQRManager(s *store.Store) *QRManager {
	return &QRManager{store: s, client: mihoyo.NewClient(""), sessions: map[string]*qrSession{}}
}
func (m *QRManager) State(userID string) model.LoginState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if session := m.sessions[userID]; session != nil {
		state := session.state
		state.Ticket = ""
		return state
	}
	return model.LoginState{}
}

func (m *QRManager) Start(parent context.Context, userID, accountName string) (model.LoginState, error) {
	return m.StartBinding(parent, userID, accountName, "")
}

func (m *QRManager) StartBinding(parent context.Context, userID, accountName, accountID string) (model.LoginState, error) {
	if userID == "" {
		return model.LoginState{}, errors.New("扫码登录缺少用户身份")
	}
	if accountID != "" {
		a, ok := m.store.AccountForUser(userID, false, accountID)
		if !ok {
			return model.LoginState{}, errors.New("只能重新绑定自己的账号")
		}
		accountName = a.Name
	}
	accountName = strings.TrimSpace(accountName)
	if accountName == "" || utf8.RuneCountInString(accountName) > 64 {
		return model.LoginState{}, errors.New("请输入 1–64 个字符的账号名称")
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	current := &qrSession{state: model.LoginState{Running: true, Status: "starting", Account: accountName, AccountID: accountID, StartedAt: time.Now()}, cancel: cancel}
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		cancel()
		return model.LoginState{}, errors.New("服务正在停止")
	}
	if old := m.sessions[userID]; old != nil && old.state.Running {
		m.mu.Unlock()
		cancel()
		return model.LoginState{}, errors.New("已有扫码登录正在进行")
	}
	m.sessions[userID] = current
	m.mu.Unlock()
	cfg := m.store.Config()
	cfg.Device = model.Device{ID: mihoyo.DeviceID(), FP: mihoyo.DeviceFP(), Name: "MiyoHub", Model: "MiyoHub"}
	var result map[string]any
	err := m.client.JSON(ctx, http.MethodPost, passportAPI+qrFetchPath, nil, map[string]any{}, qrHeaders(cfg), &result)
	if err == nil && retcode(result) != 0 {
		err = errors.New("生成二维码失败: " + text(result["message"], "接口错误"))
	}
	data := dataMap(result["data"])
	qrURL, ticket := text(data["url"], ""), text(data["ticket"], "")
	if err == nil && (qrURL == "" || ticket == "") {
		err = errors.New("二维码接口未返回 url/ticket")
	}
	if err != nil {
		m.finish(userID, current, "error", err)
		cancel()
		return model.LoginState{}, err
	}
	m.mu.Lock()
	if m.sessions[userID] != current || ctx.Err() != nil {
		m.mu.Unlock()
		cancel()
		return model.LoginState{}, errors.New("扫码登录已取消")
	}
	current.state.Status = "waiting"
	current.state.QRURL = qrURL
	current.state.Ticket = ticket
	state := current.state
	state.Ticket = ""
	m.mu.Unlock()
	go m.poll(ctx, userID, cfg, current)
	return state, nil
}

func (m *QRManager) Refresh(ctx context.Context, userID string) (model.LoginState, error) {
	m.mu.Lock()
	current := m.sessions[userID]
	account := "main"
	accountID := ""
	if current != nil {
		account = current.state.Account
		accountID = current.state.AccountID
		if current.cancel != nil {
			current.cancel()
		}
	}
	delete(m.sessions, userID)
	m.mu.Unlock()
	return m.StartBinding(ctx, userID, account, accountID)
}
func (m *QRManager) Cancel(userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if session := m.sessions[userID]; session != nil {
		if session.cancel != nil {
			session.cancel()
		}
		session.cancel = nil
		session.state.Running = false
		session.state.Status = "cancelled"
		session.state.QRURL = ""
		session.state.Ticket = ""
	}
}

func (m *QRManager) poll(ctx context.Context, userID string, cfg model.Config, current *qrSession) {
	m.mu.RLock()
	state := current.state
	m.mu.RUnlock()
	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.finish(userID, current, "timeout", ctx.Err())
			return
		case <-deadline.C:
			m.finish(userID, current, "timeout", errors.New("扫码登录超时"))
			return
		case <-ticker.C:
			var result map[string]any
			if err := m.client.JSON(ctx, http.MethodPost, passportAPI+qrQueryPath, nil, map[string]any{"ticket": state.Ticket}, qrQueryHeaders(cfg, state.Ticket), &result); err != nil {
				m.finish(userID, current, "error", err)
				return
			}
			if retcode(result) != 0 {
				m.finish(userID, current, "error", fmt.Errorf("查询二维码状态失败: %s", text(result["message"], "接口错误")))
				return
			}
			data := dataMap(result["data"])
			status := text(data["status"], "")
			if status == "Expired" || status == "Timeout" || status == "Cancelled" {
				m.finish(userID, current, "timeout", errors.New("二维码已失效，请刷新后重试"))
				return
			}
			m.mu.Lock()
			if session := m.sessions[userID]; session == current && ctx.Err() == nil {
				session.state.Status = status
			}
			m.mu.Unlock()
			if status == "Confirmed" {
				if err := m.complete(ctx, userID, current, cfg, state.Account, data); err != nil {
					m.finish(userID, current, "error", err)
				} else {
					m.finish(userID, current, "success", nil)
				}
				return
			}
		}
	}
}

func (m *QRManager) complete(ctx context.Context, userID string, current *qrSession, cfg model.Config, accountName string, data map[string]any) error {
	user := dataMap(data["user_info"])
	mid, stuid := text(user["mid"], ""), text(user["aid"], "")
	tokens, ok := data["tokens"].([]any)
	if !ok || len(tokens) == 0 {
		return errors.New("扫码结果缺少 token")
	}
	first, ok := tokens[0].(map[string]any)
	if !ok {
		return errors.New("扫码 token 格式错误")
	}
	stoken := text(first["token"], "")
	if mid == "" || stuid == "" || stoken == "" {
		return errors.New("扫码结果缺少 stoken/mid/aid")
	}
	account, err := loginAccount(ctx, m.client, cfg.Device, accountName, stuid, stoken, mid)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[userID] != current || ctx.Err() != nil {
		return errors.New("扫码登录已取消")
	}
	return m.store.BindAccountForUser(userID, current.state.AccountID, account)
}

func (m *QRManager) finish(userID string, current *qrSession, status string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[userID] != current || !current.state.Running {
		return
	}
	current.state.Running = false
	current.state.Status = status
	if err != nil {
		current.state.Error = err.Error()
	}
	current.state.Ticket = ""
	current.state.QRURL = ""
	if current.cancel != nil {
		current.cancel()
		current.cancel = nil
	}
}
func (m *QRManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = true
	for _, session := range m.sessions {
		if session.cancel != nil {
			session.cancel()
		}
		session.state.Running = false
	}
	m.sessions = map[string]*qrSession{}
}
func qrQueryHeaders(cfg model.Config, ticket string) http.Header {
	h := qrHeaders(cfg)
	raw, _ := json.Marshal(map[string]any{"ticket": ticket})
	h.Set("DS", mihoyo.DSX4("", string(raw)))
	return h
}

func qrHeaders(cfg model.Config) http.Header {
	return http.Header{"User-Agent": {"Mozilla/5.0 miHoYoBBS/2.90.1 Capture/2.2.0"}, "Accept": {"*/*"}, "Accept-Language": {"zh-cn"}, "X-Rpc-Client_type": {"3"}, "X-Rpc-App_version": {"2.90.1"}, "X-Rpc-Device_id": {cfg.Device.ID}, "X-Rpc-Device_fp": {cfg.Device.FP}, "X-Rpc-Game_biz": {"bbs_cn"}, "X-Rpc-App_id": {"bll8iq97cem8"}, "X-Rpc-Sdk_version": {"2.90.1"}, "X-Rpc-Device_model": {cfg.Device.Model}, "X-Rpc-Device_name": {cfg.Device.Name}, "X-Rpc-Account_version": {"2.90.1"}, "Ds": {mihoyo.DSX4("", "{}")}, "Content-Type": {"application/json; charset=UTF-8"}}
}

func retcode(value map[string]any) int {
	if n, ok := value["retcode"].(float64); ok {
		return int(n)
	}
	return -1
}
func dataMap(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return map[string]any{}
}
func text(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	result := fmt.Sprint(value)
	if result == "" || result == "<nil>" {
		return fallback
	}
	return result
}
