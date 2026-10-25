"""云客户机验收；默认只读或输出待审操作，不把检查器通过当作装机通过。

真实变更脚本需逐份审核。本入口不会代替审核执行外部写盘或启动。
私有配置和输出目录都应由 dev 独占。所有入口必须在 lober VM 运行。
"""
import argparse
import base64
import hashlib
import ipaddress
import inspect
import selectors
import json
import os
from pathlib import Path
import re
import signal
import shutil
import socket
import ssl
import stat
import struct
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request
import urllib.parse
import uuid
import xml.etree.ElementTree as ET


PURPOSE = 'production-qualification-20261025'
BASE = '/var/lib/libvirt/images/lober-pq-20261025'
VM_BASE = '/home/dev/lober-production-qualification-20261025'
TARGET = 'lober-pq25-linux01'
NETWORK = 'lober-pq25'
BRIDGE = 'virbr-pq25'
SN = 'PQ25L01'
MAC = '52:54:00:25:10:01'
SERVICE_MAC = '52:54:00:25:10:10'
SERIAL = 'PQ25_LINUX01_DISK'
DATA_SERIAL = 'PQ25_MEDIA01_DISK'
SERVICE_UUID = '895a44e1-8abd-4463-bbf7-fba8a743ed59'
GIB = 1024 ** 3
SUBNET = ipaddress.ip_network('192.168.201.0/24')
COMMAND_RECORDS = None
ACTIVE_CHILDREN = {}
ACTIVE_CAPTURES = []
QUALIFICATION_CONTRACT_VERSION = 2


def require(condition, message):
    if not condition:
        raise ValueError(message)


def sha256(path):
    digest = hashlib.sha256()
    with Path(path).open('rb') as source:
        for block in iter(lambda: source.read(1024 * 1024), b''):
            digest.update(block)
    return digest.hexdigest()


def owner_valid(owner):
    require(set(owner) == {'purpose', 'run_id', 'target_uuid'}, 'owner 字段不完整或有未知字段')
    require(owner['purpose'] == PURPOSE, '非本轮 owner')
    require(re.fullmatch(r'[a-z0-9][a-z0-9-]{2,63}', owner['run_id']), 'run_id 无效')
    require(str(uuid.UUID(owner['target_uuid'])) == owner['target_uuid'], '目标 UUID 无效')
    require(owner['target_uuid'] not in [SERVICE_UUID, '33d609aa-8a55-4855-abb1-89634a403fe1'], '禁止旧 VM')


def target_spec(owner):
    return {'host': 'ser866667880745', 'name': TARGET, 'uuid': owner['target_uuid'], 'sn': SN,
        'mac': MAC, 'cpus': 2, 'memory_mib': 4096, 'disk_path': BASE + '/linux01.qcow2',
        'disk_serial': SERIAL, 'disk_bytes': 16 * GIB, 'data_path': BASE + '/media01.qcow2',
        'data_serial': DATA_SERIAL, 'data_bytes': 16 * GIB, 'service_uuid': SERVICE_UUID,
        'service_mac': SERVICE_MAC, 'network': NETWORK, 'bridge': BRIDGE,
        'subnet': str(SUBNET), 'host_ip': '192.168.201.1', 'service_ip': '192.168.201.10',
        'guest_ip': '192.168.201.101', 'candidate_port': 18090, 'artifact_port': 18082}


def resolve_target(owner, target=None):
    """明确消费冻结目标；旧调用仍只认首块原盘，不改模块常量冒充第二机。"""
    owner_valid(owner)
    legacy = target_spec(owner)
    if target is None or target == legacy:
        return legacy
    require(set(target) == set(legacy), '目标合同有缺失或未知字段')
    require(target['uuid'] == owner['target_uuid'], '目标合同 UUID 与 owner 不符')
    match = re.fullmatch(r'lober-pq25-linux(01|02)', target['name'])
    require(match is not None, '只允许本轮明确的两个客户名字')
    number = match[1]
    generation = re.fullmatch(re.escape(BASE) + '/targets/linux' + number + r'-g([0-9]{2})/system\.qcow2', target['disk_path'])
    require(generation is not None and int(generation[1]) > 0, '新盘须有独立正代际，不复用首条失败盘')
    expected = {**legacy, 'name': 'lober-pq25-linux' + number, 'uuid': owner['target_uuid'],
        'sn': 'PQ25L' + number, 'mac': '52:54:00:25:10:' + number,
        'disk_path': BASE + '/targets/linux' + number + '-g' + generation[1] + '/system.qcow2',
        'disk_serial': 'PQ25_LINUX' + number + '_G' + generation[1],
        'guest_ip': '192.168.201.' + str(100 + int(number))}
    require(target == expected, '目标合同偏离准确盘/网/序列/规模')
    return dict(target)


def manifest_target(manifest):
    cloud = manifest['cloud']
    target = resolve_target(cloud['owner'], cloud['target'])
    if target != target_spec(cloud['owner']):
        contract = cloud['target_contract']
        require(contract['target'] == target and contract['owner'] == cloud['owner']
                and canonical_sha256(contract) == cloud['target_contract_sha256'], '新目标资源合同未冻结')
        review = contract['review']
        require(review['approved'] is True and review['root_reviewer'] and review['independent_reviewer']
                and review['root_reviewer'] != review['independent_reviewer'], '新目标/盘代际未单独核准')
        require(contract['preserve_failed_disks'] and BASE + '/linux01.qcow2' in contract['preserve_failed_disks']
                and target['disk_path'] not in contract['preserve_failed_disks'], '首条失败盘必须保留')
    return target


def propose_target_contract(owner,target,preserve_failed_disks):
    """只生成待审新盘/目标定义；不创建、不替换旧失败盘。"""
    target=resolve_target(owner,target)
    require(target!=target_spec(owner),'原失败盘不能当新成功盘方案')
    require(BASE+'/linux01.qcow2' in preserve_failed_disks and target['disk_path'] not in preserve_failed_disks,
            '新方案必须保留首条失败盘')
    root=ET.fromstring(definitions(owner)['domain.xml'])
    root.find('name').text=target['name']
    root.find("./sysinfo/system/entry[@name='serial']").text=target['sn']
    root.find('./devices/disk/source').set('file',target['disk_path'])
    root.find('./devices/disk/serial').text=target['disk_serial']
    root.find('./devices/interface/mac').set('address',target['mac'])
    for source in root.findall('./devices/serial/source')+root.findall('./devices/console/source'):
        source.set('path',str(Path(target['disk_path']).parent/'serial.log'))
    return {'schema':'lober-cloud-target-resource-v1','owner':owner,'target':target,
        'preserve_failed_disks':list(preserve_failed_disks),'domain_xml':ET.tostring(root,encoding='unicode'),
        'review':{'approved':False,'root_reviewer':'','independent_reviewer':''},
        'executes_mutation':False,'initial_host_free_bytes':38*GIB,'running_host_reserve_bytes':6*GIB,
        'new_directory_mode':0o755,'private_registration_mode':0o600,'blank_disk_full_map_required':True,
        'network_dhcp_addition':{'mac':target['mac'],'name':target['name'],'ip':target['guest_ip']},
        'existing_domain_policy':'STOP_FOR_SEPARATE_EXACT_REPLACEMENT_REVIEW',
        'failed_disk_delete_or_reuse_allowed':False}


def runtime_owner(manifest):
    value=manifest['cloud'].get('shared_owner',manifest['cloud']['owner'])
    owner_valid(value)
    return value


def check_run_identity(config,manifest):
    common=bound_json(config['run_identity_file'],manifest['run_identity_sha256'])
    for key in ('run_id','product_sha','test_sha','binary_sha256'):
        require(common[key]==manifest[key],'共同run_identity在seed后被替换：'+key)
    target=manifest_target(manifest)
    require(target_in_contract(common['installation_targets'].get(target['uuid']),target),'目标/盘代际未在seed前共同合同中冻结')
    require(common['installation_checkers'].get(str(Path(__file__).resolve()))==sha256(__file__),
            '检查器未在seed前共同合同中冻结')
    for name in ('cloud_guest_identity','cloud_capture_session','cloud_packet_capture','cloud_no_task_probe'):
        path=Path(__file__).with_name(name+'.py')
        require(common['installation_helpers'].get(str(path.resolve()))==sha256(path),'独立helper未在seed前冻结')
    return common


def target_in_contract(frozen,target):
    if isinstance(frozen,dict):return frozen==target
    if not isinstance(frozen,list) or not 1<=len(frozen)<=3:return False
    try:
        for item in frozen:resolve_target({'purpose':PURPOSE,'run_id':'target-contract','target_uuid':target['uuid']},item)
        legacy=target_spec({'target_uuid':target['uuid']})
        return (len({item['disk_path'] for item in frozen})==len(frozen) and target in frozen
            and sum(item!=legacy for item in frozen)<=2
            and all(len({item[key] for item in frozen})==1 for key in ('name','sn','mac','guest_ip')))
    except (KeyError,TypeError,ValueError):return False


def check_disk(value, blank):
    info = value['info']
    require(info.get('format') == 'qcow2' and info.get('virtual-size') == 16 * GIB, '必须是新 16GiB qcow2')
    require(not any(info.get(key) for key in ('backing-filename', 'full-backing-filename', 'backing-filename-format')),
            '磁盘有 backing，不可使用')
    require(not info.get('encrypted') and not info.get('snapshots'), '磁盘含加密或旧快照')
    if blank:
        cursor = 0
        for extent in value['map']:
            require(extent.get('start') == cursor and extent.get('length', 0) > 0, '磁盘 map 有空洞或重叠')
            require(extent.get('zero') is True and extent.get('data') is False,
                    '磁盘已有 data 或不能证明全空')
            cursor += extent['length']
        require(cursor == 16 * GIB, '空白证据没有覆盖整块盘')


def check_target(xml, actual_owner, expected_owner, disk, blank=False, target=None):
    owner_valid(expected_owner)
    spec = resolve_target(expected_owner, target)
    require(actual_owner == expected_owner, '目标 owner 不符')
    root = ET.fromstring(xml)
    require(root.tag == 'domain' and root.get('type') == 'kvm', '不是指定 KVM 客户')
    require(root.findtext('name') == spec['name'] and root.findtext('uuid') == spec['uuid'], '目标名字或 UUID 不符')
    require(root.findtext("./sysinfo/system/entry[@name='serial']") == spec['sn'], '目标 SN 不符')
    os_type = root.find('./os/type')
    require(os_type is not None and os_type.get('arch') == 'x86_64' and os_type.text == 'hvm', '目标架构不符')
    require(root.find('./os/loader') is None and root.find('./os/nvram') is None, '本资格只覆盖 BIOS')
    require(not root.findall('./os/boot'), '不得用全局磁盘启动顺序绕过网络启动')
    require(root.findtext('vcpu') == '2', '客户必须是 2 核')
    memory = root.find('memory')
    require(memory is not None and (memory.get('unit'), memory.text) in [('MiB', '4096'), ('KiB', '4194304')],
            '首条 Debian 客户必须是 4GiB')
    disks = root.findall('./devices/disk')
    require(len(disks) == 1, '只允许一块客户盘，不能夹带旧盘或光驱')
    d = disks[0]
    require(d.attrib == {'type': 'file', 'device': 'disk'}, '客户盘类型不符')
    source = d.find('source')
    attrs = dict(source.attrib) if source is not None else {}
    if 'index' in attrs:
        require(attrs.pop('index').isdigit(), '客户盘内部序号无效')
    require(attrs == {'file': spec['disk_path']}, '客户盘路径不符')
    require(d.find('target') is not None and d.find('target').attrib == {'dev': 'vda', 'bus': 'virtio'}, '客户盘映射不符')
    require(d.findtext('serial') == spec['disk_serial'] and d.find('boot') is not None and d.find('boot').get('order') == '2', '客户盘序列或顺序不符')
    require(d.find('driver') is not None and d.find('driver').get('type') == 'qcow2', '客户盘驱动格式不符')
    interfaces = root.findall('./devices/interface')
    require(len(interfaces) == 1, '客户只能连接本轮新网')
    net = interfaces[0]
    source = net.find('source')
    attrs = dict(source.attrib) if source is not None else {}
    if 'bridge' in attrs:
        require(attrs.pop('bridge') == BRIDGE, '客户实际网桥不符')
    if 'portid' in attrs:
        uuid.UUID(attrs.pop('portid'))
    require(net.get('type') == 'network' and attrs == {'network': NETWORK}, '客户网络不符')
    require(net.find('mac') is not None and net.find('mac').get('address', '').lower() == spec['mac'], '客户 MAC 不符')
    require(net.find('boot') is not None and net.find('boot').get('order') == '1', '必须先网络启动')
    require(net.find('model') is not None and net.find('model').get('type') == 'e1000', 'BIOS PXE 网卡型号不符')
    check_disk(disk, blank)


def check_manifest(value):
    for key in ('product_sha', 'test_sha'):
        require(re.fullmatch(r'[0-9a-f]{40}', value[key]), key + ' 无效')
    cloud = value['cloud']
    owner_valid(cloud['owner'])
    manifest_target(value)
    require(value['run_id'] == cloud['owner']['run_id'], 'manifest run_id 不一致')
    require((cloud['system'], cloud['architecture'], cloud['firmware']) == ('debian', 'x86_64', 'bios'), '本入口只接受首条 Debian BIOS')
    for name in ('binary', 'source_archive'):
        require(sha256(value[name]) == value[name + '_sha256'], name + ' 摘要变化')
    require(bool(value['files']), '缺少源文件摘要')
    source_root = Path(cloud['source_root']).resolve(strict=True)
    for name, digest in value['files'].items():
        path = (source_root / name).resolve(strict=True)
        require(not Path(name).is_absolute() and path.is_relative_to(source_root), '源文件必须在本轮源码目录')
        require(sha256(path) == digest, '候选源文件摘要变化')
    require(sha256(cloud['executor']) == cloud['executor_sha256'], '执行器摘要变化')


def canonical_sha256(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def bound_json(path, expected):
    require(re.fullmatch('[0-9a-f]{64}', expected), '缺少冻结原件摘要')
    value = private_json(path)
    require(sha256(path) == expected, '冻结原件被替换')
    return value


def check_instance_binding(frozen, actual, owner, binary, digest):
    require(frozen['owner'] == owner and actual['owner'] == owner, '实际实例 owner 不符')
    for key in ('pid', 'starttime', 'uid', 'pgrp', 'session', 'vm_boot_id', 'cmdline_sha256',
                'executable_path', 'executable_sha256', 'config_path', 'config_sha256', 'socket',
                'environment_sha256','database_origin_sha256','database'):
        require(actual[key] == frozen[key], '实际运行实例变化：' + key)
    require(actual['pid'] > 1 and actual['pid'] == actual['pgrp'] == actual['session'], '实例没有独占进程组')
    require(actual['executable_path'] == binary and actual['executable_sha256'] == digest, '18090 运行的不是冻结二进制')
    require(actual['socket'] == '192.168.201.10:18090', '实例监听端点不符')


def observe_instance(record, manifest):
    pid = record['pid']
    actual = process_identity(pid)
    proc = Path('/proc') / str(pid)
    actual['session'] = int((proc / 'stat').read_text().rsplit(')', 1)[1].split()[3])
    actual['executable_path'] = os.readlink(proc / 'exe')
    actual['executable_sha256'] = sha256(proc / 'exe')
    configuration = Path(record['config_path'])
    require(configuration.resolve().is_relative_to(Path(VM_BASE) / 'machines'), '实例配置不在本轮目录')
    require(configuration.is_absolute() and all(not parent.is_symlink() for parent in configuration.parents), '实例配置祖先不能为链接')
    actual['owner'] = private_json(configuration.parent / '.owner.json')
    settings = private_json(configuration)
    environment = dict(item.split(b'=',1) for item in (proc/'environ').read_bytes().split(b'\0') if b'=' in item)
    actual['environment_sha256'] = hashlib.sha256((proc/'environ').read_bytes()).hexdigest()
    dsn = environment.get(b'LOBER_PG_DSN',b'').decode() or settings['pg']['dsn']
    origin = bound_json(manifest['cloud']['database_origin_file'],manifest['cloud']['database_origin_sha256'])
    require(origin['owner']==record['owner'] and origin['binary_sha256']==manifest['binary_sha256']
            and origin['config_sha256']==sha256(configuration) and origin['dsn_sha256']==hashlib.sha256(dsn.encode()).hexdigest()
            and re.fullmatch('[0-9a-f]{64}',origin['deployment_receipt_sha256']), '数据库不是根冻结的本轮独占实例')
    launch=bound_json(origin['deployment_receipt_file'],origin['deployment_receipt_sha256'])
    require(launch['root_actor']=='root' and launch['root_record_ref'] and launch['owner']==record['owner']
        and launch['pid']==pid and launch['starttime']==actual['starttime']
        and launch['binary_sha256']==manifest['binary_sha256'] and launch['config_sha256']==sha256(configuration),
        '根启动回执与实际候选进程/配置不同')
    sql="SELECT json_build_object('database',current_database(),'schema',current_schema(),'database_oid',(SELECT oid::text FROM pg_database WHERE datname=current_database()),'server_address',inet_server_addr()::text,'server_port',inet_server_port(),'postmaster_started',pg_postmaster_start_time()::text);"
    database=json.loads(command(['psql','-X','--no-password','--quiet','--tuples-only','--no-align','--set','ON_ERROR_STOP=1','--command',sql],
        timeout=10,env={'PATH':'/usr/bin:/bin','LC_ALL':'C','PGDATABASE':dsn,
                       'PGOPTIONS':'-c default_transaction_read_only=on -c statement_timeout=5000'}))
    require(database==origin['database'] and database['database'].startswith('pq25_')
            and database['database'] not in ('lober','lober_e2e_round6') and database['schema']=='public', '实际数据库连接来源变化')
    actual.update(database=database,database_origin_sha256=manifest['cloud']['database_origin_sha256'])
    actual['config_path'], actual['config_sha256'] = str(configuration), sha256(configuration)
    require(settings['listen'] == '192.168.201.10:18090' and settings['machine']['enabled'] is True,
            '运行实例不是本轮机器服务')
    require(settings['preview']['gateway'] == 'http://192.168.201.10:18090'
            and settings['preview']['artifacts'] == 'http://192.168.201.10:18082', '机器交付指向其他候选或制品服务')
    media_root = Path(VM_BASE) / 'machines/media'
    for name in ('staging', 'artifacts', 'smb', 'tools'):
        require(Path(settings['iso'][name]).resolve().is_relative_to(media_root), '实例存储不是本轮独占数据盘')
    argv = (proc / 'cmdline').read_bytes().rstrip(b'\0').split(b'\0')
    require(argv == [str(manifest['binary']).encode(), b'-config', str(configuration).encode()], '实例启动参数不符')
    listeners = []
    for line in command(['ss', '-H', '-lntp']).splitlines():
        parts = line.split()
        if len(parts) > 3 and parts[3].endswith(':18090'):
            listeners.append((parts[3], re.findall(r'pid=([0-9]+)', line)))
    require(listeners == [('192.168.201.10:18090', [str(pid)])], '18090 的实际监听进程不符')
    actual['socket'] = listeners[0][0]
    require(actual['uid'] == os.geteuid(), '实例进程不是本轮执行用户所有')
    return actual


def verify_instance(config, manifest):
    frozen = bound_json(config['instance_record_file'], manifest['cloud']['instance_record_sha256'])
    actual = observe_instance(frozen, manifest)
    check_instance_binding(frozen, actual, runtime_owner(manifest), manifest['binary'], manifest['binary_sha256'])
    return frozen


def freeze_instance_record(pid, configuration, owner, manifest, output):
    """根在实际启动专用服务后调用；只读真实 /proc 和监听端口，不生成假 PID。"""
    check_manifest(manifest)
    record = {'pid': int(pid), 'config_path': str(Path(configuration).resolve()), 'owner': owner}
    record = observe_instance(record, manifest)
    check_instance_binding(record, record, owner, manifest['binary'], manifest['binary_sha256'])
    write_once(Path(output), json.dumps(record, indent=2) + '\n')
    return record


def import_process(pid):
    proc=Path('/proc')/str(pid)
    fields=(proc/'stat').read_text().rsplit(')',1)[1].split()
    return {**process_identity(pid),'parent':int(fields[1]),'state':fields[0],
            'executable_path':os.readlink(proc/'exe'),'executable_sha256':sha256(proc/'exe')}


def import_topology_process(pid):
    """只读出生时间、父PID、UID等元数据；不读exe/cwd/argv。"""
    proc=Path('/proc')/str(pid)
    first=(proc/'stat').read_text().rsplit(')',1)[1].split()
    uid=proc.stat().st_uid
    second=(proc/'stat').read_text().rsplit(')',1)[1].split()
    require(first[19]==second[19] and proc.stat().st_uid==uid,'进程出生身份在拓扑读取中改变')
    return {'pid':pid,'parent':int(second[1]),'pgrp':int(second[2]),'starttime':second[19],
        'state':second[0],'uid':uid,'vm_boot_id':Path('/proc/sys/kernel/random/boot_id').read_text().strip()}


def import_processes():
    result={}
    for proc in Path('/proc').iterdir():
        if not proc.name.isdigit():continue
        try:
            require(len(result)<32768,'进程拓扑超出有界范围，不能截断')
            result[int(proc.name)]=import_topology_process(int(proc.name))
        except FileNotFoundError:continue
    return result


def send_import_signal(expected,number):
    """固定到内核进程句柄再核身份，避免PID复用窗口误停别的进程。"""
    descriptor=os.pidfd_open(expected['pid'],0)
    try:
        actual=import_process(expected['pid'])
        for key in ('pid','starttime','uid','pgrp','parent','cmdline_sha256','vm_boot_id','executable_path','executable_sha256'):
            require(actual[key]==expected[key],'pidfd固定后导入进程身份变化')
        signal.pidfd_send_signal(descriptor,number)
        if number==signal.SIGSTOP:
            deadline=time.monotonic()+5
            while time.monotonic()<deadline:
                state=import_topology_process(expected['pid'])
                require(all(state[key]==expected[key] for key in ('pid','starttime','uid','vm_boot_id')),
                        '冻结确认期间进程身份改变')
                if state['state'] in ('T','t'):break
                time.sleep(.01)
            else:raise ValueError('准确进程尚未确认冻结')
    finally:os.close(descriptor)


def import_fd_metadata(expected,roots):
    """单PID有界只读补查；只返回本轮目录命中，不输出其它路径或进程正文。"""
    require(type(expected['pid']) is int and expected['pid']>1 and expected['uid']==1000,'只查准确dev进程')
    base=Path(VM_BASE)/'machines/media'
    require(1<=len(roots)<=4 and all(root.is_absolute() and root==root.resolve()
        and root!=base and root.is_relative_to(base) for root in roots),'补证根路径不是本轮准确子目录')
    keys=('pid','starttime','uid','vm_boot_id')
    before=import_topology_process(expected['pid'])
    require(all(before[key]==expected[key] for key in keys),'补证前PID已复用/身份改变')
    proc=Path('/proc')/str(expected['pid']);found=[]
    if before['state']=='Z':
        require([path.name for path in (proc/'task').iterdir()]==[str(expected['pid'])]
            and list((proc/'fd').iterdir())==[],'退出进程仍有线程/FD，不能推断停写')
        after=import_topology_process(expected['pid'])
        require(after['state']=='Z' and all(after[key]==expected[key] for key in keys),'退出身份在补证中改变')
        return {'schema':'lober-import-fd-metadata-v1','state':'PRESENT',
            'identity':{key:expected[key] for key in keys},'holders':[]}
    cwd=Path(os.readlink(proc/'cwd'))
    if any(cwd.is_relative_to(root) for root in roots):found.append({'kind':'cwd'})
    descriptors=list((proc/'fd').iterdir())
    require(len(descriptors)<=4096,'补证FD超过4096，不能截断')
    for fd in descriptors:
        try:
            target=os.readlink(fd)
            flags=next(line.split()[1] for line in (proc/'fdinfo'/fd.name).read_text().splitlines() if line.startswith('flags:'))
            later=next(line.split()[1] for line in (proc/'fdinfo'/fd.name).read_text().splitlines() if line.startswith('flags:'))
            require(os.readlink(fd)==target and later==flags,'补证期间FD被替换')
        except FileNotFoundError:continue
        if int(flags,8)&os.O_ACCMODE and any(Path(target).is_relative_to(root) for root in roots):
            found.append({'kind':'write','fd':int(fd.name)})
    after=import_topology_process(expected['pid'])
    require(all(after[key]==expected[key] for key in keys),'补证后PID已复用/身份改变')
    return {'schema':'lober-import-fd-metadata-v1','state':'PRESENT',
        'identity':{key:expected[key] for key in keys},'holders':found}


def privileged_import_fd_metadata(expected,roots,records):
    require(records is not None,'管理员补证必须保存完整私有原件')
    expected={key:expected[key] for key in ('pid','starttime','uid','vm_boot_id')}
    source='import json,os,pathlib,socket,time\nfrom pathlib import Path\nVM_BASE='+repr(VM_BASE)+'\n'
    source+='\n\n'.join(inspect.getsource(fn) for fn in (require,import_topology_process,import_fd_metadata))+'\n'
    source+='expected='+repr(expected)+'\nroots=[Path(value) for value in '+repr([str(root) for root in roots])+']\n'
    source+='''require(socket.gethostname()=='lober' and os.geteuid()==0,'wrong privileged probe context')
try:
 value=import_fd_metadata(expected,roots)
except FileNotFoundError:
 if not (Path('/proc')/str(expected['pid'])).exists() and Path('/proc/sys/kernel/random/boot_id').read_text().strip()==expected['vm_boot_id']:
  value={'schema':'lober-import-fd-metadata-v1','state':'GONE','identity':expected,'holders':[]}
 else:value={'schema':'lober-import-fd-metadata-v1','state':'UNKNOWN','identity':expected,'error_type':'FileNotFoundError'}
except BaseException as error:
 value={'schema':'lober-import-fd-metadata-v1','state':'UNKNOWN','identity':expected,'error_type':type(error).__name__}
print(json.dumps(value))
'''
    prefix=Path(records)/('proc-probe-'+str(expected['pid'])+'-'+str(time.time_ns()))
    write_once(prefix.with_suffix('.py'),source)
    write_once(prefix.with_suffix('.begin.json'),json.dumps({'identity':expected,'source_sha256':sha256(prefix.with_suffix('.py')),
        'argv':['sudo','-n','/usr/bin/python3','-I','-'],'timeout_seconds':10}))
    ended={'state':'UNKNOWN'}
    try:
        raw=command(['sudo','-n','/usr/bin/python3','-I','-'],source,timeout=10)
        require(len(raw.encode())<=256*1024,'补证输出超出上限')
        write_once(prefix.with_suffix('.stdout.json'),raw)
        value=json.loads(raw)
        require(value.get('schema')=='lober-import-fd-metadata-v1' and value.get('identity')==expected
            and value.get('state') in ('PRESENT','GONE') and type(value.get('holders')) is list,
            '受限进程补证失败/身份改变，不能放锁')
        require(set(value)=={'schema','state','identity','holders'} and len(value['holders'])<=4097
            and (value['state']!='GONE' or value['holders']==[]),'补证输出字段/范围不完整')
        for holder in value['holders']:
            require(type(holder) is dict and (holder=={'kind':'cwd'} or
                set(holder)=={'kind','fd'} and holder['kind']=='write' and type(holder['fd']) is int and holder['fd']>=0),
                '补证写引用字段不完整')
        ended={'state':value['state'],'identity':expected}
        return value
    except BaseException as error:
        ended['error_type']=type(error).__name__;raise
    finally:write_once(prefix.with_suffix('.end.json'),json.dumps(ended))


def import_write_holders(roots,records=None):
    """核专用导入目录没有遗留可写FD/工作目录；不猜别的用户已退出。"""
    found=[]
    for proc in Path('/proc').iterdir():
        if not proc.name.isdigit() or int(proc.name)==os.getpid():continue
        try:
            if proc.stat().st_uid!=os.getuid():continue
            expected=import_topology_process(int(proc.name))
        except FileNotFoundError:continue
        try:value=import_fd_metadata(expected,roots)
        except PermissionError as error:
            require(records is not None,'受限FD扫描缺少原件目录')
            write_once(Path(records)/('proc-read-failed-'+str(proc.name)+'-'+str(time.time_ns())+'.json'),
                json.dumps({'identity':{key:expected[key] for key in ('pid','starttime','uid','vm_boot_id')},
                    'error_type':type(error).__name__,'privileged_readonly_fallback':True}))
            value=privileged_import_fd_metadata(expected,roots,records)
        except FileNotFoundError:
            try:import_topology_process(expected['pid'])
            except FileNotFoundError:continue
            raise ValueError('FD原件消失但进程仍在，不能推断无写入')
        found.extend({'pid':expected['pid'],**row} for row in value['holders'])
    return found


def stop_import_instance(config,manifest,output):
    """只停根已冻结的独占导入进程树；无法证实则上层继续持锁。"""
    frozen=bound_json(config['instance_record_file'],manifest['cloud']['instance_record_sha256'])
    require(frozen['owner']==runtime_owner(manifest) and frozen['uid']==os.getuid()
            and frozen['pid']==frozen['pgrp']==frozen['session'] and frozen['pid']>1,'不能停止非独占候选实例')
    settings=bound_json(frozen['config_path'],frozen['config_sha256'])
    roots=[Path(settings['iso'][name]).resolve() for name in ('staging','artifacts','smb','tools')]
    require(all(root.is_relative_to(Path(VM_BASE)/'machines/media') and root!=Path(VM_BASE)/'machines/media' for root in roots),
            '停止的导入路径不在独占介质子目录')
    key=('pid','starttime','uid','pgrp','cmdline_sha256','vm_boot_id','executable_path','executable_sha256')
    records=Path(output)/'import-stop';records.mkdir(mode=0o700,exist_ok=True)
    stopped_record=records/'tree.json'
    if stopped_record.exists():
        tree=private_json(stopped_record)
        require(tree['owner']==frozen['owner'] and tree['instance_record_sha256']==manifest['cloud']['instance_record_sha256'],
                '停止原件不是准确实例')
        members={int(pid):item for pid,item in tree['members'].items()}
    else:
        current=import_process(frozen['pid'])
        require(current is not None and all(current[k]==frozen[k] for k in key),
                '导入根进程身份已变或首次停止前已丢失，不能猜其子进程已退出')
        send_import_signal(current,signal.SIGSTOP)
        write_once(records/('root-frozen-'+str(time.time_ns())+'.json'),json.dumps({'owner':frozen['owner'],
            'instance_record_sha256':manifest['cloud']['instance_record_sha256'],'process':current,'signal':'SIGSTOP'}))
        members={frozen['pid']:current}
        # 根先冻结，之后逐层冻结后代；只有稳定无新后代才允许结束它们。
        for _ in range(64):
            all_processes=import_processes()
            additions={pid:item for pid,item in all_processes.items() if item['parent'] in members and pid not in members}
            if not additions:break
            for pid,item in additions.items():
                require(item['uid']==frozen['uid'] and item['vm_boot_id']==frozen['vm_boot_id'],'导入后代身份不明')
                actual=import_process(pid)
                require(all(actual[key]==item[key] for key in ('pid','starttime','uid','pgrp','parent','vm_boot_id')),
                        '冻结前导入后代身份变化')
                send_import_signal(actual,signal.SIGSTOP);members[pid]=actual
        else:raise ValueError('导入后代未稳定，保留锁和已冻结进程')
        write_once(stopped_record,json.dumps({'owner':frozen['owner'],
            'instance_record_sha256':manifest['cloud']['instance_record_sha256'],'members':members},indent=2))
    # 未先落准确树原件，绝不逐个猜PID停止。重试只处理该已知树。
    for pid,item in reversed(list(members.items())):
        try:actual=import_process(pid)
        except FileNotFoundError:continue
        require(all(actual[k]==item[k] for k in key),'停止时PID已被复用，拒绝误杀')
        if actual['state']!='Z':send_import_signal(item,signal.SIGKILL)
    deadline=time.monotonic()+15
    while time.monotonic()<deadline:
        remaining=[]
        for pid,item in members.items():
            try:actual=import_process(pid)
            except FileNotFoundError:continue
            if actual['starttime']==item['starttime'] and actual['state']!='Z':remaining.append(pid)
        if not remaining:
            require(import_write_holders(roots,records)==[],'专用导入目录仍有可写FD/工作目录，不能释放锁')
            proof={'imports_stopped':True,'owner':frozen['owner'],'members':list(members),
                   'instance_record_sha256':manifest['cloud']['instance_record_sha256'],'tree_sha256':sha256(stopped_record)}
            write_once(records/('confirmed-'+str(time.time_ns())+'.json'),json.dumps(proof))
            return proof
        time.sleep(.1)
    raise ValueError('准确导入树仍有活进程，禁止释放heavy锁')


def owned_client_guard_source(manifest):
    """准确新客户的宿主只读校验；后续动作都使用同一UUID。"""
    target=manifest_target(manifest);owner=manifest['cloud']['owner']
    source='import hashlib,json,os,re,stat,subprocess,time,uuid,ipaddress,socket\nfrom pathlib import Path\nimport xml.etree.ElementTree as ET\n'
    for name in ('PURPOSE','BASE','VM_BASE','TARGET','SN','MAC','SERIAL','DATA_SERIAL','SERVICE_UUID','SERVICE_MAC','NETWORK','BRIDGE','GIB'):
        source+=name+'='+repr(globals()[name])+'\n'
    source+='SUBNET=ipaddress.ip_network("192.168.201.0/24")\n'
    source+='\n\n'.join(inspect.getsource(fn) for fn in (require,sha256,owner_valid,target_spec,resolve_target,check_disk,check_target))+'\n'
    source+='owner='+repr(owner)+'\ntarget='+repr(target)+'\n'
    source+='''require(socket.gethostname()=='ser866667880745' and os.geteuid()==0,'wrong host')
def virsh(*args):return subprocess.check_output(['virsh','-c','qemu:///system',*args],text=True,timeout=15).strip()
path=Path(target['disk_path'])
require(path.is_file() and not path.is_symlink(),'owned target disk absent or linked')
marker=path.parent/'.owner.json'
require(marker.is_file() and not marker.is_symlink() and json.loads(marker.read_text())==owner,'target directory owner mismatch')
require(all(not p.is_symlink() for p in path.parents),'target path has linked ancestor')
disk={'info':json.loads(subprocess.check_output(['qemu-img','info','--force-share','--output=json',str(path)],text=True,timeout=10))}
before={}
for kind,args in [('inactive',['--inactive']),('live',[])]:
 raw=virsh('dumpxml',target['uuid'],*args)
 check_target(raw,owner,owner,disk,target=target)
 root=ET.fromstring(raw)
 require(not root.findall('./devices/filesystem') and not root.findall('./devices/hostdev'),'target has extra writable host devices')
 before[kind]=raw
'''
    return source


def owned_client_stop_source(manifest):
    source=owned_client_guard_source(manifest)
    source+=r'''
state=virsh('domstate',target['uuid'])
pidfile=Path('/run/libvirt/qemu')/(target['name']+'.pid')
old=None
if state!='shut off':
 require(pidfile.is_file() and not pidfile.is_symlink(),'missing exact client QEMU pid')
 pid=int(pidfile.read_text());proc=Path('/proc')/str(pid)
 argv=(proc/'cmdline').read_bytes().rstrip(b'\0').split(b'\0')
 require(b'-uuid' in argv and argv[argv.index(b'-uuid')+1]==target['uuid'].encode(),'QEMU uuid mismatch')
 old=(pid,(proc/'stat').read_text().rsplit(')',1)[1].split()[19])
if state!='shut off':virsh('destroy',target['uuid'])
require(virsh('domstate',target['uuid'])=='shut off','owned client stop unconfirmed')
if old:
 deadline=time.monotonic()+15
 while time.monotonic()<deadline:
  proc=Path('/proc')/str(old[0])
  try:current=(proc/'stat').read_text().rsplit(')',1)[1].split()[19]
  except FileNotFoundError:break
  if current!=old[1]:break
  time.sleep(.1)
 else:raise RuntimeError('old customer QEMU not confirmed gone')
require(path.is_file() and not path.is_symlink(),'failed disk not preserved')
for proc in Path('/proc').iterdir():
 if not proc.name.isdigit():continue
 try:args=(proc/'cmdline').read_bytes().rstrip(b'\0').split(b'\0')
 except FileNotFoundError:continue
 if b'-uuid' in args:require(args[args.index(b'-uuid')+1]!=target['uuid'].encode(),'owned QEMU still present')
print(json.dumps({'client_stopped':True,'owner':owner,'target':target,'state':'shut off','failed_disks_preserved':True,'qemu_gone':True,'before':before,'inactive_after':virsh('dumpxml',target['uuid'],'--inactive')}))
'''
    return source


def guest_agent_read(config,manifest,payload):
    source=owned_client_guard_source(manifest)
    request={'execute':'guest-exec','arguments':{'path':'/usr/bin/python3','arg':['-c',payload],'capture-output':True}}
    source+='request='+repr(json.dumps(request))+'\n'
    source+='''import base64
pid=json.loads(virsh('qemu-agent-command',target['uuid'],request))['return']['pid']
for _ in range(90):
 status=json.loads(virsh('qemu-agent-command',target['uuid'],json.dumps({'execute':'guest-exec-status','arguments':{'pid':pid}})))['return']
 if status.get('exited'):
  require(status.get('exitcode')==0 and not status.get('out-truncated') and not status.get('err-truncated'),'guest agent inspection failed')
  print(base64.b64decode(status['out-data'],validate=True).decode());break
 time.sleep(1)
else:raise RuntimeError('target inspection timed out')
'''
    return json.loads(host_command(config,source,timeout=130))


def target_action_source(manifest,phase):
    require(phase in ('start','reboot'),'未知客户动作')
    source=owned_client_guard_source(manifest)
    source+='\n\n'.join(inspect.getsource(fn) for fn in (canonical_sha256,xml_signature,normalized_policy,check_policy))+'\n'
    contract=manifest['cloud']['network_state_contract']
    source+='contract='+repr(contract)+'\nphase='+repr(phase)+'\n'
    source+='definition_sha256='+repr(manifest['cloud']['client_definition_sha256'])+'\n'
    source+='''require(canonical_sha256(xml_signature(before['inactive']))==definition_sha256,'complete customer definition changed')
require(target['network'] in virsh('net-list','--name').splitlines(),'owned network inactive')
for name,args in [('active',[]),('inactive',['--inactive'])]:
 node=ET.fromstring(virsh('net-dumpxml',target['network'],*args))
 if name=='active' and 'connections' in node.attrib:require(node.attrib.pop('connections').isdigit(),'invalid connection count')
 require(canonical_sha256(xml_signature(node))==contract[name+'_xml_sha256'],'actual network definition changed')
actual=json.loads(subprocess.check_output(['nft','-j','list','table','inet','lober_pq25'],text=True,timeout=10))
check_policy(actual,contract['host_policy'])
if phase=='start':
 require(virsh('domstate',target['uuid'])=='shut off','customer is already running')
 disk['map']=json.loads(subprocess.check_output(['qemu-img','map','--force-share','--output=json',target['disk_path']],text=True,timeout=10))
 check_disk(disk,True)
 virsh('start',target['uuid'])
 require(virsh('domstate',target['uuid'])=='running','new customer start not confirmed')
else:
 require(virsh('domstate',target['uuid'])=='running','repeat customer is not running')
 virsh('reboot',target['uuid'],'--mode','agent')
print(json.dumps({'owner':owner,'target':target,'phase':phase,'failed_disks_preserved':True,
 'started':phase=='start','state':virsh('domstate',target['uuid']),'network_boot_first':True}))
'''
    return source


def check_vm_runtime_network(config,manifest):
    links=json.loads(command(['ip','-j','address'],timeout=10))
    rows=[row for row in links if row.get('address','').lower()==SERVICE_MAC]
    require(len(rows)==1 and rows[0]['ifname']==config['new_interface'],'本轮新接口身份改变')
    require(any(addr.get('local')=='192.168.201.10' for addr in rows[0].get('addr_info',[])),'服务新地址缺失')
    contract=manifest['cloud']['network_state_contract']
    expected=isolation_policy('vm',config['new_interface'],contract['targets'])
    check_policy(contract['vm_policy'],expected)
    actual=json.loads(command(['sudo','-n','nft','-j','list','table','inet','lober_pq25_vm'],timeout=10))
    check_policy(actual,expected)
    require(Path('/proc/sys/net/ipv4/ip_forward').read_text().strip()=='0'
        and Path('/proc/sys/net/ipv6/conf/all/forwarding').read_text().strip()=='0','原VM转发设置变化')


def load_cloud_helper(name,manifest):
    import importlib.util
    require(name in ('cloud_guest_identity','cloud_capture_session','cloud_no_task_probe'),'未知独立helper')
    path=Path(__file__).with_name(name+'.py')
    require(path.is_file() and not path.is_symlink() and sha256(path)==manifest['cloud']['helper_sha256'][name],
            '独立helper不是冻结版本')
    review=manifest['cloud']['helper_reviews'][name]
    require(review['approved'] is True and review['root_reviewer']!=review['independent_reviewer']
            and review['root_reviewer'] and review['independent_reviewer'],'独立helper尚未双审')
    spec=importlib.util.spec_from_file_location(name,path);module=importlib.util.module_from_spec(spec)
    sys.modules[name]=module;spec.loader.exec_module(module);return module


def validate_phase_helpers(manifest,phase):
    names={'start':['cloud_capture_session'],'parallel':['cloud_capture_session'],
        'qualify':['cloud_capture_session','cloud_guest_identity'],'short':['cloud_capture_session','cloud_no_task_probe']}
    for name in names.get(phase,[]):load_cloud_helper(name,manifest)


def phase_resources(config,manifest,initial=False):
    require(shutil.disk_usage('/').free>=GIB,'执行期间根盘低于1GiB')
    mount=Path(VM_BASE)/'machines/media'
    identity=download_mount_identity(mount)
    rows=json.loads(command(['findmnt','--json','--mountpoint',str(mount),'--output','TARGET,SOURCE,FSTYPE,UUID,OPTIONS'],timeout=10))['filesystems']
    blocks=json.loads(command(['lsblk','--json','--bytes','--output','PATH,SIZE,TYPE,SERIAL','/dev/vdb'],timeout=10))['blockdevices']
    require(len(rows)==len(blocks)==1 and rows[0]['uuid']=='7efe0e2b-6fb6-46f7-bc93-1bc0246ee80a'
        and blocks[0]['serial']==DATA_SERIAL and int(blocks[0]['size'])==16*GIB,'实际专用介质盘UUID/序列/大小变化')
    require(shutil.disk_usage(mount).free>=GIB,'执行期间专用盘低于1GiB')
    raw=host_command(config,"import json,shutil,socket\nif socket.gethostname()!='ser866667880745':raise RuntimeError('wrong host')\nprint(json.dumps({'free':shutil.disk_usage('/var/lib/libvirt/images').free}))\n",timeout=12)
    free=json.loads(raw)['free']
    require(free>=(38 if initial else 6)*GIB,'宿主初始38GiB/执行中6GiB门槛不满足')
    return {'host_free':free,'mount':identity}


class PhaseWatch:
    """API/SSH阻塞期间也监测资源；线程失败用主线程信号进入统一收尾。"""
    def __init__(self,probe,interval=2):
        self.probe,self.interval=probe,interval;self.ended=threading.Event();self.error=None;self.thread=None
    def check(self):
        if self.error is not None:raise RuntimeError('后台资源/抓包监护失败：'+type(self.error).__name__) from self.error
    def interrupt(self,number,frame):self.check()
    def __enter__(self):
        require(threading.current_thread() is threading.main_thread(),'真实监护必须在主线程建立')
        self.probe();self.old=signal.getsignal(signal.SIGUSR1);signal.signal(signal.SIGUSR1,self.interrupt)
        def watch():
            while not self.ended.wait(self.interval):
                try:self.probe()
                except BaseException as error:
                    self.error=error;os.kill(os.getpid(),signal.SIGUSR1);return
        self.thread=threading.Thread(target=watch,name='cloud-resource-watch',daemon=False);self.thread.start();return self
    def __exit__(self,kind,error,traceback):
        self.ended.set()
        while self.thread.is_alive():self.thread.join(timeout=1)
        signal.signal(signal.SIGUSR1,self.old)
        if kind is None:self.check()
        return False


def check_prepare_binding(prepared, manifest, actual_iso):
    require(prepared['owner'] == runtime_owner(manifest), 'prepare共享实例 owner 不符')
    for key in ('product_sha', 'test_sha', 'binary_sha256', 'run_id'):
        require(prepared[key] == manifest[key], 'prepare 候选身份变化：' + key)
    for key in ('executor_sha256', 'instance_record_sha256'):
        require(prepared[key] == manifest['cloud'][key], 'prepare 执行器或实际进程变化')
    require(prepared['media_manifest_sha256'] == canonical_sha256(manifest['cloud']['media']), 'prepare 原件或签名链变化')
    expected = prepared['iso']
    require(expected == actual_iso, '实际 ISO 不是本轮冻结导入结果')
    require(actual_iso['state'] == 'ready' and actual_iso['family'] == 'linux' and actual_iso['series'] == 'debian'
            and actual_iso['arch'] == 'x86_64' and actual_iso['version'] == manifest['cloud']['media']['version']
            and actual_iso['sha256'] == manifest['cloud']['media']['dvd']['sha256'], '真实 ISO 资格不符')
    require(all(actual_iso['artifacts'].get(role) for role in ('kernel', 'initrd', 'repo')), '导入缺必需制品')


def check_prepare_originals(config,manifest,prepared):
    root=Path(config['prepare_chain_file']).parent
    require(prepared['database_origin_sha256']==manifest['cloud']['database_origin_sha256'], 'prepare数据库来源串轮')
    require(prepared['import_receipt_sha256']==prepared['evidence_sha256']['import-private.json'],'导入首次回执未绑定')
    require(set(prepared['evidence_sha256'])=={'import-private.json','import-ready-private.json','media-verified.json'},'prepare原件集合不完整')
    for name,digest in prepared['evidence_sha256'].items():
        require(Path(name).name==name,'prepare原件路径越界')
        value=bound_json(root/name,digest)
        if name=='import-private.json':require(value['id']==prepared['iso']['id'],'导入首次回执ISO id串错')
        if name=='import-ready-private.json':require(value==prepared['iso'],'真实ready原件与链不符')


def check_start_binding(started,manifest,target):
    require(started['owner']==manifest['cloud']['owner'] and started['target']==target
        and started['target_sha256']==canonical_sha256(target),'start链串客户/盘代际')
    for key in ('product_sha','test_sha','binary_sha256','run_id'):
        require(started[key]==manifest[key],'start链候选身份变化：'+key)
    for key in ('prepare_chain_sha256','instance_record_sha256','database_origin_sha256','short_failure_evidence_sha256'):
        require(started[key]==manifest['cloud'][key],'start输入链变化：'+key)
    require(started['executor_sha256']==sha256(__file__),'start检查器变化')


def check_task_binding(task, prepared, template, hook, owner, target=None):
    target = resolve_target(owner,target)
    snapshot = task['snapshot']
    iso = prepared['iso']
    require(task['hostSN'] == target['sn'] and task['isoID'] == iso['id'] and task['templateID'] == template['id']
            and task['templateVersion'] == template['version'], '任务串用了其他 ISO 或模板')
    require(snapshot['family'] == 'linux' and snapshot['series'] == 'debian' and snapshot['format'] == 'preseed', '任务平台快照不符')
    require(snapshot['host']['sn'] == target['sn'] and snapshot['host']['arch'] == 'x86_64' and snapshot['host']['firmware'] == 'bios'
            and snapshot['host']['attrs'].get('run_id') == owner['run_id']
            and snapshot['host']['attrs'].get('target_uuid')==target['uuid'], '任务主机快照不属本轮准确客户')
    require(snapshot['iso'] == {key: iso[key] for key in ('id', 'name', 'version', 'arch', 'artifacts')}, '任务介质快照与导入不符')
    require(all(snapshot['template'][key] == template[key] for key in ('id', 'name', 'version', 'body')), '任务模板快照不符')
    require(template['state'] == 'approved' and hook['state'] == 'approved'
            and template['author'] != template['approver'] and hook['author'] != hook['approver'], '冻结模板或 hook 未独立批准')
    slots = [slot for slot in snapshot['slots'] if slot['name'] == 'qualify']
    require(len(slots) == 1 and slots[0]['exec'] is True and slots[0]['timing'] == 'firstboot'
            and len(slots[0]['instances']) == 1, '任务缺冻结 qualify 槽位')
    instance = slots[0]['instances'][0]
    implementations = [impl for impl in hook['impls'] if impl['family'] == 'linux' and 'debian' in impl['series']]
    require(len(implementations) == 1, '冻结 hook 实现不唯一')
    body = implementations[0]['body']
    require(instance['hookID'] == hook['id'] and instance['hookVersion'] == hook['version']
            and instance['body'] == body and instance['digest'] == hashlib.sha256(body.encode()).hexdigest()
            and instance['required'] is True and instance['interpreter'] == 'sh', '任务 hook 快照串错')
    disks = [slot for slot in snapshot['slots'] if slot['name'] == 'partition']
    require(len(disks) == 1 and disks[0]['instances'][0]['params']['disk']['items'] == ['vda'], '任务没有锁定本轮 vda')


def check_ssh_binding(agent, ssh, access, owner, ssh_exit):
    require(ssh_exit == 0, '真实 SSH 登录失败，不能跳过')
    require(access['owner'] == owner and access['target_uuid'] == owner['target_uuid']
            and access['address'] == '192.168.201.101' and agent['address'] == access['address'], 'SSH 地址不是准确客户')
    require(agent['ssh_host_public_key'] == access['host_public_key'], '可信 agent 采集的客户主机公钥变化')
    for key in ('smbios_serial', 'machine_id', 'boot_id', 'root_uuid', 'root_disk_serial'):
        require(bool(ssh[key]) and ssh[key] == agent[key], 'SSH 登录的是另一客户：' + key)
    require(ssh['smbios_serial'] == SN and ssh['root_disk_serial'] == SERIAL, 'SSH 磁盘或 SMBIOS 不符')


def check_preflight(value, owner, existing=False):
    owner_valid(owner)
    require(value['hostname'] == 'ser866667880745', '宿主身份不符')
    require(value['free_bytes'] >= (6 if existing else 38) * GIB, '宿主真实空闲不足')
    require(value['vm_free_bytes'] >= GIB, 'lober 根盘空闲不足 1GiB')
    auth = value['auth']
    require(auth.get('ok') is True and auth.get('strict_host_key') is True and auth.get('from_vm') is True,
            '缺少 VM 到宿主的严格认证通道')
    if existing:
        require(value['base_exists'] and value['owner'] == owner, '既有目录未登记本轮 owner')
        require(value.get('registered_target_sha256') == value.get('actual_target_sha256')
                and bool(value.get('actual_target_sha256')), '既有目标实际 XML 与本轮登记不符')
        check_target(value['actual_target_xml'], value['owner'], owner, value['target_disk'], blank=False)
        return
    require(not value['base_exists'] and value['owner'] is None, '禁止接管既有目录')
    for xml in value['domains']:
        root = ET.fromstring(xml)
        require(root.findtext('name') != TARGET and root.findtext('uuid') != owner['target_uuid'], '目标域已存在')
        require(root.findtext("./sysinfo/system/entry[@name='serial']") != SN, 'SN 冲突')
        for net in root.findall('./devices/interface'):
            mac = net.find('mac')
            require(mac is None or mac.get('address', '').lower() not in (MAC, SERVICE_MAC), 'MAC 冲突')
        for d in root.findall('./devices/disk/source'):
            require(not d.get('file', '').startswith(BASE + '/'), '新目录磁盘已被其他域引用')
    for xml in value['networks']:
        root = ET.fromstring(xml)
        require(root.findtext('name') != NETWORK, '新网络名字冲突')
        bridge = root.find('bridge')
        require(bridge is None or bridge.get('name') != BRIDGE, '新网桥冲突')
        for addr in root.findall('ip'):
            net = ipaddress.ip_network(addr.get('address') + '/' + addr.get('netmask', addr.get('prefix', '32')), strict=False)
            require(net.version != 4 or not net.overlaps(SUBNET), '新网段与现存网络冲突')
        for host in root.findall('./ip/dhcp/host'):
            require(host.get('mac', '').lower() not in (MAC, SERVICE_MAC), 'DHCP MAC 冲突')
    for addr in value['addresses'] + value['routes']:
        if addr == 'default':
            continue
        net = ipaddress.ip_network(addr, strict=False)
        require(net.version != 4 or not net.overlaps(SUBNET), '新网段与地址或路由冲突')
    require(BRIDGE not in value['bridges'], '网桥名字已存在')
    require(not set(value['vm_ports'] + value['host_ports']).intersection([18090, 18082]), '新端口已被占用')


def check_guest(value, version, target=None):
    target = target_spec({'target_uuid': '00000000-0000-0000-0000-000000000000'}) if target is None else target
    require(value['ssh_exit'] == 0, '真实目标登录失败，不能 skip')
    require(value['os_id'] == 'debian' and value['version_id'] == version.split('.')[0], '目标系统版本不符')
    if version.count('.') == 2:
        require(value.get('debian_version') == '.'.join(version.split('.')[:2]), '目标 Debian 更新版本不符')
    require(value['arch'] == 'x86_64' and value['firmware'] == 'bios', '目标架构或固件不符')
    require(value['root_disk_serial'] == target['disk_serial'] and value['root_source'].startswith('/dev/vda'), '系统没有装在指定新盘')
    require(value['address'] == target['guest_ip'] and value['default_gateway'] == target['host_ip'], '目标网络不符')
    if 'smbios_serial' in value:require(value['smbios_serial'] == target['sn'], '客户 SMBIOS 不符')
    require(len(value['partitions']) >= 2 and all(p.startswith('/dev/vda') for p in value['partitions']), '缺少目标分区证据')
    for key in ('root_uuid', 'machine_id', 'boot_id'):
        require(bool(value[key]), '缺少系统标识 ' + key)
    require(re.fullmatch('[0-9a-f]{64}', value['sentinel_sha256']), '缺少磁盘哨兵摘要')
    require(value['hook_runs'] == 1, '必需 hook 运行次数不符')


def check_qualification(value, negative=False, target=None, bindings=None):
    if target is None:
        target = target_spec({'target_uuid': '00000000-0000-0000-0000-000000000000'})
    else:
        resolve_target({'purpose': PURPOSE, 'run_id': 'qualification-contract', 'target_uuid': target['uuid']}, target)
    task, events, guest = value['task'], value['events'], value['guest']
    expected = 'failed' if negative else 'done'
    require(task['hostSN'] == target['sn'] and task['state'] == expected, '未实际采到预期安装终态')
    require(events and all(e['taskID'] == task['id'] for e in events), '事件缺失或串任务')
    seqs = [e['seq'] for e in events]
    require(seqs == sorted(set(seqs)) and seqs[0] > 0, '事件重复或顺序错误')
    kinds = [e['kind'] for e in events]
    require('machine.installing' in kinds and 'machine.init_start' in kinds, '没有真实安装及首启阶段事件')
    slots = [s for s in task['snapshot']['slots'] if s['name'] == 'qualify']
    require(len(slots) == 1 and slots[0]['exec'] and slots[0]['timing'] == 'firstboot', '缺少冻结必需 hook 槽位')
    slot = slots[0]
    require(len(slot['instances']) == 1 and slot['instances'][0]['required'], 'hook 不是单个必需实例')
    match = [e for e in events if e['kind'] == 'hook.report' and all(e['fields'].get(k) == v for k, v in
             {'slot': str(slot['seq']), 'instance': '1', 'timing': 'firstboot',
              'digest': slot['instances'][0]['digest'], 'rc': '41' if negative else '0'}.items())]
    require(len(match) == 1, '不能把其他 hook、超时或任意失败算成指定必需 hook 结果')
    if not negative:
        require('machine.ready' in kinds, '缺少真实 ready 事件')
        require(guest['credential_files'] == 0 and not guest['retained_auth_paths'], '临时装机凭据未清理')
    check_guest(guest, value['media_version'], target)
    repeat = value['repeat']
    require(repeat['task_id'] == task['id'] and repeat['task_state'] == expected, '重复启动改变任务身份或状态')
    check_guest(repeat['guest'], value['media_version'], target)
    require(repeat['guest']['boot_id'] != guest['boot_id'], '没有实际重复启动')
    for key in ('root_uuid', 'machine_id', 'sentinel_sha256'):
        require(repeat['guest'][key] == guest[key], '重复启动后磁盘证据变化：' + key)
    for key in ('network_boot_request', 'localboot_response', 'retry_rejected', 'duplicate_callback_rejected'):
        require(repeat[key] is True, '防重装缺少实际证据：' + key)
    if bindings is not None:check_qualification_originals(value,target,bindings)
    return {'status': 'EXPECTED_FAILURE' if negative else 'PASS', 'scope': 'debian-x86_64-bios-single-guest'}


def check_qualification_originals(value,target,bindings):
    """实际资格/容量汇总都重读原件，不仅相信JSON里的成功布尔值。"""
    import importlib.util
    modules={}
    for name in ('cloud_packet_capture','cloud_capture_session','cloud_guest_identity'):
        path=Path(__file__).with_name(name+'.py')
        require(not path.is_symlink() and bindings['helpers'].get(str(path.resolve()))==sha256(path),'汇总helper不是共同合同版本')
        spec=importlib.util.spec_from_file_location(name,path);module=importlib.util.module_from_spec(spec)
        sys.modules[name]=module;spec.loader.exec_module(module);modules[name]=module
    manifest=bindings['manifest'];require(manifest_target(manifest)==target,'资格目标和冻结manifest不同')
    inputs=value['input_files']
    prepared=bound_json(inputs['prepare']['path'],manifest['cloud']['prepare_chain_sha256'])
    started=bound_json(inputs['start']['path'],manifest['cloud']['start_chain_sha256'])
    check_start_binding(started,manifest,target)
    require(started['prepare_chain_sha256']==manifest['cloud']['prepare_chain_sha256']
        and started['target']==target and started['database_origin_sha256']==manifest['cloud']['database_origin_sha256'], '资格起始链串轮')
    check_task_binding(value['task'],prepared,started['template'],started['hook'],manifest['cloud']['owner'],target)
    capture=modules['cloud_capture_session'];packets={}
    for kind in ('install','repeat'):
        receipt=value['capture_receipts'][kind]
        frozen=bound_json(inputs['capture_contracts'][kind],manifest['cloud']['capture_contract_sha256'][kind])
        check_capture_receipt_binding(receipt,frozen,target,manifest['cloud']['owner'])
        reference=value['capture_anchors'][kind]
        if kind=='install':require(reference['sha256']==manifest['cloud']['install_capture_anchor_sha256'],'装机抓包anchor未冻结')
        else:
            anchor_path=Path(reference['path']).resolve();result_root=Path(bindings['result_root']).resolve()
            require(anchor_path.is_relative_to(result_root)
                and bindings['result_files'].get(str(anchor_path.relative_to(result_root)))==reference['sha256'],
                '重复抓包anchor未在可信结果原件链')
        anchor=bound_json(reference['path'],reference['sha256'])
        require(receipt['owner']==manifest['cloud']['owner'] and receipt['target_uuid']==target['uuid']
                and receipt['packet_contract']['task_id']==value['task']['id'],'抓包原件串客户/任务')
        packets[kind]=capture.analyze_session(receipt,frozen,target,anchor=anchor)
    expected=hashlib.sha256(('/v1/boot?sn='+target['sn']+'&arch=x86_64&firmware=bios').encode()).hexdigest()
    local=hashlib.sha256(b'#!ipxe\nexit || sanboot --no-describe --drive 0x80\n').hexdigest()
    require(any(row['method']=='GET' and row['target_sha256']==expected and row['response_status']==200
                and row['response_body_sha256']==local for row in packets['repeat']['http']),'汇总缺真实同流本地盘脚本')
    require(any(row['callback'] and row['response_status']==200 for row in packets['install']['http']),'汇总缺安装成功回调')
    for guest in (value['guest'],value['repeat']['guest']):
        access=guest['ssh_access'];binding=bound_json(access['binding_file'],access['binding_sha256'])
        require(binding['owner']==manifest['cloud']['owner'] and binding['target_contract']==target,'SSH原件串客户')
        identity=modules['cloud_guest_identity'];agent=binding['agent_evidence']
        require(identity.private_file(agent['raw_path'])==binding['agent_raw_file'],'可信QGA原件变化')
        raw=private_json(agent['raw_path']);ssh=private_json(access['ssh_stdout_file'])
        require(raw==ssh==agent['identity'],'SSH和QGA原始身份不同')
        for key in ('smbios_serial','machine_id','boot_id','root_uuid','root_disk_serial','address'):
            require(raw[key]==guest[key],'系统证据不是已验证客户')


def checksum_entry(text, name):
    rows = [line.split() for line in text.splitlines() if line.strip()]
    matches = [row[0] for row in rows if len(row) == 2 and row[1].lstrip('*').removeprefix('./') == name]
    require(len(matches) == 1 and re.fullmatch('[0-9a-f]{64}', matches[0]), '校验清单缺少唯一文件：' + name)
    return matches[0]


def check_media_chain(release, sums, version, build, mini_digest):
    require(re.fullmatch(r'13\.[0-9]+\.0', version), '首条流程须固定 Debian 13 更新版本')
    require(build == '20250803+deb13u' + version.split('.')[1], 'netboot 构建与 DVD 更新版本不符')
    fields = dict(line.split(': ', 1) for line in release.splitlines() if ': ' in line and not line.startswith(' '))
    require(fields.get('Origin') == 'Debian' and fields.get('Codename') == 'trixie'
            and fields.get('Version') == '.'.join(version.split('.')[:2]), '签名 Release 版本不符')
    path = 'main/installer-amd64/' + build + '/images/SHA256SUMS'
    in_sha256, matches = False, []
    for line in release.splitlines():
        if line and not line.startswith(' '):
            in_sha256 = line == 'SHA256:'
        if in_sha256 and line.startswith(' '):
            row = line.split()
            if len(row) == 3 and row[2] == path:
                matches.append((row[0], int(row[1])))
    require(matches == [(hashlib.sha256(sums.encode()).hexdigest(), len(sums.encode()))], '签名 Release 未绑定此份 netboot 校验清单')
    require(checksum_entry(sums, 'netboot/mini.iso') == mini_digest, 'netboot mini.iso 摘要不符')


def verify_media(media):
    root = Path(VM_BASE) / 'machines/media'
    require(root.is_mount() and not root.is_symlink(), '介质只能在本轮新独占数据盘')
    require((root / '.owner').read_text().strip() == PURPOSE, '独占介质盘 owner 不符')
    serial = command(['lsblk', '-n', '-o', 'SERIAL', command(['findmnt', '-n', '-o', 'SOURCE', '--target', str(root)])]).strip()
    require(serial == DATA_SERIAL, '介质文件系统来自其他磁盘')
    require(shutil.disk_usage('/').free >= GIB and shutil.disk_usage(root).free >= GIB, '文件系统余量不足')
    for name in ('dvd', 'netboot', 'dvd_sums', 'dvd_signature', 'release', 'release_signature',
                 'netboot_sums', 'cd_keyring', 'archive_keyring'):
        entry = media[name]
        path = Path(entry['path'])
        require(path.is_file() and not path.is_symlink(), '介质或校验文件缺失')
        require(sha256(path) == entry['sha256'], '介质或校验文件摘要变化：' + name)
        if name not in ('cd_keyring', 'archive_keyring'):
            require(path.resolve().is_relative_to(root.resolve()), '禁止复用旧提取文件或旧目录')
        require(path.stat().st_size == entry['size'], '介质文件大小变化')
    dvd_status = command(['gpgv', '--status-fd', '1', '--keyring', media['cd_keyring']['path'],
                          media['dvd_signature']['path'], media['dvd_sums']['path']])
    fingerprints = re.findall(r'\[GNUPG:\] VALIDSIG ([A-F0-9]{40})', dvd_status)
    require(any(f in ('10460DAD76165AD81FBC0CE9988021A964E6EA7D', 'DF9B9C49EAA9298432589D76DA87E80D6294BE9B')
                for f in fingerprints), 'DVD 签名不是官方稳定版 CD 签名身份')
    archive_status = command(['gpgv', '--status-fd', '1', '--keyring', media['archive_keyring']['path'],
                              media['release_signature']['path'], media['release']['path']])
    require('[GNUPG:] VALIDSIG ' in archive_status, 'Debian archive Release 签名未通过')
    dvd_name = 'debian-' + media['version'] + '-amd64-DVD-1.iso'
    require(Path(media['dvd']['path']).name == dvd_name, '必须是完整 DVD-1')
    require(media['dvd']['size'] >= GIB, 'DVD 文件明显不完整')
    require(checksum_entry(Path(media['dvd_sums']['path']).read_text(), dvd_name) == media['dvd']['sha256'], 'DVD 摘要与官方签名清单不符')
    check_media_chain(Path(media['release']['path']).read_text(), Path(media['netboot_sums']['path']).read_text(),
                      media['version'], media['installer_build'], media['netboot']['sha256'])
    return {'version': media['version'], 'dvd_sha256': media['dvd']['sha256'],
            'netboot_sha256': media['netboot']['sha256'], 'dvd_signature_status': dvd_status,
            'archive_signature_status': archive_status}


def definitions(owner):
    owner_valid(owner)
    domain = f'''<domain type='kvm'><name>{TARGET}</name><uuid>{owner['target_uuid']}</uuid>
<memory unit='MiB'>4096</memory><currentMemory unit='MiB'>4096</currentMemory><vcpu>2</vcpu>
<os><type arch='x86_64' machine='q35'>hvm</type><smbios mode='sysinfo'/></os>
<sysinfo type='smbios'><system><entry name='serial'>{SN}</entry></system></sysinfo>
<features><acpi/><apic/></features><cpu mode='host-passthrough'/><on_reboot>restart</on_reboot>
<devices><emulator>/usr/bin/qemu-system-x86_64</emulator>
<controller type='pci' index='0' model='pcie-root'/>
<controller type='pci' index='1' model='pcie-root-port'><model name='pcie-root-port'/><target chassis='1' port='0x10'/><address type='pci' domain='0x0000' bus='0x00' slot='0x02' function='0x0'/></controller>
<disk type='file' device='disk'><driver name='qemu' type='qcow2'/><source file='{BASE}/linux01.qcow2'/>
<target dev='vda' bus='virtio'/><serial>{SERIAL}</serial><boot order='2'/></disk>
<interface type='network'><mac address='{MAC}'/><source network='{NETWORK}'/><model type='e1000'/><boot order='1'/><address type='pci' domain='0x0000' bus='0x01' slot='0x00' function='0x0'/></interface>
<channel type='unix'><target type='virtio' name='org.qemu.guest_agent.0'/></channel>
<serial type='file'><source path='{BASE}/linux01-serial.log'/><target port='0'/></serial>
<console type='file'><source path='{BASE}/linux01-serial.log'/><target type='serial' port='0'/></console>
</devices></domain>'''
    network_uuid = str(uuid.uuid5(uuid.UUID(owner['target_uuid']), 'network'))
    network = f'''<network xmlns:dnsmasq='http://libvirt.org/schemas/network/dnsmasq/1.0'><name>{NETWORK}</name><uuid>{network_uuid}</uuid><forward mode='nat'/><bridge name='{BRIDGE}' stp='on' delay='0'/><mac address='52:54:00:25:10:fe'/>
<ip address='192.168.201.1' netmask='255.255.255.0'><tftp root='{BASE}/tftp'/><dhcp>
<host mac='{MAC}' name='pq25-linux01' ip='192.168.201.101'/>
<host mac='{SERVICE_MAC}' name='lober-pq-media' ip='192.168.201.10'/>
<bootp file='undionly.kpxe' server='192.168.201.1'/></dhcp></ip>
<dnsmasq:options><dnsmasq:option value='dhcp-userclass=set:ipxe,iPXE'/>
<dnsmasq:option value='dhcp-boot=tag:ipxe,pq25.ipxe'/>
<dnsmasq:option value='tftp-port-range=40000,40010'/></dnsmasq:options></network>'''
    data = f'''<disk type='file' device='disk'><driver name='qemu' type='qcow2'/><source file='{BASE}/media01.qcow2'/>
<target dev='vdb' bus='virtio'/><serial>{DATA_SERIAL}</serial></disk>'''
    interface = f"<interface type='network'><mac address='{SERVICE_MAC}'/><source network='{NETWORK}'/><model type='virtio'/></interface>"
    return {'domain.xml': domain, 'network.xml': network, 'data-disk.xml': data, 'service-interface.xml': interface}


def initialize_storage_dirs(base, owner):
    owner_valid(owner)
    base = Path(base)
    require(not base.exists() and not base.is_symlink(), '禁止接管既有目录')
    base.mkdir(mode=0o755)
    base.chmod(0o755)
    (base / 'tftp').mkdir(mode=0o755)
    (base / 'tftp').chmod(0o755)
    write_once(base / '.owner.json', json.dumps(owner, sort_keys=True) + '\n')
    require(stat.S_IMODE(base.stat().st_mode) == 0o755
            and stat.S_IMODE((base / 'tftp').stat().st_mode) == 0o755, '新目录实际权限不符')


def xml_signature(node):
    if isinstance(node, str):
        node = ET.fromstring(node)
    return (node.tag, tuple(sorted(node.attrib.items())), (node.text or '').strip(),
            tuple(xml_signature(child) for child in node))


def check_network(xml, owner, active=False):
    actual, expected = ET.fromstring(xml), ET.fromstring(definitions(owner)['network.xml'])
    if active:
        if 'connections' in actual.attrib:
            require(actual.attrib.pop('connections').isdigit(), '活动网络连接计数无效')
        forward = actual.find('forward')
        nat = forward.find('nat') if forward is not None else None
        if nat is not None:
            require(len(forward) == 1 and not nat.attrib and len(nat) == 1
                    and nat[0].tag == 'port' and nat[0].attrib == {'start': '1024', 'end': '65535'}
                    and len(nat[0]) == 0 and not (nat.text or '').strip()
                    and not (nat[0].text or '').strip(), '活动网络 NAT 不是观察到的 libvirt 默认值')
            forward.remove(nat)
    require(xml_signature(actual) == xml_signature(expected), '实际活动/持久网络与核准定义不符')


def validate_attachment(xml, kind):
    node = ET.fromstring(xml)
    expected = ET.fromstring(definitions({'purpose': PURPOSE, 'run_id': 'validation-only',
        'target_uuid': '015d0477-b113-41b8-93a7-e24c51cf686b'})[
        'data-disk.xml' if kind == 'disk' else 'service-interface.xml'])
    # libvirt 只能为新设备添加自动地址和内部名字。
    for child in list(node):
        if child.tag in ('alias', 'address'):
            node.remove(child)
        elif kind == 'interface' and child.tag == 'target':
            require(set(child.attrib) == {'dev'} and re.fullmatch('vnet[0-9]+', child.get('dev', '')), '实际网卡目标不是 libvirt 自动接口')
            node.remove(child)
        elif child.tag == 'backingStore':
            require(not child.attrib and len(child) == 0 and not (child.text or '').strip(), '设备包含 backing')
            node.remove(child)
    source = node.find('source')
    if source is not None and 'index' in source.attrib:
        require(source.attrib.pop('index').isdigit(), '设备内部序号无效')
    if kind == 'interface':
        source = node.find('source')
        if source is not None:
            if 'bridge' in source.attrib:
                require(source.attrib.pop('bridge') == BRIDGE, '实际附加网桥不符')
            if 'portid' in source.attrib:
                uuid.UUID(source.attrib.pop('portid'))
        node.attrib.pop('trustGuestRxFilters', None)
    require(xml_signature(node) == xml_signature(expected), '附加设备不是核准的新盘/新网卡')


def read_frozen_xml(path, expected, kind):
    path = Path(path)
    require(not path.is_symlink() and path.is_file(), '附加 XML 不能是符号链接')
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(descriptor, 'r') as source:
        require(stat.S_ISREG(os.fstat(source.fileno()).st_mode), '附加 XML 不是普通文件')
        require(stat.S_IMODE(os.fstat(source.fileno()).st_mode) == 0o600, '附加 XML 必须私有')
        raw = source.read()
    require(raw == expected + '\n', '附加 XML 字节已变化')
    validate_attachment(raw, kind)
    return raw


def service_device(root, kind):
    candidates = []
    for device in root.findall('./devices/' + kind):
        if kind == 'disk':
            target, source = device.find('target'), device.find('source')
            if ((target is not None and target.get('dev') == 'vdb') or device.findtext('serial') == DATA_SERIAL
                    or (source is not None and source.get('file') == BASE + '/media01.qcow2')):
                candidates.append(device)
        else:
            mac, source = device.find('mac'), device.find('source')
            if ((mac is not None and mac.get('address', '').lower() == SERVICE_MAC)
                    or (source is not None and source.get('network') == NETWORK)):
                candidates.append(device)
    return candidates


def check_service_pair(live, inactive, device, kind, before_attach):
    validate_attachment(device, kind)
    for xml in (live, inactive):
        root = ET.fromstring(xml)
        require(root.findtext('name') == 'lober' and root.findtext('uuid') == SERVICE_UUID, '服务 VM 身份不符')
        found = service_device(root, kind)
        if before_attach:
            require(not found, 'live 或 inactive 已占用目标设备，禁止重跑')
        else:
            require(len(found) == 1, '新设备未在两份实际配置中唯一出现')
            validate_attachment(ET.tostring(found[0], encoding='unicode'), kind)


def check_service_delta(before, after, device, kind):
    old, new = ET.fromstring(before), ET.fromstring(after)
    check_service_pair(before, before, device, kind, before_attach=True)
    check_service_pair(after, after, device, kind, before_attach=False)
    attached = service_device(new, kind)[0]
    new.find('devices').remove(attached)
    require(xml_signature(old) == xml_signature(new), '附加改动超出了唯一授权设备')


def apply_attachment(backend, device, kind, output):
    output = Path(output)
    before = {side: backend.dump(side) for side in ('live', 'inactive')}
    check_service_pair(before['live'], before['inactive'], device, kind, before_attach=True)
    write_once(output / 'attachment-before.json', json.dumps(before, indent=2) + '\n')
    try:
        for side in ('live', 'inactive'):
            try:
                backend.attach(side, device)
            finally:
                actual = {}
                for which in ('live', 'inactive'):
                    try:
                        actual[which] = backend.dump(which)
                    except BaseException as error:
                        actual[which] = {'unknown': type(error).__name__}
                write_once(output / ('attachment-after-' + side + '.json'), json.dumps(actual, indent=2) + '\n')
            check_service_delta(before[side], actual[side], device, kind)
        check_service_pair(actual['live'], actual['inactive'], device, kind, before_attach=False)
        write_once(output / 'attachment-complete.json', json.dumps({'actual': actual, 'safe_to_retry': False}, indent=2) + '\n')
        return actual
    except BaseException:
        write_once(output / 'attachment-failure.json', json.dumps({'actual': actual, 'before': before,
                    'safe_to_retry': False, 'requires_manual_recovery': True}, indent=2) + '\n')
        raise


def isolation_policy(where, interface, targets=None):
    guests='192.168.201.101'
    if targets is not None:
        require(1<=len(targets)<=2 and len({target['uuid'] for target in targets})==len(targets),'隔离目标不唯一')
        for target in targets:resolve_target({'purpose':PURPOSE,'run_id':'isolation-contract','target_uuid':target['uuid']},target)
        addresses=sorted({target['guest_ip'] for target in targets})
        require(len(addresses)==len(targets),'隔离客户地址冲突')
        guests=addresses[0] if len(addresses)==1 else {'set':addresses}
    require(where in ('host', 'vm') and re.fullmatch('[a-zA-Z0-9_.-]{1,15}', interface), '隔离对象无效')
    table = 'lober_pq25' if where == 'host' else 'lober_pq25_vm'
    entries = [{'add': {'table': {'family': 'inet', 'name': table}}}]
    def chain(name, hook):
        entries.append({'add': {'chain': {'family': 'inet', 'table': table, 'name': name,
            'type': 'filter', 'hook': hook, 'prio': -150, 'policy': 'accept'}}})
    def match(left, right, op='=='):
        return {'match': {'op': op, 'left': left, 'right': right}}
    incoming = match({'meta': {'key': 'iifname'}}, interface)
    outgoing = match({'meta': {'key': 'oifname'}}, interface)
    ipv6 = match({'meta': {'key': 'nfproto'}}, 'ipv6')
    def ip(field, value, op='=='):
        return match({'payload': {'protocol': 'ip', 'field': field}}, value, op)
    def port(protocol, value):
        return match({'payload': {'protocol': protocol, 'field': 'dport'}}, value)
    def rule(name, expressions, verdict):
        entries.append({'add': {'rule': {'family': 'inet', 'table': table, 'chain': name,
            'expr': expressions + [{verdict: None}], 'comment': PURPOSE}}})
    chain('input', 'input')
    chain('forward', 'forward')
    if where == 'vm':
        chain('output', 'output')
        rule('input', [incoming, ipv6], 'drop')
        rule('input', [incoming, ip('saddr', guests, '!=')], 'drop')
        rule('input', [incoming, ip('daddr', '192.168.201.10', '!=')], 'drop')
        for value in (18090, 18082):
            rule('input', [incoming, port('tcp', value)], 'accept')
        rule('input', [incoming, match({'ct': {'key': 'state'}}, 'established')], 'accept')
        rule('input', [incoming], 'drop')
        rule('forward', [incoming], 'drop')
        rule('forward', [outgoing], 'drop')
        rule('output', [outgoing, ipv6], 'drop')
        rule('output', [outgoing, ip('saddr', '192.168.201.10', '!=')], 'drop')
        rule('output', [outgoing, ip('daddr', guests, '!=')], 'drop')
        rule('output', [outgoing, match({'ct': {'key': 'state'}}, 'established')], 'accept')
        rule('output', [outgoing, port('tcp', 22)], 'accept')
        rule('output', [outgoing], 'drop')
    else:
        rule('forward', [incoming, outgoing], 'accept')
        for prefix in ('0.0.0.0/8', '10.0.0.0/8', '100.64.0.0/10', '127.0.0.0/8',
                       '169.254.0.0/16', '172.16.0.0/12', '192.0.0.0/24', '192.168.0.0/16',
                       '198.18.0.0/15', '224.0.0.0/4', '240.0.0.0/4', '110.42.44.11/32'):
            address, length = prefix.split('/')
            right = address if length == '32' else {'prefix': {'addr': address, 'len': int(length)}}
            rule('forward', [incoming, ip('daddr', right)], 'drop')
        rule('forward', [incoming, ipv6], 'drop')
        rule('input', [incoming, ipv6], 'drop')
        rule('input', [incoming, ip('daddr', '255.255.255.255'), port('udp', 67)], 'accept')
        rule('input', [incoming, ip('daddr', '192.168.201.1', '!=')], 'drop')
        rule('input', [incoming, match({'ct': {'key': 'state'}}, 'established')], 'accept')
        for value in (53, 67, 69, *range(40000, 40011)):
            rule('input', [incoming, port('udp', value)], 'accept')
        rule('input', [incoming, port('tcp', 53)], 'accept')
        rule('input', [incoming], 'drop')
    return {'nftables': entries}


def normalized_policy(policy):
    objects, chains = [], {}
    for entry in policy['nftables']:
        entry = entry.get('add', entry)
        if 'metainfo' in entry:
            continue
        kind = next(iter(entry))
        require(kind in ('table', 'chain', 'rule'), '隔离规则出现未知对象')
        value = {key: data for key, data in entry[kind].items() if key not in ('handle', 'index')}
        if kind == 'rule':
            chains.setdefault(value['chain'], []).append(value)
        else:
            objects.append({kind: value})
    return {'objects': sorted(objects, key=lambda row: json.dumps(row, sort_keys=True)),
            'rules_by_chain': {name: chains[name] for name in sorted(chains)}}


def check_policy(actual, expected):
    require(normalized_policy(actual) == normalized_policy(expected), '隔离表为空、缺规则或内容发生变化')


def check_existing_rules_preserved(before, after, owned_table):
    def clean(value):
        if isinstance(value, list):
            return [clean(part) for part in value]
        if isinstance(value, dict):
            return {key: ({k: v for k, v in part.items() if k not in ('packets', 'bytes')}
                          if key == 'counter' and isinstance(part, dict) else clean(part)) for key, part in value.items()}
        return value
    def groups(document):
        objects, rules = [], {}
        for entry in document['nftables']:
            if 'metainfo' in entry:
                continue
            kind = next(iter(entry))
            item = entry[kind]
            table = item.get('name') if kind == 'table' else item.get('table')
            if table == owned_table:
                continue
            value = clean({k: v for k, v in item.items() if k not in ('handle', 'index', 'use')})
            text = json.dumps({kind: value}, sort_keys=True)
            if kind == 'rule':
                rules.setdefault((item['family'], table, item['chain']), []).append(text)
            else:
                objects.append(text)
        return objects, rules
    old_objects, old_rules = groups(before)
    new_objects, new_rules = groups(after)
    require(all(item in new_objects for item in old_objects), '旧规则对象被移除或修改')
    for key, rows in old_rules.items():
        cursor = iter(new_rules.get(key, []))
        require(all(any(candidate == row for candidate in cursor) for row in rows), '旧规则被移除、修改或重排')


def network_resume_fingerprints(snapshot):
    result = {key: snapshot[key] for key in ('failure_sha256', 'failed_record_sha256')}
    for key in ('active_network_xml', 'inactive_network_xml', 'service_live_xml', 'service_inactive_xml'):
        result[key + '_sha256'] = hashlib.sha256(snapshot[key].strip().encode()).hexdigest()
    result['host_policy_sha256'] = canonical_sha256(normalized_policy(snapshot['host_policy']))
    return result


def check_network_resume(snapshot, contract, owner):
    require(contract['owner'] == owner and snapshot['owner'] == owner, '继续合同 owner 不符')
    require(contract['scope'] == 'continue-active-network-before-service-attach', '继续合同不能扩大操作范围')
    require(re.fullmatch('records-network-[0-9]+', contract['failed_records'])
            and snapshot['failed_records'] == contract['failed_records'], '继续合同失败原件目录不符')
    require(snapshot['step_flags'] == {'isolation_recorded': True, 'network_recorded': True, 'attachment_started': False},
            '当前不是已启网但尚未附新接口的部分状态')
    require(snapshot['target_state'] == 'shut off', '新客户未关机，拒绝继续')
    require(snapshot['network_active'] is True, '本轮网络不再活动，不能套用补附加合同')
    check_target(snapshot['target_xml'], owner, owner, snapshot['target_disk'], blank=True)
    check_network(snapshot['inactive_network_xml'], owner, active=False)
    check_network(snapshot['active_network_xml'], owner, active=True)
    check_policy(snapshot['host_policy'], isolation_policy('host', BRIDGE))
    check_service_pair(snapshot['service_live_xml'], snapshot['service_inactive_xml'],
                       definitions(owner)['service-interface.xml'], 'interface', before_attach=True)
    require(network_resume_fingerprints(snapshot) == contract['fingerprints'], '实际部分状态已变化，需要新合同而非直接重试')


def recorded_failures_resolved(base, owner):
    resolved = set()
    for path in Path(base).glob('recovery-*.json'):
        record = private_json(path)
        require(record['owner'] == owner and record['scope'] == 'continue-active-network-before-service-attach'
                and record['state'] == 'completed', '恢复完成原件无效')
        resolved.update(record['resolved_failure_sha256'])
    failures = [Path(base) / 'failure.json', *Path(base).glob('records-*/failure.json')]
    return all(not path.exists() or (not path.is_symlink() and sha256(path) in resolved) for path in failures)


class SafetyLease:
    """失败后持续持锁；只有读到准确停止证明才让其他重任务进入。"""
    def __init__(self, lock, output, stop, retry_seconds=5):
        self.lock, self.output, self.stop = Path(lock), Path(output), stop
        self.retry_seconds, self.handle = retry_seconds, None
        self.handlers = {}

    def acquire(self):
        import fcntl
        descriptor=os.open(self.lock,os.O_RDONLY|os.O_NOFOLLOW)
        require(stat.S_ISREG(os.fstat(descriptor).st_mode),'重锁不是原普通文件/硬链接')
        self.handle = os.fdopen(descriptor,'r')
        try:
            fcntl.flock(self.handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BaseException:
            self.handle.close()
            self.handle = None
            raise
        for sig in (signal.SIGTERM, signal.SIGHUP, signal.SIGINT):
            self.handlers[sig] = signal.getsignal(sig)
            signal.signal(sig, self.interrupt)

    def interrupt(self, number, frame):
        raise InterruptedError('编排收到终止信号 ' + str(number))

    def __enter__(self):
        self.acquire()
        return self

    def __exit__(self, kind, error, traceback):
        try:
            if kind is not None:
                write_once(self.output / ('phase-interrupted-' + str(time.time_ns()) + '.json'),
                           json.dumps({'error_type': kind.__name__, 'lock_retained_until_stop': True}) + '\n')
        finally:
            self.finish(failed=kind is not None)
        return False

    def finish(self, failed):
        if failed:
            for sig in self.handlers:
                signal.signal(sig, signal.SIG_IGN)
            while True:
                try:
                    result = self.stop()
                except BaseException as error:
                    result = {'client_stopped': False, 'imports_stopped': False, 'error': type(error).__name__}
                result['at'] = int(time.time())
                safe = result.get('client_stopped') is True and result.get('imports_stopped') is True
                result['lock_released'] = safe
                try:
                    write_once(self.output / ('cleanup-' + str(time.time_ns()) + '.json'), json.dumps(result, indent=2) + '\n')
                except BaseException as error:
                    print('停止原件写入失败，继续持锁：'+type(error).__name__,file=sys.stderr,flush=True)
                    safe=False
                if safe:
                    break
                time.sleep(self.retry_seconds)
        for sig, handler in self.handlers.items():
            signal.signal(sig, handler)
        if self.handle is not None:
            self.handle.close()
            self.handle = None



class HostPreparation:
    """只操作固定宿主和本轮资源；每个变更后保存实际状态。"""
    def __init__(self, owner, phase, vm_free):
        global COMMAND_RECORDS
        owner_valid(owner)
        require(socket.gethostname() == 'ser866667880745' and os.geteuid() == 0, '宿主身份不符')
        require(int(vm_free) >= GIB or phase in ('stop', 'probe-data'), 'VM 根盘余量不足')
        self.owner, self.phase, self.base = owner, phase, Path(BASE)
        self.created = False
        os.environ['LC_ALL'] = 'C'
        if phase == 'define':
            value = json.loads(command([sys.executable, '-c', HOST_SNAPSHOT]))
            value.update(vm_free_bytes=int(vm_free), vm_ports=[], auth={'ok': True, 'strict_host_key': True, 'from_vm': True})
            check_preflight(value, owner)
            print(json.dumps({'actual_preflight': value}), flush=True)
            import pwd
            pwd.getpwnam('libvirt-qemu')
            pwd.getpwnam('libvirt-dnsmasq')
            initialize_storage_dirs(self.base, owner)
            self.created = True
        else:
            require(self.base.is_dir() and not self.base.is_symlink(), '本轮目录无效')
            require(private_json(self.base / '.owner.json') == owner, 'owner 不符')
            require(not (self.base / 'failure.json').exists() or phase in ('stop', 'probe', 'probe-data', 'resume-network')
                    or recorded_failures_resolved(self.base, owner), '存在部分失败，禁止直接重跑')
        self.records = self.base / ('records-' + phase + '-' + str(time.time_ns()))
        self.records.mkdir(mode=0o700)
        COMMAND_RECORDS = self.records
        operation = process_identity(os.getpid())
        operation.update(owner=owner, phase=phase)
        write_once(self.records / 'operation-started.json', json.dumps(operation, indent=2) + '\n')
        if phase not in ('stop', 'probe-data'):
            self.resources()

    def virsh(self, *args):
        return command(['virsh', '-c', 'qemu:///system', *args]).strip()

    def resources(self):
        require(shutil.disk_usage(self.base.parent).free >= 6 * GIB, '宿主低于 6GiB 保留')

    def snapshot(self, label):
        result = {'owner': self.owner, 'phase': self.phase, 'at': int(time.time()), 'disks': {}}
        queries = {'target_live': ['dumpxml', self.owner['target_uuid']],
            'target_inactive': ['dumpxml', self.owner['target_uuid'], '--inactive'],
            'target_state': ['domstate', self.owner['target_uuid']],
            'service_live': ['dumpxml', SERVICE_UUID], 'service_inactive': ['dumpxml', SERVICE_UUID, '--inactive'],
            'network_active': ['net-dumpxml', NETWORK], 'network_inactive': ['net-dumpxml', NETWORK, '--inactive']}
        for key, args in queries.items():
            try:
                result[key] = self.virsh(*args)
            except BaseException as error:
                result[key] = {'unknown': type(error).__name__}
        for name in ('linux01', 'media01'):
            path = self.base / (name + '.qcow2')
            if path.is_file() and not path.is_symlink():
                try:
                    result['disks'][name] = json.loads(command(['qemu-img', 'info', '--force-share', '--output=json', str(path)]))
                except BaseException as error:
                    result['disks'][name] = {'unknown': type(error).__name__}
        write_once(self.records / (label + '.json'), json.dumps(result, indent=2) + '\n')
        return result

    def step(self, name, operation):
        write_once(self.records / (name + '-started.json'), json.dumps({'step': name, 'owner': self.owner, 'at': int(time.time())}) + '\n')
        try:
            result = operation()
            self.resources()
            return result
        finally:
            self.snapshot(name + '-actual')

    def permissions(self, boot=False):
        require(stat.S_IMODE(self.base.stat().st_mode) == 0o755
                and stat.S_IMODE((self.base / 'tftp').stat().st_mode) == 0o755, '专用目录实际权限不符')
        disks = [str(self.base / (name + '.qcow2')) for name in ('linux01', 'media01')]
        qemu_probe = "import os,json\npaths=" + repr(disks) + "\nhandles=[open(p,'r+b') for p in paths]\nif os.access(" + repr(str(self.base / '.owner.json')) + ",os.R_OK):raise RuntimeError('private owner exposed')\nprint(json.dumps({'disk_open_rw':True,'private_owner_readable':False}))"
        command(['runuser', '-u', 'libvirt-qemu', '--', sys.executable, '-c', qemu_probe])
        names = ['undionly.kpxe', 'pq25.ipxe'] if boot else []
        dns_probe = "import os,json\nbase=" + repr(str(self.base)) + "\nif not os.access(base+'/tftp',os.X_OK) or os.access(base+'/.owner.json',os.R_OK):raise RuntimeError('directory/private permissions wrong')\n[open(base+'/tftp/'+n,'rb').read(1) for n in " + repr(names) + "]\nprint(json.dumps({'tftp_traversable':True,'files_readable':" + repr(names) + "}))"
        command(['runuser', '-u', 'libvirt-dnsmasq', '--', sys.executable, '-c', dns_probe])

    def disk(self, name, blank=False):
        path = self.base / (name + '.qcow2')
        require(path.is_file() and not path.is_symlink(), '磁盘路径无效')
        result = {'info': json.loads(command(['qemu-img', 'info', '--force-share', '--output=json', str(path)])), 'map': []}
        if blank:
            result['map'] = json.loads(command(['qemu-img', 'map', '--force-share', '--output=json', str(path)]))
        check_disk(result, blank)
        return result

    def registered(self):
        record = private_json(self.base / 'registration.json')
        require(record['owner'] == self.owner, '登记 owner 不符')
        raw = self.virsh('dumpxml', self.owner['target_uuid'], '--inactive')
        require(hashlib.sha256(raw.encode()).hexdigest() == record['domain_sha256'], '持久客户配置变化')
        check_target(raw, self.owner, self.owner, self.disk('linux01'))
        active = self.virsh('dumpxml', self.owner['target_uuid'])
        check_target(active, self.owner, self.owner, self.disk('linux01'))
        check_network(self.virsh('net-dumpxml', NETWORK, '--inactive'), self.owner)
        return record

    def define(self):
        import pwd, grp
        qemu, group = pwd.getpwnam('libvirt-qemu').pw_uid, grp.getgrnam('kvm').gr_gid
        frozen = definitions(self.owner)
        for name, body in frozen.items():
            write_once(self.base / name, body + '\n')
        record = {'owner': self.owner, 'disks': {}, 'frozen_xml_sha256': {name: hashlib.sha256((body + '\n').encode()).hexdigest() for name, body in frozen.items()}}
        for name in ('linux01', 'media01'):
            path = self.base / (name + '.qcow2')
            self.step('create-' + name, lambda path=path: command(['qemu-img', 'create', '-f', 'qcow2', str(path), '16G']))
            disk = self.disk(name, blank=True)
            record['disks'][name] = {**disk, 'path': str(path), 'file_sha256': sha256(path)}
            os.chown(path, qemu, group)
            path.chmod(0o600)
        serial = self.base / 'linux01-serial.log'
        serial.touch(mode=0o600, exist_ok=False)
        os.chown(serial, qemu, group)
        self.permissions()
        self.step('define-network', lambda: self.virsh('net-define', str(self.base / 'network.xml')))
        self.step('define-domain', lambda: self.virsh('define', str(self.base / 'domain.xml')))
        raw = self.virsh('dumpxml', self.owner['target_uuid'], '--inactive')
        check_target(raw, self.owner, self.owner, self.disk('linux01', blank=True), blank=True)
        check_target(self.virsh('dumpxml', self.owner['target_uuid']), self.owner, self.owner, self.disk('linux01', blank=True), blank=True)
        net = self.virsh('net-dumpxml', NETWORK, '--inactive')
        check_network(net, self.owner)
        require(self.virsh('domstate', self.owner['target_uuid']) == 'shut off', '目标未关机')
        record.update(actual_domain_xml=raw, actual_network_xml=net,
            domain_sha256=hashlib.sha256(raw.encode()).hexdigest(), network_sha256=hashlib.sha256(net.encode()).hexdigest())
        write_once(self.base / 'registration.json', json.dumps(record, indent=2) + '\n')
        return record

    def dump(self, side):
        return self.virsh('dumpxml', SERVICE_UUID, *(['--inactive'] if side == 'inactive' else []))

    def attach(self, side, body):
        path = self.records / ('attach-' + side + '.xml')
        write_once(path, body)
        self.virsh('attach-device', SERVICE_UUID, str(path), '--live' if side == 'live' else '--config')

    def attach_disk(self):
        self.registered()
        frozen = definitions(self.owner)['data-disk.xml']
        body = read_frozen_xml(self.base / 'data-disk.xml', frozen, 'disk')
        self.disk('media01', blank=True)
        self.permissions()
        return apply_attachment(self, body, 'disk', self.records)

    def probe(self, blank=True):
        self.registered()
        body = definitions(self.owner)['data-disk.xml']
        live, inactive = self.dump('live'), self.dump('inactive')
        check_service_pair(live, inactive, body, 'disk', before_attach=False)
        return {'owner': self.owner, 'service_uuid': SERVICE_UUID, 'live': live, 'inactive': inactive,
                'data_disk': self.disk('media01', blank=blank), 'at': int(time.time())}

    def boot(self, payload):
        self.registered()
        content = base64.b64decode(payload['base64'], validate=True)
        require(len(content) <= 1024 * 1024 and hashlib.sha256(content).hexdigest() == payload['sha256'], '引导文件摘要不符')
        script = '#!ipxe\ndhcp\nchain http://192.168.201.10:18090/v1/boot?sn=${serial}&arch=x86_64&firmware=bios || exit\n'
        for name, data in [('undionly.kpxe', content), ('pq25.ipxe', script.encode())]:
            path = self.base / 'tftp' / name
            with path.open('xb') as output:
                output.write(data)
            path.chmod(0o644)
        self.permissions(boot=True)
        return {name: sha256(self.base / 'tftp' / name) for name in ('undionly.kpxe', 'pq25.ipxe')}

    def activate_network(self):
        self.registered()
        self.permissions(boot=True)
        body = read_frozen_xml(self.base / 'service-interface.xml', definitions(self.owner)['service-interface.xml'], 'interface')
        check_service_pair(self.dump('live'), self.dump('inactive'), body, 'interface', before_attach=True)
        require(NETWORK not in self.virsh('net-list', '--name').splitlines(), '新网已经活动，拒绝重跑')
        before = json.loads(command(['nft', '-j', 'list', 'ruleset']))
        require('nf_tables' in command(['iptables', '--version']) and 'nf_tables' in command(['ip6tables', '--version']),
                '宿主存在未覆盖的旧规则后端，拒绝联网')
        old_networks = {name: self.virsh('net-dumpxml', name, '--inactive')
                        for name in self.virsh('net-list', '--all', '--name').splitlines() if name and name != NETWORK}
        write_once(self.records / 'other-networks-before.json', json.dumps(old_networks, indent=2) + '\n')
        require(not any(x.get('table', {}).get('name') == 'lober_pq25' for x in before['nftables']), '隔离表已存在')
        write_once(self.records / 'rules-before.json', json.dumps(before, indent=2) + '\n')
        policy = isolation_policy('host', BRIDGE)
        self.step('install-host-isolation', lambda: command(['nft', '-j', '-f', '-'], json.dumps(policy)))
        check_policy(json.loads(command(['nft', '-j', 'list', 'table', 'inet', 'lober_pq25'])), policy)
        self.step('activate-network', lambda: self.virsh('net-start', NETWORK))
        check_network(self.virsh('net-dumpxml', NETWORK), self.owner, active=True)
        actual = apply_attachment(self, body, 'interface', self.records)
        after = json.loads(command(['nft', '-j', 'list', 'ruleset']))
        write_once(self.records / 'rules-after.json', json.dumps(after, indent=2) + '\n')
        check_existing_rules_preserved(before, after, 'lober_pq25')
        new_networks = {name: self.virsh('net-dumpxml', name, '--inactive')
                        for name in self.virsh('net-list', '--all', '--name').splitlines() if name and name != NETWORK}
        write_once(self.records / 'other-networks-after.json', json.dumps(new_networks, indent=2) + '\n')
        require(old_networks == new_networks, '其他网络定义发生变化')
        return actual

    def resume_network_snapshot(self, contract):
        directory = self.base / contract['failed_records']
        require(directory.is_dir() and not directory.is_symlink(), '失败原件目录不合法')
        marker, failed = self.base / 'failure.json', directory / 'failure.json'
        require(private_json(marker)['phase'] == 'network' and private_json(failed)['phase'] == 'network', '失败不属网络准备阶段')
        return {'owner': self.owner, 'failed_records': directory.name,
            'failure_sha256': sha256(marker), 'failed_record_sha256': sha256(failed),
            'network_active': NETWORK in self.virsh('net-list', '--name').splitlines(),
            'active_network_xml': self.virsh('net-dumpxml', NETWORK),
            'inactive_network_xml': self.virsh('net-dumpxml', NETWORK, '--inactive'),
            'service_live_xml': self.dump('live'), 'service_inactive_xml': self.dump('inactive'),
            'target_state': self.virsh('domstate', self.owner['target_uuid']),
            'target_xml': self.virsh('dumpxml', self.owner['target_uuid'], '--inactive'),
            'target_disk': self.disk('linux01', blank=True),
            'host_policy': json.loads(command(['nft', '-j', 'list', 'table', 'inet', 'lober_pq25'])),
            'step_flags': {'isolation_recorded': (directory / 'install-host-isolation-actual.json').is_file(),
                'network_recorded': (directory / 'activate-network-actual.json').is_file(),
                'attachment_started': (directory / 'attachment-before.json').exists()}}

    def resume_network(self, contract):
        self.registered()
        require(shutil.disk_usage(self.base.parent).free >= 38 * GIB, '继续前宿主仍须满足原 38GiB 准入')
        snapshot = self.resume_network_snapshot(contract)
        write_once(self.records / 'resume-before.json', json.dumps(snapshot, indent=2) + '\n')
        check_network_resume(snapshot, contract, self.owner)
        directory = self.base / contract['failed_records']
        before_rules = private_json(directory / 'rules-before.json')
        current_rules = json.loads(command(['nft', '-j', 'list', 'ruleset']))
        check_existing_rules_preserved(before_rules, current_rules, 'lober_pq25')
        old_networks = private_json(directory / 'other-networks-before.json')
        current_networks = {name: self.virsh('net-dumpxml', name, '--inactive')
                            for name in self.virsh('net-list', '--all', '--name').splitlines() if name and name != NETWORK}
        require(old_networks == current_networks, '失败后其他网络定义发生变化')
        self.permissions(boot=True)
        body = read_frozen_xml(self.base / 'service-interface.xml', definitions(self.owner)['service-interface.xml'], 'interface')
        # 仅补尚未做过的附加；不安装规则、不 net-start、不重建或格式化盘。
        actual = apply_attachment(self, body, 'interface', self.records)
        check_network(self.virsh('net-dumpxml', NETWORK), self.owner, active=True)
        check_policy(json.loads(command(['nft', '-j', 'list', 'table', 'inet', 'lober_pq25'])), isolation_policy('host', BRIDGE))
        after_rules = json.loads(command(['nft', '-j', 'list', 'ruleset']))
        write_once(self.records / 'resume-rules-after.json', json.dumps(after_rules, indent=2) + '\n')
        check_existing_rules_preserved(before_rules, after_rules, 'lober_pq25')
        recovery = {'owner': self.owner, 'scope': contract['scope'], 'state': 'completed',
            'contract_sha256': canonical_sha256(contract), 'actual': actual,
            'resolved_failure_sha256': [contract['fingerprints']['failure_sha256'], contract['fingerprints']['failed_record_sha256']],
            'records': self.records.name, 'old_networks_unchanged': True}
        write_once(self.base / ('recovery-network-' + str(time.time_ns()) + '.json'), json.dumps(recovery, indent=2) + '\n')
        return recovery

    def start(self, proof):
        self.registered()
        require(proof['owner'] == self.owner and 0 <= time.time() - proof['at'] <= 10, 'VM 隔离检查证明失效')
        check_policy(proof['actual_policy'], isolation_policy('vm', proof['interface']))
        require(proof['interface_mac'] == SERVICE_MAC, 'VM 隔离不是本轮网卡')
        require(self.virsh('domstate', self.owner['target_uuid']) == 'shut off', '目标已运行')
        require(NETWORK in self.virsh('net-list', '--name').splitlines(), '活动网络缺失')
        check_network(self.virsh('net-dumpxml', NETWORK), self.owner, active=True)
        check_policy(json.loads(command(['nft', '-j', 'list', 'table', 'inet', 'lober_pq25'])), isolation_policy('host', BRIDGE))
        record = private_json(self.base / 'registration.json')
        current = self.disk('linux01', blank=True)
        require(current['info'] == record['disks']['linux01']['info'] and current['map'] == record['disks']['linux01']['map'], '新客户盘已变化')
        return self.step('start-target', lambda: self.virsh('start', self.owner['target_uuid']))

    def stop(self):
        names = self.virsh('list', '--all', '--uuid').splitlines()
        if self.owner['target_uuid'] not in names:
            return {'client_stopped': True, 'reason': 'owned UUID absent', 'owner': self.owner}
        raw = self.virsh('dumpxml', self.owner['target_uuid'], '--inactive')
        check_target(raw, self.owner, self.owner, self.disk('linux01'))
        state = self.virsh('domstate', self.owner['target_uuid'])
        if state != 'shut off':
            try:
                self.virsh('destroy', self.owner['target_uuid'])
            finally:
                self.snapshot('stop-actual')
        require(self.virsh('domstate', self.owner['target_uuid']) == 'shut off', '未证明本轮客户写入停止')
        return {'client_stopped': True, 'state': 'shut off', 'owner': self.owner, 'failed_disks_preserved': True}

    def run(self, argument=None):
        try:
            action = {'define': self.define, 'attach-data': self.attach_disk,
                'network': self.activate_network, 'stop': self.stop, 'probe': self.probe,
                'probe-data': lambda: self.probe(blank=False)}
            result = (self.resume_network(argument) if self.phase == 'resume-network' else self.start(argument) if self.phase == 'start' else self.boot(argument)
                      if self.phase == 'boot' else action[self.phase]())
            self.snapshot('phase-complete')
            write_once(self.records / 'operation-completed.json', json.dumps({'owner': self.owner, 'writers_stopped': True}) + '\n')
            print(json.dumps({'status': 'COMPLETED', 'phase': self.phase, 'owner': self.owner, 'result': result}), flush=True)
            return result
        except BaseException as error:
            for number in (signal.SIGTERM, signal.SIGHUP, signal.SIGINT):
                signal.signal(number, signal.SIG_IGN)
            stopped = local_jobs_stopped()
            write_once(self.records / 'operation-writers-stopped.json', json.dumps({'owner': self.owner, 'writers_stopped': stopped}) + '\n')
            partial = self.snapshot('phase-failed')
            failure = {'phase': self.phase, 'owner': self.owner, 'safe_to_retry': False,
                       'error': type(error).__name__, 'actual': partial}
            write_once(self.records / 'failure.json', json.dumps(failure, indent=2) + '\n')
            marker = self.base / 'failure.json'
            if not marker.exists():
                write_once(marker, json.dumps(failure, indent=2) + '\n')
            raise


def process_identity(pid):
    proc = Path('/proc') / str(pid)
    tail = (proc / 'stat').read_text().rsplit(')', 1)[1].split()
    return {'pid': pid, 'starttime': tail[19], 'pgrp': int(tail[2]),
            'uid': proc.stat().st_uid, 'cmdline_sha256': sha256(proc / 'cmdline'),
            'vm_boot_id': Path('/proc/sys/kernel/random/boot_id').read_text().strip()}


def stop_owned_operations(base, owner):
    for started in Path(base).glob('records-*/operation-started.json'):
        record = private_json(started)
        require(record['owner'] == owner, '操作记录 owner 不符')
        if record['phase'] == 'stop':
            continue
        if (started.parent / 'operation-completed.json').exists():
            continue
        try:
            actual = process_identity(record['pid'])
        except FileNotFoundError:
            continue
        require(all(actual[key] == record[key] for key in ('pid', 'starttime', 'uid', 'cmdline_sha256', 'vm_boot_id')),
                '操作进程身份变化，不能停止其他进程')
        require(actual['uid'] == 0, '宿主操作进程用户不符')
        os.kill(record['pid'], signal.SIGTERM)
        deadline = time.monotonic() + 45
        while time.monotonic() < deadline:
            acknowledgment = started.parent / 'operation-writers-stopped.json'
            if acknowledgment.is_file() and private_json(acknowledgment).get('writers_stopped') is True:
                break
            try:
                process_identity(record['pid'])
            except FileNotFoundError:
                break
            time.sleep(.2)
        else:
            raise ValueError('未证明本轮宿主操作写入停止')
    return True


def stop_owned_client(owner):
    require(socket.gethostname() == 'ser866667880745', '宿主身份不符')
    base = Path(BASE)
    found = command(['virsh', '-c', 'qemu:///system', 'list', '--all', '--uuid']).splitlines()
    if base.exists():
        require(not base.is_symlink() and private_json(base / '.owner.json') == owner, '停止前 owner 不符')
        stop_owned_operations(base, owner)
    if owner['target_uuid'] not in found:
        return {'client_stopped': True, 'owner': owner, 'reason': 'owned base and UUID both absent'}
    require(base.is_dir() and not base.is_symlink(), '目录缺失但目标存在，不能冒认并停机')
    return HostPreparation(owner, 'stop', 0).run()


def host_script_source(owner, phase):
    constants = ('PURPOSE', 'BASE', 'VM_BASE', 'TARGET', 'NETWORK', 'BRIDGE', 'SN', 'MAC',
                 'SERVICE_MAC', 'SERIAL', 'DATA_SERIAL', 'SERVICE_UUID', 'GIB', 'HOST_SNAPSHOT')
    source = 'import base64,hashlib,ipaddress,json,os,re,shutil,signal,socket,stat,subprocess,sys,time,uuid\nfrom pathlib import Path\nimport xml.etree.ElementTree as ET\n'
    source += '\n'.join(name + '=' + repr(globals()[name]) for name in constants) + '\n'
    source += "SUBNET=ipaddress.ip_network('192.168.201.0/24')\nCOMMAND_RECORDS=None\nACTIVE_CHILDREN={}\n"
    helpers = (require, sha256, owner_valid, target_spec, resolve_target, check_disk, check_target, check_preflight,
        definitions, private_json, write_once, command, initialize_storage_dirs, xml_signature, check_network,
        validate_attachment, read_frozen_xml, service_device, check_service_pair, check_service_delta,
        apply_attachment, isolation_policy, normalized_policy, check_policy, check_existing_rules_preserved,
        canonical_sha256, network_resume_fingerprints, check_network_resume, recorded_failures_resolved, process_identity, local_jobs_stopped,
        HostPreparation, stop_owned_operations, stop_owned_client)
    source += '\n\n'.join(inspect.getsource(helper) for helper in helpers)
    source += '\nos.umask(0o077)\nowner=' + repr(owner) + '\n'
    source += "def interrupted(number,frame):raise InterruptedError('preparation interrupted '+str(number))\n"
    source += 'for number in (signal.SIGTERM,signal.SIGHUP,signal.SIGINT):signal.signal(number,interrupted)\n'
    if phase == 'stop':
        source += "print(json.dumps({'result':stop_owned_client(owner)}))\n"
    else:
        source += 'runtime=HostPreparation(owner,' + repr(phase) + ',int(sys.argv[1]))\n'
        source += "runtime.run(json.loads(sys.argv[2]) if len(sys.argv)>2 else None)\n"
    return source


def vm_network_isolation(owner):
    require(socket.gethostname() == 'lober' and os.geteuid() == 0, '只能从本轮 VM 核准入口配置新接口')
    root = Path(VM_BASE) / 'machines'
    output = root / ('vm-network-' + str(time.time_ns()))
    output.mkdir(mode=0o700)
    global COMMAND_RECORDS
    COMMAND_RECORDS = output
    links = json.loads(command(['ip', '-j', 'address']))
    matches = [link for link in links if link.get('address', '').lower() == SERVICE_MAC]
    require(len(matches) == 1, '本轮新 MAC 不是唯一网卡')
    link = matches[0]
    require('UP' not in link['flags'] and not link.get('addr_info'), '新网卡已启用，先独立审查当前暴露情况')
    before = command(['ip', '-j', 'route', 'show', 'table', 'all'])
    write_once(output / 'interfaces-before.json', json.dumps(links, indent=2) + '\n')
    before_rules = json.loads(command(['nft', '-j', 'list', 'ruleset']))
    write_once(output / 'rules-before.json', json.dumps(before_rules, indent=2) + '\n')
    require(not any(entry.get('table', {}).get('name') == 'lober_pq25_vm' for entry in before_rules['nftables']), 'VM 本轮隔离表已存在，禁止直接重跑')
    policy = isolation_policy('vm', link['ifname'])
    # 在启用网卡前原子添加本轮独立表；表已存在便拒绝。
    command(['nft', '-j', '-f', '-'], json.dumps(policy))
    actual = json.loads(command(['nft', '-j', 'list', 'table', 'inet', 'lober_pq25_vm']))
    check_policy(actual, policy)
    write_once(output / 'policy-confirmed-before-link-up.json', json.dumps(actual, indent=2) + '\n')
    try:
        command(['ip', 'address', 'add', '192.168.201.10/24', 'dev', link['ifname']])
        command(['ip', 'link', 'set', 'dev', link['ifname'], 'up'])
    finally:
        write_once(output / 'interfaces-after.json', command(['ip', '-j', 'address']) + '\n')
        write_once(output / 'routes-after.json', command(['ip', '-j', 'route', 'show', 'table', 'all']) + '\n')
        write_once(output / 'rules-after.json', command(['nft', '-j', 'list', 'ruleset']) + '\n')
    old_routes = [r for r in json.loads(before) if r.get('dev') != link['ifname']]
    after_routes = [r for r in json.loads(command(['ip', '-j', 'route', 'show', 'table', 'all'])) if r.get('dev') != link['ifname']]
    require(old_routes == after_routes, '旧路由发生变化')
    return {'owner': owner, 'interface': link['ifname'], 'interface_mac': SERVICE_MAC,
            'at': time.time(), 'actual_policy': actual, 'old_routes_unchanged': True}


def format_new_media(owner, proof):
    require(socket.gethostname() == 'lober' and os.geteuid() == 0, '只能在固定服务 VM 格式化核准新盘')
    require(proof['owner'] == owner and proof['service_uuid'] == SERVICE_UUID
            and 0 <= time.time() - proof['at'] <= 15, '本轮实际附盘证明缺失或过期')
    check_service_pair(proof['live'], proof['inactive'], definitions(owner)['data-disk.xml'], 'disk', before_attach=False)
    check_disk(proof['data_disk'], blank=True)
    require(Path('/sys/class/dmi/id/product_uuid').read_text().strip().lower() == SERVICE_UUID, '当前 VM 的硬件 UUID 不符')
    require(shutil.disk_usage('/').free >= GIB, 'VM 根盘不足 1GiB')
    device = '/dev/vdb'
    inventory = json.loads(command(['lsblk', '--json', '--bytes', '--output', 'NAME,PATH,SIZE,TYPE,SERIAL,MOUNTPOINTS', device]))['blockdevices']
    require(len(inventory) == 1, '新盘映射不唯一')
    disk = inventory[0]
    require(disk['path'] == device and disk['type'] == 'disk' and disk['size'] == 16 * GIB
            and disk['serial'] == DATA_SERIAL, '当前 vdb 不是核准新盘')
    require(not disk.get('children') and not any(disk.get('mountpoints') or []), '新盘已有分区或挂载')
    require(not list(Path('/sys/class/block/vdb/holders').iterdir()), '新盘有持有者')
    require(not json.loads(command(['wipefs', '--no-act', '--json', device])).get('signatures'), '新盘已有签名')
    mount = Path(VM_BASE) / 'machines/media'
    require(not mount.exists() and not mount.is_symlink(), '挂载目标已存在')
    for ancestor in [mount.parent, *mount.parent.parents]:
        require(not ancestor.is_symlink(), '挂载点祖先有符号链接')
    require(mount.resolve().is_relative_to(Path(VM_BASE) / 'machines'), '挂载点逃出本轮目录')
    with open(device, 'rb', buffering=0) as source:
        remaining, zeros = 16 * GIB, bytes(1024 * 1024)
        while remaining:
            block = source.read(min(len(zeros), remaining))
            require(block and block == zeros[:len(block)], '新盘不全空')
            remaining -= len(block)
            require(shutil.disk_usage('/').free >= GIB, '读空盘时 VM 根盘不足 1GiB')
    mount.mkdir(mode=0o700)
    try:
        command(['mkfs.ext4', '-L', 'PQ25_MEDIA01', device], timeout=180)
        command(['mount', '-o', 'nodev,nosuid', device, str(mount)])
        require(command(['findmnt', '-n', '-o', 'SOURCE', '--target', str(mount)]).strip() == device, '实际挂载源不符')
        write_once(mount / '.owner', PURPOSE + '\n')
        command(['chown', 'dev:dev', str(mount), str(mount / '.owner')])
        return {'device': device, 'serial': DATA_SERIAL, 'mount': str(mount),
                'filesystem_uuid': command(['blkid', '-s', 'UUID', '-o', 'value', device]).strip()}
    except BaseException:
        # 只收尾本轮新挂载；保留失败文件系统和目录，不重做格式化。
        raw = subprocess.run(['findmnt', '-n', '-o', 'SOURCE', '--mountpoint', str(mount)], capture_output=True, text=True)
        if raw.returncode == 0:
            require(raw.stdout.strip() == device, '收尾发现其他挂载，拒绝操作')
            command(['umount', str(mount)])
        command(['blockdev', '--flushbufs', device])
        require(Path('/sys/class/block/vdb/inflight').read_text().split() == ['0', '0'], '新盘仍有未完成写入')
        raise


def quiesce_new_media(owner, proof):
    require(socket.gethostname() == 'lober' and os.geteuid() == 0, '只能停止固定 VM 的本轮数据盘写入')
    require(proof['owner'] == owner and proof['service_uuid'] == SERVICE_UUID
            and 0 <= time.time() - proof['at'] <= 20, '实际附盘停止证明失效')
    check_service_pair(proof['live'], proof['inactive'], definitions(owner)['data-disk.xml'], 'disk', before_attach=False)
    require(Path('/sys/class/dmi/id/product_uuid').read_text().strip().lower() == SERVICE_UUID, 'VM UUID 不符')
    require(command(['lsblk', '-n', '-o', 'SERIAL', '/dev/vdb']).strip() == DATA_SERIAL, '停止目标不是新数据盘')
    mount = Path(VM_BASE) / 'machines/media'
    for ancestor in [mount, *mount.parents]:
        require(not ancestor.is_symlink(), '停止挂载点有符号链接')
    mounted = subprocess.run(['findmnt', '-n', '-o', 'SOURCE', '--mountpoint', str(mount)], capture_output=True, text=True)
    if mounted.returncode == 0:
        require(mounted.stdout.strip() == '/dev/vdb', '停止时挂载源不是本轮数据盘')
        command(['umount', str(mount)])
    command(['blockdev', '--flushbufs', '/dev/vdb'])
    require(Path('/sys/class/block/vdb/inflight').read_text().split() == ['0', '0'], '新数据盘仍在写入')
    return {'data_writes_stopped': True, 'serial': DATA_SERIAL, 'failed_disk_preserved': True}


def preparation_entry(config_path, manifest_path, phase, output):
    global COMMAND_RECORDS
    require(phase in ('define', 'attach-data', 'format', 'boot', 'network', 'resume-network', 'vm-network', 'probe'), '准备包不能启动或安装客户')
    require(socket.gethostname() == 'lober', '准备编排必须从 lober VM 发起')
    os.umask(0o077)
    config, frozen = private_json(config_path), private_json(manifest_path)
    owner = frozen['owner']
    owner_valid(owner)
    require(sha256(__file__) == frozen['executor_sha256'], '冻结准备执行器已变化')
    require(frozen['scope'] == 'preparation-only' and frozen['target'] == target_spec(owner), '准备范围不符')
    require(phase in frozen['phase_scripts'], '此冻结包未包含该阶段')
    review = frozen['review']
    require(review['approved'] is True and isinstance(review['root_reviewer'], str)
            and isinstance(review['independent_reviewer'], str) and bool(review['root_reviewer'])
            and bool(review['independent_reviewer']) and review['root_reviewer'] != review['independent_reviewer'], '准备阶段未获根和独立审查核准')
    require(private_json(config['identity_file']) == owner, '准备 owner 与私有身份不符')
    output = Path(output).resolve()
    require(output.is_relative_to(Path(VM_BASE) / 'machines') and not output.exists(), '输出必须是本轮新目录')
    output.mkdir(mode=0o700)
    COMMAND_RECORDS = output
    def host(phase_name, payload=None):
        path = Path(config['scripts_dir']) / frozen['phase_scripts'][phase_name]
        require(not path.is_symlink() and sha256(path) == frozen['scripts'][path.name], '准确准备脚本已变化')
        arguments = ['cloud-preparation', str(shutil.disk_usage('/').free)]
        if payload is not None:
            arguments.append(json.dumps(payload))
        source = 'import sys\nsys.argv=' + repr(arguments) + '\n' + path.read_text()
        raw = host_command(config, source)
        write_once(output / ('host-' + phase_name + '-' + str(time.time_ns()) + '.log'), raw)
        return json.loads(raw.strip().splitlines()[-1])['result']
    def stop():
        local_stopped = local_jobs_stopped()
        value = host('stop')
        while isinstance(value, dict) and 'result' in value:
            value = value['result']
        data_stopped = True
        if phase == 'format' and local_stopped:
            proof = host('probe-data')
            path = Path(config['scripts_dir']) / frozen['phase_scripts']['quiesce']
            require(not path.is_symlink() and sha256(path) == frozen['scripts'][path.name], '新数据盘停止脚本变化')
            source = 'import sys\nsys.argv=' + repr(['vm-quiesce', json.dumps(proof)]) + '\n' + path.read_text()
            data_stopped = json.loads(command(['sudo', '-n', 'python3', '-'], source))['data_writes_stopped'] is True
        return {'client_stopped': value.get('client_stopped') is True and local_stopped and data_stopped, 'imports_stopped': True,
                'local_jobs_stopped': local_stopped,
                'reason': 'preparation starts no import process', 'host': value}
    lease = SafetyLease(Path(VM_BASE) / 'leases/heavy.lock', output, stop)
    required_phases = {phase if phase in frozen['phase_scripts'] else 'probe', 'stop'}
    if phase == 'format':
        required_phases.update(('probe', 'probe-data', 'quiesce'))
    for phase_name in required_phases:
        path = Path(config['scripts_dir']) / frozen['phase_scripts'][phase_name]
        require(path.is_file() and not path.is_symlink() and sha256(path) == frozen['scripts'][path.name], '缺少准确准备/停止脚本')
    failed = True
    result = {'phase': phase, 'status': 'FAIL', 'scope': 'preparation-only', 'owner': owner,
              'manifest_sha256': sha256(manifest_path), 'executor_sha256': sha256(__file__)}
    try:
        with lease:
            if phase == 'define':
                ports = [int(row.split()[4].rsplit(':', 1)[1]) for row in command(['ss', '-H', '-lntu']).splitlines()]
                require(not set(ports).intersection((18090, 18082)), 'VM 的新端口已被占用')
                links = json.loads(command(['ip', '-j', 'address']))
                for link in links:
                    require(link.get('address', '').lower() != SERVICE_MAC, 'VM 新网卡 MAC 已存在')
                    for address in link.get('addr_info', []):
                        network = ipaddress.ip_network(address['local'] + '/' + str(address['prefixlen']), strict=False)
                        require(network.version != 4 or not network.overlaps(SUBNET), 'VM 新网段与旧地址冲突')
            if phase == 'format':
                proof = host('probe')
                source = (Path(config['scripts_dir']) / frozen['phase_scripts']['format']).read_text()
                source = 'import sys\nsys.argv=' + repr(['vm-format', json.dumps(proof)]) + '\n' + source
                write_once(output / 'format-proof-private.json', json.dumps(proof, indent=2) + '\n')
                result['actual'] = json.loads(command(['sudo', '-n', 'python3', '-'], source, timeout=360))
            elif phase == 'vm-network':
                source = (Path(config['scripts_dir']) / frozen['phase_scripts']['vm-network']).read_text()
                result['actual'] = json.loads(command(['sudo', '-n', 'python3', '-'], source))
            elif phase == 'boot':
                boot = frozen['ipxe_bootstrap']
                require(sha256(boot['path']) == boot['sha256'], '本轮引导输入变化')
                result['actual'] = host('boot', {'sha256': boot['sha256'], 'base64': base64.b64encode(Path(boot['path']).read_bytes()).decode()})
            else:
                contract = (bound_json(config['resume_contract_file'], frozen['resume_contract_sha256'])
                            if phase == 'resume-network' else None)
                result['actual'] = host(phase, contract)
            failed, result['status'] = False, 'COMPLETED'
    except BaseException as error:
        result['error'] = type(error).__name__
        raise
    finally:
        result['stopped_before_unlock_on_failure'] = failed
        write_once(output / 'result.json', json.dumps(result, indent=2) + '\n')


def vm_script_source(owner, phase):
    constants = ('PURPOSE', 'BASE', 'VM_BASE', 'TARGET', 'NETWORK', 'BRIDGE', 'SN', 'MAC',
                 'SERVICE_MAC', 'SERIAL', 'DATA_SERIAL', 'SERVICE_UUID', 'GIB')
    source = 'import base64,hashlib,ipaddress,json,os,re,shutil,signal,socket,stat,subprocess,sys,time,uuid\nfrom pathlib import Path\nimport xml.etree.ElementTree as ET\n'
    source += '\n'.join(name + '=' + repr(globals()[name]) for name in constants) + '\nCOMMAND_RECORDS=None\nACTIVE_CHILDREN={}\n'
    helpers = (require, sha256, owner_valid, definitions, private_json, write_once, command, xml_signature,
        validate_attachment, service_device, check_service_pair, check_disk, isolation_policy,
        normalized_policy, check_policy, vm_network_isolation, format_new_media, quiesce_new_media)
    source += '\n\n'.join(inspect.getsource(helper) for helper in helpers)
    source += '\nos.umask(0o077)\nowner=' + repr(owner) + '\n'
    source += "def interrupted(number,frame):raise InterruptedError('VM preparation interrupted '+str(number))\n"
    source += 'for number in (signal.SIGTERM,signal.SIGHUP,signal.SIGINT):signal.signal(number,interrupted)\n'
    if phase == 'format':
        source += "records=Path(VM_BASE)/'machines'/('format-records-'+str(time.time_ns()))\nrecords.mkdir(mode=0o700)\nCOMMAND_RECORDS=records\n"
        source += "signal.signal(signal.SIGALRM,interrupted)\nsignal.alarm(240)\n"
        source += 'print(json.dumps(format_new_media(owner,json.loads(sys.argv[1]))))\n'
    elif phase == 'quiesce':
        source += 'print(json.dumps(quiesce_new_media(owner,json.loads(sys.argv[1]))))\n'
    else:
        source += 'print(json.dumps(vm_network_isolation(owner)))\n'
    return source


def mutation_scripts(owner):
    scripts = {}
    for name, phase in [('01-host-define.py', 'define'), ('02-host-attach-data.py', 'attach-data'),
                        ('04-host-network.py', 'network'), ('04a-host-boot.py', 'boot'),
                        ('04c-host-resume-network.py', 'resume-network'),
                        ('05-host-start.py', 'start'), ('00-host-safe-stop.py', 'stop'), ('02b-host-probe.py', 'probe'),
                        ('02c-host-data-proof.py', 'probe-data')]:
        scripts[name] = host_script_source(owner, phase)
    scripts['03-vm-format-data.py'] = vm_script_source(owner, 'format')
    scripts['03b-vm-stop-data.py'] = vm_script_source(owner, 'quiesce')
    scripts['04b-vm-network.py'] = vm_script_source(owner, 'vm-network')
    scripts['isolation-host.json'] = json.dumps(isolation_policy('host', BRIDGE), indent=2) + '\n'
    scripts['isolation-vm-template.json'] = json.dumps(isolation_policy('vm', 'FILL_INTERFACE'), indent=2) + '\n'
    return scripts



def stage_boot(config, manifest):
    bootstrap = manifest['cloud']['ipxe_bootstrap']
    source = Path(bootstrap['path'])
    require(source.is_file() and not source.is_symlink() and source.stat().st_size <= 1024 * 1024
            and sha256(source) == bootstrap['sha256'], '实际 iPXE 引导文件摘要不符')
    owner = manifest['cloud']['owner']
    ipxe = '''#!ipxe
dhcp
chain http://192.168.201.10:18090/v1/boot?sn=${serial}&arch=x86_64&firmware=bios || exit
'''
    script = '''import base64,hashlib,json,pathlib,socket
def need(ok,message):
 if not ok:raise RuntimeError(message)
need(socket.gethostname()=='ser866667880745','unexpected host')
base=pathlib.Path('/var/lib/libvirt/images/lober-pq-20261025')
'''
    script += 'owner=' + repr(owner) + '\n'
    script += 'encoded=' + repr(base64.b64encode(source.read_bytes()).decode()) + '\n'
    script += 'expected=' + repr(bootstrap['sha256']) + '\n'
    script += 'ipxe=' + repr(ipxe) + '\n'
    script += '''need(not base.is_symlink() and json.loads((base/'.owner.json').read_text())==owner,'owner mismatch')
tftp=base/'tftp';need(tftp.is_dir() and not tftp.is_symlink(),'invalid tftp root')
content=base64.b64decode(encoded,validate=True)
need(hashlib.sha256(content).hexdigest()==expected,'bootstrap changed')
for name,body in [('undionly.kpxe',content),('pq25.ipxe',ipxe.encode())]:
 p=tftp/name
 with p.open('xb') as output:output.write(body)
 p.chmod(0o644)
print(json.dumps({p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in [tftp/'undionly.kpxe',tftp/'pq25.ipxe']}))
'''
    return host_command(config, script)


def private_json(path):
    path = Path(path)
    require(not path.is_symlink() and path.is_file(), '私有配置必须为普通文件')
    require(stat.S_IMODE(path.stat().st_mode) & 0o077 == 0, '私有配置权限必须为 0600 或更严格')
    return json.loads(path.read_text())


def write_once(path, content):
    path=Path(path)
    descriptor=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
    with os.fdopen(descriptor,'w',encoding='utf-8') as output:
        output.write(content);output.flush();os.fsync(output.fileno())
    directory=os.open(path.parent,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
    try:os.fsync(directory)
    finally:os.close(directory)


def command(args, stdin=None, timeout=30, env=None):
    identifier = str(time.time_ns())
    record = {'argv': args, 'stdin_sha256': hashlib.sha256(stdin.encode()).hexdigest() if stdin is not None else None,
              'state': 'STARTED', 'at': int(time.time())}
    if env is not None:
        record['environment_sha256'] = canonical_sha256(env)
    if COMMAND_RECORDS is not None:
        write_once(COMMAND_RECORDS / ('command-' + identifier + '-begin.json'), json.dumps(record, indent=2) + '\n')
    process, failure = None, None
    stdout, stderr, code = '', '', None
    try:
        process = subprocess.Popen(args, stdin=subprocess.PIPE if stdin is not None else subprocess.DEVNULL,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, start_new_session=True, env=env)
        ACTIVE_CHILDREN[process.pid] = process
        stdout, stderr = process.communicate(stdin, timeout=timeout)
        code, record['state'] = process.returncode, 'EXITED'
    except BaseException as error:
        failure = error
        record['state'] = 'TIMEOUT' if isinstance(error, subprocess.TimeoutExpired) else 'INTERRUPTED'
        record['error_type'] = type(error).__name__
        if process is not None:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            except PermissionError:
                killed = subprocess.run(['sudo', '-n', '/bin/kill', '-KILL', '--', '-' + str(process.pid)],
                    capture_output=True, text=True, timeout=10)
                record['privileged_child_stop_exit'] = killed.returncode
            try:
                stdout, stderr = process.communicate(timeout=15)
            except subprocess.TimeoutExpired as pending:
                stdout = pending.stdout.decode(errors='replace') if isinstance(pending.stdout, bytes) else (pending.stdout or '')
                stderr = pending.stderr.decode(errors='replace') if isinstance(pending.stderr, bytes) else (pending.stderr or '')
                record['child_stop_unconfirmed'] = True
            code = process.returncode
    finally:
        record.update(exit=code, stdout=stdout, stderr=stderr, finished_at=int(time.time()))
        if process is not None and process.poll() is not None:
            ACTIVE_CHILDREN.pop(process.pid, None)
        if COMMAND_RECORDS is not None:
            write_once(COMMAND_RECORDS / ('command-' + identifier + '-end.json'), json.dumps(record, indent=2) + '\n')
    if failure is not None:
        raise failure
    require(code == 0, '外部命令失败；原件已保留：' + Path(args[0]).name)
    return stdout


def local_jobs_stopped():
    for pid, process in list(ACTIVE_CHILDREN.items()):
        if process.poll() is not None:
            ACTIVE_CHILDREN.pop(pid, None)
            continue
        try:
            os.killpg(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        except PermissionError:
            subprocess.run(['sudo', '-n', '/bin/kill', '-KILL', '--', '-' + str(pid)],
                           capture_output=True, timeout=10)
        try:process.wait(timeout=15)
        except subprocess.TimeoutExpired:continue
        if process.poll() is not None:
            ACTIVE_CHILDREN.pop(pid, None)
    return not ACTIVE_CHILDREN


def stream_command(args, script, path, timeout):
    """抓包只写私有文件；命令日志只留摘要，不把包内容塞进日志。"""
    path = Path(path)
    require(not path.exists() and not path.is_symlink() and path.parent.stat().st_uid == os.getuid()
            and stat.S_IMODE(path.parent.stat().st_mode) == 0o700, '二进制输出须在本轮私有新文件')
    identifier = str(time.time_ns())
    record = {'argv': args, 'stdin_sha256': hashlib.sha256(script.encode()).hexdigest(), 'state': 'STARTED'}
    if COMMAND_RECORDS is not None:write_once(COMMAND_RECORDS / ('stream-' + identifier + '-begin.json'), json.dumps(record))
    process = None
    try:
        with path.open('xb') as sink, (path.parent / (path.name + '.transport.stderr')).open('xb') as errors:
            path.chmod(0o600);os.fchmod(errors.fileno(),0o600)
            process = subprocess.Popen(args, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=errors, start_new_session=True)
            ACTIVE_CHILDREN[process.pid] = process
            deadline = time.monotonic() + timeout
            with selectors.DefaultSelector() as selector:
                selector.register(process.stdout,selectors.EVENT_READ,'output')
                selector.register(process.stdin,selectors.EVENT_WRITE,'input')
                os.set_blocking(process.stdin.fileno(),False)
                encoded=script.encode();offset=0;finished=False
                while True:
                    require(time.monotonic() < deadline, '抓包传输超时，保留部分文件')
                    for key,_ in selector.select(min(1,max(.01,deadline-time.monotonic()))):
                        if key.data=='input':
                            try:offset+=os.write(process.stdin.fileno(),encoded[offset:offset+65536])
                            except BlockingIOError:continue
                            if offset==len(encoded):selector.unregister(process.stdin);process.stdin.close()
                        else:
                            block=os.read(process.stdout.fileno(),65536)
                            if not block:finished=True;break
                            sink.write(block)
                            require(sink.tell() <= 64*1024**2, '抓包传输超过64MiB，保留部分原件')
                            require(shutil.disk_usage(path.parent).free >= GIB, '抓包传输期间根盘不足1GiB')
                    if finished:
                        require(offset==len(encoded),'宿主提前关闭二进制传输')
                        break
            sink.flush();os.fsync(sink.fileno())
            code=process.wait(timeout=max(.1,deadline-time.monotonic()))
            record.update(state='EXITED',exit=code)
            require(code==0,'宿主抓包传输失败')
        return {'path':str(path),'sha256':sha256(path),'bytes':path.stat().st_size,'exit':0}
    except BaseException as error:
        record.update(state='FAILED',error_type=type(error).__name__)
        if process is not None and process.poll() is None:
            os.killpg(process.pid,signal.SIGKILL);process.wait(timeout=15)
        raise
    finally:
        if process is not None and process.poll() is not None:ACTIVE_CHILDREN.pop(process.pid,None)
        record.update(bytes=path.stat().st_size if path.exists() else 0,sha256=sha256(path) if path.exists() else None)
        if COMMAND_RECORDS is not None:write_once(COMMAND_RECORDS / ('stream-' + identifier + '-end.json'),json.dumps(record))


def host_command(config, script, timeout=150, stream_file=None):
    def invoke(args, seconds):
        return command(args,script,seconds) if stream_file is None else stream_command(args,script,stream_file,seconds)
    if config.get('host_access_file'):
        access_path = Path(config['host_access_file'])
        require(sha256(access_path) == config['host_access_sha256'], '受限宿主连接元数据摘要变化')
        access = private_json(access_path)
        prefix = access['sshPrefix']
        require(prefix[:2] == ['/usr/bin/sshpass', '-f'] and prefix[3] == '/usr/bin/ssh', '只允许已核对的 sshpass 文件入口')
        password = Path(prefix[2])
        private_root = Path(VM_BASE) / 'machines/private-host-access'
        require(password.resolve().is_relative_to(private_root) and password.is_file() and not password.is_symlink()
                and stat.S_IMODE(password.stat().st_mode) == 0o600
                and stat.S_IMODE(password.parent.stat().st_mode) == 0o700, '宿主认证文件归属或权限不符')
        require(prefix[-1] in ('mgr@192.168.100.1', 'root@192.168.100.1'), '禁止改宿主或猜测其他账号')
        options = prefix[4:-1]
        require(len(options) % 2 == 0 and all(options[i] == '-o' for i in range(0, len(options), 2)), '未知 SSH 参数')
        values = dict(v.split('=', 1) for v in options[1::2])
        required = {'BatchMode': 'no', 'StrictHostKeyChecking': 'yes', 'HostKeyAlias': '110.42.44.11',
                    'PreferredAuthentications': 'password', 'PubkeyAuthentication': 'no',
                    'NumberOfPasswordPrompts': '1', 'ConnectTimeout': '6',
                    'UserKnownHostsFile': str(private_root / 'known_hosts')}
        require(values == required and len(values) * 2 == len(options), '宿主严格认证参数与已核准入口不同')
        known_hosts = private_root / 'known_hosts'
        require(known_hosts.is_file() and not known_hosts.is_symlink()
                and stat.S_IMODE(known_hosts.stat().st_mode) == 0o600, '宿主信任文件权限不符')
        return invoke(prefix + ['sudo -n python3 -'],timeout)
    alias = config['host_ssh_alias']
    require(re.fullmatch('[a-zA-Z][a-zA-Z0-9_-]{0,63}', alias), 'SSH 只能使用预先审核的配置别名')
    ssh_config = Path(config['host_ssh_config'])
    private_json(config['identity_file'])
    require(ssh_config.is_file() and not ssh_config.is_symlink() and stat.S_IMODE(ssh_config.stat().st_mode) & 0o077 == 0,
            '宿主 SSH 配置必须私有')
    return invoke(['ssh', '-F', str(ssh_config), '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=yes',
                    '-o', 'IdentitiesOnly=yes', '-o', 'ConnectTimeout=8', alias, 'sudo -n python3 -'],min(timeout,45))


def host_stream(config, script, path, timeout=150):
    return host_command(config,script,timeout,stream_file=path)


host_command.stream_to_private_file = host_stream


def download_plan():
    return {'version': '13.7.0', 'installer_build': '20250803+deb13u7', 'images': [
        {'name': 'mini.iso', 'size': 67108864,
         'sha256': '34e4af5d0aa0886a4b7338dce796b9bc27ef1e6dcf44cdcc4b22c100eb492388',
         'url': 'https://mirrors.ustc.edu.cn/debian/dists/trixie/main/installer-amd64/20250803+deb13u7/images/netboot/mini.iso'},
        {'name': 'debian-13.7.0-amd64-DVD-1.iso', 'size': 3992977408,
         'sha256': '347b6c67a3cc0b7ddb60b178f683470c4e2b7ac426c996d9337a2ff36c1a32d2',
         'url': 'https://mirrors.ustc.edu.cn/debian-cd/13.7.0/amd64/iso-dvd/debian-13.7.0-amd64-DVD-1.iso'}]}


def download_https(url):
    parsed = urllib.parse.urlsplit(url)
    require(parsed.scheme == 'https' and parsed.hostname and not parsed.username and not parsed.password
            and parsed.port in (None, 443) and not parsed.fragment, '下载或跳转地址必须为无凭据 HTTPS')
    return url


def check_download_plan(plan):
    expected = download_plan()
    require(set(plan) == set(expected) and plan['version'] == expected['version']
            and plan['installer_build'] == expected['installer_build'] and len(plan['images']) == 2,
            '仅下载本轮两份 Debian 原件')
    for actual, signed in zip(plan['images'], expected['images']):
        require(set(actual) == set(signed) and all(actual[k] == signed[k] for k in ('name', 'size', 'sha256')),
                '镜像大小或已签摘要不能替换')
        download_https(actual['url'])


def check_download_observation(value, owner, initial, remaining):
    owner_valid(owner)
    require(value['owner'] == owner and value['host_owner'] == owner, '下载 owner 不符')
    require(value['hostname'] == 'lober' and value['vm_uuid'] == SERVICE_UUID
            and value['host'] == 'ser866667880745', '下载 VM 或宿主不符')
    require(value['mount'] == VM_BASE + '/machines/media' and value['source'] == '/dev/vdb'
            and value['filesystem_uuid'] == '7efe0e2b-6fb6-46f7-bc93-1bc0246ee80a'
            and value['serial'] == DATA_SERIAL and value['disk_bytes'] == 16 * GIB
            and value['media_owner'] == PURPOSE and value['filesystem_type'] == 'ext4'
            and {'rw', 'nodev', 'nosuid'}.issubset(value['options']), '不是本轮核准的新专用盘')
    binding = {'source': BASE + '/media01.qcow2', 'target': 'vdb', 'bus': 'virtio',
               'serial': DATA_SERIAL, 'format': 'qcow2'}
    require(value['host_data_bindings'] == [binding, binding], '宿主活动/持久数据盘映射不符')
    require(value['host_free'] >= (38 if initial else 6) * GIB, '宿主空间低于原准入或执行保留线')
    require(value['root_free'] >= GIB and remaining >= 0
            and value['media_free'] >= remaining + 3992977408 + GIB,
            '根盘或专用盘余量不足；须保留完整 DVD 提取空间及 1GiB')


def download_mount_identity(mount):
    mount = Path(mount)
    require(str(mount) == VM_BASE + '/machines/media' and not mount.is_symlink(), '非核准介质挂载点')
    for parent in mount.parents:
        require(not parent.is_symlink(), '挂载点祖先不能为链接')
    rows = [line.split() for line in Path('/proc/self/mountinfo').read_text().splitlines()]
    rows = [row for row in rows if row[4] == str(mount)]
    require(len(rows) == 1, '新盘挂载记录不唯一')
    row = rows[0]; split = row.index('-')
    require(row[split + 1] == 'ext4' and row[split + 2] == '/dev/vdb'
            and {'rw', 'nodev', 'nosuid'}.issubset(row[5].split(',')), '新盘挂载内容变化')
    marker = mount / '.owner'
    require(marker.is_file() and not marker.is_symlink() and marker.stat().st_uid == os.getuid()
            and stat.S_IMODE(marker.stat().st_mode) == 0o600 and marker.read_text().strip() == PURPOSE,
            '专用盘 owner 或权限不符')
    return {'mount_id': row[0], 'device': row[2], 'source': row[split + 2],
            'st_dev': mount.stat().st_dev, 'options': sorted(row[5].split(','))}


def download_local_guard(mount, identity, remaining):
    require(download_mount_identity(mount) == identity, '下载期间专用盘被替换或重新挂载')
    require(shutil.disk_usage('/').free >= GIB, '下载期间根盘低于 1GiB')
    require(remaining >= 0 and shutil.disk_usage(mount).free >= remaining + 3992977408 + GIB,
            '下载期间专用盘不足以保留原件、提取空间及 1GiB')


DOWNLOAD_HOST_READ = r'''
import json,pathlib,shutil,socket,subprocess,xml.etree.ElementTree as ET
base=pathlib.Path('/var/lib/libvirt/images/lober-pq-20261025')
if socket.gethostname()!='ser866667880745':raise RuntimeError('wrong host')
bindings=[]
for suffix in ([],['--inactive']):
 raw=subprocess.check_output(['virsh','-c','qemu:///system','dumpxml','895a44e1-8abd-4463-bbf7-fba8a743ed59',*suffix],text=True,timeout=5)
 disks=[d for d in ET.fromstring(raw).findall('./devices/disk') if d.findtext('serial')=='PQ25_MEDIA01_DISK' or d.find('target') is not None and d.find('target').get('dev')=='vdb']
 if len(disks)!=1:raise RuntimeError('ambiguous data disk')
 d=disks[0]
 bindings.append({'source':d.find('source').get('file'),'target':d.find('target').get('dev'),'bus':d.find('target').get('bus'),'serial':d.findtext('serial'),'format':d.find('driver').get('type')})
marker=base/'.owner.json'
if marker.is_symlink() or not marker.is_file():raise RuntimeError('invalid owner marker')
print(json.dumps({'host':socket.gethostname(),'host_free':shutil.disk_usage(base.parent).free,'host_owner':json.loads(marker.read_text()),'host_data_bindings':bindings}))
'''


def observe_download(config, owner):
    mount = Path(VM_BASE) / 'machines/media'
    identity = download_mount_identity(mount)
    row = json.loads(command(['findmnt', '--json', '--mountpoint', str(mount), '--output', 'TARGET,SOURCE,FSTYPE,UUID,OPTIONS'], timeout=10))['filesystems']
    require(len(row) == 1 and row[0]['target'] == str(mount), '实际挂载不唯一')
    disk = json.loads(command(['lsblk', '--json', '--bytes', '--output', 'PATH,SIZE,TYPE,SERIAL', '/dev/vdb'], timeout=10))['blockdevices']
    require(len(disk) == 1 and disk[0]['path'] == '/dev/vdb' and disk[0]['type'] == 'disk'
            and not disk[0].get('children'), '数据盘不是核准整盘')
    host = json.loads(host_command(config, DOWNLOAD_HOST_READ, timeout=12))
    value = {'owner': owner, 'hostname': socket.gethostname(),
        'vm_uuid': command(['sudo', '-n', 'cat', '/sys/class/dmi/id/product_uuid'], timeout=10).strip().lower(),
        'mount': str(mount), 'source': row[0]['source'], 'filesystem_type': row[0]['fstype'],
        'filesystem_uuid': row[0]['uuid'], 'options': row[0]['options'].split(','),
        'serial': disk[0]['serial'], 'disk_bytes': disk[0]['size'], 'media_owner': (mount / '.owner').read_text().strip(),
        'root_free': shutil.disk_usage('/').free, 'media_free': shutil.disk_usage(mount).free, **host}
    return value, identity


def verify_download_trust(frozen, output):
    files = frozen['trust']
    require(set(files) == {'dvd_sums','dvd_signature','release','release_signature','netboot_sums','cd_keyring','archive_keyring'}, '签名输入不完整')
    for label, entry in files.items():
        path = Path(entry['path'])
        require(path.is_file() and not path.is_symlink() and path.stat().st_size == entry['size']
                and sha256(path) == entry['sha256'], '既有签名链原件变化：' + label)
    dvd = command(['gpgv', '--status-fd', '1', '--keyring', files['cd_keyring']['path'], files['dvd_signature']['path'], files['dvd_sums']['path']], timeout=15)
    release = command(['gpgv', '--status-fd', '1', '--keyring', files['archive_keyring']['path'], files['release_signature']['path'], files['release']['path']], timeout=15)
    primary = lambda raw: {line.split()[-1] for line in raw.splitlines() if line.startswith('[GNUPG:] VALIDSIG ')}
    require(primary(dvd) == {'DF9B9C49EAA9298432589D76DA87E80D6294BE9B'}, 'DVD 官方签名身份不符')
    require(primary(release) == {'B8B80B5B623EAB6AD8775C45B7C5D7D6350947F8', '04B54C3CDCA79751B16BC6B5225629DF75B188BD', '41587F7DB8C774BCCF131416762F67A0B2C39DE4'}, 'Release 官方签名身份不符')
    plan = frozen['plan'];check_download_plan(plan)
    require(checksum_entry(Path(files['dvd_sums']['path']).read_text(), plan['images'][1]['name']) == plan['images'][1]['sha256'], 'DVD 不是签名清单目标')
    check_media_chain(Path(files['release']['path']).read_text(), Path(files['netboot_sums']['path']).read_text(), plan['version'], plan['installer_build'], plan['images'][0]['sha256'])
    result = {'dvd_status': dvd, 'release_status': release, 'files': files}
    write_once(Path(output) / 'signature-chain.json', json.dumps(result, indent=2) + '\n')
    return result


def download_stream(spec, response, output, guard, max_seconds):
    require(response.status == 200 and response.headers.get('Content-Length') == str(spec['size'])
            and response.headers.get('Content-Encoding', 'identity') == 'identity', '下载响应状态、长度或编码不符')
    total, digest, started = 0, hashlib.sha256(), time.monotonic()
    while True:
        guard(total)
        if time.monotonic() - started > max_seconds:
            raise TimeoutError('下载超出总时限')
        block = response.read(min(256 * 1024, spec['size'] - total + 1))
        if time.monotonic() - started > max_seconds:
            raise TimeoutError('下载超出总时限')
        if not block:
            break
        require(total + len(block) <= spec['size'], '响应超过核准镜像大小')
        guard(total)
        output.write(block);output.flush()
        total += len(block);digest.update(block)
    require(total == spec['size'] and digest.hexdigest() == spec['sha256'], '整文件大小或官方已签摘要不符')
    guard(total)
    return {'bytes': total, 'sha256': digest.hexdigest()}


def open_download(url):
    class Redirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, request, fp, code, message, headers, newurl):
            return super().redirect_request(request, fp, code, message, headers, download_https(newurl))
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), Redirect(),
                                        urllib.request.HTTPSHandler(context=ssl.create_default_context()))
    return opener.open(urllib.request.Request(download_https(url), headers={'Accept-Encoding': 'identity',
        'User-Agent': 'lober-production-qualification-download/1'}), timeout=15)


class DownloadChild:
    """只有这个子进程取文件；用内核固定句柄停止，不能误杀复用 PID。"""
    def __init__(self):
        self.pid, self.pidfd, self.file, self.returncode = None, None, None, 0
        self.signals = []

    def start(self, spec, partial, status_path, mount, mount_identity, future_bytes, lock_fd, max_seconds):
        require(hasattr(os, 'pidfd_open') and hasattr(signal, 'pidfd_send_signal'), '内核或 Python 不支持固定进程句柄')
        self.file = open(partial, 'xb', buffering=0)
        gate_read, gate_write = os.pipe()
        parent = os.getpid()
        blocked = signal.pthread_sigmask(signal.SIG_BLOCK, (signal.SIGTERM, signal.SIGHUP, signal.SIGINT))
        self.returncode = None
        try:
            self.pid = os.fork()
        except BaseException:
            self.returncode = 0
            os.close(gate_read);os.close(gate_write);self.file.close()
            signal.pthread_sigmask(signal.SIG_SETMASK, blocked)
            raise
        if self.pid == 0:
            os.close(gate_write);os.close(lock_fd)
            for number in (signal.SIGTERM, signal.SIGHUP, signal.SIGINT):
                signal.signal(number, signal.SIG_DFL)
            try:
                import ctypes
                require(ctypes.CDLL(None, use_errno=True).prctl(1, signal.SIGKILL, 0, 0, 0) == 0,
                        '无法设置父进程丢失即停止')
                signal.pthread_sigmask(signal.SIG_SETMASK, blocked)
                require(os.getppid() == parent and os.read(gate_read, 1) == b'1', '父监护进程已经退出')
                os.close(gate_read)
                require(os.fstat(self.file.fileno()).st_dev == mount_identity['st_dev'], '输出不在已核准数据盘')
                with open_download(spec['url']) as response:
                    final_url = download_https(response.geturl())
                    value = download_stream(spec, response, self.file,
                        lambda written: download_local_guard(mount, mount_identity, future_bytes + spec['size'] - written), max_seconds)
                os.fsync(self.file.fileno())
                write_once(status_path, json.dumps({'status': 'VERIFIED', 'url': spec['url'], 'final_url': final_url, **value}) + '\n')
                os._exit(0)
            except BaseException as error:
                try:
                    write_once(status_path, json.dumps({'status': 'FAIL_PARTIAL_PRESERVED', 'error_type': type(error).__name__, 'error': str(error)}) + '\n')
                finally:
                    os._exit(1)
        os.close(gate_read)
        try:
            self.pidfd = os.pidfd_open(self.pid)
            self.identity = process_identity(self.pid)
            self.identity['executable_sha256'] = sha256(Path('/proc') / str(self.pid) / 'exe')
            require(self.identity['uid'] == os.getuid(), '下载子进程所属用户不符')
            os.write(gate_write, b'1')
        except BaseException:
            # 子进程尚在管道门后；关门后只会失败退出，不会进入下载。
            os.close(gate_write)
            _, status = os.waitpid(self.pid, 0)
            self.returncode = os.waitstatus_to_exitcode(status)
            self.file.close()
            if self.pidfd is not None: os.close(self.pidfd);self.pidfd = None
            raise
        finally:
            signal.pthread_sigmask(signal.SIG_SETMASK, blocked)
        os.close(gate_write)

    def poll(self):
        if self.returncode is None:
            pid, status = os.waitpid(self.pid, os.WNOHANG)
            if pid:
                require(pid == self.pid, '回收了非本轮下载子进程')
                self.returncode = os.waitstatus_to_exitcode(status)
        return self.returncode

    def stop_and_wait(self):
        if not hasattr(self, 'signals'): self.signals = []
        if self.poll() is None:
            try:
                signal.pidfd_send_signal(self.pidfd, signal.SIGTERM)
                self.signals.append({'signal':'SIGTERM','at':time.time(),'method':'pidfd_send_signal'})
            except ProcessLookupError: pass
            deadline = time.monotonic() + 2
            while self.poll() is None:
                if time.monotonic() >= deadline:
                    try:
                        signal.pidfd_send_signal(self.pidfd, signal.SIGKILL)
                        self.signals.append({'signal':'SIGKILL','at':time.time(),'method':'pidfd_send_signal'})
                    except ProcessLookupError: pass
                time.sleep(0.1)
        return {'pid': self.pid, 'identity': getattr(self, 'identity', None), 'signals':self.signals,
                'reaped': True, 'exit': self.returncode}

    def close(self):
        require(self.poll() is not None, '未确认下载进程停止，不能关闭句柄')
        if self.file is not None and not self.file.closed:
            os.fsync(self.file.fileno());self.file.close()
        if self.pidfd is not None:
            os.close(self.pidfd);self.pidfd = None


def supervise_download(child, partial, guard, max_seconds, idle_seconds):
    started = last_progress = time.monotonic()
    previous_size = -1
    try:
        while child.poll() is None:
            size = Path(partial).stat().st_size
            now = time.monotonic()
            if size != previous_size:
                last_progress, previous_size = now, size
            if now - started > max_seconds or now - last_progress > idle_seconds:
                raise TimeoutError('下载总时间或无进展时间超限')
            guard(size)
            time.sleep(0.5)
        require(child.returncode == 0, '下载子进程失败；保留 partial 与退出原件')
        guard(Path(partial).stat().st_size)
    except BaseException:
        child.stop_and_wait()
        raise


def download_entry(config_path, manifest_path, output_path):
    """独立入口：只取已签原件，不使用产品 API，不启动或停止客户。"""
    global COMMAND_RECORDS
    import fcntl
    os.umask(0o077)
    frozen, config = private_json(manifest_path), private_json(config_path)
    require(frozen['schema'] == 'lober-cloud-download-v1' and frozen['scope'] == 'download-only', '不是仅下载批准包')
    review = frozen['review']
    require(review['approved'] is True and review['root_reviewer'] not in ('', 'pending-root')
            and review['independent_reviewer'] not in ('', 'pending-independent')
            and review['root_reviewer'] != review['independent_reviewer'], '仅下载包尚未双人核准')
    require(sha256(__file__) == frozen['executor_sha256'] and sha256(config_path) == frozen['config_sha256'], '执行器或配置变化')
    require(socket.gethostname() == 'lober' and os.getuid() == frozen['vm_uid'] and os.geteuid() != 0,
            '只能由本轮服务 VM 的核准普通用户下载')
    owner = private_json(config['identity_file'])
    require(owner == frozen['owner'], '下载身份文件变化')
    owner_valid(owner);check_download_plan(frozen['plan'])
    require(frozen['limits'] == {'max_seconds_per_image': 3600, 'idle_seconds': 45,
            'host_poll_seconds': 5, 'host_timeout_seconds': 12}, '不能降低监护频率或放大时限')
    mount = Path(VM_BASE) / 'machines/media'
    output = Path(output_path)
    require(output.parent == mount and re.fullmatch(r'official-download-[a-z0-9-]{3,80}', output.name)
            and not output.exists() and not output.is_symlink(), '下载必须用专用盘下全新尝试目录')
    lock_path = Path(VM_BASE) / 'leases/heavy.lock'
    require(lock_path.is_file() and not lock_path.is_symlink(), '共享重锁缺失')
    lock = lock_path.open('r');children=[];handlers={};result=None;made_output=False;record_error=None
    def interrupt(number, frame): raise InterruptedError('下载编排收到信号 ' + str(number))
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        for number in (signal.SIGTERM, signal.SIGHUP, signal.SIGINT):
            handlers[number]=signal.getsignal(number);signal.signal(number, interrupt)
        total = sum(image['size'] for image in frozen['plan']['images'])
        observed, mount_identity = observe_download(config, owner)
        check_download_observation(observed, owner, initial=True, remaining=total)
        output.mkdir(mode=0o700);made_output=True;COMMAND_RECORDS=output
        result={'status':'FAIL_PARTIAL_PRESERVED','scope':'download-only','owner':owner,
                'manifest_sha256':sha256(manifest_path),'executor_sha256':sha256(__file__),
                'initial':observed,'downloads':[]}
        write_once(output/'started.json',json.dumps({'owner':owner,'process':process_identity(os.getpid()),
            'manifest_sha256':result['manifest_sha256'],'mount_identity':mount_identity},indent=2)+'\n')
        result['signature_chain']=verify_download_trust(frozen,output)
        for spec in frozen['plan']['images']:
            future=total-spec['size'];partial=output/(spec['name']+'.part');status=output/(spec['name']+'.worker.json')
            child=DownloadChild();children.append(child)
            child.start(spec,partial,status,mount,mount_identity,future,lock.fileno(),3600)
            write_once(output/(spec['name']+'.process.json'),json.dumps(child.identity,indent=2)+'\n')
            last_host=[0.0]
            def guard(written):
                require(0 <= written <= spec['size'], '下载子进程超过准确字节预算')
                remaining=future+spec['size']-written
                download_local_guard(mount,mount_identity,remaining)
                if time.monotonic()-last_host[0]>=5:
                    actual=json.loads(host_command(config,DOWNLOAD_HOST_READ,timeout=12))
                    sample={**observed,**actual,'root_free':shutil.disk_usage('/').free,
                            'media_free':shutil.disk_usage(mount).free}
                    check_download_observation(sample,owner,initial=False,remaining=remaining)
                    write_once(output/('resource-'+str(time.time_ns())+'.json'),json.dumps(sample)+'\n')
                    last_host[0]=time.monotonic()
            supervise_download(child,partial,guard,3600,45)
            child.close()
            verified=private_json(status)
            require(verified['status']=='VERIFIED' and verified['bytes']==spec['size']
                    and verified['sha256']==spec['sha256'] and partial.stat().st_size==spec['size'], '缺少完整下载核对原件')
            # 再独立读取整文件；不凭 worker 的退出码或自报摘要放行。
            digest=hashlib.sha256();read_bytes=0
            with partial.open('rb') as source:
                for block in iter(lambda:source.read(256*1024),b''):
                    guard(spec['size']);digest.update(block);read_bytes+=len(block)
            require(read_bytes==spec['size'] and digest.hexdigest()==spec['sha256'], '实际原件整文件核对失败')
            final=output/spec['name']
            os.link(partial,final)  # 不覆盖既有文件；失败原件和成功原始路径都保留。
            result['downloads'].append({'path':str(final),'partial_path':str(partial),**spec,**verified})
            total=future
        result['status']='SIGNED_IMAGES_DOWNLOADED_NOT_IMPORTED'
        return result
    except BaseException as error:
        if result is not None:result.update(error_type=type(error).__name__,error=str(error))
        raise
    finally:
        for number in handlers:signal.signal(number,signal.SIG_IGN)
        # 任何记录/磁盘错误也不能跳过回收；未知状态一直持锁。
        stops=[]
        for child in children:
            while True:
                try:
                    stopped=child.stop_and_wait()
                    if stopped['reaped']:stops.append(stopped);break
                except BaseException as error:
                    print('仅下载子进程停止尚未确认：'+type(error).__name__,file=sys.stderr,flush=True)
                time.sleep(1)
            try:child.close()
            except BaseException as error:
                if result is not None:result.update(status='FAIL_PARTIAL_PRESERVED',flush_error=type(error).__name__)
        if made_output:
            try:
                result['children_stopped']=stops;result['lock_release_after_reap']=True
                result['partials']=[{'name':p.name,'bytes':p.stat().st_size} for p in output.glob('*.part') if p.is_file()]
                write_once(output/'result.json',json.dumps(result,ensure_ascii=False,indent=2)+'\n')
            except BaseException as error:
                record_error=error
                if result is not None:
                    result.update(status='FAIL_PARTIAL_PRESERVED',final_record_error=type(error).__name__)
                print('下载结果写入失败，原件仍保留：'+type(error).__name__,file=sys.stderr,flush=True)
        COMMAND_RECORDS=None
        lock.close()
        for number,handler in handlers.items():signal.signal(number,handler)
        if record_error is not None:
            raise record_error


HOST_SNAPSHOT = r'''
import hashlib,json,pathlib,shutil,socket,subprocess
def run(*args): return subprocess.check_output(args,text=True,timeout=10).strip()
def virsh(*args): return run('virsh','-c','qemu:///system',*args)
base=pathlib.Path('/var/lib/libvirt/images/lober-pq-20261025')
out={'hostname':socket.gethostname(),'free_bytes':shutil.disk_usage('/var/lib/libvirt/images').free,
     'base_exists':base.exists() or base.is_symlink(),'owner':None,
     'domains':[virsh('dumpxml',n) for n in virsh('list','--all','--name').splitlines() if n],
     'networks':[virsh('net-dumpxml',n) for n in virsh('net-list','--all','--name').splitlines() if n],
     'addresses':[],'routes':[],'bridges':[],'host_ports':[]}
for link in json.loads(run('ip','-j','address')):
 out['bridges'].append(link['ifname'])
 out['addresses'] += [a['local']+'/'+str(a['prefixlen']) for a in link.get('addr_info',[])]
for route in json.loads(run('ip','-j','route','show','table','all')):
 if route.get('dst'):out['routes'].append(route['dst'])
for line in run('ss','-H','-lntu').splitlines():
 try:out['host_ports'].append(int(line.split()[4].rsplit(':',1)[1]))
 except (ValueError,IndexError):raise RuntimeError('cannot parse host socket')
if base.exists():
 if base.is_symlink():raise RuntimeError('owned directory is a symlink')
 marker=base/'.owner.json'
 if marker.is_file() and not marker.is_symlink():out['owner']=json.loads(marker.read_text())
 registration=base/'registration.json'
 if registration.is_file() and not registration.is_symlink():
  record=json.loads(registration.read_text())
  raw=virsh('dumpxml','lober-pq25-linux01','--inactive')
  out['registered_target_sha256']=record['domain_sha256']
  out['actual_target_sha256']=hashlib.sha256(raw.encode()).hexdigest()
  out['actual_target_xml']=raw
  out['target_disk']={'info':json.loads(run('qemu-img','info','--force-share','--output=json',str(base/'linux01.qcow2'))),'map':[]}
print(json.dumps(out))
'''


def collect_preflight(config, owner):
    value = json.loads(host_command(config, HOST_SNAPSHOT))
    value['vm_free_bytes'] = shutil.disk_usage('/').free
    value['vm_ports'] = []
    for line in command(['ss', '-H', '-lntu']).splitlines():
        value['vm_ports'].append(int(line.split()[4].rsplit(':', 1)[1]))
    value['auth'] = {'ok': True, 'strict_host_key': True, 'from_vm': socket.gethostname() == 'lober'}
    check_preflight(value, owner, existing=bool(value['base_exists']))
    return value


class Console:
    """只连接本轮专用服务；凭据从私有文件读取，不放在命令行。"""
    def __init__(self, config, output, manifest):
        require(config['api_url'] == 'http://192.168.201.10:18090/console/v1', '禁止连接旧服务或未冻结地址')
        self.config, self.manifest = config, manifest
        self.instance = verify_instance(config, manifest)
        self.url, self.output = config['api_url'], output
        self.credentials = private_json(config['api_credentials'])
        self.token = None
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def api(self, method, path, body=None, token=None, expected=200):
        verify_instance(self.config, self.manifest)
        headers = {'Content-Type': 'application/json'}
        auth = token or self.token
        if auth:
            headers['Authorization'] = 'Bearer ' + auth
        request = urllib.request.Request(self.url + path, method=method, headers=headers,
            data=json.dumps(body).encode() if body is not None else None)
        identifier=str(time.time_ns())
        frame={'method':method,'path':path,'request_sha256':hashlib.sha256(request.data or b'').hexdigest(),'state':'STARTED'}
        write_once(self.output/('api-'+identifier+'-begin.json'),json.dumps(frame))
        status,payload=None,b''
        try:
            try:
                with self.opener.open(request, timeout=30) as response:
                    status,payload=response.status,response.read(16*1024**2+1)
            except urllib.error.HTTPError as error:
                status,payload=error.code,error.read(16*1024**2+1)
            require(len(payload)<=16*1024**2,'本轮API响应超过16MiB')
            frame['state']='RESPONDED'
        except BaseException as error:
            frame.update(state='FAILED',error_type=type(error).__name__);raise
        finally:
            frame.update(status=status,response_sha256=hashlib.sha256(payload).hexdigest(),response_bytes=len(payload))
            write_once(self.output/('api-'+identifier+'-end.json'),json.dumps(frame))
        with (self.output / 'api-records-private.jsonl').open('a') as log:
            os.chmod(log.name, 0o600)
            log.write(json.dumps({'at': int(time.time()), 'method': method, 'path': path,
                                  'status': status, 'response_sha256': hashlib.sha256(payload).hexdigest()}) + '\n')
        if not path.startswith('/auth/'):
            write_once(self.output / ('api-response-' + str(time.time_ns()) + '.json'),
                       json.dumps({'method': method, 'path': path, 'status': status,
                                   'response': json.loads(payload)}, indent=2) + '\n')
        require(status == expected, '本轮 API 返回非预期状态：' + method + ' ' + path + ' ' + str(status))
        self.last_raw_response={'path':'/console/v1'+path,'status':status,'payload':payload}
        return json.loads(payload).get('data')

    def login(self, username, password):
        return self.api('POST', '/auth/login', {'username': username, 'password': password})['token']

    def authenticate(self):
        self.token = self.login(self.credentials['admin_username'], self.credentials['admin_password'])

    def approve(self, kind, draft, author, approver):
        path = '/' + kind + '/' + draft['id'] + '/versions/' + str(draft['version'])
        submitted = self.api('POST', path + '/submit?rev=' + str(draft['rev']), token=author)
        approved = self.api('POST', path + '/approve?rev=' + str(submitted['rev']), token=approver)
        require(approved['state'] == 'approved' and approved['author'] != approved['approver'], '模板或 hook 未独立批准')
        return approved

    def events(self, task_id):
        events, after = [], 0
        for _ in range(1000):
            items = self.api('GET', '/tasks/' + task_id + '/events?after_seq=' + str(after) + '&limit=100')['items']
            if not items:
                return events
            require(all(e['taskID'] == task_id and e['seq'] > after for e in items), '事件身份或顺序错误')
            events.extend(items)
            after = items[-1]['seq']
        raise ValueError('事件分页超限，不能截断后声称通过')


def prepare_api(config, manifest, output):
    """供已审查的单独执行脚本调用；正常导入，不改介质状态。"""
    check_manifest(manifest)
    verify_instance(config,manifest)
    media = manifest['cloud']['media']
    verified = verify_media(media)
    api = Console(config, output, manifest)
    api.authenticate()
    require(not (output / 'import-private.json').exists(), '禁止重复导入后丢失首条任务')
    sources = [{'kind': 'path', 'addr': media[name]['path'], 'role': role,
                'size': media[name]['size'], 'sha256': media[name]['sha256']}
               for name, role in [('dvd', ''), ('netboot', 'netboot')]]
    item = api.api('POST', '/isos', {'name': manifest['run_id'] + '-debian-DVD-1',
                                    'family': 'linux', 'series': 'debian', 'sources': sources})
    write_once(output / 'import-private.json', json.dumps(item, indent=2) + '\n')
    deadline = time.monotonic() + 3600
    while time.monotonic() < deadline:
        phase_resources(config,manifest)
        require(shutil.disk_usage('/').free >= GIB and shutil.disk_usage(Path(media['dvd']['path']).parent).free >= GIB,
                '介质导入时资源余量不足')
        current = api.api('GET', '/isos/' + item['id'])
        if current['state'] == 'ready':
            require(current['series'] == 'debian' and current['arch'] == 'x86_64'
                    and current['version'] == media['version'] and current['sha256'] == media['dvd']['sha256'], '导入结果身份不符')
            require(all(current['artifacts'].get(k) for k in ('kernel', 'initrd', 'repo')), '实际介质缺安装文件')
            write_once(output / 'import-ready-private.json', json.dumps(current, indent=2) + '\n')
            write_once(output / 'media-verified.json', json.dumps(verified, indent=2) + '\n')
            chain = {key: manifest[key] for key in ('product_sha', 'test_sha', 'run_id', 'binary_sha256')}
            chain.update(owner=runtime_owner(manifest), executor_sha256=sha256(__file__),
                instance_record_sha256=manifest['cloud']['instance_record_sha256'],
                media_manifest_sha256=canonical_sha256(media), iso=current,
                database_origin_sha256=manifest['cloud']['database_origin_sha256'],
                import_receipt_sha256=sha256(output/'import-private.json'),
                evidence_sha256={name: sha256(output / name) for name in ('import-private.json', 'import-ready-private.json', 'media-verified.json')})
            write_once(output / 'prepare-chain-private.json', json.dumps(chain, indent=2) + '\n')
            return current
        require(current['state'] not in ('failed', 'cancelled'), '真实介质导入失败')
        time.sleep(2)
    raise ValueError('真实介质导入超过一小时，不能记通过')


def start_api(config, manifest, output):
    """冻结 hook、派生内置模板、预览并创建真实任务。只由核准脚本调用。"""
    check_manifest(manifest)
    require(config.get('short_failure_evidence_verified') is True,'无任务短流程未由根核验，不能创建真实任务')
    require(config.get('short_failure_evidence_file') and manifest['cloud'].get('short_failure_evidence_sha256'),
        '缺根核验的无任务短流程冻结原件')
    short=bound_json(config['short_failure_evidence_file'],manifest['cloud']['short_failure_evidence_sha256'])
    require(short['owner']==runtime_owner(manifest) and short['no_task_created'] is True and short['client_stopped'] is True
        and short['failed_disks_preserved'] is True and short['root_verified'] is True,'无任务实际短流程没有准确核验/停写证明')
    target=manifest_target(manifest)
    require(not (output / 'task-private.json').exists(), '目标已有任务，请先检查，不重复创建')
    prepared = bound_json(config['prepare_chain_file'], manifest['cloud']['prepare_chain_sha256'])
    check_prepare_originals(config,manifest,prepared)
    api = Console(config, output, manifest)
    api.authenticate()
    media = api.api('GET', '/isos/' + prepared['iso']['id'])
    check_prepare_binding(prepared, manifest, media)
    author = api.login(api.credentials['author_username'], api.credentials['author_password'])
    approver = api.login(api.credentials['approver_username'], api.credentials['approver_password'])
    require(api.credentials['author_username'] != api.credentials['approver_username'], 'hook 作者与批准人必须不同')
    hook_body = '''#!/bin/sh
set -eu
directory=/var/lib/lober-qualification
mkdir -p "$directory"
test ! -e "$directory/sentinel"
dd if=/dev/urandom of="$directory/sentinel" bs=32 count=1 2>/dev/null
cat /sys/class/dmi/id/product_serial > "$directory/smbios-sn"
chmod 0444 "$directory/smbios-sn"
printf 'firstboot-hook\\n' >> "$directory/hook-runs"
systemctl start ssh
systemctl is-active --quiet ssh
systemctl start qemu-guest-agent
systemctl is-active --quiet qemu-guest-agent
cat /etc/os-release
uname -m
'''
    negative = config['required_failure']
    require(type(negative) is bool, 'required_failure 必须明确 true 或 false')
    if negative:
        hook_body += 'echo required-hook-failure-41\nexit 41\n'
    hook = api.api('POST', '/hooks', {'name': manifest['run_id'] + '-qualification',
        'description': PURPOSE, 'timeoutSec': 180, 'required': True, 'params': [],
        'impls': [{'family': 'linux', 'series': ['debian'], 'kind': 'script', 'body': hook_body,
                   'interpreter': 'sh', 'timings': ['firstboot']}]}, token=author)
    hook = api.approve('hooks', hook, author, approver)
    builtin = manifest['cloud']['builtin_template']
    require(builtin['id'] == 'builtin-preseed' and builtin['version'] == 3, '本候选必须使用已核对的 Debian 内置模板 v3')
    native = api.api('GET', '/templates/builtin-preseed/versions/3')
    require(native['builtin'] is True and native['state'] == 'approved'
            and hashlib.sha256(native['body'].encode()).hexdigest() == builtin['body_sha256'], '当前服务的内置模板与冻结值不符')
    draft = api.api('POST', '/templates/builtin-preseed/derive', {'from': 3, 'name': manifest['run_id']}, token=author)
    require(draft['body'].count('{{lober:firstboot}}') == 1, '当前内置模板与预期不同，应重新审查')
    draft['body'] = draft['body'].replace('{{lober:firstboot}}', '{{slot:qualify@firstboot}} && {{lober:firstboot}}')
    draft['assembly']['qualify'] = [{'hookID': hook['id'], 'hookVersion': hook['version']}]
    draft = api.api('PUT', '/templates/' + draft['id'] + '/versions/' + str(draft['version']),
        {key: draft[key] for key in ('name', 'family', 'series', 'body', 'params', 'bindings', 'assembly', 'rev')}, token=author)
    template = api.approve('templates', draft, author, approver)
    # 不接管已有 SN；即使名字像本轮目标也拒绝。
    hosts = api.api('GET', '/hosts?limit=100')['items']
    found=[h for h in hosts if h['sn']==target['sn']]
    require(len(found)<=1,'服务端客户SN不唯一')
    attrs={'qualification':PURPOSE,'run_id':manifest['run_id'],'target_uuid':target['uuid']}
    if found:
        require(found[0]['attrs']==attrs and found[0]['arch']=='x86_64' and found[0]['firmware']=='bios',
                '不能接管其他轮SN；串行复用也须准确本轮目标')
        require(config.get('allow_terminal_reuse',config.get('allow_serial_reuse')) is True,'已有SN仅可在明确终态复用合同中接续')
        cursor='';seen=set()
        for _ in range(1000):
            page=api.api('GET','/tasks?host_sn='+urllib.parse.quote(target['sn'],safe='')+'&limit=100'+('&cursor='+urllib.parse.quote(cursor,safe='') if cursor else ''))
            for prior in page['items']:
                require(prior['hostSN']==target['sn'] and prior['id'] not in seen
                    and prior['state'] in ('done','failed','cancelled'),'同客户仍有活任务/列表不可靠，不创建新任务')
                seen.add(prior['id'])
            cursor=page.get('next','')
            if not cursor:break
        else:raise ValueError('本客户历史任务分页超界，不截断后复用')
    else:
        api.api('POST', '/hosts', {'sn':target['sn'],'arch':'x86_64','firmware':'bios','attrs':attrs})
    key = Path(config['guest_public_key']).read_text().strip()
    require(key.startswith('ssh-ed25519 ') and '\n' not in key, '客户公钥必须单行 ed25519')
    require(hashlib.sha256(key.encode()).hexdigest() == manifest['cloud']['guest_public_key_sha256'], '客户登录公钥不是冻结输入')
    entered = {'partition/1/disk': 'vda', 'account/1/username': 'lober',
               'account/1/pwhash': api.credentials['guest_password_hash'], 'sshkey/1/key': key,
               'network/1/hostname': target['name'], 'network/1/interface': 'enp1s0',
               'tpl/mirror_host': 'ftp.debian.org', 'tpl/mirror_path': '/debian',
               'packages/1/packages': 'curl\nopenssh-server\nqemu-guest-agent\npython3'}
    selection = {'isoID': media['id'], 'templateID': template['id'], 'templateVersion': template['version']}
    params = api.api('POST', '/wizard/params', {**selection, 'hostSNs': [target['sn']], 'entered': {target['sn']: entered}})
    write_once(output / 'params-private.json', json.dumps(params, indent=2) + '\n')
    require(not params['common'] and len(params['hosts']) == 1 and not params['hosts'][0]['problems'], '当前 API 预览拒绝参数')
    task = api.api('POST', '/wizard/tasks', {**selection, 'hosts': [{'sn':target['sn'],'entered':entered}]})['tasks'][0]
    write_once(output / 'created-task-response-private.json',json.dumps(task,indent=2)+'\n')
    require(task['hostSN'] == target['sn'] and task['state'] == 'pending', '实际任务身份或起始状态不符')
    check_task_binding(task, prepared, template, hook, manifest['cloud']['owner'],target)
    write_once(output / 'task-private.json', json.dumps(task, indent=2) + '\n')
    write_once(output / 'hook-private.json', json.dumps(hook, indent=2) + '\n')
    write_once(output / 'template-private.json', json.dumps(template, indent=2) + '\n')
    chain = {'owner': manifest['cloud']['owner'], 'prepare_chain_sha256': manifest['cloud']['prepare_chain_sha256'],
             'instance_record_sha256': manifest['cloud']['instance_record_sha256'], 'task': task, 'template': template, 'hook': hook,
             'product_sha': manifest['product_sha'], 'test_sha': manifest['test_sha'], 'binary_sha256': manifest['binary_sha256'],'run_id':manifest['run_id'],
             'executor_sha256': sha256(__file__), 'guest_public_key_sha256': manifest['cloud']['guest_public_key_sha256'],
             'target':target,'target_sha256':canonical_sha256(target),
             'database_origin_sha256':manifest['cloud']['database_origin_sha256']}
    chain['short_failure_evidence_sha256']=manifest['cloud']['short_failure_evidence_sha256']
    write_once(output / 'start-chain-private.json', json.dumps(chain, indent=2) + '\n')
    return task


GUEST_INSPECT = r'''
import hashlib,json,os,pathlib,re,subprocess
def run(*args):return subprocess.check_output(args,text=True,timeout=15).strip()
release={}
for line in pathlib.Path('/etc/os-release').read_text().splitlines():
 if '=' in line:
  k,v=line.split('=',1);release[k]=v.strip('"')
root=run('findmnt','-n','-o','SOURCE','/')
serials=run('lsblk','-s','-n','-o','SERIAL',root).splitlines()
partitions=[line.split()[0] for line in run('lsblk','-l','-n','-p','-o','NAME,TYPE','/dev/vda').splitlines() if line.split()[1]=='part']
addresses=[a['local'] for link in json.loads(run('ip','-j','address')) for a in link.get('addr_info',[]) if a.get('family')=='inet']
routes=json.loads(run('ip','-j','route','show','default'))
directory=pathlib.Path('/var/lib/lober-qualification')
runtime=pathlib.Path('/var/lib/lober')
credentials=[p.name for p in runtime.iterdir() if p.is_file() and (p.name.endswith('.token') or p.name.startswith(('hook-','script-')) or p.name=='firstboot.pending')] if runtime.is_dir() else []
retained=[]
pattern=re.compile(rb'/v1/(?:answer|signal)/[^/\s]+/[A-Za-z0-9_.=-]{32,}/')
for base in ['/var/lib/lober','/var/lib/cloud','/var/log/installer','/var/adm/autoinstall/cache','/root']:
 for folder,dirs,files in os.walk(base,followlinks=False):
  dirs[:]=[d for d in dirs if not pathlib.Path(folder,d).is_symlink()]
  for name in files:
   p=pathlib.Path(folder,name)
   if p.is_symlink() or not p.is_file():continue
   with p.open('rb') as source:
    tail=b''
    while True:
     block=source.read(1024*1024)
     if not block:break
     if pattern.search(tail+block):retained.append(str(p));break
     tail=(tail+block)[-1024:]
print(json.dumps({'os_id':release['ID'],'version_id':release['VERSION_ID'],
 'debian_version':pathlib.Path('/etc/debian_version').read_text().strip(),
 'arch':run('uname','-m'),'firmware':'uefi' if pathlib.Path('/sys/firmware/efi').exists() else 'bios',
 'smbios_serial':pathlib.Path('/sys/class/dmi/id/product_serial').read_text().strip(),
 'root_disk_serial':next((s.strip() for s in serials if s.strip()),''),'root_source':root,
 'root_uuid':run('findmnt','-n','-o','UUID','/'),'partitions':partitions,
 'machine_id':pathlib.Path('/etc/machine-id').read_text().strip(),
 'boot_id':pathlib.Path('/proc/sys/kernel/random/boot_id').read_text().strip(),
 'sentinel_sha256':hashlib.sha256((directory/'sentinel').read_bytes()).hexdigest(),
 'hook_runs':len((directory/'hook-runs').read_text().splitlines()),
 'credential_files':len(credentials),'retained_auth_paths':retained,
 'address':'192.168.201.101' if '192.168.201.101' in addresses else '',
 'default_gateway':routes[0].get('gateway','') if len(routes)==1 else ''}))
'''


def inspect_guest(config, manifest, output, label):
    target=manifest_target(manifest)
    identity_helper=load_cloud_helper('cloud_guest_identity',manifest)
    raw=guest_agent_read(config,manifest,identity_helper.GUEST_IDENTITY_SCRIPT)
    raw_path=output/(label+'-agent-identity-private.json')
    write_once(raw_path,json.dumps(raw,sort_keys=True)+'\n')
    agent={'target_uuid':target['uuid'],'transport':'libvirt-qga',
        'collection_script_sha256':hashlib.sha256(identity_helper.GUEST_IDENTITY_SCRIPT.encode()).hexdigest(),
        'identity':raw,'raw_path':str(raw_path),'raw_sha256':sha256(raw_path)}
    verified=identity_helper.verify_guest_identity({**config,'owner':manifest['cloud']['owner']},target,agent,
        output/(label+'-ssh-identity'),command)
    # 系统验收和两条独立身份通道必须为同一次客户启动。
    inspection=GUEST_INSPECT.replace("'192.168.201.101'",repr(target['guest_ip']))
    value=guest_agent_read(config,manifest,inspection)
    for key in ('smbios_serial','machine_id','boot_id','root_uuid','root_disk_serial','address'):
        require(value[key]==verified[key],'系统证据和真实SSH/QGA不是同一客户：'+key)
    value.update(ssh_exit=verified['ssh_exit'],identity_evidence_sha256=sha256(raw_path),ssh_access=verified['access'])
    write_once(output/(label+'-guest.json'),json.dumps(value,indent=2)+'\n')
    return value


def run_approved_host_script(config, manifest, name):
    path = Path(config['scripts_dir']) / name
    require(path.is_file() and not path.is_symlink(), '缺少核准脚本')
    require(sha256(path) == manifest['cloud']['approved_scripts'].get(str(path.resolve())), '此份外部操作脚本未核准或摘要已变')
    require(shutil.disk_usage('/').free >= GIB, 'lober 根盘不足 1GiB')
    if name in ('05-host-start.py','09-host-reboot.py'):
        phase='start' if name=='05-host-start.py' else 'reboot'
        require(path.read_text()==target_action_source(manifest,phase),'启动/重启脚本未绑定当前准确目标和完整盘网合同')
        check_vm_runtime_network(config,manifest)
    return host_command(config, path.read_text())


def capture_contract(config,manifest,kind,task):
    frozen=bound_json(config['capture_contract_files'][kind],manifest['cloud']['capture_contract_sha256'][kind])
    target=manifest_target(manifest);row=frozen['targets'][target['uuid']]
    require(row['target_sha256']==canonical_sha256(target) and row['packet_contract']['task_id']==task['id'],
        '抓包合同没有绑定准确目标/真实任务')
    origin=bound_json(config['boot_origin_file'],manifest['cloud']['boot_origin_sha256'])
    require(origin['owner']==manifest['cloud']['owner'] and origin['target']==target
        and origin['client_xml_sha256']==manifest['cloud']['client_xml_sha256'], 'ROM/DHCP来源不是准确本轮客户')
    require(origin['firmware_sha256']==row['boot_chain']['firmware_sha256']
        and origin['dhcp_config_sha256']==row['boot_chain']['dhcp_config_sha256'], '抓包引导链与真实ROM/DHCP来源不符')
    # 来源原件由根通过准确宿主采得；执行前重新读实际文件摘要，不能只信表内名字。
    probe="import hashlib,json,pathlib,socket\nif socket.gethostname()!='ser866667880745':raise RuntimeError('wrong host')\npaths="+repr({key:origin[key+'_path'] for key in ('firmware','dhcp_config')})+"\nprint(json.dumps({key:hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest() for key,path in paths.items()}))\n"
    actual=json.loads(host_command(config,probe,timeout=15))
    require(all(actual[key]==origin[key+'_sha256'] for key in ('firmware','dhcp_config')),'实际ROM/DHCP配置变化')
    return frozen


def check_capture_receipt_binding(receipt,frozen,target,owner):
    row=frozen['targets'][target['uuid']]
    require(receipt['owner']==owner and receipt['target_uuid']==target['uuid']
        and receipt['target_sha256']==canonical_sha256(target) and row['target_sha256']==canonical_sha256(target),
        '抓包回执目标与启动前合同不符')
    require(receipt['contract_sha256']==canonical_sha256(frozen) and receipt['packet_contract']==row['packet_contract']
        and receipt['boot_chain']==row['boot_chain'],'抓包回执不能事后放宽必需协议/文件/响应')
    require(receipt.get('packet_mode','TASK')==row.get('packet_mode','TASK'),'抓包回执不能切换任务/无任务模式')
    for key in ('max_bytes','max_seconds','tcp_reset_policy'):
        require(receipt[key]==frozen[key],'抓包回执限制与启动前冻结值不符：'+key)


class Capture:
    def __init__(self,config,manifest,output,kind,task):
        self.helper=load_cloud_helper('cloud_capture_session',manifest)
        folder=output/(kind+'-capture');folder.mkdir(mode=0o700);self.output=folder
        self.target=manifest_target(manifest);self.owner=manifest['cloud']['owner']
        self.frozen=capture_contract(config,manifest,kind,task)
        self.session=self.helper.CaptureSession(config,self.owner,self.target,self.frozen,folder,host_command)
        self.receipt=None;self.running=False;ACTIVE_CAPTURES.append(self)
        self.session.start()
        self.running=True
    def check(self):
        if self.running and self.receipt is None:self.session.check()
    def close(self):
        self.running=False
        if self.receipt is None:
            candidate=self.session.close()
            self.helper.verify_receipt(candidate)
            check_capture_receipt_binding(candidate,self.frozen,self.target,self.owner)
            write_once(self.output/'capture-receipt-private.json',json.dumps(candidate,indent=2)+'\n')
            anchor={'contract_sha256':canonical_sha256(self.frozen),'target_sha256':canonical_sha256(self.target),
                'receipt_sha256':canonical_sha256(candidate)}
            payload=json.dumps(anchor,indent=2)+'\n'
            self.anchor_file=self.output/'capture-anchor-private.json'
            write_once(self.anchor_file,payload)
            self.anchor_sha256=hashlib.sha256(payload.encode()).hexdigest()
            self.receipt=candidate
        return self.receipt
    def analyze(self):
        receipt=self.close()
        anchor=bound_json(self.anchor_file,self.anchor_sha256)
        return self.helper.analyze_session(receipt,self.frozen,self.target,anchor=anchor)


def short_file(output,name,data):
    """原始字节先私有落盘并flush，再给核验器引用；不重写成自报摘要。"""
    path=Path(output)/name
    require(path.name==name,'短流程原件名字越界')
    raw=data if isinstance(data,bytes) else (json.dumps(data,indent=2,ensure_ascii=False)+'\n').encode()
    descriptor=os.open(path,os.O_CREAT|os.O_EXCL|os.O_WRONLY|os.O_NOFOLLOW,0o600)
    with os.fdopen(descriptor,'wb') as sink:sink.write(raw);sink.flush();os.fsync(sink.fileno())
    directory=os.open(path.parent,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
    try:os.fsync(directory)
    finally:os.close(directory)
    return {'path':str(path),'bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest()}


def short_api_snapshot(api,target,context,output,label):
    started=time.time_ns();requests=[]
    for suffix,name in (('/tasks?page=1&page_size=100','all'),
        ('/tasks?host_sn='+urllib.parse.quote(target['sn'],safe='')+'&page=1&page_size=100','sn')):
        page=api.api('GET',suffix)
        require(page['items']==[] and page.get('next','')=='' and type(page.get('total')) is int and page['total']==0,
                '无任务短流程必须同实例总任务数及本SN任务数均为0')
        original=api.last_raw_response
        require(original['path']=='/console/v1'+suffix and original['status']==200,'无任务API原始响应串请求')
        body=short_file(output,label+'-'+name+'-response.json',original['payload'])
        requests.append({'method':'GET','path':original['path'],'status':200,'body':body})
    return short_file(output,label+'.json',{'schema':'lober-no-task-api-v1','context':context,
        'started_ns':started,'finished_ns':time.time_ns(),'requests':requests})


def short_disk_snapshot(config,manifest,context,output,label):
    source=owned_client_guard_source(manifest)
    source+='''require(virsh('domstate',target['uuid'])=='shut off','short-flow disk must be offline')
info=subprocess.check_output(['qemu-img','info','--force-share','--output=json',target['disk_path']])
mapping=subprocess.check_output(['qemu-img','map','--force-share','--output=json',target['disk_path']])
check_disk({'info':json.loads(info),'map':json.loads(mapping)},True)
stat_before=path.stat()
print(json.dumps({'domain_state':'shut off','at_ns':time.time_ns(),
 'source_identity':{'device':stat_before.st_dev,'inode':stat_before.st_ino},
 'info':info.decode(),'map':mapping.decode(),'domain_xml':before['inactive'],'owner':marker.read_bytes().decode()}))
'''
    raw=json.loads(host_command(config,source,timeout=40))
    require(raw['domain_state']=='shut off','空盘原件不是关机读取')
    refs={key:short_file(output,label+'-'+key+('.xml' if key=='domain_xml' else '.json'),raw[key].encode())
          for key in ('info','map','domain_xml','owner')}
    return short_file(output,label+'.json',{'schema':'lober-no-task-disk-v1','context':context,
        'at_ns':raw['at_ns'],'domain_state':'shut off','source_identity':raw['source_identity'],**refs})


def short_host_action(config,manifest,contract,context,output,action):
    source=target_action_source(manifest,'start') if action=='start' else owned_client_stop_source(manifest)
    require(hashlib.sha256(source.encode()).hexdigest()==contract['operations'][action+'_source_sha256'],
            '短流程动作不是事前独审的准确源码')
    source_ref=short_file(output,'client-'+action+'-source.py',source.encode())
    previous=set(COMMAND_RECORDS.glob('command-*-end.json'));started=time.time_ns()
    if action=='start':stdout=run_approved_host_script(config,manifest,'05-host-start.py')
    else:stdout=host_command(config,source,timeout=90)
    finished=time.time_ns();matches=[]
    for path in set(COMMAND_RECORDS.glob('command-*-end.json'))-previous:
        value=private_json(path)
        if value.get('stdin_sha256')==source_ref['sha256']:matches.append(value)
    require(len(matches)==1,'没有唯一的真实短流程命令结束原件')
    command_record=matches[0]
    require(command_record['state']=='EXITED' and command_record['exit']==0 and command_record['stdout']==stdout,
            '短流程动作退出或原始输出未确认')
    out=short_file(output,'client-'+action+'-stdout.json',stdout.encode())
    err=short_file(output,'client-'+action+'-stderr.txt',command_record['stderr'].encode())
    return short_file(output,'client-'+action+'.json',{'schema':'lober-no-task-action-v1','context':context,
        'action':action,'started_ns':started,'finished_ns':finished,
        'command':{'source':source_ref,'stdout':out,'stderr':err,'exit':0,'timed_out':False}})


def validate_short_approval(config,manifest):
    """资源尚未批准时在租约/收尾动作之前只读拒绝，不能顺手停止服务。"""
    target=manifest_target(manifest)
    contract=bound_json(config['no_task_contract_file'],manifest['cloud']['no_task_contract_sha256'])
    load_cloud_helper('cloud_no_task_probe',manifest).prepare_no_task_probe(contract)
    require(contract['owner']==manifest['cloud']['owner'] and contract['target']==target,'短流程资源不是准确目标')
    resource=bound_json(config['no_task_target_contract_file'],manifest['cloud']['no_task_target_contract_file_sha256'])
    require(canonical_sha256(resource)==contract['bindings']['target_contract_sha256']
        and resource['owner']==contract['owner'] and resource['target']==target,'短流程资源原件串轮')
    review=resource['review']
    require(review['approved'] is True and review['root_reviewer'] and review['independent_reviewer']
        and review['root_reviewer']!=review['independent_reviewer'],'未单独批准短流程准确目标')


def short_no_task(config,manifest,output):
    """真实短流程只交待根核验；没有任务POST，不自设verified=true。"""
    target=manifest_target(manifest);owner=manifest['cloud']['owner']
    require(target==target_spec(owner),'本短流程只覆盖首条准确legacy空白目标')
    contract=bound_json(config['no_task_contract_file'],manifest['cloud']['no_task_contract_sha256'])
    probe=load_cloud_helper('cloud_no_task_probe',manifest)
    probe.prepare_no_task_probe(contract)
    expected_checkers={key:sha256(Path(__file__).with_name(name+'.py')) for key,name in
        (('probe_sha256','cloud_no_task_probe'),('parser_sha256','cloud_packet_capture'),('capture_session_sha256','cloud_capture_session'))}
    require(contract['checkers']==expected_checkers,'短流程检查器/解析器/捕获器不是同一冻结版本')
    require(contract['owner']==owner and contract['target']==target,'短流程合同串目标')
    bindings=contract['bindings']
    for key in ('product_sha','test_sha','binary_sha256'):require(bindings[key]==manifest[key],'短流程候选串轮')
    require(bindings['instance_record_sha256']==manifest['cloud']['instance_record_sha256'],'短流程实际实例未冻结')
    instance=verify_instance(config,manifest)
    origin=bound_json(manifest['cloud']['database_origin_file'],manifest['cloud']['database_origin_sha256'])
    require(bindings['runtime_record_sha256']==origin['deployment_receipt_sha256'],'短流程runtime不是根启动原件')
    runtime=bound_json(config['no_task_runtime_record_file'],bindings['runtime_record_sha256'])
    resource=bound_json(config['no_task_target_contract_file'],manifest['cloud']['no_task_target_contract_file_sha256'])
    require(canonical_sha256(resource)==bindings['target_contract_sha256'] and resource['owner']==owner and resource['target']==target
        and resource['schema']=='lober-cloud-target-resource-v1' and resource['executes_mutation'] is False
        and resource['failed_disk_delete_or_reuse_allowed'] is False,'短流程资源原件不是准确已保留目标')
    review=resource['review']
    require(review['approved'] is True and review['root_reviewer'] and review['independent_reviewer']
        and review['root_reviewer']!=review['independent_reviewer'],'短流程资源合同尚未独审')
    context={'owner':owner,'target_sha256':canonical_sha256(target),'bindings':bindings}
    records={
        'target_contract':short_file(output,'target-contract.json',Path(config['no_task_target_contract_file']).read_bytes()),
        'runtime_record':short_file(output,'runtime-record.json',Path(config['no_task_runtime_record_file']).read_bytes()),
        'instance_record':short_file(output,'instance-record.json',Path(config['instance_record_file']).read_bytes())}
    api=Console(config,output,manifest);api.authenticate()
    records['api_before']=short_api_snapshot(api,target,context,output,'api-before')
    records['disk_before']=short_disk_snapshot(config,manifest,context,output,'disk-before')
    frozen=capture_contract(config,manifest,'short',{'id':None})
    require(frozen==contract['capture_contract'],'短流程捕获不是同一事前冻结完整合同')
    require(frozen['targets'][target['uuid']]['packet_mode']=='NO_TASK','短流程不能借普通任务抓包合同')
    seconds=config['short_observation_seconds']
    require(type(seconds) is int and 5<=seconds<=60 and seconds+20<=contract['max_seconds']
        and seconds+20<=frozen['max_seconds'],'短流程观察时窗未在核准预算内')
    for action,source in (('start',target_action_source(manifest,'start')),('stop',owned_client_stop_source(manifest))):
        require(hashlib.sha256(source.encode()).hexdigest()==contract['operations'][action+'_source_sha256'],
            '短流程开始前动作源码未完整核准')
    capture=Capture(config,manifest,output,'short',{'id':None})
    try:
        records['client_start']=short_host_action(config,manifest,contract,context,output,'start')
        deadline=time.monotonic()+seconds
        while time.monotonic()<deadline:capture.check();time.sleep(min(.25,max(.001,deadline-time.monotonic())))
        records['client_stop']=short_host_action(config,manifest,contract,context,output,'stop')
    except BaseException:
        # 先交外层SafetyLease停客户/导入；不能被抓包close阻塞推迟停写。
        raise
    capture.close()
    capture.analyze()
    records['capture_receipt']={'path':str(capture.output/'capture-receipt-private.json'),
        'bytes':(capture.output/'capture-receipt-private.json').stat().st_size,'sha256':sha256(capture.output/'capture-receipt-private.json')}
    records['capture_anchor']={'path':str(capture.anchor_file),'bytes':capture.anchor_file.stat().st_size,'sha256':capture.anchor_sha256}
    records['disk_after']=short_disk_snapshot(config,manifest,context,output,'disk-after')
    records['api_after']=short_api_snapshot(api,target,context,output,'api-after')
    receipt={'schema':'lober-cloud-no-task-probe-receipt-v1','owner':owner,'contract_sha256':canonical_sha256(contract),
        'status':'EXPECTED_FAILURE','failure_kind':'no_active_task_local_boot_on_blank_disk','synthetic':False,'root_verified':False,
        'no_task_created':True,'client_stopped':True,'failed_disks_preserved':True,'records':records}
    receipt_ref=short_file(output,'no-task-receipt.json',receipt)
    anchor={'contract_sha256':canonical_sha256(contract),'receipt_sha256':canonical_sha256(receipt)}
    anchor_ref=short_file(output,'no-task-anchor.json',anchor)
    checked=probe.verify_no_task_probe(receipt,contract,anchor=bound_json(anchor_ref['path'],anchor_ref['sha256']))
    require(checked['status']=='ROOT_REVIEW_REQUIRED' and checked['short_failure_evidence_verified'] is False,
        '短流程检查器不得自行批准真实任务前置')
    return {'status':'ROOT_REVIEW_REQUIRED','short_failure_evidence_verified':False,
        'short_receipt':receipt_ref,'short_anchor':anchor_ref,'scope':'NO_TASK_REAL_SHORT_PENDING_ROOT_REVIEW'}


def begin_install_case(config, manifest, output):
    require(config.get('short_failure_evidence_verified') is True, '先完成故意失败短流程采证，并由根核对')
    started=bound_json(config['start_chain_file'],manifest['cloud']['start_chain_sha256'])
    check_start_binding(started,manifest,manifest_target(manifest))
    prepared=bound_json(config['prepare_chain_file'],manifest['cloud']['prepare_chain_sha256'])
    check_prepare_originals(config,manifest,prepared)
    api = Console(config, output, manifest)
    api.authenticate()
    task=api.api('GET','/tasks/'+started['task']['id'])
    require(task['state']=='pending' and task['snapshot']==started['task']['snapshot'],'只能启动已冻结的本轮pending任务')
    check_prepare_binding(prepared,manifest,api.api('GET','/isos/'+prepared['iso']['id']))
    check_task_binding(task,prepared,started['template'],started['hook'],manifest['cloud']['owner'],manifest_target(manifest))
    capture = Capture(config,manifest,output,'install',task)
    try:
        started = run_approved_host_script(config, manifest, '05-host-start.py')
        write_once(output / 'actual-start-private.log', started + '\n')
        return {'config':config,'manifest':manifest,'output':output,'task':task,'api':api,'capture':capture}
    except BaseException:
        raise  # capture已登记；外层先停客户，再收尾抓包。


def poll_install_case(case):
    case['capture'].check()
    task=case['api'].api('GET','/tasks/'+case['task']['id'])
    if task['state'] not in ('done','failed','cancelled'):return None
    write_once(case['output']/'terminal-task-private.json',json.dumps(task,indent=2)+'\n')
    write_once(case['output']/'events-private.json',json.dumps(case['api'].events(task['id']),indent=2)+'\n')
    require(task['state']==('failed' if case['config']['required_failure'] else 'done'),'真实流程进入非预期终态')
    return task


def install(config,manifest,output):
    case=begin_install_case(config,manifest,output)
    completed=False
    try:
        deadline=time.monotonic()+5400
        while time.monotonic() < deadline:
            phase_resources(config,manifest)
            task=poll_install_case(case)
            if task is not None:completed=True;return task
            time.sleep(10)
        raise ValueError('真实安装超过90分钟，没有完成证明')
    finally:
        if completed:case['capture'].close()


def parallel_install(cases,output):
    active=[];done={};completed=False
    try:
        for config,manifest in cases:
            require(config['required_failure'] is False,'并行比较只接真实成功case；故意失败单列')
            folder=output/manifest_target(manifest)['sn'];folder.mkdir(mode=0o700)
            active.append(begin_install_case(config,manifest,folder))
        deadline=time.monotonic()+5400
        while time.monotonic()<deadline:
            for index,case in enumerate(active):
                if index in done:continue
                value=poll_install_case(case)
                if value is not None:done[index]=value
            if len(done)==2:completed=True;return [done[index] for index in range(2)]
            time.sleep(10)
        raise ValueError('两客户并行安装未在90分钟完成')
    finally:
        if completed:
            for case in active:case['capture'].close()


def qualify(config, manifest, output):
    target=manifest_target(manifest)
    started = bound_json(config['start_chain_file'], manifest['cloud']['start_chain_sha256'])
    check_start_binding(started,manifest,target)
    prepared = bound_json(config['prepare_chain_file'], manifest['cloud']['prepare_chain_sha256'])
    require(started['owner'] == manifest['cloud']['owner'] and started['prepare_chain_sha256'] == manifest['cloud']['prepare_chain_sha256']
            and started['instance_record_sha256'] == manifest['cloud']['instance_record_sha256']
            and started['executor_sha256'] == sha256(__file__), 'start 链并非本轮冻结输入')
    task_record = started['task']
    api = Console(config, output, manifest)
    api.authenticate()
    task = api.api('GET', '/tasks/' + task_record['id'])
    check_prepare_binding(prepared, manifest, api.api('GET', '/isos/' + prepared['iso']['id']))
    check_prepare_originals(config,manifest,prepared)
    check_task_binding(task, prepared, started['template'], started['hook'], manifest['cloud']['owner'],target)
    require(task['snapshot'] == task_record['snapshot'], '安装后的任务快照变化')
    expected = 'failed' if config['required_failure'] else 'done'
    require(task['hostSN'] == target['sn'] and task['state'] == expected, '未实际采到安装终态')
    events = api.events(task['id'])
    first = inspect_guest(config, manifest, output, 'before-repeat')
    capture = Capture(config,manifest,output,'repeat',task)
    try:
        write_once(output / 'actual-reboot-private.log', run_approved_host_script(config, manifest, '09-host-reboot.py') + '\n')
        deadline, attempt = time.monotonic() + 600, 0
        while time.monotonic() < deadline:
            capture.check()
            attempt += 1
            try:
                second = inspect_guest(config, manifest, output, 'repeat-' + str(attempt))
                if second['boot_id'] != first['boot_id']:
                    break
            except (ConnectionError, subprocess.TimeoutExpired):
                pass
            time.sleep(5)
        else:
            raise ValueError('重复启动后登录或客户系统检查失败，没有 skip')
    except BaseException:
        raise  # 错误路径由外层先停写，再关闭所有已登记capture。
    capture.close()
    repeated=capture.analyze()
    repeat_receipt=capture.receipt
    boot_target='/v1/boot?sn='+target['sn']+'&arch=x86_64&firmware=bios'
    localboot=hashlib.sha256(b'#!ipxe\nexit || sanboot --no-describe --drive 0x80\n').hexdigest()
    matching=[row for row in repeated['http'] if row['method']=='GET' and row['target_sha256']==hashlib.sha256(boot_target.encode()).hexdigest()
              and row['response_status']==200 and row['response_body_sha256']==localboot]
    require(len(matching)==1,'缺本客户同流GET/200完整本地盘响应')
    before_events = api.events(task['id'])
    retry = api.api('POST', '/tasks/' + task['id'] + '/retry', expected=422)
    retry_ok = any(p['code'] == 'task_retry_after_reboot' for p in retry['problems'])
    helper=load_cloud_helper('cloud_capture_session',manifest)
    install_receipt=bound_json(config['install_capture_receipt_file'],manifest['cloud']['install_capture_receipt_sha256'])
    frozen_install=capture_contract(config,manifest,'install',task)
    check_capture_receipt_binding(install_receipt,frozen_install,target,manifest['cloud']['owner'])
    require(install_receipt['owner']==manifest['cloud']['owner'] and install_receipt['target_uuid']==target['uuid'], '装机抓包原件属于别轮客户')
    install_anchor=bound_json(config['install_capture_anchor_file'],manifest['cloud']['install_capture_anchor_sha256'])
    installed=helper.analyze_session(install_receipt,frozen_install,target,anchor=install_anchor)
    original=Path(install_receipt['path'])
    require(original.resolve().is_relative_to(Path(VM_BASE)/'machines') and not original.is_symlink(),'装机抓包不在本轮目录')
    os.link(original,output/'install-original-private.pcap')
    callbacks=[row for row in installed['http'] if row['callback'] and row['response_status']==200]
    require(callbacks,'没有真实成功回调，不制造新信号')
    captured=helper.extract_verified_callback(install_receipt,frozen_install,target,callbacks[-1]['reference'],anchor=install_anchor)
    path=captured['target']
    require(path.startswith('/v1/signal/'+task['id']+'/'),'回调属于其他任务')
    url = config['api_url'].removesuffix('/console/v1') + path
    request = urllib.request.Request(url,method='POST',data=captured['body'],
        headers={'Content-Type':captured['content_type']} if captured['content_type'] is not None else {})
    verify_instance(config,manifest)
    try:
        with api.opener.open(request, timeout=15) as response:
            callback_status = response.status
    except urllib.error.HTTPError as error:
        callback_status = error.code
    latest = api.api('GET', '/tasks/' + task['id'])
    unchanged = api.events(task['id']) == before_events and latest['state'] == expected
    evidence = {'media_version': manifest['cloud']['media']['version'], 'task': task, 'events': events,
        'input_files':{'prepare':{'path':config['prepare_chain_file']},'start':{'path':config['start_chain_file']},
                       'capture_contracts':config['capture_contract_files']},
        'capture_receipts':{'install':install_receipt,'repeat':repeat_receipt},
        'capture_anchors':{'install':{'path':config['install_capture_anchor_file'],'sha256':manifest['cloud']['install_capture_anchor_sha256']},
            'repeat':{'path':str(capture.anchor_file),'sha256':capture.anchor_sha256}},
        'guest': first, 'repeat': {'task_id': latest['id'], 'task_state': latest['state'], 'guest': second,
        'network_boot_request': True, 'localboot_response': True, 'retry_rejected': retry_ok,
        'duplicate_callback_rejected': callback_status in (401, 403, 409) and unchanged},
        'raw': {'install_capture_sha256': install_receipt['sha256'], 'repeat_capture_sha256': repeat_receipt['sha256'],
                'duplicate_callback_status': callback_status, 'duplicate_callback_path_sha256': hashlib.sha256(path.encode()).hexdigest()}}
    write_once(output / 'qualification-evidence-private.json', json.dumps(evidence, indent=2) + '\n')
    write_once(output/'packet-evidence-private.json',json.dumps({'install':installed,'repeat':repeated},indent=2)+'\n')
    installing=[event['at'] for event in events if event['kind']=='machine.installing']
    require(installing,'没有真实installing时间')
    write_once(output/'installation-timeline-private.json',json.dumps({'task_id':task['id'],'target_uuid':target['uuid'],
        'install_started_at':min(installing),'guest_accepted_at':int(time.time())})+'\n')
    write_once(output/'target-private.json',json.dumps(target,indent=2)+'\n')
    common=check_run_identity(config,manifest)
    return check_qualification(evidence, config['required_failure'],target=target,
        bindings={'helpers':common['installation_helpers'],'manifest':manifest,'result_root':str(output),
            'result_files':{str(path.relative_to(output)):sha256(path) for path in output.rglob('*') if path.is_file()}})


def reviewed_phase(phase, config_path, manifest_path, output):
    global COMMAND_RECORDS
    os.umask(0o077)
    require(socket.gethostname() == 'lober', '真实编排必须从 lober VM 发起')
    config, manifest = private_json(config_path), json.loads(Path(manifest_path).read_text())
    check_manifest(manifest)
    require(sha256(config_path)==manifest['cloud']['config_sha256'],'实际阶段配置不是根冻结的准确字节')
    review = manifest['cloud']['review']
    require(review.get('approved') is True and review.get('executor_sha256') == sha256(__file__)
            and review.get('root_reviewer') and review.get('independent_reviewer')
            and review['root_reviewer'] != review['independent_reviewer'], '缺少根和独立审查对当前执行器的核准')
    require(sha256(__file__) == manifest['cloud']['executor_sha256'], '当前执行器摘要不符')
    require(private_json(config['identity_file']) == manifest['cloud']['owner'], '私有身份不符')
    check_run_identity(config,manifest)
    validate_phase_helpers(manifest,phase)
    if phase=='short':validate_short_approval(config,manifest)
    phase_cases=[(config,manifest)]
    if phase=='parallel':
        refs=config['parallel_cases']
        require(len(refs)==2 and canonical_sha256(refs)==manifest['cloud']['parallel_cases_sha256'],'两客户入口列表未冻结')
        phase_cases=[]
        for ref in refs:
            child_config=bound_json(ref['config_file'],ref['config_sha256'])
            child=bound_json(ref['manifest_file'],ref['manifest_sha256'])
            check_manifest(child);check_run_identity(child_config,child)
            require(ref['config_sha256']==child['cloud']['config_sha256'],'并行case配置与其准确manifest不同')
            require(child['cloud']['review']==review and runtime_owner(child)==runtime_owner(manifest)
                and child['cloud']['instance_record_sha256']==manifest['cloud']['instance_record_sha256'],
                '并行case不是同一已审独占实例')
            require(private_json(child_config['identity_file'])==child['cloud']['owner'],'并行case owner不符')
            phase_cases.append((child_config,child))
        targets=[manifest_target(child) for _,child in phase_cases]
        for key in ('uuid','sn','mac','disk_path','disk_serial','guest_ip'):
            require(len({target[key] for target in targets})==2,'并行客户身份/盘/网络重复：'+key)
    output = Path(output).resolve()
    require(output.is_relative_to(Path(VM_BASE) / 'machines') and not output.exists(), '必须使用本轮全新输出目录')
    output.mkdir(mode=0o700)
    COMMAND_RECORDS = output
    ACTIVE_CAPTURES.clear()
    def stop():
        proof={'client_stopped':False,'imports_stopped':False,'captures_stopped':True}
        clients=[]
        for child_config,child in phase_cases:
            try:clients.append(json.loads(host_command(child_config,owned_client_stop_source(child),timeout=90)))
            except BaseException as error:clients.append({'client_stopped':False,'error':type(error).__name__})
        proof.update(client_stopped=all(item.get('client_stopped') is True for item in clients),clients=clients)
        try:proof.update(stop_import_instance(config,manifest,output))
        except BaseException as error:proof['import_error']=type(error).__name__
        for capture in list(ACTIVE_CAPTURES):
            try:capture.close()
            except BaseException as error:
                proof['captures_stopped']=False;proof['capture_error']=type(error).__name__
        proof['local_jobs_stopped']=local_jobs_stopped()
        proof['client_stopped']=proof['client_stopped'] and proof['captures_stopped'] and proof['local_jobs_stopped']
        return proof
    lease=SafetyLease(Path(VM_BASE)/'leases/heavy.lock',output,stop)
    result = {'phase': phase, 'status': 'FAIL', 'scope': 'debian-x86_64-bios-single-guest',
              'run_id': manifest['run_id'], 'manifest_sha256': sha256(manifest_path),
              'run_identity_sha256':manifest['run_identity_sha256'],'owner':manifest['cloud']['owner'],
              'product_sha': manifest['product_sha'], 'test_sha': manifest['test_sha'],
              'binary_sha256': manifest['binary_sha256'], 'executor_sha256': sha256(__file__), 'at': int(time.time())}
    if phase=='parallel':result['scope']='debian-x86_64-bios-two-guests'
    with lease:
        def probe():
            current=phase_resources(config,manifest)
            require(current['mount']==baseline['mount'],'执行中介质盘被重新挂载/替换')
            for capture in list(ACTIVE_CAPTURES):capture.check()
        try:
            baseline=phase_resources(config,manifest,initial=True)
            with PhaseWatch(probe):
                if phase=='prepare':prepare_api(config,manifest,output);result['status']='MEDIA_IMPORTED'
                elif phase=='short':result.update(short_no_task(config,manifest,output))
                elif phase=='boot':
                    write_once(output/'actual-boot-files.json',stage_boot(config,manifest)+'\n');result['status']='BOOT_FILES_STAGED'
                elif phase=='task':
                    task=start_api(config,manifest,output);result.update(status='TASK_CREATED_PENDING_CAPTURE_FREEZE',task_id=task['id'])
                elif phase=='start':
                    task=install(config,manifest,output);result.update(status='TERMINAL',task_id=task['id'],task_state=task['state'])
                elif phase=='parallel':
                    tasks=parallel_install(phase_cases,output);result.update(status='TWO_TERMINAL_PENDING_INDIVIDUAL_QUALIFICATION',task_ids=[task['id'] for task in tasks])
                elif phase=='qualify':result.update(qualify(config,manifest,output))
                else:raise ValueError('unsupported reviewed phase')
            result['files']={str(p.relative_to(output)):sha256(p) for p in output.rglob('*') if p.is_file()}
            # 最终原件写失败也进入同一停止路径，不能先return成功再finally吞异常。
            write_once(output/'result.json',json.dumps(result,ensure_ascii=False,indent=2)+'\n')
        except BaseException as error:
            try:write_once(output/('failure-'+str(time.time_ns())+'.json'),json.dumps({'error_type':type(error).__name__,'status':'FAIL'}))
            except OSError:pass
            raise
    return result


def main():
    global COMMAND_RECORDS
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--phase', required=True, choices=['preflight','define','prepare','short','task','start','parallel','qualify'])
    parser.add_argument('--config', required=True, type=Path)
    parser.add_argument('--manifest', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    os.umask(0o077)
    require(socket.gethostname() == 'lober', '所有真实编排只能从 lober VM 发起')
    config = private_json(args.config)
    manifest = json.loads(args.manifest.read_text())
    check_manifest(manifest)
    owner = manifest['cloud']['owner']
    identity = private_json(config['identity_file'])
    require(identity == owner, '私有身份文件与冻结 manifest 不符')
    require(sha256(__file__) == manifest['cloud']['executor_sha256'], '当前执行器不是冻结执行器')
    if args.phase in ('prepare','short','task','start','parallel','qualify') and config.get('execute_reviewed') is True:
        reviewed_phase(args.phase, args.config, args.manifest, args.output)
        return 0
    output = args.output.resolve()
    require(output.is_relative_to(Path(VM_BASE) / 'machines') and not output.exists(), '输出必须为本轮 machines 下新目录')
    output.mkdir(mode=0o700)
    COMMAND_RECORDS = output
    result = {'phase': args.phase, 'at': int(time.time()), 'status': 'BLOCKED',
              'manifest_sha256': sha256(args.manifest),
              'run_id': manifest['run_id'], 'binary_sha256': manifest['binary_sha256'],
              'executor_sha256': sha256(__file__), 'scope': 'debian-x86_64-bios-single-guest'}
    try:
        if args.phase == 'preflight':
            value = collect_preflight(config, owner)
            write_once(output / 'preflight.json', json.dumps(value, indent=2) + '\n')
            result['status'] = 'PREFLIGHT_OK'
        elif args.phase == 'define':
            target=manifest_target(manifest)
            if target!=target_spec(owner):
                plan=propose_target_contract(owner,target,manifest['cloud']['target_contract']['preserve_failed_disks'])
                write_once(output/'target-resource-proposal.json',json.dumps(plan,indent=2)+'\n')
                write_once(output/'target-domain-proposal.xml',plan['domain_xml']+'\n')
            else:
                for name, body in definitions(owner).items():write_once(output/name,body+'\n')
                for name, body in mutation_scripts(owner).items():write_once(output/name,body)
                collect_preflight(config,owner)
            result['status'] = 'REVIEW_REQUIRED'
            result['reason'] = '仅生成精确脚本；创建、附盘、格式化、改网和启动尚未执行'
        else:
            scripts = mutation_scripts(owner)
            if args.phase in ('short','task','parallel'):
                write_once(output/(args.phase+'-phase-review-required.json'),json.dumps({'review_required':True,'phase':args.phase,'executes_mutation':False}))
            else:
                selected = {'prepare':'06-vm-prepare-api.py','start':'07-vm-install.py','qualify':'08-vm-qualify.py'}[args.phase]
                write_once(output / selected, scripts[selected])
            result['status'] = 'REVIEW_REQUIRED'
            result['reason'] = '仅生成当前阶段执行脚本；核准准确摘要后才设置 execute_reviewed=true'
        result['files'] = {p.name: sha256(p) for p in output.iterdir() if p.is_file()}
        write_once(output / 'result.json', json.dumps(result, ensure_ascii=False, indent=2) + '\n')
        print(json.dumps(result, ensure_ascii=False))
        return 0 if result['status'] in ('PREFLIGHT_OK', 'REVIEW_REQUIRED') else 2
    except Exception as error:
        result['reason'] = str(error)
        write_once(output / 'result.json', json.dumps(result, ensure_ascii=False, indent=2) + '\n')
        raise


if __name__ == '__main__':
    raise SystemExit(main())
