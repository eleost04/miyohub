package mihoyo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

func TestRecordValueNormalization(t *testing.T) {
	for _, raw := range []string{`null`, `true`, `false`, `""`, `"garbage"`, `1.5`, `"1.0"`, `1e4`, `"NaN"`, `-1`, `10000000000000000000000`, `{}`, `[]`} {
		if recordInt(json.RawMessage(raw), 0, 1000) != nil {
			t.Fatalf("invalid number accepted: %s", raw)
		}
	}
	for _, raw := range []string{`0`, `"0"`, `" 0 "`} {
		if n := recordInt(json.RawMessage(raw), 0, 1000); n == nil || *n != 0 {
			t.Fatal("valid zero lost")
		}
	}
	for _, raw := range []string{`false`, `"false"`, `0`, `"0"`} {
		if b := recordBool(json.RawMessage(raw)); b == nil || *b {
			t.Fatal("false became truthy")
		}
	}
	for _, raw := range []string{`null`, `2`, `"no"`, `{}`, `[]`} {
		if recordBool(json.RawMessage(raw)) != nil {
			t.Fatal("unknown bool became false")
		}
	}
	for _, raw := range []string{`12`, `true`, `"bad\ntext"`} {
		if recordText(json.RawMessage(raw), 100) != "" {
			t.Fatal("invalid text accepted")
		}
	}
}

func TestThreeGameNotesPreserveMissingAndZero(t *testing.T) {
	for _, tc := range []struct {
		game, raw string
		energy    int64
	}{
		{"genshin", `{"current_resin":"0","max_resin":200,"resin_recovery_time":"600","finished_task_num":null,"is_extra_task_reward_received":"false"}`, 0},
		{"starrail", `{"current_stamina":"120","max_stamina":"300","current_train_score":0,"weekly_cocoon_cnt":"1","weekly_cocoon_limit":3}`, 120},
		{"zzz", `{"energy":{"progress":{"current":"99","max":240},"restore":"600"},"vitality":{"current":0,"max":400},"card_sign":"CardSignNo","vhs_sale":{"sale_state":"SaleStateDoing"}}`, 99},
	} {
		t.Run(tc.game, func(t *testing.T) {
			note, err := parseRecordNote(tc.game, recordMap(json.RawMessage(tc.raw)))
			if err != nil || note.Metrics[0].Current == nil || *note.Metrics[0].Current != tc.energy {
				t.Fatal("bad note", err)
			}
			if tc.game == "genshin" && (note.Metrics[1].Current != nil || note.Flags[0].Value == nil || *note.Flags[0].Value) {
				t.Fatal("missing or false misparsed")
			}
			if tc.game == "starrail" && *note.Metrics[3].Current != 1 {
				t.Fatal("used weekly count must not be treated as remaining")
			}
			if tc.game == "zzz" && (*note.Flags[0].Value || !*note.Flags[1].Value) {
				t.Fatal("ZZZ enums misparsed")
			}
		})
	}
	if _, err := parseRecordNote("genshin", recordObject{}); err == nil {
		t.Fatal("empty note became completed")
	}
}

func TestGameRecordProtocolAndBoundParameters(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.Header.Get("Cookie") != "stuid=60101;cookie_token=synthetic" || r.Header.Get("X-Rpc-Device_id") != "fixture-device" {
			t.Error("protocol credentials or method changed")
		}
		if r.URL.Path == AccountRolesPath {
			_, _ = w.Write([]byte(`{"retcode":0,"data":{"list":[{"game_biz":"hk4e_cn","game_uid":"60201","region":"cn_gf01","nickname":"fixture"}]}}`))
			return
		}
		if r.URL.Query().Get("role_id") != "60201" || r.URL.Query().Get("server") != "cn_gf01" {
			t.Error("role parameters lost")
		}
		if !strings.Contains(r.URL.Path, "zzz") {
			parts := strings.Split(r.Header.Get("DS"), ",")
			if len(parts) != 3 {
				t.Error("missing DS")
			} else if parts[2] != md5hex(fmt.Sprintf("salt=%s&t=%s&r=%s&b=&q=%s", PassportX4Salt, parts[0], parts[1], r.URL.RawQuery)) {
				t.Error("signature differs from transmitted bytes")
			}
		}
		_, _ = w.Write([]byte(`{"retcode":"0","data":{"current_resin":"5","current_stamina":6,"energy":{"progress":{"current":7}}}}`))
	}))
	defer upstream.Close()
	c := NewClient(upstream.URL)
	a := model.Account{Cookie: "stuid=60101;cookie_token=synthetic", Device: model.Device{ID: "fixture-device", FP: "fixture-fp"}}
	roles, err := c.RecordRoles(context.Background(), a, "genshin")
	if err != nil || len(roles) != 1 {
		t.Fatal("role query failed", err)
	}
	for _, game := range []string{"genshin", "starrail", "zzz"} {
		if _, err := c.RecordNote(context.Background(), a, game, roles[0]); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 4 {
		t.Fatal("unexpected upstream operations")
	}
	if _, err := c.RecordNote(context.Background(), a, "genshin", model.RecordRole{UID: "bad&role_id=other", Region: "cn_gf01"}); err == nil || calls.Load() != 4 {
		t.Fatal("unsafe role allowed upstream")
	}
}

func TestGameRecordRiskResponsesNeverLeakDetails(t *testing.T) {
	for _, tc := range []struct {
		response string
		kind     string
		cooldown time.Duration
	}{
		{`{"data":{}}`, "invalid", 5 * time.Minute},
		{`{"retcode":1034,"message":"synthetic-sensitive-data","data":{"challenge":"private"}}`, "verification", 6 * time.Hour},
		{`{"retcode":"10035"}`, "verification", 6 * time.Hour},
		{`{"retcode":5003}`, "verification", 6 * time.Hour},
		{`{"retcode":-100}`, "credentials", 15 * time.Minute},
		{`{"retcode":0,"data":null}`, "invalid", 5 * time.Minute},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(tc.response)) }))
		_, err := NewClient(server.URL).RecordRoles(context.Background(), model.Account{Cookie: "synthetic"}, "genshin")
		server.Close()
		failure, ok := err.(*RecordError)
		if !ok || failure.Kind != tc.kind || failure.Cooldown != tc.cooldown || strings.Contains(failure.Message, "synthetic-sensitive") {
			t.Fatal("unsafe failure handling", err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(429)
	}))
	defer server.Close()
	_, err := NewClient(server.URL).RecordRoles(context.Background(), model.Account{Cookie: "synthetic"}, "genshin")
	if e, ok := err.(*RecordError); !ok || e.Cooldown != time.Hour {
		t.Fatal("Retry-After ignored")
	}
}
