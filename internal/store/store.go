// Package store 使用 JSON 文件持久化种子记录、索引记录与 ffprobe 探测缓存。
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// TorrentRecord 记录一个已生成的种子。
type TorrentRecord struct {
	InfoHash    string    `json:"info_hash"`
	Name        string    `json:"name"`
	Folder      string    `json:"folder"`
	TorrentFile string    `json:"torrent_file"` // .torrent 文件绝对路径
	Size        int64     `json:"size"`
	FileCount   int       `json:"file_count"`
	IndexKeys   []string  `json:"index_keys"` // 指向该种子的索引 key 列表
	CreatedAt   time.Time `json:"created_at"`
}

// IndexRecord 是 bilibili 唯一索引条目：其它应用可通过 bvid 关联到同一个视频。
type IndexRecord struct {
	Key         string    `json:"key"` // 唯一 key：单页为 bvid，多页为 bvid_分页
	BVID        string    `json:"bvid"`
	CID         string    `json:"cid,omitempty"`
	Title       string    `json:"title"`
	PageTitle   string    `json:"page_title,omitempty"`
	UpperName   string    `json:"upper_name,omitempty"`
	URL         string    `json:"url"` // B 站原链接
	InfoHash    string    `json:"info_hash"`
	TorrentName string    `json:"torrent_name"`
	FilePath    string    `json:"file_path"` // 在种子内的路径（斜杠分隔）
	Folder      string    `json:"folder"`
	FileSize    int64     `json:"file_size"`
	Types       []string  `json:"types"`
	CreatedAt   time.Time `json:"created_at"`
}

// DB 是持久化数据集合。
type DB struct {
	Torrents map[string]*TorrentRecord `json:"torrents"` // key: info_hash
	Indexes  map[string]*IndexRecord   `json:"indexes"`  // key: 索引 key
	Probe    map[string][]string       `json:"probe"`    // key: 视频文件路径
}

// Store 管理持久化。
type Store struct {
	mu     sync.Mutex
	dir    string
	dbPath string
	db     *DB
}

// Open 打开（或创建）数据目录下的 store。
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	s := &Store{dir: dir, dbPath: filepath.Join(dir, "db.json")}
	data, err := os.ReadFile(s.dbPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.db = &DB{
				Torrents: map[string]*TorrentRecord{},
				Indexes:  map[string]*IndexRecord{},
				Probe:    map[string][]string{},
			}
			return s, s.save()
		}
		return nil, fmt.Errorf("读取数据文件失败: %w", err)
	}
	s.db = &DB{}
	if err := json.Unmarshal(data, s.db); err != nil {
		return nil, fmt.Errorf("解析数据文件失败: %w", err)
	}
	if s.db.Torrents == nil {
		s.db.Torrents = map[string]*TorrentRecord{}
	}
	if s.db.Indexes == nil {
		s.db.Indexes = map[string]*IndexRecord{}
	}
	if s.db.Probe == nil {
		s.db.Probe = map[string][]string{}
	}
	return s, nil
}

// save 原子写入 db.json。
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.db, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.dbPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.dbPath)
}

// AddTorrent 保存种子记录并关联索引。
func (s *Store) AddTorrent(t *TorrentRecord, indexes []*IndexRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Torrents[t.InfoHash] = t
	keys := make([]string, 0, len(indexes))
	for _, idx := range indexes {
		s.db.Indexes[idx.Key] = idx
		keys = append(keys, idx.Key)
	}
	t.IndexKeys = keys
	return s.save()
}

// RemoveTorrent 删除种子及其关联索引。
func (s *Store) RemoveTorrent(infoHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.db.Torrents[infoHash]
	if !ok {
		return nil
	}
	for _, key := range t.IndexKeys {
		delete(s.db.Indexes, key)
	}
	delete(s.db.Torrents, infoHash)
	return s.save()
}

// TorrentByFolder 根据文件夹路径查找种子记录。
func (s *Store) TorrentByFolder(folder string) *TorrentRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.db.Torrents {
		if t.Folder == folder {
			return t
		}
	}
	return nil
}

// Torrent 按 info_hash 获取种子记录。
func (s *Store) Torrent(infoHash string) *TorrentRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Torrents[infoHash]
}

// AllTorrents 返回全部种子记录（按创建时间倒序）。
func (s *Store) AllTorrents() []*TorrentRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*TorrentRecord, 0, len(s.db.Torrents))
	for _, t := range s.db.Torrents {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// Index 按 key 获取索引记录。
func (s *Store) Index(key string) *IndexRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Indexes[key]
}

// IndexesForTorrent 返回某种子下的所有索引记录。
func (s *Store) IndexesForTorrent(infoHash string) []*IndexRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*IndexRecord
	for _, idx := range s.db.Indexes {
		if idx.InfoHash == infoHash {
			out = append(out, idx)
		}
	}
	return out
}

// AllIndexes 返回全部索引记录。
func (s *Store) AllIndexes() []*IndexRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*IndexRecord, 0, len(s.db.Indexes))
	for _, idx := range s.db.Indexes {
		out = append(out, idx)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BVID < out[j].BVID })
	return out
}

// ProbeGet 获取探测缓存。
func (s *Store) ProbeGet(file string) ([]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tags, ok := s.db.Probe[file]
	return tags, ok
}

// ProbeSet 写入探测缓存并持久化。
func (s *Store) ProbeSet(file string, tags []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if tags == nil {
		tags = []string{}
	}
	s.db.Probe[file] = tags
	_ = s.save()
}

// ResetProbe 清空探测缓存并持久化（重置媒体库后全量重新探测时使用）。
func (s *Store) ResetProbe() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Probe = map[string][]string{}
	_ = s.save()
}
