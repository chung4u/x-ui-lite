package service

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"x-ui/database/model"
	"x-ui/xray"
)

// The transport stub exercises the accounting and eviction path without opening a stats port.
type hyStatsStub struct {
	up, down int
	kicked   string
}

func (s *hyStatsStub) RoundTrip(r *http.Request) (*http.Response, error) {
	var body []byte
	switch r.URL.Path {
	case "/traffic":
		body, _ = json.Marshal(map[string]interface{}{"1": map[string]int{"tx": s.up, "rx": s.down}})
	case "/online":
		body = []byte(`{"1":1}`)
	case "/kick":
		b, _ := io.ReadAll(r.Body)
		s.kicked = string(b)
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: http.Header{}}, nil
}
func TestHyLifecycle(t *testing.T) {
	policyTestDB(t)
	in := policyTestInbound(t)
	const client = "11111111-1111-1111-1111-111111111111"
	t.Setenv("XUI_HY2_SECRET", "")
	if status, err := HyManage(in.Id, client, "status"); status != nil || err != nil {
		t.Fatalf("disabled integration status = %v, %v", status, err)
	}
	t.Setenv("XUI_HY2_SECRET", "test")
	t.Setenv("XUI_HY2_PUBLIC_ADDR", "hy2.example.test:5443")
	manage := func(action string) map[string]interface{} {
		t.Helper()
		result, e := HyManage(in.Id, client, action)
		if e != nil {
			t.Fatal(e)
		}
		return result
	}
	auth := func(key string) bool {
		r := httptest.NewRequest("POST", "/auth", strings.NewReader(`{"auth":"`+key+`"}`))
		w := httptest.NewRecorder()
		HyAuth(w, r)
		var a struct{ Ok bool }
		json.Unmarshal(w.Body.Bytes(), &a)
		return a.Ok
	}
	result := manage("enable")
	linkValue, _ := result["link"].(string)
	link, err := url.Parse(linkValue)
	if err != nil || link.Scheme != "hysteria2" || link.Host != "hy2.example.test:5443" {
		t.Fatal("incorrect public endpoint in HY2 URL")
	}
	proxy, err := hyProxy(in.Id, client)
	if err != nil || proxy == nil || proxy.Server != "hy2.example.test" || proxy.Port != 5443 || proxy.SNI != proxy.Server {
		t.Fatal("incorrect public endpoint in HY2 subscription")
	}
	k, e := hyCurrent(in.Id, client)
	if e != nil || !auth(k.Secret) {
		t.Fatal("credential rejected")
	}
	policy := hyDB().Model(&model.InboundUserPolicy{}).Where("inbound_id=? AND client_id=?", in.Id, client)
	check := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	check(policy.UpdateColumn("expiry_time", time.Now().UnixMilli()-100).Error)
	if auth(k.Secret) {
		t.Fatal("expired account accepted")
	}
	check(policy.Updates(map[string]interface{}{"expiry_time": 0, "total": 100}).Error)
	check((&InboundService{}).AddTraffic([]*xray.Traffic{{IsUser: true, UserTag: in.Tag, ClientID: client, Up: 20}}))
	stub := &hyStatsStub{up: 30, down: 60}
	old := hyHTTP
	hyHTTP = &http.Client{Transport: stub}
	defer func() { hyHTTP = old }()
	collect := func() { check(collectHyEpoch(func() ([]byte, error) { return []byte("epoch-1"), nil })) }
	collect()
	collect()
	if auth(k.Secret) || stub.kicked != `["1"]` {
		t.Fatal("quota enforcement failed")
	}
	var usage model.InboundUserTraffic
	check(hyDB().Where("inbound_id=? AND client_id=?", in.Id, client).First(&usage).Error)
	if usage.Up != 50 || usage.Down != 60 || usage.DailyUp != 50 || usage.DailyDown != 60 {
		t.Fatal("usage mismatch or double charge")
	}
	stub.up, stub.down = 70, 90
	check(collectHyEpoch(func() ([]byte, error) { return []byte("epoch-2"), nil }))
	check(hyDB().First(&usage, usage.Id).Error)
	if usage.Up != 120 || usage.Down != 150 {
		t.Fatal("process restart lost traffic")
	}
	check(policy.UpdateColumn("total", 0).Error)
	t.Setenv("XUI_HY2_PUBLIC_ADDR", "")
	if _, err := HyManage(in.Id, client, "rotate"); err == nil || !auth(k.Secret) {
		t.Fatal("missing public endpoint must not rotate working credentials")
	}
	t.Setenv("XUI_HY2_PUBLIC_ADDR", "hy2.example.test:5443")
	manage("rotate")
	next, e := hyCurrent(in.Id, client)
	check(e)
	if next.ID == k.ID || auth(k.Secret) || !auth(next.Secret) {
		t.Fatal("rotation failed")
	}
	manage("disable")
	if auth(next.Secret) {
		t.Fatal("disabled credential accepted")
	}
}
