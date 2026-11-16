package session

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type Options struct {
	PublicOrigin   string
	TrustedProxies []*net.IPNet
}

func OptionsFromEnv() (Options, error) {
	o := Options{PublicOrigin: strings.TrimRight(strings.TrimSpace(os.Getenv("XUI_PUBLIC_ORIGIN")), "/")}
	if o.PublicOrigin != "" {
		u, err := url.Parse(o.PublicOrigin)
		if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
			return o, fmt.Errorf("XUI_PUBLIC_ORIGIN 必须是完整的 http(s) 公网来源地址，不含路径")
		}
		if u.Scheme == "http" && !net.ParseIP(u.Hostname()).IsLoopback() {
			return o, fmt.Errorf("非本机管理入口必须使用 HTTPS")
		}
	}
	for _, value := range strings.Split(os.Getenv("XUI_TRUSTED_PROXIES"), ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if ip := net.ParseIP(value); ip != nil {
			bits := 128
			if ip.To4() != nil {
				bits = 32
			}
			o.TrustedProxies = append(o.TrustedProxies, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		} else {
			_, subnet, err := net.ParseCIDR(value)
			if err != nil {
				return o, fmt.Errorf("XUI_TRUSTED_PROXIES 含无效地址")
			}
			ones, _ := subnet.Mask.Size()
			if ones == 0 {
				return o, fmt.Errorf("不能信任所有代理地址")
			}
			o.TrustedProxies = append(o.TrustedProxies, subnet)
		}
	}
	return o, nil
}
func options(c *gin.Context) Options {
	v, _ := c.Get("security_options")
	o, _ := v.(Options)
	return o
}
func trusted(o Options, ip net.IP) bool {
	for _, subnet := range o.TrustedProxies {
		if subnet.Contains(ip) {
			return true
		}
	}
	return false
}
func peerIP(c *gin.Context) net.IP {
	host, _, _ := net.SplitHostPort(c.Request.RemoteAddr)
	return net.ParseIP(host)
}
func ClientIP(c *gin.Context) string {
	ip := peerIP(c)
	o := options(c)
	if trusted(o, ip) {
		chain := strings.Split(c.GetHeader("X-Forwarded-For"), ",")
		for i := len(chain) - 1; i >= 0; i-- {
			next := net.ParseIP(strings.TrimSpace(chain[i]))
			if next == nil {
				break
			}
			ip = next
			if !trusted(o, ip) {
				break
			}
		}
	}
	return ip.String()
}
func SecureRequest(c *gin.Context) bool {
	return c.Request.TLS != nil || (trusted(options(c), peerIP(c)) && c.GetHeader("X-Forwarded-Proto") == "https")
}
func publicHTTPS(c *gin.Context) bool { return strings.HasPrefix(options(c).PublicOrigin, "https://") }
func Origin(c *gin.Context) string {
	if origin := options(c).PublicOrigin; origin != "" {
		return origin
	}
	scheme := "http"
	if SecureRequest(c) {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}
func sameOrigin(c *gin.Context) bool {
	if c.GetHeader("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	source := c.GetHeader("Origin")
	if source == "" {
		source = c.GetHeader("Referer")
	}
	if source == "" {
		return true
	} // Non-browser clients still require the CSRF token.
	u, err := url.Parse(source)
	if err != nil || u.User != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Scheme+"://"+u.Host, Origin(c))
}
func reject(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"success": false, "msg": message})
}
func Middleware(o Options, basePath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("security_options", o)
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Content-Security-Policy", "frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if o.PublicOrigin != "" {
			u, _ := url.Parse(o.PublicOrigin)
			if !strings.EqualFold(c.Request.Host, u.Host) {
				reject(c, http.StatusMisdirectedRequest, "入口地址不匹配")
				return
			}
		}
		if strings.HasPrefix(c.Request.URL.Path, basePath+"assets/") {
			c.Next()
			return
		}
		c.Header("Cache-Control", "no-store")
		if strings.HasPrefix(c.Request.URL.Path, basePath+"sub/") {
			c.Next()
			return
		}
		local := peerIP(c).IsLoopback() && c.GetHeader("X-Forwarded-For") == "" && c.GetHeader("X-Forwarded-Proto") == ""
		if !SecureRequest(c) && !local {
			reject(c, http.StatusForbidden, "管理入口需要 HTTPS，请配置证书或可信反向代理")
			return
		}
		readSession(c)
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && c.Request.Method != http.MethodOptions {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2*1024*1024)
			if !sameOrigin(c) || !validCSRF(c) {
				reject(c, http.StatusForbidden, "安全验证失败，请刷新页面后重试")
				return
			}
		}
		c.Next()
	}
}

type attempt struct {
	count int
	until time.Time
}
type limiter struct {
	mu      sync.Mutex
	entries map[string]attempt
}

var requestLimits = limiter{entries: make(map[string]attempt)}
var passwordSlots = make(chan struct{}, 4)

func (l *limiter) allow(key string, max int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	entry := l.entries[key]
	if !entry.until.After(now) {
		for k, v := range l.entries {
			if !v.until.After(now) {
				delete(l.entries, k)
			}
		}
		if len(l.entries) >= 4096 {
			return false
		}
		entry = attempt{until: now.Add(window)}
	}
	if entry.count >= max {
		return false
	}
	entry.count++
	l.entries[key] = entry
	return true
}

// Login and password reauthentication share both rate and Argon2 concurrency limits.
func BeginAuthentication(c *gin.Context, username string) (func(), bool) {
	if !requestLimits.allow("ip:"+ClientIP(c), 20, 5*time.Minute) || !requestLimits.allow("account:"+digest(username), 10, 5*time.Minute) || !requestLimits.allow("auth-global", 100, time.Minute) {
		c.Header("Retry-After", "300")
		reject(c, http.StatusTooManyRequests, "尝试过于频繁，请稍后重试")
		return nil, false
	}
	select {
	case passwordSlots <- struct{}{}:
		return func() { <-passwordSlots }, true
	default:
		c.Header("Retry-After", "5")
		reject(c, http.StatusTooManyRequests, "登录服务繁忙，请稍后重试")
		return nil, false
	}
}
