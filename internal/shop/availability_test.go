package shop

import (
	"strings"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

func TestBookedSaleIsNotComparedWithNextRound(t *testing.T) {
	opening := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC).Unix()
	next := opening + 7*24*60*60
	good := normalizeGood(map[string]any{
		"goods_id": "fixture", "type": 1, "status": "online", "total": 0, "next_num": 4,
		"sale_start_time": opening, "next_time": next, "now_time": opening + 1,
	})
	if int64(intValue(good["sale_start_time"])) != opening || int64(intValue(good["next_time"])) != next {
		t.Fatal("normalization lost sale rounds")
	}
	p := model.ExchangePlan{Auto: true, ExchangeAt: opening}
	err := validateAvailability(good, p, false, true)
	if err == nil || !strings.Contains(err.Error(), "本期库存已兑完") || strings.Contains(err.Error(), "早于") {
		t.Fatal("a sold-out booked round was mistaken for an early booking", err)
	}
	if p.ExchangeAt != opening {
		t.Fatal("reservation silently moved to another round")
	}
	if err := validateAvailability(good, p, false, false); err == nil || !strings.Contains(err.Error(), "早于") {
		t.Fatal("new booking could precede the next available round", err)
	}
	p.ExchangeAt = next
	if err := validateAvailability(good, p, false, false); err != nil {
		t.Fatal("valid next-round booking rejected", err)
	}
}

func TestScheduledPreparationAllowsBookedUpcomingRound(t *testing.T) {
	opening := time.Now().Add(time.Minute).Unix()
	good := normalizeGood(map[string]any{"status": "offline", "total": 0, "next_num": 4, "sale_start_time": opening - 604800, "next_time": opening, "now_time": opening - 180})
	p := model.ExchangePlan{Auto: true, ExchangeAt: opening}
	if err := validateAvailability(good, p, false, true); err != nil {
		t.Fatal("upcoming round could not prepare", err)
	}
	good["exchange_timestamp"] = opening + 3600
	good["sale_start_time"] = opening + 3600
	if err := validateAvailability(good, p, false, true); err == nil || !strings.Contains(err.Error(), "已变为") {
		t.Fatal("upstream postponement was not explained", err)
	}
}
