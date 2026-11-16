package web

import (
	"encoding/json"
	"github.com/robfig/cron/v3"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"x-ui/database"
	"x-ui/database/model"
	"x-ui/util/credential"
	"x-ui/web/global"
	"x-ui/web/service"
)

const testPassword = "only-a-local-test-password"

var csrfPattern = regexp.MustCompile(`X-CSRF-Token'\] = '([^']+)'`)

type securityBrowser struct {
	handler http.Handler
	cookies map[string]*http.Cookie
	csrf    string
}

func securityFixture(t *testing.T) (*securityBrowser, string) {
	t.Helper()
	t.Setenv("XUI_DEBUG", "")
	t.Setenv("XUI_PUBLIC_ORIGIN", "https://panel.test")
	t.Setenv("XUI_TRUSTED_PROXIES", "")
	if err := database.InitDB(filepath.Join(t.TempDir(), "panel.db")); err != nil {
		t.Fatal(err)
	}
	username := t.Name()
	if err := (&service.UserService{}).UpdateFirstUser(username, testPassword); err != nil {
		t.Fatal(err)
	}
	s := NewServer()
	s.cron = cron.New()
	global.SetWebServer(s)
	handler, err := s.initRouter()
	if err != nil {
		t.Fatal(err)
	}
	return &securityBrowser{handler: handler, cookies: map[string]*http.Cookie{}}, username
}
func (b *securityBrowser) request(method, path string, data url.Values, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://panel.test"+path, strings.NewReader(data.Encode()))
	r.RemoteAddr = "192.0.2.10:23456"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Requested-With", "XMLHttpRequest")
	if method != "GET" {
		r.Header.Set("Origin", "https://panel.test")
	}
	if b.csrf != "" {
		r.Header.Set("X-CSRF-Token", b.csrf)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	for _, cookie := range b.cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	b.handler.ServeHTTP(w, r)
	for _, cookie := range w.Result().Cookies() {
		if cookie.MaxAge < 0 {
			delete(b.cookies, cookie.Name)
		} else {
			b.cookies[cookie.Name] = cookie
		}
	}
	if token := w.Header().Get("X-CSRF-Token"); token != "" {
		b.csrf = token
	}
	if match := csrfPattern.FindStringSubmatch(w.Body.String()); len(match) == 2 {
		b.csrf = match[1]
	}
	return w
}
func (b *securityBrowser) login(t *testing.T, username string) {
	t.Helper()
	if w := b.request("GET", "/", nil, nil); w.Code != 200 || b.csrf == "" {
		t.Fatalf("login page: %d, csrf=%t", w.Code, b.csrf != "")
	}
	w := b.request("POST", "/login", url.Values{"username": {username}, "password": {testPassword}}, nil)
	assertSuccess(t, w)
}
func assertSuccess(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	var result struct{ Success bool }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || !result.Success {
		t.Fatalf("request failed: HTTP %d", w.Code)
	}
}
func cloneBrowser(b *securityBrowser) *securityBrowser {
	copy := &securityBrowser{handler: b.handler, cookies: map[string]*http.Cookie{}, csrf: b.csrf}
	for k, v := range b.cookies {
		c := *v
		copy.cookies[k] = &c
	}
	return copy
}

func TestAdminLoginCSRFAndRevocableLogout(t *testing.T) {
	b, username := securityFixture(t)
	w := b.request("POST", "/login", url.Values{"username": {username}, "password": {testPassword}}, nil)
	if w.Code != 403 {
		t.Fatal("login accepted without pre-login CSRF")
	}
	b.request("GET", "/", nil, nil)
	before := b.cookies["roylive_session"].Value
	b.login(t, username)
	cookie := b.cookies["roylive_session"]
	if cookie.Value == before || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || len(cookie.Value) != 43 || strings.Contains(cookie.Value, username) {
		t.Fatal("unsafe or unrotated cookie")
	}
	var rows []model.AdminSession
	database.GetDB().Find(&rows)
	for _, row := range rows {
		if row.TokenHash == cookie.Value {
			t.Fatal("raw session persisted")
		}
	}
	for _, headers := range []map[string]string{{"X-CSRF-Token": ""}, {"X-CSRF-Token": "wrong"}, {"Origin": "https://evil.test"}, {"Origin": "null"}, {"Sec-Fetch-Site": "cross-site"}} {
		if w := b.request("POST", "/xui/setting/all", nil, headers); w.Code != 403 {
			t.Fatal("forged request accepted")
		}
	}
	assertSuccess(t, b.request("POST", "/xui/setting/all", nil, nil))
	if w := b.request("GET", "/logout", nil, nil); w.Code != 404 {
		t.Fatal("GET logout still exists")
	}
	assertSuccess(t, b.request("POST", "/xui/setting/all", nil, nil))
	stolen := cloneBrowser(b)
	assertSuccess(t, b.request("POST", "/logout", nil, nil))
	if w := stolen.request("POST", "/xui/setting/all", nil, nil); w.Code == 200 {
		t.Fatal("revoked session accepted")
	}
	legacy := &securityBrowser{handler: b.handler, cookies: map[string]*http.Cookie{"session": {Name: "session", Value: "old-signed-session"}}}
	if w := legacy.request("GET", "/xui/", nil, nil); w.Code == 200 {
		t.Fatal("legacy cookie accepted")
	}
}

func TestAdminPasswordChangeRevokesAllSessionsAndPreservesProxyData(t *testing.T) {
	b, username := securityFixture(t)
	b.login(t, username)
	other := &securityBrowser{handler: b.handler, cookies: map[string]*http.Cookie{}}
	other.login(t, username)
	stale := cloneBrowser(b)
	inbound := model.Inbound{UserId: 1, Port: 12345, Protocol: model.VLESS, Tag: "test-inbound", Settings: `{"clients":[]}`}
	database.GetDB().Create(&inbound)
	sub := model.InboundSubscription{InboundId: inbound.Id, ClientId: "synthetic-client", Token: "synthetic-subscription"}
	database.GetDB().Create(&sub)
	traffic := model.InboundUserTraffic{InboundId: inbound.Id, ClientId: "synthetic-client", Up: 123, Down: 456}
	database.GetDB().Create(&traffic)
	form := url.Values{"oldUsername": {username}, "oldPassword": {testPassword}, "newUsername": {username}, "newPassword": {"changed-local-test-password"}}
	assertSuccess(t, b.request("POST", "/xui/setting/updateUser", form, nil))
	for _, browser := range []*securityBrowser{other, stale} {
		if w := browser.request("POST", "/xui/setting/all", nil, nil); w.Code == 200 {
			t.Fatal("old session survived password change")
		}
	}
	var user model.User
	database.GetDB().First(&user, 1)
	if user.Password != "" || !credential.Verify(user.PasswordHash, "changed-local-test-password") || credential.Verify(user.PasswordHash, testPassword) {
		t.Fatal("password change failed")
	}
	var saved model.InboundSubscription
	database.GetDB().First(&saved, sub.Id)
	var measured model.InboundUserTraffic
	database.GetDB().First(&measured, traffic.Id)
	if saved.Token != sub.Token || measured.Up != 123 || measured.Down != 456 {
		t.Fatal("proxy data altered")
	}
}

func TestAdminSessionExpiresAndCLIResetRevokes(t *testing.T) {
	b, username := securityFixture(t)
	b.login(t, username)
	if err := (&service.UserService{}).UpdateFirstUser(username, "another-local-test-password"); err != nil {
		t.Fatal(err)
	}
	if w := b.request("POST", "/xui/setting/all", nil, nil); w.Code == 200 {
		t.Fatal("CLI reset did not revoke")
	}
	if err := (&service.UserService{}).UpdateFirstUser(username, testPassword); err != nil {
		t.Fatal(err)
	}
	b.login(t, username)
	database.GetDB().Model(&model.AdminSession{}).Where("1 = 1").Update("expires_at", time.Now().Add(-time.Minute).Unix())
	if w := b.request("POST", "/xui/setting/all", nil, nil); w.Code == 200 {
		t.Fatal("expired session accepted")
	}
}
