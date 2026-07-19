# mc-fake-e2e — the walking skeleton's minimal base image
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

# Bun at the mise pin (§16.1).
RUN curl -fsSL https://bun.sh/install | bash -s "bun-v${BUN_VERSION}" \
 && ln -s /root/.bun/bin/bun /usr/local/bin/bun \
 && bun --version

# The bind-mounted workspace's apparent uid varies by Docker Desktop file
# sharing; in-container git must still read/write it (worktrees, mc-land).
RUN git config --system safe.directory '*'

# The public dispatcher keeps ordinary mc verbs at the calling uid. Only its
# sealed-completion arm enters the setuid completion wrapper; /mc/private is
# the kernel traversal gate that makes the mounted seal unreachable to uid
# 10002 even if Docker presents it as writable.
RUN groupadd --gid 10001 mc-completion \
 && useradd --uid 10001 --gid 10001 --no-create-home --shell /usr/sbin/nologin mc-completion \
 && groupadd --gid 10002 mc-model \
 && useradd --uid 10002 --gid 10002 --no-create-home --shell /usr/sbin/nologin mc-model \
 && install -d -o 10001 -g 10001 -m 0700 /mc/private \
 && install -d -o 10001 -g 10001 -m 0711 /usr/local/libexec
COPY mc /usr/local/libexec/mc-real
COPY mc-dispatch /usr/local/bin/mc
COPY mc-complete /usr/local/libexec/mc-complete
COPY mc-land /usr/local/bin/mc-land
RUN chown root:root /usr/local/libexec/mc-real /usr/local/bin/mc /usr/local/bin/mc-land \
 && chown mc-completion:mc-completion /usr/local/libexec/mc-complete \
 && chmod 0755 /usr/local/libexec/mc-real /usr/local/bin/mc /usr/local/bin/mc-land \
 && chmod 6755 /usr/local/libexec/mc-complete

# tini as PID 1 (§11.2): signal forwarding + zombie reaping for hang/reap.
ENTRYPOINT ["/usr/bin/tini", "--"]
