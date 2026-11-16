# 管理员安全升级

升级前请创建 SQLite 数据库备份，并确认有可用的 SSH 管理通道。原有管理员账号和密码仍可登录。首次启动会将旧密码迁移为 Argon2id 散列并清空明文字段，已登录的管理员需要重新登录。恢复旧版本前，请在数据库副本上验证所选版本与当前认证格式兼容。

## 管理入口

面板支持直接配置 HTTPS 证书，也支持在可信反向代理上终止 TLS。使用反向代理时，通过 `systemctl edit x-ui` 配置公网来源和连接面板时的代理源 IP。公网来源应包含协议、域名和实际使用的端口，省略面板路径。

```ini
[Service]
Environment="XUI_PUBLIC_ORIGIN=https://panel.example.com"
Environment="XUI_TRUSTED_PROXIES=192.0.2.10"
```

将示例值换成实际来源，并让反向代理覆盖客户端提交的转发头。未配置 HTTPS 时，可在本机用 SSH 隧道连接服务器的回环端口。将示例中的端口、SSH 用户、服务器和面板路径替换为安装器输出的值。

```bash
ssh -N -L 127.0.0.1:54321:127.0.0.1:54321 root@panel.example.com
```

在浏览器打开 `http://127.0.0.1:54321/面板路径/`。

## 重置管理员密码

新密码至少 12 个字符，最多 1024 字节。在服务器的 Bash 中从标准输入设置密码，避免将其写入命令行参数或历史。把 `admin` 换成要使用的管理员账号。

```bash
read -rs -p '新密码：' panel_password
printf '%s' "$panel_password" | /usr/local/x-ui/x-ui setting -username admin -password-stdin
unset panel_password
```

通过网页或命令行修改管理员凭据后，需要重新登录所有管理员会话。
