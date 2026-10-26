package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	// 内嵌时区库：镜像里没有 tzdata 时，TZ 环境变量依然生效。
	_ "time/tzdata"

	"bookkeeping/internal/backup"
	"bookkeeping/internal/db"
	"bookkeeping/internal/httpapi"
	"bookkeeping/internal/ledger"
)

// 前端资源编译进二进制，运行时不依赖任何外部文件。
//
//go:embed web
var webFiles embed.FS

func main() {
	log.SetFlags(log.LstdFlags)

	dataDir := envOr("DATA_DIR", "data")
	dbPath := envOr("DB_PATH", filepath.Join(dataDir, "bookkeeping.db"))
	addr := ":" + envOr("PORT", "8080")

	static, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatalf("加载前端资源失败: %v", err)
	}

	conn, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}

	migrateCtx, cancelMigrate := context.WithTimeout(context.Background(), 30*time.Second)
	err = db.Migrate(migrateCtx, conn)
	cancelMigrate()
	if err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}

	api := httpapi.New(ledger.New(conn), static, os.Getenv("APP_PASSWORD"))

	srv := &http.Server{
		Addr:              addr,
		Handler:           api.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       120 * time.Second,
	}

	// 每日自动备份，默认保留 7 天；设为 0 可关闭。
	backupDays := envInt("BACKUP_KEEP_DAYS", 7)
	backupDir := envOr("BACKUP_DIR", filepath.Join(dataDir, "backups"))
	backupCtx, cancelBackup := context.WithCancel(context.Background())
	defer cancelBackup()
	if backupDays > 0 {
		go backup.New(conn, backupDir, backupDays).Run(backupCtx)
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("个人记账已启动：http://localhost%s  数据文件：%s", addr, dbPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		log.Fatalf("服务异常退出: %v", err)
	case sig := <-stop:
		log.Printf("收到信号 %s，正在关闭…", sig)
	}

	cancelBackup()

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("关闭 HTTP 服务时出错: %v", err)
	}

	// 把 WAL 合并回主库，保证直接复制 bookkeeping.db 就是一份完整数据。
	if _, err := conn.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		log.Printf("WAL 检查点失败: %v", err)
	}
	if err := conn.Close(); err != nil {
		log.Printf("关闭数据库失败: %v", err)
	}
	log.Println("已退出")
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}
