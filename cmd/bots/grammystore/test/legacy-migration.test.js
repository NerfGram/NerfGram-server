import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { DatabaseSync } from "node:sqlite";
import { migrateLegacy } from "../scripts/migrate-legacy-db.js";
import { BotDatabase } from "../src/db.js";

test("legacy Python database is migrated without modifying the source", (t) => {
  const dir = mkdtempSync(path.join(tmpdir(), "telesrv-legacy-"));
  t.after(() => rmSync(dir, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 }));
  const source = path.join(dir, "old.sqlite3");
  const destination = path.join(dir, "new.sqlite3");
  const old = new DatabaseSync(source);
  old.exec(`
      CREATE TABLE users(tg_id INTEGER PRIMARY KEY,username TEXT,first_name TEXT,phone TEXT,latest_code TEXT,code_expires_at INTEGER,bonuses INTEGER,referrer_id INTEGER,referrals_count INTEGER,language TEXT,notifications INTEGER,is_admin INTEGER,state TEXT,created_at INTEGER,updated_at INTEGER,last_daily_at INTEGER,number_changed_at INTEGER);
      CREATE TABLE numbers(phone TEXT PRIMARY KEY,tg_id INTEGER,active INTEGER,created_at INTEGER,retired_at INTEGER);
      CREATE TABLE support_messages(id INTEGER PRIMARY KEY,tg_id INTEGER,text TEXT,status TEXT,created_at INTEGER);
      INSERT INTO users VALUES(10,'owner','Owner','+79990000001','12345',2000,70,NULL,1,'ru',1,1,'',100,200,86400,0);
      INSERT INTO users VALUES(11,'friend','Friend','+18880000001','54321',3000,5,10,0,'en',0,0,'',110,210,0,0);
      INSERT INTO numbers VALUES('+79990000001',10,1,100,NULL);
      INSERT INTO numbers VALUES('+18880000001',11,1,110,NULL);
      INSERT INTO support_messages VALUES(7,11,'help','open',120);
  `);
  old.close();
  const sourceBefore = readFileSync(source);

  assert.deepEqual(migrateLegacy(source, destination), { users: 2, numbers: 2, supportMessages: 1 });
  const migrated = new BotDatabase(destination);
  assert.deepEqual(migrated.db.prepare("SELECT telegram_id,bonus,referred_by,language,notifications,daily_day FROM users ORDER BY telegram_id").all().map((row) => ({ ...row })), [
    { telegram_id: 10, bonus: 70, referred_by: null, language: "ru", notifications: 1, daily_day: "1970-01-02" },
    { telegram_id: 11, bonus: 5, referred_by: 10, language: "en", notifications: 0, daily_day: "" },
  ]);
  assert.deepEqual(migrated.db.prepare("SELECT phone,owner_id,is_current,login_code FROM numbers ORDER BY owner_id").all().map((row) => ({ ...row })), [
    { phone: "+79990000001", owner_id: 10, is_current: 1, login_code: "12345" },
    { phone: "+18880000001", owner_id: 11, is_current: 1, login_code: "54321" },
  ]);
  assert.equal(migrated.db.prepare("SELECT text FROM support_messages WHERE id=7").get().text, "help");
  migrated.close();
  assert.deepEqual(readFileSync(source), sourceBefore);
  assert.throws(() => migrateLegacy(source, destination), /destination already exists/);
});
