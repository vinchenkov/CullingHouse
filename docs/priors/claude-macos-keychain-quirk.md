# Prior: host-side `claude login` on macOS vs. container credential files (RECONSTRUCTED)

On macOS, Claude Code stores its OAuth in the **Keychain**, and a host-side
`claude login` can **delete `.credentials.json`** — silently invalidating a
file-based credential home a container depends on.

Consequence (spec §11.4, handoff S3): the container-side credential home for
the `claude` binding must be **its own canonical dir, never the operator's
`~/.claude`**. `claude setup-token` is the S3 mitigation candidate. Note
which Claude install method (native vs npm) is in use and pin it — the two
diverge on keychain behavior.
