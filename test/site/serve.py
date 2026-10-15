#!/usr/bin/env python3
"""Serves a folder the way GitHub Pages serves skilus.dev's install scripts.

GitHub Pages sends files it has no media type for, such as install and
install.ps1, as application/octet-stream. CI runs the install scripts through
this server so `curl … | sh` and `irm … | iex` are tested with that header.

Usage: test/site/serve.py <folder> <port>
"""
import functools
import http.server
import sys


class Handler(http.server.SimpleHTTPRequestHandler):
    def guess_type(self, path):
        if str(path).endswith('.html'):
            return 'text/html'
        if str(path).endswith('.sh'):
            return 'application/x-sh'
        return 'application/octet-stream'


if __name__ == '__main__':
    folder, port = sys.argv[1], int(sys.argv[2])
    handler = functools.partial(Handler, directory=folder)
    http.server.ThreadingHTTPServer(('127.0.0.1', port), handler).serve_forever()
