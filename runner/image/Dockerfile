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

# The two baked binaries: mc (linux/arm64, built by build.sh — §11.2 "baked,
# never bind-mounted") and mc-land (§7 Landing: "a script baked into the
# base image and versioned with it").
COPY mc /usr/local/bin/mc
COPY mc-land /usr/local/bin/mc-land
RUN chmod 0755 /usr/local/bin/mc /usr/local/bin/mc-land

# tini as PID 1 (§11.2): signal forwarding + zombie reaping for hang/reap.
ENTRYPOINT ["/usr/bin/tini", "--"]
