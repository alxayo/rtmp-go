#!/usr/bin/env python3
"""CORS-enabled HTTP server for serving HLS streams."""
import http.server
import os
import sys

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 8080
ROOT = os.path.dirname(os.path.abspath(__file__))


class CORSHandler(http.server.SimpleHTTPRequestHandler):
    def end_headers(self):
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Range")
        # Prevent caching of .m3u8 playlists; allow .ts segments to cache briefly
        if self.path.endswith(".m3u8"):
            self.send_header("Cache-Control", "no-store, no-cache, must-revalidate")
        else:
            self.send_header("Cache-Control", "max-age=2")
        super().end_headers()

    def do_OPTIONS(self):
        self.send_response(200)
        self.end_headers()

    def log_message(self, fmt, *args):
        print(f"[HTTP] {self.address_string()} - {fmt % args}")


os.chdir(ROOT)
server = http.server.HTTPServer(("0.0.0.0", PORT), CORSHandler)
print(f"[HLS server] Serving {ROOT} on http://0.0.0.0:{PORT}")
print(f"[HLS server] Player  -> http://0.0.0.0:{PORT}/index.html")
print(f"[HLS server] Playlist-> http://0.0.0.0:{PORT}/hls/stream.m3u8")
server.serve_forever()
