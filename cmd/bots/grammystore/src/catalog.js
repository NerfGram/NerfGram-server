export const KINDS = Object.freeze({ premium: "premium", stars: "stars", number: "number", username: "username" });

const fixed = Object.freeze([
  { kind: KINDS.premium, code: "premium_1m", title: "Premium — 1 month", titleRu: "Premium — 1 месяц", description: "Premium subscription for one month", descriptionRu: "Подписка Premium на один месяц", starsPrice: 20, months: 1 },
  { kind: KINDS.premium, code: "premium_3m", title: "Premium — 3 months", titleRu: "Premium — 3 месяца", description: "Premium subscription for three months", descriptionRu: "Подписка Premium на три месяца", starsPrice: 40, months: 3 },
  { kind: KINDS.number, code: "num_short", title: "Anonymous +888 8 XXX", titleRu: "Анонимный +888 8 XXX", description: "Short collectible anonymous number", descriptionRu: "Короткий коллекционный анонимный номер", starsPrice: 50, numberFormat: "short" },
  { kind: KINDS.number, code: "num_long", title: "Anonymous +888 0XXX XXXX", titleRu: "Анонимный +888 0XXX XXXX", description: "Anonymous +888 number", descriptionRu: "Анонимный номер +888", starsPrice: 25, numberFormat: "long" },
  { kind: KINDS.username, code: "uname_10", title: "Collectible username — 10 TON", titleRu: "Коллекционный username — 10 TON", description: "Mint a collectible username", descriptionRu: "Выпустить коллекционный username", starsPrice: 10, bid: 10 },
  { kind: KINDS.username, code: "uname_100", title: "Collectible username — 100 TON", titleRu: "Коллекционный username — 100 TON", description: "Mint a collectible username", descriptionRu: "Выпустить коллекционный username", starsPrice: 20, bid: 100 },
  { kind: KINDS.username, code: "uname_1000", title: "Collectible username — 1000 TON", titleRu: "Коллекционный username — 1000 TON", description: "Mint a collectible username", descriptionRu: "Выпустить коллекционный username", starsPrice: 40, bid: 1000 },
]);

export function catalog(starsRate = 20) {
  const starPackages = [1, 5, 10, 25, 50, 100].map((price) => ({
    kind: KINDS.stars,
    code: `stars_${price}`,
    title: `${price * starsRate} Stars`,
    description: `${price * starsRate} server Stars for ${price} Telegram Stars`,
    descriptionRu: `${price * starsRate} серверных Stars за ${price} Telegram Stars`,
    starsPrice: price,
    starsAmount: price * starsRate,
  }));
  return [...fixed, ...starPackages];
}

export function findProduct(code, starsRate = 20) {
  const fixedProduct = catalog(starsRate).find((product) => product.code === code);
  if (fixedProduct) return fixedProduct;
  const match = String(code).match(/^stars_([1-9]\d{0,5})$/);
  if (!match) return null;
  const starsPrice = Number(match[1]);
  if (starsPrice > 100000) return null;
  return {
    kind: KINDS.stars,
    code: `stars_${starsPrice}`,
    title: `${starsPrice * starsRate} Stars`,
    description: `${starsPrice * starsRate} server Stars for ${starsPrice} Telegram Stars`,
    descriptionRu: `${starsPrice * starsRate} серверных Stars за ${starsPrice} Telegram Stars`,
    starsPrice,
    starsAmount: starsPrice * starsRate,
  };
}

export function productsOfKind(kind, starsRate = 20) {
  return catalog(starsRate).filter((product) => product.kind === kind);
}

export function localizeProduct(product, language = "en") {
  if (!product) return null;
  if (language !== "ru") return product;
  return { ...product, title: product.titleRu ?? product.title, description: product.descriptionRu ?? product.description };
}

export function normalizeUsername(value) {
  const username = String(value ?? "").trim().replace(/^@/, "").toLowerCase();
  return /^[a-z][a-z0-9_]{4,31}$/.test(username) ? username : "";
}

export function buildPayload(productCode, targetUserID = 0, extra = "", starsAmount = 0) {
  const encoded = Buffer.from(extra, "utf8").toString("base64url");
  return `store|${productCode}|${targetUserID}|${encoded}|${starsAmount}`;
}

export function parsePayload(payload) {
  const [scope, code, target, encoded = "", rawStarsAmount = "0"] = String(payload).split("|", 5);
  const targetUserID = Number(target);
  const starsAmount = Number(rawStarsAmount || 0);
  if (scope !== "store" || !code || !Number.isSafeInteger(targetUserID) || targetUserID < 0 ||
      !Number.isSafeInteger(starsAmount) || starsAmount < 0) throw new Error("invalid invoice payload");
  return { code, targetUserID, extra: Buffer.from(encoded, "base64url").toString("utf8"), starsAmount };
}
