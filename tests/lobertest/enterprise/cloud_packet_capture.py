"""解析本轮私有抓包。只证明包内事实，不执行网络操作或签发装机资格。"""
from pathlib import Path
import hashlib
import heapq
import ipaddress
import re
import socket
import stat
import struct

__all__ = ['CaptureError', 'analyze_capture', 'extract_verified_request']

MAX_CAPTURE = 64 << 20
MAX_PACKETS = 200000
MAX_STREAM = 8 << 20
MAX_STREAM_TOTAL = 32 << 20
MAX_FLOWS = 4096
MAX_MESSAGES = 4096
MAX_HEADER = 65536
MAX_TFTP_ATTEMPTS = 64
MAX_PROVENANCE_WORK = 4_000_000


class CaptureError(ValueError):
    pass


class _IncompleteHTTP(CaptureError):
    pass


def require_complete(value, message):
    if not value:
        raise _IncompleteHTTP(message)


def need(value, message):
    if not value:
        raise CaptureError(message)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def valid_sha(value):
    return isinstance(value, str) and re.fullmatch('[0-9a-f]{64}', value) is not None


def validate_contract(value):
    keys = {'schema', 'pcap_sha256', 'pcap_size', 'capture_start_ns', 'capture_end_ns', 'client_mac',
            'client_ip', 'boot_server_mac', 'boot_server_ip', 'service_mac', 'service_ip', 'service_port',
            'task_id', 'tftp', 'http'}
    need(isinstance(value, dict) and set(value) == keys and value['schema'] == 'lober-cloud-packet-v1', 'Unknown packet contract')
    need(valid_sha(value['pcap_sha256']) and type(value['pcap_size']) is int and 24 <= value['pcap_size'] <= MAX_CAPTURE,
         'Invalid frozen capture identity')
    need(type(value['capture_start_ns']) is int and type(value['capture_end_ns']) is int
         and 0 <= value['capture_start_ns'] < value['capture_end_ns'], 'Invalid capture time window')
    for key in ('client_mac', 'boot_server_mac', 'service_mac'):
        need(isinstance(value[key], str) and re.fullmatch('(?:[0-9a-f]{2}:){5}[0-9a-f]{2}', value[key]), 'Invalid frozen MAC')
        need(not int(value[key][:2], 16) & 1, 'Frozen peer must be unicast')
    need(len({value[k] for k in ('client_mac', 'boot_server_mac', 'service_mac')}) == 3, 'Peer MACs overlap')
    for key in ('client_ip', 'boot_server_ip', 'service_ip'):
        try:
            address = ipaddress.IPv4Address(value[key])
        except (ValueError, TypeError):
            raise CaptureError('Invalid frozen IPv4') from None
        need(str(address) == value[key] and not address.is_multicast and not address.is_unspecified, 'Invalid frozen IPv4')
    need(len({value[k] for k in ('client_ip', 'boot_server_ip', 'service_ip')}) == 3, 'Peer IPv4s overlap')
    need(type(value['service_port']) is int and 1 <= value['service_port'] <= 65535, 'Invalid frozen port')
    need(isinstance(value['task_id'], str) and re.fullmatch('[a-zA-Z0-9_-]{1,80}', value['task_id']), 'Invalid task binding')
    need(isinstance(value['tftp'], dict) and 1 <= len(value['tftp']) <= 16, 'Required TFTP set absent')
    for name, item in value['tftp'].items():
        need(isinstance(name, str) and re.fullmatch(r'[A-Za-z0-9_.\-/]{1,255}', name)
             and not name.startswith('/') and all(p not in ('', '.', '..') for p in name.split('/')), 'Invalid TFTP name')
        need(isinstance(item, dict) and set(item) == {'size', 'sha256'} and type(item['size']) is int
             and 0 < item['size'] <= MAX_STREAM_TOTAL and valid_sha(item['sha256']), 'Invalid TFTP identity')
    need(isinstance(value['http'], list) and 1 <= len(value['http']) <= 128, 'Required HTTP set absent')
    names = set()
    for item in value['http']:
        need(isinstance(item, dict) and {'name', 'method', 'target_sha256', 'status'}.issubset(item)
             and set(item).issubset({'name', 'method', 'target_sha256', 'status', 'request_body_sha256', 'response_body_sha256'}),
             'Invalid HTTP requirement')
        need(isinstance(item['name'], str) and re.fullmatch('[a-z][a-z0-9_-]{0,63}', item['name'])
             and item['name'] not in names, 'Invalid or duplicate evidence name')
        names.add(item['name'])
        need(item['method'] in ('GET', 'POST') and valid_sha(item['target_sha256']) and type(item['status']) is int
             and 200 <= item['status'] < 300, 'HTTP requirement must bind a success')
        for key in ('request_body_sha256', 'response_body_sha256'):
            need(key not in item or valid_sha(item[key]), 'Invalid HTTP body digest')


def read_capture(path, contract):
    path = Path(path)
    need(path.is_absolute() and path.resolve() == path and not path.is_symlink(), 'Capture path is not an exact local file')
    before = path.stat()
    need(stat.S_ISREG(before.st_mode) and before.st_size == contract['pcap_size'], 'Capture size changed')
    with path.open('rb') as source:
        data = source.read(MAX_CAPTURE + 1)
    after = path.stat()
    need((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns) ==
         (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns), 'Capture changed while reading')
    need(len(data) == contract['pcap_size'] and sha(data) == contract['pcap_sha256'], 'Capture bytes differ from frozen identity')
    return data


def parse_pcap(data, contract):
    formats = {b'\xd4\xc3\xb2\xa1': ('<', 1000), b'\xa1\xb2\xc3\xd4': ('>', 1000),
               b'\x4d\x3c\xb2\xa1': ('<', 1), b'\xa1\xb2\x3c\x4d': ('>', 1)}
    need(len(data) >= 24 and data[:4] in formats, 'Unsupported or truncated PCAP header')
    endian, factor = formats[data[:4]]
    major, minor, _, _, snaplen, link = struct.unpack_from(endian + 'HHIIII', data, 4)
    need((major, minor) == (2, 4) and link == 1 and 14 <= snaplen <= MAX_CAPTURE, 'Unsupported PCAP version or link type')
    cursor, packets, count = 24, [], 0
    while cursor < len(data):
        need(cursor + 16 <= len(data), 'Truncated packet record')
        seconds, fraction, captured, original = struct.unpack_from(endian + 'IIII', data, cursor)
        cursor += 16
        need(captured == original and 14 <= captured <= snaplen and cursor + captured <= len(data), 'Packet was truncated or has invalid lengths')
        need(fraction < (1_000_000_000 if factor == 1 else 1_000_000), 'Invalid timestamp fraction')
        at = seconds * 1_000_000_000 + fraction * factor
        need(contract['capture_start_ns'] <= at <= contract['capture_end_ns'], 'Packet lies outside frozen capture window')
        count += 1
        need(count <= MAX_PACKETS, 'Packet count limit exceeded')
        packet = decode_packet(data[cursor:cursor + captured], count, at)
        if packet is not None:
            packets.append(packet)
        cursor += captured
    need(count > 0, 'Empty capture')
    return packets, count


def decode_packet(frame, index, at):
    destination, source = frame[:6].hex(':'), frame[6:12].hex(':')
    kind, cursor, tags = struct.unpack_from('!H', frame, 12)[0], 14, 0
    while kind in (0x8100, 0x88a8):
        need(cursor + 4 <= len(frame) and tags < 2, 'Invalid VLAN header')
        kind = struct.unpack_from('!H', frame, cursor + 2)[0]
        cursor += 4; tags += 1
    if kind != 0x0800:
        return None
    need(cursor + 20 <= len(frame), 'Truncated IPv4 header')
    version, header = frame[cursor] >> 4, (frame[cursor] & 15) * 4
    length, fragment = struct.unpack_from('!HH', frame, cursor + 2)[0], struct.unpack_from('!H', frame, cursor + 6)[0]
    need(version == 4 and 20 <= header <= length and cursor + length <= len(frame), 'Invalid IPv4 lengths')
    need(not fragment & 0x3fff, 'Fragmented IPv4 cannot prove a complete protocol message')
    protocol = frame[cursor + 9]
    if protocol not in (6, 17):
        return None
    packet = {'index': index, 'time_ns': at, 'src_mac': source, 'dst_mac': destination,
              'src_ip': socket.inet_ntoa(frame[cursor + 12:cursor + 16]),
              'dst_ip': socket.inet_ntoa(frame[cursor + 16:cursor + 20]), 'protocol': protocol}
    payload = frame[cursor + header:cursor + length]
    need(len(payload) >= (20 if protocol == 6 else 8), 'Truncated transport header')
    packet['sport'], packet['dport'] = struct.unpack_from('!HH', payload)
    if protocol == 17:
        size = struct.unpack_from('!H', payload, 4)[0]
        need(size == len(payload) and size >= 8, 'Invalid UDP length')
        packet['data'] = payload[8:]
    else:
        offset = (payload[12] >> 4) * 4
        need(20 <= offset <= len(payload), 'Invalid TCP header length')
        packet.update(seq=struct.unpack_from('!I', payload, 4)[0], ack=struct.unpack_from('!I', payload, 8)[0],
                      flags=payload[13], data=payload[offset:])
    # 网桥抓到的校验和可能尚未由网卡补全；不把校验和字段当身份或完整性证明。
    return packet


def dhcp_message(packet, contract):
    if packet['protocol'] != 17 or (packet['sport'], packet['dport']) not in ((68, 67), (67, 68)):
        return None
    data = packet['data']
    need(len(data) >= 240 and data[236:240] == b'\x63\x82\x53\x63', 'Malformed DHCP payload')
    if data[1:3] != b'\x01\x06' or data[28:34].hex(':') != contract['client_mac']:
        return None
    options, cursor, ended = {}, 240, False
    while cursor < len(data):
        code = data[cursor]; cursor += 1
        if code == 255:
            ended = True; break
        if code == 0:
            continue
        need(cursor < len(data) and cursor + 1 + data[cursor] <= len(data), 'Truncated DHCP option')
        size = data[cursor]; cursor += 1
        if code in (50, 51, 53, 54, 67, 77):
            need(code not in options, 'Duplicate critical DHCP option')
        options[code] = options.get(code, b'') + data[cursor:cursor + size]
        cursor += size
    need(ended and len(options.get(53, b'')) == 1, 'DHCP message type/end absent')
    kind = options[53][0]
    if kind not in (1, 2, 3, 5):
        return None
    client = kind in (1, 3)
    need(data[0] == (1 if client else 2) and packet['sport'] == (68 if client else 67), 'DHCP direction mismatch')
    need(packet['src_mac'] == contract['client_mac' if client else 'boot_server_mac'], 'DHCP peer MAC mismatch')
    peer = contract['boot_server_mac' if client else 'client_mac']
    need(packet['dst_mac'] in (peer, 'ff:ff:ff:ff:ff:ff'), 'DHCP destination MAC mismatch')
    if client:
        need(packet['src_ip'] in ('0.0.0.0', contract['client_ip']) and packet['dst_ip'] in
             (contract['boot_server_ip'], '255.255.255.255'), 'DHCP client routing mismatch')
    if not client:
        need(packet['src_ip'] == contract['boot_server_ip'] and data[16:20] == socket.inet_aton(contract['client_ip'])
             and packet['dst_ip'] in (contract['client_ip'], '255.255.255.255')
             and data[20:24] == socket.inet_aton(contract['boot_server_ip']), 'DHCP offered address/server mismatch')
    need(data[24:28] == b'\0' * 4, 'Relayed DHCP is outside this direct bridge contract')
    if kind != 1:
        need(options.get(54) == socket.inet_aton(contract['boot_server_ip']), 'DHCP server identifier mismatch')
    if kind == 3:
        need(options.get(50) == socket.inet_aton(contract['client_ip']), 'DHCP requested address mismatch')
    if kind == 5:
        need(len(options.get(51, b'')) == 4 and struct.unpack('!I', options[51])[0] > 0, 'DHCP lease length absent')
    filename = options.get(67, data[108:236].split(b'\0', 1)[0]).rstrip(b'\0')
    return {'type': kind, 'xid': struct.unpack_from('!I', data, 4)[0], 'packet': packet,
            'boot_file': filename, 'user_class_sha256': sha(options.get(77, b''))}


def prove_dhcp(packets, contract):
    exchanges = {}
    for packet in packets:
        message = dhcp_message(packet, contract)
        if message is not None:
            exchanges.setdefault(message['xid'], []).append(message)
    for xid, messages in exchanges.items():
        chain = []
        for message in messages:
            expected = (1, 2, 3, 5)[len(chain)]
            if message['type'] == expected:
                chain.append(message)
            if len(chain) == 4:
                if chain[-1]['boot_file'] in {name.encode() for name in contract['tftp']}:
                    return {'client_ip': contract['client_ip'], 'client_mac': contract['client_mac'],
                            'server_ip': contract['boot_server_ip'], 'transaction_id': xid,
                            'packet_indices': [m['packet']['index'] for m in chain],
                            'time_ns': [m['packet']['time_ns'] for m in chain],
                            'boot_file': chain[-1]['boot_file'].decode('ascii'),
                            'client_user_class_sha256': chain[2]['user_class_sha256']}
                chain = []
    raise CaptureError('Complete bound DHCP discover/offer/request/ack absent')


def tftp_options(data):
    need(data.endswith(b'\0'), 'Incomplete TFTP option strings')
    parts = data[:-1].split(b'\0')
    need(len(parts) % 2 == 0 and len(parts) <= 32, 'Malformed TFTP options')
    options = {}
    for key, value in zip(parts[::2], parts[1::2]):
        try:
            key = key.decode('ascii').lower(); text = value.decode('ascii')
        except UnicodeDecodeError:
            raise CaptureError('Non-ASCII TFTP option') from None
        need(key in ('blksize', 'tsize', 'timeout', 'windowsize') and key not in options and text.isdecimal(), 'Unsupported TFTP negotiation')
        options[key] = int(text)
    return options


def prove_tftp(packets, contract, lease):
    expected, proven = contract['tftp'], []
    attempt_count = 0
    for name, frozen in expected.items():
        attempts = []
        for packet in packets:
            if packet['protocol'] != 17 or (packet['src_ip'], packet['dst_ip'], packet['dport']) != (contract['client_ip'], contract['boot_server_ip'], 69):
                continue
            if packet['src_mac'] != contract['client_mac'] or packet['dst_mac'] != contract['boot_server_mac']:
                continue
            data = packet['data']
            if data[:2] != b'\0\1':
                continue
            parts = data[2:].split(b'\0')
            need(len(parts) >= 3 and parts[-1] == b'', 'Incomplete TFTP RRQ')
            if parts[0] != name.encode():
                continue
            need(parts[1].lower() == b'octet' and packet['index'] > lease['packet_indices'][-1], 'TFTP mode or lease order mismatch')
            options = tftp_options(b'\0'.join(parts[2:])) if len(parts) > 3 else {}
            attempt_count += 1
            need(attempt_count <= MAX_TFTP_ATTEMPTS, 'TFTP retry work bound exceeded')
            attempts.append((packet, options))
        for rrq, requested in attempts:
            server_tid, block_size, chunks, acknowledgments, indices, final = None, 512, {}, {}, [rrq['index']], None
            data_indices = {}
            oack, first_data, last_index, transferred, negotiation = False, None, rrq['index'], 0, None
            for packet in packets:
                if packet['index'] <= rrq['index'] or packet['protocol'] != 17:
                    continue
                server = (packet['src_ip'], packet['dst_ip'], packet['dport'], packet['src_mac'], packet['dst_mac']) == (
                    contract['boot_server_ip'], contract['client_ip'], rrq['sport'], contract['boot_server_mac'], contract['client_mac'])
                client = (packet['src_ip'], packet['dst_ip'], packet['sport'], packet['src_mac'], packet['dst_mac']) == (
                    contract['client_ip'], contract['boot_server_ip'], rrq['sport'], contract['client_mac'], contract['boot_server_mac'])
                if not (server or client):
                    continue
                data = packet['data']
                if client and packet['dport'] == 69 and data[:2] == b'\0\1':
                    # 同端口的新RRQ不能给前一次传输补块。
                    break
                if len(data) < 2:
                    raise CaptureError('Truncated TFTP operation')
                opcode = struct.unpack_from('!H', data)[0]
                if server and server_tid is None and opcode in (3, 6):
                    server_tid = packet['sport']
                if server_tid is None or (server and packet['sport'] != server_tid) or (client and packet['dport'] != server_tid):
                    continue
                if server and opcode == 5:
                    raise CaptureError('Required TFTP transfer returned an error')
                if server and opcode == 6:
                    need(not chunks, 'TFTP negotiation changed after data')
                    negotiated = tftp_options(data[2:])
                    need(negotiation is None or negotiation == negotiated, 'TFTP negotiation changed within one transfer')
                    negotiation = negotiated
                    need(set(negotiated).issubset(requested), 'Unrequested TFTP option')
                    block_size = negotiated.get('blksize', 512)
                    need(8 <= block_size <= 65464 and block_size <= requested.get('blksize', 512), 'Invalid TFTP block size')
                    need('tsize' not in negotiated or negotiated['tsize'] == frozen['size'], 'TFTP negotiated size mismatch')
                    need('windowsize' not in negotiated or 1 <= negotiated['windowsize'] <= requested['windowsize'] <= 65535, 'Invalid TFTP window size')
                    need('timeout' not in negotiated or 1 <= negotiated['timeout'] <= 255 and negotiated['timeout'] == requested['timeout'], 'TFTP timeout negotiation mismatch')
                    oack = True
                elif server and opcode == 3:
                    need(len(data) >= 4, 'Truncated TFTP DATA')
                    number = struct.unpack_from('!H', data, 2)[0]; body = data[4:]
                    need(1 <= number <= 65535 and len(body) <= block_size, 'Invalid TFTP block')
                    need(number not in chunks or chunks[number] == body, 'Conflicting TFTP retransmission')
                    if number not in chunks:
                        chunks[number] = body
                        data_indices[number] = packet['index']
                        transferred += len(body)
                    need(transferred <= frozen['size'], 'TFTP transfer exceeds frozen file')
                    if first_data is None:
                        first_data = packet['index']
                    if len(body) < block_size:
                        need(final is None or final == number, 'Conflicting TFTP final blocks')
                        final = number
                    last_index = packet['index']
                elif client and opcode == 4:
                    need(len(data) == 4, 'Malformed TFTP ACK')
                    number = struct.unpack_from('!H', data, 2)[0]
                    acknowledgments.setdefault(number, []).append(packet['index'])
                else:
                    raise CaptureError('Unexpected packet in required TFTP transfer')
                indices.append(packet['index'])
                if final is not None and final in acknowledgments:
                    break
            if final is None or first_data is None or set(chunks) != set(range(1, final + 1)):
                continue
            need(not oack or any(i < first_data for i in acknowledgments.get(0, [])), 'TFTP OACK was not acknowledged')
            need(any(i > last_index for i in acknowledgments.get(final, [])), 'TFTP final ACK absent')
            window = (negotiation or {}).get('windowsize', 1)
            covered = rrq['index']
            for number in range(1, final + 1):
                covered = max(covered, data_indices[number])
                if number % window == 0 and number < final:
                    following = min(data_indices[i] for i in range(number + 1, min(number + window, final) + 1))
                    need(any(covered < i < following for i in acknowledgments.get(number, [])),
                         'TFTP window acknowledgment absent after complete window and before next window')
            data = b''.join(chunks[i] for i in range(1, final + 1))
            need(len(data) == frozen['size'] and sha(data) == frozen['sha256'], 'TFTP complete content differs')
            proven.append({'name': name, 'bytes': len(data), 'sha256': sha(data), 'client_port': rrq['sport'],
                           'server_port': server_tid, 'block_size': block_size, 'blocks': final,
                           'negotiated_options': negotiation or {}, 'packet_indices': indices})
            break
        else:
            raise CaptureError('Required complete TFTP transfer absent')
    return proven


def relative_seq(number, origin):
    result = (number - origin) & 0xffffffff
    return result if result < 0x80000000 else result - 0x100000000


def reassemble(segments, fin_positions):
    body = bytearray()
    for offset, data, packet in sorted(segments, key=lambda row: (row[0], row[2]['index'])):
        need(0 <= offset <= len(body) and offset + len(data) <= MAX_STREAM, 'TCP stream gap or bound exceeded')
        overlap = min(len(data), len(body) - offset)
        need(body[offset:offset + overlap] == data[:overlap], 'Conflicting TCP overlap')
        body.extend(data[overlap:])
    need(not fin_positions or set(fin_positions) == {len(body)}, 'TCP FIN does not follow complete stream')
    return bytes(body)


def tcp_flows(packets, contract):
    active, flows = {}, []
    for packet in packets:
        if packet['protocol'] != 6:
            continue
        client = (packet['src_ip'], packet['dst_ip'], packet['dport'], packet['src_mac'], packet['dst_mac']) == (
            contract['client_ip'], contract['service_ip'], contract['service_port'], contract['client_mac'], contract['service_mac'])
        server = (packet['src_ip'], packet['dst_ip'], packet['sport'], packet['src_mac'], packet['dst_mac']) == (
            contract['service_ip'], contract['client_ip'], contract['service_port'], contract['service_mac'], contract['client_mac'])
        if not (client or server):
            continue
        port = packet['sport'] if client else packet['dport']
        syn, ack = bool(packet['flags'] & 2), bool(packet['flags'] & 16)
        if packet['flags'] & 4:
            flow = active.get(port)
            if flow is not None and not flow['reset']:
                if flow['server_isn'] is None:
                    valid = server and ack and packet['ack'] == (flow['client_isn'] + 1) & 0xffffffff
                else:
                    side = 'client' if client else 'server'
                    offset = relative_seq(packet['seq'], (flow[side + '_isn'] + 1) & 0xffffffff)
                    expected = flow[side + '_next']
                    expected += expected in flow[side + '_fin']
                    valid = offset == expected
                if valid:
                    flow['reset'] = packet['index']
            # 复位携带的诊断字节永远不是HTTP数据；无效复位不能破坏此前证据。
            continue
        if client and syn and not ack:
            need(not packet['data'], 'TCP fast-open payload is unsupported')
            if port not in active or active[port]['reset'] or active[port]['client_isn'] != packet['seq']:
                need(len(flows) < MAX_FLOWS, 'TCP connection bound exceeded')
                flow = {'client_port': port, 'client_isn': packet['seq'], 'server_isn': None,
                        'client': [], 'server': [], 'client_fin': [], 'server_fin': [], 'acks': [],
                        'reset': None, 'client_next': 0, 'server_next': 0, 'client_seen': [], 'server_seen': []}
                active[port] = flow; flows.append(flow)
            continue
        need(port in active, 'TCP peer response lacks its client SYN')
        flow = active[port]
        if flow['reset']:
            continue
        if server and syn:
            need(ack and packet['ack'] == (flow['client_isn'] + 1) & 0xffffffff and not packet['data'], 'TCP SYN/ACK mismatch')
            need(flow['server_isn'] in (None, packet['seq']), 'Conflicting TCP server ISN')
            flow['server_isn'] = packet['seq']; continue
        need(not syn and flow['server_isn'] is not None, 'TCP handshake incomplete')
        side = 'client' if client else 'server'
        if ack:
            flow['acks'].append((side, packet['ack']))
        offset = relative_seq(packet['seq'], (flow[side + '_isn'] + 1) & 0xffffffff)
        if packet['data']:
            need(ack and offset >= 0 and offset + len(packet['data']) <= MAX_STREAM, 'Invalid TCP data sequence')
            flow[side].append((offset, packet['data'], packet))
            pending = flow[side + '_seen']
            heapq.heappush(pending, (offset, offset + len(packet['data'])))
            while pending and pending[0][0] <= flow[side + '_next']:
                _, end = heapq.heappop(pending)
                flow[side + '_next'] = max(flow[side + '_next'], end)
        if packet['flags'] & 1:
            flow[side + '_fin'].append(offset + len(packet['data']))
    total = 0
    for index, flow in enumerate(flows):
        need(flow['server_isn'] is not None or not flow['client'] and not flow['server'], 'TCP handshake incomplete')
        for side in ('client', 'server'):
            flow[side + '_stream'] = reassemble(flow[side], flow[side + '_fin'])
            total += len(flow[side + '_stream'])
            need(total <= MAX_STREAM_TOTAL, 'Combined TCP stream bound exceeded')
        for side, number in flow['acks']:
            peer = 'server' if side == 'client' else 'client'
            offset = relative_seq(number, (flow[peer + '_isn'] + 1) & 0xffffffff)
            need(0 <= offset <= len(flow[peer + '_stream']) + bool(flow[peer + '_fin']), 'TCP ACK lies outside its peer stream')
        identity = (contract['client_ip'], flow['client_port'], contract['service_ip'], contract['service_port'],
                    flow['client_isn'], flow['server_isn'], index)
        flow['id'] = sha(repr(identity).encode())
    return flows


def parse_headers(raw):
    lines = raw.split(b'\r\n')
    need(lines and lines[0] and b'\0' not in raw, 'Malformed HTTP header')
    headers = {}
    for line in lines[1:]:
        need(b':' in line and line[:1] not in (b' ', b'\t'), 'Malformed HTTP field')
        name, value = line.split(b':', 1)
        need(re.fullmatch(rb"[!#$%&'*+.^_`|~0-9A-Za-z-]+", name) is not None, 'Invalid HTTP field name')
        headers.setdefault(name.lower(), []).append(value.strip(b' \t'))
    for key in (b'content-length', b'transfer-encoding'):
        need(len(headers.get(key, [])) <= 1, 'Ambiguous HTTP framing')
    need(not (b'content-length' in headers and b'transfer-encoding' in headers), 'Conflicting HTTP framing')
    return lines[0], headers


def chunked_body(data, cursor):
    chunks, total = [], 0
    for _ in range(65536):
        end = data.find(b'\r\n', cursor)
        need((len(data) - cursor if end < 0 else end - cursor) <= 1024, 'HTTP chunk size line bound exceeded')
        require_complete(end >= 0, 'Incomplete chunk size')
        number = data[cursor:end].split(b';', 1)[0]
        need(re.fullmatch(rb'[0-9A-Fa-f]{1,16}', number) is not None, 'Invalid HTTP chunk size')
        size = int(number, 16); cursor = end + 2
        if size == 0:
            if data[cursor:cursor + 2] == b'\r\n':
                return b''.join(chunks), cursor + 2
            trailer = data.find(b'\r\n\r\n', cursor)
            need((len(data) - cursor if trailer < 0 else trailer - cursor) <= MAX_HEADER, 'HTTP trailers bound exceeded')
            require_complete(trailer >= 0, 'Incomplete HTTP trailers')
            _, fields = parse_headers(b'trailers\r\n' + data[cursor:trailer])
            need(not set(fields).intersection((b'content-length', b'transfer-encoding')), 'Framing field in HTTP trailer')
            return b''.join(chunks), trailer + 4
        total += size
        need(total <= MAX_STREAM, 'HTTP chunk body bound exceeded')
        require_complete(cursor + size + 2 <= len(data), 'Incomplete HTTP chunk body')
        need(data[cursor + size:cursor + size + 2] == b'\r\n', 'Invalid HTTP chunk terminator')
        chunks.append(data[cursor:cursor + size]); cursor += size + 2
    raise CaptureError('HTTP chunk count bound exceeded')


def http_message(data, cursor, is_request, closed=False, request_method=None):
    start = cursor; end = data.find(b'\r\n\r\n', cursor)
    need((len(data) - cursor if end < 0 else end - cursor) <= MAX_HEADER, 'HTTP message header bound exceeded')
    require_complete(end >= 0, 'Incomplete HTTP message header')
    first, headers = parse_headers(data[cursor:end]); cursor = end + 4
    item = {'start': start, 'headers': headers}
    if is_request:
        parts = first.split(b' ')
        need(len(parts) == 3 and re.fullmatch(rb'[A-Z]{1,16}', parts[0]) is not None
             and parts[1].startswith(b'/') and parts[2] in (b'HTTP/1.0', b'HTTP/1.1'), 'Invalid HTTP request line')
        need(all(32 < byte < 127 for byte in parts[1]), 'Invalid HTTP request target')
        item.update(method=parts[0].decode('ascii'), target=parts[1])
    else:
        parts = first.split(b' ', 2)
        need(len(parts) >= 2 and parts[0] in (b'HTTP/1.0', b'HTTP/1.1') and re.fullmatch(rb'[0-9]{3}', parts[1]), 'Invalid HTTP status line')
        item['status'] = int(parts[1])
    version = parts[2] if is_request else parts[0]
    need(version != b'HTTP/1.0' or b'transfer-encoding' not in headers, 'HTTP/1.0 transfer encoding is invalid framing')
    no_body = not is_request and (item['status'] < 200 or item['status'] in (204, 304) or request_method == 'HEAD')
    if no_body:
        body = b''
    elif b'transfer-encoding' in headers:
        need(headers[b'transfer-encoding'][0].lower() == b'chunked', 'Unsupported HTTP transfer encoding')
        body, cursor = chunked_body(data, cursor)
    elif b'content-length' in headers:
        value = headers[b'content-length'][0]
        need(re.fullmatch(rb'[0-9]{1,10}', value) is not None, 'Invalid HTTP Content-Length')
        size = int(value)
        need(size <= MAX_STREAM, 'HTTP body bound exceeded')
        require_complete(cursor + size <= len(data), 'Incomplete HTTP body')
        body = data[cursor:cursor + size]; cursor += size
    elif is_request:
        body = b''
    else:
        require_complete(closed, 'Unfinished close-delimited HTTP response')
        body = data[cursor:]; cursor = len(data)
    item.update(body=body, end=cursor, raw_sha256=sha(data[start:cursor]))
    return item, cursor


def provenance(segments, start, end):
    packets = [packet for offset, data, packet in segments if offset < end and offset + len(data) > start]
    need(packets, 'HTTP message has no packet provenance')
    pending, covered, complete = [], start, None
    # 段按捕获顺序进入流。只取首次覆盖边界，迟到的同字节副本仍留在来源中。
    for offset, data, packet in segments:
        if offset >= end or offset + len(data) <= start:
            continue
        heapq.heappush(pending, (max(start, offset), min(end, offset + len(data))))
        while pending and pending[0][0] <= covered:
            _, boundary = heapq.heappop(pending)
            covered = max(covered, boundary)
        if covered == end:
            complete = packet
            break
    need(complete is not None, 'HTTP message provenance has a gap')
    return {'packet_indices': sorted({p['index'] for p in packets}),
            'first_ns': min(p['time_ns'] for p in packets), 'last_ns': max(p['time_ns'] for p in packets),
            'first_complete_packet_index': complete['index'], 'first_complete_ns': complete['time_ns']}


def http_exchanges(flows, contract):
    exchanges = []
    provenance_work = 0
    for flow in flows:
        client, server = flow['client_stream'], flow['server_stream']
        requests, cursor = [], 0
        while cursor < len(client):
            need(len(exchanges) + len(requests) < MAX_MESSAGES, 'HTTP message count bound exceeded')
            try:
                item, cursor = http_message(client, cursor, True)
            except _IncompleteHTTP:
                if flow['reset']:
                    break
                raise
            requests.append(item)
        provenance_work += len(requests) * (len(flow['client']) + len(flow['server']))
        need(provenance_work <= MAX_PROVENANCE_WORK, 'HTTP packet provenance work bound exceeded')
        cursor, aborted = 0, False
        for index, request in enumerate(requests):
            interim = 0
            try:
                while True:
                    response, cursor = http_message(server, cursor, False, bool(flow['server_fin']), request['method'])
                    if response['status'] >= 200:
                        break
                    interim += 1
                    need(interim <= 16, 'HTTP interim response count bound exceeded')
            except _IncompleteHTTP:
                if flow['reset']:
                    aborted = True
                    break
                raise
            req_ref = provenance(flow['client'], request['start'], request['end'])
            res_ref = provenance(flow['server'], response['start'], response['end'])
            need(req_ref['first_complete_packet_index'] < min(res_ref['packet_indices']), 'HTTP response precedes its complete request')
            callback = request['method'] == 'POST' and request['target'].startswith(b'/v1/signal/')
            if callback:
                need(request['target'].startswith(('/v1/signal/' + contract['task_id'] + '/').encode()), 'Callback task mismatch')
            reference = {'capture_sha256': contract['pcap_sha256'], 'flow_id': flow['id'], 'request_index': index,
                         'method': request['method'], 'target_sha256': sha(request['target']), 'request_body_sha256': sha(request['body'])}
            public = {'reference': reference, 'method': request['method'], 'target_sha256': sha(request['target']),
                      'request_body_sha256': sha(request['body']), 'request_body_bytes': len(request['body']),
                      'response_status': response['status'], 'response_body_sha256': sha(response['body']),
                      'response_body_bytes': len(response['body']), 'request_raw_sha256': request['raw_sha256'],
                      'response_raw_sha256': response['raw_sha256'], 'request': req_ref, 'response': res_ref,
                      'request_stream_sha256': sha(client), 'response_stream_sha256': sha(server),
                      'client_port': flow['client_port'], 'callback': callback}
            exchanges.append((public, request))
        need(aborted or cursor == len(server), 'Unpaired HTTP response bytes')
    return exchanges


def _analyze(path, contract):
    validate_contract(contract)
    data = read_capture(path, contract)
    packets, count = parse_pcap(data, contract)
    lease = prove_dhcp(packets, contract)
    transfers = prove_tftp(packets, contract, lease)
    exchanges = http_exchanges(tcp_flows(packets, contract), contract)
    records, private, used = [], [], set()
    for required in contract['http']:
        for public, request in exchanges:
            identity = (public['reference']['flow_id'], public['reference']['request_index'])
            if identity in used or not all(public[key] == required[key] for key in ('method', 'target_sha256')):
                continue
            if public['response_status'] != required['status']:
                continue
            if any(public[key] != required[key] for key in ('request_body_sha256', 'response_body_sha256') if key in required):
                continue
            need(min(public['request']['packet_indices']) > max(max(row['packet_indices']) for row in transfers),
                 'HTTP precedes the complete required TFTP chain')
            used.add(identity)
            record = {**public, 'name': required['name']}
            records.append(record); private.append((record, request)); break
        else:
            raise CaptureError('Required bound successful HTTP exchange absent')
    return {'schema': 'lober-cloud-packet-evidence-v1', 'scope': 'PACKET_EVIDENCE_ONLY',
            'capture_sha256': sha(data), 'capture_bytes': len(data), 'packet_count': count,
            'dhcp': lease, 'tftp': transfers, 'http': records}, private


def analyze_capture(path, contract):
    """返回脱敏包证据；契约内每个必需交换都须真实存在且完整。"""
    return _analyze(path, contract)[0]


def extract_verified_request(path, contract, reference):
    """仅供私有回放准备。返回真实成功回调的原目标和正文，不发送请求。"""
    _, records = _analyze(path, contract)
    for public, request in records:
        if public['reference'] == reference:
            need(public['callback'] and public['method'] == 'POST' and public['response_status'] == 200,
                 'Only an exact previously successful callback may be extracted')
            values = request['headers'].get(b'content-type', [])
            need(len(values) <= 1, 'Ambiguous callback Content-Type')
            return {'method': 'POST', 'target': request['target'].decode('ascii'), 'body': request['body'],
                    'content_type': values[0].decode('latin1') if values else None, 'reference': public['reference'],
                    'response_status': public['response_status']}
    raise CaptureError('Verified callback reference absent')
