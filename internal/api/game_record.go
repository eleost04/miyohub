package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/eleost04/miyohub/internal/gamerecord"
	"github.com/eleost04/miyohub/internal/model"
)

func (s *Server) gameNote(w http.ResponseWriter, r *http.Request, user model.User) {
	s.gameRecord(w, r, user, false)
}
func (s *Server) gameCalendar(w http.ResponseWriter, r *http.Request, user model.User) {
	s.gameRecord(w, r, user, true)
}
func (s *Server) gameRecord(w http.ResponseWriter, r *http.Request, user model.User, calendar bool) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if !s.limits.allow("record:"+user.ID, 30, time.Minute) {
		rateError(w)
		return
	}
	query := r.URL.Query()
	for key, values := range query {
		if len(values) != 1 || (key != "account_id" && key != "game" && key != "role_id" && key != "server") {
			writeError(w, 400, errors.New("游戏数据查询参数无效"))
			return
		}
	}
	a, ok := s.accountFor(user, query.Get("account_id"))
	if !ok {
		writeError(w, 404, errors.New("账号不存在或无权访问"))
		return
	}
	if a.Disabled {
		writeError(w, 400, errors.New("账号已停用，请先启用后再读取"))
		return
	}
	if _, active := s.store.AccountRunnable(a.ID); !active {
		writeError(w, 403, errors.New("账号或所属用户已停用"))
		return
	}
	for _, task := range s.runner.ProgressForUser(user) {
		if task.AccountID == a.ID {
			writeJSON(w, 200, map[string]any{"ok": true, "data": model.RecordSnapshot{Status: "busy", Message: "该账号正在执行签到任务，请结束后再查询", RefreshAt: time.Now().Add(time.Minute)}})
			return
		}
	}
	for _, plan := range s.store.ExchangePlansForUser(user.ID, false) {
		if plan.AccountID == a.ID && plan.State == "running" {
			writeJSON(w, 200, map[string]any{"ok": true, "data": model.RecordSnapshot{Status: "busy", Message: "该账号正在兑换，请结束后再查询", RefreshAt: time.Now().Add(time.Minute)}})
			return
		}
	}
	var result model.RecordSnapshot
	var err error
	if calendar {
		result, err = s.records.Calendar(r.Context(), a, query.Get("game"), query.Get("role_id"), query.Get("server"))
	} else {
		result, err = s.records.Note(r.Context(), a, query.Get("game"), query.Get("role_id"), query.Get("server"))
	}
	if err != nil {
		writeError(w, 400, err)
		return
	}
	current, ok := s.accountFor(user, a.ID)
	_, active := s.store.AccountRunnable(a.ID)
	if !ok || !active || !gamerecord.SameIdentity(current, a) {
		writeError(w, 409, errors.New("账号状态已变化，请重新选择"))
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "data": result})
}
