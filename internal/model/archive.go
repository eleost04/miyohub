package model

import "time"

// A portable allowlist, deliberately not State or Account: no site identity,
// permission, session, log, exchange plan or automatic-run state is transferable.
type AccountArchive struct {
	Version   int               `json:"version"`
	CreatedAt time.Time         `json:"created_at"`
	Accounts  []PortableAccount `json:"accounts"`
}

type PortableAccount struct {
	Name         string              `json:"name"`
	Group        string              `json:"group,omitempty"`
	UID          string              `json:"uid"`
	Cookie       string              `json:"cookie"`
	SToken       string              `json:"stoken,omitempty"`
	Mid          string              `json:"mid,omitempty"`
	Device       Device              `json:"device"`
	CloudTokens  map[string]string   `json:"cloud_tokens,omitempty"`
	TaskSettings AccountTaskSettings `json:"task_settings"`
}

type ArchivePreview struct {
	Names   []string `json:"names"`
	Add     int      `json:"add"`
	Skipped int      `json:"skipped"`
}
