package library

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
)

// 视频类型检测：
//   - hdrprobe：解析码流，识别杜比视界 / HDR10 / HDR10+ / HLG / HDR Vivid、编码、分辨率、位深；
//   - ffprobe（可选，probe_videos=true）：补充音频类型（杜比全景声 / FLAC / DTS 等）。
// 不再依赖文件名猜测。

// hdrProbeReport 是 hdrprobe --format ndjson 输出的 Report 结构（只解析所需字段）。
type hdrProbeReport struct {
	File        string `json:"file"`
	VideoTracks []struct {
		Codec    string `json:"codec"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
		BitDepth int    `json:"bit_depth"`
		HDR      *struct {
			Format string `json:"format"`
			Base   string `json:"base"`
		} `json:"hdr"`
		DolbyVision *struct {
			Profile string `json:"profile"`
		} `json:"dolby_vision"`
		HDR10Plus *struct{} `json:"hdr10plus"`
		HDRVivid  *struct{} `json:"hdr_vivid"`
		SlHdr     *struct{} `json:"sl_hdr"`
	} `json:"video_tracks"`
}

// tagsFromHDRReport 将 hdrprobe 报告转换为视频类型标签（所有视频轨道的并集）。
func tagsFromHDRReport(rep *hdrProbeReport) []string {
	var tags []string
	add := func(t string) {
		if t != "" {
			for _, x := range tags {
				if x == t {
					return
				}
			}
			tags = append(tags, t)
		}
	}
	for _, t := range rep.VideoTracks {
		if t.DolbyVision != nil {
			add("杜比视界")
			if p := strings.TrimSpace(t.DolbyVision.Profile); p != "" {
				add("DV " + p)
			}
		}
		if t.HDR10Plus != nil {
			add("HDR10+")
		}
		if t.HDRVivid != nil {
			add("HDR Vivid")
		}
		if t.SlHdr != nil {
			add("SL-HDR")
		}
		if t.HDR != nil {
			switch t.HDR.Base {
			case "HDR10":
				add("HDR10")
			case "HLG":
				add("HLG")
			case "SDR":
				add("SDR")
			}
		}
		switch t.Codec {
		case "HEVC", "AVC", "AV1", "VP9", "ProRes":
			add(t.Codec)
		}
		if t.Width > 0 || t.Height > 0 {
			switch {
			case t.Width >= 3840 || t.Height >= 2160:
				add("4K")
			case t.Width >= 1920 || t.Height >= 1080:
				add("1080P")
			case t.Width >= 1280 || t.Height >= 720:
				add("720P")
			}
		}
		switch t.BitDepth {
		case 8:
			add("8bit")
		case 10:
			add("10bit")
		case 12:
			add("12bit")
		}
	}
	return tags
}

// runHDRProbe 对一组文件批量运行 hdrprobe（--format ndjson，一次调用处理全部文件），
// 返回 文件路径 → 视频类型标签。hdrprobe 缺失或全部失败时返回空 map。
func runHDRProbe(bin string, files []string) map[string][]string {
	out := map[string][]string{}
	if len(files) == 0 {
		return out
	}
	args := append([]string{"--format", "ndjson"}, files...)
	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 2 {
			// 退出码 2 表示部分文件失败，stdout 仍包含成功者的报告
			return out
		}
	}
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	for {
		var rep hdrProbeReport
		if err := dec.Decode(&rep); err != nil {
			break
		}
		if rep.File == "" {
			continue
		}
		out[rep.File] = tagsFromHDRReport(&rep)
	}
	return out
}

// ffprobe 音频探测结果结构。
type ffprobeStream struct {
	CodecName string `json:"codec_name"`
	Profile   string `json:"profile"`
	CodecType string `json:"codec_type"`
}

type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
}

// ffprobeAudioTags 调用 ffprobe 提取音频类型标签（杜比全景声 / 杜比音效 / TrueHD / FLAC / DTS / AAC / Opus）。
// ffprobe 不可用或探测失败时返回 nil。
func ffprobeAudioTags(ffprobePath, file string) []string {
	cmd := exec.Command(ffprobePath, "-v", "error",
		"-show_entries", "stream=codec_name,profile,codec_type",
		"-of", "json", file)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var parsed ffprobeOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil
	}
	var tags []string
	for _, st := range parsed.Streams {
		if st.CodecType != "audio" {
			continue
		}
		switch st.CodecName {
		case "eac3":
			if strings.Contains(strings.ToUpper(st.Profile), "JOC") {
				tags = append(tags, "杜比全景声")
			} else {
				tags = append(tags, "杜比音效")
			}
		case "truehd":
			tags = append(tags, "TrueHD")
		case "flac":
			tags = append(tags, "FLAC")
		case "dts":
			tags = append(tags, "DTS")
		case "aac":
			tags = append(tags, "AAC")
		case "opus":
			tags = append(tags, "Opus")
		}
	}
	return tags
}

// mergeTags 合并两组标签并去重，保持顺序。
func mergeTags(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{a, b} {
		for _, t := range list {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// findFFprobe 在 PATH 中查找 ffprobe 可执行文件。
func findFFprobe() string {
	path, err := exec.LookPath("ffprobe")
	if err != nil {
		return ""
	}
	return path
}

// detectTypesBatch 批量计算一组视频文件的类型标签。
// 先由 hdrprobe 探测视频/HDR/杜比视界等（结果缓存），再按配置追加 ffprobe 音频标签。
func (s *Scanner) detectTypesBatch(files []string) map[string][]string {
	result := map[string][]string{}
	for _, f := range files {
		result[f] = nil
	}

	if s.hdrprobe != "" {
		var uncached []string
		for _, f := range files {
			if _, ok := s.cache.Get(f); !ok {
				uncached = append(uncached, f)
			}
		}
		if len(uncached) > 0 {
			got := runHDRProbe(s.hdrprobe, uncached)
			for f, tags := range got {
				s.cache.Set(f, tags)
			}
		}
		for _, f := range files {
			if tags, ok := s.cache.Get(f); ok {
				result[f] = append(result[f], tags...)
			}
		}
	}

	if s.probe {
		if fp := findFFprobe(); fp != "" {
			for _, f := range files {
				audio := ffprobeAudioTags(fp, f)
				if len(audio) == 0 {
					continue
				}
				merged := mergeTags(result[f], audio)
				result[f] = merged
				s.cache.Set(f, merged)
			}
		}
	}
	return result
}
