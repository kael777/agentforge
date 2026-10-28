#!/usr/bin/env python3
"""AgentForge WebSocket 端到端演示客户端。

它会依次完成：

    1. 拿到访问 token（注册或登录）
    2. 用 token 换一张 30 秒、单次使用的 WebSocket 票据
    3. 建连（必须带 Origin，服务端会校验）
    4. 订阅 topic -> 发布消息 -> 收到广播 -> 心跳
    5. 故意用一个非法 topic，演示错误帧
    6. 断开后重放同一张票据，确认被拒绝

用法：

    pip install "websockets>=12"
    python devops/scripts/ws-demo.py

可选参数：

    --api http://127.0.0.1:55555
    --ws  ws://127.0.0.1:8090/ws
    --origin http://localhost:5173
    --topic demo
"""

from __future__ import annotations

import argparse
import asyncio
import json
import sys
import urllib.error
import urllib.parse
import urllib.request

import websockets


def open_socket(uri: str, origin: str):
    """兼容不同版本的 websockets：v12+ 用 additional_headers，旧版用 extra_headers。"""
    return _open_socket(uri, origin)


async def _open_socket(uri: str, origin: str):
    try:
        return await websockets.connect(uri, additional_headers={"Origin": origin})
    except TypeError:
        return await websockets.connect(uri, extra_headers={"Origin": origin})


def post_json(url: str, payload: dict | None = None, token: str | None = None) -> dict:
    data = b"" if payload is None else json.dumps(payload).encode()
    request = urllib.request.Request(url, data=data, method="POST")
    request.add_header("Content-Type", "application/json")
    if token:
        request.add_header("Authorization", f"Bearer {token}")
    with urllib.request.urlopen(request, timeout=10) as response:
        body = response.read() or b"{}"
        return json.loads(body)


def section(title: str) -> None:
    print(f"\n== {title}")


def get_token(api: str, email: str, password: str) -> str:
    credentials = {"email": email, "password": password}
    section("准备账号")
    try:
        post_json(f"{api}/api/v1/auth/register", credentials)
        print(f"注册成功：{email}")
    except urllib.error.HTTPError as error:
        if error.code != 409:
            raise
        print(f"账号已存在，直接登录：{email}")

    result = post_json(f"{api}/api/v1/auth/login", credentials)
    token = result.get("token")
    if not token:
        raise RuntimeError("登录没有返回 token")
    print(f"拿到访问 token（{len(token)} 字符）")
    return token


def get_ticket(api: str, token: str) -> str:
    section("用 token 换 WebSocket 票据")
    result = post_json(f"{api}/api/v1/ws/ticket", token=token)
    print(f"票据有效期 {result.get('expires_in')} 秒，只能使用一次")
    return result["ticket"]


async def expect(socket, wanted: str, timeout: float = 5.0) -> dict:
    raw = await asyncio.wait_for(socket.recv(), timeout=timeout)
    frame = json.loads(raw)
    print(f"<- {json.dumps(frame, ensure_ascii=False)}")
    if frame.get("type") != wanted:
        raise RuntimeError(f"期望 {wanted}，实际收到 {frame.get('type')}")
    return frame


async def run(api: str, ws_url: str, origin: str, topic: str, email: str, password: str) -> None:
    token = get_token(api, email, password)
    ticket = get_ticket(api, token)

    uri = f"{ws_url}?ticket={urllib.parse.quote(ticket)}"
    section("建立连接")
    print(f"连接 {ws_url}，Origin: {origin}")
    # 服务端要求 Origin 必须命中白名单，缺失或伪造都会被 403 拒绝。
    socket = await open_socket(uri, origin)
    try:
        print("已连接")

        section(f"订阅 {topic}")
        await socket.send(json.dumps({"type": "subscribe", "topic": topic}))
        await expect(socket, "subscribed")

        section("发布消息（自己订阅了自己发的 topic，所以会收到广播）")
        payload = {"hello": "world", "from": "python"}
        await socket.send(json.dumps({"type": "publish", "topic": topic, "data": payload}))
        message = await expect(socket, "message")
        assert message["data"] == payload, f"广播内容不一致：{message['data']}"

        section("心跳")
        await socket.send(json.dumps({"type": "ping"}))
        await expect(socket, "pong")

        section("非法 topic 应当返回错误帧而不是断开")
        await socket.send(json.dumps({"type": "subscribe", "topic": "../etc/passwd"}))
        await expect(socket, "error")
    finally:
        section("关闭连接")
        await socket.close()
        print(f"关闭码 {socket.close_code}，原因 {socket.close_reason!r}")

    section("重放同一张票据")
    try:
        replay = await open_socket(uri, origin)
    except Exception as error:  # noqa: BLE001 - 演示脚本，任何拒绝都算通过
        print(f"按预期被拒绝：{type(error).__name__} {error}")
    else:
        await replay.close()
        raise RuntimeError("重放居然成功了，票据单次使用没有生效")


def main() -> int:
    # Windows 控制台默认不是 UTF-8，中文输出会变成乱码。
    if sys.platform == "win32":
        for stream in (sys.stdout, sys.stderr):
            try:
                stream.reconfigure(encoding="utf-8")
            except (AttributeError, ValueError):
                pass

    parser = argparse.ArgumentParser(description="AgentForge WebSocket 演示客户端")
    parser.add_argument("--api", default="http://127.0.0.1:55555")
    parser.add_argument("--ws", default="ws://127.0.0.1:8090/ws")
    parser.add_argument("--origin", default="http://localhost:5173")
    parser.add_argument("--topic", default="demo")
    parser.add_argument("--email", default="ws-demo@example.com")
    parser.add_argument("--password", default="password123")
    args = parser.parse_args()

    try:
        asyncio.run(run(args.api, args.ws, args.origin, args.topic, args.email, args.password))
    except KeyboardInterrupt:
        return 130
    except Exception as error:  # noqa: BLE001 - 顶层只有演示脚本
        print(f"\n失败：{type(error).__name__}: {error}", file=sys.stderr)
        return 1

    print("\nRESULT: PASS - 取票、鉴权、订阅、广播、心跳、错误处理与票据重放全部符合预期。")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
