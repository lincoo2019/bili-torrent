package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 是 bili-torrent 的运行时配置。
type Config struct {
	// Listen 是 HTTP 服务监听地址，例如 ":8080"。
	Listen string `yaml:"listen"`
	// DataDir 用于存放持久化数据（种子记录、索引、ffprobe 探测缓存）。
	DataDir string `yaml:"data_dir"`
	// Roots 是需要扫描的下载根目录，可配置多个。目录下按 bili-sync 等方式下载的
	// bilibili 视频（含 nfo 元数据）会被识别并展示。
	Roots []string `yaml:"roots"`
	// TorrentDir 是生成的 .torrent 文件的输出目录。
	TorrentDir string `yaml:"torrent_dir"`
	// Trackers 是制作种子时写入的 announce 地址，可配置多个。
	Trackers []string `yaml:"trackers"`
	// Private 标记种子是否私有（写入 info.private=1）。
	Private bool `yaml:"private"`
	// Comment 是写入种子的备注。
	Comment string `yaml:"comment"`
	// PieceLength 是 piece length 的指数（2^n 字节），0 表示自动计算。
	PieceLength int `yaml:"piece_length"`
	// Exclude 是制作种子时排除的文件 glob 模式（相对于种子根目录）。
	Exclude []string `yaml:"exclude"`
	// ProbeVideos 是否使用 ffprobe 补充探测音频类型（杜比全景声/FLAC/DTS 等）。
	ProbeVideos bool `yaml:"probe_videos"`
	// HDRProbePath 是 hdrprobe 可执行文件路径，默认 "hdrprobe"（在 PATH 中查找）；
	// 用于从码流快速检测杜比视界/HDR/分辨率等视频类型。留空禁用 hdrprobe 检测。
	HDRProbePath string `yaml:"hdrprobe_path"`
	// ScanInterval 是自动扫描间隔（秒），0 表示仅在启动和手动触发时扫描。
	ScanInterval int `yaml:"scan_interval"`
	// AuthToken 是可选的 API 鉴权 token（Bearer），留空则不鉴权。
	AuthToken string `yaml:"auth_token"`
	// TLSCert 是 HTTPS 证书文件路径（PEM，通常为 fullchain），留空则不启用 HTTPS。
	TLSCert string `yaml:"tls_cert"`
	// TLSKey 是 HTTPS 私钥文件路径（PEM）。
	TLSKey string `yaml:"tls_key"`

	// SourcePath 记录配置文件实际路径（内部使用）。
	SourcePath string `yaml:"-"`
}

// Default 返回默认配置。
func Default() *Config {
	return &Config{
		Listen:       ":8080",
		DataDir:      "./data",
		TorrentDir:   "./torrents",
		Private:      true,
		PieceLength:  0,
		HDRProbePath: "hdrprobe",
	}
}

// Load 加载配置：先读默认值，再读取 YAML 文件（若存在），最后应用环境变量覆盖。
// 加载完成后会自动创建配置引用的目录（config/data/downloads/torrents），
// 因此只映射一个 data 目录时，子文件夹由应用自行创建，已存在则复用。
func Load(path string) (*Config, error) {
	cfg := Default()

	if path == "" {
		path = findConfigPath()
	}
	// 只要指定了配置路径（如环境变量 BILI_TORRENT_CONFIG），即使文件尚不存在，
	// 也确保其所在目录存在，便于后续将配置保存到该处。
	if path != "" {
		if dir := filepath.Dir(path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("创建配置目录 %s 失败: %w", dir, err)
			}
		}
	}
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("读取配置文件失败: %w", err)
			}
		} else if len(data) > 0 {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("解析配置文件 %s 失败: %w", path, err)
			}
			cfg.SourcePath = path
		}
	}

	applyEnv(cfg)

	if err := cfg.EnsureDirs(); err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// findConfigPath 返回配置文件路径：优先取环境变量 BILI_TORRENT_CONFIG
// （即使文件尚不存在也返回，便于首次启动后保存），其次取已存在的候选路径。
func findConfigPath() string {
	if v := os.Getenv("BILI_TORRENT_CONFIG"); v != "" {
		return v
	}
	for _, c := range []string{"config.yaml", "/config/config.yaml"} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// EnsureDirs 确保配置引用的目录存在（不存在则自动创建，已存在则复用）：
// 数据目录、种子输出目录、每个下载根目录，以及配置文件所在目录。
func (c *Config) EnsureDirs() error {
	mkdir := func(p, what string) error {
		if p == "" {
			return nil
		}
		if err := os.MkdirAll(p, 0o755); err != nil {
			return fmt.Errorf("创建%s目录 %q 失败: %w", what, p, err)
		}
		return nil
	}
	if err := mkdir(c.DataDir, "数据"); err != nil {
		return err
	}
	if err := mkdir(c.TorrentDir, "种子输出"); err != nil {
		return err
	}
	for _, r := range c.Roots {
		if err := mkdir(r, "下载根"); err != nil {
			return err
		}
	}
	if c.SourcePath != "" {
		if err := mkdir(filepath.Dir(c.SourcePath), "配置"); err != nil {
			return err
		}
	}
	return nil
}

// applyEnv 使用 BILI_TORRENT_* 环境变量覆盖配置项，便于 Docker 部署。
func applyEnv(cfg *Config) {
	if v := os.Getenv("BILI_TORRENT_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("BILI_TORRENT_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("BILI_TORRENT_TORRENT_DIR"); v != "" {
		cfg.TorrentDir = v
	}
	if v := os.Getenv("BILI_TORRENT_ROOTS"); v != "" {
		cfg.Roots = splitList(v)
	}
	if v := os.Getenv("BILI_TORRENT_TRACKERS"); v != "" {
		cfg.Trackers = splitList(v)
	}
	if v := os.Getenv("BILI_TORRENT_PRIVATE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Private = b
		}
	}
	if v := os.Getenv("BILI_TORRENT_PROBE_VIDEOS"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.ProbeVideos = b
		}
	}
	if v := os.Getenv("BILI_TORRENT_HDRPROBE_PATH"); v != "" {
		cfg.HDRProbePath = v
	}
	if v := os.Getenv("BILI_TORRENT_AUTH_TOKEN"); v != "" {
		cfg.AuthToken = v
	}
	if v := os.Getenv("BILI_TORRENT_TLS_CERT"); v != "" {
		cfg.TLSCert = v
	}
	if v := os.Getenv("BILI_TORRENT_TLS_KEY"); v != "" {
		cfg.TLSKey = v
	}
	if v := os.Getenv("BILI_TORRENT_SCAN_INTERVAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.ScanInterval = n
		}
	}
}

func splitList(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Validate 校验并规范化配置。
func (c *Config) Validate() error {
	if strings.TrimSpace(c.Listen) == "" {
		c.Listen = ":8080"
	}

	abs := func(p string) (string, error) {
		a, err := filepath.Abs(p)
		if err != nil {
			return "", fmt.Errorf("路径 %q 解析失败: %w", p, err)
		}
		return a, nil
	}

	dataDir, err := abs(c.DataDir)
	if err != nil {
		return err
	}
	c.DataDir = dataDir

	torrentDir, err := abs(c.TorrentDir)
	if err != nil {
		return err
	}
	c.TorrentDir = torrentDir

	seen := map[string]bool{}
	roots := make([]string, 0, len(c.Roots))
	for _, r := range c.Roots {
		ra, err := abs(r)
		if err != nil {
			return err
		}
		if info, err := os.Stat(ra); err != nil {
			return fmt.Errorf("下载根目录 %q 不存在或不可访问: %w", ra, err)
		} else if !info.IsDir() {
			return fmt.Errorf("下载根目录 %q 不是目录", ra)
		}
		if !seen[ra] {
			seen[ra] = true
			roots = append(roots, ra)
		}
	}
	c.Roots = roots

	if c.PieceLength < 0 || c.PieceLength > 27 {
		return fmt.Errorf("piece_length 必须在 0(自动) 或 16-27 之间，当前为 %d", c.PieceLength)
	}
	if c.PieceLength > 0 && c.PieceLength < 16 {
		return fmt.Errorf("piece_length 为 2^n 字节的指数，n 最小为 16（64KiB），当前为 %d", c.PieceLength)
	}
	if c.ScanInterval < 0 {
		c.ScanInterval = 0
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return fmt.Errorf("tls_cert 与 tls_key 必须同时配置（或同时留空）")
	}
	return nil
}

// Save 将当前配置写回配置文件（SourcePath 或默认候选路径），原子写入。
func (c *Config) Save() error {
	path := c.SourcePath
	if path == "" {
		path = os.Getenv("BILI_TORRENT_CONFIG")
	}
	if path == "" {
		// 与 findConfigPath 的候选路径保持一致
		if info, err := os.Stat("/config"); err == nil && info.IsDir() {
			path = "/config/config.yaml"
		} else {
			path = "config.yaml"
		}
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建配置目录失败: %w", err)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("写入配置失败: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}
	c.SourcePath = path
	return nil
}
