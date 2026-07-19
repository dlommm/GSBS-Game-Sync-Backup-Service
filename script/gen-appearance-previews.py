#!/usr/bin/env python3
"""Regenerate the Settings > Appearance preview screenshots.

Boots a throwaway server with demo data (invented game titles, generated
monogram tiles — deliberately NO real cover art, so nothing third-party
ships in the binary), then screenshots the dashboard once per color scheme
and once per layout with headless Chrome. Output:
server/webui/static/previews/{design-<key>,layout-<key>}.jpg (560px JPEG).

Run from the repo root on a machine with Chrome + sqlite3 + sips (macOS):
    python3 script/gen-appearance-previews.py
"""

import json
import pathlib
import re
import subprocess
import sys
import tempfile
import time
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parent.parent
OUT = ROOT / "server" / "webui" / "static" / "previews"
PORT = 18093
BASE = f"http://127.0.0.1:{PORT}"
CHROME = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
DESIGNS = ["default", "hud", "crt", "hearth", "synth", "slate"]
CSS_LAYOUTS = ["topnav", "dense", "library"]  # pure-CSS; widgets needs the stored pref
GAMES = [
    ("1001", "Starfall Odyssey"), ("1002", "Pixel Harvest"), ("1003", "Ironclad Tactics"),
    ("1004", "Neon Drift"), ("1005", "Emberkeep"), ("1006", "Tidebound"),
]


def sh(*args, **kw):
    return subprocess.run(args, check=True, capture_output=True, text=True, **kw)


def curl(path, jar, post=None):
    args = ["curl", "-s", "-b", str(jar), "-c", str(jar)]
    if post is not None:
        for k, v in post.items():
            args += ["--data-urlencode", f"{k}={v}"]
    args.append(BASE + path)
    return subprocess.run(args, capture_output=True, text=True).stdout


def splice(doc, cid, content):
    i = doc.find(f'id="{cid}"')
    if i < 0:
        return doc
    start = doc.find(">", i) + 1
    depth, j = 1, start
    tag_re = re.compile(r"<(/?)(section|div|form|ul)\b")
    while depth > 0:
        m = tag_re.search(doc, j)
        if not m:
            return doc
        depth += -1 if m.group(1) else 1
        if depth == 0:
            return doc[:start] + content + doc[m.start():]
        j = m.end()
    return doc


def dump_dashboard(jar, dest):
    page = curl("/dashboard", jar)
    for cid, path in [
        ("dashboard-stats", "/dashboard/partial/stats"),
        ("recent-games", "/dashboard/partial/saves"),
        ("activity-feed", "/dashboard/partial/activity"),
        ("clients-table", "/dashboard/partial/clients"),
    ]:
        page = splice(page, cid, curl(path, jar))
    page = page.replace('hx-trigger="load', 'hx-trigger="never-load')
    page = page.replace('<div id="gsbs-tour"></div>', "")
    page = page.replace("<head>", f'<head><base href="{BASE}/">', 1)
    dest.write_text(page)


def shoot(html, query, dest_png):
    sh(CHROME, "--headless", "--disable-gpu", "--window-size=1500,980",
       "--hide-scrollbars", f"--screenshot={dest_png}", "--virtual-time-budget=7000",
       f"file://{html}{query}")


def to_jpeg(png, jpg):
    sh("sips", "-Z", "560", "-s", "format", "jpeg", "-s", "formatOptions", "85",
       str(png), "--out", str(jpg))


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    # The embed pattern requires the jpgs to exist before the server builds
    # (chicken-and-egg on a fresh checkout): seed minimal placeholders.
    placeholder = bytes.fromhex(
        "ffd8ffe000104a46494600010100000100010000ffdb004300ffffffffffffffffffffffff"
        "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
        "ffffffffffffffffffffffffffc00011080001000103012200021101031101ffc4001f0000"
        "010501010101010100000000000000000102030405060708090a0bffc400b5100002010303"
        "020403050504040000017d01020300041105122131410613516107227114328191a1082342"
        "b1c11552d1f02433627282090a161718191a25262728292a3435363738393a434445464748"
        "494a535455565758595a636465666768696a737475767778797a838485868788898a929394"
        "95969798999aa2a3a4a5a6a7a8a9aab2b3b4b5b6b7b8b9bac2c3c4c5c6c7c8c9cad2d3d4d5"
        "d6d7d8d9dae1e2e3e4e5e6e7e8e9eaf1f2f3f4f5f6f7f8f9faffda0008010100003f00bf9f"
        "ffd9")
    for d in DESIGNS:
        p = OUT / f"design-{d}.jpg"
        if not p.exists():
            p.write_bytes(placeholder)
    for l in ["sidebar"] + CSS_LAYOUTS + ["widgets"]:
        p = OUT / f"layout-{l}.jpg"
        if not p.exists():
            p.write_bytes(placeholder)
    tmp = pathlib.Path(tempfile.mkdtemp(prefix="gsbs-previews-"))
    sh("go", "build", "-o", str(tmp / "gsbs-server"), "./server", cwd=str(ROOT))
    srv = subprocess.Popen(
        [str(tmp / "gsbs-server")],
        env={"PATH": "/usr/bin:/bin", "GSBS_DB": str(tmp / "gsbs.db"),
             "GSBS_SAVE_ROOT": str(tmp / "saves"), "GSBS_ADDR": f"127.0.0.1:{PORT}",
             "HOME": str(tmp)},
        stdout=(tmp / "server.log").open("w"), stderr=subprocess.STDOUT,
    )
    try:
        for _ in range(60):
            try:
                urllib.request.urlopen(BASE + "/setup", timeout=1)
                break
            except Exception:
                time.sleep(0.3)
        jar = tmp / "jar.txt"
        setup = curl("/setup", jar)
        m = re.search(r'name="csrf" value="([^"]+)"', setup)
        curl("/setup", jar, post={
            "csrf": m.group(1), "username": "preview",
            "password": "preview-pass-123", "confirm_password": "preview-pass-123",
        })

        # Demo data: titles via the manifest, saves via the API, two devices.
        rows = ",".join(
            f"('m{gid}','{gid}',{gid},'{title}','windows','%APPDATA%/{gid}',0,datetime('now'),'pcgw')"
            for gid, title in GAMES)
        sh("sqlite3", str(tmp / "gsbs.db"),
           "INSERT INTO game_save_locations (id, game_id, pcgw_page_id, game_title, platform, path_template, is_config, updated_at, source) VALUES " + rows)
        for name, osname in [("Gaming-PC", "windows"), ("Steam-Deck", "linux")]:
            req = urllib.request.Request(BASE + "/api/login", method="POST",
                data=json.dumps({"username": "preview", "password": "preview-pass-123",
                                 "client_name": name, "client_os": osname}).encode(),
                headers={"Content-Type": "application/json"})
            token = json.load(urllib.request.urlopen(req))["token"]
        import hashlib
        for i, (gid, _) in enumerate(GAMES):
            body = (f"demo-save-{gid}-" + "x" * (2000 * (i + 1))).encode()
            req = urllib.request.Request(BASE + "/api/saves", method="POST", data=body, headers={
                "Authorization": "Bearer " + token, "X-Game-ID": gid, "X-Path-Key": "slot-" + gid,
                "X-Relative-Path": "saves/slot1.sav",
                "X-Content-Hash": hashlib.sha256(body).hexdigest()})
            urllib.request.urlopen(req)

        html = tmp / "dash.html"
        dump_dashboard(jar, html)
        for d in DESIGNS:
            shoot(html, f"?design={d}", tmp / f"design-{d}.png")
            to_jpeg(tmp / f"design-{d}.png", OUT / f"design-{d}.jpg")
        shoot(html, "", tmp / "layout-sidebar.png")
        to_jpeg(tmp / "layout-sidebar.png", OUT / "layout-sidebar.jpg")
        for l in CSS_LAYOUTS:
            shoot(html, f"?layout={l}", tmp / f"layout-{l}.png")
            to_jpeg(tmp / f"layout-{l}.png", OUT / f"layout-{l}.jpg")

        # widgets renders server-side from the stored pref — set, re-dump, shoot.
        uid = sh("sqlite3", str(tmp / "gsbs.db"), "SELECT id FROM users LIMIT 1").stdout.strip()
        sh("sqlite3", str(tmp / "gsbs.db"),
           f"INSERT OR REPLACE INTO user_prefs (user_id,key,value,updated_at) VALUES "
           f"('{uid}','appearance.layout','widgets',datetime('now')),"
           f"('{uid}','dashboard.widgets','{{\"order\":[\"games\",\"stats\",\"devices\",\"pulse\",\"activity\"],\"hidden\":[]}}',datetime('now'))")
        wh = tmp / "dash-widgets.html"
        dump_dashboard(jar, wh)
        shoot(wh, "", tmp / "layout-widgets.png")
        to_jpeg(tmp / "layout-widgets.png", OUT / "layout-widgets.jpg")

        sizes = {p.name: p.stat().st_size for p in sorted(OUT.glob("*.jpg"))}
        print(json.dumps(sizes, indent=1))
    finally:
        srv.terminate()


if __name__ == "__main__":
    main()
