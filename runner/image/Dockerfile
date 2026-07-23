# Shared fake/production runner base. The fake build remains minimal; the
# production build flips RUNTIME_DEPS=1 to install the pinned real adapters.
# (docs/phase1b-contract.md §1 "The image"). Deliberately NOT
# mcspike-08-base: the fake family is deterministic, token-free, and
# networkless, so no harness CLIs, Node, or Playwright ride along.
#
# Built natively arm64, no --platform (AGENTS.md env facts; S8 pattern).
# Runner source is bind-mounted RO at /app/src at run time, never baked
# (spec §11.2); only mc and mc-land are baked (§11.2, §7 Landing).
FROM ubuntu:24.04

# Exact-version pins as ARGs (§11.2). GIT_MIN is a floor, enforced below:
# §6.2 relative worktree links need git >= 2.48 (ubuntu 24.04 ships 2.43,
# hence the git-core PPA).
ARG BUN_VERSION=1.3.9
ARG GIT_MIN=2.48
ARG TINI_VERSION=0.19.0
ARG RUNTIME_DEPS=0

RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      ca-certificates curl unzip gnupg software-properties-common \
 && add-apt-repository -y ppa:git-core/ppa \
 && apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      git "tini=${TINI_VERSION}*" \
 && rm -rf /var/lib/apt/lists/* \
 && ver="$(git --version | awk '{print $3}')" \
 && dpkg --compare-versions "$ver" ge "$GIT_MIN" \
 || { echo "git $ver < required $GIT_MIN (§6.2 relative worktrees)"; exit 1; }

# Bun at the mise pin (§16.1). Install under /opt (world-traversable) rather
# than the installer's default /root/.bun: a production Worker runs the runner
# as the model uid (10001/10002), and /root is 0700, so a /root-hosted runtime
# is unreachable to that uid ("exec bun failed: Permission denied").
RUN curl -fsSL https://bun.sh/install | BUN_INSTALL=/opt/bun bash -s "bun-v${BUN_VERSION}" \
 && ln -s /opt/bun/bin/bun /usr/local/bin/bun \
 && chmod -R a+rX /opt/bun \
 && bun --version

# The runner source remains bind-mounted at /app/src. Production-only package
# dependencies live one level above that mount so the mount cannot hide them;
# the fake image does not install or carry a harness CLI.
COPY runner/agent-runner/package.json runner/agent-runner/bun.lock /opt/mc-agent-runner/
RUN if [ "$RUNTIME_DEPS" = "1" ]; then \
      apt-get update \
      && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends bubblewrap socat \
      && rm -rf /var/lib/apt/lists/* \
      && cd /opt/mc-agent-runner \
      && bun install --production --frozen-lockfile \
      && install -d /app \
      && mv node_modules /app/node_modules \
      && chmod -R a+rX /app/node_modules; \
    elif [ "$RUNTIME_DEPS" != "0" ]; then \
      echo "RUNTIME_DEPS must be 0 or 1" >&2; exit 1; \
    fi
# The bind-mounted workspace's apparent uid varies by Docker Desktop file
# sharing; in-container git must still read/write it (worktrees, mc-land).
RUN git config --system safe.directory '*'

# The public dispatcher sends ordinary verbs through the real setuid mc gate.
# A privileged invocation pins identity to /mc/run.json in code, so the model
# cannot redirect the authorization envelope through its environment. The
# sealed-completion arm still uses its narrower publisher wrapper; /mc/private
# is the traversal gate that keeps the mounted seal unreachable to uid 10002.
RUN groupadd --gid 10001 mc-completion \
 && useradd --uid 10001 --gid 10001 --no-create-home --shell /usr/sbin/nologin mc-completion \
 && groupadd --gid 10002 mc-model \
 && useradd --uid 10002 --gid 10002 --no-create-home --shell /usr/sbin/nologin mc-model \
 && install -d -o 10001 -g 10001 -m 0700 /mc/private /mc/spine \
 && install -d -o 10001 -g 10001 -m 0711 /usr/local/libexec
# MC_BIN selects WHICH mc binary is baked, and it is the only difference
# between the fake-route e2e image and the production image (contract §8:
# "the production image contains no fake route and has its own untagged
# build"). Defaulting to `mc` keeps the existing e2e build unchanged.
ARG MC_BIN=mc
COPY runner/image/${MC_BIN} /usr/local/libexec/mc-real
COPY runner/image/mc-dispatch /usr/local/bin/mc
COPY runner/image/mc-complete /usr/local/libexec/mc-complete
COPY runner/image/mc-land /usr/local/bin/mc-land
RUN chown root:root /usr/local/bin/mc /usr/local/bin/mc-land \
 && chown mc-completion:mc-completion /usr/local/libexec/mc-real /usr/local/libexec/mc-complete \
 && chmod 0755 /usr/local/bin/mc /usr/local/bin/mc-land \
 && chmod 6755 /usr/local/libexec/mc-real /usr/local/libexec/mc-complete

# tini as PID 1 (§11.2): signal forwarding + zombie reaping for hang/reap.
ENTRYPOINT ["/usr/bin/tini", "--"]
