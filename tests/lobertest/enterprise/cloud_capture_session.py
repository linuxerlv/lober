"""本轮私有抓包生命周期；只证明原件来源，不签发装机通过。"""
import copy
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import socket
import stat
import time
import uuid

SCHEMA = 'lober-cloud-capture-session-v1'
HOST = 'ser866667880745'
BRIDGE = 'virbr-pq25'
NETWORK = 'lober-pq25'
VM_BASE = Path('/home/dev/lober-production-qualification-20261025')
PURPOSE = 'production-qualification-20261025'
PARSER_SHA = 'b319d32db8a14f238c437843523428bea8d4274d5042d9fc9c51f6c997a481d0'


class CaptureSessionError(ValueError):
    """失败须交给主层停止客户写盘；未知停止状态不能释放重任务锁。"""


def need(ok, message):
    if not ok:
        raise CaptureSessionError(message)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def canonical(value):
    return digest(json.dumps(value, sort_keys=True, separators=(',', ':')).encode())


def private_write(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w', encoding='utf-8') as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write('\n'); stream.flush(); os.fsync(stream.fileno())


def private_dir(path):
    path = Path(path)
    need(path.is_absolute() and not path.is_symlink() and path.is_dir(), '私有目录无效')
    for parent in [path, *path.parents]:
        need(not parent.is_symlink(), '目录祖先不能是符号链接')
    info = path.stat()
    need(info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o700, '私有目录归属或权限不符')
    return path


def validate(owner, target, frozen):
    need(type(owner) is dict and set(owner) == {'purpose', 'run_id', 'target_uuid'}, 'owner 字段不符')
    need(owner['purpose'] == PURPOSE and re.fullmatch(r'[a-z0-9][a-z0-9-]{2,63}', owner['run_id']) is not None, 'owner 不属本轮')
    need(str(uuid.UUID(owner['target_uuid'])) == owner['target_uuid'], '目标 UUID 无效')
    need(target['uuid'] == owner['target_uuid'] and target['host'] == HOST and target['bridge'] == BRIDGE
         and target['network'] == NETWORK and target['candidate_port'] == 18090 and target['artifact_port'] == 18082, '目标或新桥不符')
    need(target['uuid'] not in {'895a44e1-8abd-4463-bbf7-fba8a743ed59', '33d609aa-8a55-4855-abb1-89634a403fe1'}, '禁止服务或旧客户')
    keys = {'schema', 'host', 'bridge', 'network', 'targets', 'tftp_port_range', 'max_bytes', 'max_seconds', 'tcp_reset_policy'}
    need(type(frozen) is dict and set(frozen) == keys and frozen['schema'] == SCHEMA, '抓包合同字段不符')
    need((frozen['host'], frozen['bridge'], frozen['network']) == (HOST, BRIDGE, NETWORK), '抓包位置不符')
    need(frozen['tftp_port_range'] == [40000, 40010], 'TFTP 端口范围必须为已冻结范围')
    need(type(frozen['max_bytes']) is int and 24 <= frozen['max_bytes'] <= 64 * 1024**2, '原件容量上限无效')
    need(type(frozen['max_seconds']) is int and 1 <= frozen['max_seconds'] <= 86400, '抓包时间上限无效')
    need(frozen['tcp_reset_policy'] in {'no-rst', 'rfc5961-exact-rcv-nxt'}, '未冻结客户复位行为')
    rows = frozen['targets']
    need(type(rows) is dict and 1 <= len(rows) <= 2 and target['uuid'] in rows, '目标不在显式合同中')
    row = rows[target['uuid']]
    row_keys = {'target_sha256', 'packet_contract', 'boot_chain'}
    need(type(row) is dict and set(row) in (row_keys, row_keys | {'packet_mode'})
         and row['target_sha256'] == canonical(target), '目标完整合同摘要不符')
    mode = row.get('packet_mode', 'TASK')
    need(mode in ('TASK', 'NO_TASK'), '未知抓包模式')
    packet = row['packet_contract']
    packet_keys = {'schema', 'client_mac', 'boot_server_mac', 'service_mac', 'client_ip', 'boot_server_ip', 'service_ip', 'service_port', 'task_id', 'tftp', 'http'}
    if mode == 'NO_TASK':
        packet_keys |= {'scope', 'client_sn', 'max_boot_exchanges'}
        need(len(rows) == 1 and packet.get('client_sn') == target['sn'], '无任务模式只允许一个准确序列号目标')
    need(type(packet) is dict and set(packet) == packet_keys, '解析合同须在启动前冻结，不能事后删必需项')
    need((packet['client_mac'], packet['client_ip'], packet['boot_server_ip'], packet['service_ip'], packet['service_port']) ==
         (target['mac'], target['guest_ip'], target['host_ip'], target['service_ip'], 18090), '解析地址与目标不符')
    from cloud_packet_capture import validate_contract, validate_no_task_contract
    validator = validate_no_task_contract if mode == 'NO_TASK' else validate_contract
    validator(dict(packet, pcap_sha256='0' * 64, pcap_size=24, capture_start_ns=0, capture_end_ns=1))
    chain = row['boot_chain']
    need(type(chain) is dict and set(chain) == {'firmware_sha256', 'dhcp_config_sha256', 'user_class_sha256', 'boot_file', 'required_tftp'}, '必须冻结实际 ROM 与 DHCP 分支')
    need(all(re.fullmatch('[0-9a-f]{64}', chain[k]) for k in ('firmware_sha256', 'dhcp_config_sha256', 'user_class_sha256')), '启动链摘要无效')
    need(chain['required_tftp'] == sorted(packet['tftp']) and chain['boot_file'] in packet['tftp'], '必需 TFTP 集合与启动分支不符')
    for key in ('client_ip', 'boot_server_ip', 'service_ip'):
        need(str(ipaddress.IPv4Address(packet[key])) == packet[key], 'IPv4 地址不规范')
    return row


def capture_filter(packet):
    # DHCP 用 BOOTP 内的客户硬件地址，保留发向广播地址的 offer/ack。
    mac = packet['client_mac'].replace(':', '')
    dhcp = '(udp and (port 67 or port 68) and udp[36:4] = 0x' + mac[:8] + ' and udp[40:2] = 0x' + mac[8:] + ')'
    pair = '(host ' + packet['client_ip'] + ' and host ' + packet['boot_server_ip'] + ')'
    tftp = '(udp and ' + pair + ' and (port 69 or portrange 40000-40010))'
    http = '(tcp and host ' + packet['client_ip'] + ' and host ' + packet['service_ip'] + ' and port 18090)'
    return '(' + dhcp + ' or ' + tftp + ' or ' + http + ')'


# 这段程序只由既有严格宿主入口发送。测试不调用真实抓包入口。
HOST_RUNTIME = r'''
import hashlib,json,os,re,signal,socket,stat,subprocess,sys,time
from pathlib import Path
import xml.etree.ElementTree as ET
os.umask(0o077)

def require(ok,message):
 if not ok: raise RuntimeError(message)
def sha(path):
 h=hashlib.sha256()
 with open(path,'rb') as f:
  for b in iter(lambda:f.read(1048576),b''):h.update(b)
 return h.hexdigest()
def read(path):
 require(not path.is_symlink() and path.is_file(),'record not regular')
 require(path.stat().st_uid==0 and stat.S_IMODE(path.stat().st_mode)==0o600,'record permissions')
 return json.loads(path.read_text())
def write(path,value):
 fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'w') as f:json.dump(value,f,sort_keys=True);f.flush();os.fsync(f.fileno())
def identity(pid):
 p=Path('/proc')/str(pid)
 tail=(p/'stat').read_text().rsplit(')',1)[1].split()
 return {'pid':pid,'starttime':tail[19],'uid':p.stat().st_uid,'exe':os.readlink(p/'exe'),
  'argv':(p/'cmdline').read_bytes().rstrip(b'\0').decode().split('\0'),
  'boot_id':Path('/proc/sys/kernel/random/boot_id').read_text().strip()}
def same(record):
 try:return identity(record['pid'])==record
 except FileNotFoundError:return False
def file_id(path):
 st=path.stat(follow_symlinks=False)
 require(stat.S_ISREG(st.st_mode) and st.st_uid==0 and stat.S_IMODE(st.st_mode)==0o600,'pcap permissions')
 return {'device':st.st_dev,'inode':st.st_ino,'mtime_ns':st.st_mtime_ns,'size':st.st_size}
def context(r):
 require(socket.gethostname()=='ser866667880745' and os.geteuid()==0,'host identity')
 base=Path(r['base'])
 require(base.is_relative_to('/var/lib/libvirt/images/lober-pq-20261025') and '..' not in base.parts,'base outside round')
 for p in [base,*base.parents]:require(not p.is_symlink(),'symlink ancestor')
 require(read(base/'.owner.json')==r['owner'],'owner changed')
 require(r['target']['bridge']=='virbr-pq25' and r['target']['network']=='lober-pq25','bridge changed')
 def virsh(*args):return subprocess.check_output(['virsh','-c','qemu:///system',*args],text=True,timeout=10)
 domain=ET.fromstring(virsh('dumpxml',r['target']['uuid']))
 require(domain.findtext('uuid')==r['owner']['target_uuid'],'target uuid')
 interfaces=domain.findall('./devices/interface')
 require(len(interfaces)==1 and interfaces[0].find('mac').get('address')==r['target']['mac'] and interfaces[0].find('source').get('network')=='lober-pq25','target interface')
 net=ET.fromstring(virsh('net-dumpxml','lober-pq25'))
 require(net.find('bridge').get('name')=='virbr-pq25','actual bridge')
 require(net.find('mac').get('address')==r['packet']['boot_server_mac'],'boot server mac')
 require(Path('/sys/class/net/virbr-pq25/bridge').is_dir(),'not a bridge')
 return base
def supervise(r):
 context(r)
 folder=Path(r['folder']); log=folder/'tcpdump.stderr'; capture=folder/'capture.pcap'
 argv=['/usr/bin/tcpdump','-Z','root','-U','-n','-s','0','-B','4096','-i','virbr-pq25','-w',str(capture),r['filter']]
 started=time.time_ns(); clock_start=time.monotonic(); last_stats=clock_start; child=None; ident=None; reason='INTERNAL_FAILURE'; stopped=False
 try:
  with log.open('xb',buffering=0) as err:
   os.chmod(log,0o600)
   child=subprocess.Popen(argv,stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=err,start_new_session=True)
   # Popen 已完成 exec；在任何后续动作前记下准确进程。
   ident=identity(child.pid)
   require(ident['uid']==0 and ident['exe']=='/usr/bin/tcpdump' and ident['argv']==argv,'tcpdump identity')
   write(folder/'process.json',{'process':ident,'capture_start_ns':started,'argv':argv})
   ready=False
   while True:
    if child.poll() is not None:reason='EARLY_EXIT';break
    require(same(ident),'process identity changed')
    if capture.exists() and file_id(capture)['size']>r['max_bytes']:reason='CAPACITY_EXCEEDED';break
    if time.monotonic()-clock_start>r['max_seconds']:reason='TIMEOUT';break
    text=log.read_text()
    if any(int(v)>0 for v in re.findall(r'(?m)^(\d+) packets? dropped by (?:kernel|interface)$',text)):reason='PACKET_DROP';break
    if ready and time.monotonic()-last_stats>=2:
     fd=os.pidfd_open(child.pid)
     try:
      require(same(ident),'cannot signal reused pid');signal.pidfd_send_signal(fd,signal.SIGUSR1)
     finally:os.close(fd)
     last_stats=time.monotonic()
    if (folder/'stop-request.json').exists():
     require(read(folder/'stop-request.json')=={'session_id':r['session_id'],'owner':r['owner']},'stop owner changed')
     reason='REQUESTED';break
    if not ready and b'listening on virbr-pq25,' in log.read_bytes():
     write(folder/'ready.json',{'process':ident,'capture_start_ns':started,'session_id':r['session_id']});ready=True
    time.sleep(.1)
   if child.poll() is None:
    require(same(ident),'cannot signal reused pid')
    fd=os.pidfd_open(child.pid)
    try:
     require(same(ident),'cannot signal reused pid')
     signal.pidfd_send_signal(fd,signal.SIGINT)
    finally:os.close(fd)
   child.wait(timeout=12);stopped=True
  end=time.time_ns()
  text=log.read_text()
  counts={k:int(v) for v,k in re.findall(r'(?m)^(\d+) packets? (captured|received by filter|dropped by kernel)$',text)}
  require(set(counts)=={'captured','received by filter','dropped by kernel'} and counts['captured']>0 and counts['dropped by kernel']==0
   and not any(int(v)>0 for v in re.findall(r'(?m)^(\d+) packets? dropped by interface$',text)),'missing statistics or lost packets')
  require(child.returncode==0 and ready,'capture exit/start failure')
  require(sorted(p.name for p in folder.iterdir() if p.name.startswith('capture.pcap'))==['capture.pcap'],'rotated or extra capture')
  before=file_id(capture); checksum=sha(capture)
  require(before==file_id(capture) and 24<=before['size']<=r['max_bytes'],'capture changed or capacity exceeded')
  result={'state':'FROZEN' if reason=='REQUESTED' else 'FAILED','reason':reason,'stopped':True,
   'process':ident,'capture_start_ns':started,'capture_end_ns':end,'file_identity':before,'sha256':checksum,
   'size':before['size'],'host_path':str(capture),'counts':counts,'exit':child.returncode}
 except BaseException as error:
  # 已知准确子进程时才请求退出。无法确认退出会明确阻止主层解锁。
  if child is not None and child.poll() is None and ident is not None:
   try:
    require(same(ident),'pid changed')
    fd=os.pidfd_open(child.pid)
    try:
     require(same(ident),'pid changed');signal.pidfd_send_signal(fd,signal.SIGINT)
    finally:os.close(fd)
    child.wait(timeout=12);stopped=True
   except BaseException:stopped=False
  elif child is not None and child.poll() is not None:stopped=True
  result={'state':'FAILED','reason':reason,'error_type':type(error).__name__,'stopped':stopped,'process':ident}
 result.update(session_id=r['session_id'],owner=r['owner'],target_uuid=r['target']['uuid'],bridge='virbr-pq25',filter=r['filter'])
 write(folder/'terminal.json',result)

def control(r,action):
 context(r); folder=Path(r['folder'])
 if action=='start':
  parent=folder.parent
  if not parent.exists():parent.mkdir(mode=0o700)
  require(not parent.is_symlink() and parent.stat().st_uid==0 and stat.S_IMODE(parent.stat().st_mode)==0o700,'capture root private')
  folder.mkdir(mode=0o700)
  write(folder/'request.json',r)
  source=folder/'supervisor.py'
  fd=os.open(source,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
  with os.fdopen(fd,'w') as f:f.write(r['runtime_source']+'\nr=json.loads(Path(__file__).with_name("request.json").read_text());supervise(r)\n');f.flush();os.fsync(f.fileno())
  with (folder/'supervisor.stderr').open('xb') as err:
   os.chmod(folder/'supervisor.stderr',0o600)
   p=subprocess.Popen([sys.executable,str(source)],stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=err,start_new_session=True)
  write(folder/'supervisor.json',identity(p.pid))
 else:
  require(read(folder/'request.json')==r,'session request changed')
 if action=='close':
  stop=folder/'stop-request.json'
  if not stop.exists():write(stop,{'session_id':r['session_id'],'owner':r['owner']})
 deadline=time.monotonic()+(18 if action=='close' else 8 if action=='start' else 0)
 while True:
  if (folder/'terminal.json').exists():return read(folder/'terminal.json')
  sup=read(folder/'supervisor.json');require(same(sup),'supervisor disappeared or changed')
  if action!='close' and (folder/'ready.json').exists():
   ready=read(folder/'ready.json');require(same(ready['process']),'capture exited or changed')
   return dict(ready,state='RUNNING',owner=r['owner'],target_uuid=r['target']['uuid'],bridge='virbr-pq25',filter=r['filter'])
  require(time.monotonic()<deadline,'capture state unknown')
  time.sleep(.1)
'''


class CaptureSession:
    def __init__(self, config, owner, target_contract, frozen_capture_contract, output_dir, host_command):
        need(socket.gethostname() == 'lober' and os.geteuid() == 1000, '只能在 lober VM 的 dev1000 运行')
        self.config = config
        self.owner, self.target, self.frozen = copy.deepcopy((owner, target_contract, frozen_capture_contract))
        self.row = validate(self.owner, self.target, self.frozen)
        self.output = private_dir(output_dir)
        need(self.output.resolve().is_relative_to(VM_BASE), '输出不在本轮 VM 目录')
        need(callable(getattr(host_command, 'stream_to_private_file', None)), '宿主入口缺少不泄露原件的二进制落盘能力')
        self.host = host_command
        self.session_id = uuid.uuid4().hex
        base = str(Path(self.target['disk_path']).parent)
        self.request = {'session_id': self.session_id, 'base': base, 'folder': base + '/private-captures/' + self.session_id,
            'owner': self.owner, 'target': self.target, 'packet': self.row['packet_contract'], 'filter': capture_filter(self.row['packet_contract']),
            'max_bytes': self.frozen['max_bytes'], 'max_seconds': self.frozen['max_seconds'], 'runtime_source': HOST_RUNTIME}
        self.attempted = False
        self.process = None
        self.receipt = None
        private_write(self.output / ('capture-' + self.session_id + '-intent.json'), self.request)

    def script(self, action):
        return HOST_RUNTIME + '\nr=' + repr(self.request) + '\nprint(json.dumps(control(r,' + repr(action) + ')))\n'

    def _call(self, action):
        value = json.loads(self.host(self.config, self.script(action), timeout=35))
        need(value.get('session_id') == self.session_id and value.get('owner') == self.owner
             and value.get('target_uuid') == self.target['uuid'] and value.get('bridge') == BRIDGE
             and value.get('filter') == self.request['filter'], '宿主回执身份不符')
        process = value.get('process')
        need(value.get('state') not in {'RUNNING', 'FROZEN'} or process is not None, '运行或冻结回执缺少准确进程身份')
        if process is not None:
            validate_process(process, self.request['folder'], self.request['filter'])
            need(self.process is None or process == self.process, '准确原抓包进程身份变化，禁止按新 PID 停止')
            self.process = copy.deepcopy(process)
        private_write(self.output / ('capture-' + self.session_id + '-' + action + '-' + str(time.time_ns()) + '.json'), value)
        return value

    def start(self):
        need(not self.attempted, '同一抓包不能重复启动')
        self.attempted = True
        value = self._call('start')
        need(value.get('state') == 'RUNNING', '抓包未运行；主层必须停止客户写盘')
        return value

    def check(self):
        need(self.attempted and self.receipt is None, '抓包不在运行阶段')
        value = self._call('check')
        need(value.get('state') == 'RUNNING', '抓包已失败或提前停止；主层必须停止客户写盘')
        return value

    def close(self):
        if self.receipt is not None:
            verify_receipt(self.receipt)
            return copy.deepcopy(self.receipt)
        if not self.attempted:
            return {'stopped': True, 'state': 'NEVER_STARTED', 'scope': 'CAPTURE_LIFECYCLE_ONLY'}
        terminal = self._call('close')
        need(terminal.get('stopped') is True, '抓包停止未知；禁止释放重任务锁')
        need(terminal.get('state') == 'FROZEN' and terminal.get('reason') == 'REQUESTED', '抓包失败，不能冻结为有效原件')
        need(terminal.get('host_path') == self.request['folder'] + '/capture.pcap', '宿主原件路径不符')
        path = self.output / ('capture-' + self.session_id + '.pcap')
        proof = self.host.stream_to_private_file(self.config, self._stream_script(terminal), path, timeout=60)
        need(proof == {'path': str(path), 'sha256': terminal['sha256'], 'bytes': terminal['size'], 'exit': 0}, '私有原件传输回执不符')
        receipt = dict(terminal, path=str(path), scope='CAPTURE_LIFECYCLE_ONLY', contract_sha256=canonical(self.frozen),
                       target_sha256=canonical(self.target), packet_contract=copy.deepcopy(self.row['packet_contract']),
                       boot_chain=copy.deepcopy(self.row['boot_chain']), tcp_reset_policy=self.frozen['tcp_reset_policy'],
                       max_bytes=self.frozen['max_bytes'], max_seconds=self.frozen['max_seconds'])
        if 'packet_mode' in self.row:
            receipt['packet_mode'] = self.row['packet_mode']
        receipt['local_identity'] = local_identity(path)
        verify_receipt(receipt)
        private_write(self.output / ('capture-' + self.session_id + '-receipt.json'), receipt)
        self.receipt = receipt
        return copy.deepcopy(receipt)

    def _stream_script(self, terminal):
        return HOST_RUNTIME + '\nr=' + repr(self.request) + '\nt=' + repr(terminal) + r'''
context(r)
p=Path(t['host_path'])
require(read(p.parent/'terminal.json')==t,'terminal changed')
fd=os.open(p,os.O_RDONLY|os.O_NOFOLLOW)
try:
 st=os.fstat(fd)
 expected=t['file_identity']
 actual={'device':st.st_dev,'inode':st.st_ino,'mtime_ns':st.st_mtime_ns,'size':st.st_size}
 require(actual==expected and file_id(p)==expected and sha(p)==t['sha256'],'frozen original changed')
 h=hashlib.sha256()
 while True:
  b=os.read(fd,1048576)
  if not b:break
  h.update(b);sys.stdout.buffer.write(b)
 sys.stdout.buffer.flush()
 require(h.hexdigest()==t['sha256'] and file_id(p)==expected,'original exchanged during transfer')
 after=os.fstat(fd)
 require((after.st_dev,after.st_ino,after.st_mtime_ns,after.st_size)==(st.st_dev,st.st_ino,st.st_mtime_ns,st.st_size),'open original changed')
finally:os.close(fd)
'''


def local_identity(path):
    path = Path(path)
    need(not path.is_symlink(), '原件不能是符号链接')
    st = path.stat()
    need(stat.S_ISREG(st.st_mode) and st.st_uid == 1000 and stat.S_IMODE(st.st_mode) == 0o600, '原件须是 dev 私有普通文件')
    return {'device': st.st_dev, 'inode': st.st_ino, 'mtime_ns': st.st_mtime_ns, 'size': st.st_size}


def validate_process(process, folder, filter_text):
    expected = ['/usr/bin/tcpdump', '-Z', 'root', '-U', '-n', '-s', '0', '-B', '4096', '-i', BRIDGE,
                '-w', folder + '/capture.pcap', filter_text]
    need(type(process) is dict and set(process) == {'pid', 'starttime', 'uid', 'exe', 'argv', 'boot_id'}, '缺少准确抓包进程身份')
    need(type(process['pid']) is int and process['pid'] > 1 and str(process['starttime']).isdigit()
         and process['uid'] == 0 and process['exe'] == '/usr/bin/tcpdump' and process['argv'] == expected
         and isinstance(process['boot_id'], str) and bool(process['boot_id']), '抓包进程身份或准确参数不符')


def verify_receipt(receipt):
    need(receipt.get('scope') == 'CAPTURE_LIFECYCLE_ONLY' and receipt.get('stopped') is True
         and receipt.get('state') == 'FROZEN' and receipt.get('reason') == 'REQUESTED', '未确认正常停止')
    need(receipt.get('bridge') == BRIDGE and receipt.get('target_uuid') == receipt['owner']['target_uuid'], '回执目标不符')
    need(receipt['filter'] == capture_filter(receipt['packet_contract']), '过滤条件扩大或变化')
    validate_process(receipt['process'], str(Path(receipt['host_path']).parent), receipt['filter'])
    path = Path(receipt['path'])
    private_dir(path.parent)
    before = local_identity(path)
    need(before == receipt['local_identity'] and before['size'] == receipt['size'], '原件被交换或长度变化')
    need(digest(path.read_bytes()) == receipt['sha256'] and local_identity(path) == before, '原件摘要变化')
    need(receipt['counts']['dropped by kernel'] == 0 and receipt['counts']['captured'] > 0, '抓包丢包或为空')
    need(type(receipt['capture_start_ns']) is int and type(receipt['capture_end_ns']) is int
         and 0 <= receipt['capture_start_ns'] < receipt['capture_end_ns'], '抓包时间窗无效')
    need(24 <= receipt['size'] <= receipt['max_bytes'] <= 64 * 1024**2
         and receipt['capture_end_ns'] - receipt['capture_start_ns'] <= (receipt['max_seconds'] + 15) * 10**9, '抓包容量或时间超出冻结范围')
    return path


def _parser_contract(receipt, frozen_capture_contract, target_contract, anchor):
    # 三项摘要须由主层从独立的冻结记录读取，不能用待验回执临时计算。
    need(type(anchor) is dict and set(anchor) == {'contract_sha256', 'target_sha256', 'receipt_sha256'}, '缺少独立冻结锚点')
    need(all(isinstance(value, str) and re.fullmatch('[0-9a-f]{64}', value) for value in anchor.values()), '冻结锚点摘要无效')
    need(canonical(frozen_capture_contract) == anchor['contract_sha256'], '原冻结全合同摘要不符')
    need(canonical(target_contract) == anchor['target_sha256'], '原冻结目标摘要不符')
    need(canonical(receipt) == anchor['receipt_sha256'], '回执与独立冻结原件不符')
    row = validate(receipt['owner'], target_contract, frozen_capture_contract)
    need(receipt['contract_sha256'] == anchor['contract_sha256'] and receipt['target_sha256'] == anchor['target_sha256'], '回执合同或目标摘要不符')
    need(receipt['target_uuid'] == target_contract['uuid'] and receipt['bridge'] == frozen_capture_contract['bridge'], '回执目标或新桥不符')
    need(receipt['packet_contract'] == row['packet_contract'] and receipt['boot_chain'] == row['boot_chain'], '解析必需项或启动链与原合同不符')
    need(receipt.get('packet_mode', 'TASK') == row.get('packet_mode', 'TASK'), '回执模式与独立冻结合同不符')
    need(all(receipt[key] == frozen_capture_contract[key] for key in ('tcp_reset_policy', 'max_bytes', 'max_seconds')), '复位策略或预算与原合同不符')
    need(isinstance(receipt['session_id'], str) and re.fullmatch('[0-9a-f]{32}', receipt['session_id']), '原采集会话标识无效')
    expected_host_path = str(Path(target_contract['disk_path']).parent) + '/private-captures/' + receipt['session_id'] + '/capture.pcap'
    need(receipt['host_path'] == expected_host_path, '原件不属于已冻结目标的本轮会话')
    verify_receipt(receipt)
    import cloud_packet_capture as parser
    need(digest(Path(parser.__file__).read_bytes()) == PARSER_SHA, '纯解析器版本变化，须重新审查')
    return dict(copy.deepcopy(row['packet_contract']), pcap_sha256=receipt['sha256'], pcap_size=receipt['size'],
                capture_start_ns=receipt['capture_start_ns'], capture_end_ns=receipt['capture_end_ns'])


def analyze_session(receipt, frozen_capture_contract, target_contract, *, anchor):
    """锚点须来自主层独立冻结原件；本函数不从待验回执生成信任。"""
    from cloud_packet_capture import analyze_capture, analyze_no_task_capture, parse_pcap
    receipt, frozen_capture_contract, target_contract, anchor = copy.deepcopy((receipt, frozen_capture_contract, target_contract, anchor))
    frozen = _parser_contract(receipt, frozen_capture_contract, target_contract, anchor)
    # 分派只读已独立锚定并验证的原合同，不根据回执或 task_id 猜模式。
    mode = frozen_capture_contract['targets'][target_contract['uuid']].get('packet_mode', 'TASK')
    analyzer = analyze_no_task_capture if mode == 'NO_TASK' else analyze_capture
    result = analyzer(receipt['path'], frozen)
    need(result['packet_count'] == receipt['counts']['captured'], '解析包数与 tcpdump 结束统计不符')
    need(all(40000 <= row['server_port'] <= 40010 for row in result['tftp']), '实际 TFTP 端口不在冻结范围')
    chain = receipt['boot_chain']
    need(result['dhcp']['boot_file'] == chain['boot_file'] and result['dhcp']['client_user_class_sha256'] == chain['user_class_sha256'], '真实 DHCP 与冻结启动分支不同')
    if receipt['tcp_reset_policy'] == 'no-rst':
        packets, _ = parse_pcap(Path(receipt['path']).read_bytes(), frozen)
        need(not any(p['protocol'] == 6 and p['flags'] & 4 for p in packets), '实际流含 RST，尚未证明客户接收策略适用')
    verify_receipt(receipt)
    return result


def extract_verified_callback(receipt, frozen_capture_contract, target_contract, reference, *, anchor):
    from cloud_packet_capture import extract_verified_request
    receipt, frozen_capture_contract, target_contract, reference, anchor = copy.deepcopy((receipt, frozen_capture_contract, target_contract, reference, anchor))
    frozen = _parser_contract(receipt, frozen_capture_contract, target_contract, anchor)
    need(frozen_capture_contract['targets'][target_contract['uuid']].get('packet_mode', 'TASK') == 'TASK', '无任务抓包禁止提取或回放回调')
    analyze_session(receipt, frozen_capture_contract, target_contract, anchor=anchor)
    value = extract_verified_request(receipt['path'], frozen, reference)
    need(value['method'] == 'POST' and value['response_status'] == 200, '只能提取原件中已成功的本任务回调')
    verify_receipt(receipt)
    return value
