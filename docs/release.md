# 发布流程

本文说明如何构建 release 产物并发布新版本。

## 生成 release 产物

```bash
scripts/release.sh v0.1.0
```

产物会输出到 `dist/`：

```text
lsp-bridge_v0.1.0_linux_amd64.tar.gz
lsp-bridge_v0.1.0_linux_arm64.tar.gz
lsp-bridge_v0.1.0_darwin_amd64.tar.gz
lsp-bridge_v0.1.0_darwin_arm64.tar.gz
checksums.txt
```

## 本地发布

如果已安装并登录 GitHub CLI，可以直接发布：

```bash
PUBLISH=1 scripts/release.sh v0.1.0
```

## GitHub Actions

仓库已配置两个工作流：

- `CI`: 推送到 `master`/`main` 或发起 PR 时，执行脚本语法检查、`go test ./...`、`go build -buildvcs=false ./...` 和 release 归档构建。
- `Release`: 推送 `v*` tag 时，构建 release 归档并发布到 GitHub Releases。

发布新版本：

```bash
git tag -a v0.1.0 -m v0.1.0
git push origin v0.1.0
```
