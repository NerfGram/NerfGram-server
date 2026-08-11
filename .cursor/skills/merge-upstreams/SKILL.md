---
name: merge-upstreams
description: >-
  Merge gramsrv and NerfGram upstreams into the FromGram fork, resolve
  conflicts, dedupe migrations, and verify Docker builds. Use when syncing
  upstream/main, nerfgram/main, merging upstream changes, or handling fork
  duplicate migrations and branding conflicts.
---

# Merge Upstreams (FromGram)

Sync this fork with both upstream remotes while preserving FromGram branding
and fork-specific features.

## Remotes


| Remote     | URL                                                 | Branch |
| ---------- | --------------------------------------------------- | ------ |
| `upstream` | `https://github.com/iamxvbaba/gramsrv.git`          | `main` |
| `nerfgram` | `https://github.com/kernelfoxx/NerfGram-server.git` | `main` |
| `origin`   | `https://github.com/fromchat-messenger/FromGram`    | `main` |


## Workflow checklist

- [ ] 1. Fetch both remotes
- [ ] 2. Merge upstream/main
- [ ] 3. Resolve conflicts (strategy below)
- [ ] 4. Commit upstream merge
- [ ] 5. Merge nerfgram/main
- [ ] 6. Resolve conflicts (strategy below)
- [ ] 7. Drop incompatible/duplicate migrations
- [ ] 8. docker compose build telesrv telesrv-admin
- [ ] 9. Commit nerfgram merge

PowerShell: chain commands with `;`, not `&&`.

```powershell
cd C:\Users\denis\FluxGram-server
git fetch upstream --no-tags
git fetch nerfgram --no-tags
git log --oneline HEAD..upstream/main | Select-Object -First 20
git log --oneline HEAD..nerfgram/main | Select-Object -First 20
```



## Merge order

Always merge **upstream first**, then **nerfgram**. Upstream carries the
canonical schema and protocol work; NerfGram is a sibling fork with overlapping
fixes on an older schema.

```powershell
git merge upstream/main -m "Merge upstream gramsrv/main into FromGram."
# resolve, commit

git merge nerfgram/main -m "Merge NerfGram-server/main into FromGram."
# resolve, commit
```



## Conflict resolution



### Keep FromGram (ours)

- `internal/branding/branding.go` — configurable branding arch from upstream,
but **FromGram defaults** (`ProductName=FromGram`, `ProductUsername=fromgram`,
`PublicBaseURL=https://t.fromchat.ru`)
- `internal/links/links.go` — `DefaultAppName=FromGram`, `DefaultDownloadURL`
- `internal/compat/tdesktop/startup_stubs.go` — FromGram short name
- `README.md`, `.env.example` — FromGram clone URLs and brand env vars
- Admin UI (`cmd/telesrv-admin/web/**`) — upstream localization/features with
  **NerfGram glassmorphism dark theme** (blue palette, backdrop blur, Outfit
  headings). Default theme is dark; keep FromGram branding text.
- Chinese docs intentionally removed: `git rm README.zh-CN.md docs/configuration.zh-CN.md`



### Take upstream features (theirs for feature code)

During upstream merge, prefer upstream for:

- `internal/rpc/**`, `internal/app/**`, `internal/store/**`
- `cmd/telesrv/main.go`, `cmd/telesrv-admin/**`
- New migrations `0167`–`0174`

Combine when both sides add value:

- `internal/rpc/auth.go` — login notices (FromGram) + username hydration (upstream)
- `internal/rpc/channels_core.go` — `enrichCommunityFull` + hydrated chats



### Nerfgram merge: cherry-pick, don't wholesale take

NerfGram uses an older `owner_user_id` collectible schema. **Do not** take
NerfGram store/RPC wholesale. Cherry-pick isolated fixes only:

- `fragment.go` — default TON/USD currency on collectible info
- `user.go` — registry fallback in `ByUsername` after `ErrNoRows`
- Remove duplicate RPC methods (e.g. second `onAccountReorderUsernames`)

```powershell
# Bulk keep ours for schema/branding conflicts:
git checkout --ours -- internal/branding internal/links internal/config `
  internal/rpc/deps.go internal/rpc/convert_users.go `
  internal/store/postgres/collectible_username.go `
  internal/store/postgres/peer_username.go cmd/telesrv/main.go
```



## Migration duplicates

Read `deploy/migrations/README.md` first.

**Rules:**

1. Fork migrations use `YYYYMMDDHHMMSS_name` timestamps (never reuse upstream
  sequential numbers).
2. Upstream sequential migrations (`0001`–`0174`) keep their numbers.
3. Bridge migrations (`20260714003093_*_bridge`) that duplicate already-applied
  `01xx` migrations should be no-oped:

```sql
-- Superseded by earlier 01xx migrations on fresh installs.
SELECT 1;
```

1. **Drop** NerfGram migrations that reference `owner_user_id` or re-drop indexes
  already handled by `0151_collectible_usernames.up.sql`.
2. **Bridge** upstream `0167`–`0174` when schema is already at a timestamp
  `>= 20260714003099` — use `20260811094500_upstream_0167_0174_bridge`.

Check for duplicate version numbers:

```powershell
Get-ChildItem deploy/migrations/*.up.sql | ForEach-Object {
  $_.Name.Substring(0,14)
} | Group-Object | Where-Object Count -gt 1
```



## Verify builds

```powershell
docker compose build telesrv telesrv-admin
```

Do **not** run `go build` or `go test` (workspace rule).

## Common post-merge fixes


| Symptom                          | Fix                                                             |
| -------------------------------- | --------------------------------------------------------------- |
| `<<<<<<<` markers left           | `rg '^<<<<<<<'` then fix before commit                          |
| Duplicate method in `account.go` | Remove NerfGram copy; keep registry-based upstream version      |
| `CollectibleUsernames` undefined | NerfGram dep name; use `Usernames` (upstream registry)          |
| Migration collision at startup   | Renumber or noop duplicate; never delete applied migrations     |
| `relation "gif_catalog" does not exist` | Run bridge `20260811094500_upstream_0167_0174_bridge` |
| `duplicate handler for account.reorderUsernames` | Remove duplicate `registerRPC` in `account.go` |
| `duplicate handler for payments.getStarsSubscriptions` | Remove stub `registerRPC` in `payments.go` (keep real handler) |
| Admin dist asset conflicts       | Keep upstream-built `web/dist/`; `git rm` stray NerfGram assets |




## Reference

- Prior merge patterns: agent transcripts for upstream/nerfgram syncs
- Branding env vars: `TELESRV_BRAND_*` in `.env.example`
- Collectible username guard: `internal/store/postgres/collectible_username.go`
(refuse mint if live user already has plain username)

