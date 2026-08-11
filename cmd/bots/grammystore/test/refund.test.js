import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { BotDatabase } from "../src/db.js";
import { executeCompensatedRefund, fulfillmentForSale, reverseSaleFulfillment } from "../src/bot.js";

test("legacy Stars sale resolves the exact granted amount from its title", () => {
  const item = fulfillmentForSale({
    product: "stars_1", title: "20 LegacyBrand Stars", recipient_id: 1780243200,
    invoice_payload: "store|stars_1|1780243200|", fulfillment: {},
  }, 50);
  assert.deepEqual(item, { kind: "stars", recipientID: 1780243200, amount: 20 });
});

test("Stars reversal debits the snapshotted grant with a deterministic key", async () => {
  const calls = [];
  const db = { starsRate: () => 999 };
  const gramsrv = { debitStars: async (...args) => calls.push(args) };
  const sale = { charge_id: "charge-1", fulfillment: { kind: "stars", recipientID: 1001, amount: 20 } };
  await reverseSaleFulfillment(sale, db, gramsrv);
  assert.deepEqual(calls, [[1001, 20, "Telegram bot refund", "refund:charge-1:stars"]]);
});

test("Premium reversal only revokes the entitlement created by the purchase", async () => {
  const calls = [];
  const db = { starsRate: () => 20 };
  const gramsrv = { revokePremium: async (...args) => calls.push(args) };
  const sale = { charge_id: "charge-premium", fulfillment: { kind: "premium", recipientID: 1001, months: 3, entitlementID: 77 } };
  await reverseSaleFulfillment(sale, db, gramsrv);
  assert.deepEqual(calls, [[1001, 77, "Telegram bot refund", "refund:charge-premium:premium"]]);
});

test("legacy Premium reversal fails safe instead of clearing unrelated Premium", async () => {
  const sale = { charge_id: "legacy", product: "premium_1m", recipient_id: 1001, fulfillment: {} };
  await assert.rejects(() => reverseSaleFulfillment(sale, { starsRate: () => 20 }, {}), /ID/);
});

test("Telegram retry does not debit the internal product twice", async (t) => {
  const dir = mkdtempSync(path.join(tmpdir(), "telesrv-refund-"));
  const db = new BotDatabase(path.join(dir, "bot.sqlite3"));
  t.after(() => { db.close(); rmSync(dir, { recursive: true, force: true }); });
  db.addSale({
    product: "stars_1", title: "20 Telesrv Stars", starsPrice: 1, recipientID: 1001,
    buyerID: 7, buyerName: "Buyer", chargeID: "charge-retry",
    fulfillment: { kind: "stars", recipientID: 1001, amount: 20 },
  });
  const sale = db.saleByCharge("charge-retry");
  const debits = [];
  const gramsrv = { debitStars: async (...args) => debits.push(args) };
  await assert.rejects(() => executeCompensatedRefund({
    sale, telegramID: 7, db, gramsrv,
    refundStarPayment: async () => { throw new Error("temporary Telegram failure"); },
  }), /temporary/);
  assert.equal(db.refundByCharge("charge-retry").status, "internal_reversed");
  await executeCompensatedRefund({ sale, telegramID: 7, db, gramsrv, refundStarPayment: async () => true });
  assert.equal(debits.length, 1);
  assert.equal(db.isRefunded("charge-retry"), true);
});
