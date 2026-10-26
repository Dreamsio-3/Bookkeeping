package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMigrateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "migrate.db")

	conn, err := Open(path)
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer conn.Close()

	// 迁移脚本必须可以重复执行：容器重启会再次调用 Migrate。
	for i := 0; i < 3; i++ {
		if err := Migrate(ctx, conn); err != nil {
			t.Fatalf("第 %d 次迁移失败: %v", i+1, err)
		}
	}

	var version int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("读取版本失败: %v", err)
	}
	if version != 1 {
		t.Fatalf("版本应为 1，实际 %d", version)
	}

	// 重复迁移不应重复建表或删除数据。
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO entries (kind, amount_cents, occurred_at, person, category, note, created_at, updated_at)
		VALUES ('expense', 100, '2026-01-01 00:00:00', '', '', '', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`); err != nil {
		t.Fatalf("插入测试数据失败: %v", err)
	}
	if err := Migrate(ctx, conn); err != nil {
		t.Fatalf("再次迁移失败: %v", err)
	}

	var count int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries").Scan(&count); err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("迁移后数据应保留，实际 %d 条", count)
	}
}

func TestCheckConstraintRejectsBadKind(t *testing.T) {
	ctx := context.Background()
	conn, err := Open(filepath.Join(t.TempDir(), "check.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer conn.Close()
	if err := Migrate(ctx, conn); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	_, err = conn.ExecContext(ctx, `
		INSERT INTO entries (kind, amount_cents, occurred_at, person, category, note, created_at, updated_at)
		VALUES ('transfer', 100, '2026-01-01 00:00:00', '', '', '', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`)
	if err == nil {
		t.Fatal("数据库约束应拒绝非法类型")
	}
}
