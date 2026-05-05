package api

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"s-matrix/core/singbox"
)

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func currentSingboxVersion() string {
	out, err := exec.Command("sing-box", "version").CombinedOutput()
	if err != nil {
		return "unknown"
	}
	if m := regexp.MustCompile(`version\s+(\S+)`).FindStringSubmatch(string(out)); len(m) > 1 {
		return m[1]
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
}

func fetchSingboxReleases(limit int) ([]githubRelease, error) {
	if limit <= 0 || limit > 30 {
		limit = 12
	}
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("https://api.github.com/repos/SagerNet/sing-box/releases?per_page=%d", limit), nil)
	req.Header.Set("User-Agent", "S-Matrix/singbox-version")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("github releases HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}
	return releases, nil
}

func singboxAssetName(version string) string {
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "amd64"
	case "arm64":
		arch = "arm64"
	case "386":
		arch = "386"
	}
	return fmt.Sprintf("sing-box-%s-linux-%s.tar.gz", strings.TrimPrefix(version, "v"), arch)
}

func findRelease(releases []githubRelease, version string) (githubRelease, bool) {
	for _, r := range releases {
		if r.TagName == version || strings.TrimPrefix(r.TagName, "v") == strings.TrimPrefix(version, "v") {
			return r, true
		}
	}
	return githubRelease{}, false
}

func releaseAssetURL(r githubRelease, version string) (string, error) {
	want := singboxAssetName(version)
	for _, a := range r.Assets {
		if a.Name == want && a.BrowserDownloadURL != "" {
			return a.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("asset not found: %s", want)
}

func installSingboxFromTarGz(downloadURL, version string) error {
	tmp, err := os.MkdirTemp("", "smatrix-singbox-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	req, _ := http.NewRequest(http.MethodGet, downloadURL, nil)
	req.Header.Set("User-Agent", "S-Matrix/singbox-installer")
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("download HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var extracted string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if h.FileInfo().IsDir() || filepath.Base(h.Name) != "sing-box" {
			continue
		}
		extracted = filepath.Join(tmp, "sing-box")
		out, err := os.OpenFile(extracted, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, tr)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		break
	}
	if extracted == "" {
		return fmt.Errorf("sing-box binary not found in archive for %s", version)
	}
	if out, err := exec.Command(extracted, "version").CombinedOutput(); err != nil {
		return fmt.Errorf("downloaded sing-box version failed: %s: %w", string(out), err)
	}
	backup := fmt.Sprintf("/usr/local/bin/sing-box.bak-%s", time.Now().Format("20060102-150405"))
	if _, err := os.Stat("/usr/local/bin/sing-box"); err == nil {
		_ = os.Rename("/usr/local/bin/sing-box", backup)
	}
	if err := os.Rename(extracted, "/usr/local/bin/sing-box"); err != nil {
		// Cross-device fallback, just in case.
		in, openErr := os.Open(extracted)
		if openErr != nil {
			return err
		}
		defer in.Close()
		out, createErr := os.OpenFile("/usr/local/bin/sing-box", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if createErr != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return os.Chmod("/usr/local/bin/sing-box", 0755)
}

func SingboxVersionHandler(c *gin.Context) {
	releases, err := fetchSingboxReleases(20)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "current": currentSingboxVersion()})
		return
	}
	stable, alpha := "", ""
	versions := make([]gin.H, 0, len(releases))
	for _, r := range releases {
		if r.TagName == "" {
			continue
		}
		if stable == "" && !r.Prerelease {
			stable = r.TagName
		}
		if alpha == "" && r.Prerelease {
			alpha = r.TagName
		}
		versions = append(versions, gin.H{"tag": r.TagName, "prerelease": r.Prerelease})
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "current": currentSingboxVersion(), "stable": stable, "alpha": alpha, "versions": versions})
}

type singboxSwitchRequest struct {
	Version string `json:"version"`
	Channel string `json:"channel"`
}

func switchSingboxVersion(req singboxSwitchRequest) (gin.H, int) {
	releases, err := fetchSingboxReleases(30)
	if err != nil {
		return gin.H{"error": err.Error()}, http.StatusInternalServerError
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		wantAlpha := strings.EqualFold(req.Channel, "alpha") || strings.EqualFold(req.Channel, "dev")
		for _, r := range releases {
			if r.TagName == "" {
				continue
			}
			if wantAlpha == r.Prerelease {
				version = r.TagName
				break
			}
		}
	}
	if version == "" {
		return gin.H{"error": "version required"}, http.StatusBadRequest
	}
	rel, ok := findRelease(releases, version)
	if !ok {
		return gin.H{"error": "version not found in recent releases"}, http.StatusBadRequest
	}
	assetURL, err := releaseAssetURL(rel, rel.TagName)
	if err != nil {
		return gin.H{"error": err.Error()}, http.StatusBadRequest
	}
	if err := installSingboxFromTarGz(assetURL, rel.TagName); err != nil {
		return gin.H{"error": err.Error()}, http.StatusInternalServerError
	}
	return gin.H{"ok": true, "current": currentSingboxVersion(), "version": rel.TagName}, http.StatusOK
}

func SingboxSwitchHandler(c *gin.Context) {
	var req singboxSwitchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	body, status := switchSingboxVersion(req)
	c.JSON(status, body)
}

func SingboxSwitchWithManagerHandler(manager *singbox.SingboxManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req singboxSwitchRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		body, status := switchSingboxVersion(req)
		if status >= 400 {
			c.JSON(status, body)
			return
		}
		if manager != nil {
			if err := manager.Restart(); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "sing-box installed but restart failed: " + err.Error(), "current": currentSingboxVersion()})
				return
			}
		}
		body["running"] = manager == nil || manager.Status()
		c.JSON(http.StatusOK, body)
	}
}
