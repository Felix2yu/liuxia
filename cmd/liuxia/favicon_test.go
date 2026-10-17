package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// favicon 由内嵌资源直出，是浏览器/爬虫的默认探测点：
// GET 要给本体与正确 Content-Type，非 GET 要 405，资源缺失要 404。
// 这三条都在启动路径上，缺任何一条都会变成线上 500 或静默空响应。
func TestServeFavicon(t *testing.T) {
	t.Run("GET 返回图标本体", func(t *testing.T) {
		w := httptest.NewRecorder()
		serveFavicon(w, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200，实际 %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "image/x-icon" {
			t.Fatalf("Content-Type 期望 image/x-icon，实际 %q", ct)
		}
		if w.Body.Len() == 0 {
			t.Fatal("响应体为空：favicon 未内嵌进二进制")
		}
	})

	t.Run("非 GET 返回 405", func(t *testing.T) {
		w := httptest.NewRecorder()
		serveFavicon(w, httptest.NewRequest(http.MethodPost, "/favicon.ico", nil))
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("期望 405，实际 %d", w.Code)
		}
	})

	t.Run("资源缺失返回 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/x.ico", nil)
		serveAsset(w, req, "static/not-exist.ico", "image/x-icon")
		if w.Code != http.StatusNotFound {
			t.Fatalf("期望 404，实际 %d", w.Code)
		}
	})
}
