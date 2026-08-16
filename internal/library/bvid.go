package library

import (
	"path/filepath"
	"regexp"
	"strings"
)

// bvidRe 匹配 bilibili 视频编号 BVxxxxxxxxxx。
var bvidRe = regexp.MustCompile(`BV[0-9A-Za-z]{10}`)

// bvidFromName 从字符串中提取 bvid，不存在返回空串。
func bvidFromName(s string) string {
	if m := bvidRe.FindString(s); m != "" {
		return m
	}
	return ""
}

// bvidFromNames 从一组视频文件名和目录名中提取 bvid。
func bvidFromNames(files []string, dir string) string {
	if b := bvidFromName(filepath.Base(dir)); b != "" {
		return b
	}
	for _, f := range files {
		if b := bvidFromName(filepath.Base(f)); b != "" {
			return b
		}
	}
	return ""
}

// isBilibiliName 判断文件名/目录名是否含 bilibili 视频编号。
func isBilibiliName(s string) bool {
	return bvidFromName(s) != ""
}

// bvidTokens 从路径字符串中找出所有 bvid（用于索引），不去重。
func bvidTokens(s string) []string {
	return bvidRe.FindAllString(strings.ToUpper(s), -1)
}
