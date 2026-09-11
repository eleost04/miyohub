package shop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/eleost04/miyohub/internal/mihoyo"
)

func TestNormalizeGoodStatusAndStock(t *testing.T) {
	good := normalizeGood(map[string]any{
		"goods_id":             "gift-1",
		"goods_name":           "测试礼包",
		"status":               "online",
		"price":                float64(100),
		"total":                float64(7),
		"unlimit":              false,
		"account_cycle_limit":  float64(2),
		"account_exchange_num": float64(1),
		"account_cycle_type":   "month",
	})
	if good["display_status"] != "online" || good["stock"] != "7" || good["exchange_time"] != "正在兑换" {
		t.Fatalf("unexpected online good: %#v", good)
	}
	if good["limit"] != "每月 1/2" {
		t.Fatalf("unexpected limit: %v", good["limit"])
	}
}

func TestNormalizeGoodScheduledSoldOut(t *testing.T) {
	good := normalizeGood(map[string]any{
		"goods_id":   "gift-2",
		"goods_name": "下次礼包",
		"status":     "not_in_sell",
		"total":      float64(0),
		"next_time":  float64(1790000000),
	})
	if good["display_status"] != "sold_out_with_next" || good["sold_out"] != true {
		t.Fatalf("unexpected scheduled good: %#v", good)
	}
	if good["exchange_timestamp"] != 1790000000 {
		t.Fatalf("unexpected exchange timestamp: %v", good["exchange_timestamp"])
	}
}

func TestCatalogPaginationPassesFivePagesAndStopsOnDuplicates(t *testing.T) {
	for _, repeated := range []bool{false, true} {
		t.Run(fmt.Sprint(repeated), func(t *testing.T) {
			calls := 0
			client := mihoyo.NewClient("")
			client.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				if repeated {
					page = 1
				}
				items := []map[string]any{}
				for n := 0; n < 20; n++ {
					items = append(items, map[string]any{"goods_id": fmt.Sprintf("g-%d-%d", page, n), "type": 2, "price": 10})
				}
				raw, err := json.Marshal(map[string]any{"retcode": 0, "data": map[string]any{"list": items, "has_more": page < 6}})
				if err != nil {
					t.Fatal(err)
				}
				return response(string(raw)), nil
			})
			result, err := (Service{Client: client}).Goods(context.Background(), "")
			if err != nil {
				t.Fatal(err)
			}
			wantCalls, wantGoods := 6, 120
			if repeated {
				wantCalls, wantGoods = 2, 20
			}
			if calls != wantCalls || len(result["goods"].([]map[string]any)) != wantGoods {
				t.Fatal("catalog pagination lost goods or repeated pages", calls, len(result["goods"].([]map[string]any)))
			}
		})
	}
}

func TestCatalogAllAliasUsesWorkingUpstreamQuery(t *testing.T) {
	client := mihoyo.NewClient("")
	client.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("game") != "" {
			t.Error("unsupported all key sent upstream")
		}
		return response(`{"retcode":0,"data":{"list":[{"goods_id":"physical","type":1},{"goods_id":"virtual","type":2}],"has_more":false}}`), nil
	})
	result, err := (Service{Client: client}).Goods(t.Context(), "all")
	if err != nil || len(result["goods"].([]map[string]any)) != 2 {
		t.Fatal("all-goods compatibility lost physical or virtual goods", err)
	}
}
