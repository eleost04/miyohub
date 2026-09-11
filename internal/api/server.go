package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/eleost04/miyohub/internal/auth"
	"github.com/eleost04/miyohub/internal/buildinfo"
	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/notify"
	"github.com/eleost04/miyohub/internal/scheduler"
	"github.com/eleost04/miyohub/internal/shop"
	"github.com/eleost04/miyohub/internal/store"
	"github.com/eleost04/miyohub/internal/tasks"
)

type Server struct {
	options      Options
	limits       rateLimits
	store        *store.Store
	runner       *tasks.Runner
	qr           *auth.QRManager
	sms          *auth.SMSManager
	scheduler    *scheduler.Scheduler
	personal     *scheduler.Personal
	exchange     *shop.Engine
	shopClient   *mihoyo.Client
	pushSender   notify.Deliverer
	push         *notify.Dispatcher
	pushBindings *notify.Bindings
	weixin       *notify.WeixinMonitor
	probes       captchaProbes
}

func NewServer(s *store.Store) *Server { return NewServerWithOptions(s, Options{}) }
func NewServerWithOptions(s *store.Store, options Options) *Server {
	if options.Version == "" {
		options.Version = buildinfo.Version()
	}
	server := &Server{options: options, store: s, runner: tasks.NewRunner(s), qr: auth.NewQRManager(s), sms: auth.NewSMSManager(s), shopClient: mihoyo.NewClient("")}
	server.exchange = shop.NewEngine(s, server.shopClient)
	sender := notify.Sender{HTTP: notify.NewHTTPClient()}
	server.pushSender = sender
	server.push = notify.NewDispatcher(s, sender)
	server.pushBindings = notify.NewBindings(s, sender)
	server.weixin = notify.NewWeixinMonitor(s, sender)
	server.runner.Notify = server.push.Enqueue
	server.exchange.Notify = server.push.Enqueue
	server.scheduler = scheduler.New(s.Config().Schedule, func(ctx context.Context) error {
		return server.runner.RunScheduled(ctx)
	}, func(message string) {
		_ = server.store.AddLog("scheduler", message)
	})
	server.personal = scheduler.NewPersonal(s, func(ctx context.Context, id string) error {
		return server.runner.StartWithOptions(ctx, []string{id}, tasks.RunOptions{Automatic: true})
	})
	return server
}

func (s *Server) Start() error {
	if err := s.options.validate(); err != nil {
		return err
	}
	if err := s.store.RecoverCaptchaProbes(); err != nil {
		return err
	}
	if err := s.scheduler.Start(); err != nil {
		return err
	}
	s.personal.Start()
	if err := s.push.Start(); err != nil {
		s.scheduler.Stop()
		s.personal.Stop()
		return err
	}
	s.weixin.Start()
	s.exchange.Start()
	return nil
}

func (s *Server) Stop() {
	s.personal.Stop()
	s.probes.stop()
	s.runner.Stop()
	s.qr.Stop()
	s.sms.Stop()
	s.scheduler.Stop()
	s.exchange.Stop()
	s.pushBindings.Stop()
	s.weixin.Stop()
	s.push.Stop()
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", s.health)
	mux.HandleFunc("/api/v1/bootstrap", s.bootstrap)
	mux.HandleFunc("/api/v1/auth/status", s.authStatus)
	mux.HandleFunc("/api/v1/auth/setup", s.authSetup)
	mux.HandleFunc("/api/v1/auth/register", s.authRegister)
	mux.HandleFunc("/api/v1/auth/login", s.authLogin)
	mux.HandleFunc("/api/v1/auth/logout", s.authLogout)
	mux.HandleFunc("/api/v1/auth/password", s.withAuth(s.changePassword))
	mux.HandleFunc("/api/v1/auth/me", s.withAuth(s.me))
	mux.HandleFunc("/api/v1/profile/onboarding", s.withAuth(s.onboarding))
	mux.HandleFunc("/api/v1/admin/users", s.withAuth(s.adminUsers))
	mux.HandleFunc("/api/v1/admin/users/status", s.withAuth(s.adminUserStatus))
	mux.HandleFunc("/api/v1/admin/users/role", s.withAuth(s.adminUserRole))
	mux.HandleFunc("/api/v1/admin/users/permissions", s.withAuth(s.adminUserPermissions))
	mux.HandleFunc("/api/v1/admin/users/password", s.withAuth(s.adminUserPassword))
	mux.HandleFunc("/api/v1/admin/users/delete", s.withAuth(s.adminUserDelete))
	mux.HandleFunc("/api/v1/admin/invite-codes", s.withAuth(s.adminInviteCodes))
	mux.HandleFunc("/api/v1/admin/settings", s.withAuth(s.adminSettings))
	mux.HandleFunc("/api/v1/config", s.withAuth(s.config))
	mux.HandleFunc("/api/v1/status", s.withAuth(s.status))
	mux.HandleFunc("/api/v1/run", s.withAuth(s.run))
	mux.HandleFunc("/api/v1/run/cancel", s.withAuth(s.cancelRun))
	mux.HandleFunc("/api/v1/accounts", s.withAuth(s.accounts))
	mux.HandleFunc("/api/v1/accounts/check", s.withAuth(s.accountCheck))
	mux.HandleFunc("/api/v1/accounts/tasks", s.withAuth(s.accountTasks))
	mux.HandleFunc("/api/v1/captcha/config", s.withAuth(s.captchaConfig))
	mux.HandleFunc("/api/v1/captcha/test", s.withAuth(s.captchaTest))
	mux.HandleFunc("/api/v1/push/test", s.withAuth(s.pushTest))
	mux.HandleFunc("/api/v1/push/config", s.withAuth(s.pushConfig))
	mux.HandleFunc("/api/v1/push/history", s.withAuth(s.pushHistory))
	mux.HandleFunc("/api/v1/push/qr", s.withAuth(s.pushQRState))
	mux.HandleFunc("/api/v1/push/qr/start", s.withAuth(s.pushQRStart))
	mux.HandleFunc("/api/v1/push/qr/cancel", s.withAuth(s.pushQRCancel))
	mux.HandleFunc("/api/v1/push/qr/verify", s.withAuth(s.pushQRVerify))
	mux.HandleFunc("/api/v1/login/qr", s.withAuth(s.qrState))
	mux.HandleFunc("/api/v1/login/qr/start", s.withAuth(s.qrStart))
	mux.HandleFunc("/api/v1/login/qr/refresh", s.withAuth(s.qrRefresh))
	mux.HandleFunc("/api/v1/login/qr/cancel", s.withAuth(s.qrCancel))
	mux.HandleFunc("/api/v1/login/sms/send", s.withAuth(s.smsSend))
	mux.HandleFunc("/api/v1/login/sms", s.withAuth(s.smsState))
	mux.HandleFunc("/api/v1/login/sms/captcha", s.withAuth(s.smsCaptcha))
	mux.HandleFunc("/api/v1/login/sms/verify", s.withAuth(s.smsVerify))
	mux.HandleFunc("/api/v1/login/sms/cancel", s.withAuth(s.smsCancel))
	mux.HandleFunc("/api/v1/shop/status", s.withAuth(s.shopStatus))
	mux.HandleFunc("/api/v1/shop/goods", s.withAuth(s.shopGoods))
	mux.HandleFunc("/api/v1/shop/image", s.shopImage)
	mux.HandleFunc("/api/v1/shop/good-detail", s.withAuth(s.shopGoodDetail))
	mux.HandleFunc("/api/v1/shop/device-fp", s.withAuth(s.shopDeviceFP))
	mux.HandleFunc("/api/v1/shop/points", s.withAuth(s.shopPoints))
	mux.HandleFunc("/api/v1/shop/addresses", s.withAuth(s.shopAddresses))
	mux.HandleFunc("/api/v1/shop/roles", s.withAuth(s.shopRoles))
	mux.HandleFunc("/api/v1/shop/plans", s.withAuth(s.shopPlans))
	mux.HandleFunc("/api/v1/shop/plans/run", s.withAuth(s.shopPlanRun))
	mux.HandleFunc("/api/v1/shop/exchange", s.withAuth(s.shopPlanRun))
	mux.HandleFunc("/api/v1/shop/plans/cancel", s.withAuth(s.shopPlanCancel))
	return s.security(mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "miyohub", "version": s.options.Version})
}

func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": model.AuthStatus{NeedAuth: true, HasAdmin: s.store.HasAdmin(), RegistrationMode: s.store.RegistrationMode()}})
}

func (s *Server) authSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	user, token, err := s.store.CreateAdmin(input.Username, input.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.setSession(w, r, token)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]any{"user": user}})
}

func (s *Server) authRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		InviteCode string `json:"invite_code"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	user, token, err := s.store.RegisterWithInvite(input.Username, input.Password, input.InviteCode)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if token != "" {
		s.setSession(w, r, token)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]any{"user": user, "pending": token == ""}})
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	user, token, err := s.store.Authenticate(input.Username, input.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	s.setSession(w, r, token)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]any{"user": user}})
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if token := sessionToken(r); token != "" {
		if user, ok := s.store.UserBySession(token); ok {
			s.qr.Cancel(user.ID)
			s.sms.Cancel(user.ID)
			s.pushBindings.Cancel(user.ID, "")
		}
		if err := s.store.DeleteSession(token); err != nil {
			writeError(w, 500, errors.New("退出失败，请重试"))
			return
		}
	}
	s.clearSession(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) withAuth(next func(http.ResponseWriter, *http.Request, model.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.store.UserBySession(sessionToken(r))
		if !ok {
			writeError(w, http.StatusUnauthorized, errors.New("未登录"))
			return
		}
		next(w, r, user)
	}
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": user})
}

func requireAdmin(w http.ResponseWriter, user model.User) bool {
	if user.Role != "admin" || user.Status != "active" {
		writeError(w, http.StatusForbidden, errors.New("只有管理员可以执行此操作"))
		return false
	}
	return true
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request, user model.User) {
	if !requireAdmin(w, user) {
		return
	}
	if r.Method == http.MethodPut {
		s.adminUserAccess(w, r, user)
		return
	}
	if r.Method == http.MethodPost {
		var input struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Role     string `json:"role"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if input.Role == "" {
			input.Role = "user"
		}
		created, err := s.store.CreateUser(input.Username, input.Password, input.Role)
		if err != nil {
			writeError(w, 400, err)
			return
		}
		writeJSON(w, 201, map[string]any{"ok": true, "data": created})
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]any{"users": s.store.ListUsers(), "registration_mode": s.store.RegistrationMode()}})
}

func (s *Server) adminUserStatus(w http.ResponseWriter, r *http.Request, user model.User) {
	if !requireAdmin(w, user) {
		return
	}
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		UserID string `json:"user_id"`
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.UserID == user.ID {
		writeError(w, http.StatusBadRequest, errors.New("不能修改自己的状态"))
		return
	}
	if err := s.store.UpdateUserStatus(input.UserID, input.Status); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if input.Status != "active" {
		s.cancelUserWork(input.UserID)
	}
	s.exchange.Wake()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) adminUserRole(w http.ResponseWriter, r *http.Request, user model.User) {
	if !requireAdmin(w, user) {
		return
	}
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.UserID == user.ID {
		writeError(w, http.StatusBadRequest, errors.New("不能修改自己的角色"))
		return
	}
	if err := s.store.UpdateUserRole(input.UserID, input.Role); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if input.Role != "admin" {
		s.cancelUserWork(input.UserID)
	}
	s.exchange.Wake()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) adminUserPassword(w http.ResponseWriter, r *http.Request, user model.User) {
	if !requireAdmin(w, user) {
		return
	}
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		UserID   string `json:"user_id"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := s.store.ResetUserPassword(input.UserID, input.Password); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) adminUserDelete(w http.ResponseWriter, r *http.Request, user model.User) {
	if !requireAdmin(w, user) {
		return
	}
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		UserID string `json:"user_id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.UserID == user.ID {
		writeError(w, http.StatusBadRequest, errors.New("不能删除当前管理员账号"))
		return
	}
	s.cancelUserWork(input.UserID)
	if err := s.store.DeleteUser(input.UserID); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) adminInviteCodes(w http.ResponseWriter, r *http.Request, user model.User) {
	if !requireAdmin(w, user) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": s.store.ListInviteCodes()})
	case http.MethodPost:
		var input struct {
			Code           string                 `json:"code"`
			ExpiresAt      *string                `json:"expires_at"`
			MaxUses        *int                   `json:"max_uses"`
			Note           string                 `json:"note"`
			Permissions    *model.UserPermissions `json:"permissions"`
			UseSiteCaptcha *bool                  `json:"use_site_captcha"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if input.Code != "" {
			writeError(w, 400, errors.New("邀请码由服务器随机生成，无需手动填写"))
			return
		}
		expiry := time.Now().Add(7 * 24 * time.Hour).Format(time.RFC3339)
		if input.ExpiresAt != nil {
			expiry = *input.ExpiresAt
		}
		expiresAt, err := parseInviteExpiry(expiry)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		grant := store.InviteGrant{Permissions: model.UserPermissions{Exchange: true, SiteCaptcha: true}, UseSiteCaptcha: true}
		if input.Permissions != nil {
			grant.Permissions = *input.Permissions
			grant.UseSiteCaptcha = grant.Permissions.SiteCaptcha
		}
		if input.UseSiteCaptcha != nil {
			grant.UseSiteCaptcha = *input.UseSiteCaptcha
		}
		maxUses := 1
		if input.MaxUses != nil {
			maxUses = *input.MaxUses
		}
		item, err := s.store.CreateInviteCode("", expiresAt, maxUses, input.Note, user.ID, grant)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": item})
	case http.MethodPut:
		var input struct {
			Code     string `json:"code"`
			Disabled bool   `json:"disabled"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if err := s.store.DisableInvite(input.Code, input.Disabled); err != nil {
			writeError(w, 400, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	case http.MethodDelete:
		var input struct {
			Code string `json:"code"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if err := s.store.DeleteInviteCode(input.Code); err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func parseInviteExpiry(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	location, err := time.LoadLocation(model.DefaultTimezone)
	if err != nil {
		return time.Time{}, err
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", value, location)
	if err != nil {
		return time.Time{}, errors.New("失效时间格式应为 RFC3339 或 YYYY-MM-DD HH:MM:SS")
	}
	return parsed, nil
}

func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request, user model.User) {
	if !requireAdmin(w, user) {
		return
	}
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		RegistrationMode string `json:"registration_mode"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := s.store.SetRegistrationMode(input.RegistrationMode); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]string{"registration_mode": s.store.RegistrationMode()}})
}

func (s *Server) config(w http.ResponseWriter, r *http.Request, user model.User) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": s.visibleConfig(user)})
	case http.MethodPut:
		if user.Role != "admin" {
			writeError(w, http.StatusForbidden, errors.New("只有管理员可以修改全局配置"))
			return
		}
		var patch store.SettingsPatch
		if !decodeJSON(w, r, &patch) {
			return
		}
		if err := s.store.UpdateSettings(patch); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.exchange.Wake()
		cfg := s.store.Config()
		if !cfg.Enabled {
			for _, a := range cfg.Accounts {
				s.runner.CancelAccount(a.ID, "管理员已关闭站点任务服务")
			}
		}
		if !cfg.Shop.Enabled {
			for _, p := range cfg.Shop.Plans {
				s.exchange.Cancel(p.ID)
			}
		}
		if err := s.scheduler.Reload(s.store.Config().Schedule); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": s.visibleConfig(user)})
	default:
		methodNotAllowed(w)
	}
}

func publicConfig(config model.Config) model.Config {
	config.Accounts = append([]model.Account{}, config.Accounts...)
	for index := range config.Accounts {
		config.Accounts[index] = publicAccount(config.Accounts[index])
	}

	config.Device = model.Device{}
	config.Push.Channels = []model.PushChannel{}
	config.Captcha.Channels = append([]model.CaptchaChannel{}, config.Captcha.Channels...)
	for index := range config.Captcha.Channels {
		channel := &config.Captcha.Channels[index]
		channel.Configured = []string{}
		if channel.UserKey != "" {
			channel.Configured = append(channel.Configured, "userkey")
		}
		if channel.Token != "" {
			channel.Configured = append(channel.Configured, "token")
		}
		channel.UserKey, channel.Token, channel.ClearToken = "", "", false
	}
	return config
}

func publicConfigForUser(config model.Config, user model.User) model.Config {
	config = publicConfig(config)
	if user.Role != "admin" {
		config.Captcha = model.CaptchaConfig{Channels: []model.CaptchaChannel{}}
	}
	// Legacy global exclusions are not personal rules and may contain another
	// user's role UIDs. Account-scoped preferences are exposed on each account.
	config.Games.Blacklist = map[string][]string{}
	allowed := map[string]bool{}
	visibleAccounts := make([]model.Account, 0, len(config.Accounts))
	for _, account := range config.Accounts {
		if account.UserID == user.ID {
			visibleAccounts = append(visibleAccounts, account)
			allowed[account.ID] = true
		}
	}
	config.Accounts = visibleAccounts
	visiblePlans := make([]model.ExchangePlan, 0, len(config.Shop.Plans))
	for _, plan := range config.Shop.Plans {
		if allowed[plan.AccountID] {
			visiblePlans = append(visiblePlans, plan)
		}
	}
	config.Shop.Plans = visiblePlans
	return config
}

func (s *Server) status(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	visible := s.visibleConfig(user)
	data := s.runtimeStatus(user)
	data["user"], data["accounts"], data["exchange"] = user, visible.Accounts, visible.Shop
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": data})
}

func (s *Server) run(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	input, ok := s.readTaskSelection(w, r, user)
	if !ok {
		return
	}
	if err := s.runner.StartWithOptions(r.Context(), input.AccountIDs, input.RunOptions); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "message": "任务已开始"})
}

func (s *Server) qrState(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": s.qr.State(user.ID)})
}

func (s *Server) qrStart(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		AccountName string `json:"account_name"`
		AccountID   string `json:"account_id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.AccountName == "" {
		input.AccountName = "main"
	}
	state, err := s.qr.StartBinding(context.Background(), user.ID, input.AccountName, input.AccountID)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": state})
}

func (s *Server) qrRefresh(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	state, err := s.qr.Refresh(context.Background(), user.ID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": state})
}

func (s *Server) qrCancel(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	s.qr.Cancel(user.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": s.qr.State(user.ID)})
}

func (s *Server) accountFor(user model.User, id string) (model.Account, bool) {
	return s.store.AccountForUser(user.ID, user.Role == "admin", id)
}

func (s *Server) shopGoods(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	_ = user
	service := shop.Service{Client: s.shopClient, Config: s.store.Config()}
	result, err := service.Goods(r.Context(), r.URL.Query().Get("game"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	for _, good := range result["goods"].([]map[string]any) {
		icon, _ := good["icon"].(string)
		good["icon"] = shopImageURL(icon)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": result})
}

func (s *Server) shopGoodDetail(w http.ResponseWriter, r *http.Request, _ model.User) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	service := shop.Service{Client: s.shopClient, Config: s.store.Config()}
	result, err := service.GoodDetail(r.Context(), r.URL.Query().Get("goods_id"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	icon, _ := result["icon"].(string)
	result["icon"] = shopImageURL(icon)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": result})
}

func (s *Server) shopPoints(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	account, ok := s.accountFor(user, accountID)
	if !ok {
		writeError(w, http.StatusForbidden, errors.New("账号不存在或无权访问"))
		return
	}
	service := shop.Service{Client: s.shopClient, Config: s.store.Config(), Account: account}
	result, err := service.Points(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": result})
}

func (s *Server) shopAddresses(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	account, ok := s.accountFor(user, strings.TrimSpace(r.URL.Query().Get("account_id")))
	if !ok {
		writeError(w, http.StatusForbidden, errors.New("账号不存在或无权访问"))
		return
	}
	service := shop.Service{Client: s.shopClient, Config: s.store.Config(), Account: account}
	result, err := service.Addresses(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": result})
}

func (s *Server) shopRoles(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	account, ok := s.accountFor(user, strings.TrimSpace(r.URL.Query().Get("account_id")))
	if !ok {
		writeError(w, http.StatusForbidden, errors.New("账号不存在或无权访问"))
		return
	}
	service := shop.Service{Client: s.shopClient, Config: s.store.Config(), Account: account}
	result, err := service.Roles(r.Context(), r.URL.Query().Get("game_biz"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": result})
}

func textValue(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	valueText := strings.TrimSpace(fmt.Sprint(value))
	if valueText == "" {
		return fallback
	}
	return valueText
}

func sessionToken(r *http.Request) string {
	if cookie, err := r.Cookie("miyohub_session"); err == nil {
		return cookie.Value
	}
	return ""
}
func (s *Server) setSession(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{Name: "miyohub_session", Value: token, Path: "/", Secure: s.secureCookies(r), HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 3600})
}
func (s *Server) clearSession(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "miyohub_session", Value: "", Path: "/", Secure: s.secureCookies(r), SameSite: http.SameSiteLaxMode, HttpOnly: true, MaxAge: -1})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(destination); err != nil {
		var large *http.MaxBytesError
		status := 400
		if errors.As(err, &large) {
			status = 413
		}
		writeError(w, status, decodeRequestError(err))
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, 400, errors.New("只允许一个 JSON 对象"))
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
}
func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, errors.New("请求方法不支持"))
}

func DeviceClassFromUA(ua string) model.DeviceClass {
	value := strings.ToLower(ua)
	if strings.Contains(value, "android") {
		return model.DeviceAndroid
	}
	if strings.Contains(value, "iphone") || strings.Contains(value, "ipad") {
		return model.DeviceIOS
	}
	if strings.Contains(value, "tablet") {
		return model.DeviceTablet
	}
	return model.DeviceDesktop
}

func RequestContext(r *http.Request) context.Context {
	return context.WithValue(r.Context(), deviceContextKey{}, DeviceClassFromUA(r.UserAgent()))
}

type deviceContextKey struct{}
