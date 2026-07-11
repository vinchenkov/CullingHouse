#!/usr/bin/env bash
# S3 probe 6 — Docker Desktop restart MID-REFRESH (serialized leg).
#
# DESTRUCTIVE: quits and restarts Docker Desktop (kills every running
# container; restart=always/unless-stopped ones come back on their own,
# restart=no ones are re-started by the `restore` step).
#
# Works ONLY against a SCRATCH copy of the codex credential dir
# (~/.mc-dev-home/spike03/restart-codex, seeded from the newest known
# refresh-token chain at ~/.mc-dev-home/spike03/race-codex). The canonical
# ~/.mc-dev-home/cred/codex is never mounted or modified here.
#
# Secrets discipline: never prints token values (first 8 chars max, same
# convention as probes 2-4); poller logs carry sha256 prefixes/sizes only.
#
# Subcommands (so each stays inside a single foreground call):
#   prep            snapshot the running-container set, seed scratch dirs
#   drill N D       one kill cycle: expire tokens, start refresh turn +
#                   synthetic rename-loop, quit Docker after D seconds,
#                   confirm backend death, relaunch, verify integrity
#   verify          post-drill: auth.json parses + clean codex run from it
#   restore         re-start pre-drill restart=no containers, compare sets
set -uo pipefail

HERE="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$HERE/out"
STASH="$HOME/.mc-dev-home/spike03"
SEED="$STASH/race-codex"                 # newest refresh-token chain (finding 5)
SCRATCH="$STASH/restart-codex"           # drill target (scratch copy)
SYNTH="$STASH/restart-synth"             # synthetic rename-loop target dir
IMG="mcspike-08-base:latest"
TURN_CTR="mcspike-03-restart-turn"
SYNTH_CTR="mcspike-03-restart-synth"
mkdir -p "$OUT" "$STASH"

ts() { python3 -c 'import time;print(f"{time.time():.3f}")'; }
say() { printf '[%s] %s\n' "$(date -u +%H:%M:%SZ)" "$*"; }

snap() { # label file  -> one line, token prefixes only (probe2 convention)
  python3 - "$2" "$1" <<'EOF'
import json,sys,hashlib,os,base64,datetime
p,label=sys.argv[1],sys.argv[2]
raw=open(p,'rb').read()
a=json.loads(raw)
tok=a['tokens']['access_token']
pay=tok.split('.')[1]; pay+='='*(-len(pay)%4)
exp=json.loads(base64.urlsafe_b64decode(pay)).get('exp',0)
print(f"[{label}] sha256={hashlib.sha256(raw).hexdigest()[:16]} inode={os.stat(p).st_ino} "
      f"size={len(raw)} access_token[:8]={tok[:8]} refresh_sha8={hashlib.sha256(a['tokens']['refresh_token'].encode()).hexdigest()[:8]} "
      f"last_refresh={a['last_refresh']} access_exp={datetime.datetime.utcfromtimestamp(exp).isoformat()}Z")
EOF
}

expire_tokens() { # file — rewrite exp claim to the past (forces the refresh path)
  python3 - "$1" <<'EOF'
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
print("expired access_token+id_token (exp=now-3600)")
EOF
}

docker_alive() { # liveness by OUTPUT SHAPE, never exit code (S7 finding 2)
  local v
  v="$(docker version --format '{{.Server.Version}}' 2>/dev/null | head -1)"
  [[ "$v" =~ ^[0-9]+(\.[0-9]+)+$ ]]
}

start_poller() { # logfile — 20 Hz watcher over scratch auth.json + synth target
  python3 - "$SCRATCH/auth.json" "$SYNTH/target.json" "$1" <<'EOF' &
import sys,time,os,hashlib,json
auth,synth,logp=sys.argv[1],sys.argv[2],sys.argv[3]
last={}
def sample(tag,p,f):
    try:
        raw=open(p,'rb').read()
    except FileNotFoundError:
        st=(tag,'MISSING',0,'-','-')
    else:
        try:
            json.loads(raw); ok='ok'
        except Exception as e:
            ok=f'PARSE-FAIL:{type(e).__name__}'
        try: ino=os.stat(p).st_ino
        except Exception: ino='-'
        st=(tag,ok,len(raw),hashlib.sha256(raw).hexdigest()[:16],ino)
    if last.get(tag)!=st:
        last[tag]=st
        f.write(f"{time.time():.3f} file={st[0]} parse={st[1]} size={st[2]} sha={st[3]} ino={st[4]}\n")
        f.flush()
with open(logp,'a') as f:
    f.write(f"{time.time():.3f} poller-start\n")
    while True:
        sample('auth',auth,f)
        sample('synth',synth,f)
        time.sleep(0.05)
EOF
  echo $!
}

case "${1:-}" in
# ---------------------------------------------------------------- prep
prep)
  say "recording pre-drill running-container set"
  docker ps --format '{{.Names}}' | while read -r n; do
    printf '%s restart=%s\n' "$n" "$(docker inspect -f '{{.HostConfig.RestartPolicy.Name}}' "$n")"
  done | sort > "$OUT/restart-preset.txt"
  cat "$OUT/restart-preset.txt"

  [[ -f "$SEED/auth.json" ]] || { echo "seed chain missing: $SEED/auth.json"; exit 1; }
  BK="$STASH/backup-restartdrill-$(date +%s)"; mkdir -p "$BK"
  cp -p "$SEED/auth.json" "$BK/"; say "seed auth.json backed up to $BK (host stash, outside repo)"

  rm -rf "$SCRATCH" "$SYNTH"; mkdir -p "$SYNTH"
  cp -Rp "$SEED" "$SCRATCH"
  python3 -c 'import json;json.dump({"counter":0,"pad":"x"*4096},open("'"$SYNTH"'/target.json","w"))'
  snap "prep-seeded" "$SCRATCH/auth.json"
  say "scratch ready: $SCRATCH (canonical cred dir untouched)"
  ;;
# ---------------------------------------------------------------- drill
drill)
  N="${2:?drill number}"; D="${3:?quit delay seconds}"
  POLL="$OUT/restart-d$N-poll.log"; TLOG="$OUT/restart-d$N-turn.log"
  : > "$POLL"; : > "$TLOG"
  docker_alive || { echo "docker not alive at drill start"; exit 1; }
  docker rm -f "$TURN_CTR" "$SYNTH_CTR" >/dev/null 2>&1 || true

  snap "d$N-pre" "$SCRATCH/auth.json" | tee -a "$TLOG"
  expire_tokens "$SCRATCH/auth.json" | tee -a "$TLOG"
  snap "d$N-expired" "$SCRATCH/auth.json" | tee -a "$TLOG"
  EXP_SHA="$(snap x "$SCRATCH/auth.json" | grep -o 'sha256=[0-9a-f]*')"

  PPID_POLL="$(start_poller "$POLL")"
  say "poller pid=$PPID_POLL -> $POLL"

  say "starting synthetic rename-loop container"
  docker run -d --name "$SYNTH_CTR" -v "$SYNTH":/mnt/synth "$IMG" bash -c '
    i=0
    while :; do
      i=$((i+1))
      python3 -c "import json,os,tempfile,sys
i=int(sys.argv[1])
fd,tmp=tempfile.mkstemp(dir=\"/mnt/synth\")
with os.fdopen(fd,\"w\") as f: json.dump({\"counter\":i,\"pad\":\"x\"*4096},f)
os.rename(tmp,\"/mnt/synth/target.json\")" $i
    done' >/dev/null

  T0="$(ts)"; say "T0=$T0 starting forced-refresh turn container"
  docker run -d --name "$TURN_CTR" \
    -v "$SCRATCH":/codex-home -e CODEX_HOME=/codex-home -w /tmp \
    "$IMG" bash -c '
      for v in ANTHROPIC_API_KEY CODEX_API_KEY OPENAI_API_KEY; do
        [ -n "${!v:-}" ] && { echo "FORBIDDEN ENV PRESENT: $v"; exit 90; }
      done
      codex exec --skip-git-repo-check --sandbox read-only \
        "Reply with the single word OK and nothing else." 2>&1
    ' >/dev/null

  say "sleeping D=$D s, then quitting Docker Desktop"
  sleep "$D"
  TQ="$(ts)"; say "TQ=$TQ osascript quit app Docker"
  osascript -e 'quit app "Docker"' 2>&1 | tee -a "$TLOG" || true

  # S7 finding 3: backend can wedge; confirm it actually exited, escalate.
  DEAD=""; W0="$(ts)"
  for i in $(seq 1 90); do
    pgrep -x com.docker.backend >/dev/null 2>&1 || { DEAD="$(ts)"; break; }
    sleep 1
  done
  if [[ -z "$DEAD" ]]; then
    say "backend still up after 90s -> SIGTERM"
    pkill -x -TERM com.docker.backend || true
    for i in $(seq 1 30); do
      pgrep -x com.docker.backend >/dev/null 2>&1 || { DEAD="$(ts)"; break; }; sleep 1
    done
  fi
  if [[ -z "$DEAD" ]]; then
    say "backend survived SIGTERM -> SIGKILL"
    pkill -x -KILL com.docker.backend || true
    sleep 3; DEAD="$(ts)"
  fi
  say "TDEAD=$DEAD com.docker.backend gone (quit->dead $(python3 -c "print(f'{$DEAD-$TQ:.1f}')")s)"

  say "relaunching Docker Desktop"
  open -a Docker
  ALIVE=""; R0="$(ts)"
  while :; do
    docker_alive && { ALIVE="$(ts)"; break; }
    python3 -c "import time,sys;sys.exit(0 if time.time()-$R0<300 else 1)" || break
    sleep 2
  done
  kill "$PPID_POLL" 2>/dev/null || true
  [[ -n "$ALIVE" ]] || { say "FAIL: docker did not recover within 300s"; exit 1; }
  say "TALIVE=$ALIVE docker recovered (dead->alive $(python3 -c "print(f'{$ALIVE-$DEAD:.1f}')")s)"

  say "post-recovery integrity"
  POST="$(snap "d$N-post" "$SCRATCH/auth.json")" || { say "FAIL: auth.json does not parse after kill"; exit 1; }
  echo "$POST" | tee -a "$TLOG"
  POST_SHA="$(echo "$POST" | grep -o 'sha256=[0-9a-f]*')"
  if [[ "$POST_SHA" == "$EXP_SHA" ]]; then
    say "state=OLD (still the expired-tampered file; kill landed before persist)"
  else
    say "state=NEW (rotation persisted before/through the kill)"
  fi
  say "poller verdict (any non-ok parse = torn state):"
  grep -c 'parse=ok' "$POLL" | xargs -I{} say "  ok-samples-with-change: {}"
  if grep -E 'parse=(PARSE-FAIL|MISSING)' "$POLL"; then
    say "FAIL: torn/missing state observed by host poller"; exit 1
  else
    say "PASS: no torn intermediate state ever observed host-side"
  fi
  say "synthetic target after kill:"
  python3 -c 'import json;d=json.load(open("'"$SYNTH"'/target.json"));print("  parse=ok counter=%d"%d["counter"])' \
    || { say "FAIL: synthetic target torn"; exit 1; }
  say "captured turn-container log (killed container, best effort):"
  docker logs "$TURN_CTR" 2>&1 | tail -15 | tee -a "$TLOG" || true
  docker rm -f "$TURN_CTR" "$SYNTH_CTR" >/dev/null 2>&1 || true
  say "drill $N complete"
  ;;
# ---------------------------------------------------------------- verify
verify)
  say "subsequent clean run from the post-kill scratch state"
  snap "verify-pre" "$SCRATCH/auth.json"
  docker rm -f "$TURN_CTR" >/dev/null 2>&1 || true
  docker run --rm --name "$TURN_CTR" \
    -v "$SCRATCH":/codex-home -e CODEX_HOME=/codex-home -w /tmp \
    "$IMG" bash -c '
      for v in ANTHROPIC_API_KEY CODEX_API_KEY OPENAI_API_KEY; do
        [ -n "${!v:-}" ] && { echo "FORBIDDEN ENV PRESENT: $v"; exit 90; }
      done
      codex exec --skip-git-repo-check --sandbox read-only \
        "Reply with the single word OK and nothing else." 2>&1 | tail -8
    '
  RC=$?
  snap "verify-post" "$SCRATCH/auth.json"
  [[ $RC -eq 0 ]] && say "PASS: clean run after drill (exit 0)" || { say "FAIL: post-drill run rc=$RC"; exit 1; }
  ;;
# ---------------------------------------------------------------- restore
restore)
  say "restoring pre-drill running-container set"
  [[ -f "$OUT/restart-preset.txt" ]] || { echo "no preset snapshot"; exit 1; }
  while read -r name _; do
    if ! docker ps --format '{{.Names}}' | grep -qx "$name"; then
      say "starting $name"; docker start "$name" >/dev/null || say "WARN: could not start $name"
    fi
  done < "$OUT/restart-preset.txt"
  sleep 5
  docker ps --format '{{.Names}}' | sort > "$OUT/restart-postset.txt"
  if diff <(awk '{print $1}' "$OUT/restart-preset.txt" | sort) "$OUT/restart-postset.txt" > "$OUT/restart-setdiff.txt"; then
    say "PASS: running-container set restored (matches pre-drill)"
  else
    say "FAIL: set differs:"; cat "$OUT/restart-setdiff.txt"; exit 1
  fi
  ;;
*)
  echo "usage: $0 prep | drill N D | verify | restore"; exit 2;;
esac
