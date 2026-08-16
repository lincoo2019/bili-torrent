package torrent

import (
	"crypto/sha1"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
)

func TestCreateTorrent(t *testing.T) {
	dir := t.TempDir()
	files := []string{"a.txt", "sub/b.txt", "sub/c.mkv"}
	for _, f := range files {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		data := []byte(strings.Repeat(f, 64))
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 再放一个应被排除的文件
	if err := os.WriteFile(filepath.Join(dir, "skip.part"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := t.TempDir()
	res, err := Create(Options{
		Path:      dir,
		Name:      "my-video",
		Trackers:  []string{"http://tracker.example.com/announce"},
		Comment:   "test",
		Private:   true,
		OutputDir: out,
		Exclude:   []string{"*.part"},
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if res.InfoHash == "" {
		t.Fatal("empty infohash")
	}
	if len(res.Files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(res.Files))
	}

	// 解析生成的 .torrent 文件，校验 info 结构与 infohash 一致性
	data, err := os.ReadFile(res.TorrentFile)
	if err != nil {
		t.Fatalf("read torrent: %v", err)
	}
	var mi metainfo.MetaInfo
	if err := bencode.Unmarshal(data, &mi); err != nil {
		t.Fatalf("unmarshal torrent: %v", err)
	}
	if mi.Announce != "http://tracker.example.com/announce" {
		t.Errorf("unexpected announce: %s", mi.Announce)
	}
	var info metainfo.Info
	if err := bencode.Unmarshal(mi.InfoBytes, &info); err != nil {
		t.Fatalf("unmarshal info: %v", err)
	}
	if info.Name != "my-video" {
		t.Errorf("unexpected name: %s", info.Name)
	}
	if info.Private == nil || !*info.Private {
		t.Error("private flag not set")
	}
	if len(info.Files) != 3 {
		t.Fatalf("expected 3 files in info, got %d", len(info.Files))
	}

	// 校验 pieces 哈希与文件内容一致（文件按相对路径排序后拼接，再按 piece 切分计算 SHA1）
	// 注意：测试写入的文件内容为 strings.Repeat(相对路径, 64)
	expected := sha1.Sum([]byte(strings.Repeat("a.txt", 64) + strings.Repeat("sub/b.txt", 64) + strings.Repeat("sub/c.mkv", 64)))
	if len(info.Pieces) != 20 {
		t.Fatalf("expected 1 piece (20 bytes), got %d", len(info.Pieces))
	}
	if string(info.Pieces) != string(expected[:]) {
		t.Errorf("piece hash mismatch:\n got %x\nwant %x", info.Pieces, expected)
	}

	// 校验 info hash 与返回的 infohash 一致
	if mi.HashInfoBytes().String() != res.InfoHash {
		t.Errorf("infohash mismatch: %s vs %s", mi.HashInfoBytes().String(), res.InfoHash)
	}
}
