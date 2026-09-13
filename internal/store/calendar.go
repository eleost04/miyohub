package store

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

func (s *Store) CalendarEvents(userID, accountID, game string) []model.CustomCalendarEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []model.CustomCalendarEvent{}
	if !s.accountOwnedLocked(userID, false, accountID) {
		return result
	}
	for _, event := range s.data.CalendarEvents {
		if event.UserID == userID && event.AccountID == accountID && event.Game == game {
			result = append(result, clone(event))
		}
	}
	return result
}

func (s *Store) AddCalendarEvent(userID, accountID, game string, event model.CalendarEvent) (model.CustomCalendarEvent, error) {
	now := time.Now()
	event.Title = strings.TrimSpace(event.Title)
	if !mihoyo.RecordGame(game) || !mihoyo.CalendarEventValid(event, now) {
		return model.CustomCalendarEvent{}, errors.New("日程名称、类型或时间无效；开始应早于结束，日期须在一年内")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accountRunnableLocked(accountID)
	if !ok || a.UserID != userID {
		return model.CustomCalendarEvent{}, errors.New("账号不存在或已停用")
	}
	count := 0
	for _, e := range s.data.CalendarEvents {
		if e.UserID == userID {
			count++
		}
	}
	if count >= 100 || len(s.data.CalendarEvents) >= 5000 {
		return model.CustomCalendarEvent{}, errors.New("日程数量已达上限，请先清理旧日程")
	}
	event.ID, event.Source, event.Finished = randomID("calendar_"), "manual", nil
	item := model.CustomCalendarEvent{CalendarEvent: clone(event), UserID: userID, AccountID: accountID, Game: game, CreatedAt: now}
	s.data.CalendarEvents = append(s.data.CalendarEvents, item)
	return clone(item), s.saveLocked()
}

func (s *Store) DeleteCalendarEvent(userID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, event := range s.data.CalendarEvents {
		if event.ID != id || event.UserID != userID {
			continue
		}
		s.data.CalendarEvents = slices.Delete(s.data.CalendarEvents, i, i+1)
		for j, r := range s.data.CalendarReminders {
			if r.UserID == userID && r.EventID == id && (r.Status == "pending" || r.Status == "queued") {
				s.data.CalendarReminders[j].Status = "cancelled"
				s.data.CalendarReminders[j].Detail = "日程已删除"
				s.data.CalendarReminders[j].UpdatedAt = time.Now()
			}
		}
		return s.saveLocked()
	}
	return errors.New("日程不存在或无权访问")
}

func (s *Store) removeCalendarLocked(userID, accountID string) {
	s.data.CalendarReminders = slices.DeleteFunc(s.data.CalendarReminders, func(r model.CalendarReminder) bool {
		return (userID != "" && r.UserID == userID) || (accountID != "" && r.AccountID == accountID)
	})
	s.data.CalendarEvents = slices.DeleteFunc(s.data.CalendarEvents, func(e model.CustomCalendarEvent) bool {
		return (userID != "" && e.UserID == userID) || (accountID != "" && e.AccountID == accountID)
	})
}
