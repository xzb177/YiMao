#!/usr/bin/env python3
from __future__ import annotations
import json, os, re, subprocess, sys
from pathlib import Path
ID_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$")
MAX_TITLE, MAX_RECORDS, MAX_BASELINE_PATHS, MAX_BODY = 180, 25, 50, 12000
DATA = Path(os.environ.get("YIMAO_REVIEW_FILE", "/app/data/review_requests.json"))
STATE = lambda: Path(os.environ.get("YIMAO_WASH_BRIDGE_STATE", "/var/lib/yimao-wash-bridge/state.json"))
HERMES = os.environ.get("HERMES_CLI", "/opt/hermes/.venv/bin/hermes")
CONTRACT = ("Only operate MoviePilot/qB/Emby for this approved wash; preserve every baseline resource; "
 "do not modify YiMao source, config, or review_requests.json; do not create an ordinary non-wash request. "
 "Inspect existing subscriptions, trigger the appropriate best-version/wash search and download, and report "
 "exact MoviePilot/qB identifiers, action, and verification. If blocked, explain why and keep this task visible. "
 "Call kanban_show first, heartbeat during long work, and kanban_complete with machine-readable metadata "
 "including request_id, subscription/torrent identifiers, action, verification, blocked_reason, and residual_risk.")

def text(v, limit):
    return re.sub(r"\s+", " ", str(v or "").replace("\x00", " ")).strip()[:limit]

def records(raw):
    if isinstance(raw, list): return [x for x in raw if isinstance(x, dict)]
    if isinstance(raw, dict):
        for key in ("reviews", "requests", "items"):
            if isinstance(raw.get(key), list): return [x for x in raw[key] if isinstance(x, dict)]
        return [x for x in raw.values() if isinstance(x, dict)]
    return []

def eligible(x):
    # This bridge is deliberately movie-only. TV wash behavior remains entirely
    # in YiMao's existing workbench and is never sent to the MoviePilot worker.
    if x.get("business_type") != "wash" or x.get("status") != "approved": return False
    if x.get("media_type") != "movie": return False
    rid, title = text(x.get("request_id"), 128), text(x.get("media_title"), MAX_TITLE)
    try: tmdb, season = int(x.get("tmdb_id") or 0), int(x.get("season") or 0)
    except (TypeError, ValueError): return False
    return bool(ID_RE.fullmatch(rid) and tmdb > 0 and title and season == 0 and isinstance(x.get("wash_baseline"), list) and x["wash_baseline"])

def body(x):
    paths = [text(p, 500) for p in x.get("wash_baseline", [])[:MAX_BASELINE_PATHS] if text(p, 500)]
    payload = {"request_id": text(x.get("request_id"),128), "tmdb_id": int(x.get("tmdb_id") or 0), "media_title": text(x.get("media_title"),MAX_TITLE), "media_year": int(x.get("media_year") or 0), "media_type": text(x.get("media_type"),20), "season": int(x.get("season") or 0), "baseline_path_count": len(paths), "baseline_paths": paths}
    result = "YiMao approved wash handoff\n\n" + json.dumps(payload, ensure_ascii=False, indent=2) + "\n\n" + CONTRACT
    return result[:MAX_BODY]

def dispatch_args(x):
    rid = text(x.get("request_id"),128)
    title = "YiMao wash: " + text(x.get("media_title"),MAX_TITLE)
    return [HERMES,"kanban","create",title,"--body",body(x),"--assignee","mp","--idempotency-key","yimao-wash:"+rid,"--created-by","yimao-wash-bridge","--max-retries","3","--max-runtime","2h","--json"]

def dispatch(x):
    return subprocess.run(dispatch_args(x), capture_output=True, text=True, timeout=30).returncode

def load_raw():
    if os.environ.get("YIMAO_REVIEW_FILE") or DATA.exists():
        return json.loads(DATA.read_text(encoding="utf-8"))
    proc = subprocess.run(["docker", "exec", "yimao", "cat", str(DATA)], capture_output=True, text=True, timeout=15)
    if proc.returncode != 0: raise RuntimeError("docker_read_failed")
    return json.loads(proc.stdout)

def load_state():
    path = STATE()
    if not path.exists(): return None
    raw = json.loads(path.read_text(encoding="utf-8"))
    seen = raw.get("seen", []) if isinstance(raw, dict) else []
    if not isinstance(seen, list) or not all(isinstance(x, str) and ID_RE.fullmatch(x) for x in seen): raise ValueError("state_invalid")
    return set(seen)

def save_state(seen):
    path = STATE()
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text(json.dumps({"seen": sorted(seen)}, ensure_ascii=False) + "\n", encoding="utf-8")
    tmp.replace(path)

def main(argv=None):
    argv = list(argv or [])
    if any(arg not in {"--bootstrap"} for arg in argv) or len(argv) > 1:
        print("usage: yimao_wash_bridge.py [--bootstrap]", file=sys.stderr); return 2
    try:
        raw = load_raw()
        chosen = [x for x in records(raw) if eligible(x)][:MAX_RECORDS]
        seen = load_state()
    except Exception:
        print("scan_error=review_file_or_state_unreadable", file=sys.stderr); return 1
    ids = {text(x.get("request_id"), 128) for x in chosen}
    if argv == ["--bootstrap"]:
        if seen is not None:
            print("bootstrap=already_initialized", file=sys.stderr); return 1
        save_state(ids)
        print(f"bootstrap=ok skipped_existing={len(ids)}")
        return 0
    if seen is None:
        print("scan_error=bootstrap_required", file=sys.stderr); return 1
    pending = [x for x in chosen if text(x.get("request_id"), 128) not in seen]
    failed = 0
    for x in pending:
        if dispatch(x) == 0:
            seen.add(text(x.get("request_id"), 128))
            save_state(seen)
        else:
            failed += 1
    print(f"scan=approved_movie_wash candidates={len(chosen)} new={len(pending)} created_or_reused={len(pending)-failed} failed={failed}")
    return 1 if failed else 0

if __name__ == "__main__": raise SystemExit(main(sys.argv[1:]))
