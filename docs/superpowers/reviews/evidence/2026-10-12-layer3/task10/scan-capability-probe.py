from pathlib import Path
import json, subprocess, sys

repo=Path.cwd()
phase=sys.argv[1]
source=(repo/'internal/core/scan/scan_test.go').read_text()
if phase=='baseline':
    source=subprocess.check_output(['git','show','a4cc3b0d0e6982b330cf9d082119f5a14ff701de:internal/core/scan/scan_test.go'],text=True)
mutations={
'parts': '''
            if s.format == "kickstart" {
                src = strings.ReplaceAll(src, "{{lober:syslog}}\\n", "")
                s.parts = []catalog.SystemPart{"firstboot", "cleanup"}
            }
''',
'sys': '''
            if s.format == "kickstart" {
                src += "\\n{{sys:token}}\\n"
                s.sys = append(slices.Clone(s.sys), "token")
            }
''',
'timing': '''
            if s.format == "unattend" {
                src = strings.ReplaceAll(src, "{{slot:装前@pre}}", "{{slot:装前@post-target}}")
                s.slots = slices.Clone(s.slots)
                s.slots[1].Timing = "post-target"
            }
'''}
evidence=repo/'docs/superpowers/reviews/evidence/2026-10-12-layer3/task10'
evidence.mkdir(exist_ok=True)
for key,mutation in mutations.items():
    altered=source.replace('src := string(raw)','src := string(raw)'+mutation,1)
    overlay_file=repo/f'.tmp/task10-{phase}-{key}.go'
    overlay_file.write_text(altered)
    overlay_json=repo/f'.tmp/task10-{phase}-{key}.json'
    overlay_json.write_text(json.dumps({'Replace': {str(repo/'internal/core/scan/scan_test.go'):str(overlay_file)}}))
    command=['go','test','-overlay',str(overlay_json),'./internal/core/scan','-run','^Test七种格式的样例扫出预期的占位符$','-count=1','-v']
    output=subprocess.run(command,capture_output=True,text=True)
    (evidence/f'{phase}-{key}.log').write_text('Command: '+' '.join(command)+'\n'+output.stdout+output.stderr+f'\nexit_code={output.returncode}\n')
    print(f'{phase}/{key}: exit_code={output.returncode}')
    print(output.stdout[-1800:])
    if phase=='baseline': assert output.returncode==0
    else:
        assert output.returncode!=0
        target={'parts':'RequiredParts','sys':'SystemValues','timing':'Timings'}[key]
        assert target in output.stdout, output.stdout
print('Controlled real scanner fixture mutations verified; no tracked fixture bytes changed.')
