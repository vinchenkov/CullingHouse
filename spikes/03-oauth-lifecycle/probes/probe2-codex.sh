#!/usr/bin/env bash
# S3 probe 2 — codex OAuth refresh round-trip through the bind-mounted
# canonical dir (~/.mc-dev-home/cred/codex, mounted as CODEX_HOME).
#
# LIVE: burns one tiny subscription turn per codex exec (2 runs).
# Secrets discipline: never prints token values (first 8 chars max).
# A timestamped backup of the canonical dir is stashed under
# ~/.mc-dev-home/spike03/ before anything is touched.
set -euo pipefail

CRED="${HOME}/.mc-dev-home/cred/codex"
STASH="${HOME}/.mc-dev-home/spike03"
IMG="mcspike-08-base:latest"
OUT="$(cd "$(dirname "$0")/.." && pwd)/out"
mkdir -p "$OUT" "$STASH"

[[ -f "$CRED/auth.json" ]] || { echo "no canonical codex auth.json"; exit 1; }

BK="$STASH/backup-codex-$(date +%s)"
mkdir -p "$BK"; cp -p "$CRED/auth.json" "$BK/"
echo "backup: $BK (host-side stash, outside repo)"

snap() { # label
  python3 - "$CRED/auth.json" "$1" <<'EOF'
import json,sys,hashlib,os,base64,datetime
p,label=sys.argv[1],sys.argv[2]
raw=open(p,'rb').read()
a=json.loads(raw)
tok=a['tokens']['access_token']
pay=tok.split('.')[1]; pay+='='*(-len(pay)%4)
exp=json.loads(base64.urlsafe_b64decode(pay)).get('exp',0)
print(f"[{label}] sha256={hashlib.sha256(raw).hexdigest()[:16]} inode={os.stat(p).st_ino} "
      f"access_token[:8]={tok[:8]} refresh_token[:8]={a['tokens']['refresh_token'][:8]} "
      f"last_refresh={a['last_refresh']} access_exp={datetime.datetime.utcfromtimestamp(exp).isoformat()}Z")
EOF
}

age_last_refresh() { # push last_refresh 60 days into the past (atomic replace)
  python3 - "$CRED/auth.json" <<'EOF'
import json,sys,datetime,os,tempfile
p=sys.argv[1]
a=json.load(open(p))
a['last_refresh']=(datetime.datetime.utcnow()-datetime.timedelta(days=60)).strftime('%Y-%m-%dT%H:%M:%S.%fZ')
fd,tmp=tempfile.mkstemp(dir=os.path.dirname(p))
with os.fdopen(fd,'w') as f: json.dump(a,f)
os.chmod(tmp,0o600); os.rename(tmp,p)
print("aged last_refresh to", a['last_refresh'])
EOF
}

expire_tokens() { # rewrite exp claim to the past in access_token + id_token
  # (invalidates the signature; the server 401s; codex must take its
  # refresh path with the still-valid refresh_token)
  python3 - "$CRED/auth.json" <<'EOF'
import json,sys,base64,time,os,tempfile
p=sys.argv[1]
a=json.load(open(p))
def expire(tok):
    h,pay,sig=tok.split('.')
    pad=pay+'='*(-len(pay)%4)
    c=json.loads(base64.urlsafe_b64decode(pad))
    c['exp']=int(time.time())-3600
    np=base64.urlsafe_b64encode(json.dumps(c).encode()).decode().rstrip('=')
    return f"{h}.{np}.{sig}"
a['tokens']['access_token']=expire(a['tokens']['access_token'])
a['tokens']['id_token']=expire(a['tokens']['id_token'])
fd,tmp=tempfile.mkstemp(dir=os.path.dirname(p))
with os.fdopen(fd,'w') as f: json.dump(a,f)
os.chmod(tmp,0o600); os.rename(tmp,p)
print("rewrote exp to now-3600 in access_token and id_token")
EOF
}

run_turn() { # label
  local label="$1"
  echo "== codex exec turn ($label) =="
  docker run --rm \
    -v "$CRED":/codex-home \
    -e CODEX_HOME=/codex-home \
    -w /tmp \
    "$IMG" bash -c '
      # forbidden-env guard (Inv. 13)
      for v in ANTHROPIC_API_KEY CODEX_API_KEY OPENAI_API_KEY; do
        [ -n "${!v:-}" ] && { echo "FORBIDDEN ENV PRESENT: $v"; exit 90; }
      done
      codex exec --skip-git-repo-check --sandbox read-only \
        "Reply with the single word OK and nothing else." 2>&1 | tail -20
    '
}

snap "pre"
# Finding (2026-07-10): aging last_refresh alone does NOT trigger a refresh
# in codex 0.144.1 — the turn just runs on the valid access token. The
# working trigger is token expiry: rewrite exp to the past, the server
# rejects, codex refreshes with the refresh_token and persists rotation.
age_last_refresh
expire_tokens
snap "aged+expired"

run_turn "run1-forced-refresh" | tee "$OUT/probe2-run1.log"
snap "post-run1"

run_turn "run2-clean-start" | tee "$OUT/probe2-run2.log"
snap "post-run2"

echo "== host login state (reads ~/.codex, untouched by this probe) =="
codex login status 2>&1 | head -3 || true

echo "PROBE2 done — compare the snap lines: refresh happened iff access_token/sha/last_refresh changed after run1."
