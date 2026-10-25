"""只准备并核对无任务短流程原件。没有启动客户或连接宿主的入口。"""
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import stat
import uuid
import xml.etree.ElementTree as ET

__all__ = ['ProbeError', 'prepare_no_task_probe', 'verify_no_task_probe']
VM_BASE = Path('/home/dev/lober-production-qualification-20261025')
HOST_BASE = '/var/lib/libvirt/images/lober-pq-20261025'
PURPOSE = 'production-qualification-20261025'
FAILURE_KIND = 'no_active_task_local_boot_on_blank_disk'
RECORDS = {'target_contract', 'runtime_record', 'instance_record', 'api_before', 'api_after',
           'disk_before', 'disk_after', 'client_start', 'client_stop', 'capture_receipt', 'capture_anchor'}
TARGET = {'host': 'ser866667880745', 'name': 'lober-pq25-linux01', 'uuid': '7fe71ebb-a34d-4155-8410-d1427ca2f83a',
          'sn': 'PQ25L01', 'mac': '52:54:00:25:10:01', 'cpus': 2, 'memory_mib': 4096,
          'disk_path': HOST_BASE + '/linux01.qcow2', 'disk_serial': 'PQ25_LINUX01_DISK', 'disk_bytes': 16 * 1024**3,
          'data_path': HOST_BASE + '/media01.qcow2', 'data_serial': 'PQ25_MEDIA01_DISK', 'data_bytes': 16 * 1024**3,
          'service_uuid': '895a44e1-8abd-4463-bbf7-fba8a743ed59', 'service_mac': '52:54:00:25:10:10',
          'network': 'lober-pq25', 'bridge': 'virbr-pq25', 'subnet': '192.168.201.0/24',
          'host_ip': '192.168.201.1', 'service_ip': '192.168.201.10', 'guest_ip': '192.168.201.101',
          'candidate_port': 18090, 'artifact_port': 18082}


class ProbeError(ValueError):
    """任何未知事实仍须保留安全租约和现场，不能当预期失败通过。"""
    retain_safety_lease = True


def need(ok, message):
    if not ok:
        raise ProbeError(message)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def canonical(value):
    return digest(json.dumps(value, sort_keys=True, separators=(',', ':')).encode())


def sha_ok(value):
    return isinstance(value, str) and re.fullmatch('[0-9a-f]{64}', value) is not None


def _pairs(pairs):
    result = {}
    for key, value in pairs:
        need(key not in result, 'JSON有重复字段')
        result[key] = value
    return result


def _json(data):
    try:
        return json.loads(data, object_pairs_hook=_pairs)
    except (ValueError, UnicodeError) as error:
        raise ProbeError('原件不是明确JSON') from error


def _bound(ref, *, limit=2 * 1024**2):
    need(type(ref) is dict and set(ref) == {'path', 'bytes', 'sha256'}, '原件引用字段不符')
    need(type(ref['bytes']) is int and 0 <= ref['bytes'] <= limit and sha_ok(ref['sha256']), '原件引用大小或摘要不符')
    need(isinstance(ref['path'], str), '原件路径须明确')
    path = Path(ref['path'])
    need(path.is_absolute() and path.resolve() == path and path.is_relative_to(VM_BASE), '原件不在本轮准确目录')
    need(not any(p.is_symlink() for p in [path, *path.parents]), '原件及祖先不能是符号链接')
    parent = path.parent.stat()
    need(parent.st_uid == 1000 and stat.S_IMODE(parent.st_mode) == 0o700, '原件父目录不私有')
    before = path.lstat()
    need(stat.S_ISREG(before.st_mode) and before.st_uid == 1000 and stat.S_IMODE(before.st_mode) == 0o600
         and before.st_size == ref['bytes'], '原件身份或长度不符')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
        opened = os.fstat(stream.fileno())
        data = stream.read(limit + 1)
        after_open = os.fstat(stream.fileno())
    after = path.lstat()
    identity = lambda st: (st.st_dev, st.st_ino, st.st_size, st.st_mtime_ns, st.st_uid, st.st_mode)
    need(identity(before) == identity(opened) == identity(after_open) == identity(after)
         and len(data) == ref['bytes'] and digest(data) == ref['sha256'], '读取期间原件变化')
    return data


class _Originals:
    def __init__(self):
        self.refs = {}

    def read(self, ref):
        data = _bound(ref)
        self.refs[canonical(ref)] = copy.deepcopy(ref)
        return data

    def recheck(self):
        for ref in self.refs.values():
            _bound(ref)


def _validate(contract):
    keys = {'schema', 'scope', 'owner', 'target', 'bindings', 'checkers', 'capture_contract', 'task_policy', 'max_seconds', 'operations'}
    need(type(contract) is dict and set(contract) == keys and contract['schema'] == 'lober-cloud-no-task-probe-v1'
         and contract['scope'] == 'NO_TASK' and contract['task_policy'] == 'TOTAL_TASKS_ZERO', '无任务方案字段不符')
    owner, target = contract['owner'], contract['target']
    need(type(owner) is dict and set(owner) == {'purpose', 'run_id', 'target_uuid'} and owner['purpose'] == PURPOSE
         and isinstance(owner['run_id'], str) and re.fullmatch('[a-z0-9][a-z0-9-]{2,63}', owner['run_id'])
         and owner['target_uuid'] == TARGET['uuid'], '不是已冻结本轮客户owner')
    need(type(target) is dict and set(target) == set(TARGET), '目标完整字段不符')
    expected = dict(TARGET)
    if target['disk_path'] != TARGET['disk_path']:
        generation = re.fullmatch(re.escape(HOST_BASE) + r'/targets/linux01-g([0-9]{2})/system\.qcow2', str(target['disk_path']))
        need(generation is not None and int(generation[1]) > 0, '新盘代际未明确')
        expected.update(disk_path=target['disk_path'], disk_serial='PQ25_LINUX01_G' + generation[1])
    need(target == expected, '客户目标与已批准准确身份不符')
    bindings = contract['bindings']
    need(type(bindings) is dict and set(bindings) == {'product_sha', 'test_sha', 'binary_sha256', 'runtime_record_sha256',
                                                    'instance_record_sha256', 'target_contract_sha256'}, '缺少版本运行实例绑定')
    for key, value in bindings.items():
        need(isinstance(value, str) and re.fullmatch('[0-9a-f]{' + ('40' if key in ('product_sha', 'test_sha') else '64') + '}', value), '版本或运行摘要不符')
    checkers = contract['checkers']
    need(type(checkers) is dict and set(checkers) == {'probe_sha256', 'parser_sha256', 'capture_session_sha256'}
         and all(sha_ok(v) for v in checkers.values()), '缺少源检查器版本')
    import cloud_packet_capture as parser
    import cloud_capture_session as session
    for key, source in (('probe_sha256', __file__), ('parser_sha256', parser.__file__), ('capture_session_sha256', session.__file__)):
        need(digest(Path(source).read_bytes()) == checkers[key], '检查器源码与冻结值不同')
    need(type(contract['max_seconds']) is int and 1 <= contract['max_seconds'] <= 180, '短流程时间预算无效')
    operations = contract['operations']
    need(type(operations) is dict and set(operations) == {'start_source_sha256', 'stop_source_sha256'}
         and all(sha_ok(v) for v in operations.values()), '准确启停源码未冻结')
    frozen = contract['capture_contract']
    need(type(frozen) is dict and set(frozen) == {'schema', 'host', 'bridge', 'network', 'targets', 'tftp_port_range', 'max_bytes', 'max_seconds', 'tcp_reset_policy'}, '完整采集合同字段不符')
    need((frozen['schema'], frozen['host'], frozen['bridge'], frozen['network']) ==
         ('lober-cloud-capture-session-v1', target['host'], target['bridge'], target['network'])
         and frozen['tftp_port_range'] == [40000, 40010] and frozen['tcp_reset_policy'] in ('no-rst', 'rfc5961-exact-rcv-nxt'), '采集位置或策略不符')
    need(type(frozen['max_bytes']) is int and 24 <= frozen['max_bytes'] <= 64 * 1024**2
         and type(frozen['max_seconds']) is int and 1 <= frozen['max_seconds'] <= contract['max_seconds'], '采集预算越界')
    need(type(frozen['targets']) is dict and set(frozen['targets']) == {target['uuid']}, '无任务短流程只准准确单客户')
    row = frozen['targets'][target['uuid']]
    need(type(row) is dict and set(row) == {'packet_mode', 'target_sha256', 'packet_contract', 'boot_chain'}
         and row['packet_mode'] == 'NO_TASK' and row['target_sha256'] == canonical(target), '采集模式或准确目标不符')
    packet = row['packet_contract']
    need(type(packet) is dict and not set(packet).intersection({'pcap_sha256', 'pcap_size', 'capture_start_ns', 'capture_end_ns'}), '事前合同不能自填事后采集身份')
    parser.validate_no_task_contract(dict(packet, pcap_sha256='0' * 64, pcap_size=24, capture_start_ns=0, capture_end_ns=1))
    need((packet['client_sn'], packet['client_mac'], packet['client_ip'], packet['boot_server_ip'], packet['service_ip'], packet['service_mac'], packet['service_port']) ==
         (target['sn'], target['mac'], target['guest_ip'], target['host_ip'], target['service_ip'], target['service_mac'], target['candidate_port']), '抓包地址或SN不属于准确客户')
    chain = row['boot_chain']
    need(type(chain) is dict and set(chain) == {'firmware_sha256', 'dhcp_config_sha256', 'user_class_sha256', 'boot_file', 'required_tftp'}
         and all(sha_ok(chain[k]) for k in ('firmware_sha256', 'dhcp_config_sha256', 'user_class_sha256'))
         and chain['required_tftp'] == sorted(packet['tftp']) and chain['boot_file'] in packet['tftp'], '真实引导分支未事前冻结')


def prepare_no_task_probe(contract):
    """只生成待核准备摘要；没有命令执行、网络请求或开门布尔。"""
    contract = copy.deepcopy(contract)
    _validate(contract)
    return {'scope': 'NO_TASK_PROBE_PREPARATION_ONLY', 'status': 'PROPOSAL_ONLY', 'contract_sha256': canonical(contract),
            'target_sha256': canonical(contract['target']), 'executes_mutation': False,
            'short_failure_evidence_verified': False, 'root_verified': False,
            'steps': ['API_TOTAL_ZERO_AND_DISK_ZERO_BEFORE', 'CAPTURE_START_BEFORE_CLIENT', 'REAL_NETWORK_BOOT_GET',
                      'EXACT_CLIENT_STOP_AND_QEMU_GONE', 'CAPTURE_FREEZE', 'DISK_ZERO_AND_API_TOTAL_ZERO_AFTER', 'ROOT_REVIEW']}


def _context(value, contract, schema):
    need(type(value) is dict and value.get('schema') == schema and value.get('context') == {
        'owner': contract['owner'], 'target_sha256': canonical(contract['target']), 'bindings': contract['bindings']}, '原件运行/目标上下文不符')


def _interval(value):
    start, finish = value.get('started_ns'), value.get('finished_ns')
    need(type(start) is int and type(finish) is int and 0 <= start < finish, '原件时窗不完整')
    return start, finish


def _api(value, contract, read):
    _context(value, contract, 'lober-no-task-api-v1')
    need(set(value) == {'schema', 'context', 'started_ns', 'finished_ns', 'requests'}, 'API记录字段不完整')
    interval = _interval(value)
    paths = ['/console/v1/tasks?page=1&page_size=100',
             '/console/v1/tasks?host_sn=' + contract['target']['sn'] + '&page=1&page_size=100']
    rows = value.get('requests')
    need(type(rows) is list and len(rows) == 2, '缺少全局及准确SN的原始API')
    for row, path in zip(rows, paths):
        need(type(row) is dict and set(row) == {'method', 'path', 'status', 'body'} and row['method'] == 'GET'
             and row['path'] == path and type(row['status']) is int and row['status'] == 200, '无任务API方法/URI/状态不符')
        body = _json(read(row['body']))
        need(type(body) is dict and set(body) == {'code', 'data', 'message'} and type(body['code']) is int and body['code'] == 0
             and body['message'] == '', '无任务API原始envelope不成功')
        data = body['data']
        need(type(data) is dict and set(data) == {'items', 'next', 'total'} and data['items'] == [] and data['next'] == ''
             and type(data['total']) is int and data['total'] == 0, '私有实例仍有任务或空列表不是完整第一页')
    return interval


def _xml(raw, target):
    need(b'<!' not in raw and len(raw) <= 1024**2, '客户XML包含不支持声明')
    try:
        root = ET.fromstring(raw)
    except ET.ParseError as error:
        raise ProbeError('客户XML不完整') from error
    need(root.tag == 'domain' and root.get('type') == 'kvm' and root.findtext('uuid') == target['uuid']
         and root.findtext('name') == target['name'] and root.findtext("./sysinfo/system/entry[@name='serial']") == target['sn'], 'XML不是准确客户')
    os_type = root.find('./os/type'); memory = root.find('memory')
    need(os_type is not None and os_type.text == 'hvm' and os_type.get('arch') == 'x86_64'
         and root.find('./os/loader') is None and root.find('./os/nvram') is None and not root.findall('./os/boot')
         and root.findtext('vcpu') == '2' and memory is not None
         and (memory.get('unit'), memory.text) in (('MiB', '4096'), ('KiB', '4194304')), '客户不是BIOS准确规模')
    disks, interfaces = root.findall('./devices/disk'), root.findall('./devices/interface')
    need(len(disks) == len(interfaces) == 1, '客户有额外盘或接口')
    disk, interface = disks[0], interfaces[0]
    disk_source, net_source = disk.find('source'), interface.find('source')
    source_attrs = dict(disk_source.attrib) if disk_source is not None else {}
    if 'index' in source_attrs:
        need(source_attrs.pop('index').isdigit(), '盘内部索引无效')
    need(source_attrs == {'file': target['disk_path']}, '盘source有额外设备或错误路径')
    net_attrs = dict(net_source.attrib) if net_source is not None else {}
    if 'bridge' in net_attrs:
        need(net_attrs.pop('bridge') == target['bridge'], '实际桥不属于本轮')
    if 'portid' in net_attrs:
        try:
            need(str(uuid.UUID(net_attrs.pop('portid'))) is not None, '网络端口UUID无效')
        except (ValueError, TypeError):
            raise ProbeError('网络端口UUID无效') from None
    need(net_attrs == {'network': target['network']}, '网络source有额外来源')
    def attr(node, path, key):
        found = node.find(path)
        return found.get(key) if found is not None else None
    need(disk.attrib == {'type': 'file', 'device': 'disk'} and attr(disk, 'source', 'file') == target['disk_path']
         and disk.findtext('serial') == target['disk_serial'] and attr(disk, 'driver', 'type') == 'qcow2'
         and attr(disk, 'target', 'dev') == 'vda' and attr(disk, 'target', 'bus') == 'virtio'
         and attr(disk, 'boot', 'order') == '2' and interface.get('type') == 'network'
         and attr(interface, 'source', 'network') == target['network'] and attr(interface, 'mac', 'address') == target['mac']
         and attr(interface, 'model', 'type') == 'e1000' and attr(interface, 'boot', 'order') == '1', '客户盘路径/序列或网络优先不符')


def _disk(value, contract, read):
    _context(value, contract, 'lober-no-task-disk-v1')
    need(set(value) == {'schema', 'context', 'at_ns', 'domain_state', 'source_identity', 'owner', 'domain_xml', 'info', 'map'}, '盘记录字段不完整')
    target = contract['target']
    need(value.get('domain_state') == 'shut off' and type(value.get('at_ns')) is int and value['at_ns'] >= 0, '盘证据不是停机后取得')
    need(_json(read(value['owner'])) == contract['owner'], '盘owner原件不符')
    _xml(read(value['domain_xml']), target)
    source = value.get('source_identity')
    need(type(source) is dict and set(source) == {'device', 'inode'} and all(type(v) is int and v >= 0 for v in source.values()), '缺少源盘设备/索引身份')
    info, extents = _json(read(value['info'])), _json(read(value['map']))
    need(type(info) is dict and info.get('filename') == target['disk_path'] and info.get('format') == 'qcow2'
         and type(info.get('virtual-size')) is int and info['virtual-size'] == target['disk_bytes']
         and not any(info.get(k) for k in ('backing-filename', 'full-backing-filename', 'backing-filename-format', 'encrypted', 'snapshots')), '盘大小/路径/格式或backing不符')
    need(type(extents) is list and 1 <= len(extents) <= 32768, '缺少有界完整盘map')
    cursor = 0
    for extent in extents:
        need(type(extent) is dict and type(extent.get('start')) is int and extent['start'] == cursor
             and type(extent.get('length')) is int and extent['length'] > 0 and extent.get('zero') is True
             and extent.get('data') is False and extent.get('depth', 0) == 0, '盘map有缺口/重叠/非零/下层数据')
        cursor += extent['length']
        need(cursor <= target['disk_bytes'], '盘map超出虚拟盘')
    need(cursor == target['disk_bytes'], '盘map没有覆盖全部16GiB')
    return value['at_ns'], source


def _action(value, contract, action, read):
    _context(value, contract, 'lober-no-task-action-v1')
    need(set(value) == {'schema', 'context', 'action', 'started_ns', 'finished_ns', 'command'}, '启停记录字段不完整')
    need(value.get('action') == action, '启停动作不符')
    interval = _interval(value); command = value.get('command')
    need(type(command) is dict and set(command) == {'source', 'stdout', 'stderr', 'exit', 'timed_out'}
         and type(command['exit']) is int and command['exit'] == 0 and command['timed_out'] is False, '启停命令失败、超时或结果未知')
    need(digest(read(command['source'])) == contract['operations'][action + '_source_sha256'] and read(command['stderr']) == b'', '准确启停源码或原始错误输出不符')
    facts = _json(read(command['stdout']))
    need(type(facts) is dict and facts.get('owner') == contract['owner'] and facts.get('target') == contract['target'], '启停原始结果不属于准确客户')
    if action == 'start':
        need(facts.get('started') is True and facts.get('state') == 'running' and facts.get('network_boot_first') is True, '真实网络启动没有确认')
    else:
        need(facts.get('client_stopped') is True and facts.get('state') == 'shut off'
             and facts.get('failed_disks_preserved') is True and facts.get('qemu_gone') is True, '准确客户停写或原QEMU消失未知')
    return interval


def verify_no_task_probe(receipt, contract, *, anchor):
    """只核已冻结的原件；根独立核准前永远不开正式任务前置。"""
    need((socket.gethostname(), os.geteuid()) == ('lober', 1000), '只能在lober VM核原件')
    receipt, contract, anchor = copy.deepcopy((receipt, contract, anchor))
    need(type(anchor) is dict and set(anchor) == {'contract_sha256', 'receipt_sha256'}
         and all(sha_ok(v) for v in anchor.values()) and canonical(contract) == anchor['contract_sha256']
         and canonical(receipt) == anchor['receipt_sha256'], '缺少独立冻结合同/回执锚点')
    _validate(contract)
    keys = {'schema', 'owner', 'contract_sha256', 'status', 'failure_kind', 'synthetic', 'root_verified',
            'no_task_created', 'client_stopped', 'failed_disks_preserved', 'records'}
    need(type(receipt) is dict and set(receipt) == keys and receipt['schema'] == 'lober-cloud-no-task-probe-receipt-v1'
         and receipt['owner'] == contract['owner'] and receipt['contract_sha256'] == anchor['contract_sha256']
         and receipt['status'] == 'EXPECTED_FAILURE' and receipt['failure_kind'] == FAILURE_KIND
         and receipt['synthetic'] is False and receipt['root_verified'] is False
         and all(receipt[k] is True for k in ('no_task_created', 'client_stopped', 'failed_disks_preserved')), '不是可核验的真实预期失败原件')
    refs = receipt['records']
    need(type(refs) is dict and set(refs) == RECORDS, '原件集合不完整')
    originals = _Originals()
    records = {key: _json(originals.read(ref)) for key, ref in refs.items()}
    for key in ('runtime_record', 'instance_record'):
        need(refs[key]['sha256'] == contract['bindings'][key + '_sha256'], '原始运行/实例记录不是冻结实例')
    resource = records['target_contract']
    need(type(resource) is dict and canonical(resource) == contract['bindings']['target_contract_sha256']
         and resource.get('schema') == 'lober-cloud-target-resource-v1' and resource.get('owner') == contract['owner']
         and resource.get('target') == contract['target'] and resource.get('executes_mutation') is False
         and resource.get('failed_disk_delete_or_reuse_allowed') is False, '根核准完整目标资源合同不符')
    review = resource.get('review')
    need(type(review) is dict and review.get('approved') is True and isinstance(review.get('root_reviewer'), str)
         and isinstance(review.get('independent_reviewer'), str) and review['root_reviewer'] and review['independent_reviewer']
         and review['root_reviewer'] != review['independent_reviewer'], '目标资源未独立核准')
    if contract['target']['disk_path'] != TARGET['disk_path']:
        need(TARGET['disk_path'] in resource.get('preserve_failed_disks', [])
             and contract['target']['disk_path'] not in resource['preserve_failed_disks'], '新盘代际必须保留旧失败盘')
    before_api, after_api = _api(records['api_before'], contract, originals.read), _api(records['api_after'], contract, originals.read)
    before_disk, after_disk = _disk(records['disk_before'], contract, originals.read), _disk(records['disk_after'], contract, originals.read)
    need(before_disk[1] == after_disk[1], '前后不是同一源盘')
    start, stop = _action(records['client_start'], contract, 'start', originals.read), _action(records['client_stop'], contract, 'stop', originals.read)
    capture, capture_anchor = records['capture_receipt'], records['capture_anchor']
    need(type(capture_anchor) is dict and capture_anchor == {'contract_sha256': canonical(contract['capture_contract']),
         'target_sha256': canonical(contract['target']), 'receipt_sha256': canonical(capture)}, '采集独立锚点或原回执不符')
    import cloud_capture_session as session
    packets = session.analyze_session(capture, contract['capture_contract'], contract['target'], anchor=capture_anchor)
    packet_contract = contract['capture_contract']['targets'][contract['target']['uuid']]['packet_contract']
    need(packets.get('scope') == 'NO_TASK_PACKET_EVIDENCE_ONLY' and type(packets.get('boot_exchange_count')) is int
         and packets['boot_exchange_count'] == len(packets.get('http', []))
         and 1 <= packets['boot_exchange_count'] <= packet_contract['max_boot_exchanges'], '采集没有经过有界显式无任务解析')
    expected = packet_contract['http'][0]
    for response in packets['http']:
        need(response['method'] == 'GET' and response['response_status'] == 200 and response['callback'] is False
             and all(response[key] == expected[key] for key in ('target_sha256', 'request_body_sha256', 'response_body_sha256')),
             '无任务证据出现不同于冻结boot目标/正文的交换')
        need(start[0] < response['request']['first_ns'] <= response['response']['first_complete_ns'] <= stop[0],
             '每次真实boot交换都须在启动后、停止前完成')
    need(max(before_api[1], before_disk[0]) <= capture['capture_start_ns'] <= start[0] < start[1] <= stop[0] < stop[1]
         and max(row['response']['first_complete_ns'] for row in packets['http']) <= capture['capture_end_ns'] <= min(after_disk[0], after_api[0])
         and stop[1] <= capture['capture_end_ns']
         and stop[1] <= min(after_disk[0], after_api[0]) and stop[1] - start[0] <= contract['max_seconds'] * 10**9, '实际启停/包/API/盘原件时序不成立')
    # 再读全部顶层原件与源码；检查不能被并行替换文件救活。
    originals.recheck()
    _bound({'path': capture['path'], 'bytes': capture['size'], 'sha256': capture['sha256']}, limit=64 * 1024**2)
    _validate(contract)
    return {'scope': 'NO_TASK_PROBE_EVIDENCE_ONLY', 'status': 'ROOT_REVIEW_REQUIRED', 'failure_kind': FAILURE_KIND,
            'contract_sha256': anchor['contract_sha256'], 'receipt_sha256': anchor['receipt_sha256'],
            'target_sha256': canonical(contract['target']), 'capture_sha256': packets['capture_sha256'],
            'boot_exchange_count': packets['boot_exchange_count'],
            'record_sha256': {key: ref['sha256'] for key, ref in refs.items()},
            'short_failure_evidence_verified': False, 'root_verified': False, 'executes_mutation': False}
