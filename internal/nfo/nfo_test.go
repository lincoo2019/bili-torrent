package nfo

import "testing"

func TestParseMovie(t *testing.T) {
	content := `<?xml version="1.0" encoding="utf-8" standalone="yes"?>
<movie>
    <plot><![CDATA[原始视频：<a href="https://www.bilibili.com/video/BV1f2g3h4i5j6/">BV1f2g3h4i5j6</a><br/><br/>简介内容]]></plot>
    <outline/>
    <title>单页测试视频</title>
    <actor>
        <name>1002</name>
        <role>另一个UP</role>
        <thumb>https://i1.hdslb.com/bfs/face/2.jpg</thumb>
    </actor>
    <year>2023</year>
    <genre>科技</genre>
    <uniqueid type="bilibili">BV1f2g3h4i5j6</uniqueid>
    <premiered>2023-05-06</premiered>
</movie>`
	n, err := Parse([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if n.Kind != "movie" {
		t.Errorf("kind = %s", n.Kind)
	}
	if n.BVID != "BV1f2g3h4i5j6" {
		t.Errorf("bvid = %s", n.BVID)
	}
	if n.Title != "单页测试视频" {
		t.Errorf("title = %s", n.Title)
	}
	if !contains(n.Plot, "原始视频") {
		t.Errorf("plot = %s", n.Plot)
	}
	if n.UpperName() != "另一个UP" {
		t.Errorf("upper = %s", n.UpperName())
	}
}

func TestParseTVShow(t *testing.T) {
	content := `<tvshow>
    <plot><![CDATA[原始视频：<a href="https://www.bilibili.com/video/BV1a2b3c4d5e6/">BV1a2b3c4d5e6</a><br/><br/>多页简介]]></plot>
    <outline/>
    <title>异世界日常</title>
    <actor><name>1001</name><role>测试UP主</role><thumb>f</thumb></actor>
    <year>2024</year>
    <uniqueid type="bilibili">BV1a2b3c4d5e6</uniqueid>
    <premiered>2024-01-01</premiered>
</tvshow>`
	n, err := Parse([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if n.Kind != "tvshow" || n.BVID != "BV1a2b3c4d5e6" || n.Title != "异世界日常" {
		t.Errorf("unexpected: %+v", n)
	}
}

func TestParseEpisode(t *testing.T) {
	content := `<episodedetails>
    <plot/>
    <outline/>
    <title>第一话 相遇</title>
    <season>1</season>
    <episode>2</episode>
</episodedetails>`
	n, err := Parse([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if n.Kind != "episodedetails" {
		t.Errorf("kind = %s", n.Kind)
	}
	if n.Episode != 2 || n.Season != 1 {
		t.Errorf("episode=%d season=%d", n.Episode, n.Season)
	}
	if n.BVID != "" {
		t.Errorf("episode should have no bvid, got %s", n.BVID)
	}
}

// TestBVIDFromPlot 验证无 uniqueid 时从简介链接兜底提取 bvid。
func TestBVIDFromPlot(t *testing.T) {
	content := `<movie>
    <plot><![CDATA[原始视频：<a href="https://www.bilibili.com/video/BV1x2y3z4a5b/">BV1x2y3z4a5b</a><br/><br/>介绍]]></plot>
    <title>标题</title>
</movie>`
	n, err := Parse([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if n.BVID != "BV1x2y3z4a5b" {
		t.Errorf("bvid = %s", n.BVID)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
