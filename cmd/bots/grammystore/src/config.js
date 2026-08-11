import "dotenv/config";
import path from "node:path";

function required(name) {
  const value = (process.env[name] ?? "").trim();
  if (!value) throw new Error(`${name} is required`);
  return value;
}

function integer(name, fallback, { min = Number.MIN_SAFE_INTEGER } = {}) {
  const raw = (process.env[name] ?? "").trim();
  const value = raw ? Number(raw) : fallback;
  if (!Number.isSafeInteger(value) || value < min) throw new Error(`${name} must be an integer >= ${min}`);
  return value;
}

function ownerIDs() {
  const ids = new Set();
  for (const raw of required("OWNER_IDS").split(",")) {
    const id = Number(raw.trim());
    if (!Number.isSafeInteger(id) || id <= 0) throw new Error("OWNER_IDS contains an invalid Telegram user ID");
    ids.add(id);
  }
  return ids;
}

export function loadConfig() {
  const webhookSecret = required("CODE_WEBHOOK_SECRET");
  if (webhookSecret.length < 24) throw new Error("CODE_WEBHOOK_SECRET must contain at least 24 characters");
  return Object.freeze({
    botToken: required("BOT_TOKEN"),
    productName: (process.env.PRODUCT_NAME ?? "Telesrv").trim() || "Telesrv",
    ownerIDs: ownerIDs(),
    publicUsername: (process.env.BOT_PUBLIC_USERNAME ?? "").replace(/^@/, "").trim(),
    gramsrvAPI: (process.env.GRAMSRV_API ?? "http://127.0.0.1:2399").replace(/\/+$/, ""),
    gramsrvToken: required("GRAMSRV_TOKEN"),
    gramsrvActor: (process.env.GRAMSRV_ACTOR ?? "telesrv-grammy-bot").trim(),
    publicBaseURL: (process.env.PUBLIC_BASE_URL ?? "https://example.com").replace(/\/+$/, ""),
    dbPath: path.resolve(process.env.BOT_DB_PATH ?? "./data/bot.sqlite3"),
    codeHost: (process.env.CODE_HTTP_HOST ?? "127.0.0.1").trim(),
    codePort: integer("CODE_HTTP_PORT", 2800, { min: 1 }),
    codeWebhookSecret: webhookSecret,
    requiredChannel: (process.env.REQUIRED_CHANNEL ?? "").trim(),
    requiredChannelURL: (process.env.REQUIRED_CHANNEL_URL ?? "").trim(),
    defaultLanguage: (process.env.DEFAULT_LANGUAGE ?? "ru").toLowerCase() === "en" ? "en" : "ru",
    defaultNumberCountry: (process.env.DEFAULT_NUMBER_COUNTRY ?? "RU").toUpperCase() === "US" ? "US" : "RU",
    referralBonus: integer("REFERRAL_BONUS", 100, { min: 0 }),
    dailyBonus: integer("DAILY_BONUS", 15, { min: 0 }),
    notificationTTLDays: integer("NOTIFICATION_TTL_DAYS", 30, { min: 1 }),
  });
}
