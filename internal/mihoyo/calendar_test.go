package mihoyo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

func TestCalendarTimestampUnitsAndInvalidData(t *testing.T) {
	seconds, millis := recordTime(json.RawMessage(`"1780000000"`)), recordTime(json.RawMessage(`1780000000000`))
	if seconds == nil || millis == nil || !seconds.Equal(*millis) {
		t.Fatal("seconds and milliseconds differ")
	}
	for _, raw := range []string{`0`, `-1`, `"unknown"`, `1780000000000000`, `17800000000`, `null`, `true`, `1780000000.5`, `"9999999999999999999"`} {
		if recordTime(json.RawMessage(raw)) != nil {
			t.Fatalf("invalid calendar time accepted: %s", raw)
		}
	}
	now := time.Unix(1780000000, 0)
	data := recordMap(json.RawMessage(`{"now":"1780000000","avatar_card_pool_list":[{"pool_name":"示例祈愿","time_info":{"start_ts":"1779990000","end_ts":"1780100000000"}}],"act_list":[{"name":"限时活动","is_finished":"false","time_info":{"end_ts":"1780200000"}},{"name":"无效日期","time_info":{"start_ts":1780300000,"end_ts":1780200000}},{"name":"已经结束","time_info":{"end_ts":1779990000}},{"name":"缺少截止","time_info":{}}]}`))
	calendar, err := parseRecordCalendar("genshin", data, now)
	if err != nil || len(calendar.Events) != 2 || calendar.Skipped != 2 || calendar.ServerTime == nil {
		t.Fatal("bad calendar normalization", err)
	}
	if calendar.Events[1].StartAt != nil || calendar.Events[1].Finished == nil || *calendar.Events[1].Finished {
		t.Fatal("missing date or false flag guessed")
	}
	if calendar.Events[0].EndAt.Unix() != 1780100000 {
		t.Fatal("milliseconds misread as seconds")
	}
	for _, raw := range []string{`{}`, `{"act_list":null}`, `{"act_list":{}}`} {
		if _, err := parseRecordCalendar("genshin", recordMap(json.RawMessage(raw)), now); err == nil {
			t.Fatal("invalid list treated as empty")
		}
	}
	if empty, err := parseRecordCalendar("starrail", recordMap(json.RawMessage(`{"act_list":[]}`)), now); err != nil || len(empty.Events) != 0 {
		t.Fatal("valid empty calendar rejected")
	}
}

func TestCalendarRequestSignsExactBodyAndQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(r.URL.Path, "genshin") {
			if r.Method != "POST" || r.URL.RawQuery != "" || string(body) != `{"role_id":"100001","server":"cn_gf01"}` {
				t.Error("Genshin calendar body differs")
			}
		} else if r.Method != "GET" || len(body) != 0 || r.URL.Path != "/game_record/app/hkrpg/api/get_act_calender" {
			t.Error("Star Rail calendar route differs")
		}
		parts := strings.Split(r.Header.Get("DS"), ",")
		if len(parts) != 3 {
			t.Error("DS missing")
		} else if parts[2] != md5hex(fmt.Sprintf("salt=%s&t=%s&r=%s&b=%s&q=%s", PassportX4Salt, parts[0], parts[1], body, r.URL.RawQuery)) {
			t.Error("calendar signature differs from wire bytes")
		}
		_, _ = w.Write([]byte(`{"retcode":0,"data":{"act_list":[]}}`))
	}))
	defer server.Close()
	c := NewClient(server.URL)
	a := model.Account{Cookie: "synthetic"}
	role := model.RecordRole{UID: "100001", Region: "cn_gf01"}
	for _, game := range []string{"genshin", "starrail"} {
		if _, err := c.RecordCalendar(context.Background(), a, game, role); err != nil {
			t.Fatal(err)
		}
	}
}
