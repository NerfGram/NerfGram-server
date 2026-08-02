#!/usr/bin/env python3
"""Bulk-import all official gifts (basic + NFT/collectible) via admin API."""

from __future__ import annotations

import json
import sys
import urllib.error
import urllib.request

TOKEN = "changeme_admin_api_token"
BASE = "http://127.0.0.1:2599"


def api(method: str, path: str, body: dict | None = None) -> dict:
    data = None if body is None else json.dumps(body).encode()
    headers = {"Authorization": f"Bearer {TOKEN}"}
    if body is not None:
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(f"{BASE}{path}", data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=300) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"HTTP {e.code}: {raw[:500]}") from e


def main() -> int:
    gifts = api("GET", "/v1/official-gifts")["gifts"]
    print(f"importing {len(gifts)} gifts...")
    ok = fail = 0
    errors: list[tuple[int, bool, str]] = []
    for i, g in enumerate(gifts, 1):
        sid = g["source_gift_id"]
        include = bool(g.get("can_upgrade"))
        body = {
            "command_id": f"import-official-{sid}",
            "actor": "ops",
            "reason": "bulk import all official gifts",
            "dry_run": False,
            "source_gift_id": sid,
            "enabled": True,
            "include_collectible": include,
            "sort_order": i,
            # NFT snapshots often omit availability_total; collectible publish needs supply.
            "supply_total": 100000 if include else 0,
        }
        kind = "nft" if include else "basic"
        try:
            resp = api("POST", "/v1/official-gifts/import", body)
            gift_id = (resp.get("details") or {}).get("gift_id")
            print(f"[{i}/{len(gifts)}] OK {kind} {sid} gift_id={gift_id}")
            ok += 1
        except Exception as e:  # noqa: BLE001
            fail += 1
            msg = str(e)
            errors.append((sid, include, msg))
            print(f"[{i}/{len(gifts)}] FAIL {kind} {sid}: {msg[:200]}")
    print(f"done ok={ok} fail={fail}")
    for sid, include, msg in errors[:15]:
        print(f" ERR {'nft' if include else 'basic'} {sid}: {msg[:200]}")
    return 1 if fail else 0


if __name__ == "__main__":
    sys.exit(main())
