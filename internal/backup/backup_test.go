package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bookkeeping/internal/db"
)

func TestBackupCreatesSnapshotAndPrunes(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()

	conn, err := db.Open(filepath.Join(base, "data", "bookkeeping.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer conn.Close()
	if err := db.Migrate(ctx, conn); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO entries (kind, amount_cents, occurred_at, person, category, note, created_at, updated_at)
		VALUES ('income', 5000, '2026-01-01 00:00:00', '张三', '工资', '', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`); err != nil {
		t.Fatalf("插入数据失败: %v", err)
	}

	dir := filepath.Join(base, "backups")
	b := New(conn, dir, 2)

	// 放几个「过期」的备份文件，验证只保留最近 N 份。
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建备份目录失败: %v", err)
	}
	for _, name := range []string{"bookkeeping-2026-01-01.db", "bookkeeping-2026-01-02.db"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("old"), 0o644); err != nil {
			t.Fatalf("写入旧备份失败: %v", err)
		}
	}

	b.once(ctx)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取备份目录失败: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("应保留 2 份备份，实际 %d 份", len(entries))
	}

	today := filepath.Join(dir, "bookkeeping-"+time.Now().Format("2006-01-02")+".db")
	info, err := os.Stat(today)
	if err != nil {
		t.Fatalf("当天备份不存在: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("备份文件不应为空")
	}

	// 再次执行同一天不应产生新文件。
	b.once(ctx)
	again, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取备份目录失败: %v", err)
	}
	if len(again) != 2 {
		t.Fatalf("重复执行不应新增备份，实际 %d 份", len(again))
	}
}
