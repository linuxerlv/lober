"""在 lobervm 的私有进程和数据库中运行正式容量负载。"""
import hashlib
import json
import os
from pathlib import Path
import pwd
import secrets
import shutil
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request


TEST_BASE = Path('/var/lib/lober-ci-test')
PG_BIN = Path('/usr/lib/postgresql/16/bin')
PG_PORT = 55433
DB_NAME = 'lober_capacity'
URL = 'http://127.0.0.1:18093'
FILES_URL = 'http://127.0.0.1:18094'
ESXI7_SHA = '6a5d1ccb57335d349e0e38be170748fd2a973de498ff9442c6254a31e7f0491c'


def read_json(path):
    return json.loads(path.read_text())


def put_json(path, data):
    with path.open('x', encoding='utf-8') as stream:
        json.dump(data, stream, ensure_ascii=False, indent=2)
        stream.write('\n')


def sha(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def api(path, token=None, body=None):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    request = urllib.request.Request(URL + '/console/v1' + path, headers=headers,
                                     data=json.dumps(body).encode() if body is not None else None)
    try:
        with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request, timeout=20) as response:
            data = json.load(response)
    except urllib.error.HTTPError as error:
        raise RuntimeError(f'容量准备接口 {path} 返回 HTTP {error.code}') from None
    if data['code'] != 0:
        raise RuntimeError(f'容量准备接口 {path} 返回失败')
    return data['data']


def login(credential):
    return api('/auth/login', body=credential)['token']


def import_iso(token, file, family, series, name):
    expected = sha(file)
    size = file.stat().st_size
    item = api('/isos', token, {'name': name, 'family': family, 'series': series,
                               'sources': [{'kind': 'path', 'addr': str(file), 'sha256': expected, 'size': size}]})
    deadline = time.monotonic() + 900
    while time.monotonic() < deadline:
        item = api('/isos/' + item['id'], token)
        if item['state'] == 'ready':
            if item['sha256'] != expected or not item['artifacts']:
                raise RuntimeError('候选导入后的镜像与输入不一致')
            return {'iso_id': item['id'], 'sha256': expected, 'size': size, 'local_file': str(file)}
        if item['state'] == 'failed':
            break
        time.sleep(1)
    raise RuntimeError(f'候选无法导入 {name}')


def bootstrap(initial_password, seed_iso, download_iso):
    initial = login({'username': 'admin', 'password': initial_password})
    admin = {'username': 'admin', 'password': secrets.token_urlsafe(32)}
    api('/auth/password', initial, {'currentPassword': initial_password, 'newPassword': admin['password']})
    token = login(admin)
    credentials = []
    for index, role in enumerate(['hook-author', 'hook-approver', 'admin'] + ['operator'] * 39):
        user = {'username': f'capacity_{index:02d}', 'password': secrets.token_urlsafe(32)}
        api('/users', token, {**user, 'displayName': user['username'], 'role': role})
        credentials.append(user)
    author, approver = (login(user) for user in credentials[:2])
    uploader = login(credentials[2])
    if 'iso:write' not in api('/user/info', uploader)['permissions']:
        raise RuntimeError('容量首个会话没有镜像导入权限')
    api('/auth/logout', uploader, {})
    template = api('/templates', author, {'name': 'capacity-seed', 'family': 'linux', 'series': ['rocky'],
        'body': '# {{param:hostname}}\n{{lober:syslog}}\n{{lober:firstboot}}\n{{lober:cleanup}}\n',
        'params': [{'name': 'hostname', 'label': 'hostname', 'type': 'hostname', 'default': 'node'}]})
    version = '/templates/' + template['id'] + '/versions/1'
    submitted = api(f"{version}/submit?rev={template['rev']}", author, {})
    approved = api(f"{version}/approve?rev={submitted['rev']}", approver, {})
    if approved['state'] != 'approved':
        raise RuntimeError('容量种子模板审批失败')
    seed = import_iso(token, seed_iso, 'linux', 'rocky', 'capacity-seed-rocky')
    download = import_iso(token, download_iso, 'esxi', 'esxi', 'capacity-verified-esxi7')
    return ({'seed': admin, 'sessions': credentials[2:]}, seed, download,
            {'isoID': seed['iso_id'], 'templateID': template['id'], 'templateVersion': 1})


def wait_service(service):
    for _ in range(100):
        if service.poll() is not None:
            raise RuntimeError('容量候选服务启动失败')
        try:
            with socket.create_connection(('127.0.0.1', 18093), timeout=1):
                return
        except OSError:
            time.sleep(.2)
    raise RuntimeError('容量候选服务启动超时')


def run(root, pg_socket):
    base = root.parent
    if (root.resolve(strict=True) != root or base.name != 'qualification' or base.parent.parent != TEST_BASE
            or not root.name.startswith('kvm-capacity.')):
        raise ValueError('容量运行目录无效')
    if root.stat().st_uid != os.getuid() or root.stat().st_mode & 0o077:
        raise ValueError('容量运行目录必须归当前账号私有')
    if (pg_socket.resolve(strict=True) != pg_socket or pg_socket.parent != Path('/run')
            or not pg_socket.name.startswith('lober-ci-capacity.') or pg_socket.stat().st_uid != os.getuid()
            or pg_socket.stat().st_mode & 0o077):
        raise ValueError('容量数据库套接字目录无效')
    os.chdir(root)
    interfaces = subprocess.check_output(['/usr/sbin/ip', '-o', 'link', 'show'], text=True).splitlines()
    if len(interfaces) != 1 or interfaces[0].split(':', 2)[1].strip() != 'lo':
        raise ValueError('容量运行必须处于隔离网络')
    sys.path.insert(0, str(root/'tools'))
    import production_capacity as capacity
    from kvm_install_evidence import collect_report, SCHEMA
    import cloud_packet_capture as packet

    candidate = read_json(root/'original-candidate.json')
    identity = {key: candidate[key] for key in ('run_id', 'product_sha', 'test_sha', 'binary_sha256')}
    case_root = root/'installation'
    checker, parser = (case_root / name for name in ('kvm_install_evidence.py', 'cloud_packet_capture.py'))
    common = root/'run-identity.json'
    put_json(common, {**identity, 'installation_schema': SCHEMA,
                      'installation_checkers': {str(checker): sha(checker)},
                      'installation_helpers': {str(parser): sha(parser)}})
    report_path = case_root/'report.json'
    report = collect_report({**identity, 'run_identity_sha256': sha(common)}, case_root, packet)
    put_json(report_path, report)
    print('四次装机和抓包已复核，通过后开始准备容量负载', flush=True)

    runtime = root/'runtime'
    media = runtime/'media'
    esxi = media/'esxi7.iso'
    media_evidence = read_json(root/'real-media.json')
    if (media_evidence['status'] != 'PASS' or any(media_evidence[key] != identity[key] for key in identity)
            or media_evidence['media_sha256']['LOBER_TEST_ESXI7_ISO'] != ESXI7_SHA or sha(esxi) != ESXI7_SHA):
        raise ValueError('真实下载镜像和候选证据不符')
    receipt = root/'download-media.json'
    put_json(receipt, {'status': 'PASS', 'synthetic': False, 'sha256': ESXI7_SHA, 'size': esxi.stat().st_size,
                      'original_evidence_sha256': sha(root/'real-media.json')})
    profile = root/'profile.json'
    put_json(profile, capacity.FORMAL_PROFILE)
    schema = capacity.schema_for(identity['run_id'])
    pg_data = runtime/'pgdata'
    db_user = pwd.getpwuid(os.getuid()).pw_name
    env = {k: v for k, v in os.environ.items() if not k.startswith('PG') and k != 'LOBER_PG_DSN'}
    subprocess.run([PG_BIN/'initdb', '-D', pg_data, '--auth-local=trust', '--auth-host=reject',
                    '--no-locale', '--encoding=UTF8'], env=env, check=True)
    pg_options = f"-c listen_addresses='' -k {pg_socket} -p {PG_PORT} -c shared_buffers=96MB -c max_connections=48"
    pg_command = [PG_BIN/'pg_ctl', '-D', pg_data, '-w']
    subprocess.run([*pg_command, '-l', root/'postgresql.log', '-o', pg_options, 'start'], env=env, check=True)
    processes = []
    try:
        psql = [PG_BIN/'psql', '-h', pg_socket, '-p', str(PG_PORT), '-X', '-v', 'ON_ERROR_STOP=1']
        subprocess.run([*psql, '-d', 'postgres', '-c', f'CREATE DATABASE {DB_NAME}'], env=env, check=True)
        subprocess.run([*psql, '-d', DB_NAME, '-c', f'CREATE SCHEMA {schema}'], env=env, check=True)
        db_owner = root/'database-owner.json'
        put_json(db_owner, {'run_id': identity['run_id'], 'schema': schema})
        auth_file, config_file = runtime/'auth.json', runtime/'config.json'
        seed_password = secrets.token_urlsafe(32)
        write_paths = {key: runtime/key for key in ('staging', 'artifacts', 'smb', 'tools')}
        for path in write_paths.values():
            path.mkdir(mode=0o700)
        config = {'listen': '127.0.0.1:18093', 'pg': {'dsn': f'host={pg_socket} port={PG_PORT} user={db_user} dbname={DB_NAME} search_path={schema}',
                  'maxConns': 20}, 'seedAdmin': seed_password, 'log': {'level': 'error'},
                  'machine': {'enabled': True, 'keyFile': str(runtime/'machine.key')},
                  'preview': {'gateway': URL, 'artifacts': FILES_URL},
                  'iso': {**{key: str(path) for key, path in write_paths.items()},
                          'localRoots': [str(media)], 'workers': 1}}
        put_json(config_file, config)
        binary, archive = Path(candidate['binary']), Path(candidate['source_archive'])
        if (binary.resolve(strict=True) != base.parent/'lober' or archive.resolve(strict=True) != base.parent/'source.tar'
                or sha(binary) != candidate['binary_sha256'] or sha(archive) != candidate['source_archive_sha256']):
            raise ValueError('容量候选程序或源码包校验不符')
        with (root/'product.log').open('xb') as log:
            product = subprocess.Popen([binary, '-config', config_file], env={**env, 'GIN_MODE': 'release'},
                                       stdout=log, stderr=subprocess.STDOUT)
        processes.append(product)
        with (root/'downloads.log').open('xb') as log:
            processes.append(subprocess.Popen([sys.executable, '-I', '-m', 'http.server', '18094', '--bind', '127.0.0.1',
                                                '--directory', media], env=env, stdout=log, stderr=subprocess.STDOUT))
        wait_service(product)
        auth, seed, download, selection = bootstrap(seed_password, media/'seed-rocky.iso', esxi)
        put_json(auth_file, auth)
        dl_url = FILES_URL + '/esxi7.iso'
        db_pid = int((pg_data/'postmaster.pid').read_text().splitlines()[0])
        host = root/'resource-admission.json'
        put_json(host, {'observed_utc_unix': time.time(), 'run_identity_sha256': sha(common),
                        'host_free_bytes': shutil.disk_usage(root).free, 'host_reserve_bytes': 6 << 30})
        imports = read_json(runtime/'imports.json')
        for entries in imports.values():
            for entry in entries:
                entry['path'] = str(runtime/'inputs'/Path(entry['path']).name)
        settings = {'run_id': identity['run_id'], 'installation_schema': SCHEMA,
                    'owner_sha256': sha(base/'.qualification-owner'), 'run_identity_file': str(common),
                    'run_identity_sha256': sha(common), 'concurrent_installation': {'path': str(report_path), 'sha256': sha(report_path)},
                    'profile_sha256': sha(profile), 'executor_sha256': sha(root/'tools/production_capacity.py'),
                    'source_root': str(root/'source'), 'profile_file': str(profile), 'evidence_root': str(root/'logs'),
                    'service_url': URL, 'allowed_private_ips': ['127.0.0.1'],
                    'database': {'schema': schema, 'name': DB_NAME, 'owner_file': str(db_owner)},
                    'artifact': {**download, 'url': dl_url, 'qualification_file': str(receipt), 'qualification_sha256': sha(receipt)},
                    'auth_file': str(auth_file), 'auth_sha256': sha(auth_file), 'config_file': str(config_file),
                    'config_sha256': sha(config_file),
                    'resource_paths': [str(path) for path in (pg_data, pg_data/'pg_wal', root/'logs', media, *write_paths.values())],
                    'resource_evidence': {'path': str(host), 'sha256': sha(host)}, 'imports': imports,
                    'source_bindings': [{**seed, 'purpose': 'seed'}, {**download, 'purpose': 'download', 'url': dl_url}],
                    'dataset': {'host_prefix': capacity.host_prefix(identity['run_id']), 'arch': 'x86_64',
                                'firmware': 'bios', 'selection': selection,
                                'entered': {'tpl/hostname': 'capacity-node'}},
                    'processes': {role: capacity.process_identity(pid) for role, pid in
                                  (('product', product.pid), ('database', db_pid))}}
        manifest = root/'capacity-candidate.json'
        put_json(root/'capacity-config.json', settings)
        put_json(manifest, {**candidate, 'capacity': settings})
        for phase in ('seed', 'normal', 'burst', 'soak', 'evaluate'):
            print(f'容量阶段 {phase} 开始', flush=True)
            subprocess.run([sys.executable, '-B', str(root/'tools/production_capacity.py'), '--phase', phase,
                            '--manifest', manifest, '--profile', profile, '--output', root/'logs', '--work-root', root,
                            '--qualification-root', base],
                           env=env, check=True)
            print(f'容量阶段 {phase} 完成', flush=True)
    finally:
        for process in processes:
            if process.poll() is None:
                process.terminate()
        for process in processes:
            process.wait(timeout=30)
        subprocess.run([*pg_command, '-m', 'fast', 'stop'], env=env, check=True)
        shutil.rmtree(runtime)


if __name__ == '__main__':
    os.umask(0o077)
    run(Path(sys.argv[1]), Path(sys.argv[2]))
