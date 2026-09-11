package tasks

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

func TestCloudInitialCreditOrCapNeedsNoSecondRequest(t *testing.T) {
	for _, tc := range []struct {
		name, wallet             string
		success, skipped, failed int
	}{
		{"credited", `{"free_time":{"free_time":115,"send_freetime":15},"play_card":{"short_msg":"已开通"},"coin":{"coin_num":20}}`, 1, 0, 0},
		{"capped", `{"free_time":{"free_time":600,"send_freetime":0}}`, 0, 1, 0},
		{"missing wallet", `{}`, 0, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := mihoyo.NewClient("")
			calls := 0
			client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Referer") != "https://zzz.mihoyo.com/" || !strings.Contains(r.Header.Get("User-Agent"), "Mozilla") || r.Header.Get("x-rpc-combo_token") != "cloud" {
					t.Error("cloud request lost its game identity")
				}
				return reply(`{"retcode":0,"data":` + tc.wallet + `}`), nil
			})
			details := []string{}
			result := (CloudCheckin{Client: client, Config: model.Config{CloudGames: model.CloudGamesConfig{Enabled: []string{"zzz"}}}, Account: model.Account{CloudTokens: map[string]string{"zzz": "cloud"}}, Emit: func(s string) { details = append(details, s) }}).Run(context.Background())
			if calls != 1 || result.Success != tc.success || result.Skipped != tc.skipped || result.Failed != tc.failed {
				t.Fatal("incorrect wallet confirmation", result, calls)
			}
			if len(details) < 2 || !strings.Contains(details[0], "云·绝区零") {
				t.Fatal("cloud result omitted localized details")
			}
		})
	}
}
