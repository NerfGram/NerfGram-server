import { existsSync } from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { DatabaseSync } from "node:sqlite";
import { BotDatabase } from "../src/db.js";

function dayFromUnix(value) {
  const seconds = Number(value);
  return Number.isSafeInteger(seconds) && seconds > 0 ? new Date(seconds * 1000).toISOString().slice(0, 10) : "";
}

function countryFor(phone) {
  if (phone.startsWith("+888")) return "ANON";
  if (phone.startsWith("+1")) return "US";
  return "RU";
}

function requireLegacySchema(db) {
  const columns = db.prepare("PRAGMA table_info(users)").all().map((row) => row.name);
  if (!columns.includes("tg_id") || columns.includes("telegram_id")) {
    throw new Error("source database is not the supported legacy bot schema");
  }
}

export function migrateLegacy(sourcePath, destinationPath) {
  sourcePath = path.resolve(sourcePath);
  destinationPath = path.resolve(destinationPath);
  if (sourcePath === destinationPath) throw new Error("source and destination must be different files");
  if (!existsSync(sourcePath)) throw new Error(`source database does not exist: ${sourcePath}`);
  if (existsSync(destinationPath)) throw new Error(`destination already exists: ${destinationPath}`);

  const legacy = new DatabaseSync(sourcePath, { readOnly: true });
  let target;
  try {
    requireLegacySchema(legacy);
    target = new BotDatabase(destinationPath);
    const users = legacy.prepare("SELECT * FROM users ORDER BY created_at,tg_id").all();
    const userIDs = new Set(users.map((row) => Number(row.tg_id)));

    target.tx(() => {
      const insertUser = target.db.prepare(`INSERT INTO users(
        telegram_id,chat_id,username,first_name,server_user_id,language,notifications,bonus,
        referred_by,referral_count,daily_day,created_at,updated_at
      ) VALUES(?,?,?,?,0,?,?,?,?,?,?,?,?)`);
      for (const row of users) {
        insertUser.run(
          Number(row.tg_id), Number(row.tg_id), row.username ?? "", row.first_name ?? "",
          row.language === "en" ? "en" : "ru", Number(row.notifications) ? 1 : 0,
          Math.max(0, Number(row.bonuses) || 0), null, Math.max(0, Number(row.referrals_count) || 0),
          dayFromUnix(row.last_daily_at), Number(row.created_at) || 0, Number(row.updated_at) || Number(row.created_at) || 0,
        );
      }
      const setReferrer = target.db.prepare("UPDATE users SET referred_by=? WHERE telegram_id=?");
      for (const row of users) {
        const referrer = Number(row.referrer_id);
        if (referrer > 0 && referrer !== Number(row.tg_id) && userIDs.has(referrer)) setReferrer.run(referrer, Number(row.tg_id));
      }

      const activeOwners = new Set();
      const insertNumber = target.db.prepare(`INSERT INTO numbers(
        phone,display,format,country,owner_id,chat_id,is_current,login_code,code_expires_at,created_at
      ) VALUES(?,?,?,?,?,?,?,?,?,?)`);
      const oldNumbers = legacy.prepare("SELECT * FROM numbers ORDER BY active DESC,created_at DESC,phone").all();
      const oldUsers = new Map(users.map((row) => [Number(row.tg_id), row]));
      for (const row of oldNumbers) {
        const ownerID = Number(row.tg_id);
        if (!userIDs.has(ownerID)) continue;
        const current = Number(row.active) !== 0 && !activeOwners.has(ownerID);
        if (current) activeOwners.add(ownerID);
        const oldUser = oldUsers.get(ownerID);
        const code = current ? String(oldUser?.latest_code ?? "") : "";
        const expires = current ? Math.max(0, Number(oldUser?.code_expires_at) || 0) : 0;
        const phone = String(row.phone);
        insertNumber.run(phone, phone, "legacy", countryFor(phone), ownerID, ownerID, current ? 1 : 0, code, expires, Number(row.created_at) || 0);
      }

      const insertSupport = target.db.prepare(`INSERT INTO support_messages(
        id,telegram_id,chat_id,text,status,created_at,answered_at
      ) VALUES(?,?,?,?,?,?,0)`);
      for (const row of legacy.prepare("SELECT * FROM support_messages ORDER BY id").all()) {
        const telegramID = Number(row.tg_id);
        if (userIDs.has(telegramID)) insertSupport.run(Number(row.id), telegramID, telegramID, row.text ?? "", row.status ?? "open", Number(row.created_at) || 0);
      }
    });

    // Leave a self-contained database file for the handoff. BotDatabase will
    // switch it back to WAL mode when the service first opens it.
    target.db.exec("PRAGMA wal_checkpoint(TRUNCATE); PRAGMA journal_mode=DELETE");
    return {
      users: target.db.prepare("SELECT count(*) count FROM users").get().count,
      numbers: target.db.prepare("SELECT count(*) count FROM numbers").get().count,
      supportMessages: target.db.prepare("SELECT count(*) count FROM support_messages").get().count,
    };
  } finally {
    target?.close();
    legacy.close();
  }
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  const [, , source, destination] = process.argv;
  if (!source || !destination) {
    console.error("usage: node scripts/migrate-legacy-db.js LEGACY_DB NEW_DB");
    process.exitCode = 2;
  } else {
    console.log(JSON.stringify(migrateLegacy(source, destination)));
  }
}
