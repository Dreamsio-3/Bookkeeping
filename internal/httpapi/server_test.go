package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"bookkeeping/internal/db"
	"bookkeeping/internal/ledger"
)

func newTestServer(t *testing.T, password string) *Server {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if err := db.Migrate(t.Context(), conn); err != nil {
		t.Fatalf("初始化表结构失败: %v", err)
	}

	static := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>ok</html>")},
	}
	return New(ledger.New(conn), fs.FS(static), password)
}

func do(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("解析响应失败: %v，原始内容 %s", err, rec.Body.String())
	}
}

func TestEntryCRUD(t *testing.T) {
	s := newTestServer(t, "")

	rec := do(t, s, http.MethodPost, "/api/entries", `{"kind":"expense","amountCents":2500,"occurredAt":"2026-06-01T09:30","person":"张三","category":"餐饮","note":"午饭"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("新建应返回 201，实际 %d：%s", rec.Code, rec.Body.String())
	}
	var created ledger.Entry
	decode(t, rec, &created)
	if created.ID == "" || created.AmountCents != 2500 {
		t.Fatalf("新建结果不符: %+v", created)
	}

	rec = do(t, s, http.MethodGet, "/api/entries/"+created.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("查询应返回 200，实际 %d", rec.Code)
	}

	rec = do(t, s, http.MethodPut, "/api/entries/"+created.ID, `{"kind":"income","amountCents":500000,"occurredAt":"2026-06-02T10:00","person":"张三","category":"工资","note":"六月工资"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("更新应返回 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	var updated ledger.Entry
	decode(t, rec, &updated)
	if updated.Kind != ledger.KindIncome || updated.AmountCents != 500000 {
		t.Fatalf("更新结果不符: %+v", updated)
	}

	rec = do(t, s, http.MethodGet, "/api/entries?limit=10", "")
	var list struct {
		Items []ledger.Entry `json:"items"`
		Total int64          `json:"total"`
	}
	decode(t, rec, &list)
	if list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("列表结果不符: %+v", list)
	}

	rec = do(t, s, http.MethodGet, "/api/summary", "")
	var sum ledger.Summary
	decode(t, rec, &sum)
	if sum.IncomeCents != 500000 || sum.ExpenseCents != 0 || sum.BalanceCents != 500000 {
		t.Fatalf("合计结果不符: %+v", sum)
	}

	rec = do(t, s, http.MethodDelete, "/api/entries/"+created.ID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("删除应返回 204，实际 %d", rec.Code)
	}

	rec = do(t, s, http.MethodGet, "/api/entries/"+created.ID, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("删除后应返回 404，实际 %d", rec.Code)
	}
}

func TestInvalidRequests(t *testing.T) {
	s := newTestServer(t, "")

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		expect int
	}{
		{"金额为零", http.MethodPost, "/api/entries", `{"kind":"expense","amountCents":0,"occurredAt":"2026-06-01T09:30"}`, http.StatusBadRequest},
		{"类型非法", http.MethodPost, "/api/entries", `{"kind":"transfer","amountCents":100,"occurredAt":"2026-06-01T09:30"}`, http.StatusBadRequest},
		{"非法 JSON", http.MethodPost, "/api/entries", `{`, http.StatusBadRequest},
		{"未知字段", http.MethodPost, "/api/entries", `{"kind":"expense","amountCents":1,"occurredAt":"2026-06-01T09:30","owner":"x"}`, http.StatusBadRequest},
		{"不存在的记录", http.MethodGet, "/api/entries/999999", "", http.StatusNotFound},
		{"SQL 注入式 ID", http.MethodDelete, "/api/entries/1%20OR%201=1", "", http.StatusNotFound},
		{"非法日期筛选", http.MethodGet, "/api/entries?from=昨天", "", http.StatusBadRequest},
		{"非法 limit", http.MethodGet, "/api/entries?limit=abc", "", http.StatusBadRequest},
		{"非法导出类型", http.MethodGet, "/api/export?kind=xxx", "", http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, s, tc.method, tc.path, tc.body)
			if rec.Code != tc.expect {
				t.Fatalf("期望状态码 %d，实际 %d：%s", tc.expect, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestExportEndpoints(t *testing.T) {
	s := newTestServer(t, "")
	do(t, s, http.MethodPost, "/api/entries", `{"kind":"expense","amountCents":1234,"occurredAt":"2026-06-01T09:30","person":"张三","category":"餐饮","note":"午饭"}`)

	rec := do(t, s, http.MethodGet, "/api/export?format=csv", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("CSV 导出失败: %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("CSV 类型头不符: %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, ".csv") {
		t.Fatalf("CSV 文件名头不符: %q", cd)
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("CSV 缺少 BOM")
	}
	if !strings.Contains(rec.Body.String(), "12.34") {
		t.Fatal("CSV 内容缺少金额")
	}

	rec = do(t, s, http.MethodGet, "/api/export?format=xlsx", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("Excel 导出失败: %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "spreadsheetml") {
		t.Fatalf("xlsx 类型头不符: %q", ct)
	}
	// zip 魔数 PK\x03\x04
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte("PK\x03\x04")) {
		t.Fatal("xlsx 不是 zip 文件")
	}
}

func TestAuthEnabled(t *testing.T) {
	s := newTestServer(t, "secret123")

	rec := do(t, s, http.MethodGet, "/api/entries", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应返回 401，实际 %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/entries", nil)
	req.SetBasicAuth("anyone", "secret123")
	authed := httptest.NewRecorder()
	s.Routes().ServeHTTP(authed, req)
	if authed.Code != http.StatusOK {
		t.Fatalf("正确口令应返回 200，实际 %d", authed.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/entries", nil)
	req.SetBasicAuth("anyone", "wrong")
	bad := httptest.NewRecorder()
	s.Routes().ServeHTTP(bad, req)
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("错误口令应返回 401，实际 %d", bad.Code)
	}

	// 健康检查不要求登录，供容器探活。
	health := do(t, s, http.MethodGet, "/api/health", "")
	if health.Code != http.StatusOK {
		t.Fatalf("健康检查应返回 200，实际 %d", health.Code)
	}
}

func TestStaticFiles(t *testing.T) {
	s := newTestServer(t, "")
	rec := do(t, s, http.MethodGet, "/", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("首页应返回 200，实际 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ok") {
		t.Fatal("首页内容不符")
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("缺少安全响应头")
	}
}

func TestMetaEndpoint(t *testing.T) {
	s := newTestServer(t, "")
	do(t, s, http.MethodPost, "/api/entries", `{"kind":"expense","amountCents":100,"occurredAt":"2026-06-01T09:30","person":"张三","category":"餐饮"}`)

	rec := do(t, s, http.MethodGet, "/api/meta", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("筛选项接口失败: %d", rec.Code)
	}
	var meta ledger.Meta
	decode(t, rec, &meta)
	if len(meta.Persons) != 1 || meta.Persons[0] != "张三" {
		t.Fatalf("人员列表不符: %v", meta.Persons)
	}
	if len(meta.Categories) != 1 || meta.Categories[0] != "餐饮" {
		t.Fatalf("分类列表不符: %v", meta.Categories)
	}
}
