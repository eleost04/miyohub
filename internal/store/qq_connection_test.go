package store

import (
	"testing"

	"github.com/eleost04/miyohub/internal/model"
)

func TestQQRuntimeStatusKeepsPreferencesAndRejectsStaleWorkers(t *testing.T) {
	s, admin, user := pushFixture(t)
	channel := model.PushChannel{Provider: "qqbot", AppID: "123456", ClientSecret: "fixture-secret", OpenID: "owner-openid", BindingState: "connecting"}
	if _, err := s.BindPushChannel(user.ID, "", 1, channel); err != nil {
		t.Fatal(err)
	}
	channel = s.PushConfigForUser(user.ID).Channels[0]
	revision := s.PushConfigForUser(user.ID).Revision
	if !s.UpdateQQConnection(user.ID, channel, "ready", "") {
		t.Fatal("owned QQ runtime status rejected")
	}
	config := s.PushConfigForUser(user.ID)
	if config.Revision != revision || config.Enabled || config.Channels[0].OpenID != "owner-openid" {
		t.Fatal("gateway modified notification preferences")
	}
	if s.UpdateQQConnection(admin.ID, channel, "ready", "") {
		t.Fatal("cross-user gateway status accepted")
	}
	stale := channel
	stale.ClientSecret = "stale-secret"
	if s.UpdateQQConnection(user.ID, stale, "ready", "") {
		t.Fatal("stale gateway credentials accepted")
	}
	other := pushPatch(s.PushSettings(admin.ID))
	channel.ID = ""
	other.Channels = []PushChannelPatch{{PushChannel: channel}}
	if _, err := s.UpdatePushSettings(admin.ID, other); err == nil {
		t.Fatal("manual config duplicated a bound bot across users")
	}
	p := pushPatch(s.PushSettings(user.ID))
	p.Channels = nil
	if _, err := s.UpdatePushSettings(user.ID, p); err != nil {
		t.Fatal(err)
	}
	if s.UpdateQQConnection(user.ID, stale, "reconnecting", "fixture") {
		t.Fatal("deleted bot was resurrected")
	}
}

func TestQQRuntimeFollowsCredentialsNotRecipientEdits(t *testing.T) {
	for _, change := range []string{"recipient", "secret", "app_id", "clear_secret"} {
		t.Run(change, func(t *testing.T) {
			s, _, user := pushFixture(t)
			channel := model.PushChannel{Provider: "qqbot", AppID: "123456", ClientSecret: "fixture-secret", OpenID: "owner-openid"}
			if _, err := s.BindPushChannel(user.ID, "", 1, channel); err != nil {
				t.Fatal(err)
			}
			channel = s.PushConfigForUser(user.ID).Channels[0]
			if !s.UpdateQQConnection(user.ID, channel, "ready", "") {
				t.Fatal("gateway did not become ready")
			}
			patch := pushPatch(s.PushSettings(user.ID))
			patch.Channels[0].Enabled = false
			switch change {
			case "recipient":
				patch.Channels[0].OpenID = "new-owner-openid"
			case "secret":
				patch.Channels[0].ClientSecret = "replacement-fixture-secret"
			case "app_id":
				patch.Channels[0].AppID = "654321"
			case "clear_secret":
				patch.Channels[0].ClearFields = []string{"client_secret"}
			}
			if _, err := s.UpdatePushSettings(user.ID, patch); err != nil {
				t.Fatal(err)
			}
			got := s.PushConfigForUser(user.ID).Channels[0]
			if (got.BindingState == "ready") != (change == "recipient") {
				t.Fatal("connection state did not follow credential changes")
			}
			if (change == "app_id" || change == "clear_secret") && got.ClientSecret != "" {
				t.Fatal("old secret survived an app change or explicit removal")
			}
		})
	}
}
