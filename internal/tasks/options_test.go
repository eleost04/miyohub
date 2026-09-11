package tasks

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

func TestRunOptionsValidationAndNarrowing(t *testing.T) {
	for _, o := range []RunOptions{{GamesOnly: true, BBSOnly: true}, {Games: []string{"invalid"}}, {BBSOnly: true, Games: []string{"genshin"}}} {
		if o.Validate() == nil {
			t.Fatal("invalid options accepted", o)
		}
	}
	cfg := model.Config{Features: model.Features{GameCheckin: true, CloudGameCheckin: true, BBSTasks: true}, Games: model.GamesConfig{Enabled: []string{"genshin", "starrail"}}, CloudGames: model.CloudGamesConfig{Enabled: []string{"genshin", "zzz"}}, BBS: model.BBSConfig{Forums: []int{2}, Checkin: true}}
	o := RunOptions{GamesOnly: true, Games: []string{"genshin", "genshin", "honkai2"}}
	filtered := o.apply(cfg)
	if !reflect.DeepEqual(filtered.Games.Enabled, []string{"genshin"}) || !reflect.DeepEqual(filtered.CloudGames.Enabled, []string{"genshin"}) || filtered.Features.BBSTasks {
		t.Fatal("task options did not narrow execution", filtered.Features)
	}
	if len(cfg.Games.Enabled) != 2 || !cfg.Features.BBSTasks {
		t.Fatal("filter mutated saved configuration")
	}
	cfg.Features.GameCheckin, cfg.Features.CloudGameCheckin = false, false
	if o.available(cfg) {
		t.Fatal("temporary selection enabled disabled task families")
	}
	if !(RunOptions{BBSOnly: true}).available(cfg) {
		t.Fatal("BBS-only selection lost enabled tasks")
	}
}

func TestRunnerFiltersGamesCloudAndBBSWithoutSavingSelection(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, _, err := s.CreateAdmin("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(u.ID, model.Account{Name: "one", Cookie: "test-cookie", CloudTokens: map[string]string{"genshin": "cloud-token", "zzz": "other-token"}}); err != nil {
		t.Fatal(err)
	}
	cfg := s.Config()
	cfg.Features = model.Features{GameCheckin: true, CloudGameCheckin: true, BBSTasks: true}
	cfg.Accounts[0].TaskSettings.Features = cfg.Features
	if err := s.ReplaceConfig(cfg); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(s)
	defer runner.Stop()
	mode, calls := "games", 0
	runner.newClient = func() *mihoyo.Client {
		client := mihoyo.NewClient("")
		client.HTTP.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
			calls++
			if mode == "bbs" && req.URL.Path == "/apihub/wapi/getUserMissionsState" {
				return reply(`{"retcode":0,"data":{"can_get_points":0}}`), nil
			}
			if mode == "games" {
				if req.URL.Path == mihoyo.AccountRolesPath && req.URL.Query().Get("game_biz") == "hk4e_cn" {
					return reply(`{"retcode":0,"data":{"list":[]}}`), nil
				}
				if req.URL.Path == "/hk4e_cg_cn/wallet/wallet/get" {
					return reply(`{"retcode":0,"data":{"free_time":{"free_time":30,"send_freetime":15}}}`), nil
				}
			}
			t.Error("selection reached an unselected task", req.URL.Path)
			return nil, errors.New("unselected request denied")
		})
		return client
	}
	if err := runner.RunWithOptions(context.Background(), nil, RunOptions{GamesOnly: true, Games: []string{"genshin"}}); err != nil {
		t.Fatal(err)
	}
	a := s.AccountsForUser(u.ID, false)[0]
	if calls != 2 || len(a.TaskResults) != 2 || a.TaskResults["games"].Skipped != 1 || a.TaskResults["cloud"].Success != 1 {
		t.Fatal("incorrect filtered results", calls, len(a.TaskResults))
	}
	mode, calls = "bbs", 0
	if err := runner.RunWithOptions(context.Background(), nil, RunOptions{BBSOnly: true}); err != nil {
		t.Fatal(err)
	}
	a = s.AccountsForUser(u.ID, false)[0]
	if calls != 1 || len(a.TaskResults) != 1 || a.TaskResults["bbs"].Skipped != 1 {
		t.Fatal("BBS-only selection ran other families")
	}
	after := s.Config()
	if !reflect.DeepEqual(after.Features, cfg.Features) || !reflect.DeepEqual(after.Games, cfg.Games) || !reflect.DeepEqual(after.CloudGames, cfg.CloudGames) {
		t.Fatal("run options were persisted")
	}
	if err := runner.StartWithOptions(context.Background(), nil, RunOptions{Games: []string{"not-a-game"}}); err == nil || runner.Running() {
		t.Fatal("invalid async selection reserved accounts")
	}
}
