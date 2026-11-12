# 服务器基础设施（Docker）

一台服务器上"数据组件"的最小集合：MySQL 必选，Redis 可选。
和 `devops/docker/docker-compose.yml`（本地开发，一次起 mysql + api + ws）是两回事。

## 为什么不用宝塔装的那套

常见情况：服务器交付时已经带了宝塔面板和它的 MySQL。**它可以继续用**，但要在两种情况里明确选一个，
不要两个都跑。

| | 用宿主机已有的 MySQL | 用 Docker 里的 MySQL |
|---|---|---|
| 端口 | 3306 | 127.0.0.1:13306（避开宿主） |
| 内存 | 一份 | 宿主那份不关就是两份 |
| 可复现 | 差：版本、参数、路径都看面板 | 好：compose 就是定义 |
| 备份 | 要记住备份宿主那份 | 卷名固定，容易定位 |
| 迁移到别的机器 | 手工重装 | `docker compose up` 就够 |
| 适合 | 临时验证、机器上已有数据 | 长期维护、要能重建 |

判断标准很简单：**如果这台机器将来要能"推倒重建"，用 Docker；如果只是先跑起来看看，用宿主的。**
但无论选哪个，都要在 `docs/logs/` 里记下实际用的是哪一个。

> 两个都跑是最差的选择：内存双份，备份容易漏，排查时 `mysql -h 127.0.0.1` 连到哪个全看运气。

## 端口纪律

对外只开一个入口：宿主机 Nginx 的 443（80 只做跳转）。其它一律 127.0.0.1。

| 组件 | 容器内 | 宿主机发布 | 谁能访问 |
|---|---|---|---|
| MySQL | 3306 | `127.0.0.1:${MYSQL_HOST_PORT:-13306}` | 只有本机 |
| Redis | 6379 | **不发布** | 只有 compose 网络内的容器 |
| API | 55555 | `127.0.0.1:55555` | 本机 Nginx 反代 |
| WS | 8090 | `127.0.0.1:8090` | 本机 Nginx 反代 |
| Web | 8080 | 不发布或 `127.0.0.1:8080` | 本机 Nginx |
| Nginx | 443 | `0.0.0.0:443` | 公网（唯一） |

`MYSQL_HOST_PORT` 默认 13306 不是随便挑的：宿主上大概率已经有个 3306。

## 用法

```bash
cd devops/docker/server

cp .env.example .env
mkdir -p secrets
openssl rand -base64 32 > secrets/mysql_root.txt
openssl rand -base64 32 > secrets/mysql_app.txt
chmod 600 secrets/*.txt

# 先预检：端口是否被占用、宿主是否已有 MySQL 在跑、有没有容器监听 0.0.0.0
bash ../../scripts/port-check.sh

docker compose --env-file .env up -d --wait
docker compose --env-file .env ps
```

密码走 `secrets/` 文件（`MYSQL_*_PASSWORD_FILE`），不进环境变量，也不会出现在
`docker inspect` 里。`secrets/` 必须在 `.gitignore` 里。

需要 Redis：

```bash
# .env 里设 REDIS_PASSWORD=...
docker compose --env-file .env --profile with-redis up -d --wait
```

## 验证

```bash
# 只应在 127.0.0.1 上监听
ss -tlnp | grep -E ':(13306|3306|6379)'

# 用应用账号连一次
mysql -h 127.0.0.1 -P 13306 -uagentforge -p -e 'SELECT VERSION()'

# 从外部确认看不到
nmap -Pn -p 13306,3306,6379 <本机公网IP>    # 期望 filtered
```

## 镜像版本

基础镜像按 digest 固定。升级是显式动作：

```bash
docker pull mysql:8.0
docker inspect --format '{{index .RepoDigests 0}}' mysql:8.0   # 拿到新 digest 后改 compose
```

当前固定：`mysql:8.0.46`、`redis:7.4.12`。

## 和备份的关系

- 容器 MySQL：数据在命名卷 `agentforge-infra_mysql-data`。备份要么用 `mysqldump` 走网络，
  要么 `docker exec` 进容器 dump——**不要**直接拷贝卷目录。
- 宿主机 MySQL：按面板/系统的路径备份。
- Redis 若启用，`appendonly yes` 的数据同样在卷里。

具体方案见 `docs/plans/plan_step6.md`。
