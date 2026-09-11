package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/notify"
	"github.com/eleost04/miyohub/internal/store"
)

func (s *Server) pushConfig(w http.ResponseWriter, r *http.Request, user model.User) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": s.store.PushSettings(user.ID)})
	case http.MethodPut:
		var patch store.PushSettingsPatch
		if !decodeJSON(w, r, &patch) {
			return
		}
		result, err := s.store.UpdatePushSettings(user.ID, patch)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, store.ErrPushConflict) {
				status = http.StatusConflict
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": result})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) pushTest(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		ChannelID string `json:"channel_id"`
		Revision  int    `json:"revision"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.ChannelID == "" {
		writeError(w, http.StatusBadRequest, errors.New("请选择单个已保存渠道测试"))
		return
	}
	if !s.limits.allow("push-test:"+user.ID, 6, time.Minute) {
		rateError(w)
		return
	}
	cfg := s.store.PushConfigForUser(user.ID)
	if cfg.Revision != input.Revision {
		writeError(w, http.StatusConflict, store.ErrPushConflict)
		return
	}
	selected := []model.PushChannel{}
	for _, channel := range cfg.Channels {
		if input.ChannelID != "" && channel.ID != input.ChannelID || input.ChannelID == "" && !channel.Enabled {
			continue
		}
		if err := notify.ValidateChannel(channel, true); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		channel.Enabled = true // An explicit test is allowed while automatic notifications are off.
		selected = append(selected, channel)
	}
	if len(selected) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("没有可测试的已保存渠道，请先配置并保存"))
		return
	}
	cfg.Enabled, cfg.ErrorOnly, cfg.Channels = true, false, selected
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	results := []notify.Result{}
	allOK := true
	for _, channel := range selected {
		// Prevent a stale test from sending to later channels after a concurrent settings edit.
		if s.store.PushConfigForUser(user.ID).Revision != input.Revision {
			results = append(results, notify.Result{ChannelID: channel.ID, Name: channel.Name, Provider: channel.Provider, Error: "配置已变化，已跳过测试，请重新加载"})
			allOK = false
			continue
		}
		one := cfg
		one.Channels = []model.PushChannel{channel}
		for _, result := range s.pushSender.Send(ctx, one, "MiyoHub · 推送测试", "这是一条手动测试消息。收到它说明当前渠道可以接收通知。\n自动通知是否启用，请以 MiyoHub 消息推送为准。", true) {
			message := notify.ProviderName(result.Provider) + " 测试：服务已接受通知，请检查接收端"
			if !result.OK {
				allOK = false
				message = notify.ProviderName(result.Provider) + " 测试：" + result.Error
			}
			_ = s.store.AddLogForUser(user.ID, "push", message)
			_ = s.store.RecordPushTest(user.ID, channel, result)
			results = append(results, result)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": map[string]any{"all_ok": allOK, "results": results}})
}
