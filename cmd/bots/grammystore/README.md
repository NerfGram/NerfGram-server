# grammY authentication and store bot

This service replaces the former JSON/Python bot with one grammY process and a
transactional SQLite database. It deliberately runs independently from
`cmd/telesrv`; a bot outage cannot stop MTProto.

## Included functionality

- automatic persistent number and initial code on `/start`;
- delivery and storage of real login codes through authenticated `POST /code`;
- free replacement numbers and paid anonymous `+888` numbers;
- Premium, server Stars and collectible username purchases through Telegram Stars;
- arbitrary Stars invoices, payment deduplication and a durable sales journal;
- compensated refunds that revoke the exact Stars, Premium entitlement,
  collectible username or paid number before returning Telegram Stars;
- account target IDs and three recent recipients;
- daily bonuses, referrals and a weighted wheel;
- promo codes and button-based giveaways;
- support tickets;
- complete Russian and English localization for menus, keyboards, invoices,
  errors, login codes and Bot API command descriptions;
- per-user language and notification settings; broadcasts skip disabled and
  stale recipients;
- owner-only statistics, broadcasts, Stars/Premium/bonus grants, invoices,
  payment refunds, login-code access, support replies, sales and Stars-rate controls;
- optional required-channel membership gate.

The bot token and Admin API token must never be committed. `.env.example`
contains names and safe local defaults only.

## Local run

Requirements: Node.js 22.13 or newer and a running gramsrv Admin API.

```bash
cd cmd/bots/grammystore
cp .env.example .env
nano .env
npm ci
npm test
npm start
```

Set `BOT_PUBLIC_USERNAME` so referral links are available before the first
`getMe`. `OWNER_IDS` accepts comma-separated Telegram IDs. Users set their
gramsrv account ID under Settings; Telegram IDs are not assumed to equal server
account IDs.

`PRODUCT_NAME` controls user-facing product text and defaults to `Telesrv`.
Deployment-specific branding belongs in the service environment, not in source.
`DEFAULT_LANGUAGE` is used until Telegram supplies or the user selects a
supported language. The selection is stored in SQLite and is not overwritten by
later updates from Telegram. Bot command descriptions are registered separately
for `ru` and `en` client locales.

Each paid fulfillment is snapshotted in the sales journal. Refunds are phased and
idempotent: if Telegram is temporarily unavailable after the server-side product
has been revoked, retrying the same transaction does not revoke it twice. Legacy
Premium and anonymous-number purchases without exact fulfillment metadata fail
safe and require manual review instead of touching unrelated account state.

## Migrating the former Python bot

Never open the legacy database directly with the new service because its table
names overlap but its columns are incompatible. Keep the old service stopped,
copy its database, and create a separate destination:

```bash
npm run migrate:legacy -- /path/to/legacy.sqlite3 /path/to/new.sqlite3
```

The command refuses to overwrite its source or an existing destination. It
preserves users, bonus balances, referrals, numbers, current login codes and
support messages. Keep the legacy database backup for historical orders and
broadcast drafts, which have no equivalent in the new Telegram Stars journal.

## Login-code webhook

Configure gramsrv's code-delivery webhook for:

```text
TELESRV_PHONE_CODE_DELIVERY_PROVIDER=webhook
TELESRV_OTP_WEBHOOK_URL=http://127.0.0.1:2800/v1/otp/deliveries
TELESRV_OTP_WEBHOOK_SECRET=<same value as CODE_WEBHOOK_SECRET>
```

gramsrv sends its version-1 JSON envelope and signs the exact request body as
`X-Telesrv-Signature: sha256=<HMAC-SHA256(timestamp + "." + body)>`. The bot
checks that signature, a five-minute timestamp window and `Idempotency-Key`.
For example, the body contains:

```json
{"version":"1","delivery_id":"...","purpose":"login_sms","channel":"sms","recipient":"+79991234567","code":"12345","expires_at":"2026-08-09T12:00:00Z","expires_in":300}
```

The endpoint is loopback-only by default. It rejects requests without the
HMAC secret, stores each delivery idempotently and returns HTTP 202 immediately;
Telegram delivery then runs asynchronously for the number owner and explicitly
granted support viewers. A slow Bot API therefore cannot turn `auth.sendCode`
into a server-side timeout. `/healthz` is read-only.

## Linux install

```bash
sudo useradd --system --home /var/lib/gramsrv-grammy-bot --shell /usr/sbin/nologin telesrv-bot || true
sudo install -d -o telesrv-bot -g telesrv-bot -m 0750 /opt/gramsrv-grammy-bot /var/lib/gramsrv-grammy-bot
sudo cp -a package.json package-lock.json src /opt/gramsrv-grammy-bot/
cd /opt/gramsrv-grammy-bot
sudo npm ci --omit=dev
sudo cp .env.example /etc/telesrv-grammy-bot.env
sudo chmod 0600 /etc/telesrv-grammy-bot.env
sudo cp deploy/telesrv-grammy-bot.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now telesrv-grammy-bot
sudo journalctl -u telesrv-grammy-bot -f
```

Use `BOT_DB_PATH=/var/lib/gramsrv-grammy-bot/bot.sqlite3` in the production env.
Back up the database with SQLite's online backup command or while the service is
stopped; include `/etc/telesrv-grammy-bot.env` in a separate encrypted secret
backup.

The systemd unit intentionally has no hosting-specific values. Do not deploy it
until the local branch has been reviewed and merged.
