#!/usr/bin/env python3
import http.server
import json
import logging
import os
import random
import socketserver
import subprocess
import time
import urllib.parse

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")
PORT = 2408
STATIC_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "static_upgrader")

# Emoji mapping for official Telegram Star Gifts
GIFT_ICONS = {
    "plush pepe": "🐸",
    "nail bracelet": "💍",
    "precious peach": "🍑",
    "homemade cake": "🎂",
    "chill flame": "🔥",
    "eternal rose": "🌹",
    "surge board": "🏄",
    "swag bag": "🎒",
    "candy cane": "🍬",
    "xmas stocking": "🧦",
    "witch hat": "🧙",
    "kissed frog": "🐸",
    "crystal ball": "🔮",
    "flying broom": "🧹",
    "voodoo doll": "🪆",
    "hex pot": "🧪",
    "evil eye": "🧿",
    "spy agaric": "🍄",
    "lol pop": "🍭",
    "spiced wine": "🍷",
    "party sparkler": "✨",
    "cookie heart": "💖",
    "ginger cookie": "🍪",
    "jester hat": "🤡",
    "diamond ring": "💎",
    "top hat": "🎩",
    "love potion": "🧪",
    "mighty arm": "💪",
    "westside sign": "🤟",
    "heart locket": "📿",
    "heroic helmet": "🪖",
    "low rider": "🚗",
    "artisan brick": "🧱",
    "rare bird": "🦜",
    "neko helmet": "🐱",
    "loot bag": "💰",
    "scared cat": "🙀",
    "instant ramen": "🍜",
    "magic potion": "🧪",
    "snow globe": "🔮",
    "swiss watch": "⌚",
    "vintage cigar": "🚬"
}

def get_gift_icon(title):
    if not title:
        return "🎁"
    t_clean = title.strip().lower()
    for k, icon in GIFT_ICONS.items():
        if k in t_clean:
            return icon
    return "🎁"

def int_to_hex_color(val, fallback="#2C2C2E"):
    if not val or val <= 0:
        return fallback
    return f"#{int(val) & 0xFFFFFF:06x}"

def run_db_query(sql):
    cmd = ['docker', 'exec', '-i', 'telesrv-postgres', 'psql', '-U', 'telesrv', '-d', 'telesrv', '-t', '-A', '-F', '|']
    p = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, encoding='utf-8')
    stdout, stderr = p.communicate(sql)
    if stderr and "ERROR" in stderr:
        logging.error(f"SQL error: {stderr.strip()}")
        return None
    return stdout.strip()

# Real NFT targets in catalog
NFT_TARGETS = [
    {"title": "Plush Pepe NFT", "base_title": "Plush Pepe", "stars": 2000, "gift_id": 9000000000000156, "coll_rev_id": 142, "slug_prefix": "official-5936013938331222567", "icon": "🐸", "bg_center": "#363738", "bg_edge": "#0e0f0f", "model_name": "Fifty Shades"},
    {"title": "Nail Bracelet NFT", "base_title": "Nail Bracelet", "stars": 10000, "gift_id": 9000000000000142, "coll_rev_id": 128, "slug_prefix": "official-5870720080265871962", "icon": "💍", "bg_center": "#e0192e", "bg_edge": "#a8384b", "model_name": "Resistance"},
    {"title": "Surge Board NFT", "base_title": "Surge Board", "stars": 2500, "gift_id": 9000000000000059, "coll_rev_id": 48, "slug_prefix": "official-5832497899283415733", "icon": "🏄", "bg_center": "#2ba0d8", "bg_edge": "#155075", "model_name": "Aqua Wave"},
    {"title": "Precious Peach NFT", "base_title": "Precious Peach", "stars": 1000, "gift_id": 9000000000000144, "coll_rev_id": 130, "slug_prefix": "official-5933671725160989227", "icon": "🍑", "bg_center": "#e07850", "bg_edge": "#b64e32", "model_name": "Pure Peach"},
    {"title": "Homemade Cake NFT", "base_title": "Homemade Cake", "stars": 500, "gift_id": 9000000000000056, "coll_rev_id": 45, "slug_prefix": "official-5783075783622787539", "icon": "🎂", "bg_center": "#58b548", "bg_edge": "#388231", "model_name": "It's My Party"},
    {"title": "Swag Bag NFT", "base_title": "Swag Bag", "stars": 500, "gift_id": 9000000000000037, "coll_rev_id": 26, "slug_prefix": "official-6012607142387778152", "icon": "🎒", "bg_center": "#7eb0d4", "bg_edge": "#587a98", "model_name": "Platinum Drip"},
    {"title": "Chill Flame NFT", "base_title": "Chill Flame", "stars": 150, "gift_id": 9000000000000030, "coll_rev_id": 19, "slug_prefix": "official-5999277561060787166", "icon": "🔥", "bg_center": "#adaf40", "bg_edge": "#6b7d24", "model_name": "Ionic Column"},
    {"title": "Eternal Rose NFT", "base_title": "Eternal Rose", "stars": 100, "gift_id": 9000000000000093, "coll_rev_id": 80, "slug_prefix": "official-5882125812596999035", "icon": "🌹", "bg_center": "#58a3d8", "bg_edge": "#386588", "model_name": "Moonstone"},
]

def get_catalog():
    sql = """
    SELECT c.gift_id, r.title, r.stars, r.convert_stars
    FROM star_gift_catalog c
    JOIN star_gift_catalog_revisions r ON r.id = c.active_revision_id
    WHERE c.enabled = true
    ORDER BY r.stars ASC, r.title ASC;
    """
    raw = run_db_query(sql)
    catalog = []
    
    # 1. Real catalog star gifts
    if raw:
        for line in raw.splitlines():
            parts = line.split("|")
            if len(parts) >= 3 and parts[0]:
                gid = int(parts[0])
                title = parts[1]
                if "Official gift" in title:
                    title = f"Star Gift #{gid % 1000}"
                stars = int(parts[2]) if parts[2] else 0
                conv = int(parts[3]) if len(parts) > 3 and parts[3] else int(stars * 0.85)
                catalog.append({
                    "id": gid,
                    "title": title,
                    "stars": stars,
                    "convert_stars": conv,
                    "is_nft": False,
                    "icon": get_gift_icon(title),
                    "bg_center": "#2C2C2E",
                    "bg_edge": "#1C1C1E",
                    "model_name": ""
                })

    # 2. Real Collectible NFT targets (high tier)
    for nft in NFT_TARGETS:
        catalog.append({
            "id": nft["gift_id"],
            "title": nft["title"],
            "base_title": nft["base_title"],
            "stars": nft["stars"],
            "convert_stars": int(nft["stars"] * 0.85),
            "is_nft": True,
            "icon": nft["icon"],
            "bg_center": nft["bg_center"],
            "bg_edge": nft["bg_edge"],
            "model_name": nft["model_name"],
            "coll_rev_id": nft["coll_rev_id"],
            "slug_prefix": nft["slug_prefix"]
        })

    # Sort by stars
    catalog.sort(key=lambda x: (x["stars"], x["is_nft"]))
    return catalog

def get_user_data(user_id):
    if not user_id or user_id <= 0:
        return {"user_id": 0, "first_name": "Игрок", "stars": 0, "gifts": []}

    # Query user profile
    u_raw = run_db_query(f"SELECT first_name, username FROM users WHERE id = {user_id};")
    first_name = "Игрок"
    username = ""
    if u_raw:
        u_parts = u_raw.split("|")
        first_name = u_parts[0] if u_parts[0] else "Игрок"
        username = u_parts[1] if len(u_parts) > 1 else ""

    # Query stars balance
    stars_raw = run_db_query(f"SELECT balance FROM stars_balances WHERE user_id = {user_id};")
    stars = int(stars_raw) if stars_raw and stars_raw.isdigit() else 0

    # Query profile gifts: BOTH regular gifts and NFT collectibles!
    sql = f"""
    SELECT 
        p.id as peer_gift_id, 
        p.gift_id, 
        COALESCE(p.unique_gift_id, 0) as unique_gift_id,
        COALESCE(u.title, r.title, 'Star Gift') as title,
        COALESCE(u.num, 0) as num,
        COALESCE(r.stars, 100) as stars,
        COALESCE(r.convert_stars, 85) as convert_stars,
        COALESCE(m.name, '') as model_name,
        COALESCE(b.center_color, 0) as center_color,
        COALESCE(b.edge_color, 0) as edge_color
    FROM peer_star_gifts p
    LEFT JOIN unique_star_gifts u ON u.id = p.unique_gift_id
    LEFT JOIN star_gift_catalog_revisions r ON r.id = p.catalog_revision_id
    LEFT JOIN star_gift_collectible_models m ON m.id = u.model_attribute_id
    LEFT JOIN star_gift_collectible_backdrops b ON b.id = u.backdrop_attribute_id
    WHERE p.owner_peer_id = {user_id} 
      AND p.converted = false 
      AND p.unsaved = false
      AND p.lifecycle_status = 'active'
    ORDER BY (p.unique_gift_id IS NOT NULL AND p.unique_gift_id > 0) DESC, p.id DESC;
    """
    raw = run_db_query(sql)
    gifts = []
    if raw:
        for line in raw.splitlines():
            parts = line.split("|")
            if len(parts) >= 8 and parts[0]:
                pid = int(parts[0])
                gid = int(parts[1])
                uid = int(parts[2])
                raw_title = parts[3]
                num = int(parts[4])
                gstars = int(parts[5]) if parts[5] else 100
                conv = int(parts[6]) if parts[6] else int(gstars * 0.85)
                mname = parts[7] if len(parts) > 7 else ""
                ccolor = int(parts[8]) if len(parts) > 8 and parts[8] else 0
                ecolor = int(parts[9]) if len(parts) > 9 and parts[9] else 0
                
                is_nft = uid > 0
                display_title = f"{raw_title} #{num}" if is_nft else raw_title
                bg_center = int_to_hex_color(ccolor, "#2C2C2E" if not is_nft else "#3A3A3C")
                bg_edge = int_to_hex_color(ecolor, "#1C1C1E" if not is_nft else "#1A1A1C")

                gifts.append({
                    "peer_gift_id": pid,
                    "gift_id": gid,
                    "unique_gift_id": uid,
                    "is_nft": is_nft,
                    "num": num,
                    "title": display_title,
                    "base_title": raw_title,
                    "stars": gstars,
                    "convert_stars": conv,
                    "icon": get_gift_icon(raw_title),
                    "model_name": mname,
                    "bg_center": bg_center,
                    "bg_edge": bg_edge
                })

    return {
        "user_id": user_id,
        "first_name": first_name,
        "username": username,
        "stars": stars,
        "gifts": gifts
    }

class UpgraderHandler(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=STATIC_DIR, **kwargs)

    def end_headers(self):
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Headers", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        self.send_header("Cache-Control", "no-cache, no-store, must-revalidate")
        super().end_headers()

    def do_OPTIONS(self):
        self.send_response(200)
        self.end_headers()

    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        path = parsed.path
        query = urllib.parse.parse_qs(parsed.query)

        if path == "/api/catalog":
            catalog = get_catalog()
            self.send_response(200)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.end_headers()
            self.wfile.write(json.dumps(catalog).encode("utf-8"))
            return

        if path == "/api/user-data":
            user_id = int(query.get("user_id", ["0"])[0])
            data = get_user_data(user_id)
            self.send_response(200)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.end_headers()
            self.wfile.write(json.dumps(data).encode("utf-8"))
            return

        super().do_GET()

    def do_POST(self):
        if self.path == "/api/upgrade":
            content_length = int(self.headers.get("Content-Length", 0))
            body = self.rfile.read(content_length).decode("utf-8")
            try:
                data = json.loads(body)
                user_id = int(data.get("user_id", 0))
                source_peer_gift_id = int(data.get("source_peer_gift_id", 0))
                src_stars = float(data.get("source_stars", 50))
                target_gift_id = int(data.get("target_gift_id", 0))
                target_is_nft = bool(data.get("target_is_nft", False))
                tgt_stars = float(data.get("target_stars", 200))
                tgt_title = data.get("target_title", "Star Gift")

                if user_id <= 0:
                    self.send_json_error(400, "Пользователь не авторизован")
                    return

                if src_stars <= 0 or tgt_stars <= 0 or tgt_stars <= src_stars:
                    self.send_json_error(400, "Некорректная сумма звёзд для апгрейда")
                    return

                source_is_nft = False
                source_unique_id = 0

                # Validate user balance or source gift
                if source_peer_gift_id > 0:
                    gift_check = run_db_query(f"""
                        SELECT id, COALESCE(unique_gift_id, 0) 
                        FROM peer_star_gifts 
                        WHERE id = {source_peer_gift_id} 
                          AND owner_peer_id = {user_id} 
                          AND converted = false 
                          AND unsaved = false 
                          AND lifecycle_status = 'active';
                    """)
                    if not gift_check:
                        self.send_json_error(400, "Выбранный подарок не найден или уже был улучшен")
                        return
                    parts = gift_check.split("|")
                    source_unique_id = int(parts[1]) if len(parts) > 1 and parts[1] else 0
                    source_is_nft = source_unique_id > 0
                else:
                    # Check stars balance
                    bal_raw = run_db_query(f"SELECT balance FROM stars_balances WHERE user_id = {user_id};")
                    curr_bal = int(bal_raw) if bal_raw and bal_raw.isdigit() else 0
                    if curr_bal < int(src_stars):
                        self.send_json_error(400, f"Недостаточно звёзд на балансе (у вас {curr_bal} ⭐, нужно {int(src_stars)} ⭐)")
                        return

                # Provably fair roll with 5% house edge
                ratio = (src_stars / tgt_stars) * 0.95
                chance = min(95.0, max(1.0, ratio * 100.0))
                win_angle = (chance / 100.0) * 360.0

                roll = random.random() * 100.0
                is_win = roll < chance

                if is_win:
                    stop_angle = random.uniform(2.0, max(2.0, win_angle - 2.0))
                else:
                    stop_angle = random.uniform(min(358.0, win_angle + 2.0), 358.0)

                now_ts = int(time.time())
                won_nft_details = None

                # 1. Deduct source
                if source_peer_gift_id > 0:
                    if source_is_nft:
                        # NFT Burn
                        run_db_query(f"""
                            BEGIN;
                            UPDATE peer_star_gifts 
                            SET lifecycle_status = 'burned', converted = false, unsaved = true 
                            WHERE id = {source_peer_gift_id} AND owner_peer_id = {user_id};
                            UPDATE unique_star_gifts 
                            SET burned = true 
                            WHERE id = {source_unique_id};
                            COMMIT;
                        """)
                    else:
                        # Regular Gift Convert
                        run_db_query(f"""
                            UPDATE peer_star_gifts 
                            SET converted = true, unsaved = true, lifecycle_status = 'converted' 
                            WHERE id = {source_peer_gift_id} AND owner_peer_id = {user_id};
                        """)
                else:
                    # Stars deduction
                    run_db_query(f"""
                        BEGIN;
                        UPDATE stars_balances 
                        SET balance = balance - {int(src_stars)} 
                        WHERE user_id = {user_id} AND balance >= {int(src_stars)};
                        INSERT INTO stars_transactions (
                            user_id, peer_type, peer_id, amount, reason, title, description, date
                        ) VALUES (
                            {user_id}, 'user', {user_id}, -{int(src_stars)}, 
                            'star_gift_upgrade', 'Upgrader', 'Апгрейд подарка в Upgrader', {now_ts}
                        );
                        COMMIT;
                    """)

                # 2. If Won, award target gift or NFT!
                if is_win:
                    gift_msg_id = int(now_ts % 1000000 + 1)
                    if target_is_nft:
                        # Mint real NFT Collectible!
                        clean_target_title = tgt_title.replace(" NFT", "").strip()
                        # Find collectible revision
                        rev_info = run_db_query(f"""
                            SELECT cr.id, cr.slug_prefix, cr.gift_id
                            FROM star_gift_collectible_revisions cr
                            JOIN star_gift_catalog_revisions r ON r.gift_id = cr.gift_id
                            WHERE r.title ILIKE '%{clean_target_title}%' OR cr.gift_id = {target_gift_id}
                            LIMIT 1;
                        """)
                        coll_rev_id = 142
                        slug_prefix = "official-5936013938331222567"
                        nft_gift_id = target_gift_id
                        if rev_info:
                            rparts = rev_info.split("|")
                            coll_rev_id = int(rparts[0])
                            slug_prefix = rparts[1]
                            nft_gift_id = int(rparts[2])

                        # Next serial number
                        next_num_raw = run_db_query(f"SELECT COALESCE(MAX(num), 0) + 1 FROM unique_star_gifts WHERE gift_id = {nft_gift_id};")
                        next_num = int(next_num_raw.strip().splitlines()[0]) if next_num_raw and next_num_raw.strip().splitlines()[0].isdigit() else 1
                        if next_num == 1488:
                            next_num = 1489

                        # Pick model, pattern, backdrop matching collectible_revision_id
                        model_raw = run_db_query(f"SELECT id, name FROM star_gift_collectible_models WHERE collectible_revision_id = {coll_rev_id} ORDER BY random() LIMIT 1;")
                        if not model_raw:
                            model_raw = run_db_query("SELECT id, name FROM star_gift_collectible_models ORDER BY random() LIMIT 1;")
                        model_parts = model_raw.strip().splitlines()[0].split("|") if model_raw else ["689", "Custom Model"]
                        model_id = int(model_parts[0])
                        model_name = model_parts[1] if len(model_parts) > 1 else "Custom Model"

                        pattern_raw = run_db_query(f"SELECT id FROM star_gift_collectible_patterns WHERE collectible_revision_id = {coll_rev_id} ORDER BY random() LIMIT 1;")
                        pattern_id = int(pattern_raw.strip().splitlines()[0]) if pattern_raw and pattern_raw.strip().splitlines()[0].isdigit() else 1

                        backdrop_raw = run_db_query(f"SELECT id, center_color, edge_color FROM star_gift_collectible_backdrops WHERE collectible_revision_id = {coll_rev_id} ORDER BY random() LIMIT 1;")
                        bparts = backdrop_raw.strip().splitlines()[0].split("|") if backdrop_raw else ["1", "7914885", "4366705"]
                        backdrop_id = int(bparts[0])
                        bg_center = int_to_hex_color(int(bparts[1]))
                        bg_edge = int_to_hex_color(int(bparts[2]))

                        # Allocate IDs from sequence to insert pre-linked
                        seq_raw = run_db_query("SELECT nextval('user_star_gifts_id_seq'), nextval('unique_star_gift_id_seq');")
                        seq_parts = seq_raw.strip().splitlines()[0].split("|") if seq_raw else ["0", "0"]
                        new_peer_id = int(seq_parts[0])
                        new_unique_id = int(seq_parts[1])

                        if new_peer_id > 0 and new_unique_id > 0:
                            unique_slug = f"{slug_prefix}-{next_num}"
                            nft_tx_sql = f"""
                                BEGIN;
                                INSERT INTO peer_star_gifts (
                                    id, owner_peer_id, from_user_id, gift_id, unique_gift_id, msg_id, gift_date,
                                    name_hidden, unsaved, converted, convert_stars, owner_peer_type,
                                    saved_id, catalog_revision_id, pinned_order, prepaid_upgrade_stars,
                                    lifecycle_status, transfer_stars, message_entities
                                ) VALUES (
                                    {new_peer_id}, {user_id}, {user_id}, {nft_gift_id}, {new_unique_id}, {gift_msg_id}, {now_ts},
                                    false, false, false, {int(tgt_stars * 0.85)}, 'user',
                                    0, {coll_rev_id}, 0, 0,
                                    'active', 25, '[]'::jsonb
                                );

                                INSERT INTO unique_star_gifts (
                                    id, gift_id, collectible_revision_id, source_saved_gift_id, title, slug, num,
                                    owner_peer_type, owner_peer_id, model_attribute_id, pattern_attribute_id, backdrop_attribute_id,
                                    keep_original_details, original_owner_peer_type, original_owner_peer_id,
                                    value_amount, value_currency
                                ) VALUES (
                                    {new_unique_id}, {nft_gift_id}, {coll_rev_id}, {new_peer_id}, '{clean_target_title}', '{unique_slug}', {next_num},
                                    'user', {user_id}, {model_id}, {pattern_id}, {backdrop_id},
                                    false, 'user', {user_id},
                                    {int(tgt_stars)}, 'XTR'
                                );

                                UPDATE star_gift_collectible_revisions SET issued = issued + 1 WHERE id = {coll_rev_id};
                                COMMIT;
                            """
                            run_db_query(nft_tx_sql)

                        won_nft_details = {
                            "is_nft": True,
                            "title": f"{clean_target_title} #{next_num}",
                            "num": next_num,
                            "model_name": model_name,
                            "bg_center": bg_center,
                            "bg_edge": bg_edge,
                            "icon": get_gift_icon(clean_target_title)
                        }

                    else:
                        # Award regular star gift
                        run_db_query(f"""
                            INSERT INTO peer_star_gifts (
                                owner_peer_id, from_user_id, gift_id, msg_id, gift_date,
                                name_hidden, unsaved, converted, convert_stars, owner_peer_type,
                                saved_id, catalog_revision_id, pinned_order, prepaid_upgrade_stars,
                                lifecycle_status, transfer_stars, message_entities
                            )
                            SELECT
                                {user_id}, {user_id}, c.gift_id, {gift_msg_id}, {now_ts},
                                false, false, false, r.convert_stars, 'user',
                                0, c.active_revision_id, 0, 0,
                                'active', 25, '[]'::jsonb
                            FROM star_gift_catalog c
                            JOIN star_gift_catalog_revisions r ON r.id = c.active_revision_id
                            WHERE c.gift_id = {target_gift_id}
                            LIMIT 1;
                        """)
                        won_nft_details = {
                            "is_nft": False,
                            "title": tgt_title,
                            "num": 0,
                            "model_name": "",
                            "bg_center": "#2C2C2E",
                            "bg_edge": "#1C1C1E",
                            "icon": get_gift_icon(tgt_title)
                        }

                # Flush read models
                run_db_query("NOTIFY telesrv_read_model_changed, 'star_gifts'; NOTIFY telesrv_read_model_changed, 'account_settings';")

                # Get updated balance
                new_bal_raw = run_db_query(f"SELECT balance FROM stars_balances WHERE user_id = {user_id};")
                new_balance = int(new_bal_raw) if new_bal_raw and new_bal_raw.isdigit() else 0

                resp_payload = {
                    "success": True,
                    "is_win": is_win,
                    "win_chance": round(chance, 1),
                    "stop_angle": round(stop_angle, 1),
                    "win_angle": round(win_angle, 1),
                    "new_balance": new_balance,
                    "won_gift": won_nft_details
                }

                self.send_response(200)
                self.send_header("Content-Type", "application/json; charset=utf-8")
                self.end_headers()
                self.wfile.write(json.dumps(resp_payload).encode("utf-8"))

            except Exception as e:
                logging.error(f"Error handling upgrade API: {e}", exc_info=True)
                self.send_json_error(500, f"Ошибка сервера: {str(e)}")
        else:
            self.send_json_error(404, "Endpoint not found")

    def send_json_error(self, code, message):
        self.send_response(code)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.end_headers()
        self.wfile.write(json.dumps({"success": False, "error": message}).encode("utf-8"))

    def log_message(self, format, *args):
        return

def main():
    os.makedirs(STATIC_DIR, exist_ok=True)
    socketserver.TCPServer.allow_reuse_address = True
    with socketserver.TCPServer(("0.0.0.0", PORT), UpgraderHandler) as httpd:
        logging.info(f"Star Gifts Upgrader running on port {PORT} with live PostgreSQL backend...")
        httpd.serve_forever()

if __name__ == "__main__":
    main()
