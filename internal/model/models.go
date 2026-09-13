package model

import "time"

const DefaultTimezone = "Asia/Shanghai"

type State struct {
	Config           Config                       `json:"config"`
	Users            []User                       `json:"users"`
	Sessions         map[string]Session           `json:"sessions"`
	Logs             []LogEntry                   `json:"logs"`
	InviteCodes      []InviteCode                 `json:"invite_codes"`
	RegistrationMode string                       `json:"registration_mode"`
	UserPush         map[string]PushConfig        `json:"user_push"`
	UserCaptcha      map[string]UserCaptchaConfig `json:"user_captcha"`
	CaptchaActivity  map[string][]CaptchaAttempt  `json:"captcha_activity,omitempty"`
	CaptchaProbes    map[string]CaptchaProbe      `json:"captcha_probes,omitempty"`
	PushDeliveries   []PushDelivery               `json:"push_deliveries"`
}

type Config struct {
	Enabled    bool             `json:"enabled"`
	Accounts   []Account        `json:"accounts"`
	Device     Device           `json:"device"`
	Features   Features         `json:"features"`
	Games      GamesConfig      `json:"games"`
	CloudGames CloudGamesConfig `json:"cloud_games"`
	BBS        BBSConfig        `json:"bbs"`
	Network    NetworkConfig    `json:"network"`
	Captcha    CaptchaConfig    `json:"captcha"`
	Schedule   Schedule         `json:"schedule"`
	Push       PushConfig       `json:"push"`
	Shop       ShopConfig       `json:"shop_exchange"`
}

type Account struct {
	Disabled        bool                   `json:"disabled"`
	Status          string                 `json:"status"`
	CheckedAt       time.Time              `json:"checked_at"`
	Device          Device                 `json:"device"`
	ShopDeviceFP    string                 `json:"shop_device_fp"`
	HasCookie       bool                   `json:"has_cookie"`
	HasSToken       bool                   `json:"has_stoken"`
	CloudConfigured []string               `json:"cloud_configured"`
	TaskResults     map[string]TaskSummary `json:"task_results"`
	LastTaskAt      time.Time              `json:"last_task_at"`
	LastAutomaticAt time.Time              `json:"last_automatic_at,omitempty"`
	ID              string                 `json:"id"`
	UserID          string                 `json:"user_id"`
	Name            string                 `json:"name"`
	Cookie          string                 `json:"cookie"`
	Stuid           string                 `json:"stuid"`
	Stoken          string                 `json:"stoken"`
	Mid             string                 `json:"mid"`
	CloudTokens     map[string]string      `json:"cloud_tokens"`
	TaskSettings    *AccountTaskSettings   `json:"task_settings"`
	ExchangeAllowed bool                   `json:"exchange_allowed"` // Public view only.
}

// AccountTaskSettings belongs to one bound account, not to the site operator.
// A nil pointer on old accounts is migrated from the legacy global rules once.
type AccountTaskSettings struct {
	Revision   int              `json:"revision"`
	Automatic  bool             `json:"automatic"`
	Schedule   *AccountSchedule `json:"schedule,omitempty"`
	Features   Features         `json:"features"`
	Games      GamesConfig      `json:"games"`
	CloudGames CloudGamesConfig `json:"cloud_games"`
	BBS        BBSConfig        `json:"bbs"`
}

type Device struct {
	ID    string `json:"id"`
	FP    string `json:"fp"`
	Name  string `json:"name"`
	Model string `json:"model"`
}

// A nil account schedule follows the site's fallback time. A custom schedule
// is owned by the account's site user and has no run-on-start side effects.
type AccountSchedule struct {
	Time     string `json:"time"`
	Timezone string `json:"timezone"`
}

type Features struct {
	GameCheckin      bool `json:"game_checkin"`
	CloudGameCheckin bool `json:"cloud_game_checkin"`
	BBSTasks         bool `json:"bbs_tasks"`
}

type GamesConfig struct {
	Enabled   []string            `json:"enabled"`
	Blacklist map[string][]string `json:"black_list"`
}

type CloudGamesConfig struct {
	Enabled []string `json:"enabled"`
}

type BBSConfig struct {
	RunAllSelected bool  `json:"run_all_selected"`
	Forums         []int `json:"forums"`
	Checkin        bool  `json:"checkin"`
	Read           bool  `json:"read"`
	Like           bool  `json:"like"`
	Share          bool  `json:"share"`
	CancelLike     bool  `json:"cancel_like"`
	PostLimit      int   `json:"post_limit"`
	DelaySeconds   []int `json:"delay_seconds"`
}

type NetworkConfig struct {
	// nil preserves the default for configurations created before this field.
	BBSStateRetries *int        `json:"bbs_state_retries,omitempty"`
	Proxy           ProxyConfig `json:"proxy"`
}

type ProxyConfig struct {
	Enabled       bool   `json:"enable"`
	URL           string `json:"url"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	HasPassword   bool   `json:"has_password,omitempty"` // Public view only.
	ClearPassword bool   `json:"clear_password,omitempty"`
}

func (n NetworkConfig) StateRetries() int {
	if n.BBSStateRetries == nil {
		return 5
	}
	return max(0, min(10, *n.BBSStateRetries))
}

type CaptchaConfig struct {
	MaxRetries int              `json:"max_retries"`
	Channels   []CaptchaChannel `json:"channels"`
	// Runtime-only policy, constructed by Store.CaptchaForUser. Never accepted
	// from configuration or returned through the API.
	PublicOnly bool                 `json:"-"`
	Allowed    func() bool          `json:"-"`
	Observe    func(CaptchaAttempt) `json:"-"`
	Purpose    string               `json:"-"`
}

// CaptchaAttempt contains no challenge, validate, credentials or endpoint URL.
type CaptchaAttempt struct {
	At         time.Time `json:"at"`
	Source     string    `json:"source"`
	Provider   string    `json:"provider"`
	ChannelID  string    `json:"channel_id"`
	Kind       string    `json:"kind"`
	OK         bool      `json:"ok"`
	Code       string    `json:"code"`
	DurationMS int64     `json:"duration_ms"`
}

// CaptchaProbe never contains challenge values, solver URLs or credentials.
type CaptchaProbe struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	RetryAt    time.Time `json:"retry_at"`
	DurationMS int64     `json:"duration_ms"`
	Message    string    `json:"message"`
}

type UserCaptchaConfig struct {
	Source     string           `json:"source"` // off, personal, site
	Revision   int              `json:"revision"`
	MaxRetries int              `json:"max_retries"`
	Channels   []CaptchaChannel `json:"channels"`
}

type CaptchaChannel struct {
	ID         string   `json:"id"`
	Provider   string   `json:"provider"`
	Enabled    bool     `json:"enable"`
	UserKey    string   `json:"userkey"`
	Type       string   `json:"type"`
	Timeout    int      `json:"timeout"`
	Endpoint   string   `json:"endpoint"`
	Token      string   `json:"token"`
	UseV3Model *bool    `json:"use_v3_model,omitempty"`
	Configured []string `json:"configured,omitempty"`  // Public view only; never trusted on input.
	ClearToken bool     `json:"clear_token,omitempty"` // Explicit removal; a blank token otherwise preserves it.
}

type Schedule struct {
	Enabled    bool   `json:"enable"`
	Time       string `json:"time"`
	Timezone   string `json:"timezone"`
	JitterMins int    `json:"jitter_minutes"`
	RunOnStart bool   `json:"run_on_start"`
}

type PushConfig struct {
	Enabled   bool          `json:"enable"`
	Tasks     bool          `json:"tasks"`
	Exchange  bool          `json:"exchange"`
	Revision  int           `json:"revision"`
	ErrorOnly bool          `json:"error_only"`
	Channels  []PushChannel `json:"channels"`
}

type PushChannel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Provider     string `json:"provider"`
	Enabled      bool   `json:"enable"`
	Token        string `json:"token"`
	Webhook      string `json:"webhook"`
	Secret       string `json:"secret"`
	ChatID       string `json:"chat_id"`
	APIURL       string `json:"api_url"`
	AppID        string `json:"app_id"`
	ClientSecret string `json:"client_secret"`
	OpenID       string `json:"openid"`
	Topic        string `json:"topic"`
	PushURL      string `json:"push_url"`
	AccessToken  string `json:"access_token"`
	SendID       string `json:"send_id"`
	MsgType      string `json:"msg_type"`
	SMTPHost     string `json:"smtp_host"`
	SMTPPort     int    `json:"smtp_port"`
	SMTPUser     string `json:"smtp_user"`
	SMTPPassword string `json:"smtp_password"`
	MailFrom     string `json:"mail_from"`
	MailTo       string `json:"mail_to"`
	SMTPSSL      bool   `json:"smtp_ssl"`
	Mode         string `json:"mode"`
	BotID        string `json:"bot_id"`
	ContextToken string `json:"context_token"`
	SyncCursor   string `json:"sync_cursor"`
	BindingState string `json:"binding_state"`
	BindingError string `json:"binding_error,omitempty"`
}

// PushDelivery contains only a sanitized report, never channel credentials.
type PushDelivery struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	AccountID   string    `json:"account_id"`
	ChannelID   string    `json:"channel_id"`
	ChannelName string    `json:"channel_name"`
	Provider    string    `json:"provider"`
	Kind        string    `json:"kind"`
	Revision    int       `json:"revision"`
	Title       string    `json:"title"`
	Message     string    `json:"message"`
	Success     bool      `json:"success"`
	Status      string    `json:"status"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ShopConfig struct {
	Enabled       bool           `json:"enable"`
	RetrySeconds  float64        `json:"retry_seconds"`
	RetryInterval float64        `json:"retry_interval"`
	Push          bool           `json:"push"`
	Plans         []ExchangePlan `json:"plans"`
}

type ExchangePlan struct {
	State      string `json:"state"`
	Phase      string `json:"phase,omitempty"`
	Attempt    int    `json:"attempt"`
	AttemptKey string `json:"attempt_key"`
	Revision   int    `json:"revision"`
	Price      int    `json:"price"`
	GoodsType  int    `json:"goods_type"`
	ID         string `json:"id"`
	Enabled    bool   `json:"enable"`
	Auto       bool   `json:"auto"`
	AccountID  string `json:"account_id"`
	GoodsID    string `json:"goods_id"`
	GoodsName  string `json:"goods_name"`
	DeviceFP   string `json:"device_fp"`
	UID        string `json:"uid"`
	Region     string `json:"region"`
	GameBiz    string `json:"game_biz"`
	AddressID  string `json:"address_id"`
	ExchangeAt int64  `json:"exchange_at"`
	LastResult string `json:"last_result"`
	LastRun    string `json:"last_run"`
}

type User struct {
	ID               string          `json:"id"`
	Username         string          `json:"username"`
	PasswordHash     string          `json:"password_hash"`
	Role             string          `json:"role"`
	Status           string          `json:"status"`
	CreatedAt        time.Time       `json:"created_at"`
	Permissions      UserPermissions `json:"permissions"`
	OnboardingStatus string          `json:"onboarding_status,omitempty"`
}

type UserPermissions struct {
	Exchange    bool `json:"exchange"`
	SiteCaptcha bool `json:"site_captcha"`
}

func (u User) CanExchange() bool {
	return u.Status == "active" && (u.Role == "admin" || u.Permissions.Exchange)
}

func (u User) CanUseSiteCaptcha() bool {
	return u.Status == "active" && (u.Role == "admin" || u.Permissions.SiteCaptcha)
}

type InviteCode struct {
	Permissions    UserPermissions `json:"permissions"`
	UseSiteCaptcha bool            `json:"use_site_captcha"`
	Code           string          `json:"code"`
	CreatedAt      time.Time       `json:"created_at"`
	ExpiresAt      time.Time       `json:"expires_at,omitempty"`
	MaxUses        int             `json:"max_uses"`
	UsedCount      int             `json:"used_count"`
	Note           string          `json:"note,omitempty"`
	CreatedBy      string          `json:"created_by,omitempty"`
	Disabled       bool            `json:"disabled"`
}

type Session struct {
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type LogEntry struct {
	At        time.Time `json:"at"`
	Component string    `json:"component"`
	Message   string    `json:"message"`
	UserID    string    `json:"user_id,omitempty"`
	AccountID string    `json:"account_id,omitempty"`
	RunID     string    `json:"run_id,omitempty"`
}

type AuthStatus struct {
	NeedAuth         bool   `json:"need_auth"`
	HasAdmin         bool   `json:"has_admin"`
	RegistrationMode string `json:"registration_mode"`
}

type LoginState struct {
	Running   bool      `json:"running"`
	Status    string    `json:"status"`
	QRURL     string    `json:"qr_url"`
	Ticket    string    `json:"ticket"`
	Account   string    `json:"account"`
	AccountID string    `json:"account_id,omitempty"`
	Error     string    `json:"error"`
	StartedAt time.Time `json:"started_at"`
}

type DeviceClass string

const (
	DeviceAndroid DeviceClass = "android"
	DeviceIOS     DeviceClass = "ios"
	DeviceTablet  DeviceClass = "tablet"
	DeviceDesktop DeviceClass = "desktop"
)

// TaskSummary records action results for one task family.
type TaskSummary struct {
	Success int      `json:"success"`
	Failed  int      `json:"failed"`
	Skipped int      `json:"skipped"`
	Status  string   `json:"status,omitempty"`
	Reason  string   `json:"reason,omitempty"`
	Details []string `json:"details,omitempty"`
}

type TaskProgress struct {
	AccountID string    `json:"account_id"`
	State     string    `json:"state"`
	Current   string    `json:"current"`
	StartedAt time.Time `json:"started_at"`
}
