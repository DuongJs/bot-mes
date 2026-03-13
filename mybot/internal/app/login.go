package app

import (
	"mybot/internal/fblogin"
	"mybot/internal/media"
)

// hasCookies returns true if the cookies map has non-empty c_user and xs values.
func hasCookies(cookies map[string]string) bool {
	return cookies["c_user"] != "" && cookies["xs"] != ""
}

// doAutoLogin performs a Facebook login using credentials from config,
// then updates the config with fresh cookies and tokens, and saves to disk.
func (b *Bot) doAutoLogin() error {
	result, err := fblogin.Login(b.Cfg.AutoLogin.UID, b.Cfg.AutoLogin.Password, b.Cfg.AutoLogin.TwoFASecret)
	if err != nil {
		return err
	}

	b.Log.Info().
		Str("login_token", result.LoginToken[:20]+"...").
		Str("access_token", result.AccessToken[:20]+"...").
		Msg("Auto-login obtained tokens")

	if result.AccessToken != "" {
		media.SetFacebookToken(result.AccessToken)
	}

	return b.Cfg.UpdateCookies(result.CookieString, result.Cookies, result.LoginToken, result.AccessToken, b.ConfigPath)
}
