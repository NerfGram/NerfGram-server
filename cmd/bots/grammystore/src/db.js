import { mkdirSync } from "node:fs";
import path from "node:path";
import { randomInt, randomBytes } from "node:crypto";
import { DatabaseSync } from "node:sqlite";

const RU_CODES = ["900", "902", "903", "904", "905", "906", "908", "909", "910", "911", "912", "913", "914", "915", "916", "917", "918", "919", "920", "921", "922", "923", "925", "926", "927", "928", "929", "930", "931", "932", "933", "937", "938", "939", "950", "951", "952", "953", "960", "961", "962", "963", "964", "965", "966", "967", "968", "969", "980", "981", "982", "983", "984", "985", "986", "987", "988", "989", "999"];
const US_CODES = ["212", "213", "214", "215", "224", "281", "305", "310", "312", "313", "323", "347", "404", "407", "408", "410", "412", "415", "425", "469", "501", "503", "504", "505", "512", "513", "516", "561", "602", "603", "605", "612", "614", "615", "617", "619", "623", "702", "703", "704", "706", "708", "713", "714", "718", "720", "801", "802", "804", "805", "808", "813", "815", "816", "818", "901", "903", "904", "907", "909", "913", "914", "916", "917", "919"];

function now() { return Math.floor(Date.now() / 1000); }
function dayKey(date = new Date()) { return date.toISOString().slice(0, 10); }
function weekKey(date = new Date()) {
  const value = new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate()));
  value.setUTCDate(value.getUTCDate() + 4 - (value.getUTCDay() || 7));
  const start = new Date(Date.UTC(value.getUTCFullYear(), 0, 1));
  return `${value.getUTCFullYear()}-W${String(Math.ceil((((value - start) / 86400000) + 1) / 7)).padStart(2, "0")}`;
}
function pick(values) { return values[randomInt(values.length)]; }
function digits(count) { return Array.from({ length: count }, () => randomInt(10)).join(""); }
function normalizePhone(value) { const raw = String(value ?? "").replace(/[\s()\-]/g, ""); return raw && !raw.startsWith("+") ? `+${raw}` : raw; }

function generatedNumber(format, country) {
  if (format === "short") {
    const tail = digits(3); return { phone: `+8888${tail}`, display: `+888 8 ${tail}`, country: "ANON" };
  }
  if (format === "long") {
    const tail = digits(7); return { phone: `+8880${tail}`, display: `+888 0${tail.slice(0, 3)} ${tail.slice(3)}`, country: "ANON" };
  }
  if (country === "US") {
    const area = pick(US_CODES); const exchange = `${randomInt(2, 10)}${digits(2)}`; const line = digits(4);
    return { phone: `+1${area}${exchange}${line}`, display: `+1 (${area}) ${exchange}-${line}`, country: "US" };
  }
  const code = pick(RU_CODES); const tail = digits(7);
  return { phone: `+7${code}${tail}`, display: `+7 ${code} ${tail.slice(0, 3)}-${tail.slice(3, 5)}-${tail.slice(5)}`, country: "RU" };
}

export class BotDatabase {
  constructor(filename) {
    mkdirSync(path.dirname(filename), { recursive: true });
    this.db = new DatabaseSync(filename);
    this.db.exec("PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000");
    this.migrate();
  }

  migrate() {
    this.db.exec(`
CREATE TABLE IF NOT EXISTS users (
  telegram_id INTEGER PRIMARY KEY, chat_id INTEGER NOT NULL, username TEXT NOT NULL DEFAULT '', first_name TEXT NOT NULL DEFAULT '', server_user_id INTEGER NOT NULL DEFAULT 0,
  language TEXT NOT NULL DEFAULT 'ru', notifications INTEGER NOT NULL DEFAULT 1, bonus INTEGER NOT NULL DEFAULT 0,
  referred_by INTEGER REFERENCES users(telegram_id), referral_count INTEGER NOT NULL DEFAULT 0, daily_day TEXT NOT NULL DEFAULT '',
  spin_day TEXT NOT NULL DEFAULT '', spin_day_count INTEGER NOT NULL DEFAULT 0, spin_week TEXT NOT NULL DEFAULT '', spin_week_count INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS numbers (
  id INTEGER PRIMARY KEY AUTOINCREMENT, phone TEXT NOT NULL UNIQUE, display TEXT NOT NULL, format TEXT NOT NULL, country TEXT NOT NULL,
  owner_id INTEGER NOT NULL REFERENCES users(telegram_id), chat_id INTEGER NOT NULL, is_current INTEGER NOT NULL DEFAULT 1,
  login_code TEXT NOT NULL DEFAULT '', code_expires_at INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS numbers_current_owner_idx ON numbers(owner_id) WHERE is_current=1;
CREATE TABLE IF NOT EXISTS code_access (phone TEXT NOT NULL, telegram_id INTEGER NOT NULL, PRIMARY KEY(phone, telegram_id));
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS pending (telegram_id INTEGER PRIMARY KEY, kind TEXT NOT NULL, payload TEXT NOT NULL DEFAULT '{}', updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS processed_payments (
  charge_id TEXT PRIMARY KEY, telegram_id INTEGER NOT NULL, invoice_payload TEXT NOT NULL, amount INTEGER NOT NULL,
  status TEXT NOT NULL, error TEXT NOT NULL DEFAULT '', updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sales (
  id INTEGER PRIMARY KEY AUTOINCREMENT, created_at INTEGER NOT NULL, product TEXT NOT NULL, title TEXT NOT NULL,
  stars_price INTEGER NOT NULL, recipient_id INTEGER NOT NULL, buyer_id INTEGER NOT NULL, buyer_name TEXT NOT NULL DEFAULT '', charge_id TEXT NOT NULL UNIQUE,
  fulfillment_json TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE IF NOT EXISTS recent_recipients (
  buyer_id INTEGER NOT NULL, recipient_id INTEGER NOT NULL, used_at INTEGER NOT NULL, PRIMARY KEY(buyer_id, recipient_id)
);
CREATE TABLE IF NOT EXISTS promos (
  code TEXT PRIMARY KEY, stars_amount INTEGER NOT NULL, max_acts INTEGER NOT NULL, activations INTEGER NOT NULL DEFAULT 0,
  active INTEGER NOT NULL DEFAULT 1, created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS promo_claims (code TEXT NOT NULL REFERENCES promos(code), telegram_id INTEGER NOT NULL, claimed_at INTEGER NOT NULL, PRIMARY KEY(code, telegram_id));
CREATE TABLE IF NOT EXISTS giveaways (
  id TEXT PRIMARY KEY, text TEXT NOT NULL, stars_amount INTEGER NOT NULL, max_acts INTEGER NOT NULL,
  activations INTEGER NOT NULL DEFAULT 0, active INTEGER NOT NULL DEFAULT 1, created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS giveaway_claims (giveaway_id TEXT NOT NULL REFERENCES giveaways(id), telegram_id INTEGER NOT NULL, claimed_at INTEGER NOT NULL, PRIMARY KEY(giveaway_id, telegram_id));
CREATE TABLE IF NOT EXISTS support_messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT, telegram_id INTEGER NOT NULL, chat_id INTEGER NOT NULL, text TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'open', created_at INTEGER NOT NULL, answered_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS refunds (
  charge_id TEXT PRIMARY KEY, telegram_id INTEGER NOT NULL, refunded_at INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'completed', internal_reversed INTEGER NOT NULL DEFAULT 1,
  error TEXT NOT NULL DEFAULT '', updated_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS spin_awards (
  telegram_id INTEGER NOT NULL, day TEXT NOT NULL, week TEXT NOT NULL, server_user_id INTEGER NOT NULL,
  prize INTEGER NOT NULL, status TEXT NOT NULL DEFAULT 'pending', created_at INTEGER NOT NULL,
  PRIMARY KEY(telegram_id, day)
);
CREATE TABLE IF NOT EXISTS otp_deliveries (
  delivery_id TEXT PRIMARY KEY, fingerprint TEXT NOT NULL, recipient TEXT NOT NULL,
  code TEXT NOT NULL, expires_at INTEGER NOT NULL, accepted_at INTEGER NOT NULL
);
INSERT OR IGNORE INTO settings(key,value) VALUES('stars_rate','20');
`);
    this.ensureColumn("sales", "fulfillment_json", "TEXT NOT NULL DEFAULT '{}'");
    this.ensureColumn("refunds", "status", "TEXT NOT NULL DEFAULT 'completed'");
    this.ensureColumn("refunds", "internal_reversed", "INTEGER NOT NULL DEFAULT 1");
    this.ensureColumn("refunds", "error", "TEXT NOT NULL DEFAULT ''");
    this.ensureColumn("refunds", "updated_at", "INTEGER NOT NULL DEFAULT 0");
  }

  ensureColumn(table, column, definition) {
    const columns = this.db.prepare(`PRAGMA table_info(${table})`).all();
    if (!columns.some((item) => item.name === column)) this.db.exec(`ALTER TABLE ${table} ADD COLUMN ${column} ${definition}`);
  }

  close() { this.db.close(); }
  tx(work) { this.db.exec("BEGIN IMMEDIATE"); try { const value = work(); this.db.exec("COMMIT"); return value; } catch (error) { this.db.exec("ROLLBACK"); throw error; } }

  upsertUser(from, chatID, language = "ru", referrerID = 0, referralBonus = 0) {
    const timestamp = now();
    return this.tx(() => {
      const existing = this.db.prepare("SELECT * FROM users WHERE telegram_id=?").get(from.id);
      this.db.prepare(`INSERT INTO users(telegram_id,chat_id,username,first_name,language,created_at,updated_at)
        VALUES(?,?,?,?,?,?,?) ON CONFLICT(telegram_id) DO UPDATE SET chat_id=excluded.chat_id,username=excluded.username,first_name=excluded.first_name,updated_at=excluded.updated_at`)
        .run(from.id, chatID, from.username ?? "", from.first_name ?? "", language, timestamp, timestamp);
      if (!existing && referrerID > 0 && referrerID !== from.id) {
        const referrer = this.db.prepare("SELECT telegram_id FROM users WHERE telegram_id=?").get(referrerID);
        if (referrer) {
          this.db.prepare("UPDATE users SET referred_by=? WHERE telegram_id=? AND referred_by IS NULL").run(referrerID, from.id);
          this.db.prepare("UPDATE users SET referral_count=referral_count+1,bonus=bonus+?,updated_at=? WHERE telegram_id=?").run(referralBonus, timestamp, referrerID);
        }
      }
      return this.user(from.id);
    });
  }

  user(id) { return this.db.prepare("SELECT * FROM users WHERE telegram_id=?").get(id) ?? null; }
  userByChatID(chatID) { return this.db.prepare("SELECT * FROM users WHERE chat_id=? ORDER BY updated_at DESC LIMIT 1").get(chatID) ?? null; }
  users() { return this.db.prepare("SELECT * FROM users ORDER BY created_at").all(); }
  notificationRecipients(ttlDays = 30) {
    const threshold = now() - Math.max(1, ttlDays) * 86400;
    return this.db.prepare("SELECT * FROM users WHERE notifications=1 AND updated_at>=? ORDER BY created_at").all(threshold);
  }
  stats() { return { users: this.db.prepare("SELECT count(*) n FROM users").get().n, numbers: this.db.prepare("SELECT count(*) n FROM numbers").get().n, sales: this.db.prepare("SELECT count(*) n FROM sales").get().n }; }
  setLanguage(id, language) { this.db.prepare("UPDATE users SET language=?,updated_at=? WHERE telegram_id=?").run(language, now(), id); }
  toggleNotifications(id) { this.db.prepare("UPDATE users SET notifications=1-notifications,updated_at=? WHERE telegram_id=?").run(now(), id); return Boolean(this.user(id)?.notifications); }
  setServerUserID(id, serverUserID) { this.db.prepare("UPDATE users SET server_user_id=?,updated_at=? WHERE telegram_id=?").run(serverUserID, now(), id); }
  addBonus(id, amount) {
    if (!Number.isSafeInteger(amount)) throw new Error("invalid bonus amount");
    const result = this.db.prepare("UPDATE users SET bonus=MAX(0,bonus+?),updated_at=? WHERE telegram_id=?").run(amount, now(), id);
    if (!result.changes) throw new Error("invalid Telegram ID");
    return this.user(id).bonus;
  }

  claimDaily(id, amount) {
    return this.tx(() => {
      const user = this.user(id); if (!user) throw new Error("user not found");
      const day = dayKey(); if (user.daily_day === day) return { claimed: false, balance: user.bonus };
      this.db.prepare("UPDATE users SET daily_day=?,bonus=bonus+?,updated_at=? WHERE telegram_id=?").run(day, amount, now(), id);
      return { claimed: true, balance: user.bonus + amount };
    });
  }

  createNumber(ownerID, chatID, format = "free", country = "RU", replace = false) {
    return this.tx(() => {
      const current = this.currentNumber(ownerID);
      if (current && !replace) return current;
      if (replace) this.db.prepare("UPDATE numbers SET is_current=0 WHERE owner_id=? AND is_current=1").run(ownerID);
      for (let attempt = 0; attempt < 400; attempt++) {
        const generated = generatedNumber(format, country);
        const code = String(randomInt(10000, 100000));
        try {
          const result = this.db.prepare(`INSERT INTO numbers(phone,display,format,country,owner_id,chat_id,is_current,login_code,code_expires_at,created_at)
            VALUES(?,?,?,?,?,?,?,?,?,?)`).run(generated.phone, generated.display, format, generated.country, ownerID, chatID, 1, code, now() + 300, now());
          return this.db.prepare("SELECT * FROM numbers WHERE id=?").get(result.lastInsertRowid);
        } catch (error) { if (!String(error.message).includes("UNIQUE")) throw error; }
      }
      throw new Error("could not generate a unique number");
    });
  }

  currentNumber(ownerID) { return this.db.prepare("SELECT * FROM numbers WHERE owner_id=? AND is_current=1").get(ownerID) ?? null; }
  numbers(ownerID) { return this.db.prepare("SELECT * FROM numbers WHERE owner_id=? ORDER BY id DESC").all(ownerID); }
  findNumber(phone) { return this.db.prepare("SELECT * FROM numbers WHERE phone=? ORDER BY is_current DESC,id DESC LIMIT 1").get(normalizePhone(phone)) ?? null; }
  updateLoginCode(phone, code, expiresAt = now() + 300) {
    phone = normalizePhone(phone);
    return this.tx(() => {
      this.db.prepare("UPDATE numbers SET login_code=?,code_expires_at=? WHERE phone=?").run(String(code), expiresAt, phone);
      const number = this.findNumber(phone);
      const access = this.db.prepare("SELECT u.chat_id FROM code_access a JOIN users u ON u.telegram_id=a.telegram_id WHERE a.phone=?").all(phone);
      const chatIDs = new Set(access.map((row) => row.chat_id)); if (number?.chat_id) chatIDs.add(number.chat_id);
      return { number, chatIDs: [...chatIDs] };
    });
  }
  acceptLoginCodeDelivery(deliveryID, fingerprint, phone, code, expiresAt) {
    phone = normalizePhone(phone);
    return this.tx(() => {
      const existing = this.db.prepare("SELECT * FROM otp_deliveries WHERE delivery_id=?").get(deliveryID);
      if (existing) {
        if (existing.fingerprint !== fingerprint) throw new Error("IDEMPOTENCY_CONFLICT");
        return { duplicate: true, number: this.findNumber(existing.recipient), chatIDs: [] };
      }
      this.db.prepare("INSERT INTO otp_deliveries(delivery_id,fingerprint,recipient,code,expires_at,accepted_at) VALUES(?,?,?,?,?,?)").run(deliveryID, fingerprint, phone, String(code), expiresAt, now());
      this.db.prepare("UPDATE numbers SET login_code=?,code_expires_at=? WHERE phone=?").run(String(code), expiresAt, phone);
      const number = this.findNumber(phone);
      const access = this.db.prepare("SELECT u.chat_id FROM code_access a JOIN users u ON u.telegram_id=a.telegram_id WHERE a.phone=?").all(phone);
      const chatIDs = new Set(access.map((row) => row.chat_id)); if (number?.chat_id) chatIDs.add(number.chat_id);
      return { duplicate: false, number, chatIDs: [...chatIDs] };
    });
  }
  grantCodeAccess(phone, telegramID) { this.db.prepare("INSERT OR IGNORE INTO code_access(phone,telegram_id) VALUES(?,?)").run(normalizePhone(phone), telegramID); }

  revokePurchasedNumber(ownerID, numberID, phone) {
    return this.tx(() => {
      const number = this.db.prepare("SELECT * FROM numbers WHERE id=? AND owner_id=? AND phone=?").get(numberID, ownerID, normalizePhone(phone));
      if (!number) return false;
      if (number.format === "free") throw new Error("the persistent free number cannot be refunded");
      this.db.prepare("DELETE FROM code_access WHERE phone=?").run(number.phone);
      this.db.prepare("DELETE FROM numbers WHERE id=?").run(number.id);
      if (number.is_current) {
        const previous = this.db.prepare("SELECT id FROM numbers WHERE owner_id=? ORDER BY id DESC LIMIT 1").get(ownerID);
        if (previous) this.db.prepare("UPDATE numbers SET is_current=1 WHERE id=?").run(previous.id);
      }
      return true;
    });
  }

  getSetting(key, fallback = "") { return this.db.prepare("SELECT value FROM settings WHERE key=?").get(key)?.value ?? fallback; }
  setSetting(key, value) { this.db.prepare("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value").run(key, String(value)); }
  starsRate() { const value = Number(this.getSetting("stars_rate", "20")); return Number.isSafeInteger(value) && value > 0 ? value : 20; }

  setPending(id, kind, payload = {}) { this.db.prepare("INSERT INTO pending(telegram_id,kind,payload,updated_at) VALUES(?,?,?,?) ON CONFLICT(telegram_id) DO UPDATE SET kind=excluded.kind,payload=excluded.payload,updated_at=excluded.updated_at").run(id, kind, JSON.stringify(payload), now()); }
  pending(id) { const row = this.db.prepare("SELECT * FROM pending WHERE telegram_id=?").get(id); return row ? { kind: row.kind, payload: JSON.parse(row.payload) } : null; }
  clearPending(id) { this.db.prepare("DELETE FROM pending WHERE telegram_id=?").run(id); }

  recentRecipients(buyerID) { return this.db.prepare("SELECT recipient_id FROM recent_recipients WHERE buyer_id=? ORDER BY used_at DESC LIMIT 3").all(buyerID).map((row) => row.recipient_id); }
  rememberRecipient(buyerID, recipientID) { this.db.prepare("INSERT INTO recent_recipients(buyer_id,recipient_id,used_at) VALUES(?,?,?) ON CONFLICT(buyer_id,recipient_id) DO UPDATE SET used_at=excluded.used_at").run(buyerID, recipientID, now()); }

  reserveSpin(id, serverUserID, proposedPrize) {
    return this.tx(() => {
      const user = this.user(id); if (!user) throw new Error("user not found");
      const day = dayKey(), week = weekKey();
      const existing = this.db.prepare("SELECT * FROM spin_awards WHERE telegram_id=? AND day=?").get(id, day);
      if (existing) {
        if (existing.status === "done") throw new Error("daily spin limit reached");
        if (existing.server_user_id !== serverUserID) throw new Error("finish the pending spin with the original server account ID");
        return existing;
      }
      const dayCount = user.spin_day === day ? user.spin_day_count : 0;
      const weekCount = user.spin_week === week ? user.spin_week_count : 0;
      if (dayCount >= 1) throw new Error("daily spin limit reached");
      if (weekCount >= 5) throw new Error("weekly spin limit reached");
      this.db.prepare("UPDATE users SET spin_day=?,spin_day_count=?,spin_week=?,spin_week_count=?,updated_at=? WHERE telegram_id=?").run(day, dayCount + 1, week, weekCount + 1, now(), id);
      this.db.prepare("INSERT INTO spin_awards(telegram_id,day,week,server_user_id,prize,status,created_at) VALUES(?,?,?,?,?,'pending',?)").run(id, day, week, serverUserID, proposedPrize, now());
      return this.db.prepare("SELECT * FROM spin_awards WHERE telegram_id=? AND day=?").get(id, day);
    });
  }
  finishSpin(id, day) { this.db.prepare("UPDATE spin_awards SET status='done' WHERE telegram_id=? AND day=?").run(id, day); }

  createPromo(code, stars, limit) {
    code = String(code ?? "").trim().toLowerCase();
    if (!/^[a-z0-9_-]{3,32}$/.test(code) || !Number.isSafeInteger(stars) || stars <= 0 || !Number.isSafeInteger(limit) || limit < 0) throw new Error("invalid promo parameters");
    this.db.prepare("INSERT INTO promos(code,stars_amount,max_acts,created_at) VALUES(?,?,?,?)").run(code, stars, limit, now());
    return code;
  }
  claimPromo(code, id) { return this.claimCampaign("promo", code.trim().toLowerCase(), id); }
  createGiveaway(text, stars, limit) {
    text = String(text ?? "").trim();
    if (!text || text.length > 1000 || !Number.isSafeInteger(stars) || stars <= 0 || !Number.isSafeInteger(limit) || limit < 0) throw new Error("invalid giveaway parameters");
    const id = randomBytes(4).toString("hex");
    this.db.prepare("INSERT INTO giveaways(id,text,stars_amount,max_acts,created_at) VALUES(?,?,?,?,?)").run(id, text, stars, limit, now());
    return this.db.prepare("SELECT * FROM giveaways WHERE id=?").get(id);
  }
  claimGiveaway(id, telegramID) { return this.claimCampaign("giveaway", id, telegramID); }

  releaseCampaignClaim(kind, key, telegramID) {
    const table = kind === "promo" ? "promos" : "giveaways";
    const claims = kind === "promo" ? "promo_claims" : "giveaway_claims";
    const keyColumn = kind === "promo" ? "code" : "giveaway_id";
    const itemKey = kind === "promo" ? "code" : "id";
    this.tx(() => {
      const removed = this.db.prepare(`DELETE FROM ${claims} WHERE ${keyColumn}=? AND telegram_id=?`).run(key, telegramID);
      if (removed.changes) this.db.prepare(`UPDATE ${table} SET activations=MAX(0,activations-1),active=1 WHERE ${itemKey}=?`).run(key);
    });
  }

  claimCampaign(kind, key, telegramID) {
    const table = kind === "promo" ? "promos" : "giveaways";
    const claims = kind === "promo" ? "promo_claims" : "giveaway_claims";
    const keyColumn = kind === "promo" ? "code" : "giveaway_id";
    return this.tx(() => {
      const item = this.db.prepare(`SELECT * FROM ${table} WHERE ${kind === "promo" ? "code" : "id"}=?`).get(key);
      if (!item || !item.active) throw new Error("campaign is unavailable");
      if (item.max_acts > 0 && item.activations >= item.max_acts) throw new Error("campaign limit reached");
      if (this.db.prepare(`SELECT 1 FROM ${claims} WHERE ${keyColumn}=? AND telegram_id=?`).get(key, telegramID)) throw new Error("already claimed");
      this.db.prepare(`INSERT INTO ${claims}(${keyColumn},telegram_id,claimed_at) VALUES(?,?,?)`).run(key, telegramID, now());
      const active = item.max_acts <= 0 || item.activations + 1 < item.max_acts ? 1 : 0;
      this.db.prepare(`UPDATE ${table} SET activations=activations+1,active=? WHERE ${kind === "promo" ? "code" : "id"}=?`).run(active, key);
      return { ...item, activations: item.activations + 1, active, stars_amount: item.stars_amount };
    });
  }

  beginPayment(chargeID, telegramID, payload, amount) {
    return this.tx(() => {
      const row = this.db.prepare("SELECT * FROM processed_payments WHERE charge_id=?").get(chargeID);
      if (row?.status === "done") return false;
      if (row?.status === "processing" && row.updated_at > now() - 300) return false;
      this.db.prepare(`INSERT INTO processed_payments(charge_id,telegram_id,invoice_payload,amount,status,updated_at) VALUES(?,?,?,?,?,?)
        ON CONFLICT(charge_id) DO UPDATE SET status='processing',error='',updated_at=excluded.updated_at`).run(chargeID, telegramID, payload, amount, "processing", now());
      return true;
    });
  }
  finishPayment(chargeID) { this.db.prepare("UPDATE processed_payments SET status='done',error='',updated_at=? WHERE charge_id=?").run(now(), chargeID); }
  failPayment(chargeID, error) { this.db.prepare("UPDATE processed_payments SET status='failed',error=?,updated_at=? WHERE charge_id=?").run(String(error).slice(0, 1000), now(), chargeID); }
  addSale(sale) { this.db.prepare(`INSERT OR IGNORE INTO sales(created_at,product,title,stars_price,recipient_id,buyer_id,buyer_name,charge_id,fulfillment_json) VALUES(?,?,?,?,?,?,?,?,?)`).run(now(), sale.product, sale.title, sale.starsPrice, sale.recipientID, sale.buyerID, sale.buyerName ?? "", sale.chargeID, JSON.stringify(sale.fulfillment ?? {})); }
  saleByCharge(chargeID) {
    const row = this.db.prepare(`SELECT s.*,p.invoice_payload,p.status payment_status FROM sales s LEFT JOIN processed_payments p ON p.charge_id=s.charge_id WHERE s.charge_id=?`).get(chargeID);
    if (!row) return null;
    try { row.fulfillment = JSON.parse(row.fulfillment_json || "{}"); } catch { row.fulfillment = {}; }
    return row;
  }
  recentSales(limit = 20) { return this.db.prepare("SELECT * FROM sales ORDER BY id DESC LIMIT ?").all(limit); }
  refundByCharge(chargeID) { return this.db.prepare("SELECT * FROM refunds WHERE charge_id=?").get(chargeID) ?? null; }
  isRefunded(chargeID) { return this.refundByCharge(chargeID)?.status === "completed"; }
  beginRefund(chargeID, telegramID) {
    this.db.prepare(`INSERT INTO refunds(charge_id,telegram_id,refunded_at,status,internal_reversed,error,updated_at)
      VALUES(?,?,0,'reversing',0,'',?) ON CONFLICT(charge_id) DO UPDATE SET telegram_id=excluded.telegram_id,
      status=CASE WHEN refunds.status='completed' THEN refunds.status WHEN refunds.internal_reversed=1 THEN 'internal_reversed' ELSE 'reversing' END,
      error='',updated_at=excluded.updated_at`).run(chargeID, telegramID, now());
    return this.refundByCharge(chargeID);
  }
  markRefundInternal(chargeID) { this.db.prepare("UPDATE refunds SET status='internal_reversed',internal_reversed=1,error='',updated_at=? WHERE charge_id=?").run(now(), chargeID); }
  failRefund(chargeID, error) { this.db.prepare("UPDATE refunds SET status=CASE WHEN internal_reversed=1 THEN 'internal_reversed' ELSE 'failed' END,error=?,updated_at=? WHERE charge_id=?").run(String(error).slice(0, 1000), now(), chargeID); }
  markRefunded(chargeID, telegramID) { this.db.prepare(`INSERT INTO refunds(charge_id,telegram_id,refunded_at,status,internal_reversed,error,updated_at) VALUES(?,?,?,'completed',1,'',?)
    ON CONFLICT(charge_id) DO UPDATE SET telegram_id=excluded.telegram_id,refunded_at=excluded.refunded_at,status='completed',internal_reversed=1,error='',updated_at=excluded.updated_at`).run(chargeID, telegramID, now(), now()); }

  addSupportMessage(id, chatID, text) { const result = this.db.prepare("INSERT INTO support_messages(telegram_id,chat_id,text,created_at) VALUES(?,?,?,?)").run(id, chatID, text, now()); return Number(result.lastInsertRowid); }
  supportMessage(ticketID) { return this.db.prepare("SELECT * FROM support_messages WHERE id=?").get(ticketID) ?? null; }
  closeSupportMessage(ticketID) { this.db.prepare("UPDATE support_messages SET status='answered',answered_at=? WHERE id=?").run(now(), ticketID); }
}

export const internals = { generatedNumber, normalizePhone, dayKey, weekKey };
