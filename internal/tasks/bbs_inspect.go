package tasks

import (
	"context"
	"sort"
)

// InspectState performs exactly one read-only query, without renewing credentials,
// solving captchas, checking in, liking, sharing or redeeming anything.
func (b BBSCheckin) InspectState(ctx context.Context) (map[string]any, error) {
	state, err := b.state(ctx)
	if err != nil {
		return nil, err
	}
	fields := []string{}
	for key := range state {
		fields = append(fields, key)
	}
	sort.Strings(fields)
	rows := []map[string]any{}
	for _, raw := range maps(state["states"]) {
		row := map[string]any{}
		for _, key := range []string{"mission_id", "mission_key", "mission_name", "name", "happened_times", "is_get_award", "process", "threshold", "target_times", "finish_time"} {
			if value, ok := raw[key]; ok {
				row[key] = value
			}
		}
		keys := []string{}
		for key := range raw {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		row["fields"] = keys
		rows = append(rows, row)
	}
	return map[string]any{"fields": fields, "already_received_points": state["already_received_points"], "can_get_points": state["can_get_points"], "today_total_points": state["today_total_points"], "is_unclaimed": state["is_unclaimed"], "states": rows, "settings": b.Config.BBS}, nil
}
