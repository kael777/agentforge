# Static frontend image: build the bundle with Node, serve it with an
# unprivileged nginx. The final image contains no Node runtime, no source and no
# package manager.
#
# Build context is the repository root, because this image also needs the nginx
# config that lives under devops/.
FROM node:24-alpine@sha256:06d7adf3e0ef61a13f1d28682726366f97a15aa1888c934d3ecec86c33918b53 AS builder

WORKDIR /src
# Copy only what the lockfile install needs, so a source-only change does not
# invalidate the dependency layer.
COPY frontend/web/package.json frontend/web/package-lock.json ./
RUN npm ci
COPY frontend/web/ .
RUN npm run build

FROM nginxinc/nginx-unprivileged:1.29-alpine@sha256:0c79d56aee561a1d81c63f00eee5fb5fe29279560cdc55e91425133104c7fbe6

LABEL org.opencontainers.image.title="agentforge-web" \
      org.opencontainers.image.description="AgentForge React frontend" \
      org.opencontainers.image.source="https://github.com/kael777/agentforge"

# The image already runs as uid 101 (nginx) and listens on 8080.
COPY devops/nginx/web-static.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /src/dist /usr/share/nginx/html

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
