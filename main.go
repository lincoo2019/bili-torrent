// bili-torrent：扫描 bilibili 下载目录，识别视频、制作种子并提供网页管理界面。
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bili-torrent/internal/config"
	"bili-torrent/internal/server"
	"bili-torrent/internal/store"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "", "配置文件路径（默认自动查找 config.yaml / /config/config.yaml）")
	showVersion := flag.Bool("version", false, "显示版本号")
	flag.Parse()

	if *showVersion {
		log.Printf("bili-torrent %s", version)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("初始化存储失败: %v", err)
	}

	srv := server.New(cfg, st)
	srv.LoadLibrary() // 加载上次扫描结果，页面立即可用（无需等待扫描）
	srv.StartScan()   // 后台增量扫描（保留探测缓存，只处理新增/变化内容）
	srv.StartPeriodicScan()

	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 30 * time.Second,
	}
	go func() {
		log.Printf("bili-torrent %s 已启动，网页: http://localhost%s", version, displayAddr(cfg.Listen))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP 服务错误: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("正在退出...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
}

// displayAddr 将 ":8080" 之类地址转换为便于访问的展示地址。
func displayAddr(addr string) string {
	if len(addr) > 0 && addr[0] == ':' {
		return addr
	}
	return addr
}
