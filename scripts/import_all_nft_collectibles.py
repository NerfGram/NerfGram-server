#!/usr/bin/env python3
"""Import collectibles for every upgradeable official gift (skip Durov + already imported)."""

from __future__ import annotations

import json
import subprocess
import sys
import time
import urllib.error
import urllib.request

TOKEN = "changeme_admin_api_token"
BASE = "http://127.0.0.1:2599"
DUROV = {
    5915521180483191380,
    5834651202612102354,
    6003477390536213997,
    6001229799790478558,
    6001425315291727333,
}


def api(method: str, path: str, body: dict | None = None, timeout: int = 900) -> dict:
    data = None if body is None else json.dumps(body).encode()
    headers = {"Authorization": f"Bearer {TOKEN}"}
    if body is not None:
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(f"{BASE}{path}", data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"HTTP {e.code}: {raw[:500]}") from e


def wait_healthy(retries: int = 90) -> None:
    for _ in range(retries):
        try:
            api("GET", "/v1/official-gifts", timeout=30)
            return
        except Exception:
            time.sleep(2)
    raise RuntimeError("admin API not healthy")


def already_imported() -> set[int]:
    sql = """
SELECT r.official_gift_id
FROM star_gift_catalog c
JOIN star_gift_catalog_revisions r ON r.id = c.active_revision_id
WHERE c.collectible_revision_id IS NOT NULL
  AND r.official_gift_id IS NOT NULL;
"""
    out = subprocess.check_output(
        ["docker", "exec", "telesrv-postgres", "psql", "-U", "telesrv", "-d", "telesrv", "-tAc", sql],
        text=True,
    )
    ids: set[int] = set()
    for line in out.splitlines():
        line = line.strip()
        if line:
            ids.add(int(line))
    return ids


def main() -> int:
    wait_healthy()
    have = already_imported()
    gifts = api("GET", "/v1/official-gifts")["gifts"]
    nfts = [
        g
        for g in gifts
        if g.get("can_upgrade")
        and int(g["source_gift_id"]) not in DUROV
        and int(g["source_gift_id"]) not in have
    ]
    print(f"already have collectibles for {len(have)}; importing {len(nfts)} remaining...")
    ok = fail = 0
    failed: list[str] = []
    for i, g in enumerate(nfts, 1):
        sid = str(g["source_gift_id"])
        body = {
            "command_id": f"import-nft-{sid}-v4",
            "actor": "ops",
            "reason": "import all official NFT collectibles",
            "dry_run": False,
            "source_gift_id": sid,
            "enabled": True,
            "include_collectible": True,
            "sort_order": 1000 + i,
            "supply_total": 100000,
        }
        success = False
        for attempt in range(1, 5):
            try:
                wait_healthy()
                # Connection may drop after a successful heavy import; verify via DB.
                try:
                    resp = api("POST", "/v1/official-gifts/import", body)
                    details = resp.get("details") or {}
                    print(
                        f"[{i}/{len(nfts)}] OK {g.get('title') or sid} "
                        f"gift_id={details.get('gift_id')} models={details.get('models')}"
                    )
                except Exception as e:  # noqa: BLE001
                    time.sleep(2)
                    if int(sid) in already_imported():
                        print(f"[{i}/{len(nfts)}] OK(after disconnect) {sid} ({e})")
                    else:
                        raise
                ok += 1
                success = True
                have.add(int(sid))
                break
            except Exception as e:  # noqa: BLE001
                print(f"[{i}/{len(nfts)}] attempt {attempt} FAIL {sid}: {str(e)[:200]}")
                time.sleep(5 * attempt)
        if not success:
            fail += 1
            failed.append(sid)
        time.sleep(2)
    print(f"done ok={ok} fail={fail}")
    if failed:
        print("failed ids:", ",".join(failed))
    final = already_imported()
    print(f"collectible official gifts now: {len(final)}")
    return 1 if fail else 0


if __name__ == "__main__":
    sys.exit(main())
