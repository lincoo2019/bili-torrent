package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"bili-torrent/internal/library"
	"bili-torrent/internal/store"
	btorrent "bili-torrent/internal/torrent"
)

// torrentView 是种子的展示信息。
type torrentView struct {
	InfoHash   string `json:"info_hash"`
	Name       string `json:"name"`
	FileName   string `json:"file_name"`
	Folder     string `json:"folder"`
	Size       int64  `json:"size"`
	FileCount  int    `json:"file_count"`
	IndexCount int    `json:"index_count"`
}

// videoView 是视频卡片的展示信息。
type videoView struct {
	Key         string       `json:"key"`
	BVID        string       `json:"bvid"`
	CID         string       `json:"cid,omitempty"`
	Title       string       `json:"title"`
	PageTitle   string       `json:"page_title,omitempty"`
	Intro       string       `json:"intro"`
	UpperName   string       `json:"upper_name"`
	Season      int          `json:"season"`
	Episode     int          `json:"episode"`
	Folder      string       `json:"folder"`
	File        string       `json:"file"`
	TorrentPath string       `json:"torrent_path"`
	Size        int64        `json:"size"`
	PosterURL   string       `json:"poster_url"`
	Types       []string     `json:"types"`
	Source      string       `json:"source"`
	URL         string       `json:"url"`
	Torrent     *torrentView `json:"torrent"`
}

func (s *Server) torrentViewOf(t *store.TorrentRecord) *torrentView {
	if t == nil {
		return nil
	}
	return &torrentView{
		InfoHash:   t.InfoHash,
		Name:       t.Name,
		FileName:   filepath.Base(t.TorrentFile),
		Folder:     t.Folder,
		Size:       t.Size,
		FileCount:  t.FileCount,
		IndexCount: len(t.IndexKeys),
	}
}

func (s *Server) videoViewOf(v *library.Video) *videoView {
	vv := &videoView{
		Key:         v.Key,
		BVID:        v.BVID,
		CID:         v.CID,
		Title:       v.Title,
		PageTitle:   v.PageTitle,
		Intro:       v.Intro,
		UpperName:   v.UpperName,
		Season:      v.Season,
		Episode:     v.Episode,
		Folder:      v.Folder,
		File:        v.File,
		TorrentPath: v.TorrentPath,
		Size:        v.Size,
		Types:       v.Types,
		Source:      v.Source,
		URL:         v.URL,
	}
	if v.Poster != "" {
		vv.PosterURL = "/api/poster?path=" + url.QueryEscape(v.Poster)
	}
	vv.Torrent = s.torrentViewOf(s.store.TorrentByFolder(v.Folder))
	return vv
}

// handleVideos GET /api/videos?status=&type=&source=&q=
func (s *Server) handleVideos(w http.ResponseWriter, r *http.Request) {
	lib := s.Library()
	if lib == nil {
		writeJSON(w, http.StatusOK, map[string]any{"videos": []any{}, "types": []any{}, "sources": []any{}, "stats": map[string]int{}})
		return
	}
	q := r.URL.Query()
	status := q.Get("status")
	typ := q.Get("type")
	source := q.Get("source")
	search := strings.ToLower(strings.TrimSpace(q.Get("q")))

	views := make([]*videoView, 0, len(lib.Videos))
	typesSet := map[string]bool{}
	sourcesSet := map[string]bool{}
	stats := map[string]int{"total": 0, "made": 0, "unmade": 0}

	for _, v := range lib.Videos {
		hasTorrent := s.store.TorrentByFolder(v.Folder) != nil
		if status == "made" && !hasTorrent {
			continue
		}
		if status == "unmade" && hasTorrent {
			continue
		}
		if typ != "" && !contains(v.Types, typ) {
			continue
		}
		if source != "" && v.Source != source {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(v.Title), search) && !strings.Contains(strings.ToLower(v.BVID), search) {
			continue
		}
		for _, t := range v.Types {
			typesSet[t] = true
		}
		if v.Source != "" {
			sourcesSet[v.Source] = true
		}
		stats["total"]++
		if hasTorrent {
			stats["made"]++
		} else {
			stats["unmade"]++
		}
		views = append(views, s.videoViewOf(v))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"videos":  views,
		"types":   sortedKeys(typesSet),
		"sources": sortedKeys(sourcesSet),
		"stats":   stats,
	})
}

// handleTorrents GET /api/torrents
func (s *Server) handleTorrents(w http.ResponseWriter, r *http.Request) {
	records := s.store.AllTorrents()
	views := make([]*torrentView, 0, len(records))
	for _, t := range records {
		views = append(views, s.torrentViewOf(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"torrents": views})
}

// handleCreateTorrents POST /api/torrents {"folders": ["/abs/path", ...]}
func (s *Server) handleCreateTorrents(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Folders []string `json:"folders"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误: %v", err)
		return
	}
	if len(req.Folders) == 0 {
		writeError(w, http.StatusBadRequest, "folders 不能为空")
		return
	}

	lib := s.Library()
	type respItem struct {
		Folder string `json:"folder"`
		TaskID string `json:"task_id"`
		Error  string `json:"error,omitempty"`
	}
	var resp []respItem
	for _, folder := range req.Folders {
		f := findFolder(lib, folder)
		if f == nil {
			resp = append(resp, respItem{Folder: folder, Error: "文件夹不在扫描结果中，请先重新扫描"})
			continue
		}
		if s.store.TorrentByFolder(folder) != nil {
			resp = append(resp, respItem{Folder: folder, Error: "该文件夹已制作过种子"})
			continue
		}
		t := s.newTask(f)
		resp = append(resp, respItem{Folder: folder, TaskID: t.ID})
		go s.runTask(t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": resp})
}

func findFolder(lib *library.Library, path string) *library.Folder {
	if lib == nil {
		return nil
	}
	for _, f := range lib.Folders {
		if f.Path == path {
			return f
		}
	}
	return nil
}

// handleDownloadTorrent GET /api/torrents/{hash}/download
func (s *Server) handleDownloadTorrent(w http.ResponseWriter, r *http.Request) {
	rec := s.store.Torrent(r.PathValue("hash"))
	if rec == nil {
		writeError(w, http.StatusNotFound, "种子不存在")
		return
	}
	w.Header().Set("Content-Type", "application/x-bittorrent")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, url.PathEscape(filepath.Base(rec.TorrentFile))))
	http.ServeFile(w, r, rec.TorrentFile)
}

// handleTorrentIndexes GET /api/torrents/{hash}/indexes
func (s *Server) handleTorrentIndexes(w http.ResponseWriter, r *http.Request) {
	indexes := s.store.IndexesForTorrent(r.PathValue("hash"))
	if indexes == nil {
		indexes = []*store.IndexRecord{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"indexes": indexes})
}

// handleIndexes GET /api/indexes
func (s *Server) handleIndexes(w http.ResponseWriter, r *http.Request) {
	indexes := s.store.AllIndexes()
	if indexes == nil {
		indexes = []*store.IndexRecord{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"indexes": indexes})
}

// handleIndexByKey GET /api/indexes/{key}
func (s *Server) handleIndexByKey(w http.ResponseWriter, r *http.Request) {
	idx := s.store.Index(r.PathValue("key"))
	if idx == nil {
		writeError(w, http.StatusNotFound, "索引不存在")
		return
	}
	writeJSON(w, http.StatusOK, idx)
}

// handleIndexJSON GET /index.json 机器可读的索引清单，供其它应用访问。
func (s *Server) handleIndexJSON(w http.ResponseWriter, r *http.Request) {
	indexes := s.store.AllIndexes()
	if indexes == nil {
		indexes = []*store.IndexRecord{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at": time.Now().Format(time.RFC3339),
		"count":        len(indexes),
		"indexes":      indexes,
	})
}

// handleTasks GET /api/tasks
func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	s.tasksMu.Lock()
	tasks := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		tasks = append(tasks, t)
	}
	s.tasksMu.Unlock()

	views := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		v := map[string]any{
			"id": t.ID, "folder": t.Folder, "folder_name": t.FolderName,
			"status": t.Status, "progress": t.Progress, "message": t.Message,
			"created_at": t.CreatedAt,
		}
		if t.FinishedAt != nil {
			v["finished_at"] = *t.FinishedAt
		}
		if t.Error != "" {
			v["error"] = t.Error
		}
		if t.Result != nil {
			v["result"] = map[string]any{
				"info_hash":    t.Result.InfoHash,
				"name":         t.Result.Name,
				"torrent_file": t.Result.TorrentFile,
				"size":         t.Result.Size,
				"files":        len(t.Result.Files),
				"indexes":      indexKeys(t.Indexes),
			}
		}
		views = append(views, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": views})
}

func indexKeys(indexes []*store.IndexRecord) []string {
	out := make([]string, 0, len(indexes))
	for _, idx := range indexes {
		out = append(out, idx.Key)
	}
	return out
}

// handleRescan POST /api/rescan 启动后台扫描（不阻塞请求）。
func (s *Server) handleRescan(w http.ResponseWriter, r *http.Request) {
	started := s.StartScan()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"running":         true,
		"already_running": !started,
	})
}

// handleScanStatus GET /api/scan 返回后台扫描进度。
func (s *Server) handleScanStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.ScanStatus())
}

// handlePoster GET /api/poster?path=...
func (s *Server) handlePoster(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		writeError(w, http.StatusBadRequest, "缺少 path 参数")
		return
	}
	clean, ok := s.safePath(p)
	if !ok {
		writeError(w, http.StatusForbidden, "路径不在扫描目录内")
		return
	}
	if _, err := os.Stat(clean); err != nil {
		writeError(w, http.StatusNotFound, "海报不存在")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, clean)
}

// handleConfig GET /api/config
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"listen":        s.cfg.Listen,
		"data_dir":      s.cfg.DataDir,
		"torrent_dir":   s.cfg.TorrentDir,
		"roots":         s.cfg.Roots,
		"trackers":      s.cfg.Trackers,
		"private":       s.cfg.Private,
		"comment":       s.cfg.Comment,
		"piece_length":  s.cfg.PieceLength,
		"probe_videos":  s.cfg.ProbeVideos,
		"hdrprobe_path": s.cfg.HDRProbePath,
		"scan_interval": s.cfg.ScanInterval,
		"has_auth":      s.cfg.AuthToken != "",
	})
}

// handleUpdateRoots PUT /api/config/roots {"roots":["/abs/path",...]}
// 更新扫描文件夹列表并持久化到配置文件，随后自动重新扫描。
func (s *Server) handleUpdateRoots(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Roots []string `json:"roots"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误: %v", err)
		return
	}

	roots := make([]string, 0, len(req.Roots))
	seen := map[string]bool{}
	for _, raw := range req.Roots {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		abs, err := filepath.Abs(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "路径 %q 无效: %v", raw, err)
			return
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			// 目录不存在则自动创建（已存在则复用），与启动时自动创建子文件夹的行为一致
			if mkErr := os.MkdirAll(abs, 0o755); mkErr != nil {
				writeError(w, http.StatusBadRequest, "创建目录 %s 失败: %v", abs, mkErr)
				return
			}
		}
		if !seen[abs] {
			seen[abs] = true
			roots = append(roots, abs)
		}
	}
	s.cfg.Roots = roots
	if err := s.cfg.Save(); err != nil {
		writeError(w, http.StatusInternalServerError, "保存配置失败: %v", err)
		return
	}
	started := s.StartScan()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"roots":           roots,
		"scan_started":    started,
		"already_running": !started,
	})
}

// ---------- 任务管理 ----------

var taskSeq atomic.Int64

func (s *Server) newTask(f *library.Folder) *Task {
	t := &Task{
		ID:         fmt.Sprintf("task-%d", taskSeq.Add(1)),
		Folder:     f.Path,
		FolderName: f.Name,
		Status:     "running",
		Message:    "正在计算文件校验和",
		CreatedAt:  time.Now(),
	}
	s.tasksMu.Lock()
	s.tasks[t.ID] = t
	s.tasksMu.Unlock()
	return t
}

func (s *Server) updateTask(t *Task, fn func()) {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()
	fn()
}

func (s *Server) runTask(t *Task) {
	f := findFolder(s.Library(), t.Folder)
	if f == nil {
		s.failTask(t, "文件夹不在扫描结果中，请先重新扫描")
		return
	}
	res, err := btorrent.Create(btorrent.Options{
		Path:           f.Path,
		Name:           f.Name,
		Trackers:       s.cfg.Trackers,
		Comment:        s.cfg.Comment,
		Private:        s.cfg.Private,
		PieceLengthExp: s.cfg.PieceLength,
		Exclude:        s.cfg.Exclude,
		OutputDir:      s.cfg.TorrentDir,
		Progress: func(done, total int) {
			if total > 0 {
				s.updateTask(t, func() { t.Progress = float64(done) / float64(total) })
			}
		},
	})
	if err != nil {
		s.failTask(t, err.Error())
		return
	}

	indexes := make([]*store.IndexRecord, 0, len(f.Videos))
	for _, v := range f.Videos {
		indexes = append(indexes, &store.IndexRecord{
			Key:         v.Key,
			BVID:        v.BVID,
			CID:         v.CID,
			Title:       v.Title,
			PageTitle:   v.PageTitle,
			UpperName:   v.UpperName,
			URL:         v.URL,
			InfoHash:    res.InfoHash,
			TorrentName: res.Name,
			FilePath:    v.TorrentPath,
			Folder:      v.Folder,
			FileSize:    v.Size,
			Types:       v.Types,
			CreatedAt:   time.Now(),
		})
	}

	rec := &store.TorrentRecord{
		InfoHash:    res.InfoHash,
		Name:        res.Name,
		Folder:      f.Path,
		TorrentFile: res.TorrentFile,
		Size:        res.Size,
		FileCount:   len(res.Files),
		CreatedAt:   time.Now(),
	}
	if err := s.store.AddTorrent(rec, indexes); err != nil {
		s.failTask(t, "保存种子记录失败: "+err.Error())
		return
	}

	now := time.Now()
	s.updateTask(t, func() {
		t.Status = "done"
		t.Progress = 1
		t.Message = fmt.Sprintf("已生成种子 %s（%d 个索引）", res.Name, len(indexes))
		t.Result = res
		t.Indexes = indexes
		t.FinishedAt = &now
	})
}

func (s *Server) failTask(t *Task, msg string) {
	now := time.Now()
	s.updateTask(t, func() {
		t.Status = "error"
		t.Error = msg
		t.Message = "失败"
		t.FinishedAt = &now
	})
}

// ---------- 工具函数 ----------

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	// 简单排序（可按需稳定排序）
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
