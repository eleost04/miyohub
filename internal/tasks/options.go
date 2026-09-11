package tasks

import (
	"errors"
	"fmt"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

// RunOptions only narrows the saved task configuration for this execution.
// It never enables a disabled feature or persists a temporary selection.
type RunOptions struct {
	Automatic bool     `json:"-"` // Internal scheduler only, never an API override.
	GamesOnly bool     `json:"games_only,omitempty"`
	BBSOnly   bool     `json:"bbs_only,omitempty"`
	Games     []string `json:"games,omitempty"`
}

func (o RunOptions) Validate() error {
	if o.GamesOnly && o.BBSOnly {
		return errors.New("游戏任务与米游币任务的仅执行选项不能同时启用")
	}
	if o.BBSOnly && len(o.Games) > 0 {
		return errors.New("仅执行米游币任务时不能再筛选游戏")
	}
	for _, key := range o.Games {
		if _, ok := mihoyo.Games[key]; !ok {
			return fmt.Errorf("不支持的游戏 %q", key)
		}
	}
	return nil
}

func (o RunOptions) apply(cfg model.Config) model.Config {
	if o.GamesOnly {
		cfg.Features.BBSTasks = false
	}
	if o.BBSOnly {
		cfg.Features.GameCheckin, cfg.Features.CloudGameCheckin = false, false
	}
	if len(o.Games) > 0 {
		selected := map[string]bool{}
		for _, key := range o.Games {
			selected[key] = true
		}
		filter := func(keys []string) []string {
			result := []string{}
			for _, key := range keys {
				if selected[key] {
					result = append(result, key)
				}
			}
			return result
		}
		cfg.Games.Enabled = filter(cfg.Games.Enabled)
		cfg.CloudGames.Enabled = filter(cfg.CloudGames.Enabled)
	}
	return cfg
}

func (o RunOptions) available(cfg model.Config) bool {
	cfg = o.apply(cfg)
	return (model.AccountTaskSettings{Features: cfg.Features, Games: cfg.Games, CloudGames: cfg.CloudGames, BBS: cfg.BBS}).HasTasks()
}
