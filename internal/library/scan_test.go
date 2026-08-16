package library

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// 使用 ../testdata/downloads 作为测试数据（bili-sync 风格下载目录）。
func testRoot() string {
	return filepath.Join("..", "..", "testdata", "downloads")
}

type noopCache struct{}

func (noopCache) Get(string) ([]string, bool) { return nil, false }
func (noopCache) Set(string, []string)        {}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestScan(t *testing.T) {
	// 测试数据为模拟文件（无真实码流），hdrprobe 不启用，类型标签应为空
	s := NewScanner([]string{testRoot()}, false, "", noopCache{})
	lib, err := s.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Folders) != 2 {
		t.Fatalf("expected 2 folders, got %d", len(lib.Folders))
	}
	if len(lib.Videos) != 3 {
		t.Fatalf("expected 3 videos, got %d", len(lib.Videos))
	}

	var multi, single *Folder
	for _, f := range lib.Folders {
		switch f.Name {
		case "异世界日常":
			multi = f
		case "单页测试视频":
			single = f
		}
	}

	// 多页视频：一个文件夹、两个视频、两个索引
	if multi == nil {
		t.Fatal("multi folder not found")
	}
	if len(multi.Videos) != 2 {
		t.Fatalf("multi folder should have 2 videos, got %d", len(multi.Videos))
	}
	if multi.Videos[0].Key == multi.Videos[1].Key {
		t.Error("two videos must have different index keys")
	}
	if multi.Videos[0].BVID != "BV1a2b3c4d5f" || multi.Videos[1].BVID != "BV1a2b3c4d5f" {
		t.Errorf("unexpected bvids: %s, %s", multi.Videos[0].BVID, multi.Videos[1].BVID)
	}
	if multi.Videos[0].Episode != 1 || multi.Videos[1].Episode != 2 {
		t.Errorf("unexpected episodes: %d, %d", multi.Videos[0].Episode, multi.Videos[1].Episode)
	}
	if multi.Poster == "" {
		t.Error("multi folder should have poster")
	}
	// 无 hdrprobe 时不进行类型检测
	if len(multi.Videos[0].Types) != 0 {
		t.Errorf("types should be empty without hdrprobe, got %v", multi.Videos[0].Types)
	}

	// 单页视频：一个视频、索引 key = bvid
	if single == nil {
		t.Fatal("single folder not found")
	}
	if len(single.Videos) != 1 {
		t.Fatalf("single folder should have 1 video, got %d", len(single.Videos))
	}
	if single.Videos[0].Key != "BV1f2g3h4j5k" {
		t.Errorf("single video key should be bvid, got %s", single.Videos[0].Key)
	}
	if single.Videos[0].URL != "https://www.bilibili.com/video/BV1f2g3h4j5k" {
		t.Errorf("unexpected url: %s", single.Videos[0].URL)
	}
}

// TestTagsFromHDRReport 验证 hdrprobe 报告 → 类型标签 的映射。
func TestTagsFromHDRReport(t *testing.T) {
	cases := []struct {
		name string
		json string
		want []string
	}{
		{
			name: "杜比视界 HDR10 4K HEVC 10bit",
			json: `{"file":"a.mkv","video_tracks":[{"codec":"HEVC","width":3840,"height":2160,"bit_depth":10,
				"hdr":{"format":"Dolby Vision / HDR10","base":"HDR10"},
				"dolby_vision":{"profile":"8.1"}}]}`,
			want: []string{"杜比视界", "DV 8.1", "HDR10", "HEVC", "4K", "10bit"},
		},
		{
			name: "HDR10+ 与 HDR10 并存",
			json: `{"file":"b.mkv","video_tracks":[{"codec":"HEVC","width":1920,"height":1080,"bit_depth":10,
				"hdr":{"format":"HDR10+ / HDR10","base":"HDR10"},"hdr10plus":{"profile":"B"}}]}`,
			want: []string{"HDR10+", "HDR10", "HEVC", "1080P", "10bit"},
		},
		{
			name: "普通 SDR AVC",
			json: `{"file":"c.mp4","video_tracks":[{"codec":"AVC","width":1280,"height":720,"bit_depth":8,
				"hdr":{"format":"SDR","base":"SDR"}}]}`,
			want: []string{"SDR", "AVC", "720P", "8bit"},
		},
		{
			name: "HLG 广播",
			json: `{"file":"d.ts","video_tracks":[{"codec":"HEVC","width":3840,"height":2160,"bit_depth":10,
				"hdr":{"format":"HLG","base":"HLG"}}]}`,
			want: []string{"HLG", "HEVC", "4K", "10bit"},
		},
	}
	for _, c := range cases {
		var rep hdrProbeReport
		if err := json.Unmarshal([]byte(c.json), &rep); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		got := tagsFromHDRReport(&rep)
		if !equalTags(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func equalTags(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestScanIncremental 验证增量扫描：文件未变化时复用上次结果（不重新构建/探测）。
func TestScanIncremental(t *testing.T) {
	s := NewScanner([]string{testRoot()}, false, "", noopCache{})
	lib1, err := s.Scan()
	if err != nil {
		t.Fatal(err)
	}

	// 无变化时再次扫描：应复用上次的 Folder（指针一致），数量与内容不变
	lib2, err := s.ScanIncremental(lib1)
	if err != nil {
		t.Fatal(err)
	}
	if len(lib2.Folders) != len(lib1.Folders) {
		t.Fatalf("folder count changed: %d -> %d", len(lib1.Folders), len(lib2.Folders))
	}
	if len(lib2.Videos) != len(lib1.Videos) {
		t.Fatalf("video count changed: %d -> %d", len(lib1.Videos), len(lib2.Videos))
	}
	for i := range lib2.Folders {
		if lib2.Folders[i] != lib1.Folders[i] {
			t.Errorf("folder %q should be reused, got a new instance", lib1.Folders[i].Path)
		}
	}

	// prev 为 nil 时退化为全量扫描
	lib3, err := s.ScanIncremental(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(lib3.Folders) != len(lib1.Folders) {
		t.Fatalf("full scan via incremental with nil prev: %d != %d", len(lib3.Folders), len(lib1.Folders))
	}
}
