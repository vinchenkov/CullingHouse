# S8 spike — representative Mission Control base image (spec §11.2 shape).
#
# Shape mandated by spec §11.2:
#   - harness CLIs at PINNED versions, pins as exact-version ARGs (artifact, not prose)
#   - Node and Bun
#   - git >= 2.48 (relative worktree links, spec §6.2)
#   - tini as PID 1
#   - Playwright browser layer kept SEPARATE and LAST so most rebuilds skip it
#   - (real image also bakes the setuid `mc` binary; out of scope for this spike —
#     the setuid gate is S1's probe, and `mc` does not exist yet)
#
# Built NATIVELY on an Apple Silicon host: no --platform flag anywhere, so the
# build can only produce the daemon's native arch (arm64) and never touches QEMU.

FROM ubuntu:24.04

# ---- the pins (exact versions, per spec §11.2) ------------------------------
ARG CLAUDE_CODE_VERSION=2.1.207
ARG CODEX_VERSION=0.144.1
ARG NODE_VERSION=22.17.1
ARG BUN_VERSION=1.3.9
ARG PLAYWRIGHT_VERSION=1.61.1

ENV DEBIAN_FRONTEND=noninteractive

# ---- layer 1: system packages + git >= 2.48 (git-core PPA; noble ships 2.43) --
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates curl gnupg unzip xz-utils tini software-properties-common \
 && add-apt-repository -y ppa:git-core/ppa \
 && apt-get update \
 && apt-get install -y --no-install-recommends git \
 && v="$(git --version | awk '{print $3}')" \
 && printf '%s\n%s\n' "2.48.0" "$v" | sort -V -C \
 && echo "git $v >= 2.48 OK" \
 && rm -rf /var/lib/apt/lists/*

# ---- layer 2: Node (pinned exact, official tarball, native arm64) ------------
RUN curl -fsSL "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-arm64.tar.xz" \
      -o /tmp/node.tar.xz \
 && tar -xJf /tmp/node.tar.xz -C /usr/local --strip-components=1 \
 && rm /tmp/node.tar.xz \
 && node --version | grep -qx "v${NODE_VERSION}"

# ---- layer 3: Bun (pinned exact, official release zip, native arm64) ---------
RUN curl -fsSL "https://github.com/oven-sh/bun/releases/download/bun-v${BUN_VERSION}/bun-linux-aarch64.zip" \
      -o /tmp/bun.zip \
 && unzip -q /tmp/bun.zip -d /tmp \
 && install -m 0755 /tmp/bun-linux-aarch64/bun /usr/local/bin/bun \
 && ln -sf /usr/local/bin/bun /usr/local/bin/bunx \
 && rm -rf /tmp/bun.zip /tmp/bun-linux-aarch64 \
 && bun --version | grep -qx "${BUN_VERSION}"

# ---- layer 4: the two runtime-harness CLIs at pinned versions ----------------
RUN npm install -g --no-fund --no-audit \
      "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" \
      "@openai/codex@${CODEX_VERSION}" \
 && claude --version \
 && codex --version

# ---- agent user (never root at runtime; real image adds privileged mc uid) ---
RUN useradd -m -u 1001 -s /bin/bash agent

# require('playwright') must resolve from the global install for any cwd
ENV NODE_PATH=/usr/local/lib/node_modules

# ---- layer 5 (LAST, kept separate per spec §11.2): Playwright + Chromium -----
# Shared browser path so the non-root agent user can use the root-installed browser.
ENV PLAYWRIGHT_BROWSERS_PATH=/opt/pw-browsers
RUN npm install -g --no-fund --no-audit "playwright@${PLAYWRIGHT_VERSION}" \
 && playwright install --with-deps chromium \
 && chmod -R a+rX "${PLAYWRIGHT_BROWSERS_PATH}" \
 && rm -rf /var/lib/apt/lists/*

LABEL mc-spike="08" mc-image-role="base-candidate"

# tini as PID 1 so signals are forwarded correctly (spec §11.2)
ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["/bin/bash"]
