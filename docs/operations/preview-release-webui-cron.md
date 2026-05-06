# `feat/webui-cron-management` 自动打包流程

> - **GitHub Actions 名称**：`WebUI Cron Preview`
> - **Workflow 文件**：[`.github/workflows/preview-webui-cron.yml`](../../.github/workflows/preview-webui-cron.yml)
> - **Job 名称**：`Build & Publish Preview`
> - **Actions 页面入口**：`https://github.com/{owner}/{repo}/actions/workflows/preview-webui-cron.yml`

本文档说明 `feat/webui-cron-management` 分支如何通过 GitHub Actions 自动构建并发布预览版 Release。

---

## 触发时机

| 触发条件 | 说明 |
|---------|------|
| `push` 到 `feat/webui-cron-management` | 自动跑（除非只改了 `*.md` / `docs/**`） |
| Actions 页面手动 `workflow_dispatch` | 任何时候可手动触发 |
| 同分支连续 push | 旧任务自动取消，只跑最新一次（`concurrency` 控制） |

---

## 执行步骤（一次完整运行）

```
1. Checkout 当前分支（fetch-depth: 0 拿全量历史）
   ↓
2. 计算版本号
   BASE = git describe 最近的 v* tag（排除 *nightly* / *preview*）
   VERSION = ${BASE}-cron-preview.{YYYYMMDD}.{8位短SHA}
   例：v0.2.0-cron-preview.20260506.3d252a14
   ↓
3. 准备工具链
   - Go (从 go.mod 读版本)
   - pnpm 10.33.0 + Node 22（带 pnpm 缓存）
   - zip
   ↓
4. 本地打一个 VERSION 名字的 git tag(仅本地, 不 push)
   GoReleaser 用它当版本号
   ↓
5. 跑 GoReleaser release --clean --skip=docker --skip=notarize
   会执行 .goreleaser.yaml 里的:
   - before hooks: go generate / pnpm build / winres / android bundle
   - builds: 全平台二进制(Linux/Win/macOS/FreeBSD/NetBSD × 多架构)
   - archives: tar.gz / zip
   - nfpms: deb / rpm
   - 跳过: docker 镜像、macOS 签名/公证
   - NIGHTLY_BUILD=true 让 goreleaser 自己不创建 GitHub Release
   ↓
6. 收集产物 + 发布 Release
   - 删掉旧的 webui-cron-preview tag 和对应 release
   - 强推新的 webui-cron-preview tag 指向当前 commit
   - gh release create 上传:
     dist/*.tar.gz, *.zip, *.deb, *.rpm, checksums.txt
     build/picoclaw-android-universal.zip
   - 标记 --prerelease --latest=false（不影响 latest）
```

---

## 产出

**GitHub Releases** 页面会出现一个固定条目：

- **标题**：`WebUI Cron Management Preview`
- **Tag**：`webui-cron-preview`（永远滚动指向最新 commit）
- **类型**：Pre-release（不会顶掉正式版的 latest 标记）
- **资产**：本次构建的所有平台二进制 + Android bundle + checksums
- **正文**：版本号 + 源分支 + 短 SHA + 与基线版本的 Full Changelog 链接

QA / 用户固定访问同一个 URL 就能拿到最新预览：

```
https://github.com/{owner}/{repo}/releases/tag/webui-cron-preview
```

---

## 不会做的事

- ❌ 不推 Docker 镜像（避免污染 ghcr/dockerhub 的 `nightly`/`latest` tag）
- ❌ 不签 macOS（省 10~20 分钟，预览版本不需要公证）
- ❌ 不传火山云 TOS（仅 GitHub Releases）
- ❌ 不打正式 `v*` tag（只用滚动 tag，不污染版本历史）
- ❌ 不影响 nightly / 正式 release 流程（互不干扰）

---

## 与现有 workflow 的关系

| Workflow 名称 | 文件 | 触发 | 结果 |
|---------------|------|------|------|
| **WebUI Cron Preview**（本文档） | `preview-webui-cron.yml` | push 到本分支 | 滚动 prerelease `webui-cron-preview` |
| **Nightly Build** | `nightly.yml` | 每日 0:00 UTC + 手动 | 滚动 prerelease `nightly`（基于 main） |
| **Release** | `release.yml` | 手动 + 已有 tag | 正式 Release（带 Docker + TOS + 签名） |
| **Create Tag** | `create-tag.yml` | 手动 | 从 main 创建新 tag |
| **build** | `build.yml` | push 到 main | 仅做编译验证 |

三者各自独立，不会互相覆盖。

---

## 关联改动

为支持本流程，对仓库做了以下调整：

| 文件 | 改动 |
|------|------|
| `.github/workflows/preview-webui-cron.yml` | 新增 |
| `.goreleaser.yaml` | 1) `git.ignore_tags` 加入 `webui-cron-preview` 和 `*-cron-preview.*`；2) 两个 builds 的 `ignore` 列表加入 `linux/mipsle` 和 `netbsd/arm64`（`pkg/cron` 引入 `modernc.org/sqlite` → `modernc.org/libc` 不支持这两个平台） |
| `.github/workflows/nightly.yml` | `git describe` 加 `--exclude "*preview*"`，nightly 不再误把 preview tag 当基线 |

---

## 已知耗时

单次 push 大约 **15~25 分钟**（取决于 GoReleaser 多平台并行构建和 Android bundle）。

如果嫌慢可以：

1. 砍 `goos`/`goarch` 矩阵——做一份精简的 `.goreleaser.preview.yaml`
2. 关闭 Android bundle（在 workflow 中改 `INCLUDE_ANDROID_BUNDLE: "false"`）
3. 只在 `workflow_dispatch` 跑全平台、push 时只跑 linux + windows amd64

---

## 后续可调

- **想给 QA 看到 Docker 镜像**：去掉 `--skip=docker`，但 ghcr / dockerhub 的 tag 模板会输出 `nightly`（因为复用了 `NIGHTLY_BUILD=true`）会冲突——这块需要扩展 goreleaser 模板加一个 `PREVIEW_BUILD` 环境变量来区分
- **想要每次都打正式 tag**：改用 `release.yml` 流程，先用 `create-tag.yml` 从本分支打 tag（需先把 `create-tag.yml` 的 `ref: main` 改为可选输入）

---

## 维护手册

### 排查首次构建失败

1. 打开 Actions 页面 → 左侧栏选 **`WebUI Cron Preview`** → 点失败的 run 看日志
2. 常见问题：
   - `pnpm build:backend` 失败 → 前端 lockfile 与 main 不同步
   - `winres` 失败 → 版本号格式 winres 不接受（参考 nightly 是否同样失败）
   - `gh release create` 401 → 检查 workflow 的 `permissions.contents: write`
   - `build constraints exclude all Go files in modernc.org/libc/*` → 又有新平台不被 modernc/libc 支持了；按 `goos/goarch` 加到 `.goreleaser.yaml` 的 builds[].ignore 列表里（参考已有的 `linux/mipsle`、`netbsd/arm64` 条目）

### 临时停用

把 workflow 顶部的 `on.push` 删除或改成只保留 `workflow_dispatch`：

```yaml
on:
  workflow_dispatch:
```

### 彻底删除预览 release

```bash
gh release delete webui-cron-preview --cleanup-tag -y
```

下次 push 后会重新生成。

---

## 本次接入的改动范围

为引入 `WebUI Cron Preview` 这个流程，仓库整体改动如下 —— **新增 1 个 workflow + 2 处配套小改动 + 1 份文档**：

| 类型 | 文件 | 性质 |
|------|------|------|
| ➕ 新增 | `.github/workflows/preview-webui-cron.yml` | **新 workflow：`WebUI Cron Preview`** |
| ✏️ 改动 | `.goreleaser.yaml` | `git.ignore_tags` 多加 2 行（pattern） |
| ✏️ 改动 | `.github/workflows/nightly.yml` | `git describe` 命令多加 `--exclude "*preview*"` |
| ➕ 新增 | `docs/operations/preview-release-webui-cron.md` | 本说明文档 |

### 关键澄清

- ✅ **没有**改动 `release.yml`、`create-tag.yml`、`build.yml`、`docker-build.yml` 等任何现有 workflow 的功能
- ✅ **没有**修改 goreleaser 的构建矩阵、产物列表、Docker 配置
- ✅ **没有**碰任何业务代码（Go / 前端）

### 那两处配套小改动的作用

1. **`.goreleaser.yaml` 的 `ignore_tags`** —— 防御性改动，告诉 goreleaser 在"自动推断版本"时忽略我们的滚动 tag。当前流程用 `GORELEASER_CURRENT_TAG` 显式传版本，所以**不加也不会出错**，加了是为未来安全。
2. **`nightly.yml` 的 `--exclude "*preview*"`** —— 防止 nightly 跑的时候，把 `webui-cron-preview` tag 误当成基线版本。这是**实际有用的**，因为我们的 preview 会推一个 git tag 到远端。

所以真正"功能性新增"就是那个新 workflow；其余 3 处都是为了让它和已有体系干净共存的"修边"动作。
