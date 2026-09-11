package model

import (
	"errors"
	"time"
)

// HasTasks reports selected work, not merely the automatic scheduling switch.
func (p AccountTaskSettings) HasTasks() bool {
	return p.Features.GameCheckin && len(p.Games.Enabled) > 0 ||
		p.Features.CloudGameCheckin && len(p.CloudGames.Enabled) > 0 ||
		p.Features.BBSTasks && len(p.BBS.Forums) > 0 && (p.BBS.Checkin || p.BBS.Read || p.BBS.Like || p.BBS.Share)
}

func ValidateAccountSchedule(schedule AccountSchedule) error {
	parsed, err := time.Parse("15:04", schedule.Time)
	if err != nil || parsed.Format("15:04") != schedule.Time {
		return errors.New("签到时间须为 00:00–23:59")
	}
	if schedule.Timezone == "" || len(schedule.Timezone) > 64 {
		return errors.New("请选择有效的签到时区")
	}
	if _, err := time.LoadLocation(schedule.Timezone); err != nil {
		return errors.New("签到时区无效")
	}
	return nil
}

func (a Account) EffectiveSchedule(fallback Schedule) Schedule {
	if a.TaskSettings != nil && a.TaskSettings.Schedule != nil {
		return Schedule{Enabled: true, Time: a.TaskSettings.Schedule.Time, Timezone: a.TaskSettings.Schedule.Timezone}
	}
	return fallback
}

func (a Account) AutomaticDoneToday(fallback Schedule, now time.Time) bool {
	zone, err := time.LoadLocation(a.EffectiveSchedule(fallback).Timezone)
	if err != nil {
		return false
	}
	return !a.LastAutomaticAt.IsZero() && a.LastAutomaticAt.In(zone).Format(time.DateOnly) == now.In(zone).Format(time.DateOnly)
}

func (a Account) NextCustomRun(now time.Time) time.Time {
	if a.Disabled || a.TaskSettings == nil || !a.TaskSettings.Automatic || a.TaskSettings.Schedule == nil {
		return time.Time{}
	}
	schedule := *a.TaskSettings.Schedule
	if ValidateAccountSchedule(schedule) != nil {
		return time.Time{}
	}
	zone, _ := time.LoadLocation(schedule.Timezone)
	today := now.In(zone)
	clock, _ := time.Parse("15:04", schedule.Time)
	next := time.Date(today.Year(), today.Month(), today.Day(), clock.Hour(), clock.Minute(), 0, 0, zone)
	if !next.After(now) || a.AutomaticDoneToday(Schedule{}, now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func (a Account) CustomRunDue(now time.Time) bool {
	if a.Disabled || a.TaskSettings == nil || !a.TaskSettings.Automatic || a.TaskSettings.Schedule == nil || ValidateAccountSchedule(*a.TaskSettings.Schedule) != nil {
		return false
	}
	zone, _ := time.LoadLocation(a.TaskSettings.Schedule.Timezone)
	local := now.In(zone)
	return local.Format("15:04") == a.TaskSettings.Schedule.Time && !a.AutomaticDoneToday(Schedule{}, now)
}
