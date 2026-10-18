"""Bounded network fixtures, affecting only the dedicated candidate instance."""
import contextlib
import http.server
import select
import socket
import threading


class DatabaseProxy:
    def __init__(self, host, port):
        if not host or host.startswith('/'):
            raise RuntimeError('Fault fixture requires a TCP database endpoint')
        self.target = (host, port)
        self.listener = socket.socket()
        self.listener.bind(('127.0.0.1', 0))
        self.listener.listen()
        self.listener.settimeout(.2)
        self.port = self.listener.getsockname()[1]
        self.blocked = False
        self.closed = False
        self.connections = set()
        self.lock = threading.Lock()
        self.accepted = 0
        self.dropped = 0
        self.thread = threading.Thread(target=self.accept, daemon=True)
        self.thread.start()

    def accept(self):
        while not self.closed:
            try:
                client, _ = self.listener.accept()
            except socket.timeout:
                continue
            except OSError:
                break
            with self.lock:
                if self.blocked:
                    client.close()
                    continue
                self.accepted += 1
                self.connections.add(client)
            threading.Thread(target=self.forward, args=(client,), daemon=True).start()

    def forward(self, client):
        upstream = None
        try:
            upstream = socket.create_connection(self.target, timeout=3)
            with self.lock:
                self.connections.add(upstream)
                if self.blocked:
                    return
            peers = {client: upstream, upstream: client}
            while not self.closed and not self.blocked:
                readable, _, _ = select.select(list(peers), [], [], .2)
                for source in readable:
                    data = source.recv(65536)
                    if not data:
                        return
                    peers[source].sendall(data)
        except OSError:
            pass
        finally:
            with self.lock:
                for connection in (client, upstream):
                    if connection is not None:
                        self.connections.discard(connection)
                        connection.close()

    def disconnect(self):
        with self.lock:
            self.blocked = True
            self.dropped = len(self.connections)
            for connection in list(self.connections):
                with contextlib.suppress(OSError):
                    connection.shutdown(socket.SHUT_RDWR)
                connection.close()

    def close(self):
        self.closed = True
        self.disconnect()
        self.listener.close()
        self.thread.join(timeout=2)


class MediaServer:
    def __init__(self, payload, bind_ip):
        self.payload = payload
        self.mode = 'cut'
        self.requests = []
        self.cut = threading.Event()
        self.release = threading.Event()
        owner = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_GET(self):
                offset = int(self.headers.get('Range', 'bytes=0-').split('=')[1].split('-')[0])
                owner.requests.append(offset)
                self.send_response(206 if offset else 200)
                self.send_header('Content-Length', str(len(payload) - offset))
                if offset:
                    self.send_header('Content-Range', f'bytes {offset}-{len(payload)-1}/{len(payload)}')
                self.end_headers()
                try:
                    if owner.mode in ('cut', 'hold'):
                        self.wfile.write(payload[offset:offset + 32768])
                        self.wfile.flush()
                        owner.cut.set()
                        if owner.mode == 'hold':
                            owner.release.wait(timeout=30)
                        self.connection.shutdown(socket.SHUT_RDWR)
                        return
                    self.wfile.write(payload[offset:])
                except (OSError, ConnectionError):
                    pass

        self.server = http.server.ThreadingHTTPServer((bind_ip, 0), Handler)
        self.server.daemon_threads = True
        self.url = 'http://' + bind_ip + ':' + str(self.server.server_address[1]) + '/synthetic.iso'
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()

    def close(self):
        self.release.set()
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=2)
