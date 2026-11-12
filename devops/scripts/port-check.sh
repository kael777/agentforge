#!/usr/bin/env bash
# 部署前预检：端口占用、宿主机已装组件、端口发布范围。
#
# 解决的问题：服务器给过来时往往自带上一个 MySQL/Redis/Nginx（宝塔、apt 或云厂商镜像），
# 直接 docker compose up 会撞端口；更糟的是"看起来起来了"，实际连的是宿主机那两个。
#
#   bash devops/scripts/port-check.sh
#   MYSQL_HOST_PORT=13307 bash devops/scripts/port-check.sh
set -uo pipefail

MYSQL_HOST_PORT="${MYSQL_HOST_PORT:-13306}"
FAIL=0

say() { printf '%s\n' "$*"; }
ok() { printf '  [ok]   %s\n' "$*"; }
warn() { printf '  [warn] %s\n' "$*"; }
bad() { printf '  [FAIL] %s\n' "$*"; FAIL=1; }

say "== 1. 目标端口是否已被占用"
for port in "$MYSQL_HOST_PORT" 6379 80 443; do
  if ss -tln "sport = :${port}" 2>/dev/null | grep -q LISTEN; then
    case "$port" in
      6379) warn "宿主 ${port} 已被占用（如果不用 Redis 可忽略）" ;;
      80|443) warn "宿主 ${port} 已被占用，确认是你要用的 Nginx" ;;
      *) bad "宿主 ${port} 已被占用，换 MYSQL_HOST_PORT 或先停掉占用者" ;;
    esac
  else
    ok "宿主 ${port} 空闲"
  fi
done

say "== 2. 宿主机是否已装同名组件"
for svc in mysql mysqld mariadb redis redis-server nginx; do
  if systemctl list-unit-files "${svc}.service" >/dev/null 2>&1 \
    && systemctl is-active --quiet "${svc}" 2>/dev/null; then
    case "$svc" in
      mysql|mysqld|mariadb)
        warn "${svc} 正在运行。二选一：用它（改 DSN，不启容器）或停用它（systemctl disable --now ${svc}）"
        warn "  两个都跑 = 内存双份 + 备份漏一个 + 排查时连错库"
        ;;
      nginx)
        warn "宿主 nginx 正在运行，容器不要再发布 80/443"
        ;;
      *)
        warn "${svc} 正在运行"
        ;;
    esac
  fi
done
[ "$FAIL" -eq 0 ] && ok "未发现同名服务冲突（或已在上方说明）"

say "== 3. Docker 端口发布范围（只应出现 127.0.0.1）"
if command -v docker >/dev/null 2>&1; then
  published="$(docker ps --format '{{.Names}} {{.Ports}}' 2>/dev/null | grep -E '0\.0\.0\.0:|:::' || true)"
  if [ -n "$published" ]; then
    bad "以下容器把端口发布到了所有网卡："
    printf '%s\n' "$published" | sed 's/^/         /'
    warn "改成 127.0.0.1:宿主端口:容器端口"
  else
    ok "没有容器监听在 0.0.0.0"
  fi

  say "== 4. 是否挂在 docker.sock"
  if docker ps --format '{{.Names}} {{.Mounts}}' 2>/dev/null | grep -q docker.sock; then
    bad "有容器挂载了 docker.sock（等于宿主机 root 权限）"
  else
    ok "无容器挂载 docker.sock"
  fi
else
  warn "未安装 docker，跳过第 3、4 项"
fi

say "== 5. 对公网实际可达的端口（需要从外部验证，这里仅提示）"
say "  从开发机执行：nmap -Pn -p- --min-rate 2000 <本机公网IP>"
say "  期望：只有 22（且仅办公 IP）与 443"

say ""
if [ "$FAIL" -ne 0 ]; then
  say "结果：存在阻断项，先处理再 up"
  exit 1
fi
say "结果：预检通过"
