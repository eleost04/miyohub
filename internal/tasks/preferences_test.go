package tasks

import (
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

func TestScheduledAndManualRunsRespectEachAccountsSavedChoices(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, _, _ := s.CreateAdmin("admin", "test-password")
	for _, name := range []string{"auto", "manual", "off", "custom"} {
		if err := s.AddAccountForUser(u.ID, model.Account{Name: name, Cookie: name}); err != nil {
			t.Fatal(err)
		}
	}
	accounts := s.AccountsForUser(u.ID, false)
	for i, a := range accounts {
		p := *a.TaskSettings
		p.Automatic = i != 1
		p.Games.Enabled = []string{"zzz"}
		if i == 1 {
			p.Games.Enabled = []string{"genshin"}
		}
		if i == 2 {
			p.Features = model.Features{}
		}
		if i == 3 {
			p.Schedule = &model.AccountSchedule{Time: "03:17", Timezone: "UTC"}
		}
		if _, err := s.UpdateAccountTasks(u, a.ID, p); err != nil {
			t.Fatal(err)
		}
	}
	r := NewRunner(s)
	defer r.Stop()
	var requests []string
	r.newClient = func() *mihoyo.Client {
		c := mihoyo.NewClient("")
		c.HTTP.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != mihoyo.AccountRolesPath {
				return nil, errors.New("unexpected action")
			}
			requests = append(requests, req.Header.Get("Cookie")+":"+req.URL.Query().Get("game_biz"))
			return reply(`{"retcode":0,"data":{"list":[]}}`), nil
		})
		return c
	}
	if err := r.RunScheduled(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(requests, []string{"auto:nap_cn"}) {
		t.Fatal("schedule ignored automatic opt-out or account game choice", requests)
	}
	requests = nil
	if err := r.Run(t.Context(), []string{accounts[1].ID}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(requests, []string{"manual:hk4e_cn"}) {
		t.Fatal("automatic opt-out also disabled manual sign-in", requests)
	}
	requests = nil
	if err := r.Run(t.Context(), []string{accounts[2].ID}); err == nil || len(requests) != 0 {
		t.Fatal("empty task preferences ran a task")
	}
	// A manual run for one account must not block other users' automatic work.
	_, reserved, release, err := r.begin(t.Context(), []string{accounts[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.RunScheduled(t.Context()); err != nil || len(requests) != 0 {
		t.Fatal("busy account blocked an otherwise empty schedule")
	}
	r.end(reserved, release)
}
