import { createHash, randomUUID } from "node:crypto";

export class GramsrvClient {
  constructor(config) { this.config = config; }

  async post(route, payload) {
    const response = await fetch(`${this.config.gramsrvAPI}${route}`, {
      method: "POST",
      headers: { authorization: `Bearer ${this.config.gramsrvToken}`, "content-type": "application/json" },
      body: JSON.stringify(payload),
      signal: AbortSignal.timeout(15_000),
    });
    const text = await response.text();
    if (!response.ok) throw new Error(`gramsrv ${route} ${response.status}: ${text.slice(0, 500)}`);
    return text ? JSON.parse(text) : {};
  }

  command(reason, fields, idempotencyKey = "") {
    const digest = idempotencyKey ? createHash("sha256").update(idempotencyKey).digest("hex").slice(0, 40) : randomUUID();
    return { command_id: `bot-${digest}`, actor: this.config.gramsrvActor, reason, dry_run: false, ...fields };
  }

  grantStars(userID, amount, reason = "Telegram bot purchase", idempotencyKey = "") {
    return this.post("/v1/accounts/grant-stars", this.command(reason, { user_id: userID, amount }, idempotencyKey));
  }

  debitStars(userID, amount, reason = "Telegram bot refund", idempotencyKey = "") {
    return this.post("/v1/accounts/debit-stars", this.command(reason, { user_id: userID, amount }, idempotencyKey));
  }

  grantPremium(userID, months, reason = "Telegram bot purchase", idempotencyKey = "") {
    return this.post("/v1/accounts/grant-premium", this.command(reason, { user_id: userID, months }, idempotencyKey));
  }

  revokePremium(userID, entitlementID, reason = "Telegram bot refund", idempotencyKey = "") {
    return this.post("/v1/accounts/grant-premium", this.command(reason, { user_id: userID, months: 0, entitlement_id: entitlementID }, idempotencyKey));
  }

  mintUsername(userID, username, bidTON, idempotencyKey = "") {
    const amount = (BigInt(bidTON) * 1_000_000_000n).toString();
    return this.post("/v1/collectible-usernames/mint", this.command("Telegram bot purchase", {
      username,
      owner_user_id: String(userID),
      currency: "TON",
      amount,
      crypto_currency: "TON",
      crypto_amount: amount,
      url: `${this.config.publicBaseURL}/nft/username/${username}`,
      purchase_date: Math.floor(Date.now() / 1000),
    }, idempotencyKey));
  }
  revokeUsername(username, expectedOwnerUserID, idempotencyKey = "") {
    return this.post("/v1/collectible-usernames/revoke", this.command("Telegram bot refund", {
      username,
      expected_owner_user_id: String(expectedOwnerUserID),
      burn: false,
    }, idempotencyKey));
  }
}
