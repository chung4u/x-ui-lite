package session

import (
	"github.com/gin-gonic/gin"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
	"x-ui/database"
	"x-ui/database/model"
)

func TestTrustedProxyBoundary(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "panel.db")); err != nil {
		t.Fatal(err)
	}
	_, network, _ := net.ParseCIDR("172.18.0.2/32")
	o := Options{PublicOrigin: "https://panel.test", TrustedProxies: []*net.IPNet{network}}
	r := gin.New()
	r.Use(Middleware(o, "/"))
	r.GET("/", func(c *gin.Context) { c.String(200, ClientIP(c)) })
	for _, tc := range []struct {
		peer, host, proto, chain, expected string
		status                             int
	}{
		{"198.51.100.2:5555", "panel.test", "https", "1.1.1.1", "", 403},
		{"172.18.0.2:5555", "panel.test", "http", "198.51.100.2", "", 403},
		{"172.18.0.2:5555", "panel.test", "https", "1.1.1.1, 198.51.100.2", "198.51.100.2", 200},
		{"172.18.0.2:5555", "evil.test", "https", "198.51.100.2", "", 421},
	} {
		req := httptest.NewRequest("GET", "http://"+tc.host+"/", nil)
		req.RemoteAddr = tc.peer
		req.Header.Set("X-Forwarded-Proto", tc.proto)
		req.Header.Set("X-Forwarded-For", tc.chain)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.status || (tc.status == 200 && w.Body.String() != tc.expected) {
			t.Fatalf("proxy validation got status=%d ip=%q", w.Code, w.Body.String())
		}
	}
}

func TestOptionsRejectUnsafeOriginsAndUniversalProxyTrust(t *testing.T) {
	for _, raw := range []string{"https://panel.test/path", "https://user:pass@panel.test", "http://public.test", "https://panel.test?key=value"} {
		t.Setenv("XUI_PUBLIC_ORIGIN", raw)
		if _, err := OptionsFromEnv(); err == nil {
			t.Fatal("invalid origin accepted")
		}
	}
	t.Setenv("XUI_PUBLIC_ORIGIN", "https://panel.test")
	for _, raw := range []string{"0.0.0.0/0", "::/0", "invalid"} {
		t.Setenv("XUI_TRUSTED_PROXIES", raw)
		if _, err := OptionsFromEnv(); err == nil {
			t.Fatal("invalid proxy trust accepted")
		}
	}
	t.Setenv("XUI_TRUSTED_PROXIES", "172.18.0.2,127.0.0.1/32")
	if _, err := OptionsFromEnv(); err != nil {
		t.Fatal(err)
	}
}

func TestLoginLimitsAcrossAddressesAndBoundedState(t *testing.T) {
	requestLimits = limiter{entries: map[string]attempt{}}
	for i := 0; i < 11; i++ {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "https://panel.test/login", nil)
		c.Request.RemoteAddr = net.JoinHostPort(net.IPv4(192, 0, 2, byte(i+1)).String(), "1000")
		finish, ok := BeginAuthentication(c, "one-account")
		if i < 10 {
			if !ok {
				t.Fatal("limited too early")
			}
			finish()
		} else if ok || w.Code != http.StatusTooManyRequests {
			t.Fatal("account limit bypassed with different IP")
		}
	}
	l := limiter{entries: map[string]attempt{}}
	l.entries["expired"] = attempt{count: 5, until: time.Now().Add(-time.Second)}
	if !l.allow("expired", 1, time.Minute) || l.allow("expired", 1, time.Minute) {
		t.Fatal("window rollover failed")
	}
}

func TestStaleAuthenticationCannotMintNewSession(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "session.db")); err != nil {
		t.Fatal(err)
	}
	var stale model.User
	database.GetDB().First(&stale)
	database.GetDB().Model(&model.User{}).Where("id = ?", stale.Id).Update("auth_version", stale.AuthVersion+1)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "https://panel.test/login", nil)
	if err := SetLoginUser(c, &stale); err == nil {
		t.Fatal("stale credentials created session after password reset")
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("failure issued cookie")
	}
}
