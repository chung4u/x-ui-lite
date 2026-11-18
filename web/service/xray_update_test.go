package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc"
	stats "x-ui/internal/xraystats"
	"x-ui/xray"
)

type updateStats struct {
	stats.UnimplementedStatsServiceServer
}

func (updateStats) QueryStats(context.Context, *stats.QueryStatsRequest) (*stats.QueryStatsResponse, error) {
	return &stats.QueryStatsResponse{}, nil
}

func testCoreScript(version string, validate, start bool) string {
	return fmt.Sprintf(`#!/bin/sh
case "$1" in
-version) echo 'Xray %s';;
run) %t && test -s "$XRAY_LOCATION_ASSET/geoip.dat" && test -s "$XRAY_LOCATION_ASSET/geosite.dat";;
-c) %t || exit 3; exec sleep 240;;
*) exit 1;;
esac
`, version, validate, start)
}

func TestCoreUpgrade(t *testing.T) {
	for _, tc := range []struct {
		name                                          string
		badDigest, badZip, invalidConfig, failedStart bool
		want                                          string
	}{
		{name: "digest", badDigest: true, want: "SHA-256"},
		{name: "damaged archive", badZip: true, want: "解包"},
		{name: "incompatible configuration", invalidConfig: true, want: "配置检查失败"},
		{name: "candidate fails at startup", failedStart: true, want: "已恢复原版本"},
		{name: "successful replacement"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.Mkdir("bin", 0700); err != nil {
				t.Fatal(err)
			}
			policyTestDB(t)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			grpcServer := grpc.NewServer()
			stats.RegisterStatsServiceServer(grpcServer, updateStats{})
			go grpcServer.Serve(listener)
			defer grpcServer.Stop()
			svc := &ServerService{}
			config, err := svc.xrayService.GetXrayConfig()
			if err != nil {
				t.Fatal(err)
			}
			for i, in := range config.InboundConfigs {
				if in.Tag == "api" {
					config.InboundConfigs[i].Port = listener.Addr().(*net.TCPAddr).Port
				}
			}
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			if err = svc.xrayService.settingService.setString("xrayTemplateConfig", string(raw)); err != nil {
				t.Fatal(err)
			}
			old := map[string]string{xray.GetBinaryName(): testCoreScript("1.0", true, true), "geoip.dat": "old-ip", "geosite.dat": "old-site"}
			next := map[string]string{xray.GetBinaryName(): testCoreScript("2.0", !tc.invalidConfig, !tc.failedStart), "geoip.dat": "new-ip", "geosite.dat": "new-site"}
			for n, body := range old {
				if err = os.WriteFile(filepath.Join("bin", n), []byte(body), 0755); err != nil {
					t.Fatal(err)
				}
			}
			prev, prevResult := p, result
			p = xray.NewProcess(config)
			if err = p.Start(); err != nil {
				t.Fatal(err)
			}
			initial := p
			defer func() {
				if p != nil && p.IsRunning() {
					p.Stop()
				}
				p = prev
				result = prevResult
			}()
			var b bytes.Buffer
			w := zip.NewWriter(&b)
			for n, body := range next {
				if n == xray.GetBinaryName() {
					n = "xray"
				}
				entry, err := w.Create(n)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = entry.Write([]byte(body)); err != nil {
					t.Fatal(err)
				}
			}
			if err = w.Close(); err != nil {
				t.Fatal(err)
			}
			if tc.badZip {
				b.Reset()
				b.WriteString("incomplete ZIP")
			}
			digest := sha256.Sum256(b.Bytes())
			if tc.badDigest {
				digest[0]++
			}
			var api *httptest.Server
			api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/tags/v2.0":
					fmt.Fprintf(w, `{"tag_name":"v2.0","assets":[{"name":%q,"browser_download_url":%q,"digest":"sha256:%x"}]}`, coreArchiveName(), api.URL+"/archive", digest)
				case "/archive":
					w.Write(b.Bytes())
				default:
					http.NotFound(w, r)
				}
			}))
			defer api.Close()
			err = svc.updateCoreFrom(api.URL, "v2.0")
			if (err == nil) != (tc.want == "") || err != nil && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected update result: %v", err)
			}
			want := old
			wantVersion := "1.0"
			if tc.want == "" {
				want = next
				wantVersion = "2.0"
			}
			if !p.IsRunning() || p.GetVersion() != wantVersion {
				t.Fatal("incorrect active core", p.GetVersion())
			}
			if (tc.badDigest || tc.badZip || tc.invalidConfig) && p != initial {
				t.Fatal("old core was interrupted during preflight")
			}
			for n, body := range want {
				got, err := os.ReadFile(filepath.Join("bin", n))
				if err != nil || string(got) != body {
					t.Fatalf("wrong file after update: %s", n)
				}
			}
			if _, err = os.Stat(filepath.Join("bin", coreRollbackDir)); !os.IsNotExist(err) {
				t.Fatal("rollback journal still active")
			}
		})
	}
}

func TestInterruptedCoreBundleRecovery(t *testing.T) {
	bin := t.TempDir()
	stage := t.TempDir()
	for _, n := range coreFiles() {
		if err := os.WriteFile(filepath.Join(bin, n), []byte("old-"+n), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := saveCoreBundle(bin, stage); err != nil {
		t.Fatal(err)
	}
	newFile := filepath.Join(stage, "new")
	if err := os.WriteFile(newFile, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(newFile, filepath.Join(bin, xray.GetBinaryName())); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := recoverCoreUpdate(bin); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range coreFiles() {
		got, err := os.ReadFile(filepath.Join(bin, n))
		if err != nil || string(got) != "old-"+n {
			t.Fatal("incomplete recovery", n)
		}
	}
}
