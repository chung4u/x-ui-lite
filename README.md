# X-UI · RoyLive 私有维护版

> 此仓库为私有维护版本，暂不对外发布。它基于 [FranzKafkaYu/x-ui](https://github.com/FranzKafkaYu/x-ui) 演进，保留原项目的 GPL-3.0 许可证和署名。

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

- 本私有版本的网页管理界面仅支持 VLESS；已有非 VLESS 入站不会在此管理流程中创建或编辑。
- 为避免影响既有节点，Reality 等现有传输配置在用户管理流程中保持原样；无需要时不建议修改。
- 上游仓库的“一键安装/更新”脚本会拉取上游版本，**不适用于部署或更新本私有维护版**。本版本请从本仓库构建并按内部发布流程部署。
- 完整变更见 [CHANGELOG.md](./CHANGELOG.md)。

## 上游项目历史说明

以下内容为上游项目保留的历史文档和功能记录，其中的多协议说明、安装脚本与效果图不代表本私有维护版的当前能力。

# 历史效果预览
`面板使用`:  
<details>
<summary><b>点击查看效果预览</b></summary>  
  
![image](https://user-images.githubusercontent.com/38254177/180629631-f76a05c8-ecf0-4685-bbc7-a7058747d213.png)  
![image](https://user-images.githubusercontent.com/38254177/180629662-b7a325fc-1ebb-47c9-992c-1e7c758a326b.png)


 </details>  
 
`Bot使用`:  
<details>
<summary><b>点击查看效果预览</b></summary>  
  
![image](https://user-images.githubusercontent.com/38254177/178551055-893936b7-b75f-4ee8-a773-eee7c6f43f51.png)  
 
</details>  

`流量提醒`:  
<details>
<summary><b>点击查看效果预览</b></summary> 
  
![image](https://user-images.githubusercontent.com/38254177/180039760-dc987a30-e21c-49a3-8e03-19666566a822.png)

</details>  

`SSH提醒`:  
<details>
<summary><b>点击查看效果预览</b></summary> 
  
![image](https://user-images.githubusercontent.com/38254177/180040129-2ec1a7c0-abd3-41dc-aab0-8cd22415c943.png)

</details>  

`限额提醒`:  
<details>
<summary><b>点击查看效果预览</b></summary> 
  
![image](https://user-images.githubusercontent.com/38254177/180040521-af6e9ef8-d7e5-44e8-834e-25b3b8e3e1b5.png)

</details>  

`到期提醒`:  
<details>
<summary><b>点击查看效果预览</b></summary> 
  
![image](https://user-images.githubusercontent.com/38254177/180041690-90ca4b1f-3a2d-470b-bc0c-eca9261a739a.png)

</details>  

`登录提醒`:  
<details>
<summary><b>点击查看效果预览</b></summary> 
  
![image](https://user-images.githubusercontent.com/38254177/180040913-b8bf2fe1-6fc1-43ab-a683-ae23db1866b2.png)  
![image](https://user-images.githubusercontent.com/38254177/180041179-a5f4cd52-a1ba-4aa9-abb2-b94e36722385.png)

</details>  

`用户速览`:  
<details>
<summary><b>点击查看效果预览</b></summary> 
  
![image](https://user-images.githubusercontent.com/38254177/230761101-20431dd7-5bce-489e-9139-0ceb9ab9a2dc.png)

</details>  

`用户查询`:  
<details>
<summary><b>点击查看效果预览</b></summary> 
  
![image](https://user-images.githubusercontent.com/38254177/230761252-c283c02d-82a4-46ce-a180-dfab4048180d.png)

</details>  



# 快捷方式
安装成功后，通过键入`x-ui`进入控制选项菜单，目前菜单内容：
```
  x-ui 面板管理脚本
  0. 退出脚本
————————————————
  1. 安装 x-ui
  2. 更新 x-ui
  3. 卸载 x-ui
————————————————
  4. 重置用户名密码
  5. 重置面板设置
  6. 设置面板端口
  7. 查看当前面板设置
————————————————
  8. 启动 x-ui
  9. 停止 x-ui
  10. 重启 x-ui
  11. 查看 x-ui 状态
  12. 查看 x-ui 日志
————————————————
  13. 设置 x-ui 开机自启
  14. 取消 x-ui 开机自启
————————————————
  15. 一键安装 bbr (最新内核)
  16. 一键申请SSL证书(acme申请)
 
面板状态: 已运行
是否开机自启: 是
xray 状态: 运行

请输入选择 [0-16]: 
```
# 配置要求  
## 内存  
- 128MB minimal/256MB+ recommend  
## OS  
- CentOS 7+
- Ubuntu 16+
- Debian 8+

# 变更记录   
- 2023.07.18：随机生成Reality dest与serverNames,去除微软域名;细化sniffing配置  
- 2023.06.10：开启TLS时自动复用面板证书与域名;增加证书热重载设定;优化设备限制功能  
- 2023.04.09：支持Reality;支持新的telegram bot控制指令  
- 2023.03.05：支持用户到期时间限制;随机用户名、密码与端口生成
- 2023.02.09：支持单端口内用户流量限制与统计；支持VLESS utls配置与分享链接导出  
- 2022.12.07：添加设备并发限制;细化tls配置,支持minVersion、maxVersion与cipherSuites选择    
- 2022.11.14：添加xtls-rprx-vision流控选项;定时自动更新geo与清除日志  
- 2022.10.23：实现全英文支持;增加批量导出分享链接功能；优化页面细节与Telegram通知    
- 2022.08.11：实现Vmess/Vless/Trojan单端口多用户；增加CPU使用超限提醒  
- 2022.07.28：增加acme standalone模式申请证书;增加x-ui自动保活机制;优化编译选项以适配更多系统  
- 2022.07.24：增加自动生成面板根路径，节点流量自动重置功能，设备IP接入变化通知功能
- 2022.07.21：增加节点IP接入变化提醒，Web面板增加停止/重启xray功能，优化部分翻译
- 2022.07.11：增加节点到期提醒、流量预警策略，增加Telegram bot节点复制、获取分享链接等
- 2022.07.03：重构Telegram bot功能，指令控制不再需要键盘输入;增加Trojan底层传输配置
- 2022.06.19：增加Shadowsocs2022新的Cipher，增加节点搜索、一键清除流量功能
- 2022.05.14：增加Telegram bot Command控制功能，支持关闭/开启/删除节点等
- 2022.04.25：增加SSH登录提醒、面板登录提醒
- 2022.04.23：增加更多Telegram bot提醒功能
- 2022.04.16：增加面板设置Telegram bot功能
- 2022.04.12：优化Telegram Bot通知提醒
- 2022.04.06：优化安装/更新流程，增加证书签发功能，添加Telegram bot机器人推送功能
# Telegram

[订阅频道](https://t.me/CoderfanBaby)  
[讨论群组](https://t.me/franzkafayu)

# 致谢

- [vaxilu/x-ui](https://github.com/vaxilu/x-ui)
- [XTLS/Xray-core](https://github.com/XTLS/Xray-core)
- [telegram-bot-api](https://github.com/go-telegram-bot-api/telegram-bot-api)  

# 广告赞助  

如果你觉得本项目对你有用,而且你也恰巧有这方面的需求,你也可以选择通过我的购买链接赞助我  
- [搬瓦工GIA高端线路](https://bandwagonhost.com/aff.php?aff=65703),仅推荐购买GIA套餐      
- [Cloudcone性价比主机提供商](https://app.cloudcone.com/?ref=7536)  
- [Spartan三网4837性价比主机](https://billing.spartanhost.net/aff.php?aff=1875)

  
如果你希望购买一些现成的代理服务,可选择下述代理服务
- [搬瓦工关联机场](https://justmysocks.net/members/aff.php?aff=18177)  
- [高端奶昔机场](https://nxboom.com/signupbyemail.aspx?MemberCode=2fd79885e45549049c66698f1eea154620230921234746)

VPS推送可关注电报[频道](https://t.me/VpsReStockAlert)  

## Stargazers over time

[![Stargazers over time](https://starchart.cc/FranzKafkaYu/x-ui.svg)](https://starchart.cc/FranzKafkaYu/x-ui)
