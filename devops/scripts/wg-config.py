#!/usr/bin/env python3
"""从节点清单生成 hub-and-spoke 的 WireGuard 配置。

为什么需要它：手工维护 `wg0.conf` 在 3 台时没问题，到第 5 台就开始出错——
`tee > wg0.conf` 是覆盖写，漏写一段就等于把那台 peer 删掉。

    # peers.csv
    name,kind,address,public_key,public_ip
    hub,hub,10.88.0.1,<hub 公钥>,203.0.113.10
    app-a,app,10.88.0.11,<A 公钥>,
    app-b,app,10.88.0.12,<B 公钥>,

    python devops/scripts/wg-config.py --inventory peers.csv --outdir ./wg-out

生成 `hub.wg0.conf` 和每台节点的 `<name>.wg0.conf`。

脚本只处理**公钥**与地址，不会读取或写入任何私钥；配置里用
`$(cat /etc/wireguard/private.key)` 占位，私钥始终留在目标机器上。
"""

from __future__ import annotations

import argparse
import base64
import csv
import ipaddress
import re
import sys
from pathlib import Path

REQUIRED_COLUMNS = {"name", "kind", "address", "public_key"}
KINDS = ("hub", "app", "dev", "temp")
# infra 是 hub 的别名，两种写法都接受
KIND_ALIASES = {"infra": "hub", "hub": "hub", "app": "app", "dev": "dev", "temp": "temp"}

# 地址分段：与 plan_step1.md 的约定一致
SEGMENTS = {
    "hub": (1, 1),
    "app": (11, 19),
    "dev": (21, 29),
    "temp": (31, 39),
}

NAME_PATTERN = re.compile(r"^[a-z0-9][a-z0-9-]{0,31}$")


class InventoryError(Exception):
    pass


def load_inventory(path: Path) -> list[dict]:
    if not path.is_file():
        raise InventoryError(f"清单文件不存在：{path}")

    with path.open(encoding="utf-8-sig", newline="") as handle:
        reader = csv.DictReader(handle)
        if reader.fieldnames is None:
            raise InventoryError("清单为空")

        missing = REQUIRED_COLUMNS - {name.strip() for name in reader.fieldnames}
        if missing:
            raise InventoryError(f"清单缺少列：{', '.join(sorted(missing))}")

        peers = []
        for line, row in enumerate(reader, start=2):
            if not any((value or "").strip() for value in row.values()):
                continue  # 跳过空行
            peer = {key: (value or "").strip() for key, value in row.items() if key}
            peer["_line"] = line
            peer["kind"] = KIND_ALIASES.get(peer["kind"], peer["kind"])
            peers.append(peer)

    if not peers:
        raise InventoryError("清单里没有任何节点")
    return peers


def validate(peers: list[dict], subnet: ipaddress.IPv4Network) -> tuple[list[str], list[str]]:
    errors: list[str] = []
    warnings: list[str] = []

    hubs = [peer for peer in peers if peer["kind"] == "hub"]
    if len(hubs) != 1:
        errors.append(f"必须恰好有一个 kind=hub 的节点，当前 {len(hubs)} 个")

    seen_names: dict[str, int] = {}
    seen_addresses: dict[str, str] = {}
    seen_keys: dict[str, str] = {}

    for peer in peers:
        line = peer["_line"]
        name = peer["name"]
        where = f"第 {line} 行（{name or '未命名'}）"

        if not NAME_PATTERN.match(name):
            errors.append(f"{where}：name 只允许小写字母、数字和连字符，长度 1-32")
            continue
        if name in seen_names:
            errors.append(f"{where}：name 与第 {seen_names[name]} 行重复")
        seen_names[name] = line

        if peer["kind"] not in KINDS:
            errors.append(f"{where}：kind 必须是 {' / '.join(KINDS)}")

        try:
            address = ipaddress.IPv4Address(peer["address"])
        except ValueError:
            errors.append(f"{where}：address 不是合法 IPv4：{peer['address']!r}")
            continue
        if address not in subnet:
            errors.append(f"{where}：address {address} 不在 {subnet} 内")
            continue

        if str(address) in seen_addresses:
            errors.append(f"{where}：address {address} 与 {seen_addresses[str(address)]} 冲突")
        seen_addresses[str(address)] = name

        low, high = SEGMENTS.get(peer["kind"], (0, 255))
        if not low <= int(str(address).split(".")[-1]) <= high:
            warnings.append(
                f"{where}：{peer['kind']} 建议使用 .{low}-.{high} 段，当前是 .{str(address).split('.')[-1]}"
            )

        key = peer["public_key"]
        if not is_wireguard_key(key):
            errors.append(f"{where}：public_key 不是合法的 WireGuard 公钥（应为 44 字符 base64）")
        elif key in seen_keys:
            errors.append(f"{where}：public_key 与 {seen_keys[key]} 重复")
        else:
            seen_keys[key] = name

        if peer["kind"] == "hub":
            public_ip = peer.get("public_ip", "")
            if not public_ip:
                errors.append(f"{where}：hub 必须填 public_ip，spoke 要用它做 Endpoint")
            elif not is_ip(public_ip):
                errors.append(f"{where}：public_ip 不是合法 IP：{public_ip!r}")
        else:
            peer.setdefault("public_ip", "")

    return errors, warnings


def is_wireguard_key(value: str) -> bool:
    if len(value) != 44 or not value.endswith("="):
        return False
    try:
        return len(base64.b64decode(value, validate=True)) == 32
    except Exception:  # noqa: BLE001 - 任何解码失败都算不合法
        return False


def is_ip(value: str) -> bool:
    try:
        ipaddress.ip_address(value)
        return True
    except ValueError:
        return False


def render_hub(peers: list[dict], hub: dict, subnet: ipaddress.IPv4Network, port: int) -> str:
    lines = [
        "# 由 devops/scripts/wg-config.py 生成，不要手改。",
        "# 改清单后重新生成，再用 wg syncconf 热加载：",
        "#   sudo wg syncconf wg0 <(wg-quick strip wg0)",
        "",
        "[Interface]",
        f"Address    = {hub['address']}/{subnet.prefixlen}",
        f"ListenPort = {port}",
        "PrivateKey = $(cat /etc/wireguard/private.key)",
        "PostUp   = sysctl -w net.ipv4.ip_forward=1",
        "PostDown = sysctl -w net.ipv4.ip_forward=0",
    ]

    for peer in peers:
        if peer["kind"] == "hub":
            continue
        lines += [
            "",
            "[Peer]",
            f"# {peer['name']}（{peer['kind']}）",
            f"PublicKey  = {peer['public_key']}",
            f"AllowedIPs = {peer['address']}/32",
        ]
    return "\n".join(lines) + "\n"


def render_spoke(hub: dict, peer: dict, subnet: ipaddress.IPv4Network, port: int) -> str:
    return "\n".join([
        "# 由 devops/scripts/wg-config.py 生成，不要手改。",
        "",
        "[Interface]",
        f"Address    = {peer['address']}/{subnet.prefixlen}",
        "PrivateKey = $(cat /etc/wireguard/private.key)",
        "",
        "[Peer]",
        f"PublicKey  = {hub['public_key']}",
        f"Endpoint   = {hub['public_ip']}:{port}",
        f"AllowedIPs = {subnet}",
        "PersistentKeepalive = 25",
    ]) + "\n"


def print_table(peers: list[dict], subnet: ipaddress.IPv4Network) -> None:
    print(f"节点分布（网段 {subnet}）：")
    print(f"  {'name':<12} {'kind':<6} {'address':<14} {'public_ip':<16}")
    for peer in peers:
        print(f"  {peer['name']:<12} {peer['kind']:<6} {peer['address']:<14} {peer.get('public_ip',''):<16}")


def main() -> int:
    parser = argparse.ArgumentParser(description="从清单生成 WireGuard hub/spoke 配置")
    parser.add_argument("--inventory", required=True, help="CSV 清单路径")
    parser.add_argument("--outdir", required=True, help="输出目录")
    parser.add_argument("--subnet", default="10.88.0.0/24", help="隧道网段，默认 10.88.0.0/24")
    parser.add_argument("--port", type=int, default=51820, help="hub 监听端口，默认 51820")
    parser.add_argument("--strict", action="store_true", help="把警告也当作错误")
    args = parser.parse_args()

    try:
        subnet = ipaddress.IPv4Network(args.subnet, strict=False)
        peers = load_inventory(Path(args.inventory))
        errors, warnings = validate(peers, subnet)
    except InventoryError as error:
        print(f"错误：{error}", file=sys.stderr)
        return 1

    for warning in warnings:
        print(f"警告：{warning}", file=sys.stderr)
    if errors or (args.strict and warnings):
        for error in errors:
            print(f"错误：{error}", file=sys.stderr)
        return 1

    hub = next(peer for peer in peers if peer["kind"] == "hub")
    outdir = Path(args.outdir)
    outdir.mkdir(parents=True, exist_ok=True)

    hub_path = outdir / "hub.wg0.conf"
    hub_path.write_text(render_hub(peers, hub, subnet, args.port), encoding="utf-8", newline="\n")

    written = [hub_path.name]
    for peer in peers:
        if peer["kind"] == "hub":
            continue
        path = outdir / f"{peer['name']}.wg0.conf"
        path.write_text(render_spoke(hub, peer, subnet, args.port), encoding="utf-8", newline="\n")
        written.append(path.name)

    print_table(peers, subnet)
    print()
    print(f"已生成 {len(written)} 个配置到 {outdir}：")
    for name in written:
        print(f"  {name}")
    print()
    print("部署提示：")
    print("  1. 配置里 PrivateKey 是 $(cat ...) 占位，直接 tee 到 /etc/wireguard/wg0.conf 即可")
    print("  2. hub 改动后用 wg syncconf 热加载，不要 systemctl restart")
    print("  3. 每新增一台，安全组要双向放行 UDP " + str(args.port))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
