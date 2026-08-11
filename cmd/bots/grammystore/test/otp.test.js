import test from "node:test";
import assert from "node:assert/strict";
import { createHmac } from "node:crypto";
import { parseTelesrvDelivery, verifyTelesrvSignature } from "../src/otp.js";

test("OTP webhook verifies gramsrv v1 signatures and request envelopes", () => {
  const now = 1_800_000_000, secret = "a-long-test-webhook-secret";
  const payload = {
    version: "1", delivery_id: "delivery-1", purpose: "login_sms", channel: "sms",
    recipient: "+79991234567", code: "12345", expires_at: "2027-01-15T08:05:00Z", expires_in: 300,
  };
  const raw = Buffer.from(JSON.stringify(payload)), timestamp = String(now);
  const signature = `sha256=${createHmac("sha256", secret).update(timestamp).update(".").update(raw).digest("hex")}`;
  const headers = { "x-telesrv-timestamp": timestamp, "x-telesrv-signature": signature, "idempotency-key": "delivery-1" };
  assert.equal(verifyTelesrvSignature(secret, headers, raw, now), true);
  assert.equal(parseTelesrvDelivery(raw, headers, now).deliveryID, "delivery-1");
  assert.equal(verifyTelesrvSignature(secret, { ...headers, "x-telesrv-signature": "sha256=bad" }, raw, now), false);
  assert.equal(verifyTelesrvSignature(secret, headers, raw, now + 301), false);
  assert.throws(() => parseTelesrvDelivery(raw, { ...headers, "idempotency-key": "other" }, now));
});
