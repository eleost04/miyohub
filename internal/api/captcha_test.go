package api

import (
	"testing"

	"github.com/eleost04/miyohub/internal/model"
)

func TestCaptchaPublicConfigurationIsWriteOnlyForSecrets(t *testing.T) {
	config := model.Config{Captcha: model.CaptchaConfig{Channels: []model.CaptchaChannel{{ID: "self", Provider: "custom", Token: "custom-secret", UserKey: "paid-secret", Endpoint: "http://captcha:9645/pass_nine"}}}}
	admin := publicConfigForUser(config, model.User{Role: "admin"})
	channel := admin.Captcha.Channels[0]
	if channel.Token != "" || channel.UserKey != "" || len(channel.Configured) != 2 || channel.Endpoint == "" {
		t.Fatal("admin captcha view is unsafe or missing endpoint")
	}
	user := publicConfigForUser(config, model.User{Role: "user"})
	if len(user.Captcha.Channels) != 0 {
		t.Fatal("ordinary user can inspect private endpoint or key")
	}
	if config.Captcha.Channels[0].Token != "custom-secret" {
		t.Fatal("redaction mutated stored secret")
	}
}
