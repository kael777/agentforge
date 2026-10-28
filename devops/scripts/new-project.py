#!/usr/bin/env python3
"""从本仓库的三个模板生成一个可直接运行的独立项目。

    python devops/scripts/new-project.py --template api --name my-service --target ../my-service
    python devops/scripts/new-project.py --template ws  --name my-realtime --target ../my-realtime
    python devops/scripts/new-project.py --template web --name my-web --target ../my-web

生成结果是自包含的：独立的 module / package 名字、自己的 Dockerfile、
自己的环境变量文件，不引用本仓库任何路径。

为什么用 Python 而不是 PowerShell + Bash 各写一份：生成器是纯文件操作，
两种 shell 各写一遍意味着两份逻辑迟早漂移。Python 3 是跨平台的，且本仓库
的 WebSocket 演示脚本已经依赖它。
"""

from __future__ import annotations

import argparse
import os
import re
import secrets
import shutil
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]

TEMPLATES = {
    "api": {
        "source": "backend/api",
        "kind": "go",
        "module": "github.com/kael777/agentforge/backend/api",
        "service": "agentforge-api",
        "description": "Go + Fiber + GORM + MySQL 后端（含安全基线）",
    },
    "ws": {
        "source": "backend/ws",
        "kind": "go",
        "module": "github.com/kael777/agentforge/backend/ws",
        "service": "agentforge-ws",
        "description": "Go WebSocket 实时服务（票据鉴权、连接限制、优雅关闭）",
    },
    "web": {
        "source": "frontend/web",
        "kind": "node",
        "module": None,
        "service": "agentforge-web",
        "description": "React + Vite 前端（路由、请求、校验、测试）",
    },
}

SKIP_DIRS = {".git", "node_modules", "bin", "dist", "__pycache__", ".vite"}
SKIP_FILES = {".env", "coverage.out"}
TEXT_SUFFIXES = {
    ".go", ".mod", ".sum", ".json", ".ts", ".tsx", ".js", ".jsx", ".md",
    ".yml", ".yaml", ".conf", ".example", ".html", ".css", ".service",
}


def fail(message: str) -> "NoReturn":  # type: ignore[name-defined]
    print(f"错误：{message}", file=sys.stderr)
    raise SystemExit(1)


def copy_tree(source: Path, target: Path) -> None:
    def ignore(directory: str, names: list[str]) -> set[str]:
        ignored = set()
        for name in names:
            if name in SKIP_DIRS or name in SKIP_FILES:
                ignored.add(name)
            elif name.startswith(".env.") and name != ".env.example":
                ignored.add(name)
        return ignored

    shutil.copytree(source, target, ignore=ignore)


def replace_in_tree(target: Path, replacements: list[tuple[str, str]]) -> None:
    """对文本文件做字面替换。二进制文件按后缀跳过。"""
    for path in target.rglob("*"):
        if not path.is_file() or path.suffix not in TEXT_SUFFIXES and path.name not in {
            "Dockerfile", "Makefile", ".gitignore", ".dockerignore", ".env.example",
        }:
            continue
        try:
            original = path.read_text(encoding="utf-8")
        except (UnicodeDecodeError, OSError):
            continue
        updated = original
        for old, new in replacements:
            updated = updated.replace(old, new)
        if updated != original:
            path.write_text(updated, encoding="utf-8", newline="")


def write_env(target: Path, template: str, name: str) -> None:
    """从 .env.example 生成 .env，并把占位密钥换成随机值。"""
    example = target / ".env.example"
    if not example.exists():
        return
    content = example.read_text(encoding="utf-8")
    if template == "api":
        content = content.replace(
            "replace-this-with-at-least-32-characters", secrets.token_urlsafe(36), 1
        )
        content = content.replace("dev-ws-ticket-secret-0123456789ab", secrets.token_urlsafe(36), 1)
    elif template == "ws":
        content = content.replace("dev-ws-ticket-secret-0123456789ab", secrets.token_urlsafe(36), 1)
    (target / ".env").write_text(content, encoding="utf-8", newline="")


def add_container_files(target: Path, template: str, module: str | None) -> None:
    """把 devops 里的镜像定义搬进生成的项目，并改写成本地路径。"""
    docker_dir = REPO_ROOT / "devops" / "docker"

    if template == "web":
        nginx_conf = (REPO_ROOT / "devops" / "nginx" / "web-static.conf").read_text(encoding="utf-8")
        (target / "nginx.conf").write_text(nginx_conf, encoding="utf-8", newline="")
        dockerfile = (docker_dir / "web.Dockerfile").read_text(encoding="utf-8")
        dockerfile = dockerfile.replace(
            "COPY frontend/web/package.json frontend/web/package-lock.json ./",
            "COPY package.json package-lock.json ./",
        ).replace("COPY frontend/web/ .", "COPY . .")
        dockerfile = dockerfile.replace(
            "COPY devops/nginx/web-static.conf /etc/nginx/conf.d/default.conf",
            "COPY nginx.conf /etc/nginx/conf.d/default.conf",
        )
        dockerfile = dockerfile.replace(
            "# Build context is the repository root, because this image also needs the nginx\n"
            "# config that lives under devops/.\n",
            "# Build context is the project root.\n",
        )
        (target / "Dockerfile").write_text(dockerfile, encoding="utf-8", newline="")
        (target / ".dockerignore").write_text(
            "node_modules\ndist\n.env\n.env.*\n!.env.example\n.git\n", encoding="utf-8", newline=""
        )
        return

    source = docker_dir / f"{template}.Dockerfile"
    dockerfile = source.read_text(encoding="utf-8")
    if module:
        dockerfile = dockerfile.replace(TEMPLATES[template]["module"], module)
    (target / "Dockerfile").write_text(dockerfile, encoding="utf-8", newline="")


def init_git(target: Path, name: str) -> bool:
    if shutil.which("git") is None:
        return False
    subprocess.run(["git", "init", "-q"], cwd=target, check=True)
    subprocess.run(["git", "add", "-A"], cwd=target, check=True)
    subprocess.run(["git", "commit", "-q", "-m", f"chore: {name} 由 AgentForge 模板生成"], cwd=target, check=True)
    return True


def main() -> int:
    parser = argparse.ArgumentParser(description="从 AgentForge 模板生成独立项目")
    parser.add_argument("--template", required=True, choices=sorted(TEMPLATES))
    parser.add_argument("--name", required=True, help="项目/服务名，例如 my-service")
    parser.add_argument("--target", required=True, help="输出目录，必须不存在或为空")
    parser.add_argument("--module", help="Go module path，默认 github.com/example/<name>")
    parser.add_argument("--init-git", action="store_true", help="初始化 git 并提交首个版本")
    args = parser.parse_args()

    spec = TEMPLATES[args.template]
    if not re.fullmatch(r"[a-z0-9][a-z0-9-]{1,40}", args.name):
        fail("--name 只允许小写字母、数字和连字符，长度 2-41")

    module = None
    if spec["kind"] == "go":
        module = args.module or f"github.com/example/{args.name}"
        if not re.fullmatch(r"[\w.\-/]+", module):
            fail("--module 不是合法的 Go module path")

    source = REPO_ROOT / spec["source"]
    if not source.is_dir():
        fail(f"模板源目录不存在：{source}")

    target = Path(args.target).resolve()
    if target.exists() and any(target.iterdir()):
        fail(f"目标目录非空：{target}")

    print(f"生成 {args.template} 模板 → {target}")
    print(f"  来源：{spec['source']}")
    print(f"  名称：{args.name}")
    if module:
        print(f"  module：{module}")

    copy_tree(source, target)

    # 只做整词替换：'kael777/agentforge/backend/api' 这种前缀片段必须整体匹配，
    # 否则会把 github.com/ 也替换掉，拼出 github.com/github.com/... 这种路径。
    replacements: list[tuple[str, str]] = []
    if spec["module"] and module:
        replacements.append((spec["module"], module))
    replacements.append((spec["service"], args.name))
    replace_in_tree(target, [pair for pair in replacements if pair[0]])

    write_env(target, args.template, args.name)
    add_container_files(target, args.template, module)

    if args.init_git and init_git(target, args.name):
        print("  已初始化 git 并提交")

    print("\n下一步：")
    if args.template == "web":
        print(f"  cd {target}")
        print("  npm ci && npm run dev")
        print("  docker build -t " + args.name + " .")
    else:
        print(f"  cd {target}")
        print("  go mod tidy && go test ./... && go run ./cmd/" + args.template)
        print("  docker build -t " + args.name + " .")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
