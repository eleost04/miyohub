package auth

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

type aigisVerification struct {
	Version                            int
	GT, Challenge, RiskType, SessionID string
	NewCaptcha                         bool
}

var captchaRiskPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var captchaOutputPattern = regexp.MustCompile(`^[A-Za-z0-9_+/=.-]+$`)
var captchaTimePattern = regexp.MustCompile(`^[0-9]{1,20}$`)

func aigisObject(value any) map[string]any {
	if raw, ok := value.(string); ok {
		var result map[string]any
		if json.Unmarshal([]byte(raw), &result) == nil {
			return result
		}
		return nil
	}
	return dataMap(value)
}

// AIGIS sends JSON or JSON-encoded data. A V3 challenge contains gt/challenge;
// V4 instead contains gt (or captcha_id) and optionally risk_type. Missing the
// V3 challenge is therefore not, by itself, an unsupported response.
func parseAigis(raw string) (aigisVerification, error) {
	invalid := errors.New("安全验证参数不完整或格式异常，请重新获取验证码")
	var envelope map[string]any
	if len(raw) > 32<<10 || json.Unmarshal([]byte(raw), &envelope) != nil {
		return aigisVerification{}, invalid
	}
	payload := aigisObject(envelope["data"])
	if len(payload) == 0 {
		payload = aigisObject(envelope["mmt_data"])
	}
	if nested := aigisObject(payload["mmt_data"]); len(nested) != 0 {
		payload = nested
	}
	v := aigisVerification{GT: text(payload["gt"], text(payload["captcha_id"], "")), Challenge: text(payload["challenge"], ""), SessionID: text(envelope["session_id"], ""), NewCaptcha: true}
	if !captchaAnswerPattern.MatchString(v.GT) || v.SessionID == "" || len(v.SessionID) > 512 || strings.ContainsAny(v.SessionID, ";\r\n\x00") {
		return aigisVerification{}, invalid
	}
	if value, ok := payload["new_captcha"].(bool); ok {
		v.NewCaptcha = value
	}
	if v.Challenge != "" {
		if !captchaAnswerPattern.MatchString(v.Challenge) {
			return aigisVerification{}, invalid
		}
		v.Version = 3
		return v, nil
	}
	v.Version, v.RiskType = 4, text(payload["risk_type"], "")
	if v.RiskType != "" && !captchaRiskPattern.MatchString(v.RiskType) {
		return aigisVerification{}, invalid
	}
	return v, nil
}

// Only explicitly validated proof fields are forwarded. A browser cannot
// override the upstream session, phone, endpoint or original request body.
func smsCaptchaAnswer(pending SMSChallenge, solution SMSCaptchaSolution) ([]byte, error) {
	invalid := errors.New("人机验证结果格式无效或与当前验证不匹配，请重新验证")
	if pending.Version == 4 {
		if solution.Challenge != "" || solution.Validate != "" || solution.CaptchaID != pending.GT || !captchaAnswerPattern.MatchString(solution.LotNumber) || !captchaAnswerPattern.MatchString(solution.PassToken) || len(solution.CaptchaOutput) > 8192 || !captchaOutputPattern.MatchString(solution.CaptchaOutput) || !captchaTimePattern.MatchString(solution.GenTime) {
			return nil, invalid
		}
		return json.Marshal(map[string]string{"captcha_id": pending.GT, "lot_number": solution.LotNumber, "captcha_output": solution.CaptchaOutput, "pass_token": solution.PassToken, "gen_time": solution.GenTime})
	}
	if !captchaAnswerPattern.MatchString(solution.Challenge) || !captchaAnswerPattern.MatchString(solution.Validate) || solution.CaptchaID != "" || solution.LotNumber != "" || solution.CaptchaOutput != "" || solution.PassToken != "" || solution.GenTime != "" {
		return nil, invalid
	}
	return json.Marshal(map[string]string{"geetest_challenge": solution.Challenge, "geetest_validate": solution.Validate, "geetest_seccode": solution.Validate + "|jordan"})
}
