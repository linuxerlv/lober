"""复核 4 GiB lobervm 的四次实际装机及各自的重复启动原件。"""
import hashlib
import json
from pathlib import Path
import re
import stat
import uuid


SCHEMA = 'lober-isolated-kvm-install-v1'
CASES = ('parallel-1', 'parallel-2', 'serial-1', 'serial-2')
FILES = ('identity.json', 'terminal.json', 'repeat-terminal.json', 'guest.log', 'repeat-guest.log',
         'boot.pcap', 'repeat.pcap', 'repeat-qmp.log')
SHARED_FILES = ('shared-service-start.json', 'shared-service-end.json', 'shared-tasks.json')
IDENTITY = ('run_id', 'product_sha', 'test_sha', 'binary_sha256')
SCRIPT = (b'#!ipxe\nisset ${smbios/serial} || exit\nchain http://192.0.2.10:18090/v1/boot?'
          b'sn=${smbios/serial}&arch=x86_64&firmware=bios || exit\n')
FALLBACK = b'#!ipxe\nexit || sanboot --no-describe --drive 0x80\n'


def require(condition, message):
    if not condition:
        raise ValueError(message)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def private_bytes(root, name, limit):
    path = root / name
    require(path.resolve(strict=True) == path and path.is_relative_to(root), '原件必须位于本轮私有目录')
    with path.open('rb') as stream:
        mode = path.stat().st_mode
        require(stat.S_ISREG(mode) and not mode & 0o077, '原件必须是私有文件')
        data = stream.read(limit + 1)
    require(0 < len(data) <= limit, '原件为空或超出大小限制')
    return data


def check_intervals(parallel, serial):
    require(len(parallel) == len(serial) == 2, '需要两次并行和两次串行')
    require(all(start < end for start, end in parallel + serial), '装机起止时间不合理')
    require(min(end for _, end in parallel) > max(start for start, _ in parallel), '并行装机时间没有重叠')
    require(max(end for _, end in parallel) <= serial[0][0] and serial[0][1] <= serial[1][0],
            '串行装机没有按顺序执行')


def check_terminal(first, repeat, serial):
    require(first == repeat and first['state'] == 'done' and first['serial'] == serial,
            '重复启动改变了任务终态或来宾不符')
    task = first['task']
    require(re.fullmatch(r'[A-Za-z0-9_-]{1,80}', task), '任务身份无效')
    events = first['events']
    sequences = [e['seq'] for e in events]
    require(sequences and sequences[0] > 0 and sequences == sorted(set(sequences))
            and all(e['taskID'] == task for e in events), '任务事件无序或来自不同任务')
    times = [[e['at'] for e in events if e['kind'] == kind]
             for kind in ('machine.installing', 'machine.init_start', 'machine.ready')]
    require(all(len(rows) == 1 and type(rows[0]) is int for rows in times), '缺少唯一的安装、首启或就绪事件')
    start, init, ready = (rows[0] for rows in times)
    require(start < init <= ready, '安装、首启、就绪顺序不符')
    return start, ready


def check_shared_tasks(tasks, expected):
    require(len(tasks) == len(expected) == 4 and all(task['state'] == 'done' for task in tasks)
            and {task['id']: task['hostSN'] for task in tasks} == expected,
            '同一个候选服务必须返回四台来宾各自完成的任务')


def check_shared_image(before, after, identity):
    image = identity.get("image_id")
    if image is None and "image_id" not in before and "image_id" not in after:
        return
    require(isinstance(image, str) and re.fullmatch(r"sha256:[0-9a-f]{64}", image)
            and before.get("image_id") == after.get("image_id") == image
            and before.get("container_id") == after.get("container_id")
            and re.fullmatch(r"[0-9a-f]{64}", before.get("container_id", "")),
            "装机期间共用容器或候选镜像不一致")


def guest(data):
    lines = data.decode().splitlines()
    properties = dict(line.split('=', 1) for line in lines[:-3] if '=' in line)
    root, machine, boot = lines[-3:]
    require(properties['ID'].strip('"') == 'debian' and properties['VERSION_ID'].strip('"') == '13'
            and re.fullmatch(r'/dev/vda[0-9]+', root) and re.fullmatch(r'[0-9a-f]{32}', machine)
            and str(uuid.UUID(boot)) == boot, '来宾系统、磁盘或启动身份不符')
    return root, machine, boot


def check_capture(parser, data, identity, task, repeat):
    contract = {'capture_start_ns': 0, 'capture_end_ns': (1 << 64) - 1,
                'client_ip': '192.0.2.21', 'client_mac': identity['mac'],
                'boot_server_ip': '192.0.2.10', 'service_ip': '192.0.2.10', 'service_port': 18090,
                'client_sn': identity['serial'], 'task_id': task, 'pcap_sha256': digest(data),
                'tftp': {'probe.ipxe': {'size': len(SCRIPT), 'sha256': digest(SCRIPT)}}}
    packets, _ = parser.parse_pcap(data, contract)
    server_macs = {p['src_mac'] for p in packets if p['src_ip'] == contract['boot_server_ip']
                   and p['protocol'] == 17 and p['sport'] == 67 and p['dport'] == 68}
    require(len(server_macs) == 1, '抓包缺少唯一的 DHCP 来源')
    contract['boot_server_mac'] = contract['service_mac'] = server_macs.pop()
    rrq = next(p['index'] for p in packets if p['src_mac'] == identity['mac'] and p['protocol'] == 17
               and p['dport'] == 69 and p['data'].startswith(b'\0\1probe.ipxe\0'))
    lease = parser.prove_dhcp([p for p in packets if p['index'] < rrq], contract)
    transfers = parser.prove_tftp(packets, contract, lease)
    exchanges = parser.http_exchanges(parser.tcp_flows(packets, contract), contract, no_task=repeat)
    target = ('/v1/boot?sn=' + identity['serial'] + '&arch=x86_64&firmware=bios').encode()
    boot = [public for public, request in exchanges if request['target'] == target and request['method'] == 'GET'
            and public['response_status'] == 200]
    require(boot and min(boot[0]['request']['packet_indices']) > max(transfers[0]['packet_indices']),
            '网络引导未在 DHCP 和完整 TFTP 传输后发生')
    if not repeat:
        require(boot[0]['response_body_sha256'] != digest(FALLBACK), '首次网络引导没有进入安装')
        prefix = ('/v1/answer/' + task + '/').encode()
        require(any(request['target'].startswith(prefix) and public['response_status'] == 200
                    for public, request in exchanges), '缺少本任务成功获取装机应答的原始抓包')
        require(any(request['target'].endswith(b'/ready') and public['callback'] and public['response_status'] == 200
                    for public, request in exchanges), '缺少本任务成功回报就绪的原始抓包')
    return packets[0]['time_ns']//10**9, packets[-1]['time_ns']//10**9


def verify_report(report, identity, root, parser):
    root = Path(root).resolve(strict=True)
    require(report['schema'] == SCHEMA and report['synthetic'] is False and report['status'] == 'PASS'
            and all(report[key] == identity[key] for key in (*IDENTITY, 'run_identity_sha256'))
            and report.get('image_id') == identity.get('image_id'),
            '原件与冻结候选不一致')
    require(set(report['cases']) == set(CASES), '四次装机原件不齐全')
    shared = {file: private_bytes(root, file, 1 << 20) for file in SHARED_FILES}
    require(report['shared'] == {file: digest(value) for file, value in shared.items()}, '共享候选原件校验值不符')
    before, after, tasks = (json.loads(shared[file]) for file in SHARED_FILES)
    check_shared_image(before, after, identity)
    require(before == after and all(before[key] == identity[key] for key in IDENTITY)
            and type(before['pid']) is int and before['pid'] > 1 and type(before['start_ticks']) is int
            and before['start_ticks'] > 0 and re.fullmatch(r'pxe_probe_[a-z0-9]{8}', before['schema']),
            '装机期间的共享候选实例身份不一致')
    distinct = {name: set() for name in ('serial', 'mac', 'uuid', 'task', 'machine')}
    expected_tasks = {}
    intervals = []
    for index, name in enumerate(CASES, start=1):
        refs = report['cases'][name]
        require(set(refs) == set(FILES), '单台原件不齐全')
        data = {file: private_bytes(root, f'{name}/{file}', 64 << 20 if file.endswith('.pcap') else 1 << 20)
                for file in FILES}
        require(all(digest(data[file]) == refs[file] for file in FILES), '单台原件校验值不符')
        machine = json.loads(data['identity.json'])
        require(all(machine[key] == identity[key] for key in IDENTITY) and machine['case'] == name
                and machine['memory_mib'] == 1024 and machine['serial'] == 'LOBER-KVM-' + name.upper()
                and machine['mac'] == f'52:54:00:42:12:{32 + index:02x}' and str(uuid.UUID(machine['uuid'])) == machine['uuid'],
                '装机用例身份不符')
        first, repeat = (guest(data[file]) for file in ('guest.log', 'repeat-guest.log'))
        require(first[:2] == repeat[:2] and first[2] != repeat[2], '重复启动未从原系统盘进入新启动实例')
        qmp = [json.loads(line) for line in data['repeat-qmp.log'].splitlines()]
        require(qmp == [{'return': {}, 'id': 'qmp_capabilities'}, {'return': {}, 'id': 'system_reset'}],
                '缺少虚拟机重启确认')
        terminal = json.loads(data['terminal.json'])
        expected_tasks[terminal['task']] = machine['serial']
        interval = check_terminal(terminal, json.loads(data['repeat-terminal.json']), machine['serial'])
        for key, value in {**{key: machine[key] for key in ('serial', 'mac', 'uuid')},
                           'task': terminal['task'], 'machine': first[1]}.items():
            require(value not in distinct[key], '重复使用来宾、系统或装机任务')
            distinct[key].add(value)
        start, end = check_capture(parser, data['boot.pcap'], machine, terminal['task'], False)
        repeat_start, _ = check_capture(parser, data['repeat.pcap'], machine, terminal['task'], True)
        require(start <= interval[0] < interval[1] <= end <= repeat_start, '装机事件与两次抓包时间不一致')
        intervals.append(interval)
    check_intervals(intervals[:2], intervals[2:])
    check_shared_tasks(tasks, expected_tasks)


def collect_report(identity, root, parser):
    root = Path(root).resolve(strict=True)
    cases = {name: {file: digest(private_bytes(root, f'{name}/{file}', 64 << 20 if file.endswith('.pcap') else 1 << 20))
                    for file in FILES} for name in CASES}
    shared = {file: digest(private_bytes(root, file, 1 << 20)) for file in SHARED_FILES}
    report = {'schema': SCHEMA, 'synthetic': False, 'status': 'PASS', **identity, 'cases': cases, 'shared': shared}
    verify_report(report, identity, root, parser)
    return report
