package service

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"x-ui/database"
	"x-ui/database/model"
	"x-ui/xray"
)

var hyLock sync.Mutex
var hyHTTP = &http.Client{Timeout: 3 * time.Second}

const hyProxyName = "Hysteria 2"

func HyAvailable() bool { return os.Getenv("XUI_HY2_SECRET") != "" }

func hyPublicEndpoint() (string, int) {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(os.Getenv("XUI_HY2_PUBLIC_ADDR")))
	port, portErr := strconv.Atoi(portText)
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if err != nil || portErr != nil || port < 1 || port > 65535 || (net.ParseIP(host) == nil && !validRealityHostname(host)) {
		return "", 0
	}
	return host, port
}

func hyDB() *gorm.DB { return database.GetDB() }
func hyCurrent(id int, client string) (model.HysteriaCredential, error) {
	var key model.HysteriaCredential
	err := hyDB().Where("inbound_id=? AND client_id=? AND enabled=?", id, client, true).Find(&key).Error
	return key, err
}
func HyManage(id int, client, action string) (map[string]interface{}, error) {
	if !HyAvailable() {
		if action == "status" {
			return nil, nil
		}
		return nil, errors.New("HY2 未配置")
	}
	host, port := hyPublicEndpoint()
	if host == "" && (action == "enable" || action == "rotate") {
		return nil, errors.New("请先设置有效的 XUI_HY2_PUBLIC_ADDR")
	}
	inbound, err := (&InboundService{}).GetInbound(id)
	if err != nil {
		return nil, err
	}
	if _, err = getVLESSInboundClient(inbound, client); err != nil {
		return nil, err
	}
	hyLock.Lock()
	defer hyLock.Unlock()
	key, err := hyCurrent(id, client)
	if err != nil {
		return nil, err
	}
	switch action {
	case "status":
	case "enable", "disable", "rotate":
		if action == "rotate" && key.ID == 0 {
			return nil, errors.New("HY2 未启用")
		}
		if action != "enable" || key.ID == 0 {
			err = hyDB().Transaction(func(tx *gorm.DB) error {
				if key.ID != 0 {
					if e := tx.Model(&key).Updates(map[string]interface{}{"enabled": false, "secret": ""}).Error; e != nil {
						return e
					}
				}
				key = model.HysteriaCredential{}
				if action == "disable" {
					return nil
				}
				secret, e := generateSubscriptionToken()
				if e != nil {
					return e
				}
				key = model.HysteriaCredential{InboundID: id, ClientID: client, Enabled: true, Secret: secret}
				return tx.Create(&key).Error
			})
			if err != nil {
				return nil, err
			}
		}
	default:
		return nil, errors.New("未知操作")
	}
	result := map[string]interface{}{"enabled": key.Enabled}
	if key.Enabled && host != "" && ValidateInboundUserAccess(inbound, client) == nil {
		result["link"] = (&url.URL{Scheme: "hysteria2", User: url.User(key.Secret), Host: net.JoinHostPort(host, strconv.Itoa(port)), Fragment: hyProxyName}).String()
	}
	return result, nil
}
func HyAuth(w http.ResponseWriter, r *http.Request) {
	var body struct{ Auth string }
	ok := false
	id := ""
	if r.Method == "POST" && r.URL.Path == "/auth" && json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) == nil && len(body.Auth) == 43 {
		var key model.HysteriaCredential
		if hyDB().Where("secret=? AND enabled=?", body.Auth, true).Find(&key).Error == nil && key.ID != 0 && subtle.ConstantTimeCompare([]byte(body.Auth), []byte(key.Secret)) == 1 {
			inbound, e := (&InboundService{}).GetInbound(key.InboundID)
			ok = e == nil && ValidateInboundUserAccess(inbound, key.ClientID) == nil
			if ok {
				id = strconv.Itoa(key.ID)
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": ok, "id": id})
}
func hyAPI(path string, body []byte, out interface{}) error {
	method := "GET"
	if body != nil {
		method = "POST"
	}
	req, e := http.NewRequest(method, "http://127.0.0.1:18742/"+path, bytes.NewReader(body))
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", os.Getenv("XUI_HY2_SECRET"))
	resp, e := hyHTTP.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HY2 stats HTTP %d", resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
	}
	return nil
}
func CollectHy() error {
	epochPath := os.Getenv("XUI_HY2_EPOCH_FILE")
	if epochPath == "" {
		epochPath = "/run/x-ui-hy2/epoch"
	}
	return collectHyEpoch(func() ([]byte, error) { return os.ReadFile(epochPath) })
}
func collectHyEpoch(readEpoch func() ([]byte, error)) error {
	hyLock.Lock()
	defer hyLock.Unlock()
	epoch, e := readEpoch()
	if e != nil {
		return e
	}
	counters := map[string]struct{ Tx, Rx int64 }{}
	if e = hyAPI("traffic", nil, &counters); e != nil {
		return e
	}
	after, e := readEpoch()
	if e != nil || !bytes.Equal(after, epoch) {
		return errors.New("HY2 进程已切换")
	}
	for id, v := range counters {
		e = hyDB().Transaction(func(tx *gorm.DB) error {
			var key model.HysteriaCredential
			if e := tx.Where("id=?", id).First(&key).Error; e != nil {
				return e
			}
			up, down := v.Tx, v.Rx
			if key.Epoch == string(epoch) {
				up -= key.Tx
				down -= key.Rx
			}
			if up < 0 || down < 0 {
				return errors.New("HY2 计数回退")
			}
			if up == 0 && down == 0 {
				return nil
			}
			var inbound model.Inbound
			if e := tx.First(&inbound, key.InboundID).Error; e != nil {
				return e
			}
			if e := persistTraffic(tx, []*xray.Traffic{{IsInbound: true, IsUser: true, Tag: inbound.Tag, UserTag: inbound.Tag, ClientID: key.ClientID, Up: up, Down: down}}); e != nil {
				return e
			}
			return tx.Model(&key).Updates(map[string]interface{}{"tx": v.Tx, "rx": v.Rx, "epoch": string(epoch)}).Error
		})
		if e != nil {
			return e
		}
	}
	online := map[string]int{}
	if e = hyAPI("online", nil, &online); e != nil {
		return e
	}
	kick := []string{}
	for id := range online {
		var k model.HysteriaCredential
		if e := hyDB().Where("id=?", id).Find(&k).Error; e != nil {
			return e
		}
		allowed := false
		if k.Enabled {
			in, e := (&InboundService{}).GetInbound(k.InboundID)
			allowed = e == nil && ValidateInboundUserAccess(in, k.ClientID) == nil
		}
		if !allowed {
			kick = append(kick, id)
		}
	}
	if len(kick) > 0 {
		b, _ := json.Marshal(kick)
		return hyAPI("kick", b, nil)
	}
	return nil
}
func hyProxy(inboundID int, client string) (*clashVLESSProxy, error) {
	if !HyAvailable() {
		return nil, nil
	}
	host, port := hyPublicEndpoint()
	if host == "" {
		return nil, nil
	}
	k, e := hyCurrent(inboundID, client)
	if e != nil || !k.Enabled {
		return nil, e
	}
	return &clashVLESSProxy{Name: hyProxyName, Type: "hysteria2", Server: host, Port: port, Password: k.Secret, SNI: host, UDP: true}, nil
}
