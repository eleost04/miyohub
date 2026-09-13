package model

import "time"

type RecordRole struct {
	UID        string `json:"uid"`
	Region     string `json:"region"`
	Nickname   string `json:"nickname"`
	RegionName string `json:"region_name"`
}

// Nil values mean unknown, not zero or completed. RecoverySeconds is a
// duration measured at ObservedAt, never an absolute Unix timestamp.
type RecordMetric struct {
	Key             string `json:"key"`
	Label           string `json:"label"`
	Current         *int64 `json:"current"`
	Max             *int64 `json:"max"`
	RecoverySeconds *int64 `json:"recovery_seconds,omitempty"`
}
type RecordFlag struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value *bool  `json:"value"`
}
type GameNote struct {
	Metrics []RecordMetric `json:"metrics"`
	Flags   []RecordFlag   `json:"flags"`
}

type RecordSnapshot struct {
	Game       string       `json:"game"`
	Roles      []RecordRole `json:"roles"`
	Role       *RecordRole  `json:"role,omitempty"`
	Note       *GameNote    `json:"note,omitempty"`
	ObservedAt time.Time    `json:"observed_at,omitzero"`
	RefreshAt  time.Time    `json:"refresh_at,omitzero"`
	Cached     bool         `json:"cached"`
	Stale      bool         `json:"stale"`
	Status     string       `json:"status"`
	Message    string       `json:"message,omitempty"`
}
