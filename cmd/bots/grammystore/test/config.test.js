import test from "node:test";
import assert from "node:assert/strict";
import { loadConfig } from "../src/config.js";

const managedEnv = [
  "BOT_TOKEN",
  "OWNER_IDS",
  "GRAMSRV_TOKEN",
  "CODE_WEBHOOK_SECRET",
  "DEFAULT_LANGUAGE",
  "DEFAULT_NUMBER_COUNTRY",
  "REFERRAL_BONUS",
  "DAILY_BONUS",
  "NOTIFICATION_TTL_DAYS",
];

function withEnv(values, fn) {
  const previous = new Map(managedEnv.map((key) => [key, process.env[key]]));
  for (const key of managedEnv) delete process.env[key];
  Object.assign(process.env, values);
  try {
    return fn();
  } finally {
    for (const key of managedEnv) {
      if (previous.get(key) === undefined) delete process.env[key];
      else process.env[key] = previous.get(key);
    }
  }
}

test("loadConfig supplies defaults required by runtime bot flows", () => {
  const config = withEnv({
    BOT_TOKEN: "999:TEST",
    OWNER_IDS: "1",
    GRAMSRV_TOKEN: "token",
    CODE_WEBHOOK_SECRET: "abcdefghijklmnopqrstuvwxyz",
  }, () => loadConfig());

  assert.equal(config.defaultLanguage, "ru");
  assert.equal(config.referralBonus, 100);
  assert.equal(config.dailyBonus, 15);
  assert.equal(config.notificationTTLDays, 30);
});

test("loadConfig validates bonus and notification overrides", () => {
  const config = withEnv({
    BOT_TOKEN: "999:TEST",
    OWNER_IDS: "1",
    GRAMSRV_TOKEN: "token",
    CODE_WEBHOOK_SECRET: "abcdefghijklmnopqrstuvwxyz",
    DEFAULT_LANGUAGE: "en",
    REFERRAL_BONUS: "7",
    DAILY_BONUS: "3",
    NOTIFICATION_TTL_DAYS: "9",
  }, () => loadConfig());

  assert.equal(config.defaultLanguage, "en");
  assert.equal(config.referralBonus, 7);
  assert.equal(config.dailyBonus, 3);
  assert.equal(config.notificationTTLDays, 9);
});
