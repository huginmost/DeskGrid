package app

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"deskgrid/internal/shell"
	"strconv"
	"strings"
)

func (s *Service) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.URL.Path == "/api/icon" {
			pixels, _ := strconv.Atoi(r.URL.Query().Get("size"))
			if pixels <= 0 {
				pixels = 48
			}
			data, e := shell.FileIconSize(r.URL.Query().Get("path"), pixels)
			if e != nil {
				w.Header().Set("Cache-Control", "no-store")
				slog.Warn("文件图标获取失败", "path", r.URL.Query().Get("path"), "error", e)
				http.Error(w, "icon unavailable", 404)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "private,max-age=60")
			_, _ = w.Write(data)
			return
		}
		if r.URL.Path != "/api/rpc" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != "POST" {
			http.Error(w, `{"error":"POST required"}`, 405)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, `{"error":"JSON required"}`, 415)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, e := url.Parse(origin)
			if e != nil || !strings.EqualFold(u.Host, r.Host) {
				http.Error(w, `{"error":"origin rejected"}`, 403)
				return
			}
		}
		var request struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		e := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&request)
		if e != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "请求不是有效 JSON"})
			return
		}
		defer func() {
			if v := recover(); v != nil {
				_ = json.NewEncoder(w).Encode(map[string]any{"error": fmt.Sprint("内部错误: ", v)})
			}
		}()
		result, e := s.Call(request.Method, request.Params)
		message := ""
		if e != nil {
			message = e.Error()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": result, "error": message})
	})
}
