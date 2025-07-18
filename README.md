# 🚀 订阅检测转换工具

> **✨ 修复逻辑、简化操作、增加功能、节省内存、一键启动无需配置**

> **⚠️ 注意：** 功能更新频繁，请查看最新的[配置文件](https://github.com/beck-8/subs-check/blob/master/config/config.example.yaml)以获取最新功能。

## 📸 预览


![preview](./doc/images/preview.png)  
![result](./doc/images/results.png)  
![admin](./doc/images/admin.png)
| | |
|---|---|
| ![tgram](./doc/images/tgram.png) | ![dingtalk](./doc/images/dingtalk.png)  |

## ✨ 功能特性

- **🔗 订阅合并**
- **🔍 节点可用性检测**
- **🗑️ 节点去重**
- **⏱️ 节点测速**
- **🎬 流媒体平台解锁检测**
- **✏️ 节点重命名**
- **🔄 任意格式订阅转换**
- **🔔 支持100+通知渠道**
- **🌐 内置 Sub-Store**
- **🖥️ WEB 控制面板**
- **⏰ 支持 Crontab 表达式**
- **🖥️ 多平台支持**

## 🛠️ 部署与使用 
> 首次运行会在当前目录生成默认配置文件。

### 🪜 代理设置（可选）
<details>
  <summary>展开查看</summary>

如果拉取非Github订阅速度慢，可使用通用的 HTTP_PROXY HTTPS_PROXY 环境变量加快速度；此变量不会影响节点测试速度
```bash
# HTTP 代理示例
export HTTP_PROXY=http://username:password@192.168.1.1:7890
export HTTPS_PROXY=http://username:password@192.168.1.1:7890

# SOCKS5 代理示例
export HTTP_PROXY=socks5://username:password@192.168.1.1:7890
export HTTPS_PROXY=socks5://username:password@192.168.1.1:7890

# SOCKS5H 代理示例
export HTTP_PROXY=socks5h://username:password@192.168.1.1:7890
export HTTPS_PROXY=socks5h://username:password@192.168.1.1:7890
```
如果想加速github的链接，可使用网上公开的github proxy，或者使用下方自建测速地址处的worker.js自建加速
```
# Github Proxy，获取订阅使用，结尾要带的 /
# github-proxy: "https://ghfast.top/"
github-proxy: "https://custom-domain/raw/"
```

</details>

### 🌐 自建测速地址（可选）
<details>
  <summary>展开查看</summary>

> **⚠️ 注意：** 避免使用 Speedtest 或 Cloudflare 下载链接，因为部分节点会屏蔽测速网站。

1. 将 [worker.js](./doc/cloudflare/worker.js) 部署到 Cloudflare Workers。
2. 绑定自定义域名（避免被节点屏蔽）。
3. 在配置文件中设置 `speed-test-url` 为你的 Workers 地址：

```yaml
# 100MB
speed-test-url: https://custom-domain/speedtest?bytes=104857600
# 1GB
speed-test-url: https://custom-domain/speedtest?bytes=1073741824
```
</details>

### 🐳 Docker 运行

> **⚠️ 注意：**  
> - 限制内存请使用 `--memory="500m"`。  
> - 可通过环境变量 `API_KEY` 设置 Web 控制面板的 API Key。

```bash
# 基础运行
docker run -d \
  --name subs-check \
  -p 8299:8299 \
  -p 8199:8199 \
  -v ./config:/app/config \
  -v ./output:/app/output \
  --restart always \
  ghcr.io/beck-8/subs-check:latest

# 使用代理运行
docker run -d \
  --name subs-check \
  -p 8299:8299 \
  -p 8199:8199 \
  -e HTTP_PROXY=http://192.168.1.1:7890 \
  -e HTTPS_PROXY=http://192.168.1.1:7890 \
  -v ./config:/app/config \
  -v ./output:/app/output \
  --restart always \
  ghcr.io/beck-8/subs-check:latest
```

### 📜 Docker-Compose

```yaml
version: "3"
services:
  subs-check:
    image: ghcr.io/beck-8/subs-check:latest
    container_name: subs-check
    volumes:
      - ./config:/app/config
      - ./output:/app/output
    ports:
      - "8299:8299"
      - "8199:8199"
    environment:
      - TZ=Asia/Shanghai
      # - HTTP_PROXY=http://192.168.1.1:7890
      # - HTTPS_PROXY=http://192.168.1.1:7890
      # - API_KEY=password
    restart: always
    network_mode: bridge
```
### 📦 二进制文件运行

下载 [Releases](https://github.com/beck-8/subs-check/releases) 中适合的版本，解压后直接运行即可。

### 🖥️ 源码运行

```bash
go run . -f ./config/config.yaml
```

## 🔔 通知渠道配置（可选）
<details>
  <summary>展开查看</summary>

> **📦 支持 100+ 通知渠道**，通过 [Apprise](https://github.com/caronc/apprise) 发送通知。

### 🌐 Vercel 部署

1. 点击[**此处**](https://vercel.com/new/clone?repository-url=https://github.com/beck-8/apprise_vercel)部署 Apprise。
2. 部署后获取 API 链接，如 `https://testapprise-beck8s-projects.vercel.app/notify`。
3. 建议为 Vercel 项目设置自定义域名（国内访问 Vercel 可能受限）。

### 🐳 Docker 部署

> **⚠️ 注意：** 不支持 arm/v7。

```bash
# 基础运行
docker run --name apprise -p 8000:8000 --restart always -d caronc/apprise:latest

# 使用代理运行
docker run --name apprise \
  -p 8000:8000 \
  -e HTTP_PROXY=http://192.168.1.1:7890 \
  -e HTTPS_PROXY=http://192.168.1.1:7890 \
  --restart always \
  -d caronc/apprise:latest
```

### 📝 配置文件中配置通知

```yaml
# 填写搭建的apprise API server 地址
# https://notify.xxxx.us.kg/notify
apprise-api-server: ""
# 填写通知目标
# 支持100+ 个通知渠道，详细格式请参照 https://github.com/caronc/apprise
recipient-url: 
  # telegram格式：tgram://{bot_token}/{chat_id}
  # - tgram://xxxxxx/-1002149239223
  # 钉钉格式：dingtalk://{Secret}@{ApiKey}
  # - dingtalk://xxxxxx@xxxxxxx
# 自定义通知标题
notify-title: "🔔 节点状态更新"
```
</details>

## 💾 保存方法配置

> **⚠️ 注意：** 选择保存方法时，请更改 `save-method` 配置。

- **本地保存**：保存到 `./output` 文件夹。
- **R2**：保存到 Cloudflare R2 [配置方法](./doc/r2.md)。
- **Gist**：保存到 GitHub Gist [配置方法](./doc/gist.md)。
- **WebDAV**：保存到 WebDAV 服务器 [配置方法](./doc/webdav.md)。
- **S3**：保存到 S3 对象存储。

## 📲 订阅使用方法

> **💡 提示：** 内置 Sub-Store，可生成多种订阅格式；高级玩家可DIY很多功能

**🚀 通用订阅**
```bash
# 通用订阅
http://127.0.0.1:8299/download/sub

# URI 订阅
http://127.0.0.1:8299/download/sub?target=URI

# Mihomo/ClashMeta
http://127.0.0.1:8299/download/sub?target=ClashMeta

# Clash
http://127.0.0.1:8299/download/sub?target=Clash

# V2Ray
http://127.0.0.1:8299/download/sub?target=V2Ray

# ShadowRocket
http://127.0.0.1:8299/download/sub?target=ShadowRocket

# Quantumult
http://127.0.0.1:8299/download/sub?target=QX

# Sing-Box
http://127.0.0.1:8299/download/sub?target=sing-box

# Surge
http://127.0.0.1:8299/download/sub?target=Surge

# Surfboard
http://127.0.0.1:8299/download/sub?target=Surfboard
```

**🚀 Mihomo/Clash 订阅（带规则）：**
> 默认使用 `https://raw.githubusercontent.com/beck-8/override-hub/refs/heads/main/yaml/ACL4SSR_Online_Full.yaml` 覆写  
可在配置中更改 `mihomo-overwrite-url`。
```bash
http://127.0.0.1:8299/api/file/mihomo
```

## 🌐 内置端口说明
> subs-check本身会在测试完后保存三个文件到output目录中；output目录中的所有文件会被8199端口提供文件服务

| 服务地址                        | 格式说明                | 来源说明|
|-------------------------------|-------------------|----|
| `http://127.0.0.1:8199/sub/all.yaml`   | Clash 格式节点 |由subs-check直接生成|
| `http://127.0.0.1:8199/sub/mihomo.yaml`| 带分流规则的 Mihomo/Clash 订阅 |从上方sub-store转换下载后提供|
| `http://127.0.0.1:8199/sub/base64.txt` | Base64 格式订阅 |从上方sub-store转换下载后提供|

## 🗺️ 架构图
<details>
  <summary>展开查看</summary>

```mermaid
graph TD
    A[订阅链接] -->|获取订阅链接| B[subs-check]
    subgraph subs-check 处理流程
        B -->|转成 YAML 格式| B1[节点去重]
        B1 -->|去除冗余节点| B2[测活]
        B2 -->|节点可用| B3[测速]
        B2 -->|节点不可用| X[丢弃]
        B3 -->|测速达标| B4[流媒体测试]
        B3 -->|测速不达标| X[丢弃]
        B4 -->|解锁检测| B5[生成 all.yaml]
    end
    B5 -->|保存到 output 目录| C[output 目录]
    B5 -->|上传 all.yaml| D[sub-store]
    C -->|保存到各位置| H1[R2/Gist/WebDAV/S3]
    H1 -->|存储完成| H2[发送消息通知]
    D -->|提供订阅转换服务| E[sub-store 转换服务]
    subgraph sub-store 独立功能
        E -->|生成配置文件| E1[mihomo.yaml, base64.txt]
        E -->|其他格式转换| E2[Clash, V2Ray, ShadowRocket 等]
        E -->|订阅分享| E3[分享订阅链接]
    end
    E1 -->|保存到 output 目录| C
    C -->|文件服务| F[8199 端口: /sub]
    B -->|Web 管理| G[8199 端口: /admin]
``` 

</details>

## 🙏 鸣谢
[cmliu](https://github.com/cmliu)、[Sub-Store](https://github.com/sub-store-org/Sub-Store)、[bestruirui](https://github.com/bestruirui/BestSub)、[iplark](https://iplark.com/)

## ⭐ Star History

[![Stargazers over time](https://starchart.cc/beck-8/subs-check.svg?variant=adaptive)](https://starchart.cc/beck-8/subs-check)

## ⚖️ 免责声明

本工具仅供学习和研究使用，使用者应自行承担风险并遵守相关法律法规。
