#!/usr/bin/env bash
# S3 probe 4 — concurrent-refresh race: two containers sharing ONE auth dir,
# both induced to refresh near-simultaneously.
#
# Safety shape (per spike brief): the shared dir is a SCRATCH COPY of the
# canonical codex dir, so a lost race cannot brick the good copy at the
# file level. THE CANONICAL DIR IS NEVER WRITTEN by this probe. Server-side
# implication: the racers refresh with the refresh_token they share with the
# canonical copy; if rotation is single-use, the canonical rt is consumed —
# the scratch copy then holds the newest rotation (kept at
# ~/.mc-dev-home/spike03/race-codex/ for the operator). Documented in
# RESULT.md; canonical's access token itself stays valid until its own exp.
#
# LIVE: burns 2 tiny subscription turns (the racers).
# Secrets discipline: prints token prefixes (8 chars) only.
set -euo pipefail

CRED="${HOME}/.mc-dev-home/cred/codex"
STASH="${HOME}/.mc-dev-home/spike03"
RACE="$STASH/race-codex"
IMG="mcspike-08-base:latest"
OUT="$(cd "$(dirname "$0")/.." && pwd)/out"
mkdir -p "$OUT" "$STASH"

[[ -f "$CRED/auth.json" ]] || { echo "no canonical codex auth.json"; exit 1; }

BK="$STASH/backup-codex-$(date +%s)"
mkdir -p "$BK"; cp -p "$CRED/auth.json" "$BK/"
echo "backup: $BK (host-side stash, outside repo)"

# Fresh scratch copy: auth.json + identity files only (codex recreates the rest).
rm -rf "$RACE"; mkdir -p "$RACE"
cp -p "$CRED/auth.json" "$RACE/"
[[ -f "$CRED/installation_id" ]] && cp -p "$CRED/installation_id" "$RACE/"
chmod 700 "$RACE"

snap() { # path label
  python3 - "$1" "$2" <<'EOF'
import json,sys,hashlib,os,base64,datetime
p,label=sys.argv[1],sys.argv[2]
raw=open(p,'rb').read()
try:
    a=json.loads(raw)
except Exception as e:
    print(f"[{label}] JSON PARSE FAILED: {e} (size={len(raw)})"); sys.exit(0)
tok=a['tokens']['access_token']
pay=tok.split('.')[1]; pay+='='*(-len(pay)%4)
exp=json.loads(base64.urlsafe_b64decode(pay)).get('exp',0)
print(f"[{label}] sha256={hashlib.sha256(raw).hexdigest()[:16]} "
      f"access[:8]={tok[:8]} rt[:8]={a['tokens']['refresh_token'][:8]} "
      f"last_refresh={a['last_refresh']} "
      f"exp={datetime.datetime.fromtimestamp(exp,datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')}")
EOF
}

expire_tokens() { # rewrite exp claim to the past (invalidates sig; forces refresh path)
  python3 - "$RACE/auth.json" <<'EOF'
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
print("rewrote exp to now-3600 in access_token and id_token (scratch copy)")
EOF
}

run_racer() { # name  -> runs detached, writes its own log
  local name="$1"
  docker run --rm --name "mcspike-03-$name" \
    -v "$RACE":/codex-home \
    -e CODEX_HOME=/codex-home \
    -w /tmp \
    "$IMG" bash -c '
      for v in ANTHROPIC_API_KEY CODEX_API_KEY OPENAI_API_KEY; do
        [ -n "${!v:-}" ] && { echo "FORBIDDEN ENV PRESENT: $v"; exit 90; }
      done
      codex exec --skip-git-repo-check --sandbox read-only \
        "Reply with the single word OK and nothing else." 2>&1 | tail -8
      exit ${PIPESTATUS[0]}
    ' > "$OUT/probe4-$name.log" 2>&1
}

snap "$RACE/auth.json" "scratch-pre"
expire_tokens
snap "$RACE/auth.json" "scratch-expired"

echo "== launching two racing codex turns against the shared scratch dir =="
run_racer racerA & PA=$!
run_racer racerB & PB=$!
RCA=0; RCB=0
wait $PA || RCA=$?
wait $PB || RCB=$?
echo "racerA exit=$RCA  racerB exit=$RCB"
echo "--- racerA tail ---"; tail -6 "$OUT/probe4-racerA.log"
echo "--- racerB tail ---"; tail -6 "$OUT/probe4-racerB.log"

echo "== scratch auth.json after the race =="
snap "$RACE/auth.json" "scratch-post-race"
python3 -c "import json,sys; json.load(open(sys.argv[1])); print('scratch auth.json: VALID JSON, not torn')" "$RACE/auth.json" \
  || echo "scratch auth.json: CORRUPT (torn write) — fallback ADR territory"

echo "== canonical dir state (READ-ONLY check: never written by this probe) =="
snap "$CRED/auth.json" "canonical-after-race"

echo "== host login state (reads ~/.codex, untouched by this probe) =="
codex login status 2>&1 | head -3 || true

echo "PROBE4 done — outcomes: racer exits above; corruption iff parse failed; serialization behavior per racer logs."
