# NerfGram Server

A fork of [owpengram/owpengram-server](https://github.com/owpengram/owpengram-server) —
a self-hosted server compatible with the Telegram protocol (MTProto, layer 225).
Internally the project is called `telesrv`, which shows up in paths
(`cmd/telesrv`, `TELESRV_*` environment variables) and in the database itself.

Works with the original Telegram Android/Desktop clients (and their forks
[owpengram-android-client](https://github.com/owpengram/owpengram-android-client),
[owpengram-desktop-client](https://github.com/owpengram/owpengram-desktop-client))
with zero client-side changes — every feature below uses standard protocol
methods the clients already implement; they just weren't implemented on the
server before.

## Quick start

### 1. Dependencies
```bash
sudo apt update && sudo apt install -y git golang-go docker.io docker-compose-plugin screen
```
Requires **Go 1.25+** (`go version`). If `apt` has an older version, install
via `sudo snap install go --classic` or from go.dev/dl.

### 2. Clone and configure
```bash
git clone https://github.com/kernelfoxx/ResendGram-server.git
cd ResendGram-server
cp .env.example .env
```

Make sure to change in `.env`:
- **`TELESRV_DEV_AUTH_CODE`** — to a long random string. The default `12345`
  is publicly known (it's right there in `.env.example`) — leaving it as-is
  is an open door into any account.
- **`TELESRV_ADMIN_API_TOKEN`**, **`TELESRV_ADMIN_UI_PASSWORD`** (or
  `TELESRV_ADMIN_UI_TOKEN`), **`TELESRV_ADMIN_SESSION_KEY`** — admin secrets,
  don't leave these empty/default either.
- Where possible, `TELESRV_PHONE_CODE_DELIVERY_PROVIDER=webhook` with a real
  SMS provider instead of `development` mode.

### 3. Postgres + Redis
```bash
cd deploy && docker compose up -d && cd ..
```

### 4. Build
```bash
go build ./...                          # sanity check that everything compiles
go build -o bin/gramsrv ./cmd/telesrv
go build -o bin/telesrv-admin ./cmd/telesrv-admin
```
Database migrations run **automatically** on `gramsrv` startup — nothing to
apply by hand.

### 5. Run

These are **two independent processes**, both required at the same time,
both reading the same `.env`:

| Binary | What it does | Without it |
|---|---|---|
| `bin/gramsrv` | The server itself: MTProto protocol, all business logic, what clients actually connect to | Nothing works — not clients, not the admin panel |
| `bin/telesrv-admin` | The admin web panel — talks to `gramsrv` over the admin API (token-authenticated) | The server works fine for users; you just lose the web UI for admin actions (still reachable via `curl` against `/v1/...` directly) |

```bash
screen -S gramsrv
./bin/gramsrv
# Ctrl+A, then D — detach, the process keeps running

screen -S admin
./bin/telesrv-admin
# Ctrl+A, D
```

Check: `screen -ls` should list both sessions. Reattach to either with
`screen -r gramsrv` / `screen -r admin`.

## ⚠️ The most common update mistake

**Both binaries must be rebuilt and restarted separately.** The full update
cycle after `git pull`:

```bash
git pull
go build -o bin/gramsrv ./cmd/telesrv
go build -o bin/telesrv-admin ./cmd/telesrv-admin
```

Then, **in each** screen session: `Ctrl+C` (kill the old process) → run the
binary again → `Ctrl+A, D`.

If you only rebuild/restart `telesrv-admin`, new buttons will show up in the
panel, but the protocol itself (what clients actually see — gifts,
usernames, the Fake badge, etc.) won't change, because that logic lives in
`gramsrv`, not the admin panel. Check the binaries are actually fresh:
```bash
ls -la bin/gramsrv bin/telesrv-admin
```
The timestamp should be after your last `git pull`. Also hard-refresh the
browser (`Ctrl+Shift+R`) for the web panel itself — otherwise it may serve a
cached old JS bundle.

## What this fork changes vs. upstream

### Security
- **Login codes for existing accounts are no longer static.** Previously
  `TELESRV_DEV_AUTH_CODE` (default `12345`, publicly documented) worked to
  log into **any** account, not just for new signups — that's what led to
  the account compromise this fork started from. Now: a brand-new phone
  number (no account yet) still gets the static code, for easy testing; an
  **existing** account always gets a random code, delivered in-app to any
  other active session the owner has (same as real Telegram when you log in
  from a new device). If the account has no other active sessions and no
  real SMS provider is configured, there's nowhere to deliver the code —
  that's an inherent limitation of dev mode, not a bug.
- **`contacts.importContacts` no longer leaks every user's phone number.**
  Previously any account could bulk-submit a batch of phone numbers and
  learn which ones were registered on the server, bypassing privacy
  settings entirely. Now it respects `PrivacyKeyAddedByPhone` ("who can find
  me by phone number") — a number is only revealed if the target allows it,
  or if they're already in your contacts.

### Gifts (Stars Gifts)
- An admin can grant an existing catalog gift to a specific user (by
  `user_id`/username/phone) for free — no real payment, funded internally
  from the system account, so the whole payment ledger stays consistent
  (no separate "free" code path bypassing normal purchase logic).
- **Supply limits for plain (not just official) gifts.** `0` = unlimited,
  but such a gift can never later be upgraded to a collectible (NFT). `>0` =
  limited supply, tracked and shown to clients as remaining/total, same as
  the original. Upgrading to a collectible is a separate, later step.
- **"Released by"** — optionally set a user or channel (by ID), shown to
  clients as "Released by @username". The referenced user/channel is
  required to already have a username — otherwise there'd be nothing to
  display.

### Collectible (NFT) usernames
Implemented from scratch — the server previously didn't respond to
`fragment.getCollectibleInfo` at all (the protocol method itself wasn't
wired up).

Important: a collectible username is a **second, additional** username
(matching real Telegram: `usernames: vector<Username>` with
`editable`/`active` flags), not a relabeling of an existing one. An admin
issues a **new** username string to a user; their primary username is left
untouched. The owner can switch between the two themselves via
`account.toggleUsername` (this RPC method was previously a no-op stub across
the whole project — now it actually works for regular users; it remains a
stub for channels/bots).

Price/currency (e.g. `1000 YUT`) is purely a display field — nothing is
actually charged.

### Fake badge
An admin can set/clear `tg.User.fake` — clients render it as a small red
"FAKE" tag next to the name (not text in the profile bio — that's how real
Telegram does it too).

## Layout

```
cmd/telesrv/                 gramsrv — the server itself
cmd/telesrv-admin/           telesrv-admin — admin web panel (Go backend + web/ — React/TS frontend)
internal/rpc/                 MTProto RPC handlers
internal/admin/                Admin command business logic
internal/adminapi/             HTTP wrapper (Bearer token) over internal/admin
internal/app/                   Application services (users, contacts, stars, stargifts, ...)
internal/store/postgres/        Postgres layer
deploy/migrations/              SQL migrations (applied automatically on gramsrv startup)
deploy/docker-compose.yml       Postgres + Redis for dev/prod
```

## Diagnostics

```bash
git log --oneline -1                             # current commit
ls -la bin/gramsrv bin/telesrv-admin              # binary timestamps — newer than the last git pull?
screen -ls                                        # are both processes actually up?
docker compose -f deploy/docker-compose.yml ps    # postgres/redis alive?
```

Query a specific table directly:
```bash
docker exec -it telesrv-postgres psql -U telesrv -d telesrv -c "SELECT ...;"
```
