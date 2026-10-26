package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bookkeeping/internal/export"
	"bookkeeping/internal/ledger"
)

const maxBodyBytes = 1 << 20 // 1MB

type Server struct {
	store    *ledger.Store
	static   fs.FS
	password string
}

func New(store *ledger.Store, static fs.FS, password string) *Server {
	return &Server{store: store, static: static, password: strings.TrimSpace(password)}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/entries", s.handleList)
	mux.HandleFunc("POST /api/entries", s.handleCreate)
	mux.HandleFunc("GET /api/entries/{id}", s.handleGet)
	mux.HandleFunc("PUT /api/entries/{id}", s.handleUpdate)
	mux.HandleFunc("DELETE /api/entries/{id}", s.handleDelete)
	mux.HandleFunc("GET /api/summary", s.handleSummary)
	mux.HandleFunc("GET /api/meta", s.handleMeta)
	mux.HandleFunc("GET /api/export", s.handleExport)
	mux.Handle("GET /", http.FileServerFS(s.static))

	return s.securityHeaders(s.auth(mux))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	filter, err := parseFilter(r)
	if err != nil {
		writeError(w, err)
		return
	}
	entries, total, err := s.store.List(r.Context(), filter)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  entries,
		"total":  total,
		"limit":  effectiveLimit(filter.Limit),
		"offset": max(filter.Offset, 0),
	})
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	input, err := decodeInput(r)
	if err != nil {
		writeError(w, err)
		return
	}
	entry, err := s.store.Create(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, entry)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	entry, err := s.store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	input, err := decodeInput(r)
	if err != nil {
		writeError(w, err)
		return
	}
	entry, err := s.store.Update(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	filter, err := parseFilter(r)
	if err != nil {
		writeError(w, err)
		return
	}
	summary, err := s.store.Summary(r.Context(), filter)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	meta, err := s.store.Meta(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	filter, err := parseFilter(r)
	if err != nil {
		writeError(w, err)
		return
	}
	entries, err := s.store.All(r.Context(), filter)
	if err != nil {
		writeError(w, err)
		return
	}

	stamp := time.Now().Format("20060102-1504")
	switch strings.ToLower(r.URL.Query().Get("format")) {
	case "xlsx", "excel":
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", `attachment; filename="bookkeeping-`+stamp+`.xlsx"`)
		if err := export.WriteXLSX(w, entries); err != nil {
			log.Printf("导出 Excel 失败: %v", err)
		}
	default:
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="bookkeeping-`+stamp+`.csv"`)
		if err := export.WriteCSV(w, entries); err != nil {
			log.Printf("导出 CSV 失败: %v", err)
		}
	}
}

func parseFilter(r *http.Request) (ledger.Filter, error) {
	q := r.URL.Query()
	filter := ledger.Filter{
		From:     strings.TrimSpace(q.Get("from")),
		To:       strings.TrimSpace(q.Get("to")),
		Kind:     strings.TrimSpace(q.Get("kind")),
		Person:   strings.TrimSpace(q.Get("person")),
		Category: strings.TrimSpace(q.Get("category")),
		Keyword:  strings.TrimSpace(q.Get("keyword")),
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return filter, fmt.Errorf("%w: limit 必须是非负整数", ledger.ErrInvalidInput)
		}
		filter.Limit = n
	}
	if raw := q.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return filter, fmt.Errorf("%w: offset 必须是非负整数", ledger.ErrInvalidInput)
		}
		filter.Offset = n
	}
	return filter, nil
}

func effectiveLimit(limit int) int {
	if limit <= 0 || limit > 500 {
		return 50
	}
	return limit
}

func decodeInput(r *http.Request) (ledger.EntryInput, error) {
	var input ledger.EntryInput
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		if errors.Is(err, io.EOF) {
			return input, fmt.Errorf("%w: 请求内容为空", ledger.ErrInvalidInput)
		}
		return input, fmt.Errorf("%w: 请求内容不是合法的 JSON", ledger.ErrInvalidInput)
	}
	return input, nil
}

func (s *Server) auth(next http.Handler) http.Handler {
	if s.password == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 健康检查放开，方便容器探活。
		if strings.HasPrefix(r.URL.Path, "/api/health") {
			next.ServeHTTP(w, r)
			return
		}
		_, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(pass), []byte(s.password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Bookkeeping", charset="UTF-8"`)
			http.Error(w, "需要登录", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

type errorBody struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("写入响应失败: %v", err)
	}
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ledger.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Error: "记录不存在"})
	case errors.Is(err, ledger.ErrInvalidInput):
		writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
	default:
		log.Printf("接口错误: %v", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Error: "服务器内部错误"})
	}
}
