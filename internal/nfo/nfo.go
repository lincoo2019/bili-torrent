// Package nfo 负责解析 bili-sync 等工具下载时生成的 Kodi 风格 nfo 元数据文件，
// 从中提取 bilibili 视频信息（bvid、标题、简介、UP 主、分集等）。
package nfo

import (
	"encoding/xml"
	"os"
	"regexp"
	"strings"
)

// Actor 表示 nfo 中的演员（UP 主）信息。
type Actor struct {
	Name  string `xml:"name"`
	Role  string `xml:"role"`
	Thumb string `xml:"thumb"`
}

type uniqueID struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

// raw 是 nfo 的通用解析结构，覆盖 movie / tvshow / episodedetails / person 等。
type raw struct {
	XMLName   xml.Name
	Title     string     `xml:"title"`
	Plot      string     `xml:"plot"`
	UniqueID  []uniqueID `xml:"uniqueid"`
	Actors    []Actor    `xml:"actor"`
	Year      string     `xml:"year"`
	Premiered string     `xml:"premiered"`
	Genres    []string   `xml:"genre"`
	Season    string     `xml:"season"`
	Episode   string     `xml:"episode"`
}

// NFO 是解析后的元数据。
type NFO struct {
	Kind      string // movie / tvshow / episodedetails / person / ...
	Title     string
	Plot      string
	BVID      string // <uniqueid type="bilibili"> 中的值
	Season    int
	Episode   int
	Actors    []Actor
	Year      string
	Premiered string
	Genres    []string
}

var bvidInURLRe = regexp.MustCompile(`bilibili\.com/video/(BV[0-9A-Za-z]{10})`)

// Parse 解析 nfo 内容。
func Parse(data []byte) (*NFO, error) {
	var r raw
	if err := xml.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	n := &NFO{
		Kind:      r.XMLName.Local,
		Title:     strings.TrimSpace(r.Title),
		Plot:      strings.TrimSpace(r.Plot),
		Season:    parseInt(r.Season),
		Episode:   parseInt(r.Episode),
		Actors:    r.Actors,
		Year:      r.Year,
		Premiered: r.Premiered,
		Genres:    r.Genres,
	}
	for _, u := range r.UniqueID {
		if strings.EqualFold(strings.TrimSpace(u.Type), "bilibili") {
			n.BVID = strings.TrimSpace(u.Value)
			break
		}
	}
	// 兜底：从简介中的 B 站原链接提取 bvid
	if n.BVID == "" {
		if m := bvidInURLRe.FindStringSubmatch(n.Plot); m != nil {
			n.BVID = m[1]
		}
	}
	return n, nil
}

// ParseFile 解析一个 nfo 文件。
func ParseFile(path string) (*NFO, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// IsBilibili 判断一个 nfo 文件是否携带 bilibili 唯一索引（bvid）。
func IsBilibili(path string) bool {
	n, err := ParseFile(path)
	return err == nil && n.BVID != ""
}

// BVIDFromFile 从 nfo 文件提取 bvid，不存在则返回空串。
func BVIDFromFile(path string) string {
	n, err := ParseFile(path)
	if err != nil {
		return ""
	}
	return n.BVID
}

func parseInt(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// UpperName 返回第一个演员（UP 主）的名字，优先使用 role。
func (n *NFO) UpperName() string {
	if n == nil || len(n.Actors) == 0 {
		return ""
	}
	if r := strings.TrimSpace(n.Actors[0].Role); r != "" {
		return r
	}
	return strings.TrimSpace(n.Actors[0].Name)
}

// Intro 返回视频简介。
func (n *NFO) Intro() string {
	if n == nil {
		return ""
	}
	return n.Plot
}
