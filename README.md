# bili-torrent

扫描 bilibili 视频下载目录，识别视频、批量制作种子，并提供网页管理界面与 **bilibili 唯一索引**。

参考 [bili-sync](https://github.com/bili-sync/bili-sync)（下载）与 [mkbrr](https://github.com/autobrr/mkbrr)（种子工具）设计，支持 Docker 部署。

## 功能

- **识别 bilibili 视频**：解析下载目录中的 nfo（`<uniqueid type="bilibili">`），兼容 bili-sync 的目录结构（单页 / 多页 / `Season 1` 分集）；无 nfo 时按文件名 `BVxxxxxxxxxx` 兜底识别。
- **视频类型检测（hdrprobe 码流检测）**：调用 [hdrprobe](https://github.com/matthane/hdrprobe) 从码流中快速识别**杜比视界**（含 DV Profile）、HDR10 / HDR10+ / HLG / HDR Vivid、编码（HEVC/AVC/AV1 等）、分辨率、位深——不再依赖文件名猜测。类型以可点击标签展示在「全部/未制作/已制作」筛选栏中，可与状态筛选组合使用。可选 `probe_videos=true` 用 ffprobe 补充音频类型（杜比全景声/FLAC/DTS 等）。
- **批量制作种子**：每个视频文件夹制作一个种子（复用 go-torrent，自动/自定义 piece length、private、tracker、排除规则）。
- **网页卡片展示**：卡片读取文件夹中的海报（poster.jpg / `*-poster.jpg` / `*-thumb.jpg`）与 nfo 信息；未制作种子的视频同样展示并提供「制作种子」入口；支持搜索、类型/来源筛选、批量选择制作。
- **bilibili 唯一索引**：以 **bvid（+分页）** 为唯一键，记录种子 info hash、种子内文件路径与 **B 站原链接**，通过 `/index.json` / `/api/indexes` 供其它应用关联访问同一个视频。
- **一个文件夹多个视频 → 一个种子 + 多个索引**：多页视频的每个分页各自生成索引，索引指向种子内对应文件。
- **网页管理扫描文件夹**：设置页可增删扫描目录（绝对路径），保存后写入配置文件并在**后台自动重新扫描**，网页顶部实时显示扫描进度（文件夹数/百分比/当前目录）。
- **增量扫描**：媒体库（识别结果）持久化到数据目录，启动即加载、页面立即可用；扫描只处理新增/变化的文件，已识别视频保留探测结果（probe cache）不再重复探测。设置页提供「重置媒体库」按钮，点击后全量重新扫描并重建识别结果（**保留探测缓存与种子记录**）。
- **Docker 支持**：多阶段构建，内置 ffmpeg/ffprobe。

## 快速开始

> 所有数据统一放在一个 `data` 目录下，只需映射这一个目录，其子文件夹
> `config`（配置）、`data`（持久化数据）、`downloads`（B 站视频）、`torrents`（种子输出）
> 由应用**启动时自动创建，已存在则复用**。

### Docker Compose

```bash
cp compose.example.yaml compose.yaml
# 可选：预先放置配置文件 data/config/config.yaml（参考 config.example.yaml），
#       不放置则使用默认配置，之后可在网页「设置」中添加扫描文件夹
docker compose up -d --build
# 打开 http://localhost:8080
```

### Docker

```bash
docker build -t bili-torrent .
docker run -d --name bili-torrent -p 8080:8080 \
  -v /path/to/data:/data \
  bili-torrent
# 子文件夹 config/ data/ downloads/ torrents/ 由应用自动创建
```

### 本地运行

```bash
cp config.example.yaml config.yaml   # 修改 roots 等配置
go build -o bili-torrent .
./bili-torrent -config config.yaml   # 打开 http://localhost:8080
```

## 配置

参考 [config.example.yaml](config.example.yaml)：

| 配置项 | 说明 | 环境变量 |
| --- | --- | --- |
| `listen` | HTTP 监听地址 | `BILI_TORRENT_LISTEN` |
| `data_dir` | 数据目录（种子记录/索引/探测缓存） | `BILI_TORRENT_DATA_DIR` |
| `torrent_dir` | `.torrent` 输出目录 | `BILI_TORRENT_TORRENT_DIR` |
| `roots` | 下载根目录（可多个） | `BILI_TORRENT_ROOTS`（逗号分隔） |
| `trackers` | 种子 announce 地址（可多个） | `BILI_TORRENT_TRACKERS` |
| `private` | 私有种子 | `BILI_TORRENT_PRIVATE` |
| `comment` | 种子备注 | — |
| `piece_length` | piece 长度指数，0=自动 | — |
| `exclude` | 制作种子时排除的 glob | — |
| `hdrprobe_path` | hdrprobe 可执行文件路径（默认 `hdrprobe`，留空禁用） | `BILI_TORRENT_HDRPROBE_PATH` |
| `probe_videos` | 用 ffprobe 补充音频类型探测（需安装 ffmpeg） | `BILI_TORRENT_PROBE_VIDEOS` |
| `scan_interval` | 自动扫描间隔（秒），0=仅手动 | `BILI_TORRENT_SCAN_INTERVAL` |
| `auth_token` | API 鉴权 token（Bearer），留空不鉴权 | `BILI_TORRENT_AUTH_TOKEN` |

## 目录识别规则

```
下载根目录/
├── 收藏夹/动漫/某动画标题/        ← 多页视频：tvshow.nfo + poster.jpg + Season 1/分集
│   ├── tvshow.nfo
│   ├── poster.jpg / fanart.jpg
│   └── Season 1/
│       ├── 标题 - S01E01 - 2160p 杜比视界.mkv   + .nfo + -thumb.jpg
│       └── 标题 - S01E02 - 1080p.mkv            + .nfo + -thumb.jpg
└── 投稿/知识区/单页标题/          ← 单页视频：movie.nfo + {bvid}.mp4
    ├── BV1xxxxxxxxxx.nfo
    └── BV1xxxxxxxxxx.mp4 (+ -poster.jpg)
```

- 含 `uniqueid type="bilibili"` 的 nfo 的目录会被识别为一个视频文件夹（种子单元）。
- 无 nfo 但文件名含 bvid 的目录也会被识别。
- 文件夹内所有文件（视频、nfo、海报、字幕、弹幕）都会进入种子。

## 唯一索引

索引记录示例（`/index.json` / `GET /api/indexes`）：

```json
{
  "key": "BV1a2b3c4d5f_1",
  "bvid": "BV1a2b3c4d5f",
  "cid": "1",
  "title": "异世界日常",
  "page_title": "第一话 相遇",
  "url": "https://www.bilibili.com/video/BV1a2b3c4d5f",
  "info_hash": "c918eeef67fa16c63edd4d7ab6d3e44f3b720003",
  "torrent_name": "异世界日常",
  "file_path": "Season 1/异世界日常 - S01E01 - 2160p 杜比视界.mkv",
  "file_size": 3145728,
  "types": ["杜比视界", "4K"],
  ...
}
```

- `key` 是全局唯一键：单页视频为 bvid，多页视频为 `bvid_分页`。
- 其它应用可通过 `bvid`（或 `/api/indexes/{key}`）关联到同一个视频，并据此在种子中找到对应文件。

## API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/videos` | 视频列表（`status=made/unmade`、`type=`、`source=`、`q=`） |
| GET | `/api/torrents` | 种子列表 |
| POST | `/api/torrents` | 批量制作种子 `{"folders":[...]}`（异步返回任务） |
| GET | `/api/torrents/{hash}/download` | 下载 `.torrent` |
| GET | `/api/torrents/{hash}/indexes` | 种子下所有索引 |
| GET | `/api/indexes` / `/api/indexes/{key}` | 索引列表（`bvid=` 过滤）/ 单个索引 |
| GET | `/api/indexes/{key}/file` | 索引对应视频文件（Range 断点播放，供扩展直接作视频源） |
| GET | `/index.json` | 机器可读索引清单 |
| GET | `/api/tasks` | 任务进度 |
| POST | `/api/rescan` | 异步启动后台扫描（立即返回，进度见 `GET /api/scan`） |
| GET | `/api/scan` | 后台扫描进度（running/progress/current_name/folders/videos） |
| POST | `/api/library/reset` | 重置媒体库：全量重新扫描并重建识别结果（保留探测缓存与种子记录） |
| GET | `/api/poster?path=` | 海报图片（限制在扫描目录内） |
| GET | `/api/file?path=` | 视频文件（限制在扫描目录内，支持 Range 断点播放；点击卡片封面打开） |
| GET | `/api/config` | 公开配置信息 |
| PUT | `/api/config/roots` | 更新扫描文件夹 `{"roots":[...]}`（持久化并后台重扫） |

## 开发

```bash
go build ./...        # 编译
go test ./...         # 测试（testdata 内含 bili-sync 风格测试数据）
```

## 技术栈

- Go 1.26，`github.com/autobrr/go-torrent`（与 mkbrr 同款种子库）
- 内嵌纯前端（无构建步骤），JSON 文件持久化（`data_dir/db.json`）
- Docker 多阶段构建，运行镜像基于 alpine + ffmpeg
