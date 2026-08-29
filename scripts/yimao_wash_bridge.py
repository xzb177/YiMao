#!/usr/bin/env python3
from __future__ import annotations
import json, os, re, subprocess, sys
from pathlib import Path
ID_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$")
MAX_TITLE, MAX_RECORDS = 180, 25
DATA = Path(os.environ.get("YIMAO_REVIEW_FILE", "/app/data/review_requests.json"))
HERMES = "/opt/hermes/bin/hermes"
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
    if x.get("business_type") != "wash" or x.get("status") != "approved": return False
    rid, title = text(x.get("request_id"), 128), text(x.get("media_title"), MAX_TITLE)
    try: tmdb, season = int(x.get("tmdb_id") or 0), int(x.get("season") or 0)
    except (TypeError, ValueError): return False
    return bool(ID_RE.fullmatch(rid) and tmdb > 0 and title and 0 <= season <= 1000 and isinstance(x.get("wash_baseline"), list) and x["wash_baseline"])

def body(x):
    paths = [text(p, 500) for p in x.get("wash_baseline", []) if text(p, 500)]
    payload = {"request_id": text(x.get("request_id"),128), "tmdb_id": int(x.get("tmdb_id") or 0), "media_title": text(x.get("media_title"),MAX_TITLE), "media_year": int(x.get("media_year") or 0), "media_type": text(x.get("media_type"),20), "season": int(x.get("season") or 0), "baseline_path_count": len(paths), "baseline_paths": paths}
    return "YiMao approved wash handoff\n\n" + json.dumps(payload, ensure_ascii=False, indent=2) + "\n\n" + CONTRACT

def dispatch(x):
    rid = text(x.get("request_id"),128)
    title = "YiMao wash: " + text(x.get("media_title"),MAX_TITLE)
    args = [HERMES,"kanban","create",title,"--body",body(x),"--assignee","mp","--idempotency-key","yimao-wash:"+rid,"--created-by","yimao-wash-bridge","--max-retries","3","--max-runtime","2h","--json"]
    return subprocess.run(args, capture_output=True, text=True, timeout=30).returncode

def main():
    try:
        if os.environ.get("YIMAO_REVIEW_FILE"):
            raw = json.loads(DATA.read_text(encoding="utf-8"))
        else:
            proc = subprocess.run(["docker", "exec", "yimao", "cat", str(DATA)], capture_output=True, text=True, timeout=15)
            if proc.returncode != 0: raise RuntimeError("docker_read_failed")
            raw = json.loads(proc.stdout)
    except Exception:
        print("scan_error=review_file_unreadable", file=sys.stderr); return 1
    chosen = [x for x in records(raw) if eligible(x)][:MAX_RECORDS]
    failed = sum(dispatch(x) != 0 for x in chosen)
    print(f"scan=approved_wash candidates={len(chosen)} created_or_reused={len(chosen)-failed} failed={failed}")
    return 1 if failed else 0

if __name__ == "__main__": raise SystemExit(main())
