// Package gamerecord provides on-demand, owner-local record snapshots. It has
// no background upstream polling, credential refresh or device registration.
package gamerecord

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

type entry struct {
	identity               [32]byte
	epoch                  uint64
	busy                   bool
	cancel                 context.CancelFunc
	cache                  map[string]model.RecordSnapshot
	next, blocked, touched time.Time
	failure                *mihoyo.RecordError
}
type Service struct {
	client   *mihoyo.Client
	mu       sync.Mutex
	entries  map[string]*entry
	active   int
	stopped  bool
	now      func() time.Time
	interval time.Duration
}

func New(client *mihoyo.Client) *Service {
	return &Service{client: client, entries: map[string]*entry{}, now: time.Now, interval: 3 * time.Second}
}
func identity(a model.Account) [32]byte {
	raw, _ := json.Marshal([]any{a.ID, a.UserID, a.Stuid, a.Cookie, a.Device, a.Disabled})
	defer clear(raw)
	return sha256.Sum256(raw)
}
func SameIdentity(a, b model.Account) bool { return identity(a) == identity(b) }

func (s *Service) Invalidate(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.entries[id]; e != nil {
		e.epoch++
		clear(e.cache)
		if e.cancel != nil {
			e.cancel()
		}
		// Keep account-wide cooldowns across credential changes.
	}
}
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	for _, e := range s.entries {
		if e.cancel != nil {
			e.cancel()
		}
		clear(e.cache)
	}
	clear(s.entries)
}

func (s *Service) read(ctx context.Context, a model.Account, key string, ttl time.Duration, load func(context.Context) (model.RecordSnapshot, error)) (model.RecordSnapshot, error) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return model.RecordSnapshot{}, context.Canceled
	}
	now := s.now()
	e := s.entries[a.ID]
	if e == nil {
		for id, old := range s.entries {
			if !old.busy && now.Sub(old.touched) > 24*time.Hour && !old.blocked.After(now) {
				delete(s.entries, id)
			}
		}
		if len(s.entries) >= 512 {
			s.mu.Unlock()
			return model.RecordSnapshot{Status: "busy", Message: "查询缓存暂忙，请稍后再试"}, nil
		}
		e = &entry{cache: map[string]model.RecordSnapshot{}}
		s.entries[a.ID] = e
	}
	e.touched = now
	if fingerprint := identity(a); e.identity != fingerprint {
		e.identity = fingerprint
		e.epoch++
		clear(e.cache)
		if e.cancel != nil {
			e.cancel()
		}
	}
	cached, exists := e.cache[key]
	if e.blocked.After(now) {
		cached.Cached, cached.Stale, cached.Status, cached.Message, cached.RefreshAt = exists, exists, e.failure.Kind, e.failure.Message, e.blocked
		s.mu.Unlock()
		return cached, nil
	}
	if exists && now.Before(cached.ObservedAt.Add(ttl)) {
		cached.Cached = true
		s.mu.Unlock()
		return cached, nil
	}
	if e.busy || s.active >= 8 {
		cached.Cached, cached.Stale, cached.Status, cached.Message, cached.RefreshAt = exists, exists, "busy", "已有查询正在处理，请稍后再试", now.Add(s.interval)
		s.mu.Unlock()
		return cached, nil
	}
	e.busy = true
	s.active++
	requestCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	e.cancel = cancel
	epoch, delay := e.epoch, e.next.Sub(now)
	s.mu.Unlock()
	defer cancel()
	var result model.RecordSnapshot
	var err error
	if delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-requestCtx.Done():
			err = requestCtx.Err()
		case <-timer.C:
		}
		timer.Stop()
	}
	if err == nil {
		result, err = load(requestCtx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e.busy = false
	e.cancel = nil
	s.active--
	now = s.now()
	e.next = now.Add(s.interval)
	if s.stopped || e.epoch != epoch || requestCtx.Err() != nil {
		if ctx.Err() != nil {
			return model.RecordSnapshot{}, ctx.Err()
		}
		if e.epoch != epoch || s.stopped {
			return model.RecordSnapshot{}, context.Canceled
		}
		// Timeout, unlike explicit cancellation, must also cool down subsequent reads.
		err = &mihoyo.RecordError{Kind: "unavailable", Message: "查询超时，稍后可手动重试", Cooldown: 2 * time.Minute}
	}
	if err != nil {
		var failure *mihoyo.RecordError
		if !errors.As(err, &failure) {
			failure = &mihoyo.RecordError{Kind: "unavailable", Message: "查询未完成，稍后可手动重试", Cooldown: 2 * time.Minute}
		}
		e.failure, e.blocked = failure, now.Add(max(failure.Cooldown, s.interval))
		cached.Cached, cached.Stale, cached.Status, cached.Message, cached.RefreshAt = exists, exists, failure.Kind, failure.Message, e.blocked
		return cached, nil
	}
	result.Status, result.ObservedAt, result.RefreshAt = "ok", now, now.Add(ttl)
	if len(e.cache) >= 64 { // bounded by verified roles, never arbitrary client keys.
		oldestKey := ""
		oldest := now
		for k, v := range e.cache {
			if v.ObservedAt.Before(oldest) {
				oldest, oldestKey = v.ObservedAt, k
			}
		}
		delete(e.cache, oldestKey)
	}
	e.cache[key] = result
	return result, nil
}

func (s *Service) roles(ctx context.Context, a model.Account, game, uid, region string) (model.RecordSnapshot, *model.RecordRole, error) {
	if !mihoyo.RecordGame(game) || (uid != "" || region != "") && !mihoyo.RecordRoleParameters(uid, region) {
		return model.RecordSnapshot{}, nil, errors.New("游戏或角色参数无效")
	}
	result, err := s.read(ctx, a, "roles:"+game, 15*time.Minute, func(ctx context.Context) (model.RecordSnapshot, error) {
		roles, err := s.client.RecordRoles(ctx, a, game)
		return model.RecordSnapshot{Roles: roles}, err
	})
	result.Game = game
	if err != nil || result.Status != "ok" && (!result.Cached || len(result.Roles) == 0) {
		return result, nil, err
	}
	if len(result.Roles) == 0 {
		result.Status, result.Message = "no_role", "此账号未绑定该游戏角色"
		return result, nil, nil
	}
	if uid == "" {
		role := result.Roles[0]
		return result, &role, nil
	}
	for _, role := range result.Roles {
		if role.UID == uid && role.Region == region {
			return result, &role, nil
		}
	}
	return model.RecordSnapshot{}, nil, errors.New("角色不存在或不属于所选账号")
}

func (s *Service) Note(ctx context.Context, a model.Account, game, uid, region string) (model.RecordSnapshot, error) {
	roles, role, err := s.roles(ctx, a, game, uid, region)
	if err != nil || role == nil {
		return roles, err
	}
	result, err := s.read(ctx, a, "note:"+game+":"+role.UID+":"+role.Region, 3*time.Minute, func(ctx context.Context) (model.RecordSnapshot, error) {
		note, err := s.client.RecordNote(ctx, a, game, *role)
		return model.RecordSnapshot{Note: &note}, err
	})
	result.Game, result.Roles, result.Role = game, roles.Roles, role
	return result, err
}
