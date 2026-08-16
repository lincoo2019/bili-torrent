// Package server 提供 HTTP API 与内嵌网页。
package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"bili-torrent/internal/config"
	"bili-torrent/internal/library"
	"bili-torrent/internal/store"
	btorrent "bili-torrent/internal/torrent"
)

//go:embed all:web
var webFS embed.FS

// Server 持有 HTTP 服务运行所需的所有状态。
type Server struct {
	cfg   *config.Config
	store *store.Store

	mu  sync.RWMutex
	lib *library.Library

	tasksMu sync.Mutex
	tasks   map[string]*Task

	scanMu     sync.Mutex // 保证同一时刻只有一个扫描任务
	scanStatMu sync.Mutex
	scanStat   ScanStatus
}

// ScanStatus 是后台扫描任务的进度状态。
type ScanStatus struct {
	Running     bool       `json:"running"`
	Progress    float64    `json:"progress"` // 0..1
	Processed   int        `json:"processed"` // 已处理文件夹单元数
	Total       int        `json:"total"`     // 待处理文件夹单元总数
	CurrentName string     `json:"current_name"`
	Folders     int        `json:"folders"`
	Videos      int        `json:"videos"`
	StartedAt   *time.Time `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	Error       string     `json:"error,omitempty"`
}

// Task 表示一次种子制作任务。
type Task struct {
	ID         string               `json:"id"`
	Folder     string               `json:"folder"`
	FolderName string               `json:"folder_name"`
	Status     string               `json:"status"` // running | done | error
	Progress   float64              `json:"progress"`
	Message    string               `json:"message"`
	Error      string               `json:"error,omitempty"`
	Result     *btorrent.Result     `json:"-"`
	Indexes    []*store.IndexRecord `json:"-"`
	CreatedAt  time.Time            `json:"created_at"`
	FinishedAt *time.Time           `json:"finished_at"`
}

// New 创建 Server。
func New(cfg *config.Config, st *store.Store) *Server {
	return &Server{cfg: cfg, store: st, tasks: map[string]*Task{}}
}

// Library 返回当前扫描结果。
func (s *Server) Library() *library.Library {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lib
}

// setScanStatus 在锁内更新扫描状态。
func (s *Server) setScanStatus(fn func(*ScanStatus)) {
	s.scanStatMu.Lock()
	fn(&s.scanStat)
	s.scanStatMu.Unlock()
}

// ScanStatus 返回当前扫描状态副本。
func (s *Server) ScanStatus() ScanStatus {
	s.scanStatMu.Lock()
	defer s.scanStatMu.Unlock()
	return s.scanStat
}

// StartScan 启动一次后台扫描；若已有扫描在运行则返回 false。
// 扫描完成后自动更新库并写入状态（folders/videos/finished_at）。
func (s *Server) StartScan() bool {
	if !s.scanMu.TryLock() {
		return false // 已有扫描在运行
	}
	now := time.Now()
	s.setScanStatus(func(st *ScanStatus) {
		st.Running = true
		st.Progress = 0
		st.Processed = 0
		st.Total = 0
		st.CurrentName = ""
		st.Folders = 0
		st.Videos = 0
		st.StartedAt = &now
		st.FinishedAt = nil
		st.Error = ""
	})
	go s.runScan()
	return true
}

// runScan 执行扫描主流程（在后台 goroutine 中运行）。
func (s *Server) runScan() {
	defer s.scanMu.Unlock()
	scanner := library.NewScanner(s.cfg.Roots, s.cfg.ProbeVideos, s.cfg.HDRProbePath, probeCache{s.store})
	scanner.SetProgress(func(processed, total int, name string) {
		progress := 0.0
		if total > 0 {
			progress = float64(processed) / float64(total)
		}
		s.setScanStatus(func(st *ScanStatus) {
			st.Processed = processed
			st.Total = total
			st.CurrentName = name
			st.Progress = progress
		})
	})
	lib, err := scanner.Scan()
	now := time.Now()
	if err != nil {
		s.setScanStatus(func(st *ScanStatus) {
			st.Running = false
			st.Progress = 1
			st.Error = err.Error()
			st.FinishedAt = &now
		})
		log.Printf("扫描失败: %v", err)
		return
	}
	s.mu.Lock()
	s.lib = lib
	s.mu.Unlock()
	s.setScanStatus(func(st *ScanStatus) {
		st.Running = false
		st.Progress = 1
		st.Folders = len(lib.Folders)
		st.Videos = len(lib.Videos)
		st.FinishedAt = &now
	})
	log.Printf("扫描完成: %d 个视频文件夹, %d 个视频", len(lib.Folders), len(lib.Videos))
}

// StartPeriodicScan 按配置周期启动自动扫描。
func (s *Server) StartPeriodicScan() {
	if s.cfg.ScanInterval <= 0 {
		return
	}
	ticker := time.NewTicker(time.Duration(s.cfg.ScanInterval) * time.Second)
	go func() {
		for range ticker.C {
			if s.StartScan() {
				log.Printf("自动扫描已启动（后台运行）")
			} else {
				log.Printf("自动扫描跳过：已有扫描在运行")
			}
		}
	}()
}

// probeCache 适配 store 到 library.ProbeCache 接口。
type probeCache struct{ st *store.Store }

func (p probeCache) Get(file string) ([]string, bool) { return p.st.ProbeGet(file) }
func (p probeCache) Set(file string, tags []string)   { p.st.ProbeSet(file, tags) }

// Handler 返回 HTTP 处理器（含 API 与静态资源）。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /api/videos", s.handleVideos)
	mux.HandleFunc("GET /api/torrents", s.handleTorrents)
	mux.HandleFunc("POST /api/torrents", s.handleCreateTorrents)
	mux.HandleFunc("GET /api/torrents/{hash}/download", s.handleDownloadTorrent)
	mux.HandleFunc("GET /api/torrents/{hash}/indexes", s.handleTorrentIndexes)
	mux.HandleFunc("GET /api/indexes", s.handleIndexes)
	mux.HandleFunc("GET /api/indexes/{key}", s.handleIndexByKey)
	mux.HandleFunc("GET /api/tasks", s.handleTasks)
	mux.HandleFunc("POST /api/rescan", s.handleRescan)
	mux.HandleFunc("GET /api/scan", s.handleScanStatus)
	mux.HandleFunc("GET /api/poster", s.handlePoster)
	mux.HandleFunc("GET /api/file", s.handleFile)
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("PUT /api/config/roots", s.handleUpdateRoots)
	mux.HandleFunc("GET /index.json", s.handleIndexJSON)

	return s.auth(mux)
}

// auth 校验 Bearer token。
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AuthToken != "" {
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != s.cfg.AuthToken {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// handleIndex 提供内嵌网页与静态资源（/ 命中 index.html，其余命中对应静态文件）。
// 由于 ServeMux 中 "/" 会兜底所有未匹配的 GET 请求，这里直接交给 FileServer 处理。
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		http.Error(w, "web assets unavailable", http.StatusInternalServerError)
		return
	}
	http.FileServer(http.FS(sub)).ServeHTTP(w, r)
}

// writeJSON 输出 JSON。
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError 输出错误 JSON。
func writeError(w http.ResponseWriter, code int, format string, args ...any) {
	writeJSON(w, code, map[string]string{"error": fmt.Sprintf(format, args...)})
}

// safePath 校验绝对路径位于某个下载根目录内，防止路径穿越。
func (s *Server) safePath(p string) (string, bool) {
	clean, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	for _, root := range s.cfg.Roots {
		rel, err := filepath.Rel(root, clean)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return clean, true
		}
	}
	return "", false
}
