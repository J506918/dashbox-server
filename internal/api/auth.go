package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"dashbox/internal/db"

	"github.com/gin-gonic/gin"
)

// ── QQ OAuth token store ──────────────────────────────────────────

type qqTokenEntry struct {
	UserID   int64  `json:"user_id"`
	Token    string `json:"token"`
	Nickname string `json:"nickname"`
	IsNew    bool   `json:"is_new"`
}

var (
	qqTokenStore = make(map[string]*qqTokenEntry)
	qqTokenMu    sync.Mutex
)

func init() {
	// Cleanup expired tokens every 2 minutes
	go func() {
		for {
			time.Sleep(2 * time.Minute)
			qqTokenMu.Lock()
			for k := range qqTokenStore {
				delete(qqTokenStore, k)
			}
			qqTokenMu.Unlock()
		}
	}()
}

// ── QQ OAuth callback (browser redirect target) ──────────────────

func (r *Router) qqCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	if code == "" || state == "" {
		c.String(http.StatusBadRequest, "missing code or state")
		return
	}

	appID := os.Getenv("QQ_APP_ID")
	appKey := os.Getenv("QQ_APP_KEY")
	redirectURI := fmt.Sprintf("https://8.136.28.140:8443/auth/qq/callback")

	// Step 1: exchange code for access_token
	tokenURL := fmt.Sprintf(
		"https://graph.qq.com/oauth2.0/token?grant_type=authorization_code"+
			"&client_id=%s&client_secret=%s&code=%s&redirect_uri=%s&fmt=json",
		url.QueryEscape(appID), url.QueryEscape(appKey),
		url.QueryEscape(code), url.QueryEscape(redirectURI),
	)
	tokenResp, err := http.Get(tokenURL)
	if err != nil {
		c.String(http.StatusInternalServerError, "token exchange failed")
		return
	}
	defer tokenResp.Body.Close()
	body, _ := io.ReadAll(tokenResp.Body)

	var tokenData struct {
		AccessToken  string `json:"access_token"`
		Error        int    `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if strings.Contains(string(body), "access_token=") {
		vals, _ := url.ParseQuery(string(body))
		tokenData.AccessToken = vals.Get("access_token")
	} else {
		json.Unmarshal(body, &tokenData)
	}
	if tokenData.AccessToken == "" {
		c.String(http.StatusUnauthorized, fmt.Sprintf("qq auth failed: %s", string(body)))
		return
	}

	// Step 2: get OpenID
	openIDURL := fmt.Sprintf(
		"https://graph.qq.com/oauth2.0/me?access_token=%s&fmt=json",
		url.QueryEscape(tokenData.AccessToken),
	)
	openIDResp, err := http.Get(openIDURL)
	if err != nil {
		c.String(http.StatusInternalServerError, "openid request failed")
		return
	}
	defer openIDResp.Body.Close()
	body, _ = io.ReadAll(openIDResp.Body)

	raw := string(body)
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end > start {
			raw = raw[start : end+1]
		}
	}
	var openIDData struct {
		OpenID    string `json:"openid"`
		ClientID  string `json:"client_id"`
		Error     int    `json:"error"`
		ErrorDesc string `json:"error_description"`
	}
	if err := json.Unmarshal([]byte(raw), &openIDData); err != nil || openIDData.OpenID == "" {
		c.String(http.StatusUnauthorized, "failed to parse openid")
		return
	}

	// Step 3: get nickname
	nickname := ""
	userInfoURL := fmt.Sprintf(
		"https://graph.qq.com/user/get_user_info?access_token=%s&oauth_consumer_key=%s&openid=%s",
		url.QueryEscape(tokenData.AccessToken),
		url.QueryEscape(appID),
		url.QueryEscape(openIDData.OpenID),
	)
	userInfoResp, err := http.Get(userInfoURL)
	if err == nil {
		defer userInfoResp.Body.Close()
		body, _ := io.ReadAll(userInfoResp.Body)
		var userInfo struct {
			Nickname string `json:"nickname"`
		}
		if json.Unmarshal(body, &userInfo) == nil {
			nickname = userInfo.Nickname
		}
	}

	// Step 4: find or create user
	user, token, err := db.GetOrCreateUserByQQ(r.db, openIDData.OpenID, nickname)
	if err != nil {
		c.String(http.StatusInternalServerError, "login failed")
		return
	}

	isNew := user.CreatedAt.Equal(user.UpdatedAt)

	// Store token for app polling
	qqTokenMu.Lock()
	qqTokenStore[state] = &qqTokenEntry{
		UserID:   user.ID,
		Token:    token,
		Nickname: user.Nickname,
		IsNew:    isNew,
	}
	qqTokenMu.Unlock()

	// Redirect to app scheme so device switches back
	c.Redirect(http.StatusFound, fmt.Sprintf("dashbox://login?state=%s", state))
	// Fallback HTML if scheme not registered
	c.Writer.WriteString(fmt.Sprintf(`<html><body style="background:#0D1B2A;color:#fff;text-align:center;padding-top:40%%;font-family:sans-serif">
<h2>登录成功</h2><p>正在返回 DashBox...</p><p style="color:#888;font-size:13px">如未自动跳转，请手动返回App</p>
<script>setTimeout(function(){location.href='dashbox://login?state=%s';},500);</script>
</body></html>`, state))
}

// ── QQ token poll (App calls this after browser redirect) ─────────

func (r *Router) qqTokenPoll(c *gin.Context) {
	state := c.Query("state")
	if state == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing state"})
		return
	}

	qqTokenMu.Lock()
	entry, ok := qqTokenStore[state]
	if ok {
		delete(qqTokenStore, state) // one-time use
	}
	qqTokenMu.Unlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "token not ready"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user_id":  entry.UserID,
		"token":    entry.Token,
		"nickname": entry.Nickname,
		"is_new":   entry.IsNew,
	})
}

// ── QQ mobile SDK login (receives access_token + openid) ──────────

type MobileQQLoginRequest struct {
	AccessToken string `json:"access_token" binding:"required"`
	OpenID      string `json:"open_id" binding:"required"`
}

func (r *Router) mobileQQLogin(c *gin.Context) {
	var req MobileQQLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	appID := os.Getenv("QQ_APP_ID")
	if appID == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "qq login not configured"})
		return
	}

	// Verify token by fetching user info from QQ
	nickname := ""
	userInfoURL := fmt.Sprintf(
		"https://graph.qq.com/user/get_user_info?access_token=%s&oauth_consumer_key=%s&openid=%s",
		url.QueryEscape(req.AccessToken),
		url.QueryEscape(appID),
		url.QueryEscape(req.OpenID),
	)
	userInfoResp, err := http.Get(userInfoURL)
	if err == nil {
		defer userInfoResp.Body.Close()
		body, _ := io.ReadAll(userInfoResp.Body)
		var userInfo struct {
			Nickname string `json:"nickname"`
			Ret      int    `json:"ret"`
		}
		if json.Unmarshal(body, &userInfo) == nil && userInfo.Ret == 0 {
			nickname = userInfo.Nickname
		}
	}

	// Find or create user
	user, token, err := db.GetOrCreateUserByQQ(r.db, req.OpenID, nickname)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "login failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user_id":  user.ID,
		"token":    token,
		"nickname": user.Nickname,
		"is_new":   user.CreatedAt.Equal(user.UpdatedAt),
	})
}
