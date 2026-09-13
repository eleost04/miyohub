package model

import "time"

// A reminder is an explicit, owner-local subscription to a known event time.
// It contains no upstream credentials and never schedules upstream polling.
type CalendarReminder struct {
	ID                string    `json:"id"`
	UserID            string    `json:"user_id"`
	AccountID         string    `json:"account_id"`
	Game              string    `json:"game"`
	EventID           string    `json:"event_id"`
	Title             string    `json:"title"`
	Source            string    `json:"source"`
	Target            string    `json:"target"`
	TargetAt          time.Time `json:"target_at"`
	RemindAt          time.Time `json:"remind_at"`
	ObservedAt        time.Time `json:"observed_at"`
	Status            string    `json:"status"`
	Detail            string    `json:"detail,omitempty"`
	AcceptedChannels  int       `json:"accepted_channels,omitempty"`
	FailedChannels    int       `json:"failed_channels,omitempty"`
	UncertainChannels int       `json:"uncertain_channels,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}
