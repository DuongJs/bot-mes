// Package fblogin provides automated Facebook login via the mobile API,
// returning session cookies that can be used by messagix.
package fblogin

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
)

const (
	bloksVersioningID = "afc4edd2a11a4765752ba5790e7db32bd43289d9974f196d1c8f962607950160"
	stylesID          = "a32f9cf6373bc5b20b1a2fb57d1c8447"
	loginAppID        = "com.bloks.www.bloks.caa.login.async.send_login_request"
	twoFAAppID        = "com.bloks.www.two_step_verification.verify_code.async"
	loginDocID        = "119940804211646733019770319568"
	bgraphURL         = "https://b-graph.facebook.com/graphql"
	pwdKeyFetchURL    = "https://graph.facebook.com/pwd_key_fetch"
	sessionForAppURL  = "https://api.facebook.com/method/auth.getSessionforApp"
	sessionAppID      = "275254692598279"
)

// ── Helpers ────────────────────────────────────────────────────────────────────

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

func randomHex(n int) string { return hex.EncodeToString(randomBytes(n)) }

func randomAlphaNum(n int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		num, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[num.Int64()]
	}
	return string(b)
}

func randomChoice(choices []string) string {
	num, _ := rand.Int(rand.Reader, big.NewInt(int64(len(choices))))
	return choices[num.Int64()]
}

func newUUID() string { return uuid.New().String() }

// ── Password encryption ────────────────────────────────────────────────────────

type pwdKeyResp struct {
	PublicKey string `json:"public_key"`
	KeyID     int    `json:"key_id"`
}

func fetchPwdKey() (*pwdKeyResp, error) {
	params := url.Values{}
	params.Set("app_version", "412474635")
	params.Set("fb_api_caller_class", "FBPasswordEncryptionKeyFetchRequest")
	params.Set("fb_api_req_friendly_name", "FBPasswordEncryptionKeyFetchRequest:networkRequest")
	params.Set("flow", "controller_initialization")
	params.Set("format", "json")
	params.Set("locale", "vi_VN")
	params.Set("pretty", "0")
	params.Set("sdk", "ios")
	params.Set("sdk_version", "3")
	params.Set("version", "2")

	req, err := http.NewRequest("GET", pwdKeyFetchURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-fb-privacy-context", "0xf0000000b659eb4a")
	req.Header.Set("user-agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 13_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/17H35 [FBAN/FBIOS;FBAV/390.1.0.38.101]")
	req.Header.Set("x-fb-connection-type", "wifi.CTRadioAccessTechnologyLTE")
	req.Header.Set("x-fb-sim-hni", "45202")
	req.Header.Set("authorization", "OAuth 6628568379|c1e620fa708a1d5696fb991c1bde5662")
	req.Header.Set("x-tigon-is-retry", "False")
	req.Header.Set("x-fb-friendly-name", "FBPasswordEncryptionKeyFetchRequest:networkRequest")
	req.Header.Set("x-fb-http-engine", "Liger")
	req.Header.Set("x-fb-client-ip", "True")
	req.Header.Set("x-fb-server-cluster", "True")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("pwd_key_fetch read body: %w", err)
	}
	var result pwdKeyResp
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("pwd_key_fetch parse error: %w", err)
	}
	return &result, nil
}

func encryptPassword(password string) (string, error) {
	keyData, err := fetchPwdKey()
	if err != nil {
		return "", fmt.Errorf("pwd_key_fetch failed: %w", err)
	}

	block, _ := pem.Decode([]byte(keyData.PublicKey))
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block")
	}
	pubKeyIface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse public key: %w", err)
	}
	rsaPub, ok := pubKeyIface.(*rsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("not an RSA public key")
	}

	randomKey := randomBytes(32)
	randomIV := randomBytes(12)
	currentTime := fmt.Sprintf("%d", time.Now().Unix())

	encRandKey, err := rsa.EncryptPKCS1v15(rand.Reader, rsaPub, randomKey)
	if err != nil {
		return "", fmt.Errorf("RSA encrypt failed: %w", err)
	}

	aesBlock, err := aes.NewCipher(randomKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(aesBlock)
	if err != nil {
		return "", err
	}
	encrypted := gcm.Seal(nil, randomIV, []byte(password), []byte(currentTime))
	authTag := encrypted[len(encrypted)-16:]
	encryptedPass := encrypted[:len(encrypted)-16]

	var payload bytes.Buffer
	payload.Write([]byte{1, byte(keyData.KeyID)})
	payload.Write(randomIV)
	lenBuf := make([]byte, 2)
	binary.LittleEndian.PutUint16(lenBuf, uint16(len(encRandKey)))
	payload.Write(lenBuf)
	payload.Write(encRandKey)
	payload.Write(authTag)
	payload.Write(encryptedPass)

	encoded := base64.StdEncoding.EncodeToString(payload.Bytes())
	return fmt.Sprintf("#PWD_WILDE:2:%s:%s", currentTime, encoded), nil
}

// ── API client ─────────────────────────────────────────────────────────────────

type api struct {
	uid       string
	password  string
	twoFACode string
	deviceID  string
	machineID string
	nonceB64  string
	hni       string
	client    *http.Client
}

func newAPI(uid, password, twoFACode string) *api {
	jar, _ := cookiejar.New(nil)

	deviceID := newUUID()
	machineID := randomAlphaNum(24)

	nonceData := map[string]string{
		"challenge_nonce": base64.StdEncoding.EncodeToString(randomBytes(32)),
		"username":        uid,
	}
	nonceJSON, _ := json.Marshal(nonceData)
	nonceB64 := base64.StdEncoding.EncodeToString(nonceJSON)
	hni := randomChoice([]string{"45201", "45204", "45202"})

	return &api{
		uid:       uid,
		password:  password,
		twoFACode: twoFACode,
		deviceID:  deviceID,
		machineID: machineID,
		nonceB64:  nonceB64,
		hni:       hni,
		client:    &http.Client{Jar: jar},
	}
}

func (a *api) applyHeaders(req *http.Request) {
	usdid := fmt.Sprintf("%s.%d.%s", newUUID(), time.Now().Unix(), randomAlphaNum(100))
	req.Header.Set("Host", "b-graph.facebook.com")
	req.Header.Set("X-Fb-Request-Analytics-Tags", `{"network_tags":{"product":"350685531728","request_category":"graphql","purpose":"fetch","retry_attempt":"0"},"application_tags":"graphservice"}`)
	req.Header.Set("X-Fb-Rmd", "state=URL_ELIGIBLE")
	req.Header.Set("Priority", "u=0")
	req.Header.Set("X-Zero-Eh", randomHex(16))
	req.Header.Set("User-Agent", "[FBAN/FB4A;FBAV/549.0.0.61.62;FBBV/891620555;FBDM/{density=3.0,width=1080,height=1920};FBLC/vi_VN;FBRV/0;FBCR/MobiFone;FBMF/Samsung;FBBD/Samsung;FBPN/com.facebook.katana;FBDV/SM-N980F;FBSV/9;FBOP/1;FBCA/x86_64:arm64-v8a;]")
	req.Header.Set("X-Fb-Friendly-Name", "FbBloksActionRootQuery-"+loginAppID)
	req.Header.Set("X-Zero-F-Device-Id", a.deviceID)
	req.Header.Set("X-Fb-Integrity-Machine-Id", a.machineID)
	req.Header.Set("X-Graphql-Request-Purpose", "fetch")
	req.Header.Set("X-Tigon-Is-Retry", "False")
	req.Header.Set("X-Graphql-Client-Library", "graphservice")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Fb-Net-Hni", a.hni)
	req.Header.Set("X-Fb-Sim-Hni", a.hni)
	req.Header.Set("Authorization", "OAuth 350685531728|62f8ce9f74b12f84c123cc23437a4a32")
	req.Header.Set("X-Zero-State", "unknown")
	req.Header.Set("X-Meta-Zca", "empty_token")
	req.Header.Set("App-Scope-Id-Header", a.deviceID)
	req.Header.Set("X-Fb-Connection-Type", "WIFI")
	req.Header.Set("X-Meta-Usdid", usdid)
	req.Header.Set("X-Fb-Http-Engine", "Tigon/Liger")
	req.Header.Set("X-Fb-Client-Ip", "True")
	req.Header.Set("X-Fb-Server-Cluster", "True")
	req.Header.Set("X-Fb-Conn-Uuid-Client", randomHex(16))
}

func (a *api) postForm(form url.Values) ([]byte, error) {
	req, err := http.NewRequest("POST", bgraphURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	a.applyHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	return body, nil
}

func ntContext() map[string]interface{} {
	return map[string]interface{}{
		"using_white_navbar":           true,
		"styles_id":                    stylesID,
		"pixel_ratio":                  3,
		"is_push_on":                   true,
		"debug_tooling_metadata_token": nil,
		"is_flipper_enabled":           false,
		"theme_params": []interface{}{
			map[string]interface{}{"value": []interface{}{}, "design_system_name": "FDS"},
		},
		"bloks_version": bloksVersioningID,
	}
}

func marshalVariables(innerObj map[string]interface{}, appID string) (string, error) {
	innerJSON, err := json.Marshal(innerObj)
	if err != nil {
		return "", err
	}
	level3 := map[string]string{"params": string(innerJSON)}
	level3JSON, err := json.Marshal(level3)
	if err != nil {
		return "", err
	}
	level2 := map[string]interface{}{
		"params":              string(level3JSON),
		"bloks_versioning_id": bloksVersioningID,
		"app_id":              appID,
	}
	variables := map[string]interface{}{
		"params":     level2,
		"scale":      "3",
		"nt_context": ntContext(),
	}
	varsJSON, err := json.Marshal(variables)
	if err != nil {
		return "", err
	}
	return string(varsJSON), nil
}

func (a *api) buildLoginVariables() (string, error) {
	now := time.Now().Unix()

	clientInputParams := map[string]interface{}{
		"aac": fmt.Sprintf(
			`{"aac_init_timestamp":%d,"aacjid":"%s","aaccs":"KOM4e6kWGcnzkrB5_kx906-l8IAy5zva03VpX__gVn4"}`,
			now, newUUID()),
		"sim_phones": []interface{}{},
		"aymh_accounts": []interface{}{
			map[string]interface{}{
				"profiles": map[string]interface{}{
					"id": map[string]interface{}{
						"is_derived": 0, "credentials": []interface{}{},
						"account_center_id": "", "profile_picture_url": "",
						"small_profile_picture_url": nil, "notification_count": 0,
						"token": "", "last_access_time": 0, "has_smartlock": 0,
						"credential_type": "none", "password": "",
						"from_accurate_privacy_result": 0, "dbln_validated": 0,
						"user_id": "", "name": "", "nta_eligibility_reason": nil,
						"username": "", "account_source": "",
					},
				},
				"id": "",
			},
		},
		"network_bssid":            nil,
		"secure_family_device_id":  newUUID(),
		"attestation_result": map[string]interface{}{
			"keyHash":   randomHex(32),
			"data":      a.nonceB64,
			"signature": "MEQCIGSSRU91Ft/RctrQvwzH+d6hFAYnwd6pe1X8IT+s1UldAiBq29Ly4BAJjiYXyq+ruKJK0QPP+NBlhMexIj10Ybhvyg==",
		},
		"has_granted_read_contacts_permissions":   0,
		"auth_secure_device_id":                   "",
		"has_whatsapp_installed":                  0,
		"password":                                a.password,
		"sso_token_map_json_string":               "",
		"block_store_machine_id":                  nil,
		"cloud_trust_token":                       nil,
		"event_flow":                              "login_manual",
		"password_contains_non_ascii":             "false",
		"sim_serials":                             []interface{}{},
		"client_known_key_hash":                   "",
		"encrypted_msisdn":                        "",
		"has_granted_read_phone_permissions":       0,
		"app_manager_id":                          "null",
		"should_show_nested_nta_from_aymh":        0,
		"device_id":                               a.deviceID,
		"zero_balance_state":                      "init",
		"login_attempt_count":                     1,
		"machine_id":                              a.machineID,
		"flash_call_permission_status": map[string]interface{}{
			"READ_PHONE_STATE":   "DENIED",
			"READ_CALL_LOG":      "DENIED",
			"ANSWER_PHONE_CALLS": "DENIED",
		},
		"accounts_list":                           []interface{}{},
		"gms_incoming_call_retriever_eligibility": "not_eligible",
		"family_device_id":                        a.deviceID,
		"fb_ig_device_id":                         []interface{}{},
		"device_emails":                           []interface{}{},
		"try_num":                                 2,
		"lois_settings":                           map[string]interface{}{"lois_token": ""},
		"event_step":                              "home_page",
		"headers_infra_flow_id":                   newUUID(),
		"openid_tokens":                           map[string]interface{}{},
		"contact_point":                           a.uid,
	}

	serverParams := map[string]interface{}{
		"should_trigger_override_login_2fa_action":    0,
		"is_vanilla_password_page_empty_password":     0,
		"is_from_logged_out":                           0,
		"should_trigger_override_login_success_action": 0,
		"login_credential_type":                        "none",
		"server_login_source":                          "login",
		"waterfall_id":                                 newUUID(),
		"two_step_login_type":                          "one_step_login",
		"login_source":                                 "Login",
		"is_platform_login":                            0,
		"pw_encryption_try_count":                      1,
		"INTERNAL__latency_qpl_marker_id":              36707139,
		"is_from_aymh":                                 0,
		"offline_experiment_group":                     "caa_iteration_v6_perf_fb_2",
		"is_from_landing_page":                         0,
		"left_nav_button_action":                       "BACK",
		"password_text_input_id":                       "ngxumu:105",
		"is_from_empty_password":                       0,
		"is_from_msplit_fallback":                      0,
		"ar_event_source":                              "login_home_page",
		"username_text_input_id":                       "ngxumu:104",
		"layered_homepage_experiment_group":             nil,
		"device_id":                                    a.deviceID,
		"login_surface":                                "login_home",
		"INTERNAL__latency_qpl_instance_id":            141917525400342,
		"reg_flow_source":                              "lid_landing_screen",
		"is_caa_perf_enabled":                          1,
		"credential_type":                              "password",
		"is_from_password_entry_page":                  0,
		"caller":                                       "gslr",
		"family_device_id":                             a.deviceID,
		"is_from_assistive_id":                         0,
		"access_flow_version":                          "pre_mt_behavior",
		"is_from_logged_in_switcher":                   0,
	}

	inner := map[string]interface{}{
		"client_input_params": clientInputParams,
		"server_params":       serverParams,
	}
	return marshalVariables(inner, loginAppID)
}

func (a *api) buildTwoFAVariables(code, twoStepCtx string) (string, error) {
	clientInputParams := map[string]interface{}{
		"auth_secure_device_id":  "",
		"block_store_machine_id": nil,
		"code":                   code,
		"should_trust_device":    1,
		"family_device_id":       a.deviceID,
		"device_id":              a.deviceID,
		"cloud_trust_token":      nil,
		"network_bssid":          nil,
		"machine_id":             a.machineID,
	}
	serverParams := map[string]interface{}{
		"INTERNAL__latency_qpl_marker_id":   36707139,
		"device_id":                          a.deviceID,
		"spectra_reg_login_data":             nil,
		"challenge":                          "totp",
		"machine_id":                         a.machineID,
		"INTERNAL__latency_qpl_instance_id": 144217313000275,
		"two_step_verification_context":      twoStepCtx,
		"flow_source":                        "two_factor_login",
	}
	inner := map[string]interface{}{
		"client_input_params": clientInputParams,
		"server_params":       serverParams,
	}
	return marshalVariables(inner, twoFAAppID)
}

var reToken = regexp.MustCompile(`EAAAAU[a-zA-Z0-9_\-]{100,}`)
var re2FACtx = regexp.MustCompile(`AW[A-Za-z0-9_\-+/=]{200,}`)

func (a *api) login() (string, error) {
	variables, err := a.buildLoginVariables()
	if err != nil {
		return "", err
	}

	form := url.Values{}
	form.Set("method", "post")
	form.Set("pretty", "false")
	form.Set("format", "json")
	form.Set("server_timestamps", "true")
	form.Set("locale", "vi_VN")
	form.Set("purpose", "fetch")
	form.Set("fb_api_req_friendly_name", "FbBloksActionRootQuery-"+loginAppID)
	form.Set("fb_api_caller_class", "graphservice")
	form.Set("client_doc_id", loginDocID)
	form.Set("fb_api_client_context", `{"is_background":false}`)
	form.Set("variables", variables)
	form.Set("fb_api_analytics_tags", `["GraphServices"]`)
	form.Set("client_trace_id", newUUID())

	body, err := a.postForm(form)
	if err != nil {
		return "", err
	}
	text := string(body)

	if strings.Contains(text, "generic_error_dialog") ||
		strings.Contains(text, "login_other_error_dialog") {
		return "", fmt.Errorf("sai thong tin dang nhap (uid/password khong hop le)")
	}

	if m := reToken.FindString(text); m != "" {
		return m, nil
	}

	textLower := strings.ToLower(text)
	if strings.Contains(textLower, "two_step_verification") ||
		strings.Contains(text, "two_fac") ||
		strings.Contains(text, "redirect_two_fac") {

		m := re2FACtx.FindString(text)
		if m == "" {
			return "", fmt.Errorf("2FA context not found in response")
		}

		totpCode, err := totp.GenerateCode(a.twoFACode, time.Now())
		if err != nil {
			return "", fmt.Errorf("TOTP generate failed: %w", err)
		}

		return a.twoFA(totpCode, m)
	}

	return "", fmt.Errorf("no EAAAAU token and no 2FA prompt detected")
}

func (a *api) twoFA(code, twoStepCtx string) (string, error) {
	variables, err := a.buildTwoFAVariables(code, twoStepCtx)
	if err != nil {
		return "", err
	}

	form := url.Values{}
	form.Set("method", "post")
	form.Set("pretty", "false")
	form.Set("format", "json")
	form.Set("server_timestamps", "true")
	form.Set("locale", "vi_VN")
	form.Set("purpose", "fetch")
	form.Set("fb_api_req_friendly_name", "FbBloksActionRootQuery-"+twoFAAppID)
	form.Set("fb_api_caller_class", "graphservice")
	form.Set("client_doc_id", loginDocID)
	form.Set("fb_api_client_context", `{"is_background":false}`)
	form.Set("variables", variables)
	form.Set("fb_api_analytics_tags", `["GraphServices"]`)
	form.Set("client_trace_id", newUUID())

	body, err := a.postForm(form)
	if err != nil {
		return "", err
	}
	if m := reToken.FindString(string(body)); m != "" {
		return m, nil
	}
	return "", fmt.Errorf("2FA verification failed: no token in response")
}

// ── Session for app ───────────────────────────────────────────────────────────

type sessionCookieEntry struct {
	Name             string `json:"name"`
	Value            string `json:"value"`
	ExpiresTimestamp int64  `json:"expires_timestamp"`
}

type sessionForAppResp struct {
	AccessToken    string               `json:"access_token"`
	SessionCookies []sessionCookieEntry `json:"session_cookies"`
}

func getSessionForApp(accessToken string) (*sessionForAppResp, error) {
	params := url.Values{}
	params.Set("access_token", accessToken)
	params.Set("new_app_id", sessionAppID)
	params.Set("format", "json")
	params.Set("generate_session_cookies", "1")

	resp, err := http.Get(sessionForAppURL + "?" + params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read session response body: %w", err)
	}

	var result sessionForAppResp
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse session response failed: %w", err)
	}
	return &result, nil
}

// ── Public API ────────────────────────────────────────────────────────────────

// LoginResult contains the cookies and tokens obtained from a successful login.
type LoginResult struct {
	// Cookies is a map of cookie name → value (c_user, xs, fr, datr, etc.)
	Cookies map[string]string
	// CookieString is "c_user=...;xs=...;fr=...;datr=..."
	CookieString string
	// LoginToken is the EAAAAU... token from the login API (before session exchange)
	LoginToken string
	// AccessToken from getSessionForApp (after session exchange)
	AccessToken string
}

// Login performs a full Facebook login flow:
//  1. Encrypt password
//  2. Login via bloks API (with 2FA if needed)
//  3. Exchange token for session cookies via auth.getSessionforApp
//
// Returns a LoginResult with the session cookies ready for use by messagix.
func Login(uid, plainPassword, twoFASecret string) (*LoginResult, error) {
	encPwd, err := encryptPassword(plainPassword)
	if err != nil {
		return nil, fmt.Errorf("encrypt password: %w", err)
	}

	a := newAPI(uid, encPwd, twoFASecret)
	loginToken, err := a.login()
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}

	if !strings.HasPrefix(loginToken, "EAAAA") {
		return nil, fmt.Errorf("invalid login token received")
	}

	session, err := getSessionForApp(loginToken)
	if err != nil {
		return nil, fmt.Errorf("getSessionForApp: %w", err)
	}
	if session.AccessToken == "" {
		return nil, fmt.Errorf("empty session access token")
	}

	cookieMap := make(map[string]string)
	var parts []string
	for _, c := range session.SessionCookies {
		cookieMap[c.Name] = c.Value
		parts = append(parts, c.Name+"="+c.Value)
	}
	cookieMap["access_token"] = session.AccessToken

	return &LoginResult{
		Cookies:      cookieMap,
		CookieString: strings.Join(parts, ";"),
		LoginToken:   loginToken,
		AccessToken:  session.AccessToken,
	}, nil
}
