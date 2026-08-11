import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { createBot } from "../src/bot.js";
import { BotDatabase } from "../src/db.js";

const botInfo = { id: 999, is_bot: true, first_name: "Test", username: "test_bot", can_join_groups: true, can_read_all_group_messages: false, supports_inline_queries: false };

function fixture(t) {
  const dir = mkdtempSync(path.join(tmpdir(), "telesrv-bot-flow-"));
  const db = new BotDatabase(path.join(dir, "bot.sqlite3"));
  const calls = [];
  const config = {
    botToken: "999:TEST",
    defaultLanguage: "ru",
    defaultNumberCountry: "RU",
    ownerIDs: new Set(),
    requiredChannel: "",
    requiredChannelURL: "",
    referralBonus: 100,
    dailyBonus: 10,
    notificationTTLDays: 30,
    productName: "Telesrv",
    publicUsername: "test_bot",
  };
  const gramsrv = {};
  const bot = createBot({ config, db, gramsrv });
  bot.botInfo = botInfo;
  bot.api.config.use(async (_previous, method, payload) => {
    calls.push({ method, payload });
    if (method === "answerCallbackQuery") return { ok: true, result: true };
    return {
      ok: true,
      result: { message_id: 100, date: 1, chat: { id: payload.chat_id ?? 1, type: "private" }, text: payload.text ?? "" },
    };
  });
  t.after(() => { db.close(); rmSync(dir, { recursive: true, force: true }); });
  return { bot, calls, config, db };
}

test("English language callback persists and redraws all settings controls in English", async (t) => {
  const { bot, calls, db } = fixture(t);
  db.upsertUser({ id: 10, first_name: "User", language_code: "ru" }, 10, "ru");
  await bot.handleUpdate({
    update_id: 1,
    callback_query: {
      id: "callback-1",
      from: { id: 10, is_bot: false, first_name: "User", language_code: "ru" },
      chat_instance: "instance",
      data: "settings:lang:en",
      message: { message_id: 1, date: 1, chat: { id: 10, type: "private" }, text: "Настройки" },
    },
  });
  assert.equal(db.user(10).language, "en");
  const answer = calls.find((call) => call.method === "answerCallbackQuery");
  assert.equal(answer.payload.text, "Language switched to English.");
  const edit = calls.find((call) => call.method === "editMessageText");
  assert.match(edit.payload.text, /Settings/);
  const labels = edit.payload.reply_markup.inline_keyboard.flat().map((button) => button.text).join("\n");
  assert.match(labels, /Account/);
  assert.match(labels, /Notifications: on/);
  assert.doesNotMatch(labels, /[А-Яа-яЁё]/u);
});

test("first /start applies referral before generic user registration", async (t) => {
  const { bot, calls, db } = fixture(t);
  db.upsertUser({ id: 1, first_name: "Referrer" }, 1, "ru");
  await bot.handleUpdate({
    update_id: 2,
    message: {
      message_id: 2,
      date: 1,
      chat: { id: 2, type: "private" },
      from: { id: 2, is_bot: false, first_name: "Guest", language_code: "en" },
      text: "/start ref_1",
      entities: [{ offset: 0, length: 6, type: "bot_command" }],
    },
  });
  assert.equal(db.user(2).referred_by, 1);
  assert.equal(db.user(1).referral_count, 1);
  assert.equal(db.user(1).bonus, 100);
  const sent = calls.find((call) => call.method === "sendMessage");
  assert.match(sent.payload.text, /Welcome!/);
  assert.match(sent.payload.text, /Referral invitation applied/);
});
