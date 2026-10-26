package backup

import (
	"context"
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Backup 负责每日生成一份数据库快照并清理过期文件。
// 数据同时保存在 Docker 数据卷里，这里的快照用于防止误删和库文件损坏。
type Backup struct {
	db       *sql.DB
	dir      string
	keepDays int
}

func New(db *sql.DB, dir string, keepDays int) *Backup {
	return &Backup{db: db, dir: dir, keepDays: keepDays}
}

func (b *Backup) Run(ctx context.Context) {
	if err := os.MkdirAll(b.dir, 0o755); err != nil {
		log.Printf("创建备份目录失败: %v", err)
		return
	}
	b.once(ctx)

	// 每天凌晨 3 点备份；容器重启后重新对齐到下一个 3 点。
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, now.Location())
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			b.once(ctx)
		}
	}
}

func (b *Backup) once(ctx context.Context) {
	// 当天已有备份就跳过，避免重启后重复占用空间。
	name := "bookkeeping-" + time.Now().Format("2006-01-02") + ".db"
	target := filepath.Join(b.dir, name)
	if _, err := os.Stat(target); err == nil {
		b.prune()
		return
	}

	// VACUUM INTO 生成的文件是一致的快照，不需要停服务。
	if _, err := b.db.ExecContext(ctx, "VACUUM INTO ?", target); err != nil {
		log.Printf("数据库备份失败: %v", err)
		return
	}
	log.Printf("数据库已备份到 %s", target)
	b.prune()
}

func (b *Backup) prune() {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		return
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "bookkeeping-") && strings.HasSuffix(entry.Name(), ".db") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	keep := b.keepDays
	if keep < 1 {
		keep = 1
	}
	for len(names) > keep {
		oldest := names[0]
		names = names[1:]
		if err := os.Remove(filepath.Join(b.dir, oldest)); err != nil {
			log.Printf("删除过期备份 %s 失败: %v", oldest, err)
			continue
		}
		log.Printf("已删除过期备份 %s", oldest)
	}
}
