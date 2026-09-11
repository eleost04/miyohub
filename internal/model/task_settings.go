package model

import "reflect"

// SameWork excludes scheduling and optimistic-lock metadata. Changing when the
// next run starts must not revoke work that is already in progress.
func (p *AccountTaskSettings) SameWork(other *AccountTaskSettings) bool {
	if p == nil || other == nil {
		return p == other
	}
	a, b := *p, *other
	for _, value := range []*AccountTaskSettings{&a, &b} {
		value.Revision, value.Automatic, value.Schedule = 0, false, nil
		if len(value.Games.Enabled) == 0 {
			value.Games.Enabled = nil
		}
		if len(value.Games.Blacklist) == 0 {
			value.Games.Blacklist = nil
		}
		if len(value.CloudGames.Enabled) == 0 {
			value.CloudGames.Enabled = nil
		}
		if len(value.BBS.Forums) == 0 {
			value.BBS.Forums = nil
		}
	}
	return reflect.DeepEqual(a, b)
}
