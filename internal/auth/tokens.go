package auth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

// RefreshCookie renews the short-lived cookie token using the account's SToken.
func RefreshCookie(ctx context.Context, client *mihoyo.Client, account model.Account) (string, error) {
	if account.Stoken == "" || account.Stuid == "" {
		return "", errors.New("账号没有可用于续期的 SToken")
	}
	headers := http.Header{"Cookie": {"stuid=" + account.Stuid + ";stoken=" + account.Stoken + ";mid=" + account.Mid}, "User-Agent": {mihoyo.DefaultMobileUA}}
	var result map[string]any
	if err := client.JSON(ctx, http.MethodGet, mihoyo.TakumiAPI+"/auth/api/getCookieAccountInfoBySToken", nil, nil, headers, &result); err != nil {
		return "", err
	}
	token := text(dataMap(result["data"])["cookie_token"], "")
	if retcode(result) != 0 || token == "" || strings.ContainsAny(token, ";\r\n\x00") {
		return "", errors.New("Cookie 续期失败，将使用已保存凭据")
	}
	parts := []string{}
	for _, part := range strings.Split(account.Cookie, ";") {
		key, _, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && key != "cookie_token" && key != "cookie_token_v2" {
			parts = append(parts, strings.TrimSpace(part))
		}
	}
	return strings.Join(append(parts, "cookie_token="+token), ";"), nil
}

func loginAccount(ctx context.Context, client *mihoyo.Client, device model.Device, name, stuid, stoken, mid string) (model.Account, error) {
	account := model.Account{Device: device, Name: name, Stuid: stuid, Stoken: stoken, Mid: mid}
	if stuid == "" || stoken == "" || mid == "" || strings.ContainsAny(stuid+stoken+mid, ";\r\n\x00") {
		return account, errors.New("登录结果缺少有效的账号凭据")
	}
	headers := http.Header{"User-Agent": {mihoyo.DefaultMobileUA}, "X-Rpc-App_version": {"2.106.2"}, "X-Rpc-Client_type": {"5"}, "X-Requested-With": {"com.mihoyo.hyperion"}, "Referer": {"https://webstatic.mihoyo.com"}, "X-Rpc-Device_id": {device.ID}, "X-Rpc-Device_fp": {device.FP}, "Cookie": {"mid=" + mid + ";stoken=" + stoken}}
	query := url.Values{"stoken": {stoken}}
	var tokens [2]string
	for i, path := range []string{ltokenPath, cookieTokenPath} {
		if i == 1 {
			headers.Set("x-rpc-client_type", "2")
		}
		headers.Set("DS", mihoyo.DSX4(query.Encode(), ""))
		var result map[string]any
		if err := client.JSON(ctx, http.MethodGet, passportAPI+path, query, nil, headers, &result); err != nil {
			return account, err
		}
		key := []string{"ltoken", "cookie_token"}[i]
		tokens[i] = text(dataMap(result["data"])[key], "")
		if retcode(result) != 0 || tokens[i] == "" || strings.ContainsAny(tokens[i], ";\r\n\x00") {
			return account, errors.New("登录凭据换取失败，请重新登录")
		}
	}
	account.Cookie = "ltuid=" + stuid + ";ltoken=" + tokens[0] + ";account_id=" + stuid + ";cookie_token=" + tokens[1] + ";stuid=" + stuid + ";stoken=" + stoken + ";mid=" + mid
	return account, nil
}
