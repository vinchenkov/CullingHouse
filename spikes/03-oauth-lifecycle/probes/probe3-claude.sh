#!/usr/bin/env bash
# S3 probe 3 — claude OAuth refresh round-trip through the bind-mounted
# canonical dir (~/.mc-dev-home/cred/claude, mounted as CLAUDE_CONFIG_DIR).
#
# Refresh trigger: claude checks .credentials.json's expiresAt locally and
# proactively refreshes with refreshToken when past. So the forcing move is
# simply rewriting expiresAt into the past — no token tampering needed.
#
# LIVE: burns one tiny subscription turn per run (2 runs).
# Secrets discipline: never prints token values (first 8 chars max).
set -euo pipefail

CRED="${HOME}/.mc-dev-home/cred/claude"
STASH="${HOME}/.mc-dev-home/spike03"
IMG="mcspike-08-base:latest"
OUT="$(cd "$(dirname "$0")/.." && pwd)/out"
mkdir -p "$OUT" "$STASH"

[[ -f "$CRED/.credentials.json" ]] || { echo "no canonical claude .credentials.json"; exit 1; }

BK="$STASH/backup-claude-$(date +%s)"
mkdir -p "$BK"; cp -p "$CRED/.credentials.json" "$BK/"
echo "backup: $BK (host-side stash, outside repo)"

snap() { # label
  python3 - "$CRED/.credentials.json" "$1" <<'EOF'
import json,sys,hashlib,os,datetime
p,label=sys.argv[1],sys.argv[2]
raw=open(p,'rb').read()
o=json.loads(raw)['claudeAiOauth']
print(f"[{label}] sha256={hashlib.sha256(raw).hexdigest()[:16]} inode={os.stat(p).st_ino} "
      f"accessToken[:8]={o['accessToken'][:8]} refreshToken[:8]={o['refreshToken'][:8]} "
      f"expiresAt={datetime.datetime.fromtimestamp(o['expiresAt']/1000).isoformat()} "
      f"refreshExpiresAt={datetime.datetime.fromtimestamp(o['refreshTokenExpiresAt']/1000).isoformat()}")
EOF
}

expire_access() { # set expiresAt one hour into the past (atomic replace)
  python3 - "$CRED/.credentials.json" <<'EOF'
import json,sys,time,os,tempfile
p=sys.argv[1]
a=json.load(open(p))
a['claudeAiOauth']['expiresAt']=int((time.time()-3600)*1000)
fd,tmp=tempfile.mkstemp(dir=os.path.dirname(p))
with os.fdopen(fd,'w') as f: json.dump(a,f)
os.chmod(tmp,0o600); os.rename(tmp,p)
print("set expiresAt to now-1h")
EOF
}

run_turn() { # label
  local label="$1"
  echo "== claude -p turn ($label) =="
  docker run --rm \
    -v "$CRED":/claude-cfg \
    -e CLAUDE_CONFIG_DIR=/claude-cfg \
    -w /tmp \
    "$IMG" bash -c '
      # forbidden-env guard (Inv. 13)
      for v in ANTHROPIC_API_KEY CODEX_API_KEY OPENAI_API_KEY CLAUDE_CODE_OAUTH_TOKEN; do
        [ -n "${!v:-}" ] && { echo "FORBIDDEN ENV PRESENT: $v"; exit 90; }
      done
      claude -p "Reply with the single word OK and nothing else." 2>&1 | tail -5
      echo "exit=$?"
    '
}

snap "pre"
expire_access
snap "expired"

run_turn "run1-forced-refresh" | tee "$OUT/probe3-run1.log"
snap "post-run1"

run_turn "run2-clean-start" | tee "$OUT/probe3-run2.log"
snap "post-run2"

echo "== canonical dir contents after (names+sizes only) =="
ls -la "$CRED"

echo "PROBE3 done — refresh happened iff accessToken/sha/expiresAt changed after run1."
