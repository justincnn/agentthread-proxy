# agentthread-proxy

OpenAI 兼容的**透明反向代理** —— 用你自己的 Key 上网,给别人发出去的请求换上你的上游 Key,再把上游的回答原样转回来。整个程序是**一个 Go 二进制**(无依赖),常驻内存仅约 3MB。

> 类比:你像"翻译官",你的 APP 说中文(任意 OpenAI 协议),代理在中途把"说话人身份"换成你提供的上游 Key,内容一个字不改地透传。支持流式(SSE)。

## 它解决什么

- 你有一个**上游 AI 网关**(任何兼容 OpenAI 的地址:如 1api / axonhub / todo2api / chatgpt / 自建网关),带自己的 `API Key`
- 你想把这个能力**暴露给 API 端点**让对方 / 客户端通过一个统一地址调用,但**不想让对方知道你的上游 Key**
- 这个代理夹在中间:客户端认证用 `PROXY_API_KEY`(你发下去的),后端转发用 `UPSTREAM_API_KEY`(你的真 Key),**上游 Key 永不外泄**

## 特性
- ✅ OpenAI 兼容:`/v1/models`、`/v1/responses`、`/v1/chat/completions` 全透传
- ✅ **SSE 流式输出**(打字机效果)逐字节转发
- ✅ 认证隔离:客户端只认 `PROXY_API_KEY`,上游只认 `UPSTREAM_API_KEY`
- ✅ 剥离头部注入:`X-Forwarded-*`、`cf-*` 等客户端手补头被清理,杜绝伪造源
- ✅ **单二进制零依赖**,内存 ~3MB,适合低配 VPS
- ✅ `systemd` 常驻管理,崩溃自动重启

## 快速开始

### 1. 下载/编译二进制

```bash
go build -o agentthread main.go          # 编译
# 或直接拷贝 aarch64/amd64 预编译产物(见 Releases)
```

### 2. 配置环境变量(写入 `.env`)

```bash
cat > .env <<'EOF'
PORT=3100
PROXY_API_KEY=你的-客户端-key              # 发给调用方,256-bit 随机建议
UPSTREAM_BASE_URL=https://你的上游地址/v1   # 例如 https://axonhub.xxx/v1
UPSTREAM_API_KEY=你的上游真-key            # 你的秘密,不放别人那
EOF
chmod 600 .env
```

### 3. 运行

```bash
# 前台(调试)
source .env && ./agentthread

# systemd 常驻(推荐)
sudo cp agentthread /usr/local/bin/
sudo tee /etc/systemd/system/agentthread.service > /dev/null <<'EOF'
[Unit]
Description=agentthread proxy
After=network.target
[Service]
Type=simple
WorkingDirectory=/root/agentthread
EnvironmentFile=/root/agentthread/.env
ExecStart=/usr/local/bin/agentthread
Restart=always
[Install]
WantedBy=multi-user.target
EOF
sudo systemctl daemon-reload && sudo systemctl enable --now agentthread
```

## 测试

```bash
BASE=http://127.0.0.1:3100
K=你的PROXY_API_KEY

curl -s $BASE/health                                   # {"status":"ok"}
curl -s -H "Authorization: Bearer $K" $BASE/v1/models  # 模型列表
curl -s -H "Authorization: Bearer $K" -H "Content-Type: application/json" \
  -d '{"model":"qwen3.8-max","input":"hi"}' \
  $BASE/v1/responses
# 流式: 加 "stream":true
```

> 注意:`/v1/models` 返回的 `gpt-5.6-*` 列表是占位标识,上游若没有这些模型,调用时**必须用上游真实模型名**(如 `qwen3.8-max`)。这也意味着该代理**不校验模型名**,完全交给上游。

## 反代到公网 HTTPS(可选)

本地起好后,Caddy 三段:

```caddyfile
agentthread.example.com {
    reverse_proxy 127.0.0.1:3100
}
```

`caddy` 自动签发 HTTPS。Caddy 开 `flush_interval 1ms`(默认)即保流式即时;若要在网络层确保 SSE 不缓冲,可加:`encode zstd gzip`(可选压缩)。

## FAQ

**Q: 为什么上游返回 "model not found"?**
A: 你传给代理的模型名上游没有。代理不校验模型名,直接透传。用上游真实模型名即可(去上游的 `/v1/models` 查)。

**Q: PROXY_API_KEY 和 UPSTREAM_API_KEY 是什么区别?**
PROXY = 你发给客户端的钥匙(客户端带它才能连代理);UPSTREAM = 你的上游真 Key(代理转发时替换,`给上游`。两者必须不同。

**Q: 会不会暴露我的上游 Key?**
不会。上游 Key 只在代理进程内使用,永不返回给客户端。响应头里的上游鉴权信息也会被剥离。

**Q: 内存占用?**
编译后静态链接,常驻约 2-3MB RSS,远低于 Node 版(30-40MB)。

**Q: 能在 Docker 跑吗?**
可以,镜像只需一个二进制 + .env,无任何运行时依赖。

## 免责声明
仅用于合法的 LLM API 网关/反向代理场景。请遵守上游服务条款,勿用于转售/非法滥用。文中的域名、Key 均为示例。

## License
MIT