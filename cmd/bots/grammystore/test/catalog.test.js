import test from "node:test";
import assert from "node:assert/strict";
import { buildPayload, findProduct, localizeProduct, normalizeUsername, parsePayload } from "../src/catalog.js";

test("invoice payload round-trips unicode extras and a Stars snapshot", () => {
  assert.deepEqual(parsePayload(buildPayload("uname_10", 123, "юзер_name")), { code: "uname_10", targetUserID: 123, extra: "юзер_name", starsAmount: 0 });
  assert.deepEqual(parsePayload(buildPayload("stars_1", 456, "", 20)), { code: "stars_1", targetUserID: 456, extra: "", starsAmount: 20 });
  assert.deepEqual(parsePayload("store|stars_1|456|"), { code: "stars_1", targetUserID: 456, extra: "", starsAmount: 0 });
});

test("dynamic Stars products use configured rate", () => {
  assert.equal(findProduct("stars_25", 30).starsAmount, 750);
});

test("arbitrary Stars invoices are bounded and use the configured rate", () => {
  assert.equal(findProduct("stars_37", 25).starsAmount, 925);
  assert.equal(findProduct("stars_100000", 25).starsAmount, 2_500_000);
  assert.equal(findProduct("stars_100001", 25), null);
  assert.equal(findProduct("stars_0", 25), null);
});

test("collectible username validation is canonical", () => {
  assert.equal(normalizeUsername(" @Valid_Name "), "valid_name");
  assert.equal(normalizeUsername("1bad"), "");
});

test("fixed and dynamic products expose both supported locales", () => {
  assert.equal(localizeProduct(findProduct("premium_3m"), "ru").title, "Premium — 3 месяца");
  assert.equal(localizeProduct(findProduct("stars_37", 25), "ru").description, "925 серверных Stars за 37 Telegram Stars");
  assert.equal(localizeProduct(findProduct("stars_37", 25), "en").description, "925 server Stars for 37 Telegram Stars");
});
