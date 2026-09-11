package shop

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

func TestShopDeviceFingerprintIncludesWebEnvironmentWithoutAccountCookie(t *testing.T) {
	client := mihoyo.NewClient("")
	client.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != mihoyo.DeviceFPURL || r.Method != "POST" || r.Header.Get("Cookie") != "" {
			t.Fatal("fingerprint request crossed the account boundary")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		seedTime, _ := strconv.ParseInt(text(body["seed_time"], ""), 10, 64)
		if body["device_id"] != "account-device" || body["platform"] != "5" || body["app_name"] != "account_cn" || time.Since(time.UnixMilli(seedTime)).Abs() > time.Minute {
			t.Error("fingerprint omitted its stable device or millisecond timestamp")
		}
		var fields map[string]any
		if err := json.Unmarshal([]byte(text(body["ext_fields"], "")), &fields); err != nil {
			t.Fatal("missing JSON-encoded fingerprint environment")
		}
		for _, key := range []string{"userAgent", "browserScreenSize", "maxTouchPoints", "isTouchSupported", "browserLanguage", "browserPlat", "browserTimeZone", "webGlRender", "webGlVendor", "numOfPlugins", "listOfPlugins", "screenRatio", "deviceMemory", "hardwareConcurrency", "cpuClass", "ifNotTrack", "ifAdBlock", "hasLiedResolution", "hasLiedOs", "hasLiedBrowser"} {
			if _, ok := fields[key]; !ok {
				t.Error("missing environment field", key)
			}
		}
		if fields["userAgent"] != mihoyo.DefaultMobileUA || fields["browserTimeZone"] != "Asia/Shanghai" {
			t.Error("fingerprint profile was inconsistent")
		}
		return response(`{"retcode":0,"data":{"device_fp":"fixture-fp"}}`), nil
	})
	service := Service{Client: client, Config: model.Config{Device: model.Device{ID: "site-device"}}, Account: model.Account{Cookie: "private-fixture-cookie", Device: model.Device{ID: "ACCOUNT-DEVICE"}}}
	if fp, err := service.DeviceFP(t.Context()); err != nil || fp != "fixture-fp" {
		t.Fatal("fingerprint did not complete", err)
	}
}

func TestShopExchangeUsesAccountIdentityAndReferencePayloads(t *testing.T) {
	for _, kind := range []string{"physical", "virtual"} {
		t.Run(kind, func(t *testing.T) {
			client := mihoyo.NewClient("")
			client.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != mihoyo.MallExchangePath || r.Method != "POST" {
					t.Fatal("wrong exchange endpoint")
				}
				for key, want := range map[string]string{
					"Cookie": "account-fixture-cookie", "x-rpc-device_id": "account-device", "x-rpc-device_fp": "fixture-fp",
					"x-rpc-device_name": "Fixture Device", "x-rpc-device_model": "Fixture Model", "x-rpc-sys_version": "12",
					"x-rpc-app_version": "2.106.2", "x-rpc-client_type": "1", "x-rpc-channel": "appstore", "x-rpc-verify_key": "bll8iq97cem8",
					"Origin": "https://webstatic.miyoushe.com", "Referer": "https://webstatic.miyoushe.com/", "Accept-Language": "zh-CN,zh-Hans;q=0.9",
				} {
					if r.Header.Get(key) != want {
						t.Error("exchange protocol mismatch", key)
					}
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body["app_id"] != float64(1) || body["point_sn"] != "myb" || body["goods_id"] != "fixture-good" || body["exchange_num"] != float64(1) {
					t.Error("incorrect exchange body")
				}
				if kind == "physical" && (body["address_id"] != "fixture-address" || body["uid"] != nil || body["game_biz"] != nil) {
					t.Error("physical exchange had incorrect delivery fields")
				}
				if kind == "virtual" && (body["uid"] != "100001" || body["region"] != "fixture-region" || body["game_biz"] != "hkrpg_cn" || body["address_id"] != nil) {
					t.Error("virtual exchange had incorrect role fields")
				}
				return response(`{"retcode":0,"data":{"order_id":"upstream-private-order"}}`), nil
			})
			service := Service{Client: client, Config: model.Config{Device: model.Device{ID: "wrong-site-device"}}, Account: model.Account{Cookie: "account-fixture-cookie", Device: model.Device{ID: "account-device", Name: "Fixture Device", Model: "Fixture Model"}}}
			plan := model.ExchangePlan{GoodsID: "fixture-good", DeviceFP: "fixture-fp"}
			if kind == "physical" {
				plan.AddressID = "fixture-address"
			} else {
				plan.UID, plan.Region, plan.GameBiz = "100001", "fixture-region", "hkrpg_cn"
			}
			result, err := service.Exchange(t.Context(), plan)
			if err != nil || result["ok"] != true {
				t.Fatal("exchange fixture failed", err)
			}
			encoded, _ := json.Marshal(result)
			if strings.Contains(string(encoded), "upstream-private-order") {
				t.Error("unneeded private exchange payload was retained")
			}
		})
	}
}

func TestShopReadProfilesKeepEndpointOriginAndOwnerIdentity(t *testing.T) {
	for _, path := range []string{mihoyo.MallGoodsPath, mihoyo.MallDetailPath, mihoyo.MallPointPath, mihoyo.MallAddressPath, mihoyo.AccountRolesPath} {
		t.Run(path, func(t *testing.T) {
			client := mihoyo.NewClient("")
			client.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
				origin := "https://user.mihoyo.com"
				if path == mihoyo.MallPointPath || path == mihoyo.AccountRolesPath {
					origin = "https://webstatic.mihoyo.com"
				}
				if r.Method != "GET" || r.URL.Path != path || r.URL.Host != "api-takumi.mihoyo.com" || r.Header.Get("Origin") != origin || r.Header.Get("Referer") != origin+"/" || r.Header.Get("x-rpc-client_type") != "5" || r.Header.Get("x-rpc-device_id") != "owner-device" || r.Header.Get("Cookie") != "owner-fixture-cookie" || r.Header.Get("Accept-Language") != "zh-CN,zh-Hans;q=0.9" {
					t.Error("shop read profile did not match its endpoint/owner")
				}
				return response(`{"retcode":0,"data":{"goods_id":"fixture","points":100,"list":[]}}`), nil
			})
			service := Service{Client: client, Account: model.Account{Cookie: "owner-fixture-cookie", Device: model.Device{ID: "owner-device"}}}
			var err error
			switch path {
			case mihoyo.MallGoodsPath:
				_, err = service.Goods(t.Context(), "")
			case mihoyo.MallDetailPath:
				_, err = service.GoodDetail(t.Context(), "fixture")
			case mihoyo.MallPointPath:
				_, err = service.Points(t.Context())
			case mihoyo.MallAddressPath:
				_, err = service.Addresses(t.Context())
			case mihoyo.AccountRolesPath:
				_, err = service.Roles(t.Context(), "hkrpg_cn")
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
