# X-UI Lite 1.0

> 面向日常服务器运维的 Xray 控制面板：把运行健康、流量与 VLESS 入站管理收敛到更清晰的工作流。

本仓库基于 [FranzKafkaYu/x-ui](https://github.com/FranzKafkaYu/x-ui) 演进，保留原项目的 GPL-3.0 许可证和署名。

[![Release](https://img.shields.io/github/v/release/chung4u/x-ui-lite?display_name=tag&label=release)](https://github.com/chung4u/x-ui-lite/releases/latest)
[![License](https://img.shields.io/github/license/chung4u/x-ui-lite)](./LICENSE)

## 一键安装

在目标服务器以 `root` 身份执行：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/chung4u/x-ui-lite/main/scripts/install.sh)
```

安装器仅提供当前 **1.0 正式版**，自动识别 amd64 / arm64 并下载预编译包，无需安装 Go。

首次安装完成后，终端将输出完整访问地址、管理员账号、密码及本机防火墙处理结果。默认端口为 `54321`，默认账号为 `admin`，默认密码为 `admin`。请首次登录后立即修改密码；如使用云服务器，还需在云防火墙中放行面板端口。

## 1.0 核心亮点

| 能力 | 解决的问题 |
| --- | --- |
| 运行健康优先 | 正常时只保留简洁状态；异常时集中展示需要处理的项目，减少无效信息。 |
| 服务器流量监控 | 支持月度额度、每月 1–31 日重置、近 14 天趋势和月底用量预估。 |
| 实时运维视图 | 处理器、内存、实时上传速率与网络连接优先展示；网速和连接数提供近 60 秒趋势。 |
| VLESS 入站管理 | 入站流量跟随面板重置日进入新周期；列表聚焦端口、用户、流量和操作。 |
| 用户与订阅分发 | 每个入站可管理独立用户，提供 VLESS 链接、订阅地址与 Clash Verge 导入配置。 |
| REALITY 降低配置门槛 | 伪装站点使用经过验证的预设；自动匹配 SNI 与握手目标，并提供密钥和 Short ID 工具。 |
| 设置更可控 | 流量配置可靠保存，支持时区、证书关键信息读取和自签名证书生成。 |

## 产品界面

### 运行状态

实时资源、连接和流量聚合在一个页面；运行健康在正常时保持克制，异常时才展开具体问题。

![系统状态](docs/screenshots/system-status.png)

### 入站管理

列表以流量与限额为核心，支持筛选、单独用户、订阅与客户端配置分发。

![入站管理](docs/screenshots/inbounds.png)

### 面板设置

将访问入口、证书、Xray、流量监控与其他设置统一组织，保存状态清晰可见。

![面板设置](docs/screenshots/panel-settings.png)

## 使用说明

- 网页管理流程专注于 VLESS；已有非 VLESS 入站不会在此界面中创建或编辑。
- 使用 REALITY 时请通过预设站点选择握手目标，修改后重新导入用户订阅或客户端配置。
- 更新安装会保留 `/etc/x-ui/x-ui.db` 中的面板数据和账号。
- 上游仓库的安装脚本会拉取上游版本，**不适用于部署或更新本维护版**。

完整更新记录见 [CHANGELOG.md](./CHANGELOG.md)。
