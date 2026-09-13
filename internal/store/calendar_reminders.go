package store

import (
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/notify"
)

func (s *Store) CalendarReminders(userID, accountID, game string) []model.CalendarReminder {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []model.CalendarReminder{}
	if !s.accountOwnedLocked(userID, false, accountID) {
		return result
	}
	for _, r := range s.data.CalendarReminders {
		if r.UserID == userID && r.AccountID == accountID && r.Game == game {
			result = append(result, r)
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].RemindAt.Before(result[j].RemindAt) })
	return result
}

// now is supplied by the server clock, never a client request field.
func (s *Store) AddCalendarReminder(userID, accountID, game string, event model.CalendarEvent, target string, leadMinutes int, observedAt, now time.Time) (model.CalendarReminder, error) {
	fail := func(message string) (model.CalendarReminder, error) {
		return model.CalendarReminder{}, errors.New(message)
	}
	if !mihoyo.RecordGame(game) || (target != "start" && target != "end") || !slices.Contains([]int{0, 10, 30, 60, 1440}, leadMinutes) {
		return fail("提醒参数无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accountRunnableLocked(accountID)
	if !ok || a.UserID != userID {
		return fail("账号不存在或已停用")
	}
	if event.Source == "manual" {
		found := false
		for _, current := range s.data.CalendarEvents {
			if current.ID == event.ID && current.UserID == userID && current.AccountID == accountID && current.Game == game {
				event, observedAt, found = current.CalendarEvent, current.CreatedAt, true
				break
			}
		}
		if !found {
			return fail("自定义日程已删除，请重新加载")
		}
	} else if event.Source != "official" || observedAt.IsZero() || now.Sub(observedAt) > 30*time.Minute || observedAt.After(now.Add(time.Minute)) {
		return fail("官方日历快照已过期，请先读取活动")
	}
	if event.ID == "" || !mihoyo.CalendarEventValid(event, now) {
		return fail("日程内容无效")
	}
	at := event.StartAt
	if target == "end" {
		at = event.EndAt
	}
	if at == nil || !at.After(now) {
		return fail("该日程缺少有效的未来提醒时间")
	}
	remindAt := at.Add(-time.Duration(leadMinutes) * time.Minute)
	if !remindAt.After(now) {
		return fail("提醒时间已过，请缩短提前时间")
	}
	count := 0
	for _, r := range s.data.CalendarReminders {
		if r.UserID != userID {
			continue
		}
		count++
		if r.AccountID == accountID && r.Game == game && r.EventID == event.ID && r.Target == target && (r.Status == "pending" || r.Status == "queued" || r.Status == "processed") {
			return fail("此时间点已安排或处理过提醒，不重复创建")
		}
	}
	if count >= 200 || len(s.data.CalendarReminders) >= 10000 {
		return fail("提醒数量已达上限，请先清理已结束的提醒")
	}
	r := model.CalendarReminder{ID: randomID("reminder_"), UserID: userID, AccountID: accountID, Game: game, EventID: event.ID, Title: event.Title, Source: event.Source, Target: target, TargetAt: *at, RemindAt: remindAt, ObservedAt: observedAt, Status: "pending", CreatedAt: now, UpdatedAt: now}
	s.data.CalendarReminders = append(s.data.CalendarReminders, r)
	return r, s.saveLocked()
}

func (s *Store) CancelCalendarReminder(userID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.data.CalendarReminders {
		if r.UserID != userID || r.ID != id {
			continue
		}
		for _, entry := range s.data.PushDeliveries {
			if entry.ReferenceID == r.ID && (entry.Status == "sending" || entry.Status == "accepted" || entry.Status == "unknown") {
				return errors.New("通知已提交处理，无法撤回；可在消息推送查看结果")
			}
		}
		if r.Status == "processed" {
			return errors.New("提醒已处理，不能撤回")
		}
		s.data.CalendarReminders[i].Status, s.data.CalendarReminders[i].Detail, s.data.CalendarReminders[i].UpdatedAt = "cancelled", "已取消，未继续发送", time.Now()
		for j, entry := range s.data.PushDeliveries {
			if entry.ReferenceID == r.ID && entry.Status == "pending" {
				s.data.PushDeliveries[j].Status = "skipped"
				s.data.PushDeliveries[j].Error = "提醒已取消"
				s.data.PushDeliveries[j].UpdatedAt = time.Now()
			}
		}
		return s.saveLocked()
	}
	return errors.New("提醒不存在或无权访问")
}

// History cleanup is explicit and cannot remove active reminders or authorize
// replay of a previously processed event time.
func (s *Store) ClearCalendarReminderHistory(userID, accountID, game string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.accountOwnedLocked(userID, false, accountID) {
		return errors.New("账号不存在或无权访问")
	}
	s.data.CalendarReminders = slices.DeleteFunc(s.data.CalendarReminders, func(r model.CalendarReminder) bool {
		return r.UserID == userID && r.AccountID == accountID && r.Game == game && r.Status != "pending" && r.Status != "queued" && (r.Status != "processed" || !r.TargetAt.After(now))
	})
	return s.saveLocked()
}

func (s *Store) calendarReferenceLocked(userID, accountID, id string) bool {
	if id == "" {
		return false
	}
	for _, r := range s.data.CalendarReminders {
		if r.ID == id {
			return r.UserID == userID && r.AccountID == accountID && r.Status == "queued"
		}
	}
	return false
}
func (s *Store) calendarDeliveryAllowedLocked(entry model.PushDelivery, now time.Time) bool {
	if !s.calendarReferenceLocked(entry.UserID, entry.AccountID, entry.ReferenceID) {
		return false
	}
	for _, r := range s.data.CalendarReminders {
		if r.ID == entry.ReferenceID {
			return !now.Before(r.RemindAt) && !now.After(r.RemindAt.Add(15*time.Minute)) && !now.After(r.TargetAt.Add(5*time.Minute))
		}
	}
	return false
}

func (s *Store) finishCalendarReminderLocked(delivery model.PushDelivery, now time.Time) {
	id := delivery.ReferenceID
	if id == "" {
		return
	}
	for i, r := range s.data.CalendarReminders {
		if r.ID != id || r.Status != "queued" {
			continue
		}
		switch delivery.Status {
		case "accepted":
			s.data.CalendarReminders[i].AcceptedChannels++
		case "unknown":
			s.data.CalendarReminders[i].UncertainChannels++
		default:
			s.data.CalendarReminders[i].FailedChannels++
		}
		for _, e := range s.data.PushDeliveries {
			if e.ReferenceID != id {
				continue
			}
			if e.Status == "pending" || e.Status == "sending" {
				return
			}
		}
		accepted, failed, uncertain := s.data.CalendarReminders[i].AcceptedChannels, s.data.CalendarReminders[i].FailedChannels, s.data.CalendarReminders[i].UncertainChannels
		detail := "推送服务已接收通知"
		if uncertain > 0 {
			detail = "部分通知发送状态未确认，未自动重发"
		} else if failed > 0 && accepted > 0 {
			detail = "部分渠道未受理，请在消息推送查看详情"
		} else if accepted == 0 {
			detail = "未发送或发送失败，请在消息推送查看详情"
		}
		s.data.CalendarReminders[i].Status, s.data.CalendarReminders[i].Detail, s.data.CalendarReminders[i].UpdatedAt = "processed", detail, now
		return
	}
}

func (s *Store) QueueDueCalendarReminders(now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed, queued := false, false
	processed := 0
	var lastErr error
	for i, r := range s.data.CalendarReminders {
		if r.Status != "pending" || now.Before(r.RemindAt) {
			continue
		}
		if processed >= 50 {
			break
		}
		processed++
		if now.After(r.RemindAt.Add(15*time.Minute)) || now.After(r.TargetAt.Add(5*time.Minute)) {
			s.data.CalendarReminders[i].Status, s.data.CalendarReminders[i].Detail, s.data.CalendarReminders[i].UpdatedAt = "expired", "提醒时间已过，未补发", now
			changed = true
			continue
		}
		a, active := s.accountRunnableLocked(r.AccountID)
		if !active || a.UserID != r.UserID {
			s.data.CalendarReminders[i].Status, s.data.CalendarReminders[i].Detail, s.data.CalendarReminders[i].UpdatedAt = "skipped", "账号或所属用户已停用", now
			changed = true
			continue
		}
		s.data.CalendarReminders[i].Status = "queued"
		ok, err := s.queuePushEventLocked(notify.CalendarEvent(s.data.Config, a, r))
		if err != nil {
			s.data.CalendarReminders[i].Status = "pending"
			lastErr = err
			continue
		}
		if !ok {
			s.data.CalendarReminders[i].Status, s.data.CalendarReminders[i].Detail = "skipped", "日历推送已关闭或没有启用的接收渠道"
		} else {
			s.data.CalendarReminders[i].Detail = "已加入发送队列，服务受理结果见消息推送"
			queued = true
		}
		s.data.CalendarReminders[i].UpdatedAt = now
		changed = true
	}
	if changed {
		if err := s.saveLocked(); err != nil {
			return false, err
		}
	}
	return queued, lastErr
}
