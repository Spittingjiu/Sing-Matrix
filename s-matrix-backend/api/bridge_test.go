package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestConnectSubUsesBridge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var gotPath string
	var gotBody string
	subgo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"source_id":7,"name":"S-Matrix"}`))
	}))
	defer subgo.Close()

	r := gin.New()
	r.POST("/connect", ConnectSubHandler())
	body := `{"subgo_url":"` + subgo.URL + `","username":"admin","password":"pw","name":"S-Matrix"}`
	req := httptest.NewRequest(http.MethodPost, "/connect", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Host = "sbui.example.com"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if gotPath != "/api/bridge/push-source" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	for _, want := range []string{`"source_type":"sbui"`, `"panel_url":"https://sbui.example.com"`, `"panel_token":"`} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("bridge body missing %s: %s", want, gotBody)
		}
	}
}

func TestConnectSubFallsBackToSourcesAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	paths := []string{}
	subgo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/bridge/push-source":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"ok":false,"error":"not found"}`))
		case "/api/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "subgo_session", Value: "sess-token", Path: "/"})
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/sources":
			cookie, err := r.Cookie("subgo_session")
			if err != nil || cookie.Value != "sess-token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"ok":false,"error":"unauthorized"}`))
				return
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer subgo.Close()

	r := gin.New()
	r.POST("/connect", ConnectSubHandler())
	req := httptest.NewRequest(http.MethodPost, "/connect", strings.NewReader(`{"subgo_url":"`+subgo.URL+`","username":"admin","password":"pw"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"via":"sources-api"`) {
		t.Fatalf("expected fallback path: %s", w.Body.String())
	}
	if len(paths) != 3 {
		t.Fatalf("unexpected call sequence: %v", paths)
	}
}

func TestConnectSubRejectsWrongCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	subgo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid credentials"}`))
	}))
	defer subgo.Close()

	r := gin.New()
	r.POST("/connect", ConnectSubHandler())
	req := httptest.NewRequest(http.MethodPost, "/connect", strings.NewReader(`{"subgo_url":"`+subgo.URL+`","username":"admin","password":"bad"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "登录失败") {
		t.Fatalf("expected login failure hint: %s", w.Body.String())
	}
}
