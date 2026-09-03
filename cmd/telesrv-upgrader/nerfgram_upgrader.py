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

# Load local gifts images and data
GIFTS_DATA_FILE = os.path.join(STATIC_DIR, "gifts_data.json")
GIFTS_CACHE = []
TITLE_TO_IMAGE = {}
if os.path.exists(GIFTS_DATA_FILE):
    try:
        with open(GIFTS_DATA_FILE, "r", encoding="utf-8") as f:
            GIFTS_CACHE = json.load(f)
            for g in GIFTS_CACHE:
                t = g.get("title", "").strip().lower()
                img = g.get("image", "")
                if t and img:
                    TITLE_TO_IMAGE[t] = img
        logging.info(f"Loaded {len(GIFTS_CACHE)} gifts from gifts_data.json ({len(TITLE_TO_IMAGE)} images mapped)")
    except Exception as e:
        logging.error(f"Failed to load gifts_data.json: {e}")

def resolve_gift_image(title, gift_id):
    if not title:
        title = ""
    t_clean = title.strip().lower()
    if t_clean in TITLE_TO_IMAGE:
        return TITLE_TO_IMAGE[t_clean]
    # Check partial
    for k, v in TITLE_TO_IMAGE.items():
        if k in t_clean or t_clean in k:
            return v
    if GIFTS_CACHE:
        idx = abs(int(gift_id)) % len(GIFTS_CACHE)
        return GIFTS_CACHE[idx].get("image", "gifts/5226539835577100016.webp")
    return "gifts/5226539835577100016.webp"

def run_db_query(sql):
    cmd = ['docker', 'exec', '-i', 'telesrv-postgres', 'psql', '-U', 'telesrv', '-d', 'telesrv', '-t', '-A', '-F', '|']
    p = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, encoding='utf-8')
    stdout, stderr = p.communicate(sql)
    if stderr and "ERROR" in stderr:
        logging.error(f"SQL error: {stderr.strip()}")
        return None
    return stdout.strip()

def get_catalog():
    sql = """
    SELECT c.gift_id, r.title, r.stars, r.convert_stars, r.document_id
    FROM star_gift_catalog c
    JOIN star_gift_catalog_revisions r ON r.id = c.active_revision_id
    WHERE c.enabled = true
    ORDER BY r.stars ASC, r.title ASC;
    """
    raw = run_db_query(sql)
    catalog = []
    if not raw:
        # Fallback to GIFTS_CACHE
        for item in GIFTS_CACHE[:40]:
            catalog.append({
                "id": item["id"],
                "title": item["title"],
                "stars": item["stars"],
                "convert_stars": int(item["stars"] * 0.85),
                "image": item["image"]
            })
        return catalog

    for line in raw.splitlines():
        parts = line.split("|")
        if len(parts) >= 4 and parts[0]:
            gid = int(parts[0])
            title = parts[1]
            if "Official gift" in title:
                title = f"Star Gift #{gid % 1000}"
            stars = int(parts[2]) if parts[2] else 0
            conv = int(parts[3]) if parts[3] else int(stars * 0.85)
            doc_id = int(parts[4]) if len(parts) > 4 and parts[4] else 0
            img = resolve_gift_image(title, gid)
            catalog.append({
                "id": gid,
                "title": title,
                "stars": stars,
                "convert_stars": conv,
                "image": img,
                "document_id": doc_id
            })
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

    # Query profile gifts (excluding unique collectibles and already converted)
    sql = f"""
    SELECT p.id, c.gift_id, r.title, r.stars, r.convert_stars, r.document_id
    FROM peer_star_gifts p
    JOIN star_gift_catalog c ON c.gift_id = p.gift_id
    JOIN star_gift_catalog_revisions r ON r.id = c.active_revision_id
    WHERE p.owner_peer_id = {user_id} 
      AND p.converted = false 
      AND p.unsaved = false
      AND (p.unique_gift_id IS NULL OR p.unique_gift_id = 0)
    ORDER BY p.id DESC;
    """
    raw = run_db_query(sql)
    gifts = []
    if raw:
        for line in raw.splitlines():
            parts = line.split("|")
            if len(parts) >= 4 and parts[0]:
                pid = int(parts[0])
                gid = int(parts[1])
                title = parts[2]
                if "Official gift" in title:
                    title = f"Star Gift #{gid % 1000}"
                gstars = int(parts[3]) if parts[3] else 0
                conv = int(parts[4]) if parts[4] else int(gstars * 0.85)
                doc_id = int(parts[5]) if len(parts) > 5 and parts[5] else 0
                img = resolve_gift_image(title, gid)
                gifts.append({
                    "peer_gift_id": pid,
                    "gift_id": gid,
                    "title": title,
                    "stars": gstars,
                    "convert_stars": conv,
                    "image": img,
                    "document_id": doc_id
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
                tgt_stars = float(data.get("target_stars", 200))
                tgt_title = data.get("target_title", "Star Gift")

                if user_id <= 0:
                    self.send_json_error(400, "Пользователь не авторизован")
                    return

                if src_stars <= 0 or tgt_stars <= 0 or tgt_stars <= src_stars:
                    self.send_json_error(400, "Некорректная сумма звёзд для апгрейда")
                    return

                # Validate user balance or source gift
                if source_peer_gift_id > 0:
                    gift_check = run_db_query(f"""
                        SELECT id FROM peer_star_gifts 
                        WHERE id = {source_peer_gift_id} 
                          AND owner_peer_id = {user_id} 
                          AND converted = false 
                          AND unsaved = false;
                    """)
                    if not gift_check:
                        self.send_json_error(400, "Выбранный подарок не найден или уже был улучшен")
                        return
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

                # Database atomic transaction
                now_ts = int(time.time())
                tx_sql = ["BEGIN;"]

                if source_peer_gift_id > 0:
                    tx_sql.append(f"""
                        UPDATE peer_star_gifts 
                        SET converted = true, unsaved = true, lifecycle_status = 'converted' 
                        WHERE id = {source_peer_gift_id} AND owner_peer_id = {user_id};
                    """)
                else:
                    tx_sql.append(f"""
                        UPDATE stars_balances 
                        SET balance = balance - {int(src_stars)} 
                        WHERE user_id = {user_id} AND balance >= {int(src_stars)};
                    """)
                    tx_sql.append(f"""
                        INSERT INTO stars_transactions (
                            user_id, peer_type, peer_id, amount, reason, title, description, date
                        ) VALUES (
                            {user_id}, 'user', {user_id}, -{int(src_stars)}, 
                            'star_gift_upgrade', 'Upgrader', 'Апгрейд подарка в Upgrader', {now_ts}
                        );
                    """)

                if is_win and target_gift_id > 0:
                    gift_msg_id = int(now_ts % 1000000 + 1)
                    tx_sql.append(f"""
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

                tx_sql.append("NOTIFY telesrv_read_model_changed, 'star_gifts';")
                tx_sql.append("NOTIFY telesrv_read_model_changed, 'account_settings';")
                tx_sql.append("COMMIT;")

                res = run_db_query("\n".join(tx_sql))
                if res is None:
                    self.send_json_error(500, "Ошибка базы данных при проведении апгрейда")
                    return

                # Get updated balance
                new_bal_raw = run_db_query(f"SELECT balance FROM stars_balances WHERE user_id = {user_id};")
                new_balance = int(new_bal_raw) if new_bal_raw and new_bal_raw.isdigit() else 0

                resp_payload = {
                    "success": True,
                    "is_win": is_win,
                    "win_chance": round(chance, 1),
                    "stop_angle": round(stop_angle, 1),
                    "win_angle": round(win_angle, 1),
                    "new_balance": new_balance
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
    with socketserver.TCPServer(("0.0.0.0", PORT), UpgraderHandler) as httpd:
        logging.info(f"Star Gifts Upgrader running on port {PORT} with live PostgreSQL backend...")
        httpd.serve_forever()

if __name__ == "__main__":
    main()
