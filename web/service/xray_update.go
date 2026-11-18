package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"x-ui/xray"
)

const (
	xrayReleaseAPI  = "https://api.github.com/repos/XTLS/Xray-core/releases"
	coreRollbackDir = ".x-ui-core-rollback"
	maxCoreAsset    = 256 << 20
)

var versionTag = regexp.MustCompile(`^v[0-9][0-9A-Za-z.-]{0,63}$`)
var updateMutex sync.Mutex
var releaseHTTP = &http.Client{Timeout: 90 * time.Second}

type coreRelease struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

func fetchReleaseFile(url string, dst io.Writer, limit int64) error {
	resp, err := releaseHTTP.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败，HTTP %d", resp.StatusCode)
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return errors.New("下载文件超过大小限制")
	}
	return nil
}

func (s *ServerService) GetXrayVersions() ([]string, error) {
	var data bytes.Buffer
	if err := fetchReleaseFile(xrayReleaseAPI, &data, 16<<20); err != nil {
		return nil, err
	}
	var releases []coreRelease
	if err := json.Unmarshal(data.Bytes(), &releases); err != nil {
		return nil, err
	}
	var tags []string
	for _, r := range releases {
		if versionTag.MatchString(r.Tag) {
			tags = append(tags, r.Tag)
		}
	}
	return tags, nil
}

func coreArchiveName() string {
	platform := runtime.GOOS
	if platform == "darwin" {
		platform = "macos"
	}
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "64"
	case "arm64":
		arch = "arm64-v8a"
	}
	return fmt.Sprintf("Xray-%s-%s.zip", platform, arch)
}

func downloadCoreArchive(api, tag, dest string) error {
	var data bytes.Buffer
	if err := fetchReleaseFile(api+"/tags/"+tag, &data, 2<<20); err != nil {
		return err
	}
	var release coreRelease
	if err := json.Unmarshal(data.Bytes(), &release); err != nil {
		return err
	}
	if release.Tag != tag {
		return errors.New("发布标签与请求版本不一致")
	}
	name := coreArchiveName()
	var url, digest, digestURL string
	for _, a := range release.Assets {
		switch a.Name {
		case name:
			url = a.URL
			digest = strings.TrimPrefix(a.Digest, "sha256:")
		case name + ".dgst":
			digestURL = a.URL
		}
	}
	if url == "" {
		return fmt.Errorf("版本 %s 缺少 %s", tag, name)
	}
	if digest == "" && digestURL != "" {
		data.Reset()
		if err := fetchReleaseFile(digestURL, &data, 8192); err != nil {
			return err
		}
		for _, line := range strings.Split(data.String(), "\n") {
			key, value, ok := strings.Cut(line, "=")
			if ok && (strings.TrimSpace(key) == "SHA2-256" || strings.TrimSpace(key) == "SHA256") {
				digest = strings.TrimSpace(value)
				break
			}
		}
	}
	want, err := hex.DecodeString(digest)
	if err != nil || len(want) != sha256.Size {
		return errors.New("发行包缺少可验证的 SHA-256")
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	err = fetchReleaseFile(url, io.MultiWriter(f, h), maxCoreAsset)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(want, h.Sum(nil)) {
		return errors.New("Xray 发行包 SHA-256 不匹配")
	}
	return nil
}

func coreFiles() []string { return []string{xray.GetBinaryName(), "geoip.dat", "geosite.dat"} }

func extractCore(archive, stage string) error {
	z, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer z.Close()
	for _, name := range coreFiles() {
		member, mode := name, os.FileMode(0644)
		if name == xray.GetBinaryName() {
			member, mode = "xray", 0755
		}
		src, err := z.Open(member)
		if err != nil {
			return err
		}
		dest, err := os.OpenFile(filepath.Join(stage, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			src.Close()
			return err
		}
		n, copyErr := io.Copy(dest, io.LimitReader(src, maxCoreAsset+1))
		src.Close()
		if copyErr == nil {
			copyErr = dest.Sync()
		}
		closeErr := dest.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if n == 0 || n > maxCoreAsset {
			return fmt.Errorf("发行包内 %s 大小无效", member)
		}
	}
	return nil
}

func syncCoreDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// Publish a complete rollback set before replacing the first live file.
// Hard links retain the original inodes even when installation is interrupted.
func saveCoreBundle(bin, stage string) error {
	backup := filepath.Join(stage, "previous")
	if err := os.Mkdir(backup, 0700); err != nil {
		return err
	}
	for _, name := range coreFiles() {
		if err := os.Link(filepath.Join(bin, name), filepath.Join(backup, name)); err != nil {
			return err
		}
	}
	if err := syncCoreDir(backup); err != nil {
		return err
	}
	if err := os.Rename(backup, filepath.Join(bin, coreRollbackDir)); err != nil {
		return err
	}
	return syncCoreDir(bin)
}

// Rename the journal away before cleaning it up, so startup never observes
// a partly removed backup. The rename is the transaction's commit point.
func finishCoreUpdate(bin, stage string) error {
	journal, finished := filepath.Join(bin, coreRollbackDir), filepath.Join(stage, "finished")
	if err := os.Rename(journal, finished); err != nil {
		return err
	}
	if err := syncCoreDir(bin); err != nil {
		return errors.Join(err, os.Rename(finished, journal))
	}
	return nil
}

func recoverCoreUpdate(bin string) error {
	backup := filepath.Join(bin, coreRollbackDir)
	if _, err := os.Stat(backup); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	stage, err := os.MkdirTemp(bin, ".core-restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, name := range coreFiles() {
		// Retain the journal's copy until the whole previous set is in place.
		tmp := filepath.Join(stage, name)
		if err = os.Link(filepath.Join(backup, name), tmp); err != nil {
			return err
		}
		if err = os.Rename(tmp, filepath.Join(bin, name)); err != nil {
			return err
		}
	}
	if err = syncCoreDir(bin); err != nil {
		return err
	}
	return finishCoreUpdate(bin, stage)
}

func startUpdatedCore(config *xray.Config) error {
	p = xray.NewProcess(config)
	result = ""
	if err := p.Start(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	// Let immediate exit/bind errors surface, then probe the real statistics
	// endpoint without consuming traffic. Legacy configurations may omit it.
	time.Sleep(350 * time.Millisecond)
	for p.IsRunning() {
		if p.GetAPIPort() == 0 {
			return nil
		}
		if _, err := p.GetTrafficContext(ctx, false); err == nil && p.IsRunning() {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("Xray 统计接口启动超时")
		case <-time.After(100 * time.Millisecond):
		}
	}
	return errors.New("Xray 启动后退出")
}

func (s *ServerService) UpdateXray(version string) error {
	return s.updateCoreFrom(xrayReleaseAPI, version)
}

func (s *ServerService) updateCoreFrom(api, version string) error {
	if !versionTag.MatchString(version) {
		return errors.New("无效的 Xray 版本")
	}
	if !updateMutex.TryLock() {
		return errors.New("已有 Xray 升级正在执行")
	}
	defer updateMutex.Unlock()
	bin, err := filepath.Abs(filepath.Dir(xray.GetBinaryPath()))
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(bin, ".core-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	archive := filepath.Join(stage, "release.zip")
	if err = downloadCoreArchive(api, version, archive); err != nil {
		return err
	}
	if err = extractCore(archive, stage); err != nil {
		return fmt.Errorf("发行包无法解包: %w", err)
	}
	return s.xrayService.replaceCore(bin, stage)
}

func (s *XrayService) replaceCore(bin, stage string) (err error) {
	// The Linux installer and scheduled Geo updater use the same file lock.
	f, err := os.OpenFile(filepath.Join(bin, ".x-ui-core.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("无法独占核心文件: %w", err)
	}
	lock.Lock()
	defer lock.Unlock()
	if err = recoverCoreUpdate(bin); err != nil {
		return err
	}
	desired, err := s.GetXrayConfig()
	if err != nil {
		return err
	}
	if err = validateCoreConfig(filepath.Join(stage, xray.GetBinaryName()), stage, desired); err != nil {
		return err
	}
	if err = s.collectTrafficLocked(); err != nil {
		return fmt.Errorf("升级前流量落库失败: %w", err)
	}
	current, err := s.GetXrayConfig()
	if err != nil {
		return err
	}
	if !desired.Equals(current) {
		if err = validateCoreConfig(filepath.Join(stage, xray.GetBinaryName()), stage, current); err != nil {
			return err
		}
		desired = current
	}
	if err = saveCoreBundle(bin, stage); err != nil {
		return err
	}
	previous := desired
	if p != nil && p.IsRunning() {
		previous = p.GetConfig()
		if err = p.Stop(); err != nil {
			return fmt.Errorf("停止原 Xray 失败: %w", err)
		}
	}
	for _, name := range coreFiles() {
		if err = os.Rename(filepath.Join(stage, name), filepath.Join(bin, name)); err != nil {
			break
		}
	}
	if err == nil {
		err = syncCoreDir(bin)
	}
	if err == nil {
		err = startUpdatedCore(desired)
	}
	if err == nil {
		err = finishCoreUpdate(bin, stage)
	}
	if err == nil {
		lastApplyError = ""
		return nil
	}

	failure := err
	if p != nil && p.IsRunning() {
		if stopErr := p.Stop(); stopErr != nil {
			return fmt.Errorf("升级失败 (%v)，停止候选核心失败: %w", failure, stopErr)
		}
	}
	if restoreErr := recoverCoreUpdate(bin); restoreErr != nil {
		return fmt.Errorf("升级失败 (%v)，恢复旧文件失败: %w", failure, restoreErr)
	}
	if restoreErr := startUpdatedCore(previous); restoreErr != nil {
		return fmt.Errorf("升级失败 (%v)，旧核心重新启动失败: %w", failure, restoreErr)
	}
	return fmt.Errorf("Xray 升级失败，已恢复原版本并验证运行: %w", failure)
}
