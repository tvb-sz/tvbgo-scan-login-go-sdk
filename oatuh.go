package tvbgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// HostProd 生產環境
	HostProd = "https://api.tvbgo.tvb.com"
	// HostQA QA 環境
	HostQA = "https://qa-api.tvbgo.tvb.com"
	// HostDev 開發環境
	HostDev = "https://mytvb.tvb-sz.com"
)

func resolveHost(host string) string {
	normalized := strings.TrimRight(strings.TrimSpace(host), "/")
	switch strings.ToLower(normalized) {
	case "", "prod", "production":
		return HostProd
	case "qa":
		return HostQA
	case "dev", "develop", "development":
		return HostDev
	}

	switch strings.ToLower(normalized) {
	case HostProd:
		return HostProd
	case HostQA:
		return HostQA
	case HostDev:
		return HostDev
	default:
		fmt.Printf("Warning: unsupported host %q, want string prod/qa/dev or constant HostProd/HostQA/HostDev, fallback to HostProd", host)
		return HostProd
	}
}

var (
	AuthorizationHasExpiredOrInvalid = errors.New("authorization has expired or invalid")
	AuthorizationParamInvalid        = errors.New("callback URL needed params is missing")
)

// OauthError RFC 6749 §5.2 錯誤響應，同時作爲本 SDK 接口方法的統一錯誤返回值。
// 成功時返回 nil；失敗時 Code 爲 RFC 錯誤碼，ErrorDescription 爲具體描述。
// 字段不能命名爲 Error，否則會與 error 接口方法 Error() 沖突。
type OauthError struct {
	Code             string `json:"error" dc:"RFC6749錯誤碼"`
	ErrorDescription string `json:"error_description" dc:"具體錯誤描述"`
	ErrorURI         string `json:"error_uri,omitempty" dc:"可選的錯誤說明URI"`
	StatusCode       int    `json:"-"` // HTTP 狀態碼，網絡錯誤時爲 0
	cause            error
}

func (e *OauthError) Error() string {
	if e == nil {
		return ""
	}
	switch {
	case e.Code != "" && e.ErrorDescription != "":
		return e.Code + ": " + e.ErrorDescription
	case e.Code != "":
		return e.Code
	case e.ErrorDescription != "":
		return e.ErrorDescription
	case e.cause != nil:
		return e.cause.Error()
	default:
		return "oauth error"
	}
}

func (e *OauthError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// oauthErrorFromJSON 參數缺失
func oauthErrorInvalidParam() *OauthError {
	return &OauthError{
		Code:             "invalid_request",
		ErrorDescription: AuthorizationParamInvalid.Error(),
		cause:            AuthorizationParamInvalid,
	}
}

// oauthErrorFromJSON json轉換異常
func oauthErrorFromJSON(err error) *OauthError {
	return &OauthError{
		Code:             "server_error",
		ErrorDescription: err.Error(),
		cause:            err,
	}
}

// oauthErrorFromResult 將 httpClient 的結果轉爲 OauthError。
// 非 200 時按 RFC 6749 解析 body 中的 error / error_description。
func oauthErrorFromResult(result Result, err error) *OauthError {
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrResponseNotOK) {
		return &OauthError{
			Code:             "server_error",
			ErrorDescription: err.Error(),
			cause:            err,
		}
	}

	oe := &OauthError{
		StatusCode: result.StatusCode,
		cause:      err,
	}
	if len(result.Body) > 0 {
		_ = json.Unmarshal(result.Body, oe)
	}
	if oe.Code == "" {
		oe.Code = "server_error"
		if oe.ErrorDescription == "" {
			oe.ErrorDescription = fmt.Sprintf("unexpected http status %d", result.StatusCode)
		}
	}
	return oe
}

// TvbGoAccessToken TvbGo oauth accessToken structure
type TvbGoAccessToken struct {
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Openid       string `json:"openid"` // 在 client_id 維度全局唯一
}

// TvbGoUserInfo TvbGo oauth user info
type TvbGoUserInfo struct {
	Openid     string `json:"openid"`
	Email      string `json:"email"`
	EmployeeID string `json:"employee_id"`
	ChiName    string `json:"chi_name"`
	EngName    string `json:"eng_name"`
	Department string `json:"department"`
}

type OauthService interface {
	// GenerateRedirectURL 生成301跳轉到TvbGo的URL
	//  - state 跳轉去oauth授權後原樣帶回的任意字符串（128字符以內）
	GenerateRedirectURL(ctx context.Context, state string) string
	// Code2accessToken TvbGo授權後回調callback回後使用code去換令牌，請務必同時取出state進行比對後再調用本方法
	//  - code  回到callback URL後從query-string裏取出的code值
	Code2accessToken(ctx context.Context, code string) (TvbGoAccessToken, *OauthError)
	// RefreshAccessToken 使用refresh_token刷新access_token的有效期
	//  - refreshToken  Code2accessToken獲取到的refresh_token
	RefreshAccessToken(ctx context.Context, refreshToken string) (TvbGoAccessToken, *OauthError)
	// Token2userInfo 獲取到令牌值後獲取用戶信息（可以獲取到郵箱）
	//  - token  有效的令牌，Code2accessToken獲取到的
	Token2userInfo(ctx context.Context, token string) (TvbGoUserInfo, *OauthError)
}

// New 構造一個tvb go oauth授權管理器對象
//   - clientId     應用程序(客戶端) ID
//   - clientSecret 應用秘鑰，在具體應用的 `客戶端憑據` 裏創建客戶端密碼，注意有輪轉有效期
//   - redirectUri  在具體應用的 客戶端憑據 裏的 `重定向URI` 添加設置，支持多個
//   - host         環境切換：prod / qa / dev，或 HostProd / HostQA / HostDev；空值默認 prod。傳域名時僅允許這三個 Host
func New(clientId, clientSecret, redirectUri, host string) OauthService {
	return &oauthService{
		clientID:     clientId,
		clientSecret: clientSecret,
		redirectUri:  redirectUri,
		host:         resolveHost(host),
		httpClient: newClient(&http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSHandshakeTimeout: 10 * time.Second,
				DisableCompression:  true,
				MaxIdleConns:        400,
				MaxIdleConnsPerHost: 20,
				MaxConnsPerHost:     50,
				IdleConnTimeout:     120 * time.Second,
			},
		}),
	}
}

type oauthService struct {
	httpClient   *httpClient
	clientID     string // 應用編號
	clientSecret string // 應用秘鑰
	redirectUri  string // OAuth應用創建時填寫的回調callback URL，也是oauth授權後跳回我們系統接收code、state的URL
	host         string // API Host，如 https://api.tvbgo.tvb.com
}

func (o *oauthService) apiURL(path string) string {
	return o.host + path
}

// GenerateRedirectURL generate TvbGo oauth login redirect URL
//   - state 跳轉去oauth授權後原樣帶回的任意字符串（128字符以內），Code2accessToken之前需自主比對
func (o *oauthService) GenerateRedirectURL(ctx context.Context, state string) string {
	param := make(url.Values)
	param.Set("client_id", o.clientID)
	param.Set("response_type", "code")
	param.Set("redirect_uri", o.redirectUri)
	param.Set("scope", "scan_login")
	param.Set("state", state)

	return o.apiURL("/connect/qrconnect") + "?" + param.Encode()
}

// Code2accessToken TvbGo oauth login callback code to token
//   - code  回到callback URL後從query-string裏取出的code值，請務同時必取出state進行比對後再調用本方法
func (o *oauthService) Code2accessToken(ctx context.Context, code string) (TvbGoAccessToken, *OauthError) {
	// ① you must check state before call this method, for remission CSRF
	if code == "" {
		return TvbGoAccessToken{}, oauthErrorInvalidParam()
	}

	// ② code exchange accessToken
	return o.TvbGoCode2accessToken(ctx, code)
}

// RefreshAccessToken 使用refresh_token刷新access_token的有效期
//   - refreshToken  Code2accessToken獲取到的refresh_token
func (o *oauthService) RefreshAccessToken(ctx context.Context, refreshToken string) (TvbGoAccessToken, *OauthError) {
	// ① check param
	if refreshToken == "" {
		return TvbGoAccessToken{}, oauthErrorInvalidParam()
	}

	// ② refresh token exchange accessToken
	return o.TvbGoRefreshAccessToken(ctx, refreshToken)
}

// Token2userInfo 換取用戶信息的
//   - token  有效的令牌，Code2accessToken獲取到的
func (o *oauthService) Token2userInfo(ctx context.Context, token string) (TvbGoUserInfo, *OauthError) {
	// ① check param
	if token == "" {
		return TvbGoUserInfo{}, oauthErrorInvalidParam()
	}

	// ② accessToken exchange user info
	return o.TvbGoAccessToken2UserInfo(ctx, token)
}

// TvbGoCode2accessToken use oauth code exchange accessToken
func (o *oauthService) TvbGoCode2accessToken(ctx context.Context, code string) (TvbGoAccessToken, *OauthError) {
	param := make(url.Values)
	param.Set("client_id", o.clientID)
	param.Set("client_secret", o.clientSecret)
	param.Set("code", code)
	param.Set("grant_type", "authorization_code")
	param.Set("redirect_uri", o.redirectUri)

	return o.exchangeToken(ctx, o.apiURL("/connect/oauth/access_token"), param)
}

// TvbGoRefreshAccessToken use refresh token refresh accessToken
func (o *oauthService) TvbGoRefreshAccessToken(ctx context.Context, refreshToken string) (TvbGoAccessToken, *OauthError) {
	param := make(url.Values)
	param.Set("client_id", o.clientID)
	param.Set("client_secret", o.clientSecret)
	param.Set("refresh_token", refreshToken)
	param.Set("grant_type", "refresh_token")
	param.Set("redirect_uri", o.redirectUri)

	return o.exchangeToken(ctx, o.apiURL("/connect/oauth/refresh_token"), param)
}

func (o *oauthService) exchangeToken(ctx context.Context, endpoint string, param url.Values) (TvbGoAccessToken, *OauthError) {
	var accessToken TvbGoAccessToken

	result, err := o.httpClient.PostForm(ctx, endpoint, param, nil)
	if oauthErr := oauthErrorFromResult(result, err); oauthErr != nil {
		return accessToken, oauthErr
	}

	if err := json.Unmarshal(result.Body, &accessToken); err != nil {
		return accessToken, oauthErrorFromJSON(err)
	}
	if accessToken.AccessToken == "" {
		return accessToken, &OauthError{
			Code:             "server_error",
			ErrorDescription: "missing access_token in token response",
			StatusCode:       result.StatusCode,
		}
	}

	return accessToken, nil
}

// TvbGoAccessToken2UserInfo use TvbGo oauth accessToken get user info
func (o *oauthService) TvbGoAccessToken2UserInfo(ctx context.Context, accessToken string) (TvbGoUserInfo, *OauthError) {
	var user TvbGoUserInfo
	head := map[string]string{
		"Authorization": "Bearer " + accessToken,
	}

	result, err := o.httpClient.Get(ctx, o.apiURL("/connect/userinfo"), nil, head)
	if oauthErr := oauthErrorFromResult(result, err); oauthErr != nil {
		if oauthErr.StatusCode == http.StatusUnauthorized {
			oauthErr.cause = AuthorizationHasExpiredOrInvalid
		}
		return user, oauthErr
	}

	if err := json.Unmarshal(result.Body, &user); err != nil {
		return user, oauthErrorFromJSON(err)
	}

	return user, nil
}
