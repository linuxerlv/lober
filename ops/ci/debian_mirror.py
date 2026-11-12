"""只向隔离装机网络提供 Debian 的签名仓库，外部入口是本机套接字。"""
import http.server
import os
from pathlib import Path
import re
import shutil
import socket
import socketserver
import sys
import threading
import urllib.error
import urllib.request


UPSTREAMS = {'debian': 'https://deb.debian.org', 'ustc': 'https://mirrors.ustc.edu.cn'}


def upstream_url(path, upstream='debian'):
    if upstream not in UPSTREAMS:
        raise ValueError('没有批准的 Debian 仓库地址')
    # APT 会转义软件包版本中的加号，其他转义字符仍由路径检查拒绝。
    path = re.sub(r'%2[bB]', '+', path)
    if not re.fullmatch(r'/debian/(?:dists/trixie(?:-updates)?|pool)/[A-Za-z0-9._+~/\-]+', path):
        raise ValueError('不在 Debian 验收仓库范围内')
    if any(part in ('', '.', '..') for part in path[1:].split('/')):
        raise ValueError('仓库路径不合法')
    return UPSTREAMS[upstream] + path


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class Mirror(http.server.BaseHTTPRequestHandler):
    def do_HEAD(self):
        self.serve_file()

    def do_GET(self):
        self.serve_file()

    def serve_file(self):
        try:
            url = upstream_url(self.path, self.server.upstream)
        except ValueError:
            self.send_error(403)
            return
        headers = {}
        if 'Range' in self.headers:
            value = self.headers['Range']
            if not re.fullmatch(r'bytes=[0-9]+-[0-9]*', value):
                self.send_error(400)
                return
            headers['Range'] = value
        request = urllib.request.Request(url, headers=headers, method=self.command)
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        try:
            remote = opener.open(request, timeout=30)
        except urllib.error.HTTPError as error:
            self.send_error(error.code if error.code in (404, 416) else 502)
            return
        except OSError:
            self.send_error(502)
            return
        with remote:
            self.send_response(remote.status)
            for key in ('Content-Type', 'Content-Length', 'Content-Range', 'Last-Modified'):
                if key in remote.headers:
                    self.send_header(key, remote.headers[key])
            self.end_headers()
            if self.command == 'GET':
                shutil.copyfileobj(remote, self.wfile, length=256 * 1024)

    def address_string(self):
        return 'isolated-install'


class Server(socketserver.ThreadingMixIn, socketserver.TCPServer):
    daemon_threads = True

    def __init__(self, path, upstream):
        self.address_family = socket.AF_UNIX
        self.slots = threading.BoundedSemaphore(12)
        if upstream not in UPSTREAMS:
            raise ValueError('没有批准的 Debian 仓库地址')
        self.upstream = upstream
        super().__init__(path, Mirror)

    def verify_request(self, request, client_address):
        request.settimeout(30)
        return self.slots.acquire(blocking=False)

    def process_request_thread(self, request, client_address):
        try:
            super().process_request_thread(request, client_address)
        finally:
            self.slots.release()


if __name__ == '__main__':
    os.umask(0o077)
    path = Path(sys.argv[1])
    upstream = sys.argv[2] if len(sys.argv) > 2 else 'debian'
    with Server(str(path), upstream) as server:
        path.chmod(0o660)
        server.serve_forever()
