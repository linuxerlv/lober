"""只用小型协议字节验证抓包证明；不连接主机或网络。"""
import copy
import hashlib
import importlib
from pathlib import Path
import socket
import struct
import tempfile
import unittest
from unittest.mock import patch

try:
    capture = importlib.import_module('cloud_packet_capture')
except ModuleNotFoundError:
    capture = None

CLIENT = '52:54:00:25:10:01'
BOOT = '52:54:00:25:10:fe'
SERVICE = '52:54:00:25:10:10'
CLIENT_IP, BOOT_IP, SERVICE_IP = '192.168.201.101', '192.168.201.1', '192.168.201.10'
TARGET = '/v1/signal/task-a/PRIVATE-TOKEN/ready'
BOOT_FILE = b'#!ipxe\nok\n'


def digest(data):
    return hashlib.sha256(data).hexdigest()


def mac(value):
    return bytes.fromhex(value.replace(':', ''))


def ethernet(source, destination, protocol, payload):
    return mac(destination) + mac(source) + struct.pack('!H', protocol) + payload


def ipv4(source, destination, protocol, payload, fragment=0):
    return struct.pack('!BBHHHBBH4s4s', 0x45, 0, 20 + len(payload), 1, fragment, 64, protocol,
                       0, socket.inet_aton(source), socket.inet_aton(destination)) + payload


def udp(source, destination, sport, dport, payload, smac=CLIENT, dmac=BOOT):
    body = struct.pack('!HHHH', sport, dport, len(payload) + 8, 0) + payload
    return ethernet(smac, dmac, 0x0800, ipv4(source, destination, 17, body))


def tcp(source, destination, sport, dport, seq, ack, flags, payload=b'', smac=CLIENT, dmac=SERVICE):
    body = struct.pack('!HHIIBBHHH', sport, dport, seq & 0xffffffff, ack & 0xffffffff,
                       0x50, flags, 8192, 0, 0) + payload
    return ethernet(smac, dmac, 0x0800, ipv4(source, destination, 6, body))


def dhcp(kind, xid=7, address=CLIENT_IP, chaddr=CLIENT, server=BOOT_IP):
    client = kind in (1, 3)
    options = b'\x35\x01' + bytes([kind])
    if kind != 1:
        options += b'\x36\x04' + socket.inet_aton(server)
    if kind == 3:
        options += b'\x32\x04' + socket.inet_aton(address)
    if kind == 5:
        options += b'\x33\x04' + struct.pack('!I', 3600)
    boot = bytearray(236)
    struct.pack_into('!BBBBI', boot, 0, 1 if client else 2, 1, 6, 0, xid)
    boot[16:20] = socket.inet_aton('0.0.0.0' if client else address)
    boot[20:24] = socket.inet_aton(server)
    boot[28:34] = mac(chaddr)
    if not client:
        boot[108:118] = b'pq25.ipxe\0'
    payload = bytes(boot) + b'\x63\x82\x53\x63' + options + b'\xff'
    return udp('0.0.0.0' if client else BOOT_IP, '255.255.255.255',
               68 if client else 67, 67 if client else 68, payload,
               CLIENT if client else BOOT, 'ff:ff:ff:ff:ff:ff')


def request(target=TARGET, body=b'yes'):
    return b'POST ' + target.encode() + b' HTTP/1.1\r\nHost: lober\r\nContent-Length: ' + str(len(body)).encode() + b'\r\n\r\n' + body


def response(status=200, body=b'OK', chunked=False, close=False):
    headers = b'HTTP/1.1 ' + str(status).encode() + b' Result\r\n'
    if chunked:
        return headers + b'Transfer-Encoding: chunked\r\n\r\n' + format(len(body), 'x').encode() + b'\r\n' + body + b'\r\n0\r\n\r\n'
    if close:
        return headers + b'Connection: close\r\n\r\n' + body
    return headers + b'Content-Length: ' + str(len(body)).encode() + b'\r\n\r\n' + body


def http_frames(req=None, res=None, client_seq=100, server_seq=500, port=42000, fin=False):
    req, res = request() if req is None else req, response() if res is None else res
    frames = [
        ('syn', tcp(CLIENT_IP, SERVICE_IP, port, 18090, client_seq, 0, 2)),
        ('synack', tcp(SERVICE_IP, CLIENT_IP, 18090, port, server_seq, client_seq + 1, 0x12, smac=SERVICE, dmac=CLIENT)),
        ('ack', tcp(CLIENT_IP, SERVICE_IP, port, 18090, client_seq + 1, server_seq + 1, 0x10)),
    ]
    cut = len(req) // 2
    # 数据按反序到达，重组必须按TCP序号处理。
    for name, offset, body in [('request2', cut, req[cut:]), ('request1', 0, req[:cut])]:
        frames.append((name, tcp(CLIENT_IP, SERVICE_IP, port, 18090, client_seq + 1 + offset, server_seq + 1, 0x18, body)))
    cut = len(res) // 2
    for name, offset, body in [('response2', cut, res[cut:]), ('response1', 0, res[:cut])]:
        frames.append((name, tcp(SERVICE_IP, CLIENT_IP, 18090, port, server_seq + 1 + offset,
                                   client_seq + 1 + len(req), 0x18, body, SERVICE, CLIENT)))
    if fin:
        frames.append(('fin', tcp(SERVICE_IP, CLIENT_IP, 18090, port, server_seq + 1 + len(res),
                                  client_seq + 1 + len(req), 0x11, smac=SERVICE, dmac=CLIENT)))
    return frames


def frames(http=None):
    result = [(name, dhcp(kind)) for name, kind in [('discover', 1), ('offer', 2), ('request', 3), ('lease', 5)]]
    result += [
        ('rrq', udp(CLIENT_IP, BOOT_IP, 41000, 69, b'\0\1pq25.ipxe\0octet\0blksize\08\0')),
        ('oack', udp(BOOT_IP, CLIENT_IP, 40001, 41000, b'\0\6blksize\08\0', BOOT, CLIENT)),
        ('ack0', udp(CLIENT_IP, BOOT_IP, 41000, 40001, b'\0\4\0\0')),
        ('data1', udp(BOOT_IP, CLIENT_IP, 40001, 41000, b'\0\3\0\1' + BOOT_FILE[:8], BOOT, CLIENT)),
        ('ack1', udp(CLIENT_IP, BOOT_IP, 41000, 40001, b'\0\4\0\1')),
        ('data2', udp(BOOT_IP, CLIENT_IP, 40001, 41000, b'\0\3\0\2' + BOOT_FILE[8:], BOOT, CLIENT)),
        ('ack2', udp(CLIENT_IP, BOOT_IP, 41000, 40001, b'\0\4\0\2')),
    ]
    return result + (http_frames() if http is None else http)


def pcap(items, endian='<', nano=False):
    magic = 0xa1b23c4d if nano else 0xa1b2c3d4
    result = struct.pack(endian + 'IHHIIII', magic, 2, 4, 0, 0, 262144, 1)
    for index, (_, body) in enumerate(items):
        fraction = index * (1_000_000 if nano else 1000)
        result += struct.pack(endian + 'IIII', 1, fraction, len(body), len(body)) + body
    return result


def contract(data):
    return {'schema': 'lober-cloud-packet-v1', 'pcap_sha256': digest(data), 'pcap_size': len(data),
            'capture_start_ns': 1_000_000_000, 'capture_end_ns': 2_000_000_000,
            'client_mac': CLIENT, 'client_ip': CLIENT_IP, 'boot_server_mac': BOOT, 'boot_server_ip': BOOT_IP,
            'service_mac': SERVICE, 'service_ip': SERVICE_IP, 'service_port': 18090, 'task_id': 'task-a',
            'tftp': {'pq25.ipxe': {'size': len(BOOT_FILE), 'sha256': digest(BOOT_FILE)}},
            'http': [{'name': 'ready', 'method': 'POST', 'target_sha256': digest(TARGET.encode()),
                      'status': 200, 'request_body_sha256': digest(b'yes'), 'response_body_sha256': digest(b'OK')}]}


class PacketChecks(unittest.TestCase):
    def parse(self, items=None, data=None, change=None):
        self.assertIsNotNone(capture, '缺少真实抓包解析模块')
        data = pcap(frames() if items is None else items) if data is None else data
        frozen = contract(data)
        if change:
            change(frozen)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'private.pcap'
            path.write_bytes(data)
            return capture.analyze_capture(path, frozen)

    def reject(self, items=None, data=None, change=None):
        self.assertIsNotNone(capture, '缺少真实抓包解析模块')
        with self.assertRaises(ValueError):
            self.parse(items, data, change)

    def parse_supported(self, items, change=None):
        try:
            return self.parse(items=items, change=change)
        except ValueError as error:
            self.fail('合法包序列被拒绝：' + str(error))

    def test_complete_bound_protocols_and_default_redaction(self):
        value = self.parse()
        self.assertEqual(value['scope'], 'PACKET_EVIDENCE_ONLY')
        self.assertEqual(value['dhcp']['client_ip'], CLIENT_IP)
        self.assertEqual(value['tftp'][0]['sha256'], digest(BOOT_FILE))
        self.assertEqual(value['http'][0]['response_status'], 200)
        self.assertNotIn('PRIVATE-TOKEN', str(value))
        self.assertNotIn(TARGET, str(value))

    def test_classic_pcap_endian_and_timestamp_precisions(self):
        for endian in '<>':
            for nano in (False, True):
                with self.subTest(endian=endian, nano=nano):
                    self.assertEqual(self.parse(data=pcap(frames(), endian, nano))['http'][0]['name'], 'ready')

    def test_capture_digest_and_window_are_frozen(self):
        for change in [lambda c: c.update(pcap_sha256='0' * 64), lambda c: c.update(pcap_size=1),
                       lambda c: c.update(capture_start_ns=1_100_000_000)]:
            with self.subTest(change=change):
                self.reject(change=change)

    def test_truncated_global_record_and_packet_headers_rejected(self):
        data = pcap(frames())
        for body in [data[:10], data[:30], data[:-1], data + b'x']:
            with self.subTest(size=len(body)):
                self.reject(data=body)

    def test_snaplen_and_original_packet_length_must_be_complete(self):
        data = bytearray(pcap(frames()))
        struct.pack_into('<I', data, 24 + 12, 9999)
        self.reject(data=bytes(data))

    def test_only_supported_link_type_and_version(self):
        for offset, fmt, number in [(20, '<I', 101), (4, '<H', 3)]:
            body = bytearray(pcap(frames())); struct.pack_into(fmt, body, offset, number)
            self.reject(data=bytes(body))

    def test_missing_or_cross_transaction_dhcp_not_lease_proof(self):
        self.reject(items=[v for v in frames() if v[0] != 'lease'])
        self.reject(items=[(n, dhcp(5, xid=99) if n == 'lease' else b) for n, b in frames()])

    def test_dhcp_wrong_mac_lease_server_or_requested_address_rejected(self):
        for bad in [dhcp(5, chaddr=SERVICE), dhcp(5, address='192.168.201.102'),
                    dhcp(5, server=SERVICE_IP), dhcp(3, address='192.168.201.102')]:
            name = 'request' if bad[14 + 20 + 8] == 1 else 'lease'
            self.reject(items=[(n, bad if n == name else b) for n, b in frames()])

    def test_dhcp_packet_routing_and_next_server_are_bound(self):
        original = dict(frames())['lease']
        for offset, body in [(0, mac(BOOT)), (14 + 16, socket.inet_aton('192.168.201.102')),
                             (14 + 20 + 8 + 20, socket.inet_aton(SERVICE_IP))]:
            bad = bytearray(original); bad[offset:offset + len(body)] = body
            with self.subTest(offset=offset):
                self.reject(items=[(n, bytes(bad) if n == 'lease' else b) for n, b in frames()])

    def test_missing_tftp_block_final_ack_or_rrq_rejected(self):
        for name in ['rrq', 'data1', 'data2', 'ack2']:
            self.reject(items=[v for v in frames() if v[0] != name])

    def test_tftp_each_negotiated_window_requires_its_ack(self):
        self.reject(items=[v for v in frames() if v[0] != 'ack1'])

    def test_tftp_wrong_file_tid_content_and_expected_hash_rejected(self):
        for name, bad in [
            ('rrq', udp(CLIENT_IP, BOOT_IP, 41000, 69, b'\0\1other.ipxe\0octet\0')),
            ('data2', udp(BOOT_IP, CLIENT_IP, 40002, 41000, b'\0\3\0\2' + BOOT_FILE[8:], BOOT, CLIENT)),
            ('data2', udp(BOOT_IP, CLIENT_IP, 40001, 41000, b'\0\3\0\2XX', BOOT, CLIENT))]:
            self.reject(items=[(n, bad if n == name else b) for n, b in frames()])
        self.reject(change=lambda c: c['tftp']['pq25.ipxe'].update(sha256='0' * 64))

    def test_tftp_duplicate_identical_data_allowed_conflict_rejected(self):
        items = frames(); index = next(i for i, v in enumerate(items) if v[0] == 'data1')
        items.insert(index + 1, items[index])
        self.assertEqual(self.parse(items)['tftp'][0]['bytes'], len(BOOT_FILE))
        items[index + 1] = ('conflict', udp(BOOT_IP, CLIENT_IP, 40001, 41000, b'\0\3\0\1XXXXXXXX', BOOT, CLIENT))
        self.reject(items=items)

    def test_tftp_without_oack_uses_default_block_size(self):
        items = [(n, b) for n, b in frames(http=[]) if n in ('discover', 'offer', 'request', 'lease')]
        items += [('rrq', udp(CLIENT_IP, BOOT_IP, 41000, 69, b'\0\1pq25.ipxe\0octet\0')),
                  ('data1', udp(BOOT_IP, CLIENT_IP, 40001, 41000, b'\0\3\0\1' + BOOT_FILE, BOOT, CLIENT)),
                  ('ack1', udp(CLIENT_IP, BOOT_IP, 41000, 40001, b'\0\4\0\1'))]
        self.assertEqual(self.parse(items + http_frames())['tftp'][0]['bytes'], len(BOOT_FILE))

    def test_tftp_exact_multiple_needs_zero_terminal_block(self):
        contents = b'12345678abcdefgh'
        items = [(n, b) for n, b in frames(http=[]) if n in ('discover', 'offer', 'request', 'lease', 'rrq', 'oack', 'ack0')]
        for number, body in [(1, contents[:8]), (2, contents[8:]), (3, b'')]:
            items += [('block', udp(BOOT_IP, CLIENT_IP, 40001, 41000, struct.pack('!HH', 3, number) + body, BOOT, CLIENT)),
                      ('ack', udp(CLIENT_IP, BOOT_IP, 41000, 40001, struct.pack('!HH', 4, number)))]
        change = lambda c: c['tftp']['pq25.ipxe'].update(size=len(contents), sha256=digest(contents))
        self.assertEqual(self.parse(items + http_frames(), change=change)['tftp'][0]['blocks'], 3)
        self.reject(items=items[:-2] + http_frames(), change=change)

    def test_tftp_negotiation_cannot_change_after_the_ack(self):
        contents = b'12345678abcdefghij'
        items = [(n, b) for n, b in frames(http=[]) if n in ('discover', 'offer', 'request', 'lease')]
        items += [('rrq', udp(CLIENT_IP, BOOT_IP, 41000, 69, b'\0\1pq25.ipxe\0octet\0blksize\x0016\0')),
                  ('oack', udp(BOOT_IP, CLIENT_IP, 40001, 41000, b'\0\6blksize\08\0', BOOT, CLIENT)),
                  ('ack0', udp(CLIENT_IP, BOOT_IP, 41000, 40001, b'\0\4\0\0')),
                  ('changed-oack', udp(BOOT_IP, CLIENT_IP, 40001, 41000, b'\0\6blksize\x0016\0', BOOT, CLIENT))]
        for number, body in [(1, contents[:16]), (2, contents[16:])]:
            items += [('data', udp(BOOT_IP, CLIENT_IP, 40001, 41000, struct.pack('!HH', 3, number) + body, BOOT, CLIENT)),
                      ('ack', udp(CLIENT_IP, BOOT_IP, 41000, 40001, struct.pack('!HH', 4, number)))]
        self.reject(items=items + http_frames(), change=lambda c: c['tftp']['pq25.ipxe'].update(size=len(contents), sha256=digest(contents)))

    def test_every_required_tftp_file_must_be_transferred(self):
        more = [('rrq-other', udp(CLIENT_IP, BOOT_IP, 41001, 69, b'\0\1undionly.kpxe\0octet\0')),
                ('data-other', udp(BOOT_IP, CLIENT_IP, 40002, 41001, b'\0\3\0\1NBP', BOOT, CLIENT)),
                ('ack-other', udp(CLIENT_IP, BOOT_IP, 41001, 40002, b'\0\4\0\1'))]
        change = lambda c: c['tftp'].update({'undionly.kpxe': {'size': 3, 'sha256': digest(b'NBP')}})
        self.assertEqual(len(self.parse(frames(http=[]) + more + http_frames(), change=change)['tftp']), 2)
        self.reject(change=change)

    def test_tftp_negotiated_size_and_windowed_acknowledgments(self):
        contents = b'12345678abcdefghij'
        options = b'blksize\08\0tsize\x000\0windowsize\x002\0'
        actual = b'blksize\08\0tsize\x0018\0windowsize\x002\0'
        items = [(n, b) for n, b in frames(http=[]) if n in ('discover', 'offer', 'request', 'lease')]
        items += [('rrq', udp(CLIENT_IP, BOOT_IP, 41000, 69, b'\0\1pq25.ipxe\0octet\0' + options)),
                  ('oack', udp(BOOT_IP, CLIENT_IP, 40001, 41000, b'\0\6' + actual, BOOT, CLIENT)),
                  ('ack0', udp(CLIENT_IP, BOOT_IP, 41000, 40001, b'\0\4\0\0'))]
        for number, body in [(1, contents[:8]), (2, contents[8:16]), (3, contents[16:])]:
            items.append(('data', udp(BOOT_IP, CLIENT_IP, 40001, 41000, struct.pack('!HH', 3, number) + body, BOOT, CLIENT)))
            if number in (2, 3):
                items.append(('ack', udp(CLIENT_IP, BOOT_IP, 41000, 40001, struct.pack('!HH', 4, number))))
        change = lambda c: c['tftp']['pq25.ipxe'].update(size=len(contents), sha256=digest(contents))
        value = self.parse(items + http_frames(), change=change)
        self.assertEqual(value['tftp'][0]['negotiated_options']['windowsize'], 2)

    def test_tcp_split_out_of_order_and_exact_retransmission(self):
        items = frames(); index = next(i for i, v in enumerate(items) if v[0] == 'request1')
        items.insert(index + 1, items[index])
        self.assertEqual(self.parse(items)['http'][0]['request_body_sha256'], digest(b'yes'))

    def test_tcp_sequence_wraparound(self):
        self.assertEqual(self.parse(frames(http_frames(client_seq=0xfffffff0, server_seq=0xfffffff8)))['http'][0]['name'], 'ready')

    def test_rst_diagnostic_payload_is_not_a_successful_response(self):
        items = [v for v in http_frames() if not v[0].startswith('response')]
        items.append(('reset', tcp(SERVICE_IP, CLIENT_IP, 18090, 42000, 501,
                                   101 + len(request()), 0x14, response(), SERVICE, CLIENT)))
        self.reject(items=frames(items))

    def test_valid_rst_stops_unhandshaken_residual_bytes(self):
        reset = ('reset', tcp(SERVICE_IP, CLIENT_IP, 18090, 42000, 501, 101, 0x14, smac=SERVICE, dmac=CLIENT))
        items = http_frames()
        self.reject(items=frames(items[:3] + [reset] + items[3:]))

    def test_rst_after_complete_success_preserves_the_exchange(self):
        reset = ('reset', tcp(SERVICE_IP, CLIENT_IP, 18090, 42000, 501 + len(response()),
                               101 + len(request()), 0x14, b'diagnostic', SERVICE, CLIENT))
        self.assertEqual(self.parse_supported(frames(http_frames() + [reset]))['http'][0]['response_body_sha256'], digest(b'OK'))

    def test_invalid_rst_cannot_terminate_or_add_http_bytes(self):
        for seq in (500, 502, 999999):
            with self.subTest(seq=seq):
                reset = ('invalid-reset', tcp(SERVICE_IP, CLIENT_IP, 18090, 42000, seq, 101,
                                              0x14, b'not HTTP bytes', SERVICE, CLIENT))
                items = http_frames()
                self.assertEqual(self.parse_supported(frames(items[:3] + [reset] + items[3:]))['http'][0]['name'], 'ready')

    def test_new_handshake_after_rst_is_an_independent_flow(self):
        # 旧连接的未获响应请求不能借用新连接；相同初始序号也需独立身份。
        old = [v for v in http_frames() if not v[0].startswith('response')]
        reset = ('reset', tcp(SERVICE_IP, CLIENT_IP, 18090, 42000, 501,
                               101 + len(request()), 0x14, smac=SERVICE, dmac=CLIENT))
        value = self.parse_supported(frames(old + [reset] + http_frames()))
        baseline = self.parse()['http'][0]['reference']['flow_id']
        self.assertNotEqual(value['http'][0]['reference']['flow_id'], baseline)

    def test_client_rst_also_stops_server_residual_response(self):
        items = http_frames()
        before = [v for v in items if not v[0].startswith('response')]
        reset = ('client-reset', tcp(CLIENT_IP, SERVICE_IP, 42000, 18090,
                                     101 + len(request()), 501, 0x14, b'diagnostic'))
        self.reject(items=frames(before + [reset] + [v for v in items if v[0].startswith('response')]))

    def test_rst_is_not_fin_for_a_close_delimited_response(self):
        reply = response(close=True)
        reset = ('reset', tcp(SERVICE_IP, CLIENT_IP, 18090, 42000, 501 + len(reply),
                               101 + len(request()), 0x14, smac=SERVICE, dmac=CLIENT))
        self.reject(items=frames(http_frames(res=reply) + [reset]))

    def test_complete_success_survives_reset_of_a_later_incomplete_exchange(self):
        second = TARGET.replace('ready', 'installed')
        req = request() + request(second)
        reply = response() + response(body=b'TWO')[:-1]
        reset = ('reset', tcp(SERVICE_IP, CLIENT_IP, 18090, 42000, 501 + len(reply),
                               101 + len(req), 0x14, smac=SERVICE, dmac=CLIENT))
        self.assertEqual(self.parse_supported(frames(http_frames(req=req, res=reply) + [reset]))['http'][0]['name'], 'ready')
        def require_later(c):
            c['http'].append({**c['http'][0], 'name': 'installed', 'target_sha256': digest(second.encode()),
                              'response_body_sha256': digest(b'TWO')})
        self.reject(items=frames(http_frames(req=req, res=reply) + [reset]), change=require_later)

    def test_reset_does_not_hide_chunk_work_bounds_in_an_incomplete_tail(self):
        req = request() + request(TARGET.replace('ready', 'installed'))
        for tail in (b'0' * 1025, b'0\r\n' + b'X: ' + b'x' * 65537):
            with self.subTest(tail_bytes=len(tail)):
                reply = response() + b'HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n' + tail
                reset = ('reset', tcp(SERVICE_IP, CLIENT_IP, 18090, 42000, 501 + len(reply),
                                       101 + len(req), 0x14, smac=SERVICE, dmac=CLIENT))
                self.reject(items=frames(http_frames(req=req, res=reply) + [reset]))

    def test_late_identical_request_retransmission_keeps_all_references(self):
        items = frames()
        duplicate = next(v for v in items if v[0] == 'request1')
        items.append(duplicate)
        value = self.parse_supported(items)['http'][0]
        self.assertEqual(value['response_body_sha256'], digest(b'OK'))
        self.assertIn(len(items), value['request']['packet_indices'])
        self.assertLess(value['request']['first_complete_packet_index'], min(value['response']['packet_indices']))

    def test_keepalive_late_retransmission_of_prior_request_is_valid(self):
        first = http_frames()
        second = TARGET.replace('ready', 'installed')
        later = http_frames(req=request(second), res=response(body=b'TWO'),
                            client_seq=100 + len(request()), server_seq=500 + len(response()))[3:]
        duplicate = next(v for v in first if v[0] == 'request1')
        items = frames(first + later[:1] + [duplicate] + later[1:])
        def more(c):
            c['http'].append({**c['http'][0], 'name': 'installed', 'target_sha256': digest(second.encode()),
                              'response_body_sha256': digest(b'TWO')})
        self.assertEqual(len(self.parse_supported(items, change=more)['http']), 2)

    def test_first_complete_request_after_response_is_still_rejected(self):
        items = frames()
        missing = next(v for v in items if v[0] == 'request1')
        self.reject(items=[v for v in items if v[0] != 'request1'] + [missing])

    def test_late_conflicting_request_retransmission_is_still_rejected(self):
        items = frames()
        bad = bytearray(next(b for n, b in items if n == 'request1')); bad[-1] ^= 1
        self.reject(items=items + [('conflict', bytes(bad))])

    def window_two_fixture(self, order):
        contents = b'12345678abcdefghij'
        options = b'blksize\08\0windowsize\x002\0'
        items = frames(http=[])[:4] + [
            ('rrq', udp(CLIENT_IP, BOOT_IP, 41000, 69, b'\0\1pq25.ipxe\0octet\0' + options)),
            ('oack', udp(BOOT_IP, CLIENT_IP, 40001, 41000, b'\0\6' + options, BOOT, CLIENT)),
            ('ack0', udp(CLIENT_IP, BOOT_IP, 41000, 40001, b'\0\4\0\0'))]
        operations = {}
        for number in (1, 2, 3):
            operations['data' + str(number)] = udp(BOOT_IP, CLIENT_IP, 40001, 41000,
                struct.pack('!HH', 3, number) + contents[(number - 1) * 8:number * 8], BOOT, CLIENT)
            operations['ack' + str(number)] = udp(CLIENT_IP, BOOT_IP, 41000, 40001, struct.pack('!HH', 4, number))
        items += [(name, operations[name]) for name in order]
        change = lambda c: c['tftp']['pq25.ipxe'].update(size=len(contents), sha256=digest(contents))
        return items + http_frames(), change

    def test_tftp_window_ack_cannot_precede_first_missing_block(self):
        items, change = self.window_two_fixture(['data2', 'ack2', 'data1', 'data3', 'ack3'])
        self.reject(items=items, change=change)

    def test_tftp_window_out_of_order_complete_before_ack_is_valid(self):
        items, change = self.window_two_fixture(['data2', 'data1', 'ack2', 'data3', 'ack3'])
        self.assertEqual(self.parse(items, change=change)['tftp'][0]['bytes'], 18)

    def test_tftp_window_late_identical_retransmission_is_valid(self):
        items, change = self.window_two_fixture(['data2', 'data1', 'ack2', 'data1', 'data2', 'data3', 'ack3'])
        self.assertEqual(self.parse(items, change=change)['tftp'][0]['bytes'], 18)

    def test_tftp_window_late_conflicting_retransmission_is_rejected(self):
        items, change = self.window_two_fixture(['data2', 'data1', 'ack2', 'data1', 'data3', 'ack3'])
        positions = [i for i, (name, _) in enumerate(items) if name == 'data1']
        bad = bytearray(items[positions[-1]][1]); bad[-1] ^= 1
        items[positions[-1]] = ('conflict', bytes(bad))
        self.reject(items=items, change=change)

    def test_tcp_acknowledgment_must_belong_to_the_same_stream(self):
        for name in ('ack', 'request1', 'response1'):
            items = frames(); index = next(i for i, v in enumerate(items) if v[0] == name)
            bad = bytearray(items[index][1]); struct.pack_into('!I', bad, 14 + 20 + 8, 999999)
            items[index] = (name, bytes(bad)); self.reject(items=items)

    def test_tcp_conflicting_overlap_missing_segment_or_handshake_rejected(self):
        for name in ['request1', 'syn', 'synack']:
            self.reject(items=[v for v in frames() if v[0] != name])
        items = frames(); index = next(i for i, v in enumerate(items) if v[0] == 'request1')
        bad = bytearray(items[index][1]); bad[-1] ^= 1
        items.insert(index + 1, ('conflict', bytes(bad))); self.reject(items=items)

    def test_wrong_mac_and_fragmented_ipv4_are_not_http_proof(self):
        items = frames(); modified = []
        for name, body in items:
            if name.startswith('response'):
                body = mac(BOOT) + body[6:]
            modified.append((name, body))
        self.reject(items=modified)
        items = frames(); index = next(i for i, v in enumerate(items) if v[0] == 'request1')
        bad = bytearray(items[index][1]); struct.pack_into('!H', bad, 14 + 6, 0x2000)
        items[index] = ('fragment', bytes(bad)); self.reject(items=items)

    def test_response_from_another_connection_cannot_pair(self):
        items = []
        for name, body in frames():
            if name.startswith('response'):
                body = bytearray(body); struct.pack_into('!H', body, 14 + 20 + 2, 43000); body = bytes(body)
            items.append((name, body))
        self.reject(items=items)

    def test_status_body_request_body_and_target_must_match(self):
        self.reject(items=frames(http_frames(res=response(500))))
        self.reject(items=frames(http_frames(res=response(body=b'NO'))))
        self.reject(items=frames(http_frames(req=request(body=b'bad'))))
        self.reject(items=frames(http_frames(req=request('/v1/signal/task-b/PRIVATE-TOKEN/ready'))))

    def test_task_binding_remains_required_even_with_other_target_hash(self):
        target = '/v1/signal/task-b/PRIVATE-TOKEN/ready'
        self.reject(items=frames(http_frames(req=request(target))), change=lambda c: c['http'][0].update(target_sha256=digest(target.encode())))

    def test_only_response_containing_request_text_cannot_satisfy(self):
        body = (TARGET + '\n#!ipxe\nexit\n').encode()
        self.reject(items=frames(http_frames(req=request('/different'), res=response(body=body))),
                    change=lambda c: c['http'][0].update(response_body_sha256=digest(body)))

    def test_http_cannot_precede_required_tftp_chain(self):
        items = frames(http=[])
        self.reject(items=items[:4] + http_frames() + items[4:])

    def test_http_chunked_complete_and_incomplete_terminal_chunk(self):
        self.assertEqual(self.parse(frames(http_frames(res=response(chunked=True))))['http'][0]['response_body_sha256'], digest(b'OK'))
        self.reject(items=frames(http_frames(res=response(chunked=True)[:-1])))

    def test_chunked_callback_body_is_reassembled(self):
        req = b'POST ' + TARGET.encode() + b' HTTP/1.1\r\nHost: lober\r\nTransfer-Encoding: chunked\r\n\r\n3\r\nyes\r\n0\r\n\r\n'
        self.assertEqual(self.parse(frames(http_frames(req=req)))['http'][0]['request_body_sha256'], digest(b'yes'))

    def test_http_ambiguous_content_lengths_and_transfer_encoding_rejected(self):
        for headers in [b'Content-Length: 2\r\nContent-Length: 2',
                        b'Content-Length: 2\r\nTransfer-Encoding: chunked', b'Content-Length: 3']:
            self.reject(items=frames(http_frames(res=b'HTTP/1.1 200 OK\r\n' + headers + b'\r\n\r\nOK')))

    def test_http10_transfer_encoding_is_not_valid_framing(self):
        self.reject(items=frames(http_frames(res=response(chunked=True).replace(b'HTTP/1.1', b'HTTP/1.0'))))
        req = b'POST ' + TARGET.encode() + b' HTTP/1.0\r\nTransfer-Encoding: chunked\r\n\r\n3\r\nyes\r\n0\r\n\r\n'
        self.reject(items=frames(http_frames(req=req)))

    def test_close_delimited_response_requires_fin(self):
        self.assertEqual(self.parse(frames(http_frames(res=response(close=True), fin=True)))['http'][0]['response_body_bytes'], 2)
        self.reject(items=frames(http_frames(res=response(close=True))))

    def test_http_keepalive_responses_pair_in_order(self):
        second = TARGET.replace('ready', 'installed')
        items = frames(http_frames(req=request() + request(second), res=response(body=b'OK') + response(body=b'TWO')))
        def more(c):
            c['http'].append({**c['http'][0], 'name': 'installed', 'target_sha256': digest(second.encode()), 'response_body_sha256': digest(b'TWO')})
        self.assertEqual(len(self.parse(items, change=more)['http']), 2)
        bad = frames(http_frames(req=request() + request(second), res=response(body=b'TWO') + response(body=b'OK')))
        self.reject(items=bad, change=more)

    def test_required_exchanges_cannot_reuse_one_request(self):
        self.reject(change=lambda c: c['http'].append({**c['http'][0], 'name': 'second-proof'}))

    def test_tcp_port_reuse_is_split_by_new_syn(self):
        items = frames(http_frames(fin=True)) + http_frames(client_seq=300, server_seq=700)
        self.assertEqual(self.parse(items)['http'][0]['name'], 'ready')

    def test_resource_bounds_apply_to_total_messages(self):
        self.assertIsNotNone(capture, '缺少真实抓包解析模块')
        two = frames() + http_frames(port=42001, client_seq=300, server_seq=700)
        with patch.object(capture, 'MAX_MESSAGES', 1):
            self.reject(items=two)
        with patch.object(capture, 'MAX_PACKETS', 3):
            self.reject()
        with patch.object(capture, 'MAX_FLOWS', 1):
            self.reject(items=two)

    def test_interim_response_count_is_bounded(self):
        interim = b'HTTP/1.1 100 Continue\r\n\r\n' * 20 + response()
        self.reject(items=frames(http_frames(res=interim)))

    def test_tftp_retry_scan_work_is_bounded(self):
        self.assertIsNotNone(capture, '缺少真实抓包解析模块')
        items = frames(); index = next(i for i, v in enumerate(items) if v[0] == 'rrq')
        items.insert(index + 1, items[index])
        with patch.object(capture, 'MAX_TFTP_ATTEMPTS', 1, create=True):
            self.reject(items=items)

    def test_http_provenance_scan_work_is_bounded(self):
        self.assertIsNotNone(capture, '缺少真实抓包解析模块')
        with patch.object(capture, 'MAX_PROVENANCE_WORK', 1, create=True):
            self.reject()

    def test_pcap_symlink_is_not_a_frozen_capture(self):
        self.assertIsNotNone(capture, '缺少真实抓包解析模块')
        data = pcap(frames())
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / 'capture'; target.write_bytes(data)
            alias = Path(directory) / 'alias'; alias.symlink_to(target)
            with self.assertRaises(ValueError):
                capture.analyze_capture(alias, contract(data))

    def test_success_without_expected_response_digest_still_records_actual_digest(self):
        def remove(c):
            c['http'][0].pop('response_body_sha256')
        self.assertEqual(self.parse(change=remove)['http'][0]['response_body_sha256'], digest(b'OK'))

    def test_missing_http_response_or_downgraded_contract_rejected(self):
        self.reject(items=[v for v in frames() if not v[0].startswith('response')])
        for change in [lambda c: c.update(tftp={}), lambda c: c.update(http=[]),
                       lambda c: c['http'][0].update(status=500), lambda c: c.update(client_ip=SERVICE_IP)]:
            self.reject(change=change)

    def test_private_replay_extracts_only_exact_verified_successful_callback(self):
        self.assertIsNotNone(capture, '缺少真实抓包解析模块')
        data = pcap(frames()); frozen = contract(data)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'private.pcap'; path.write_bytes(data)
            evidence = capture.analyze_capture(path, frozen)['http'][0]['reference']
            private = capture.extract_verified_request(path, frozen, evidence)
            self.assertEqual(private['target'], TARGET)
            self.assertEqual(private['body'], b'yes')
            changed = copy.deepcopy(evidence); changed['request_index'] += 1
            with self.assertRaises(ValueError):
                capture.extract_verified_request(path, frozen, changed)

    def test_private_replay_does_not_extract_a_boot_get(self):
        self.assertIsNotNone(capture, '缺少真实抓包解析模块')
        target = '/v1/boot?sn=PQ25L01'
        req = b'GET ' + target.encode() + b' HTTP/1.1\r\nHost: lober\r\n\r\n'
        data = pcap(frames(http_frames(req=req))); frozen = contract(data)
        frozen['http'][0].update(method='GET', target_sha256=digest(target.encode()), request_body_sha256=digest(b''))
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'private.pcap'; path.write_bytes(data)
            evidence = capture.analyze_capture(path, frozen)['http'][0]['reference']
            with self.assertRaises(ValueError):
                capture.extract_verified_request(path, frozen, evidence)

    def test_private_replay_requires_the_original_callback_status_200(self):
        self.assertIsNotNone(capture, '缺少真实抓包解析模块')
        data = pcap(frames(http_frames(res=response(status=201)))); frozen = contract(data)
        frozen['http'][0]['status'] = 201
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'private.pcap'; path.write_bytes(data)
            evidence = capture.analyze_capture(path, frozen)['http'][0]['reference']
            with self.assertRaises(ValueError):
                capture.extract_verified_request(path, frozen, evidence)


NO_TASK_TARGET = '/v1/boot?sn=PQ25L01&arch=x86_64&firmware=bios'
LOCAL_BOOT = b'#!ipxe\nexit || sanboot --no-describe --drive 0x80\n'


def no_task_request(target=NO_TASK_TARGET):
    return b'GET ' + target.encode() + b' HTTP/1.1\r\nHost: lober\r\n\r\n'


def no_task_contract(data):
    result = contract(data)
    result.update(schema='lober-cloud-no-task-packet-v1', scope='NO_TASK', task_id=None, client_sn='PQ25L01', max_boot_exchanges=4)
    result['http'] = [{'name': 'no_task_boot', 'method': 'GET', 'status': 200,
                       'target_sha256': digest(NO_TASK_TARGET.encode()), 'request_body_sha256': digest(b''),
                       'response_body_sha256': digest(LOCAL_BOOT)}]
    return result


class NoTaskPacketChecks(unittest.TestCase):
    def parse(self, items=None, change=None):
        self.assertTrue(callable(getattr(capture, 'analyze_no_task_capture', None)), '缺少显式无任务解析入口')
        data = pcap(frames(http_frames(req=no_task_request(), res=response(body=LOCAL_BOOT))) if items is None else items)
        frozen = no_task_contract(data)
        if change:
            change(frozen)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'private.pcap'; path.write_bytes(data)
            return capture.analyze_no_task_capture(path, frozen)

    def reject(self, items=None, change=None):
        self.assertTrue(callable(getattr(capture, 'analyze_no_task_capture', None)), '缺少显式无任务解析入口')
        with self.assertRaises(ValueError):
            self.parse(items, change)

    def test_complete_no_task_chain_has_separate_scope_and_no_raw_target(self):
        value = self.parse()
        self.assertEqual(value['scope'], 'NO_TASK_PACKET_EVIDENCE_ONLY')
        self.assertEqual(value['http'][0]['response_body_sha256'], digest(LOCAL_BOOT))
        self.assertNotIn(NO_TASK_TARGET, str(value))
        self.assertNotIn('PRIVATE-TOKEN', str(value))

    def test_explicit_no_task_validator_and_task_none_are_required(self):
        self.assertTrue(callable(getattr(capture, 'validate_no_task_contract', None)), '缺少显式无任务合同检查')
        for change in (lambda c: c.update(task_id='fake-task'), lambda c: c.update(scope='TASK'),
                       lambda c: c.update(schema='lober-cloud-packet-v1'), lambda c: c.pop('scope')):
            with self.subTest(change=change):
                self.reject(change=change)

    def test_normal_task_entry_and_private_extraction_reject_no_task_contract(self):
        data = pcap(frames(http_frames(req=no_task_request(), res=response(body=LOCAL_BOOT))))
        frozen = no_task_contract(data)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'private.pcap'; path.write_bytes(data)
            for action in (lambda: capture.validate_contract(frozen), lambda: capture.analyze_capture(path, frozen),
                           lambda: capture.extract_verified_request(path, frozen, {})):
                with self.subTest(action=action), self.assertRaises(ValueError):
                    action()

    def test_no_task_entry_rejects_normal_contract(self):
        self.reject(change=lambda c: [c.pop(k) for k in ('scope', 'client_sn')])

    def test_no_task_body_and_target_digests_cannot_be_omitted_or_changed(self):
        for key in ('target_sha256', 'request_body_sha256', 'response_body_sha256'):
            for remove in (False, True):
                with self.subTest(key=key, remove=remove):
                    self.reject(change=lambda c: c['http'][0].pop(key) if remove else c['http'][0].update({key: '0' * 64}))

    def test_no_task_rejects_post_signal_even_with_success_and_edited_hash(self):
        items = frames(http_frames())
        def alter(c):
            c['http'][0].update(method='POST', target_sha256=digest(TARGET.encode()),
                                 request_body_sha256=digest(b'yes'), response_body_sha256=digest(b'OK'))
        self.reject(items, alter)

    def test_no_task_rejects_wrong_sn_or_arch_firmware_target(self):
        for target in (NO_TASK_TARGET.replace('PQ25L01', 'PQ25L02'), NO_TASK_TARGET.replace('bios', 'uefi'),
                       NO_TASK_TARGET + '&extra=yes', '/v1/boot?sn=PQ25L01'):
            with self.subTest(target=target):
                self.reject(frames(http_frames(req=no_task_request(target), res=response(body=LOCAL_BOOT))),
                            lambda c: c['http'][0].update(target_sha256=digest(target.encode())))

    def test_no_task_rejects_other_http_exchange_in_same_or_another_flow(self):
        valid = http_frames(req=no_task_request(), res=response(body=LOCAL_BOOT))
        for other in (http_frames(), http_frames(port=43000)):
            with self.subTest(port=other[0][1][34:38]):
                self.reject(frames(valid + other))

    def test_no_task_accepts_equivalent_boot_gets_on_same_connection(self):
        value = self.parse(frames(http_frames(req=no_task_request() * 2, res=response(body=LOCAL_BOOT) * 2)))
        self.assertEqual(value['boot_exchange_count'], 2)
        self.assertEqual(len(value['http']), 2)

    def test_no_task_accepts_equivalent_boot_gets_on_new_connection(self):
        valid = http_frames(req=no_task_request(), res=response(body=LOCAL_BOOT))
        second = http_frames(req=no_task_request(), res=response(body=LOCAL_BOOT), client_seq=300, server_seq=700)
        value = self.parse(frames(valid + second))
        self.assertEqual(value['boot_exchange_count'], 2)
        self.assertNotEqual(value['http'][0]['reference']['flow_id'], value['http'][1]['reference']['flow_id'])

    def test_no_task_repeat_bound_is_frozen_and_cannot_be_exceeded(self):
        items = frames(http_frames(req=no_task_request() * 2, res=response(body=LOCAL_BOOT) * 2))
        self.reject(items, lambda c: c.update(max_boot_exchanges=1))
        for count in (0, 17, True, None):
            with self.subTest(count=count):
                self.reject(change=lambda c: c.update(max_boot_exchanges=count))
        self.reject(change=lambda c: c.pop('max_boot_exchanges'))

    def test_no_task_rejects_wrong_local_boot_body_or_status(self):
        for reply in (response(body=b'#!ipxe\nboot\n'), response(404, LOCAL_BOOT), response(body=LOCAL_BOOT[:-1])):
            with self.subTest(reply=reply):
                self.reject(frames(http_frames(req=no_task_request(), res=reply)))

    def test_no_task_get_body_and_incomplete_callback_tail_are_rejected(self):
        get_body = no_task_request().replace(b'\r\n\r\n', b'\r\nContent-Length: 3\r\n\r\nyes')
        self.reject(frames(http_frames(req=get_body, res=response(body=LOCAL_BOOT))))
        valid = http_frames(req=no_task_request(), res=response(body=LOCAL_BOOT))
        tail = request()[:20]
        extra = ('partial-post', tcp(CLIENT_IP, SERVICE_IP, 42000, 18090,
                                     101 + len(no_task_request()), 501 + len(response(body=LOCAL_BOOT)), 0x18, tail))
        reset = ('reset', tcp(SERVICE_IP, CLIENT_IP, 18090, 42000, 501 + len(response(body=LOCAL_BOOT)),
                               101 + len(no_task_request()) + len(tail), 0x14, smac=SERVICE, dmac=CLIENT))
        self.reject(frames(valid + [extra, reset]))

    def test_no_task_retains_complete_tftp_and_frozen_capture_requirements(self):
        items = [v for v in frames(http_frames(req=no_task_request(), res=response(body=LOCAL_BOOT))) if v[0] != 'data1']
        self.reject(items)
        for change in (lambda c: c.update(pcap_sha256='0' * 64), lambda c: c.update(tftp={}),
                       lambda c: c.update(client_mac=BOOT), lambda c: c.update(capture_end_ns=1)):
            with self.subTest(change=change):
                self.reject(change=change)


if __name__ == '__main__':
    unittest.main()
