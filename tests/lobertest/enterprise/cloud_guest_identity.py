"""从可信客户代理与严格 SSH 两边核对身份；只在 lober VM 运行。

调用方负责用准确 UUID 执行下面的采集脚本，并保留代理原件。
本文件不连接宿主，不读取旧 SSH 配置，也不改变全局 known_hosts。
"""
import base64
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import socket
import stat
import struct
import subprocess
import uuid


GUEST_IDENTITY_SCRIPT = r'''import ipaddress,json,pathlib,stat,subprocess
def run(*args):
 return subprocess.check_output(args,text=True,timeout=15).strip()
def need(ok,message):
 if not ok:raise ValueError(message)
def serial():
 try:return pathlib.Path('/sys/class/dmi/id/product_serial').read_text().strip()
 except PermissionError:
  p=pathlib.Path('/var/lib/lober-qualification/smbios-sn')
  s=p.lstat()
  need(stat.S_ISREG(s.st_mode) and s.st_uid==0 and stat.S_IMODE(s.st_mode)==0o444,'untrusted SMBIOS copy')
  return p.read_text().strip()
root=run('findmnt','-n','-o','SOURCE','/')
serials=set(s.strip() for s in run('lsblk','-s','-n','-o','SERIAL',root).splitlines() if s.strip())
need(len(serials)==1,'root disk serial is missing or ambiguous')
addresses=[a['local'] for link in json.loads(run('ip','-j','address')) for a in link.get('addr_info',[])
 if a.get('family')=='inet' and not ipaddress.ip_address(a['local']).is_loopback
 and not ipaddress.ip_address(a['local']).is_link_local]
need(len(addresses)==1,'guest address is missing or ambiguous')
key=pathlib.Path('/etc/ssh/ssh_host_ed25519_key.pub').read_text().strip().split()
need(len(key)>=2 and key[0]=='ssh-ed25519','missing ed25519 host public key')
print(json.dumps({'address':addresses[0],'ssh_host_public_key':' '.join(key[:2]),
 'smbios_serial':serial(),'root_disk_serial':next(iter(serials)),
 'root_uuid':run('findmnt','-n','-o','UUID','/'),
 'machine_id':pathlib.Path('/etc/machine-id').read_text().strip(),
 'boot_id':pathlib.Path('/proc/sys/kernel/random/boot_id').read_text().strip()}))
'''

IDENTITY_FIELDS = frozenset(('address', 'ssh_host_public_key', 'smbios_serial',
                             'machine_id', 'boot_id', 'root_uuid', 'root_disk_serial'))
AGENT_FIELDS = frozenset(('target_uuid', 'transport', 'collection_script_sha256',
                         'identity', 'raw_path', 'raw_sha256'))


def require(ok, message):
    if not ok:
        raise ValueError(message)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def json_bytes(value):
    return (json.dumps(value, sort_keys=True, ensure_ascii=False, indent=2) + '\n').encode()


def unique_object(pairs):
    value = {}
    for key, item in pairs:
        require(key not in value, '身份原件有重复字段：' + key)
        value[key] = item
    return value


def strict_json(data):
    try:
        return json.loads(data, object_pairs_hook=unique_object)
    except (UnicodeError, json.JSONDecodeError) as error:
        raise ValueError('身份原件不是唯一完整 JSON') from error


def check_identity(value, target):
    require(isinstance(value, dict) and set(value) == IDENTITY_FIELDS, '身份字段缺失或有未知字段')
    require(all(type(v) is str and 0 < len(v) <= 512 and '\n' not in v and '\r' not in v
                for v in value.values()), '身份字段为空或类型无效')
    require(value['address'] == str(ipaddress.IPv4Address(target['guest_ip'])), '地址不是准确目标')
    require(value['smbios_serial'] == target['sn'], '客户 SMBIOS 序号不符')
    require(value['root_disk_serial'] == target['disk_serial'], '客户根盘序号不符')
    require(re.fullmatch('[0-9a-f]{32}', value['machine_id']) is not None
            and value['machine_id'] != '0' * 32, 'machine-id 无效')
    for name in ('boot_id', 'root_uuid'):
        require(str(uuid.UUID(value[name])) == value[name] and uuid.UUID(value[name]).int != 0,
                name + ' 无效')
    key = value['ssh_host_public_key']
    require(re.fullmatch(r'ssh-ed25519 [A-Za-z0-9+/]+={0,2}', key) is not None, '主机公钥格式无效')
    try:
        wire = base64.b64decode(key.split()[1], validate=True)
    except ValueError as error:
        raise ValueError('主机公钥编码无效') from error
    require(len(wire) == 51 and wire[:19] == struct.pack('>I', 11) + b'ssh-ed25519' + struct.pack('>I', 32),
            '主机公钥不是完整 ed25519 公钥')
    return value


def safe_path(path):
    path = Path(path)
    require(path.is_absolute() and re.fullmatch(r'/[A-Za-z0-9_./-]+', str(path)) is not None
            and '..' not in path.parts, '私有路径必须是无展开字符的绝对路径')
    for parent in (path, *path.parents):
        require(not parent.is_symlink(), '私有路径不能经过符号链接')
    return path


def private_file(path):
    path = safe_path(path)
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid()
            and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1,
            '私有文件必须归当前用户独占且权限为 0600')
    require(path.stat().st_size <= 1024 * 1024, '私有文件过大')
    return {'path': str(path), 'sha256': digest(path.read_bytes()), 'uid': info.st_uid,
            'gid': info.st_gid, 'mode': stat.S_IMODE(info.st_mode),
            'device': info.st_dev, 'inode': info.st_ino, 'size': info.st_size}


def private_directory(path):
    path = safe_path(path)
    info = path.lstat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid()
            and stat.S_IMODE(info.st_mode) == 0o700, '目录必须由当前用户独占，权限为 0700')


def write_once(path, content):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'wb') as output:
        output.write(content if isinstance(content, bytes) else content.encode())


def text(value):
    return value.decode(errors='replace') if isinstance(value, bytes) else (value or '')


def verify_guest_identity(config, target_contract, agent_evidence, output_dir, command_runner):
    """runner 接收 (argv, stdin=脚本, timeout=秒)，返回字符串或 CompletedProcess。

    字符串只适配已有的非零即抛错执行器。失败保留原件并抛错，绝不改为跳过。
    输出目录必须是本轮尚不存在的新目录；调用方负责接管客户机停写。
    """
    require(socket.gethostname() == 'lober' and os.geteuid() == 1000, '只能在 lober VM 的 dev 用户执行')
    output = safe_path(output_dir)
    private_directory(output.parent)
    require(not output.exists(), '不能复用身份核对输出目录')
    output.mkdir(mode=0o700)
    os.chmod(output, 0o700)
    try:
        target = dict(target_contract)
        require({'uuid', 'sn', 'disk_serial', 'guest_ip'}.issubset(target), '目标合同缺少身份字段')
        require(str(uuid.UUID(target['uuid'])) == target['uuid'], '目标 UUID 无效')
        for field in ('sn', 'disk_serial'):
            require(type(target[field]) is str and re.fullmatch(r'[A-Za-z0-9_-]{1,128}', target[field]) is not None,
                    '目标序号无效：' + field)
        ip = ipaddress.IPv4Address(target['guest_ip'])
        require(str(ip) == target['guest_ip'] and not ip.is_loopback and not ip.is_unspecified
                and not ip.is_multicast and not ip.is_link_local, '目标地址无效')
        owner = dict(config['owner'])
        require(set(owner) == {'purpose', 'run_id', 'target_uuid'}
                and owner['purpose'] == 'production-qualification-20261025'
                and re.fullmatch(r'[a-z0-9][a-z0-9-]{2,63}', owner['run_id']) is not None
                and owner['target_uuid'] == target['uuid'], '目标不属于本轮 owner')
        require(isinstance(agent_evidence, dict) and set(agent_evidence) == AGENT_FIELDS, '代理来源字段缺失或未知')
        require(agent_evidence['target_uuid'] == target['uuid']
                and agent_evidence['transport'] == 'libvirt-qga'
                and agent_evidence['collection_script_sha256'] == digest(GUEST_IDENTITY_SCRIPT.encode()),
                '代理来源不是准确 UUID 或冻结脚本')
        raw = private_file(agent_evidence['raw_path'])
        require(raw['sha256'] == agent_evidence['raw_sha256'], '代理原件摘要变化')
        identity = check_identity(strict_json(Path(raw['path']).read_bytes()), target)
        require(identity == agent_evidence['identity'], '代理身份与原件不符')
        key = private_file(config['guest_ssh_identity_file'])
        private_directory(Path(key['path']).parent)
        user = config['guest_ssh_user']
        require(type(user) is str and re.fullmatch(r'[a-z_][a-z0-9_-]{0,31}', user) is not None, '客户登录用户名无效')
        timeout = config.get('guest_ssh_timeout', 30)
        require(type(timeout) is int and 1 <= timeout <= 60, 'SSH 总超时必须为 1 至 60 秒')
        alias = 'pq-guest-' + target['uuid']
        known_hosts = output / 'known_hosts'
        configuration = output / 'ssh_config'
        # -F 避开用户和系统配置；所有路径都拒绝展开字符。
        settings = '\n'.join([
            'Host pq-guest', '  HostName ' + target['guest_ip'], '  User ' + user, '  Port 22',
            '  HostKeyAlias ' + alias, '  UserKnownHostsFile ' + str(known_hosts),
            '  GlobalKnownHostsFile /dev/null', '  IdentityFile ' + key['path'],
            '  StrictHostKeyChecking yes', '  BatchMode yes', '  IdentitiesOnly yes',
            '  IdentityAgent none', '  PubkeyAuthentication yes', '  PreferredAuthentications publickey',
            '  PasswordAuthentication no', '  KbdInteractiveAuthentication no',
            '  HostKeyAlgorithms ssh-ed25519', '  VerifyHostKeyDNS no', '  UpdateHostKeys no',
            '  CheckHostIP yes', '  CanonicalizeHostname no', '  ProxyCommand none', '  ProxyJump none',
            '  PermitLocalCommand no', '  ClearAllForwardings yes', '  ForwardAgent no',
            '  ForwardX11 no', '  RequestTTY no', '  ControlMaster no', '  ControlPath none',
            '  ConnectTimeout 8', '  ConnectionAttempts 1', '  ServerAliveInterval 5',
            '  ServerAliveCountMax 1', ''])
        write_once(known_hosts, alias + ' ' + identity['ssh_host_public_key'] + '\n')
        write_once(configuration, settings)
        binding = {'owner': owner, 'target_contract': target, 'agent_evidence': agent_evidence,
                   'agent_raw_file': raw, 'identity_file': key,
                   'configuration': private_file(configuration), 'known_hosts': private_file(known_hosts),
                   'collection_script_sha256': digest(GUEST_IDENTITY_SCRIPT.encode())}
        binding_path = output / 'binding.json'
        write_once(binding_path, json_bytes(binding))
        frozen = [raw, key, binding['configuration'], binding['known_hosts'], private_file(binding_path)]
        argv = ['/usr/bin/ssh', '-F', str(configuration), 'pq-guest', '/usr/bin/python3 -']
        write_once(output / 'ssh-begin.json', json_bytes({'argv': argv, 'timeout': timeout,
                   'stdin_sha256': digest(GUEST_IDENTITY_SCRIPT.encode()), 'state': 'STARTED'}))
        stdout, stderr, code, state = '', '', None, 'INTERRUPTED'
        try:
            result = command_runner(argv, stdin=GUEST_IDENTITY_SCRIPT, timeout=timeout)
            if isinstance(result, str):
                code, stdout = 0, result
            else:
                code, stdout, stderr = result.returncode, text(result.stdout), text(result.stderr)
            state = 'EXITED'
        except subprocess.TimeoutExpired as error:
            stdout, stderr, state = text(error.output), text(error.stderr), 'TIMEOUT'
            raise
        except subprocess.CalledProcessError as error:
            stdout, stderr, code, state = text(error.output), text(error.stderr), error.returncode, 'EXITED'
            raise
        finally:
            write_once(output / 'ssh.stdout', stdout)
            write_once(output / 'ssh.stderr', stderr)
            write_once(output / 'ssh-end.json', json_bytes({'state': state, 'exit': code}))
        require(type(code) is int and code == 0, '真实 SSH 登录失败')
        private_directory(output)
        for snapshot in frozen:
            require(private_file(snapshot['path']) == snapshot, '冻结私有文件变化：' + Path(snapshot['path']).name)
        ssh = check_identity(strict_json(stdout), target)
        for field in IDENTITY_FIELDS:
            require(ssh[field] == identity[field], 'SSH 与可信客户身份不符：' + field)
        result = {**ssh, 'ssh_exit': code, 'host_public_key': ssh['ssh_host_public_key'],
                  'access': {'owner': owner, 'target_uuid': target['uuid'], 'address': ssh['address'],
                             'host_public_key': ssh['ssh_host_public_key'],
                             'binding_file': str(binding_path), 'binding_sha256': digest(binding_path.read_bytes()),
                             'ssh_stdout_file': str(output / 'ssh.stdout')}}
        write_once(output / 'result.json', json_bytes(result))
        return result
    except BaseException as error:
        write_once(output / 'failure.json', json_bytes({'status': 'FAILED', 'error_type': type(error).__name__,
                   'message': str(error)}))
        raise
