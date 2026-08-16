// Package torrent 使用 github.com/autobrr/go-torrent 将目录制作成 .torrent 文件。
package torrent

import (
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
)

// Options 是制作种子的参数。
type Options struct {
	Path           string   // 需要制作种子的文件或目录
	Name           string   // 种子名（默认取目录名）
	Trackers       []string // announce 地址
	Comment        string
	Private        bool
	PieceLengthExp int      // 2^n 字节，0 表示自动
	Exclude        []string // 排除的 glob 模式（匹配种子内相对路径或文件名）
	OutputDir      string   // .torrent 输出目录
	Progress       func(done, total int)
}

// FileEntry 是种子内的一个文件。
type FileEntry struct {
	Path string `json:"path"` // 种子内路径（斜杠分隔）
	Size int64  `json:"size"`
}

// Result 是制作完成的结果。
type Result struct {
	InfoHash    string
	Name        string
	TorrentFile string
	Files       []FileEntry
	Size        int64
}

type fileEntry struct {
	abs  string
	rel  string
	size int64
}

// pieceSizeRanges 是自动 piece length 的计算区间（与常见 tracker 推荐一致）。
var pieceSizeRanges = []struct {
	maxSize int64
	exp     uint
}{
	{512 << 20, 17}, // < 512 MiB → 128 KiB
	{1 << 30, 18},   // < 1 GiB → 256 KiB
	{2 << 30, 19},   // < 2 GiB → 512 KiB
	{4 << 30, 20},   // < 4 GiB → 1 MiB
	{8 << 30, 21},   // < 8 GiB → 2 MiB
	{16 << 30, 22},  // < 16 GiB → 4 MiB
	{32 << 30, 23},  // < 32 GiB → 8 MiB
}

// Create 将 opts.Path 制作成种子并写入 OutputDir。
func Create(opts Options) (*Result, error) {
	info, err := os.Stat(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("无法访问 %q: %w", opts.Path, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("目前仅支持对目录制作种子: %q", opts.Path)
	}

	root := filepath.Clean(opts.Path)
	name := opts.Name
	if name == "" {
		name = filepath.Base(root)
	}
	name = filepath.Base(name) // 种子名不允许包含路径

	entries, totalSize, err := collectFiles(root, opts.Exclude)
	if err != nil {
		return nil, err
	}
	if totalSize == 0 {
		return nil, fmt.Errorf("目录 %q 中没有可制作种子的文件", root)
	}

	pieceLen := calcPieceLength(totalSize, opts.PieceLengthExp)
	pieces, err := hashPieces(entries, pieceLen, opts.Progress)
	if err != nil {
		return nil, fmt.Errorf("计算校验和失败: %w", err)
	}

	infoDict := &metainfo.Info{
		Name:        name,
		PieceLength: pieceLen,
		Private:     &opts.Private,
		Pieces:      pieces,
	}
	infoDict.Files = make([]metainfo.FileInfo, len(entries))
	fileEntries := make([]FileEntry, len(entries))
	for i, e := range entries {
		parts := strings.Split(filepath.ToSlash(e.rel), "/")
		infoDict.Files[i] = metainfo.FileInfo{Path: parts, Length: e.size}
		fileEntries[i] = FileEntry{Path: filepath.ToSlash(e.rel), Size: e.size}
	}

	infoBytes, err := bencode.Marshal(infoDict)
	if err != nil {
		return nil, fmt.Errorf("编码 info 失败: %w", err)
	}

	mi := &metainfo.MetaInfo{
		Comment:      opts.Comment,
		CreatedBy:    "bili-torrent (https://github.com/bili-torrent/bili-torrent)",
		CreationDate: time.Now().Unix(),
		InfoBytes:    infoBytes,
	}
	if len(opts.Trackers) > 0 {
		mi.Announce = opts.Trackers[0]
		if len(opts.Trackers) > 1 {
			list := make([][]string, len(opts.Trackers))
			for i, t := range opts.Trackers {
				list[i] = []string{t}
			}
			mi.AnnounceList = list
		}
	}

	data, err := bencode.Marshal(mi)
	if err != nil {
		return nil, fmt.Errorf("编码种子失败: %w", err)
	}

	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建输出目录失败: %w", err)
	}
	out := filepath.Join(opts.OutputDir, name+".torrent")
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return nil, fmt.Errorf("写入种子文件失败: %w", err)
	}

	infoHash := mi.HashInfoBytes()
	return &Result{
		InfoHash:    infoHash.String(),
		Name:        name,
		TorrentFile: out,
		Files:       fileEntries,
		Size:        totalSize,
	}, nil
}

// collectFiles 收集目录下的所有文件，计算相对路径与总大小，并应用排除规则。
func collectFiles(root string, excludes []string) ([]fileEntry, int64, error) {
	var entries []fileEntry
	var total int64
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if excluded(relSlash, filepath.Base(rel), excludes) {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		entries = append(entries, fileEntry{abs: path, rel: relSlash, size: fi.Size()})
		total += fi.Size()
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	// 排序保证 infohash 确定性
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	return entries, total, nil
}

// excluded 判断相对路径或文件名是否命中排除规则。
func excluded(rel, base string, excludes []string) bool {
	for _, p := range excludes {
		if p == "" {
			continue
		}
		if ok, _ := filepath.Match(p, base); ok {
			return true
		}
		if ok, _ := filepath.Match(p, rel); ok {
			return true
		}
	}
	return false
}

// calcPieceLength 计算 piece length（2^n 字节）。
func calcPieceLength(totalSize int64, exp int) int64 {
	if exp > 0 {
		return int64(1) << exp
	}
	for _, r := range pieceSizeRanges {
		if totalSize <= r.maxSize {
			return int64(1) << r.exp
		}
	}
	return int64(1) << 24 // 默认最大 16 MiB
}

// hashPieces 按 piece 计算 SHA1 校验和。
func hashPieces(entries []fileEntry, pieceLen int64, progress func(done, total int)) ([]byte, error) {
	total := int64(0)
	for _, e := range entries {
		total += e.size
	}
	totalPieces := (total + pieceLen - 1) / pieceLen

	h := &pieceHasher{
		pieceLen: int(pieceLen),
		buf:      make([]byte, pieceLen),
		total:    int(totalPieces),
		progress: progress,
	}
	chunk := make([]byte, 1<<20)
	for _, e := range entries {
		f, err := os.Open(e.abs)
		if err != nil {
			return nil, err
		}
		for {
			n, rerr := f.Read(chunk)
			if n > 0 {
				h.write(chunk[:n])
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				f.Close()
				return nil, rerr
			}
		}
		f.Close()
	}
	h.finish()

	if progress != nil {
		progress(int(totalPieces), int(totalPieces))
	}
	return h.pieces, nil
}

type pieceHasher struct {
	pieceLen int
	buf      []byte
	pos      int
	pieces   []byte
	done     int
	total    int
	progress func(done, total int)
}

func (h *pieceHasher) report() {
	if h.progress != nil && h.done > 0 && h.done%2 == 0 {
		h.progress(h.done, h.total)
	}
}

func (h *pieceHasher) write(data []byte) {
	for len(data) > 0 {
		n := copy(h.buf[h.pos:], data)
		h.pos += n
		data = data[n:]
		if h.pos == h.pieceLen {
			sum := sha1.Sum(h.buf)
			h.pieces = append(h.pieces, sum[:]...)
			h.pos = 0
			h.done++
			h.report()
		}
	}
}

func (h *pieceHasher) finish() {
	if h.pos > 0 {
		sum := sha1.Sum(h.buf[:h.pos])
		h.pieces = append(h.pieces, sum[:]...)
		h.done++
		h.report()
	}
}
