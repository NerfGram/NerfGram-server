import http from "node:http";
import { loadConfig } from "./config.js";
import { BotDatabase } from "./db.js";
import { GramsrvClient } from "./gramsrv.js";
import { commandList, createBot } from "./bot.js";
import { normalizeLanguage, translate } from "./i18n.js";
import { parseTelesrvDelivery, verifyTelesrvSignature } from "./otp.js";

const config = loadConfig();
const db = new BotDatabase(config.dbPath);
const gramsrv = new GramsrvClient(config);
const bot = createBot({ config, db, gramsrv });

function escapeHTML(value) { return String(value ?? "").replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;"); }
function json(response, status, body) { response.writeHead(status, { "content-type": "application/json; charset=utf-8" }); response.end(JSON.stringify(body)); }

function languageByChatID(chatID) {
  return normalizeLanguage(db.userByChatID(chatID)?.language, config.defaultLanguage);
}

function loginCodeMessage(language, recipient, code, unbound = false) {
  const variables = {
    product: escapeHTML(config.productName),
    phone: escapeHTML(recipient),
    code: escapeHTML(code),
  };
  const lines = [
    translate(language, "otpTitle", variables),
    "",
    translate(language, "otpPhone", variables),
    translate(language, "otpCode", variables),
    "",
    translate(language, "otpWarning", variables),
  ];
  if (unbound) lines.push("", translate(language, "otpUnbound", variables));
  return lines.join("\n");
}

async function deliverLoginCode(recipient, code, chatIDs) {
  let delivered = 0;
  for (const chatID of chatIDs) {
    try {
      await bot.api.sendMessage(chatID, loginCodeMessage(languageByChatID(chatID), recipient, code), { parse_mode: "HTML" });
      delivered++;
    }
    catch (error) { console.error("OTP delivery failed", chatID, error); }
  }
  if (!delivered) for (const owner of config.ownerIDs) {
    await bot.api.sendMessage(owner, loginCodeMessage(languageByChatID(owner), recipient, code, true), { parse_mode: "HTML" }).catch(() => {});
  }
}

const server = http.createServer((request, response) => {
  if (request.method === "GET" && request.url === "/healthz") return json(response, 200, { ok: true });
  if (request.method !== "POST" || !["/code", "/v1/otp/deliveries"].includes(request.url)) return json(response, 404, { error: "not found" });
  const chunks = []; let size = 0, tooLarge = false;
  request.on("data", (chunk) => { size += chunk.length; if (size > 16_384) tooLarge = true; else chunks.push(chunk); });
  request.on("end", () => {
    try {
      if (tooLarge) return json(response, 413, { accepted: false, error_code: "REQUEST_TOO_LARGE", retryable: false });
      const raw = Buffer.concat(chunks);
      if (!verifyTelesrvSignature(config.codeWebhookSecret, request.headers, raw)) return json(response, 401, { accepted: false, error_code: "SIGNATURE_INVALID", retryable: false });
      const { recipient, code, deliveryID, expiresAt, fingerprint } = parseTelesrvDelivery(raw, request.headers);
      const delivery = db.acceptLoginCodeDelivery(deliveryID, fingerprint, recipient, code, expiresAt);
      // Authentication must not depend on Telegram Bot API latency. Persist the
      // code, acknowledge gramsrv immediately, then fan it out asynchronously.
      json(response, 202, { accepted: true, message_id: `grammy:${deliveryID}` });
      if (!delivery.duplicate) void deliverLoginCode(recipient, code, delivery.chatIDs).catch((error) => console.error("OTP dispatch failed", error));
    } catch (error) {
      console.error("OTP webhook failed", error);
      const conflict = error.message === "IDEMPOTENCY_CONFLICT";
      return json(response, conflict ? 409 : 400, { accepted: false, error_code: conflict ? "IDEMPOTENCY_CONFLICT" : "JSON_INVALID", retryable: false });
    }
  });
});

server.listen(config.codePort, config.codeHost, () => console.log(`OTP webhook listening on http://${config.codeHost}:${config.codePort}`));

const me = await bot.api.getMe();
bot.botInfo = me;
console.log(`Starting @${me.username}`);
await bot.api.setMyCommands(commandList(config.defaultLanguage));
await bot.api.setMyCommands(commandList("ru"), { language_code: "ru" });
await bot.api.setMyCommands(commandList("en"), { language_code: "en" });

let stopping = false;
async function shutdown(signal) {
  if (stopping) return; stopping = true; console.log(`Stopping on ${signal}`);
  bot.stop(); server.close(); db.close();
}
process.once("SIGINT", () => shutdown("SIGINT"));
process.once("SIGTERM", () => shutdown("SIGTERM"));

await bot.start({ allowed_updates: ["message", "callback_query", "pre_checkout_query"], onStart: () => console.log("Bot polling started") });
