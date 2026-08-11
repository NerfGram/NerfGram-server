import { createHash, createHmac, timingSafeEqual } from "node:crypto";

function secureEqual(left, right) {
  const expected = Buffer.from(left); const actual = Buffer.from(right ?? "");
  return expected.length === actual.length && timingSafeEqual(expected, actual);
}

export function verifyTelesrvSignature(secret, headers, raw, nowSeconds = Math.floor(Date.now() / 1000)) {
  const timestamp = String(headers["x-telesrv-timestamp"] ?? "");
  if (!/^\d{1,20}$/.test(timestamp)) return false;
  const unix = Number(timestamp);
  if (!Number.isSafeInteger(unix) || Math.abs(nowSeconds - unix) > 300) return false;
  const expected = `sha256=${createHmac("sha256", secret).update(timestamp).update(".").update(raw).digest("hex")}`;
  return secureEqual(expected, headers["x-telesrv-signature"]);
}

export function parseTelesrvDelivery(raw, headers, nowSeconds = Math.floor(Date.now() / 1000)) {
  const payload = JSON.parse(raw.toString("utf8"));
  const recipient = String(payload.recipient ?? "").trim(), code = String(payload.code ?? "").trim();
  const deliveryID = String(payload.delivery_id ?? "").trim(), idempotencyKey = String(headers["idempotency-key"] ?? "");
  const expiresAt = Math.floor(Date.parse(String(payload.expires_at ?? "")) / 1000);
  if (payload.version !== "1" || deliveryID.length < 1 || deliveryID.length > 128 || deliveryID !== idempotencyKey ||
      !recipient || recipient.length > 512 || !/^[0-9A-Za-z_-]{1,32}$/.test(code) ||
      !Number.isSafeInteger(expiresAt) || expiresAt <= nowSeconds) throw new Error("REQUEST_INVALID");
  return { recipient, code, deliveryID, expiresAt, fingerprint: createHash("sha256").update(raw).digest("hex") };
}
