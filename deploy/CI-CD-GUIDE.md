# GitHub Actions 的 CI/CD 配置如下，无需额外操作

# --- Secrets 与 Variables ---
# 在 GitHub Repo → Settings → Secrets and variables → Actions 中配置：

# Secrets (加密，不显示在日志中):
#   SSH_HOST          你的服务器 IP/域名
#   SSH_USER          服务器 SSH 用户名
#   SSH_PRIVATE_KEY   SSH 私钥（ed25519 推荐），服务器上 ~/.ssh/authorized_keys 添加对应公钥
#   DB_PASSWORD       数据库密码
#   REDIS_PASSWORD    Redis 密码 (如无 留空)
#   JWT_SECRET        JWT 签名密钥 (建议 openssl rand -hex 32 生成)

# Variables (非敏感，日志可见):
#   DB_HOST           数据库地址 (默认 mysql)
#   DB_USER           数据库用户名 (默认 root)
#   DB_NAME           数据库名 (默认 blog_front)
#   REDIS_ADDR        Redis 地址 (默认 redis:6379)
#   JWT_EXPIRE        JWT 过期秒数 (默认 7200)
#   SERVER_PORT       服务端口 (默认 8080)

# --- 服务器初始化 (仅首次) ---
# 1. 在服务器上创建目录并让 docker 网络就绪:
#    mkdir -p /opt/blog-front
#
# 2. 将 deploy/docker-compose.prod.yml 放到 /opt/blog-front/docker-compose.yml
#
# 3. 首次运行时需要手动拉取镜像和启动：
#    docker login ghcr.io -u YOUR_GITHUB_USER -p YOUR_PAT
#    cd /opt/blog-front
#    IMAGE_TAG=latest docker compose up -d
#
# 4. 后续 push 到 main 后 GitHub Actions 自动拉取新镜像并重启服务

# --- 回滚 ---
# 在服务器上执行:
#   cd /opt/blog-front
#   IMAGE_TAG=<commit_SHA> docker compose up -d
#   其中 <commit_SHA> 可在 GitHub Actions runs 页面找到

# --- 监控 ---
# GitHub Actions 内置日志 + 状态 Badge:
#   [![CI/CD](https://github.com/<user>/blog-front/actions/workflows/ci-cd.yml/badge.svg)](...)

# --- 效率优化建议 ---
# 1. Go 模块缓存: actions/setup-go@v5 自带 cache: true
# 2. Docker 层缓存: type=gha 利用 GitHub Actions Cache
# 3. 测试并行: go test -parallel=N 根据 runner 的 CPU 核心数设置
