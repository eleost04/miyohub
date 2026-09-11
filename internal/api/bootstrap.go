package api

import (
	"net/http"
	"time"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/scheduler"
)

// bootstrap replaces the auth-status -> me -> config/status request waterfall.
// Anonymous and expired sessions receive public login metadata only. Private
// data uses exactly the same visibility rules as the individual APIs.
func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	data := map[string]any{
		"version": s.options.Version,
		"auth":    model.AuthStatus{NeedAuth: true, HasAdmin: s.store.HasAdmin(), RegistrationMode: s.store.RegistrationMode()},
		"user":    nil,
	}
	if user, ok := s.store.UserBySession(sessionToken(r)); ok {
		data["user"] = user
		data["config"] = s.visibleConfig(user)
		// Accounts and exchange plans are already in config; do not transmit two
		// copies of potentially large private collections on every refresh.
		data["status"] = s.runtimeStatus(user)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": data})
}

func (s *Server) runtimeStatus(user model.User) map[string]any {
	return map[string]any{
		"running":            s.runner.RunningForUser(user),
		"tasks":              s.runner.ProgressForUser(user),
		"logs":               s.store.RedactedLogsForUser(user.ID, user.Role == "admin"),
		"scheduler":          s.personalScheduleStatus(user),
		"exchange_scheduler": s.exchange.Status(user),
	}
}

func (s *Server) personalScheduleStatus(user model.User) scheduler.Status {
	status := s.scheduler.Status()
	fallback, _ := time.Parse(time.RFC3339, status.NextRun)
	status.Enabled, status.NextRun, status.LastRun, status.LastError = false, "", "", ""
	status.Running = s.runner.RunningForUser(user)
	var next, last time.Time
	for _, a := range s.store.AccountsForUser(user.ID, false) {
		if a.Disabled || a.TaskSettings == nil || !a.TaskSettings.Automatic || !a.TaskSettings.HasTasks() {
			continue
		}
		candidate := a.NextCustomRun(time.Now())
		if a.TaskSettings.Schedule == nil && status.Schedule.Enabled {
			candidate = fallback
			status.Enabled = true
		}
		if a.TaskSettings.Schedule != nil {
			status.Enabled = true
		}
		if !candidate.IsZero() && (next.IsZero() || candidate.Before(next)) {
			next = candidate
		}
		if a.LastAutomaticAt.After(last) {
			last = a.LastAutomaticAt
		}
	}
	status.Enabled = status.Enabled && s.store.Config().Enabled
	if !next.IsZero() && status.Enabled {
		status.NextRun = next.Format(time.RFC3339)
	}
	if !last.IsZero() {
		status.LastRun = last.Format(time.RFC3339)
	}
	return status
}
