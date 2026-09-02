# X-UI · RoyLive 开源维护版

> 此仓库为公开维护版本，基于 [FranzKafkaYu/x-ui](https://github.com/FranzKafkaYu/x-ui) 演进，并保留原项目的 GPL-3.0 许可证和署名。

面向日常服务器运维的 Xray 控制面板：优先呈现运行健康、流量和入站状态，并将高频管理操作收敛为更清晰的工作流。

## 本版关键能力

- 简洁的运行健康监控：正常时只显示“运行正常”，异常时才展开具体监测项。
- 服务器流量监控：月度额度、每月 1–31 日重置、近 14 天趋势和月底用量预测。
- 资源状态重新排序：实时网速与网络连接置于前列，并提供最近 60 秒实时曲线。
- VLESS 专注的入站管理：按月自动重置入站流量，隐藏非必要传输信息，日期仅显示到天。
- 用户与配置分发：可为入站新增独立用户，直接下载 Clash Verge 配置；管理菜单可生成二维码或复制配置。
- 面板设置增强：流量配置可靠保存、可选择时区、显示证书关键信息并生成自签名证书。

## 界面预览

| 系统状态 | 入站管理 | 面板设置 |
| --- | --- | --- |
| ![系统状态](docs/screenshots/system-status.png) | ![入站管理](docs/screenshots/inbounds.png) | ![面板设置](docs/screenshots/panel-settings.png) |

截图来自隔离预览环境，未使用线上访问地址或管理凭据。

## 使用与维护说明

- 本版本的网页管理界面仅支持 VLESS；已有非 VLESS 入站不会在此管理流程中创建或编辑。
- 为避免影响既有节点，Reality 等现有传输配置在用户管理流程中保持原样；无需要时不建议修改。
- 上游仓库的“一键安装/更新”脚本会拉取上游版本，**不适用于部署或更新本维护版**。
- 完整变更见 [CHANGELOG.md](./CHANGELOG.md)。

## 一键安装

在目标服务器以 root 身份执行（自动识别 amd64/arm64，下载已编译 Release，无需安装 Go）：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/chung4u/x-ui-roylive/main/scripts/install.sh)
```

- 安装过程保留 `/etc/x-ui/x-ui.db`，不会重置面板账号、入站或流量数据。
- 首次安装使用默认账号 `admin` / `admin`，同时自动生成随机访问路径和端口，并在安装结束时输出完整访问地址；请首次登录后立即修改密码并放行对应 TCP 端口。
- 运行文件会备份到 `/usr/local/x-ui/backups/<时间>/`，然后重启 `x-ui` 服务；不会修改服务器网络或 VPN。
- 如需先上传文件、暂不重启服务，请先执行 `export XUI_NO_RESTART=1`；安装结束后手动运行 `systemctl restart x-ui`。
- 安装完成后，`x-ui install` 与 `x-ui update` 会调用同一项目安装器；不会再拉取上游版本。
- 默认安装最新 Release。若需固定版本，请设置 `XUI_VERSION=<发布标签>` 后再执行。
- GitHub Actions 会在推送 `v*` 标签时自动构建 Linux amd64/arm64 安装包并生成 Release。
