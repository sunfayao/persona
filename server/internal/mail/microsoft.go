package mail

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/nwverse/persona/server/internal/lazyre"
)

const (
	ProviderOutlook = "outlook" // personal Outlook.com and work / school Microsoft 365 mailboxes, signed in with Microsoft

	oauthMicrosoft  = "microsoft" // OAuthCreds.Provider
	microsoftScopes = "https://outlook.office.com/IMAP.AccessAsUser.All https://outlook.office.com/SMTP.Send offline_access openid email"
)

var (
	microsoftTokenURL = "https://login.microsoftonline.com/common/oauth2/v2.0/token"

	aadCode = lazyre.New(`AADSTS\d+`)
)

// userError is a mistake the user fixes (a client ID, a setting in Entra or Exchange), told in words for people:
// HTTP 400.
type userError struct {
	msg   string
	cause error // the server's own answer, if any: kept so isConnError still sees a protocol reply
}

func (e userError) Error() string { return e.msg }

func (e userError) Unwrap() error { return e.cause }

func userErr(msg string) error { return userError{msg: msg} }

func (s *Service) microsoftToken(ctx context.Context, form url.Values) (*tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, microsoftTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连不上 Microsoft 的登录服务：%w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("oauth: read token response: %w", err)
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("oauth: parse token response (HTTP %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK || tr.AccessToken == "" {
		return nil, microsoftError(tr.Error, tr.ErrorDesc)
	}
	return &tr, nil
}

// microsoftError describes a refresh failure for a stored Outlook account.
func microsoftError(code, desc string) error {
	aad := aadCode.FindString(desc)
	switch {
	case aad == "AADSTS65001" || aad == "AADSTS90094" || aad == "AADSTS90095" || aad == "AADSTS900941" || code == "consent_required":
		return userErr("你的工作账号不能自己授权第三方应用读邮箱，要公司的 IT 管理员同意：请管理员在 Microsoft Entra 里为这个应用「授予管理员同意」，" +
			"或者给你开通用户同意；也可以改用个人 Outlook 账号")
	case code == "invalid_grant" || aad == "AADSTS70008" || aad == "AADSTS700082" || aad == "AADSTS50173" ||
		aad == "AADSTS50076" || aad == "AADSTS50078" || aad == "AADSTS50079":
		return userErr("Outlook 的授权过期或被撤销了（改过密码、太久没用、或公司要求重新验证）")
	}
	msg := code
	if first, _, _ := strings.Cut(strings.TrimSpace(desc), "\n"); first != "" {
		msg += "：" + first
	}
	if msg == "" {
		msg = "未知错误"
	}
	return fmt.Errorf("Microsoft 登录失败：%s", msg)
}

// outlookHint explains Exchange's refusals of an Outlook account with a good token: IMAP or SMTP AUTH turned off
// for the mailbox or the whole company.
func outlookHint(err error) error {
	if err == nil {
		return nil
	}
	m := err.Error()
	switch {
	case strings.Contains(m, "SmtpClientAuthentication is disabled") || strings.Contains(m, "5.7.139") || strings.Contains(m, "5.7.3"):
		return fmt.Errorf("这个邮箱不允许用 SMTP 认证发信（公司关了 SMTP AUTH）：请 IT 管理员在 Microsoft 365 管理中心为你的邮箱打开"+
			"「经过身份验证的 SMTP」，收信不受影响（%w）", err)
	case strings.HasPrefix(m, "imap") && (strings.Contains(m, "AUTHENTICATE failed") || strings.Contains(m, "XOAUTH2")):
		return fmt.Errorf("Outlook 拒绝了 IMAP 登录，多半是这个邮箱关了 IMAP：个人账号在 outlook.com 的「设置 › 邮件 › 转发和 IMAP」里打开"+
			"「允许设备和应用使用 IMAP」；工作账号请 IT 管理员在 Exchange 管理中心为你的邮箱启用 IMAP（%w）", err)
	}
	return err
}
