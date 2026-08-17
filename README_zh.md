# MRSS

![MRSS 界面截图](imgs/og1.png)

<p>
  <a href="README.md">English</a> | <strong>简体中文</strong>
</p>

[![Release](https://img.shields.io/github/v/release/marcomarcogd/MRSS?label=release)](https://github.com/marcomarcogd/MRSS/releases/latest)
[![License](https://img.shields.io/badge/license-GPL--3.0-green.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://go.dev/)
[![Wails](https://img.shields.io/badge/Wails-v3-blue)](https://wails.io/)

MRSS 是一款注重隐私的跨平台桌面 RSS 阅读器，提供翻译、本地与 AI 摘要、订阅发现、自动化和多种集成功能。应用数据保存在本机，本发行版不包含分析或遥测服务。

> [!IMPORTANT]
> **需要一次手动升级：** v1.4.2 及更早版本使用旧仓库标识，无法自动安装 v1.5.0。请从 [MRSS Releases](https://github.com/marcomarcogd/MRSS/releases/latest) 手动下载 v1.5.0；完成此次升级后，应用内更新会继续使用 MRSS 仓库。

## 功能特性

- 支持 RSS、Atom、OPML、XPath、脚本和 Newsletter 订阅
- 支持文章翻译、本地 TF-IDF/TextRank 摘要和云端 AI 摘要
- 提供智能发现、筛选器、规则、标签、多媒体库和全文提取
- 集成 FreshRSS、RSSHub、Obsidian、Notion 和 Zotero
- 支持亮暗主题、独立界面/正文字体设置和快捷键
- 提供桌面便携包、无界面服务器和 Docker 镜像

## 下载

请从本复刻版的 [GitHub Releases](https://github.com/marcomarcogd/MRSS/releases/latest) 下载：

- Windows：`MRSS-{version}-windows-{arch}-installer.exe`
- macOS：`MRSS-{version}-darwin-universal.dmg`
- Linux：`MRSS-{version}-linux-{arch}.AppImage`
- 便携包：`MRSS-{version}-<platform>-<arch>-portable.*`
- Codex Skill：`MRSS-{version}-skills.zip`

所有分发包都附带 GPL-3.0 许可证和源码说明。

## 数据迁移

普通模式的数据目录为：

- Windows：`%APPDATA%\MRSS\`
- macOS：`~/Library/Application Support/MRSS/`
- Linux：`~/.local/share/MRSS/`

首次启动时，如果只有旧 `MrRSS` 目录包含 `rss.db`，MRSS 会在打开数据库前原子迁移整个目录。如果新旧数据库同时存在，MRSS 优先使用新目录并保持旧目录不变；如果原子改名失败，则继续使用旧目录并记录警告，不覆盖用户数据。

便携模式继续使用程序旁的 `data/`，服务器模式继续使用 `./data`。

## 从源码构建

环境要求：

- Go 1.25+
- Node.js 24
- Wails CLI `v3.0.0-alpha2.117`
- [构建要求](docs/BUILD_REQUIREMENTS.md)中列出的平台依赖

```bash
git clone https://github.com/marcomarcogd/MRSS.git
cd MRSS
go mod download
cd frontend && npm ci && cd ..
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117
task build
```

提交前执行：

```bash
make check
pre-commit run --all-files
```

详细规则请参阅[贡献指南](CONTRIBUTING.md)、[行为准则](CODE_OF_CONDUCT.md)、[测试指南](docs/TESTING.md)和[架构文档](docs/ARCHITECTURE.md)。

## 服务器与 Docker

```bash
docker run -d -p 1234:1234 -v mrss-data:/app/data ghcr.io/marcomarcogd/mrss:latest
```

本地 API 参见 [Swagger 文档](docs/SERVER_MODE/swagger.json)，Codex Skill 参见 [Skills 文档](docs/SKILLS.zh.md)。

## 复刻归属与许可证

MRSS 是基于 [DevXDojo/MrRSS](https://github.com/DevXDojo/MrRSS) 修改的非官方复刻版，本发行版修改日期为 2026 年 8 月 17 日。本项目与上游维护者不存在官方隶属或背书关系。

MRSS 按 [GNU GPL-3.0](LICENSE) 许可证发布，保留原始许可证、既有版权声明和 Git 历史。本发行版对应源码位于 [marcomarcogd/MRSS](https://github.com/marcomarcogd/MRSS)。本软件不提供任何担保，详情请参阅许可证。

复刻版问题请提交到 [MRSS Issues](https://github.com/marcomarcogd/MRSS/issues)；准备贡献给上游的修复，应另行遵循上游项目的贡献流程。
