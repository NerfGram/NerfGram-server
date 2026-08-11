import { Bot, GrammyError, HttpError, InlineKeyboard } from "grammy";
import { randomInt } from "node:crypto";
import { buildPayload, findProduct, KINDS, localizeProduct, normalizeUsername, parsePayload, productsOfKind } from "./catalog.js";
import { normalizeLanguage, translate, translateError } from "./i18n.js";

const spinPrizes = Object.freeze([
  { amount: 50, weight: 250 }, { amount: 100, weight: 130 }, { amount: 500, weight: 50 },
  { amount: 1000, weight: 30 }, { amount: 10000, weight: 10 }, { amount: 15, weight: 530 },
]);

function escapeHTML(value) {
  return String(value ?? "").replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;");
}

function initialLanguage(from, fallback) { return normalizeLanguage(from?.language_code, fallback); }
function isOwner(config, id) { return config.ownerIDs.has(id); }
function userName(from) { return from.username ? `@${from.username}` : [from.first_name, from.last_name].filter(Boolean).join(" "); }
function positiveInteger(value, max = Number.MAX_SAFE_INTEGER) {
  const number = Number(value);
  return Number.isSafeInteger(number) && number > 0 && number <= max ? number : 0;
}
export function isStartCommand(text) { return /^\/start(?:@\w+)?(?:\s|$)/i.test(text ?? ""); }

export function fulfillmentForSale(sale, starsRate = 20) {
  if (sale?.fulfillment?.kind) return sale.fulfillment;
  if (!sale) throw new Error("sale not found");
  if (sale.product === "custom") return { kind: "custom" };
  let parsed = null;
  try { if (sale.invoice_payload) parsed = parsePayload(sale.invoice_payload); } catch {}
  const product = findProduct(sale.product, starsRate);
  if (!product) throw new Error("could not identify the fulfilled product");
  if (product.kind === KINDS.stars) {
    const titleAmount = Number(String(sale.title).match(/^(\d+)(?:\s+\S+)?\s+Stars$/)?.[1] ?? 0);
    const amount = parsed?.starsAmount || titleAmount || product.starsAmount;
    return { kind: "stars", recipientID: sale.recipient_id, amount };
  }
  if (product.kind === KINDS.premium) return { kind: "premium", recipientID: sale.recipient_id, months: product.months, entitlementID: 0 };
  if (product.kind === KINDS.username) return { kind: "username", recipientID: sale.recipient_id, username: normalizeUsername(parsed?.extra), bid: product.bid };
  throw new Error("legacy number sale has no exact fulfillment data and requires manual review");
}

export async function reverseSaleFulfillment(sale, db, gramsrv) {
  const item = fulfillmentForSale(sale, db.starsRate());
  const key = `refund:${sale.charge_id}:${item.kind}`;
  if (item.kind === "custom") return item;
  if (item.kind === "stars") await gramsrv.debitStars(item.recipientID, item.amount, "Telegram bot refund", key);
  else if (item.kind === "premium") {
    if (!positiveInteger(item.entitlementID)) throw new Error("legacy Premium sale has no entitlement ID; automatic reversal is unsafe");
    await gramsrv.revokePremium(item.recipientID, item.entitlementID, "Telegram bot refund", key);
  } else if (item.kind === "username") {
    if (!item.username) throw new Error("sale has no stored @username");
    await gramsrv.revokeUsername(item.username, item.recipientID, key);
  } else if (item.kind === "number") {
    if (!positiveInteger(item.numberID) || !item.phone) throw new Error("sale has no stored number data");
    db.revokePurchasedNumber(item.ownerID, item.numberID, item.phone);
  } else throw new Error("unknown fulfillment kind");
  return item;
}

export async function executeCompensatedRefund({ sale, telegramID, db, gramsrv, refundStarPayment }) {
  const chargeID = sale.charge_id;
  const refund = db.beginRefund(chargeID, telegramID);
  try {
    if (!refund.internal_reversed) {
      await reverseSaleFulfillment(sale, db, gramsrv);
      db.markRefundInternal(chargeID);
    }
    try { await refundStarPayment(telegramID, chargeID); }
    catch (error) {
      if (!/REFUND.*ALREADY|ALREADY.*REFUND/i.test(String(error?.description ?? error?.message ?? error))) throw error;
    }
    db.markRefunded(chargeID, telegramID);
  } catch (error) {
    db.failRefund(chargeID, error.message);
    throw error;
  }
}

export function mainKeyboard(language, admin = false) {
  const kb = new InlineKeyboard()
    .text(translate(language, "buttonNumbers"), "menu:numbers").text(translate(language, "buttonShop"), "menu:shop").row()
    .text(translate(language, "buttonBonuses"), "menu:bonuses").text(translate(language, "buttonReferrals"), "menu:referrals").row()
    .text(translate(language, "buttonSupport"), "menu:support").text(translate(language, "buttonSettings"), "menu:settings");
  if (admin) kb.row().text(translate(language, "buttonAdmin"), "admin:menu");
  return kb;
}

export function backKeyboard(language, target = "menu:home") {
  return new InlineKeyboard().text(translate(language, "back"), target);
}

export function shopKeyboard(language) {
  return new InlineKeyboard()
    .text(translate(language, "buttonPremium"), "shop:premium").text(translate(language, "buttonStars"), "shop:stars").row()
    .text(translate(language, "buttonNumber"), "shop:number").text(translate(language, "buttonUsername"), "shop:username").row()
    .text(translate(language, "back"), "menu:home");
}

export function settingsKeyboard(language, user) {
  const current = normalizeLanguage(user?.language, language);
  const russian = `${current === "ru" ? "✅ " : ""}${translate(language, "languageRussian")}`;
  const english = `${current === "en" ? "✅ " : ""}${translate(language, "languageEnglish")}`;
  return new InlineKeyboard()
    .text(translate(language, "accountButton"), "settings:account").row()
    .text(russian, "settings:lang:ru").text(english, "settings:lang:en").row()
    .text(translate(language, user?.notifications ? "notificationsOn" : "notificationsOff"), "settings:notifications").row()
    .text(translate(language, "back"), "menu:home");
}

export function adminKeyboard(language) {
  return new InlineKeyboard()
    .text(translate(language, "adminStatsButton"), "admin:stats").text(translate(language, "adminBroadcastButton"), "admin:broadcast").row()
    .text(translate(language, "adminStarsButton"), "admin:stars").text(translate(language, "adminPremiumButton"), "admin:premium").row()
    .text(translate(language, "adminPromoButton"), "admin:promo").text(translate(language, "adminGiveawayButton"), "admin:giveaway").row()
    .text(translate(language, "adminBonusButton"), "admin:bonus").text(translate(language, "adminInvoiceButton"), "admin:invoice").row()
    .text(translate(language, "adminAccessButton"), "admin:access").text(translate(language, "adminRefundButton"), "admin:refund").row()
    .text(translate(language, "adminReplyButton"), "admin:reply").text(translate(language, "adminSalesButton"), "admin:sales").row()
    .text(translate(language, "adminRateButton"), "admin:rate").row()
    .text(translate(language, "back"), "menu:home");
}

export function commandList(language) {
  return [
    { command: "start", description: translate(language, "commandStart") },
    { command: "menu", description: translate(language, "commandMenu") },
    { command: "promo_code", description: translate(language, "commandPromo") },
  ];
}

async function editOrReply(ctx, message, keyboard = undefined) {
  const options = { parse_mode: "HTML", link_preview_options: { is_disabled: true }, reply_markup: keyboard };
  if (ctx.callbackQuery?.message) {
    try { return await ctx.editMessageText(message, options); }
    catch (error) { if (!String(error.description ?? error).includes("message is not modified")) throw error; }
    return;
  }
  return ctx.reply(message, options);
}

async function subscribed(ctx, config) {
  if (!config.requiredChannel || isOwner(config, ctx.from.id)) return true;
  try {
    const member = await ctx.api.getChatMember(config.requiredChannel, ctx.from.id);
    return ["creator", "administrator", "member"].includes(member.status) || (member.status === "restricted" && member.is_member === true);
  } catch {
    return false;
  }
}

async function subscriptionGate(ctx, config, language) {
  const kb = new InlineKeyboard();
  if (config.requiredChannelURL) kb.url(translate(language, "openChannel"), config.requiredChannelURL).row();
  kb.text(translate(language, "checkSubscription"), "subscription:check");
  await editOrReply(ctx, translate(language, "subscriptionPrompt"), kb);
}

function parseStartRef(ctx) {
  const match = String(ctx.match ?? "").match(/^ref_(\d+)$/);
  return match ? Number(match[1]) : 0;
}

function productText(product, language) {
  const extra = product.kind === KINDS.username
    ? `\n${translate(language, "productBid", { bid: product.bid })}`
    : product.kind === KINDS.stars ? `\n${translate(language, "productCredit", { amount: product.starsAmount })}` : "";
  return `<b>${escapeHTML(product.title)}</b>\n\n${escapeHTML(product.description)}\n\n${translate(language, "productPrice", { price: product.starsPrice })}${extra}`;
}

function productKeyboard(product, db, buyerID, language) {
  const kb = new InlineKeyboard();
  if (product.kind === KINDS.number) {
    return kb.text(translate(language, "buyFor", { price: product.starsPrice }), `buy:${product.code}:0`).row().text(translate(language, "back"), `shop:${product.kind}`);
  }
  const selfID = db.user(buyerID)?.server_user_id ?? 0;
  if (selfID > 0) kb.text(translate(language, "buySelf"), `buy:${product.code}:${selfID}`).row();
  kb.text(translate(language, "giftOther"), `target:${product.code}`).row();
  for (const id of db.recentRecipients(buyerID)) kb.text(`ID ${id}`, `buy:${product.code}:${id}`);
  return kb.row().text(translate(language, "back"), `shop:${product.kind}`);
}

async function sendInvoice(ctx, product, targetUserID, language, extra = "") {
  const localized = localizeProduct(product, language);
  const starsAmount = product.kind === KINDS.stars ? product.starsAmount : 0;
  await ctx.api.sendInvoice(ctx.chat.id, localized.title, localized.description, buildPayload(product.code, targetUserID, extra, starsAmount), "XTR", [{ label: localized.title, amount: product.starsPrice }]);
}

function rollPrize() {
  const total = spinPrizes.reduce((sum, value) => sum + value.weight, 0);
  let value = randomInt(total);
  for (const prize of spinPrizes) { value -= prize.weight; if (value < 0) return prize.amount; }
  return 15;
}

export function createBot({ config, db, gramsrv }) {
  const bot = new Bot(config.botToken);
  const languageOf = (id) => normalizeLanguage(db.user(id)?.language, config.defaultLanguage);
  const tr = (id, key, variables = {}) => translate(languageOf(id), key, { product: escapeHTML(config.productName), ...variables });
  const localized = (id, product) => localizeProduct(product, languageOf(id));

  bot.use(async (ctx, next) => {
    // /start owns user creation because it must apply the referral atomically on
    // the very first insert. Pre-creating that user here silently discarded ref_ links.
    const isStart = isStartCommand(ctx.message?.text);
    if (ctx.from && ctx.chat && !isStart) db.upsertUser(ctx.from, ctx.chat.id, initialLanguage(ctx.from, config.defaultLanguage));
    await next();
  });

  bot.command("start", async (ctx) => {
    const referrer = parseStartRef(ctx);
    const existed = db.user(ctx.from.id);
    db.upsertUser(ctx.from, ctx.chat.id, initialLanguage(ctx.from, config.defaultLanguage), referrer, config.referralBonus);
    const language = languageOf(ctx.from.id);
    if (!(await subscribed(ctx, config))) return subscriptionGate(ctx, config, language);
    const number = db.createNumber(ctx.from.id, ctx.chat.id, "free", config.defaultNumberCountry, false);
    const referralApplied = !existed && referrer > 0 && db.user(ctx.from.id)?.referred_by === referrer;
    const lines = [tr(ctx.from.id, "startHello"), tr(ctx.from.id, "startPhone", { phone: escapeHTML(number.display) }), tr(ctx.from.id, "startInitialCode", { code: number.login_code })];
    if (referralApplied) lines.push(tr(ctx.from.id, "referralAccepted"));
    await ctx.reply(lines.join("\n\n"), { parse_mode: "HTML", reply_markup: mainKeyboard(language, isOwner(config, ctx.from.id)) });
  });

  bot.command("menu", async (ctx) => {
    const language = languageOf(ctx.from.id);
    await editOrReply(ctx, tr(ctx.from.id, "menuTitle"), mainKeyboard(language, isOwner(config, ctx.from.id)));
  });

  bot.command("admin", async (ctx) => {
    if (!isOwner(config, ctx.from.id)) return;
    const language = languageOf(ctx.from.id);
    await editOrReply(ctx, tr(ctx.from.id, "adminTitle"), adminKeyboard(language));
  });

  bot.command("promo_code", async (ctx) => {
    const language = languageOf(ctx.from.id);
    const [code, rawID] = String(ctx.match ?? "").trim().split(/\s+/);
    const serverID = Number(rawID || db.user(ctx.from.id)?.server_user_id);
    if (!code || !Number.isSafeInteger(serverID) || serverID <= 0) return ctx.reply(tr(ctx.from.id, "promoUsage", { product: config.productName.toUpperCase() }));
    try {
      const promo = db.claimPromo(code, ctx.from.id);
      try { await gramsrv.grantStars(serverID, promo.stars_amount, `Promo ${code}`, `promo:${code.toLowerCase()}:${ctx.from.id}`); }
      catch (error) { db.releaseCampaignClaim("promo", code.toLowerCase(), ctx.from.id); throw error; }
      await ctx.reply(tr(ctx.from.id, "promoGranted", { amount: promo.stars_amount }));
    } catch (error) {
      console.error("Promo redemption failed", error);
      await ctx.reply(translateError(language, error));
    }
  });

  bot.on("pre_checkout_query", async (ctx) => {
    const language = languageOf(ctx.from.id);
    try {
      if (ctx.preCheckoutQuery.currency !== "XTR" || ctx.preCheckoutQuery.total_amount <= 0) throw new Error("unsupported currency or amount");
      if (ctx.preCheckoutQuery.invoice_payload.startsWith("custom|")) {
        const title = Buffer.from(ctx.preCheckoutQuery.invoice_payload.slice(7), "base64url").toString("utf8");
        if (!title || title.length > 32) throw new Error("invalid invoice");
      } else {
        const parsed = parsePayload(ctx.preCheckoutQuery.invoice_payload);
        const product = findProduct(parsed.code, db.starsRate());
        if (!product || product.starsPrice !== ctx.preCheckoutQuery.total_amount ||
            (product.kind === KINDS.stars && parsed.starsAmount <= 0)) throw new Error("product price changed");
      }
      await ctx.answerPreCheckoutQuery(true);
    } catch {
      await ctx.answerPreCheckoutQuery(false, { error_message: translate(language, "precheckoutInvalid") });
    }
  });

  async function fulfill(product, recipientID, buyer, chatID, chargeID, extra = "") {
    if (product.kind !== KINDS.number && (!Number.isSafeInteger(recipientID) || recipientID <= 0)) throw new Error(`recipient ${config.productName} ID is invalid`);
    if (db.saleByCharge(chargeID)) return;
    const key = `payment:${chargeID}:${product.code}`;
    let number = null;
    let fulfillment = null;
    if (product.kind === KINDS.premium) {
      const result = await gramsrv.grantPremium(recipientID, product.months, "Telegram bot purchase", key);
      fulfillment = { kind: "premium", recipientID, months: product.months, entitlementID: Number(result?.details?.entitlement_id ?? 0) };
    } else if (product.kind === KINDS.stars) {
      await gramsrv.grantStars(recipientID, product.starsAmount, "Telegram bot purchase", key);
      fulfillment = { kind: "stars", recipientID, amount: product.starsAmount };
    }
    else if (product.kind === KINDS.username) {
      const username = normalizeUsername(extra);
      if (!username) throw new Error("collectible username is invalid");
      await gramsrv.mintUsername(recipientID, username, product.bid, key);
      fulfillment = { kind: "username", recipientID, username, bid: product.bid };
    } else if (product.kind === KINDS.number) {
      number = db.createNumber(buyer.id, chatID, product.numberFormat, "ANON", true);
      recipientID = buyer.id;
      fulfillment = { kind: "number", ownerID: buyer.id, numberID: number.id, phone: number.phone, format: number.format };
    } else throw new Error("unknown product kind");
    const productView = localized(buyer.id, product);
    db.addSale({ product: product.code, title: productView.title, starsPrice: product.starsPrice, recipientID, buyerID: buyer.id, buyerName: userName(buyer), chargeID, fulfillment });
    const message = number
      ? tr(buyer.id, "numberReserved", { phone: escapeHTML(number.display) })
      : tr(buyer.id, "productGranted", { title: escapeHTML(productView.title), id: recipientID });
    await bot.api.sendMessage(chatID, message, { parse_mode: "HTML" }).catch(() => {});
  }

  bot.on("message:successful_payment", async (ctx) => {
    const payment = ctx.message.successful_payment;
    if (!db.beginPayment(payment.telegram_payment_charge_id, ctx.from.id, payment.invoice_payload, payment.total_amount)) return;
    try {
      if (payment.invoice_payload.startsWith("custom|")) {
        const title = Buffer.from(payment.invoice_payload.slice(7), "base64url").toString("utf8");
        db.addSale({ product: "custom", title, starsPrice: payment.total_amount, recipientID: ctx.from.id, buyerID: ctx.from.id, buyerName: userName(ctx.from), chargeID: payment.telegram_payment_charge_id, fulfillment: { kind: "custom" } });
        await ctx.reply(tr(ctx.from.id, "paymentReceived", { title: escapeHTML(title) }), { parse_mode: "HTML" });
      } else {
        const parsed = parsePayload(payment.invoice_payload);
        let product = findProduct(parsed.code, db.starsRate());
        if (!product) throw new Error("product no longer exists");
        if (payment.currency !== "XTR" || payment.total_amount !== product.starsPrice) throw new Error("paid amount does not match the product");
        if (product.kind === KINDS.stars) {
          if (parsed.starsAmount <= 0) throw new Error("invoice has no snapshotted server Stars amount");
          product = { ...product, starsAmount: parsed.starsAmount, title: `${parsed.starsAmount} Stars`, titleRu: `${parsed.starsAmount} Stars` };
        }
        const recipient = parsed.targetUserID || db.user(ctx.from.id)?.server_user_id || 0;
        await fulfill(product, recipient, ctx.from, ctx.chat.id, payment.telegram_payment_charge_id, parsed.extra);
      }
      db.finishPayment(payment.telegram_payment_charge_id);
    } catch (error) {
      db.failPayment(payment.telegram_payment_charge_id, error);
      console.error("Payment fulfillment failed", payment.telegram_payment_charge_id, error);
      await ctx.reply(tr(ctx.from.id, "paymentFailed", { charge: escapeHTML(payment.telegram_payment_charge_id) }), { parse_mode: "HTML" });
      for (const owner of config.ownerIDs) {
        await bot.api.sendMessage(owner, tr(owner, "fulfillmentOwnerError", { charge: escapeHTML(payment.telegram_payment_charge_id), error: escapeHTML(error.message) }), { parse_mode: "HTML" }).catch(() => {});
      }
    }
  });

  bot.callbackQuery(/^subscription:check$/, async (ctx) => {
    await ctx.answerCallbackQuery();
    const language = languageOf(ctx.from.id);
    if (await subscribed(ctx, config)) await editOrReply(ctx, tr(ctx.from.id, "menuTitle"), mainKeyboard(language, isOwner(config, ctx.from.id)));
    else await subscriptionGate(ctx, config, language);
  });

  bot.callbackQuery(/^menu:(.+)$/, async (ctx) => {
    await ctx.answerCallbackQuery();
    const page = ctx.match[1];
    const user = db.user(ctx.from.id);
    const language = languageOf(ctx.from.id);
    if (page === "home") return editOrReply(ctx, tr(ctx.from.id, "menuTitle"), mainKeyboard(language, isOwner(config, ctx.from.id)));
    if (page === "numbers") {
      const numbers = db.numbers(ctx.from.id);
      const current = db.currentNumber(ctx.from.id);
      const list = numbers.slice(0, 10).map((number) => `${number.is_current ? "▶️" : "▫️"} <code>${escapeHTML(number.display)}</code>`).join("\n");
      const code = current?.login_code && current.code_expires_at >= Math.floor(Date.now() / 1000) ? `<code>${current.login_code}</code>` : tr(ctx.from.id, "noCode");
      const kb = new InlineKeyboard().text(tr(ctx.from.id, "newFreeNumber"), "numbers:new").row().text(tr(ctx.from.id, "back"), "menu:home");
      return editOrReply(ctx, `${tr(ctx.from.id, "numbersTitle")}\n\n${list || "—"}\n\n🔑 ${code}`, kb);
    }
    if (page === "shop") return editOrReply(ctx, tr(ctx.from.id, "shopTitle"), shopKeyboard(language));
    if (page === "bonuses") {
      const kb = new InlineKeyboard().text(tr(ctx.from.id, "dailyBonus"), "bonus:daily").text(tr(ctx.from.id, "wheel"), "bonus:spin").row().text(tr(ctx.from.id, "back"), "menu:home");
      return editOrReply(ctx, `${tr(ctx.from.id, "bonusesTitle")}\n\n${tr(ctx.from.id, "balance", { balance: user.bonus })}\n${tr(ctx.from.id, "referralsCount", { count: user.referral_count })}`, kb);
    }
    if (page === "referrals") {
      const username = config.publicUsername || bot.botInfo?.username || "bot";
      const link = `https://t.me/${username}?start=ref_${ctx.from.id}`;
      return editOrReply(ctx, `${tr(ctx.from.id, "referralsTitle")}\n\n${tr(ctx.from.id, "invited", { count: user.referral_count })}\n${tr(ctx.from.id, "referralBonus", { amount: config.referralBonus })}\n\n<code>${link}</code>`, backKeyboard(language));
    }
    if (page === "support") {
      db.setPending(ctx.from.id, "support");
      return editOrReply(ctx, tr(ctx.from.id, "supportPrompt"), backKeyboard(language));
    }
    if (page === "settings") return editOrReply(ctx, tr(ctx.from.id, "settingsTitle"), settingsKeyboard(language, user));
  });

  bot.callbackQuery(/^numbers:new$/, async (ctx) => {
    await ctx.answerCallbackQuery();
    const language = languageOf(ctx.from.id);
    const kb = new InlineKeyboard().text(tr(ctx.from.id, "countryRU"), "numbers:new:RU").text(tr(ctx.from.id, "countryUS"), "numbers:new:US").row().text(tr(ctx.from.id, "back"), "menu:numbers");
    await editOrReply(ctx, tr(ctx.from.id, "chooseCountry"), kb);
  });

  bot.callbackQuery(/^numbers:new:(RU|US)$/, async (ctx) => {
    await ctx.answerCallbackQuery();
    const language = languageOf(ctx.from.id);
    const number = db.createNumber(ctx.from.id, ctx.chat.id, "free", ctx.match[1], true);
    await editOrReply(ctx, tr(ctx.from.id, "newNumber", { phone: escapeHTML(number.display), code: number.login_code }), backKeyboard(language, "menu:numbers"));
  });

  bot.callbackQuery(/^shop:(premium|stars|number|username)$/, async (ctx) => {
    await ctx.answerCallbackQuery();
    const kind = ctx.match[1];
    const language = languageOf(ctx.from.id);
    const kb = new InlineKeyboard();
    for (const product of productsOfKind(kind, db.starsRate())) {
      const view = localizeProduct(product, language);
      kb.text(`${view.title} · ${product.starsPrice} ⭐`, `product:${product.code}`).row();
    }
    if (kind === KINDS.stars) kb.text(tr(ctx.from.id, "customAmountButton"), "stars:custom").row();
    kb.text(tr(ctx.from.id, "back"), "menu:shop");
    await editOrReply(ctx, tr(ctx.from.id, "selectProduct"), kb);
  });

  bot.callbackQuery(/^stars:custom$/, async (ctx) => {
    await ctx.answerCallbackQuery();
    db.setPending(ctx.from.id, "stars_amount");
    await editOrReply(ctx, tr(ctx.from.id, "customAmountPrompt"), backKeyboard(languageOf(ctx.from.id), "shop:stars"));
  });

  bot.callbackQuery(/^product:(.+)$/, async (ctx) => {
    await ctx.answerCallbackQuery();
    const product = findProduct(ctx.match[1], db.starsRate());
    if (!product) return;
    const language = languageOf(ctx.from.id);
    const view = localizeProduct(product, language);
    await editOrReply(ctx, productText(view, language), productKeyboard(product, db, ctx.from.id, language));
  });

  bot.callbackQuery(/^target:(.+)$/, async (ctx) => {
    await ctx.answerCallbackQuery();
    db.setPending(ctx.from.id, "target", { productCode: ctx.match[1] });
    await editOrReply(ctx, tr(ctx.from.id, "targetPrompt"), backKeyboard(languageOf(ctx.from.id), `product:${ctx.match[1]}`));
  });

  bot.callbackQuery(/^buy:([^:]+):(\d+)$/, async (ctx) => {
    await ctx.answerCallbackQuery();
    const product = findProduct(ctx.match[1], db.starsRate());
    const targetID = Number(ctx.match[2]);
    if (!product) return;
    const language = languageOf(ctx.from.id);
    if (product.kind === KINDS.username) {
      db.setPending(ctx.from.id, "username", { productCode: product.code, targetID });
      return editOrReply(ctx, tr(ctx.from.id, "enterUsername"), backKeyboard(language, `product:${product.code}`));
    }
    if (targetID > 0 && targetID !== db.user(ctx.from.id)?.server_user_id) db.rememberRecipient(ctx.from.id, targetID);
    if (isOwner(config, ctx.from.id)) return fulfill(product, targetID, ctx.from, ctx.chat.id, `owner-${ctx.from.id}-${Date.now()}-${randomInt(1_000_000)}`);
    await sendInvoice(ctx, product, targetID, language);
  });

  bot.callbackQuery(/^settings:lang:(ru|en)$/, async (ctx) => {
    db.setLanguage(ctx.from.id, ctx.match[1]);
    const language = languageOf(ctx.from.id);
    await ctx.answerCallbackQuery({ text: translate(language, "languageChanged") });
    await editOrReply(ctx, tr(ctx.from.id, "settingsTitle"), settingsKeyboard(language, db.user(ctx.from.id)));
  });

  bot.callbackQuery(/^settings:notifications$/, async (ctx) => {
    const enabled = db.toggleNotifications(ctx.from.id);
    const language = languageOf(ctx.from.id);
    await ctx.answerCallbackQuery({ text: tr(ctx.from.id, enabled ? "notificationsEnabled" : "notificationsDisabled") });
    await editOrReply(ctx, tr(ctx.from.id, "settingsTitle"), settingsKeyboard(language, db.user(ctx.from.id)));
  });

  bot.callbackQuery(/^settings:account$/, async (ctx) => {
    await ctx.answerCallbackQuery();
    db.setPending(ctx.from.id, "account");
    await editOrReply(ctx, tr(ctx.from.id, "accountPrompt"), backKeyboard(languageOf(ctx.from.id), "menu:settings"));
  });

  bot.callbackQuery(/^bonus:daily$/, async (ctx) => {
    const result = db.claimDaily(ctx.from.id, config.dailyBonus);
    const language = languageOf(ctx.from.id);
    await ctx.answerCallbackQuery({ text: result.claimed ? `+${config.dailyBonus}` : tr(ctx.from.id, "dailyAlready") });
    await editOrReply(ctx, `${tr(ctx.from.id, "bonusesTitle")}\n\n${tr(ctx.from.id, "balance", { balance: result.balance })}`, backKeyboard(language, "menu:bonuses"));
  });

  bot.callbackQuery(/^bonus:spin$/, async (ctx) => {
    await ctx.answerCallbackQuery();
    const user = db.user(ctx.from.id);
    const language = languageOf(ctx.from.id);
    if (!user.server_user_id) {
      db.setPending(ctx.from.id, "account");
      return editOrReply(ctx, tr(ctx.from.id, "accountPrompt"), backKeyboard(language, "menu:bonuses"));
    }
    try {
      const award = db.reserveSpin(ctx.from.id, user.server_user_id, rollPrize());
      await gramsrv.grantStars(user.server_user_id, award.prize, "Daily bot wheel", `spin:${ctx.from.id}:${award.day}`);
      db.finishSpin(ctx.from.id, award.day);
      await editOrReply(ctx, tr(ctx.from.id, "wheelWon", { amount: award.prize }), backKeyboard(language, "menu:bonuses"));
    } catch (error) {
      console.error("Wheel grant failed", error);
      await editOrReply(ctx, translateError(language, error), backKeyboard(language, "menu:bonuses"));
    }
  });

  bot.callbackQuery(/^giveaway:([a-f0-9]+)$/, async (ctx) => {
    await ctx.answerCallbackQuery();
    const user = db.user(ctx.from.id);
    const language = languageOf(ctx.from.id);
    if (!user.server_user_id) return ctx.reply(tr(ctx.from.id, "giveawayNeedAccount"));
    const id = ctx.match[1];
    try {
      const item = db.claimGiveaway(id, ctx.from.id);
      try { await gramsrv.grantStars(user.server_user_id, item.stars_amount, `Giveaway ${id}`, `giveaway:${id}:${ctx.from.id}`); }
      catch (error) { db.releaseCampaignClaim("giveaway", id, ctx.from.id); throw error; }
      await ctx.reply(tr(ctx.from.id, "giveawayGranted", { amount: item.stars_amount }));
    } catch (error) {
      console.error("Giveaway claim failed", error);
      await ctx.reply(translateError(language, error));
    }
  });

  bot.callbackQuery(/^admin:(.+)$/, async (ctx) => {
    await ctx.answerCallbackQuery();
    if (!isOwner(config, ctx.from.id)) return;
    const action = ctx.match[1];
    const language = languageOf(ctx.from.id);
    if (action === "menu") return editOrReply(ctx, tr(ctx.from.id, "adminTitle"), adminKeyboard(language));
    if (action === "stats") {
      const stats = db.stats();
      return editOrReply(ctx, tr(ctx.from.id, "adminStats", stats), backKeyboard(language, "admin:menu"));
    }
    if (action === "sales") {
      const lines = db.recentSales().map((sale) => `${sale.id}. ${escapeHTML(sale.product)} → <code>${sale.recipient_id}</code> · ${sale.stars_price} ⭐`).join("\n");
      return editOrReply(ctx, `${tr(ctx.from.id, "adminRecentSales")}\n\n${lines || "—"}`, backKeyboard(language, "admin:menu"));
    }
    const promptKeys = {
      broadcast: "adminPromptBroadcast", stars: "adminPromptStars", premium: "adminPromptPremium", promo: "adminPromptPromo",
      giveaway: "adminPromptGiveaway", bonus: "adminPromptBonus", invoice: "adminPromptInvoice", access: "adminPromptAccess",
      refund: "adminPromptRefund", reply: "adminPromptReply", rate: "adminPromptRate",
    };
    if (promptKeys[action]) {
      db.setPending(ctx.from.id, `admin_${action}`, { operationID: `admin:${ctx.from.id}:${Date.now()}:${randomInt(1_000_000)}` });
      return editOrReply(ctx, tr(ctx.from.id, promptKeys[action], { product: config.productName.toUpperCase() }), backKeyboard(language, "admin:menu"));
    }
  });

  bot.on("message:text", async (ctx) => {
    if (ctx.message.text.startsWith("/")) return;
    const pending = db.pending(ctx.from.id);
    if (!pending) return;
    const input = ctx.message.text.trim();
    const language = languageOf(ctx.from.id);
    try {
      if (pending.kind === "account") {
        const id = Number(input);
        if (!Number.isSafeInteger(id) || id <= 0) throw new Error("invalid ID");
        db.setServerUserID(ctx.from.id, id); db.clearPending(ctx.from.id);
        return ctx.reply(tr(ctx.from.id, "accountSaved", { id }), { parse_mode: "HTML", reply_markup: mainKeyboard(language, isOwner(config, ctx.from.id)) });
      }
      if (pending.kind === "stars_amount") {
        const stars = Number(input);
        if (!Number.isSafeInteger(stars) || stars <= 0 || stars > 99999) throw new Error("amount must be from 1 to 99999");
        const product = findProduct(`stars_${stars}`, db.starsRate()); db.clearPending(ctx.from.id);
        const view = localizeProduct(product, language);
        return ctx.reply(productText(view, language), { parse_mode: "HTML", reply_markup: productKeyboard(product, db, ctx.from.id, language) });
      }
      if (pending.kind === "target") {
        const id = Number(input);
        if (!Number.isSafeInteger(id) || id <= 0) throw new Error("invalid ID");
        const product = findProduct(pending.payload.productCode, db.starsRate());
        if (!product) throw new Error("product not found");
        db.rememberRecipient(ctx.from.id, id); db.clearPending(ctx.from.id);
        if (product.kind === KINDS.username) {
          db.setPending(ctx.from.id, "username", { productCode: product.code, targetID: id });
          return ctx.reply(tr(ctx.from.id, "enterUsername"));
        }
        if (isOwner(config, ctx.from.id)) return fulfill(product, id, ctx.from, ctx.chat.id, `owner-${ctx.from.id}-${Date.now()}-${randomInt(1_000_000)}`);
        await sendInvoice(ctx, product, id, language); return;
      }
      if (pending.kind === "username") {
        const username = normalizeUsername(input);
        if (!username) throw new Error("username must be 5-32 latin characters and start with a letter");
        const product = findProduct(pending.payload.productCode, db.starsRate());
        if (!product) throw new Error("product not found");
        db.clearPending(ctx.from.id);
        if (isOwner(config, ctx.from.id)) return fulfill(product, pending.payload.targetID, ctx.from, ctx.chat.id, `owner-${ctx.from.id}-${Date.now()}-${randomInt(1_000_000)}`, username);
        await sendInvoice(ctx, product, pending.payload.targetID, language, username); return;
      }
      if (pending.kind === "support") {
        const ticket = db.addSupportMessage(ctx.from.id, ctx.chat.id, input); db.clearPending(ctx.from.id);
        for (const owner of config.ownerIDs) {
          const message = `${tr(owner, "supportOwnerTicket", { ticket })}\n${tr(owner, "supportOwnerFrom", { name: escapeHTML(userName(ctx.from)), id: ctx.from.id })}\n\n${escapeHTML(input)}`;
          await bot.api.sendMessage(owner, message, { parse_mode: "HTML" }).catch(() => {});
        }
        return ctx.reply(tr(ctx.from.id, "supportTicketSent", { ticket }));
      }
      if (!isOwner(config, ctx.from.id)) return;
      if (pending.kind === "admin_broadcast") {
        db.clearPending(ctx.from.id);
        let ok = 0, failed = 0;
        const recipients = db.notificationRecipients(config.notificationTTLDays);
        const skipped = db.users().length - recipients.length;
        for (const user of recipients) {
          try { await bot.api.sendMessage(user.chat_id, input, { parse_mode: "HTML" }); ok++; } catch { failed++; }
        }
        return ctx.reply(tr(ctx.from.id, "broadcastDone", { ok, skipped, failed }));
      }
      if (pending.kind === "admin_stars") {
        const [id, amount] = input.split(/\s+/).map(Number);
        if (!Number.isSafeInteger(id) || id <= 0 || !Number.isSafeInteger(amount) || amount <= 0) throw new Error("invalid ID or amount");
        await gramsrv.grantStars(id, amount, "Telegram bot administrator grant", pending.payload.operationID); db.clearPending(ctx.from.id);
        return ctx.reply(tr(ctx.from.id, "starsGranted", { id, amount }));
      }
      if (pending.kind === "admin_premium") {
        const [id, months] = input.split(/\s+/).map(Number);
        if (!Number.isSafeInteger(id) || id <= 0 || !Number.isSafeInteger(months) || months <= 0) throw new Error("invalid ID or months");
        await gramsrv.grantPremium(id, months, "Telegram bot administrator grant", pending.payload.operationID); db.clearPending(ctx.from.id);
        return ctx.reply(tr(ctx.from.id, "premiumGranted", { id, months }));
      }
      if (pending.kind === "admin_promo") {
        const [code, starsRaw, limitRaw] = input.split(/\s+/);
        const stars = Number(starsRaw), limit = Number(limitRaw);
        const normalized = db.createPromo(code, stars, limit); db.clearPending(ctx.from.id);
        return ctx.reply(tr(ctx.from.id, "promoCreated", { code: normalized }));
      }
      if (pending.kind === "admin_giveaway") {
        const [starsRaw, limitRaw, ...words] = input.split(/\s+/);
        const item = db.createGiveaway(words.join(" "), Number(starsRaw), Number(limitRaw)); db.clearPending(ctx.from.id);
        return ctx.reply(`🎁 ${escapeHTML(item.text)}`, { parse_mode: "HTML", reply_markup: new InlineKeyboard().text(tr(ctx.from.id, "claimReward"), `giveaway:${item.id}`) });
      }
      if (pending.kind === "admin_bonus") {
        const [id, amount] = input.split(/\s+/).map(Number);
        if (!Number.isSafeInteger(id) || id <= 0 || !Number.isSafeInteger(amount) || amount === 0) throw new Error("invalid ID or amount");
        const balance = db.addBonus(id, amount); db.clearPending(ctx.from.id);
        return ctx.reply(tr(ctx.from.id, "bonusBalance", { id, balance }));
      }
      if (pending.kind === "admin_invoice") {
        const [idRaw, starsRaw, ...words] = input.split(/\s+/);
        const id = Number(idRaw), stars = Number(starsRaw), title = words.join(" ").trim();
        if (!Number.isSafeInteger(id) || id <= 0 || !Number.isSafeInteger(stars) || stars <= 0 || !title || title.length > 32) throw new Error("invalid invoice");
        const targetLanguage = languageOf(id);
        await bot.api.sendInvoice(id, title, translate(targetLanguage, "invoiceDescription", { title }), `custom|${Buffer.from(title).toString("base64url")}`, "XTR", [{ label: title, amount: stars }]);
        db.clearPending(ctx.from.id); return ctx.reply(tr(ctx.from.id, "invoiceSent"));
      }
      if (pending.kind === "admin_access") {
        const [phone, telegramRaw] = input.split(/\s+/); const telegramID = Number(telegramRaw);
        if (!phone || !Number.isSafeInteger(telegramID) || telegramID <= 0) throw new Error("invalid phone or Telegram ID");
        db.grantCodeAccess(phone, telegramID); db.clearPending(ctx.from.id);
        return ctx.reply(tr(ctx.from.id, "accessGranted", { phone: escapeHTML(phone), id: telegramID }), { parse_mode: "HTML" });
      }
      if (pending.kind === "admin_refund") {
        const parts = input.split(/\s+/); let telegramID, chargeID, sale;
        if (parts.length === 1) { chargeID = parts[0]; sale = db.saleByCharge(chargeID); telegramID = sale?.buyer_id; }
        else if (parts.length === 2) { telegramID = positiveInteger(parts[0]); chargeID = parts[1]; }
        if (!telegramID || !chargeID) throw new Error("sale not found for this transaction ID");
        if (db.isRefunded(chargeID)) throw new Error("payment was already refunded");
        sale ??= db.saleByCharge(chargeID);
        if (!sale || sale.buyer_id !== telegramID || sale.payment_status !== "done") throw new Error("completed sale or its owner was not found");
        await executeCompensatedRefund({ sale, telegramID, db, gramsrv, refundStarPayment: bot.api.refundStarPayment.bind(bot.api) });
        db.clearPending(ctx.from.id);
        await bot.api.sendMessage(telegramID, tr(telegramID, "paymentRefunded", { charge: escapeHTML(chargeID) }), { parse_mode: "HTML" }).catch(() => {});
        return ctx.reply(tr(ctx.from.id, "refundDone"));
      }
      if (pending.kind === "admin_reply") {
        const [ticketRaw, ...words] = input.split(/\s+/); const ticketID = Number(ticketRaw), answer = words.join(" ").trim();
        const ticket = db.supportMessage(ticketID);
        if (!ticket || !answer) throw new Error("ticket not found or reply is empty");
        await bot.api.sendMessage(ticket.chat_id, tr(ticket.telegram_id, "supportReply", { ticket: ticketID, answer: escapeHTML(answer) }), { parse_mode: "HTML" });
        db.closeSupportMessage(ticketID); db.clearPending(ctx.from.id);
        return ctx.reply(tr(ctx.from.id, "supportReplySent", { ticket: ticketID }));
      }
      if (pending.kind === "admin_rate") {
        const rate = Number(input);
        if (!Number.isSafeInteger(rate) || rate <= 0) throw new Error("invalid rate");
        db.setSetting("stars_rate", rate); db.clearPending(ctx.from.id);
        return ctx.reply(tr(ctx.from.id, "rateSaved", { rate }));
      }
    } catch (error) {
      console.error("Bot input action failed", pending.kind, error);
      await ctx.reply(translateError(language, error));
    }
  });

  bot.catch(async ({ error, ctx }) => {
    if (error instanceof GrammyError) console.error("Telegram API error", error.description);
    else if (error instanceof HttpError) console.error("Telegram network error", error);
    else console.error(`Bot update ${ctx.update.update_id} failed`, error);
    if (ctx.from && ctx.chat) await ctx.reply(tr(ctx.from.id, "genericError")).catch(() => {});
  });
  return bot;
}
