# Step 1：跨服务商 WireGuard 组网与服务器基线

> 日期：2026-10-29
> 环境：三台 Ubuntu 24.04 VPS，分属不同服务商，各有公网 IP，**没有共享内网**
> 结果：完成，外部扫描只剩 22，陌生来源不可达，隧道 41 ms / 0% 丢包

## 1. 起点与目标

三台机器不在同一家云，拿不到共享内网。任何服务间通信都得走公网。

原方案是给监控端口（Loki 写入、Prometheus remote write）开公网 + basic auth。
问题是**凭据在公网上是明文传输的**，而且每台机器都要单独维护另外两台的 IP。

所以第一步不是装软件，是造一张自己掌控的虚拟网，然后在这张网上做 SSH 与防火墙基线。

## 2. 为什么选 WireGuard

| | WireGuard | Tailscale |
|---|---|---|
| 控制面 | 自己配 | 第三方协调服务器（闭源） |
| 数据面 | WireGuard | WireGuard |
| 加机器 | 手改配置 | 登录即可 |
| 依赖 | 无 | 控制面不可用则新连接建不起来 |

这个项目要演示运维基建，每一层都该看得见：密钥交换、AllowedIPs 语义、握手、转发，
出问题能一层层查下去，而不是被客户端藏起来。代价是加机器要手改配置——但只有三台。

## 3. 拓扑与地址规划

```
        应用 A                应用 B
            \                  /
         UDP 51820（加密隧道）
                \            /
              基建中心（hub）
```

| 角色 | 隧道地址 | 说明 |
|---|---|---|
| 基建 hub | `<hub>` | 唯一在公网监听 UDP 51820 |
| 应用 A | `<app-a>` | 含数据库 |
| 应用 B | `<app-b>` | |
| 开发机 | `<dev>` | 可选，road warrior |

网段固定 `10.88.0.0/24`。星形而非网状：只有 hub 需要稳定的公网入口，
A/B 只要主动连出去。加第四台机器时，只在 hub 追加一段 `[Peer]`，已有机器的配置一行不改。

## 4. 阶段一：WireGuard

### 4.1 原理（先看懂再动手）

**公钥即身份。** WireGuard 没有"用户名密码"，每个 peer 用一对 Curve25519 密钥标识自己。
配置里出现的永远是别人的**公钥**，自己的私钥只留在本机。

**握手是自动的。** 没有流量时每 2 分钟重新协商，有流量时也会定期刷新。
所以判断通不通，看 `wg show` 里的 `latest handshake` 时间戳，而不是看进程在不在跑。

**`AllowedIPs` 有两个含义**，这是最容易误解的地方：

1. **出方向**：目标地址落在这个范围内 → 走这条隧道
2. **入方向**：只接受源地址在该范围内的包（类似反向防火墙）

它同时是路由表和访问控制。写 `0.0.0.0/0` 意味着"所有流量都从这条隧道出去"，
会把云主机的默认网关顶掉，SSH 当场断线。

**`Endpoint` 决定谁主动。** 有 `Endpoint` 的一方会发起握手，没有的一方只能等。
hub 的 `[Peer]` 段里没有 `Endpoint`——它不知道 A/B 在哪，也就不可能主动连它们。
所以只有"被连接方"需要开放入站端口。

**`PersistentKeepalive` 用于保活。** 家用宽带和云 NAT 会回收 UDP 映射表，
25 秒一个空包可以把这个映射保持住。

### 4.2 生成密钥

三台相同：

```bash
sudo apt update && sudo apt install -y wireguard wireguard-tools

umask 077
wg genkey | sudo tee /etc/wireguard/private.key | wg pubkey | sudo tee /etc/wireguard/public.key
```

`umask 077` 保证生成文件是 `600`。私钥不出机器，只需要把**公钥**交给 hub。

### 4.3 hub 配置

```bash
sudo tee /etc/wireguard/wg0.conf >/dev/null <<EOF
[Interface]
Address    = <hub>/24
ListenPort = 51820
PrivateKey = $(sudo cat /etc/wireguard/private.key)
PostUp   = sysctl -w net.ipv4.ip_forward=1
PostDown = sysctl -w net.ipv4.ip_forward=0

[Peer]
# 应用 A
PublicKey  = <A 的公钥>
AllowedIPs = <app-a>/32

[Peer]
# 应用 B
PublicKey  = <B 的公钥>
AllowedIPs = <app-b>/32
EOF
sudo chmod 600 /etc/wireguard/wg0.conf
sudo systemctl enable --now wg-quick@wg0
```

三个细节：

- 私钥用 `$(sudo cat ...)` 内联，避免私钥进命令历史或聊天记录。
  **必须是 `sudo cat`**：`private.key` 是 root 600，普通用户 `cat` 会失败并把空值写进配置。
- `ip_forward=1` 是给 A↔B 转发用的：它们互不相邻，流量要经 hub。
- `AllowedIPs` 用 `/32`，精确到单个地址。

这里的 heredoc 用 `<<EOF`（**不带引号**），因为需要 `$(...)` 展开。

### 4.4 spoke 配置（A / B 各一份）

```bash
sudo tee /etc/wireguard/wg0.conf >/dev/null <<EOF
[Interface]
Address    = <app-a>/24
PrivateKey = $(sudo cat /etc/wireguard/private.key)

[Peer]
PublicKey  = <hub 的公钥>
Endpoint   = <hub 公网 IP>:51820
AllowedIPs = 10.88.0.0/24
PersistentKeepalive = 25
EOF
sudo chmod 600 /etc/wireguard/wg0.conf
sudo systemctl enable --now wg-quick@wg0
```

`AllowedIPs = 10.88.0.0/24` 而不是 `/32`：A 要能访问 B，整个网段都走隧道才是正确的路由。

### 4.5 安全组

| 机器 | 允许入站 UDP 51820 来自 |
|---|---|
| hub | 应用 A、B 的公网 IP |
| 应用 A / B | hub 的公网 IP |

**只有 hub 必需**（回包由 conntrack 放行）。

### 4.6 可能碰到的问题

| 现象 | 原因 | 排查 |
|---|---|---|
| `latest handshake` 一直空 | 安全组协议选成 TCP | `sudo tcpdump -ni any udp port 51820`，没包进来就是被挡在主机外 |
| 有握手但 ping 不通 | 本机防火墙或路由 | `ip addr show wg0`、`ip route` |
| A 通 hub 通不到 B | hub 没开转发，或 ufw FORWARD 是 DROP | `sysctl net.ipv4.ip_forward` |
| 延迟高 | 跨区域 | 无法优化，只能接受并据此设计 |
| ping 公网 IP 不通 | 很多云默认封 ICMP | 正常，必须 ping 隧道地址 |

## 5. 阶段二：SSH 加固

### 5.1 原理

**公钥认证代替密码。** 客户端用私钥签名挑战，服务端用 `authorized_keys` 里的公钥验证。
私钥不出客户端，所以即使服务器上的 `authorized_keys` 泄漏，攻击者也拿不到能用的凭据。

**`sshd_config.d/` 是 drop-in 目录。** Ubuntu 的 `/etc/ssh/sshd_config` 开头有
`Include /etc/ssh/sshd_config.d/*.conf`，且 **Include 在最前面**。OpenSSH 的规则是
"第一次出现的值生效"，所以 drop-in 里的配置覆盖主文件后面的同名项。
把加固写进独立文件，不动主配置，系统升级不会覆盖。

**`sshd -t` 是语法检查。** 直接 restart 时如果配置有错，sshd 会继续跑旧配置或者不启动，
你会以为改生效了其实没有。

### 5.2 创建用户与推送公钥

```bash
sudo adduser --disabled-password --gecos "" deploy
sudo usermod -aG sudo deploy
sudo install -d -m 700 -o deploy -g deploy /home/deploy/.ssh
```

`--disabled-password` 创建的是没有密码的账号，只能靠密钥登录。
`.ssh` 必须是 `700`，否则 sshd 会拒绝读取 `authorized_keys`。

开发机上：

```bash
ssh-keygen -t ed25519 -C "agentforge-deploy" -f ~/.ssh/agentforge_deploy
ssh-copy-id -i ~/.ssh/agentforge_deploy.pub deploy@<hub 公网 IP>
ssh -i ~/.ssh/agentforge_deploy deploy@<hub 公网 IP> 'sudo -n true && echo sudo-ok'
```

最后一条同时验证两件事：密钥能登录、`deploy` 能免密 sudo。
**这一步失败时不要继续加固 SSH。**

用 ed25519 而不是 RSA：更短更快，没有 RSA 的参数选择陷阱。

### 5.3 加固配置

**先开两个 SSH 会话**，一个连着不动，另一个改配置。

```bash
sudo tee /etc/ssh/sshd_config.d/10-hardening.conf >/dev/null <<'EOF'
PermitRootLogin no
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication yes
AllowUsers deploy
MaxAuthTries 3
ClientAliveInterval 300
ClientAliveCountMax 2
EOF

sudo sshd -t && sudo systemctl reload ssh
```

| 项 | 作用 |
|---|---|
| `PermitRootLogin no` | 根账号连尝试机会都不给 |
| `PasswordAuthentication no` | 关掉密码登录，杜绝爆破 |
| `KbdInteractiveAuthentication no` | 某些 PAM 配置会绕过上一条 |
| `AllowUsers deploy` | 白名单，其他账号有密钥也进不来 |
| `MaxAuthTries 3` | 限制单连接尝试次数 |
| `ClientAliveInterval/CountMax` | 清理挂死会话，10 分钟无响应断开 |

这里用 `<<'EOF'`（**带引号**），要的是字面量，不能做变量展开。

reload 后用**新窗口**验证 `deploy` 能登录，确认无误再关旧会话。

### 5.4 可能碰到的问题

| 现象 | 原因 | 处理 |
|---|---|---|
| 改完 reload 后连不上 | 密钥没生效就关了密码 | 用另一会话改回去；云控制台 VNC 兜底 |
| `Permission denied (publickey)` | `.ssh` 或 `authorized_keys` 权限不对 | `chmod 700 ~/.ssh; chmod 600 ~/.ssh/authorized_keys` |
| `sshd -t` 报错 | 配置语法错 | 看行号；`sudo sshd -T` 打印最终生效配置 |
| reload 成功但仍要密码 | drop-in 被别的文件覆盖 | `sudo sshd -T \| grep passwordauth` 看实际值 |
| 登录后 sudo 要密码 | sudoers 没配 | 加 `/etc/sudoers.d/deploy`，用 `visudo -c` 校验 |

## 6. 阶段三：ufw

### 6.1 原理

**默认拒绝 + 显式放行。** `ufw default deny incoming` 之后，没写规则的入站一律丢弃。

**有状态。** ufw 底层是 conntrack：自己发起的连接，回包自动放行。所以 A/B 不用为
WireGuard 开 51820——它们只是发起方。

**`allow` 和 `route` 是两条不同的链。** `ufw allow` 作用于 INPUT（到本机）；
`ufw route allow in on wg0 out on wg0` 作用于 FORWARD（经过本机转发）。
ufw 默认 FORWARD 策略是 **DROP**，所以即使放行了 51820 入站，
A 发往 B 的包在 hub 上解开后仍会被丢掉。这就是"能通 hub、通不了 B"的原因。

**顺序**：先放行 22，再 `enable`。反了就是当场失联。

### 6.2 三台通用

```bash
sudo apt install -y ufw
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow from <办公出口 IP> to any port 22 proto tcp comment 'ops'
sudo ufw allow from 10.88.0.0/24 to any port 22 proto tcp comment 'ssh over wg'
sudo ufw --force enable
sudo ufw status numbered
```

第二条的意义：出门在外时走隧道 SSH，不依赖办公 IP。

### 6.3 hub 额外

```bash
sudo ufw allow 51820/udp comment 'wireguard'
sudo ufw route allow in on wg0 out on wg0
```

第一条让握手包到达 WireGuard 进程；第二条让解开后的包能转发给另一个 peer。

### 6.4 可能碰到的问题

| 现象 | 原因 |
|---|---|
| `enable` 后 SSH 断 | 22 规则还没加，或来源 IP 写错 |
| `ufw status` 里 22 显示 `Anywhere` | 忘了 `from ...`，等于对全网开放 |
| 隧道 ping 突然不通 | hub 的 `route allow` 缺失，或被 `ufw reset` 清掉 |
| 加了规则仍不通 | 规则顺序问题，`ufw status numbered` 看编号，用 `insert` 插到前面 |

## 7. 验收

```bash
# 1. 外部端口扫描（开发机）
nmap -Pn -p- --min-rate 2000 <各机器公网 IP>     # 期望：只有 22

# 2. 陌生来源（手机热点）
nc -vz <公网 IP> 22                              # 期望：超时

# 3. 规则是精确的（三台各跑）
sudo ufw status numbered                          # 期望：22 来源是具体 IP，不是 Anywhere

# 4. 隧道可用
ping -c 3 <hub>        # 从 A/B
ping -c 3 <app-b>      # 从 A，验证 hub 转发
```

| 项 | 结果 |
|---|---|
| 外部扫描 | 只看到 22 |
| 手机热点访问 22 | 全部超时 |
| `ufw status` | 22 来源为具体 IP |
| `ping <hub>` | 通，41 ms，0% 丢包 |
| `ping <app-b>` | 通（经 hub 转发） |

## 8. 踩的坑（实际发生的）

| 坑 | 现象 | 原因 |
|---|---|---|
| 安全组协议选成 TCP | `ping <hub>` 超时，`latest handshake` 为空 | WireGuard 走 UDP 51820，云控制台的协议下拉框默认是 TCP，界面看起来"已放行" |

判断方法：hub 上 `sudo tcpdump -ni any udp port 51820`，同时从 A ping 隧道地址。
**一个包都没有**说明被挡在主机之外，不是配置问题。

## 9. 41 ms 带来的一个发现

这个延迟说明 A 和 hub 不在同一区域，原计划"两台应用机同 VPC"的假设不成立。

于是 B 访问数据库变成 `B → hub → A`，往返约 80 ms。一个请求查三次库就是 240 ms 纯等待。

处理方向（待定）：**A 对外服务，B 只做发布演练**——流量集中在 A（数据库走回环），
B 用来演练切换和回滚，不承担日常流量。

## 10. 遗留

- 开发机加入网络（road warrior）暂未配置
- 数据库未部署，`B → A:3306` 的连通性要等 Step 3 验证
- 41 ms 对日志推送可接受，但 Loki 的批量与压缩参数要按这个延迟调
- 未配置 fail2ban：22 端口已限 IP，爆破面很小，暂不加
