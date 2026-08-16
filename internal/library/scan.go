// Package library 负责扫描下载目录，识别 bilibili 视频文件夹（含 nfo 元数据），
// 并将每个文件夹下的视频文件组织为可制作种子的单元。
package library

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"bili-torrent/internal/nfo"
)

// Video 表示文件夹内的一个 bilibili 视频（对应一个分页）。
type Video struct {
	Key         string   `json:"key"`                  // 唯一索引 key：单页为 bvid，多页为 bvid+分页标识
	BVID        string   `json:"bvid"`                 // bilibili 唯一索引
	CID         string   `json:"cid,omitempty"`        // 分页 id（对应 B 站 cid/pid）
	Title       string   `json:"title"`                // 视频标题
	PageTitle   string   `json:"page_title,omitempty"` // 分页标题（多页视频）
	Intro       string   `json:"intro"`
	UpperName   string   `json:"upper_name"`
	Season      int      `json:"season"`
	Episode     int      `json:"episode"`
	Folder      string   `json:"folder"`       // 种子单位（文件夹）绝对路径
	File        string   `json:"file"`         // 视频文件绝对路径
	TorrentPath string   `json:"torrent_path"` // 在种子内的路径（斜杠分隔）
	Size        int64    `json:"size"`
	Poster      string   `json:"poster"` // 海报文件绝对路径（可能为空）
	Types       []string `json:"types"`  // 视频类型标签（杜比视界/HDR/4K 等）
	Source      string   `json:"source"` // 来源，如 收藏夹/动画
	URL         string   `json:"url"`    // B 站原链接
}

// BiliURL 返回 bvid 对应的 B 站原链接。
func BiliURL(bvid string) string {
	return "https://www.bilibili.com/video/" + bvid
}

// Folder 是一个种子单元：通常对应一个 bilibili 视频文件夹。
type Folder struct {
	Path   string   `json:"path"`
	Name   string   `json:"name"`
	Poster string   `json:"poster"`
	NFO    *nfo.NFO `json:"-"`
	Videos []*Video `json:"videos"`
	IsBili bool     `json:"is_bili"`
	Size   int64    `json:"size"`
	Source string   `json:"source"`
}

// Library 是扫描结果。
type Library struct {
	Folders []*Folder
	Videos  []*Video
	ByKey   map[string]*Video
}

// ProbeCache 缓存 ffprobe 探测结果，避免重复扫描。
type ProbeCache interface {
	Get(file string) ([]string, bool)
	Set(file string, tags []string)
}

// Scanner 扫描下载根目录。
type Scanner struct {
	roots      []string
	probe      bool
	hdrprobe   string // hdrprobe 可执行文件路径，空表示不启用
	cache      ProbeCache
	onProgress func(processed, total int, name string)
}

// NewScanner 创建扫描器。hdrprobe 为可执行文件路径，空表示不启用 hdrprobe 检测。
func NewScanner(roots []string, probe bool, hdrprobe string, cache ProbeCache) *Scanner {
	return &Scanner{roots: roots, probe: probe, hdrprobe: hdrprobe, cache: cache}
}

// SetProgress 设置扫描进度回调：processed 已处理单元数，total 单元总数，name 当前单元名。
func (s *Scanner) SetProgress(fn func(processed, total int, name string)) {
	s.onProgress = fn
}

type dirInfo struct {
	path    string
	videos  []string // 直接位于该目录的视频文件
	biliNFO *nfo.NFO // 直接位于该目录的 bilibili nfo（如存在）
}

type unit struct {
	dir        string
	nfo        *nfo.NFO
	hasBiliNFO bool
	source     string
	poster     string
}

func (u *unit) Name() string { return filepath.Base(u.dir) }

var videoExts = map[string]bool{
	".mp4": true, ".mkv": true, ".flv": true, ".webm": true,
	".ts": true, ".m4v": true, ".mov": true, ".avi": true,
}

func isVideoExt(low string) bool {
	return videoExts[filepath.Ext(low)]
}

func shouldSkipDir(path string) bool {
	base := filepath.Base(path)
	return strings.HasPrefix(base, ".")
}

// Scan 扫描所有根目录，返回完整的视频库。
func (s *Scanner) Scan() (*Library, error) {
	dirs, err := s.collectDirs()
	if err != nil {
		return nil, err
	}
	units := s.buildUnits(dirs)
	folders := s.buildFolders(dirs, units)

	return s.buildLibrary(folders), nil
}

// ScanIncremental 基于上次扫描结果做增量扫描：
//   - 文件未变化的单元直接复用上次结果（保留类型标签等探测数据，不再重新探测）；
//   - 新增或文件有变化的单元完整构建（类型探测命中 probe 缓存时同样跳过）；
//   - 已消失的单元从结果中移除。
func (s *Scanner) ScanIncremental(prev *Library) (*Library, error) {
	dirs, err := s.collectDirs()
	if err != nil {
		return nil, err
	}
	units := s.buildUnits(dirs)

	prevByPath := map[string]*Folder{}
	if prev != nil {
		for _, f := range prev.Folders {
			prevByPath[f.Path] = f
		}
	}

	keys := make([]string, 0, len(units))
	for p := range units {
		keys = append(keys, p)
	}
	sort.Strings(keys)

	var folders []*Folder
	for i, p := range keys {
		u := units[p]
		var f *Folder
		if prevF := prevByPath[p]; prevF != nil && !s.unitFilesChanged(u, units, prevF) {
			f = prevF // 文件未变，复用上次结果（含探测数据）
		} else {
			f = s.buildFolder(u, units)
		}
		if s.onProgress != nil {
			s.onProgress(i+1, len(keys), u.Name())
		}
		if f == nil || len(f.Videos) == 0 {
			continue
		}
		folders = append(folders, f)
	}
	sort.Slice(folders, func(i, j int) bool { return folders[i].Path < folders[j].Path })
	return s.buildLibrary(folders), nil
}

// buildLibrary 由文件夹列表组装 Library。
func (s *Scanner) buildLibrary(folders []*Folder) *Library {
	lib := &Library{Folders: folders, ByKey: map[string]*Video{}}
	for _, f := range folders {
		for _, v := range f.Videos {
			lib.Videos = append(lib.Videos, v)
			lib.ByKey[v.Key] = v
		}
	}
	sort.Slice(lib.Videos, func(i, j int) bool { return lib.Videos[i].Title < lib.Videos[j].Title })
	return lib
}

// unitFilesChanged 判断单元内的视频文件与上次结果是否一致（路径集合与文件大小）。
// 一致返回 false（可复用上次数据）；不一致返回 true（需要重新构建）。
func (s *Scanner) unitFilesChanged(u *unit, units map[string]*unit, prev *Folder) bool {
	if len(prev.Videos) == 0 {
		return true
	}
	sizeByFile := map[string]int64{}
	for _, v := range prev.Videos {
		sizeByFile[v.File] = v.Size
	}
	var videoFiles []string
	_ = filepath.WalkDir(u.dir, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == u.dir {
			return nil
		}
		if e.IsDir() {
			if other, ok := units[path]; ok && other != u {
				return fs.SkipDir // 嵌套单元不并入
			}
			return nil
		}
		if isVideoExt(strings.ToLower(e.Name())) {
			videoFiles = append(videoFiles, path)
		}
		return nil
	})
	if len(videoFiles) != len(prev.Videos) {
		return true
	}
	for _, f := range videoFiles {
		want, ok := sizeByFile[f]
		if !ok {
			return true
		}
		if info, err := os.Stat(f); err != nil || info.Size() != want {
			return true
		}
	}
	return false
}

// collectDirs 遍历所有根目录，记录每个目录直接包含的视频文件与 bilibili nfo。
func (s *Scanner) collectDirs() (map[string]*dirInfo, error) {
	dirs := map[string]*dirInfo{}
	for _, root := range s.roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // 跳过不可访问的路径
			}
			if !d.IsDir() {
				return nil
			}
			if shouldSkipDir(path) {
				return fs.SkipDir
			}
			info := &dirInfo{path: path}
			entries, err := os.ReadDir(path)
			if err != nil {
				return nil
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				name := e.Name()
				low := strings.ToLower(name)
				switch {
				case isVideoExt(low):
					info.videos = append(info.videos, filepath.Join(path, name))
				case strings.HasSuffix(low, ".nfo"):
					if nf, err := nfo.ParseFile(filepath.Join(path, name)); err == nil && nf.BVID != "" && info.biliNFO == nil {
						info.biliNFO = nf
					}
				}
			}
			sort.Strings(info.videos)
			dirs[path] = info
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return dirs, nil
}

// buildUnits 决定哪些目录是“种子单元”。
// 1. 目录内直接存在 bilibili nfo → 单元（如 单页视频文件夹、多页视频顶层文件夹）。
// 2. 目录内有视频文件、无 bilibili nfo、且不在其它单元内 → 若文件名含 bvid 也视为单元。
func (s *Scanner) buildUnits(dirs map[string]*dirInfo) map[string]*unit {
	units := map[string]*unit{}
	order := make([]string, 0, len(dirs))
	for p := range dirs {
		order = append(order, p)
	}
	sort.Strings(order) // 父目录在前

	for _, p := range order {
		if di := dirs[p]; di.biliNFO != nil {
			units[p] = &unit{dir: p, nfo: di.biliNFO, hasBiliNFO: true, source: s.computeSource(p), poster: findPoster(p, filepath.Base(p))}
		}
	}
	for _, p := range order {
		di := dirs[p]
		if len(di.videos) == 0 {
			continue
		}
		if _, ok := units[p]; ok {
			continue
		}
		if s.insideUnit(units, p) {
			continue
		}
		bvid := bvidFromNames(di.videos, p)
		if bvid == "" {
			continue // 无 bilibili 特征，忽略
		}
		units[p] = &unit{dir: p, nfo: &nfo.NFO{BVID: bvid, Title: filepath.Base(p)}, source: s.computeSource(p), poster: findPoster(p, filepath.Base(p))}
	}
	return units
}

func (s *Scanner) insideUnit(units map[string]*unit, p string) bool {
	parent := filepath.Dir(p)
	for parent != p && parent != "." && len(parent) > 3 {
		if _, ok := units[parent]; ok {
			return true
		}
		parent = filepath.Dir(parent)
	}
	return false
}

// buildFolders 为每个单元构造 Folder，收集其子树内的视频文件。
// 按路径排序逐个处理，并通过 onProgress 回调报告进度（单元为单位）。
func (s *Scanner) buildFolders(dirs map[string]*dirInfo, units map[string]*unit) []*Folder {
	keys := make([]string, 0, len(units))
	for p := range units {
		keys = append(keys, p)
	}
	sort.Strings(keys)

	var folders []*Folder
	for i, p := range keys {
		u := units[p]
		f := s.buildFolder(u, units)
		if s.onProgress != nil {
			s.onProgress(i+1, len(keys), u.Name())
		}
		if f == nil || len(f.Videos) == 0 {
			continue
		}
		folders = append(folders, f)
	}
	sort.Slice(folders, func(i, j int) bool { return folders[i].Path < folders[j].Path })
	return folders
}

func (s *Scanner) buildFolder(u *unit, units map[string]*unit) *Folder {
	name := filepath.Base(u.dir)
	f := &Folder{
		Path:   u.dir,
		Name:   name,
		NFO:    u.nfo,
		IsBili: u.nfo != nil && u.nfo.BVID != "",
		Poster: u.poster,
		Source: u.source,
	}

	var allFiles []string // 单元内所有文件（用于计算种子大小）
	var videoFiles []string
	_ = filepath.WalkDir(u.dir, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == u.dir {
			return nil
		}
		if e.IsDir() {
			if other, ok := units[path]; ok && other != u {
				return fs.SkipDir // 嵌套单元不并入
			}
			return nil
		}
		allFiles = append(allFiles, path)
		if isVideoExt(strings.ToLower(e.Name())) {
			videoFiles = append(videoFiles, path)
		}
		return nil
	})
	sort.Strings(allFiles)
	sort.Strings(videoFiles)

	for _, file := range allFiles {
		if info, err := os.Stat(file); err == nil {
			f.Size += info.Size()
		}
	}

	single := len(videoFiles) == 1
	for i, file := range videoFiles {
		v, err := s.buildVideo(u, file, single, i)
		if err != nil {
			continue
		}
		f.Videos = append(f.Videos, v)
	}

	// 批量探测视频类型（hdrprobe + 可选 ffprobe 音频），一次调用处理文件夹内全部视频
	if len(f.Videos) > 0 {
		files := make([]string, len(f.Videos))
		for i, v := range f.Videos {
			files[i] = v.File
		}
		tagsByFile := s.detectTypesBatch(files)
		for _, v := range f.Videos {
			v.Types = tagsByFile[v.File]
		}
	}
	return f
}

func (s *Scanner) buildVideo(u *unit, file string, single bool, idx int) (*Video, error) {
	rel, err := filepath.Rel(u.dir, file)
	if err != nil {
		return nil, err
	}
	torrentPath := filepath.ToSlash(rel)

	info, err := os.Stat(file)
	if err != nil {
		return nil, err
	}

	bvid := u.nfo.BVID
	title := u.nfo.Title
	if title == "" {
		title = u.Name()
	}

	v := &Video{
		BVID:        bvid,
		Title:       title,
		Intro:       u.nfo.Intro(),
		UpperName:   u.nfo.UpperName(),
		Folder:      u.dir,
		File:        file,
		TorrentPath: torrentPath,
		Size:        info.Size(),
		Source:      u.source,
		URL:         BiliURL(bvid),
	}

	// 分页信息：优先读取与视频同名的 nfo（episodedetails）
	base := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	if nf, err := nfo.ParseFile(filepath.Join(filepath.Dir(file), base+".nfo")); err == nil {
		v.Season = nf.Season
		v.Episode = nf.Episode
		if nf.Title != "" {
			v.PageTitle = nf.Title
		}
		if nf.Episode > 0 {
			v.CID = strconv.Itoa(nf.Episode)
		}
	}

	// 生成唯一索引 key
	if single {
		v.Key = bvid
	} else {
		page := v.CID
		if page == "" && v.Episode > 0 {
			page = strconv.Itoa(v.Episode)
		}
		if page == "" {
			page = strconv.Itoa(idx + 1)
		}
		v.Key = bvid + "_" + page
	}

	// 海报：优先取视频同名封面，其次取文件夹海报
	if p := findVideoPoster(file); p != "" {
		v.Poster = p
	} else {
		v.Poster = u.poster
	}

	// 视频类型标签由 buildFolder 中的批量探测（hdrprobe/ffprobe）统一填充
	return v, nil
}

func (s *Scanner) computeSource(dir string) string {
	for _, root := range s.roots {
		rel, err := filepath.Rel(root, dir)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) >= 2 && isSourceDir(parts[0]) {
			return parts[0] + "/" + parts[1]
		}
		if len(parts) >= 1 && parts[0] != "" {
			return parts[0]
		}
		return ""
	}
	return ""
}

func isSourceDir(s string) bool {
	switch s {
	case "收藏夹", "合集", "投稿", "favorites", "collections", "submissions", "watch_later":
		return true
	}
	return false
}

// findPoster 在文件夹中寻找海报文件。
func findPoster(dir, name string) string {
	candidates := []string{
		"poster.jpg", "poster.png", "fanart.jpg", "fanart.png", "folder.jpg",
		name + "-poster.jpg", name + "-poster.png", name + "-fanart.jpg", name + "-fanart.png",
	}
	for _, c := range candidates {
		p := filepath.Join(dir, c)
		if fileExists(p) {
			return p
		}
	}
	// 兜底：扫描目录下任意 *poster* / *fanart* / *-thumb* 图片
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		low := strings.ToLower(e.Name())
		if (strings.Contains(low, "poster") || strings.Contains(low, "fanart") || strings.Contains(low, "thumb")) &&
			(strings.HasSuffix(low, ".jpg") || strings.HasSuffix(low, ".jpeg") || strings.HasSuffix(low, ".png") || strings.HasSuffix(low, ".webp")) {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// findVideoPoster 寻找与视频文件同名的封面（{base}-poster.jpg / {base}-thumb.jpg）。
func findVideoPoster(file string) string {
	base := filepath.Join(filepath.Dir(file), strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)))
	for _, suf := range []string{"-poster.jpg", "-poster.png", "-thumb.jpg", "-thumb.png", "-fanart.jpg"} {
		if p := base + suf; fileExists(p) {
			return p
		}
	}
	return ""
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
