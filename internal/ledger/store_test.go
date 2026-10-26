package ledger

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"bookkeeping/internal/db"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx := context.Background()
	if err := db.Migrate(ctx, conn); err != nil {
		t.Fatalf("初始化表结构失败: %v", err)
	}
	return New(conn)
}

func mustCreate(t *testing.T, s *Store, in EntryInput) Entry {
	t.Helper()
	e, err := s.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("创建记录失败: %v", err)
	}
	return e
}

func validInput() EntryInput {
	return EntryInput{
		Kind:        KindExpense,
		AmountCents: 1234,
		OccurredAt:  "2026-03-01T12:30",
		Person:      "张三",
		Category:    "餐饮",
		Note:        "午饭",
	}
}

func TestCreateGetUpdateDelete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	created := mustCreate(t, s, validInput())
	if created.ID == "" {
		t.Fatal("新建记录应返回 ID")
	}
	if created.OccurredAt != "2026-03-01 12:30:00" {
		t.Fatalf("时间应统一为数据库格式，实际 %q", created.OccurredAt)
	}
	if created.AmountCents != 1234 {
		t.Fatalf("金额不应被改动，实际 %d", created.AmountCents)
	}

	updated, err := s.Update(ctx, created.ID, EntryInput{
		Kind:        KindIncome,
		AmountCents: 9900,
		OccurredAt:  "2026-03-02 08:00:00",
		Person:      "李四",
		Category:    "工资",
		Note:        "三月工资",
	})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if updated.Kind != KindIncome || updated.AmountCents != 9900 || updated.Person != "李四" {
		t.Fatalf("更新结果不符: %+v", updated)
	}
	if updated.ID != created.ID {
		t.Fatal("更新不应改变 ID")
	}

	if err := s.Delete(ctx, created.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := s.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后应查不到，实际错误 %v", err)
	}
	if err := s.Delete(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应返回不存在，实际 %v", err)
	}
}

func TestGetRejectsNonNumericID(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Get(context.Background(), "1 OR 1=1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("非数字 ID 应视为不存在，实际 %v", err)
	}
}

func TestValidation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	longNote := make([]rune, 501)
	for i := range longNote {
		longNote[i] = '字'
	}

	cases := []struct {
		name string
		edit func(*EntryInput)
	}{
		{"类型非法", func(in *EntryInput) { in.Kind = "transfer" }},
		{"金额为零", func(in *EntryInput) { in.AmountCents = 0 }},
		{"金额为负", func(in *EntryInput) { in.AmountCents = -1 }},
		{"金额过大", func(in *EntryInput) { in.AmountCents = 100_000_000_001 }},
		{"时间格式错误", func(in *EntryInput) { in.OccurredAt = "3月1日" }},
		{"时间为空", func(in *EntryInput) { in.OccurredAt = "" }},
		{"备注超长", func(in *EntryInput) { in.Note = string(longNote) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput()
			tc.edit(&in)
			if _, err := s.Create(ctx, in); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("应返回参数错误，实际 %v", err)
			}
		})
	}

	// 边界值应当被接受。
	ok := validInput()
	ok.AmountCents = 1
	ok.OccurredAt = "2026-03-01"
	mustCreate(t, s, ok)
}

func TestListFiltersAndPaging(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	mustCreate(t, s, EntryInput{Kind: KindExpense, AmountCents: 100, OccurredAt: "2026-01-01 09:00:00", Person: "张三", Category: "餐饮", Note: "早餐"})
	mustCreate(t, s, EntryInput{Kind: KindIncome, AmountCents: 500000, OccurredAt: "2026-01-15 10:00:00", Person: "张三", Category: "工资", Note: "一月工资"})
	mustCreate(t, s, EntryInput{Kind: KindExpense, AmountCents: 2500, OccurredAt: "2026-01-31 23:59:59", Person: "李四", Category: "交通", Note: "打车 100% 报销"})
	mustCreate(t, s, EntryInput{Kind: KindExpense, AmountCents: 800, OccurredAt: "2026-02-01 00:00:00", Person: "李四", Category: "餐饮", Note: "夜宵"})

	all, total, err := s.List(ctx, Filter{})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 4 || len(all) != 4 {
		t.Fatalf("应有 4 条，实际 total=%d len=%d", total, len(all))
	}
	if all[0].OccurredAt != "2026-02-01 00:00:00" {
		t.Fatalf("应按时间倒序，首条为 %q", all[0].OccurredAt)
	}

	// 日期范围含首尾两天。
	fromTo, total, err := s.List(ctx, Filter{From: "2026-01-01", To: "2026-01-31"})
	if err != nil {
		t.Fatalf("按日期查询失败: %v", err)
	}
	if total != 3 || len(fromTo) != 3 {
		t.Fatalf("1 月应有 3 条，实际 %d", total)
	}

	byKind, total, err := s.List(ctx, Filter{Kind: KindExpense})
	if err != nil || total != 3 {
		t.Fatalf("支出应有 3 条，实际 %d（err=%v）", total, err)
	}
	if len(byKind) != 3 {
		t.Fatalf("支出列表应有 3 条，实际 %d", len(byKind))
	}

	_, total, err = s.List(ctx, Filter{Person: "李四"})
	if err != nil || total != 2 {
		t.Fatalf("李四应有 2 条，实际 %d（err=%v）", total, err)
	}

	// 关键词里的 % 应按字面匹配，不能当通配符。
	byKeyword, total, err := s.List(ctx, Filter{Keyword: "100%"})
	if err != nil {
		t.Fatalf("关键词查询失败: %v", err)
	}
	if total != 1 || len(byKeyword) != 1 || byKeyword[0].Category != "交通" {
		t.Fatalf("关键词应精确匹配 1 条，实际 %d", total)
	}

	// 分页。
	page2, total, err := s.List(ctx, Filter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("分页查询失败: %v", err)
	}
	if total != 4 || len(page2) != 2 {
		t.Fatalf("分页结果不符：total=%d len=%d", total, len(page2))
	}
	if page2[0].OccurredAt != "2026-01-15 10:00:00" {
		t.Fatalf("第二页首条应为 1 月 15 日，实际 %q", page2[0].OccurredAt)
	}

	if _, _, err := s.List(ctx, Filter{From: "2026/01/01"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("非法日期应返回参数错误，实际 %v", err)
	}
	if _, _, err := s.List(ctx, Filter{Kind: "other"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("非法类型应返回参数错误，实际 %v", err)
	}
}

func TestSummary(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	mustCreate(t, s, EntryInput{Kind: KindIncome, AmountCents: 10000, OccurredAt: "2026-04-01 10:00:00"})
	mustCreate(t, s, EntryInput{Kind: KindIncome, AmountCents: 2500, OccurredAt: "2026-04-02 10:00:00"})
	mustCreate(t, s, EntryInput{Kind: KindExpense, AmountCents: 3000, OccurredAt: "2026-04-03 10:00:00"})

	sum, err := s.Summary(ctx, Filter{From: "2026-04-01", To: "2026-04-30"})
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if sum.IncomeCents != 12500 || sum.ExpenseCents != 3000 || sum.BalanceCents != 9500 || sum.Count != 3 {
		t.Fatalf("统计结果不符: %+v", sum)
	}

	empty, err := s.Summary(ctx, Filter{From: "2030-01-01"})
	if err != nil {
		t.Fatalf("空区间统计失败: %v", err)
	}
	if empty.Count != 0 || empty.BalanceCents != 0 {
		t.Fatalf("空区间应为 0，实际 %+v", empty)
	}
}

func TestMetaDistinct(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	mustCreate(t, s, EntryInput{Kind: KindExpense, AmountCents: 100, OccurredAt: "2026-05-01 10:00:00", Person: "张三", Category: "餐饮"})
	mustCreate(t, s, EntryInput{Kind: KindExpense, AmountCents: 100, OccurredAt: "2026-05-02 10:00:00", Person: "李四", Category: "餐饮"})
	mustCreate(t, s, EntryInput{Kind: KindExpense, AmountCents: 100, OccurredAt: "2026-05-03 10:00:00"})

	meta, err := s.Meta(ctx)
	if err != nil {
		t.Fatalf("读取筛选项失败: %v", err)
	}
	if len(meta.Persons) != 2 || meta.Persons[0] != "张三" && meta.Persons[1] != "张三" {
		t.Fatalf("人员去重结果不符: %v", meta.Persons)
	}
	if len(meta.Categories) != 1 || meta.Categories[0] != "餐饮" {
		t.Fatalf("分类应去重且排除空值: %v", meta.Categories)
	}
}

func TestParseAmount(t *testing.T) {
	cases := []struct {
		raw    string
		expect int64
		ok     bool
	}{
		{"12.34", 1234, true},
		{"1,234.5", 123450, true},
		{" 88 ", 8800, true},
		{"0.01", 1, true},
		{"0", 0, false},
		{"-5", 0, false},
		{"abc", 0, false},
		{"", 0, false},
	}
	for _, tc := range cases {
		got, err := ParseAmount(tc.raw)
		if tc.ok && err != nil {
			t.Fatalf("%q 应解析成功，实际 %v", tc.raw, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("%q 应解析失败，实际得到 %d", tc.raw, got)
		}
		if tc.ok && got != tc.expect {
			t.Fatalf("%q 应解析为 %d 分，实际 %d", tc.raw, tc.expect, got)
		}
	}
}
