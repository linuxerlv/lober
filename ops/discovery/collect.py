"""在采集镜像里只读查询本机硬件。"""

import json
import os
import re
import subprocess
import time
import unicodedata
from pathlib import Path


MAX_REPORT = 512 * 1024
MAX_ITEMS = 256
MAX_TEXT = 240


def text(value):
    if value is None:
        return ""
    compact = " ".join(str(value).split())
    plain = "".join(char for char in compact if unicodedata.category(char) != "Cc")
    return plain.encode("utf-8", errors="replace")[:MAX_TEXT].decode("utf-8", errors="ignore")


def dmi_value(value):
    value = text(value)
    if value.casefold() in {
        "unknown", "not specified", "not available", "not applicable", "default string",
        "to be filled by o.e.m.", "none", "n/a", "<out of spec>", "system serial number",
        "system product name", "system manufacturer", "no asset tag",
    }:
        return ""
    return value


def command(*arguments, timeout=4, output_on_failure=False):
    try:
        done = subprocess.run(arguments, check=not output_on_failure, capture_output=True,
                              encoding="utf-8", errors="replace", timeout=timeout)
        return done.stdout
    except (OSError, subprocess.CalledProcessError, subprocess.TimeoutExpired):
        return ""


def object_output(value):
    try:
        parsed = json.loads(value)
        return parsed if isinstance(parsed, dict) else {}
    except ValueError:
        return {}


def properties(value):
    return {key.strip(): text(data) for line in value.splitlines()
            if ":" in line for key, data in [line.split(":", 1)]}


def dmi_records(value):
    records = {}
    current = None
    for line in value.splitlines():
        header = re.match(r"Handle 0x[0-9a-f]+, DMI type (\d+),", line, re.I)
        if header:
            current = {}
            records.setdefault(int(header[1]), []).append(current)
        if current is not None and re.match(r"^\t\S", line) and ":" in line:
            key, data = line.strip().split(":", 1)
            value = dmi_value(data)
            if value:
                current[key] = value
    return records


def select_fields(values, names):
    return {label: values[key] for key, label in names.items() if values.get(key)}


def item(name, source, fields):
    return {"name": text(name) or "未命名部件", "fields": {"来源": source, **{
        key: text(value) for key, value in fields.items() if text(value)
    }}}


def section(key, title, items, reason=""):
    if len(items) > MAX_ITEMS:
        items = items[:MAX_ITEMS]
        reason = "部件数量超过报告上限，清单已截断"
    status = "read"
    if reason:
        status = "partial" if items else "unread"
    result = {"key": key, "title": title, "status": status, "items": items}
    if reason:
        result["reason"] = reason
    return result


class Collector:
    def __init__(self, root, boot_mac, run=command):
        self.root = root
        self.boot_mac = boot_mac.lower().replace("-", ":")
        self.run = run
        self.dmi = dmi_records(run("dmidecode", "--type", "0,1,2,3,4,7,17,39"))
        self.pci = sorted((root / "sys/bus/pci/devices").glob("*"))
        self.pci_descriptions = {}
        for line in run("lspci", "-Dnn").splitlines():
            address, _, description = line.partition(" ")
            self.pci_descriptions[address] = text(description)

    def read(self, path):
        try:
            return text((self.root / path).read_text(errors="replace"))
        except OSError:
            return ""

    def lines(self, path):
        try:
            return (self.root / path).read_text(errors="replace").splitlines()
        except OSError:
            return []

    def collect(self):
        return {"sections": [
            self.system(), self.cpu(), self.memory(), self.storage(), self.network(),
            self.expansion(), self.power(), self.cooling(),
        ]}

    def system(self):
        labels = {
            "sys_vendor": "厂商", "product_name": "型号", "product_serial": "整机序列号",
            "product_uuid": "整机标识", "product_sku": "整机料号",
            "chassis_asset_tag": "资产标签", "chassis_vendor": "机箱厂商",
            "chassis_serial": "机箱序列号", "chassis_version": "机箱版本",
            "board_vendor": "主板厂商", "board_name": "主板型号",
            "board_serial": "主板序列号", "board_version": "主板版本",
            "bios_vendor": "固件厂商", "bios_version": "固件版本", "bios_date": "固件日期",
        }
        fields = {label: dmi_value(self.read(f"sys/class/dmi/id/{key}")) for key, label in labels.items()}
        for record_type, mapping in {
            0: {"Vendor": "固件厂商", "Version": "固件版本", "Release Date": "固件日期"},
            1: {"Manufacturer": "厂商", "Product Name": "型号", "Serial Number": "整机序列号",
                "UUID": "整机标识", "SKU Number": "整机料号"},
            2: {"Manufacturer": "主板厂商", "Product Name": "主板型号", "Serial Number": "主板序列号",
                "Version": "主板版本"},
            3: {"Manufacturer": "机箱厂商", "Serial Number": "机箱序列号", "Version": "机箱版本",
                "Asset Tag": "资产标签", "SKU Number": "机箱料号"},
        }.items():
            for record in self.dmi.get(record_type, [])[:1]:
                for label, value in select_fields(record, mapping).items():
                    if not fields.get(label):
                        fields[label] = value
        if not any(fields.values()):
            fields["型号"] = self.read("proc/device-tree/model").strip("\x00")
            fields["整机序列号"] = self.read("proc/device-tree/serial-number").strip("\x00")
        fields["启动方式"] = "UEFI" if (self.root / "sys/firmware/efi").exists() else "非 UEFI"
        required = ("厂商", "型号", "整机序列号", "整机标识", "资产标签", "机箱料号",
                    "主板厂商", "主板型号", "主板序列号", "主板版本", "固件版本")
        missing = [label for label in required if not fields.get(label)]
        return section("system", "整机、主板和固件", [item("整机", "Linux 固件信息", fields)],
                       "固件未提供：" + "、".join(missing) if missing else "")

    def cpu(self):
        summary = properties(self.run("lscpu"))
        caches = {record.get("Socket Designation", ""): record.get("Installed Size", "")
                  for record in self.dmi.get(7, [])}
        items = []
        for record in self.dmi.get(4, []):
            fields = select_fields(record, {
                "Manufacturer": "厂商", "Version": "型号", "Core Count": "物理核数",
                "Thread Count": "线程数", "Current Speed": "当前频率", "Max Speed": "最大频率",
                "Status": "状态",
            })
            slot = record.get("Socket Designation", "处理器插槽")
            fields["架构"] = summary.get("Architecture")
            for level in ("L1d cache", "L1i cache", "L2 cache", "L3 cache"):
                value = summary.get(level)
                if value:
                    fields[level] = value
            items.append(item(slot, "机器固件和 Linux", fields))
        if caches:
            items += [item(label or "缓存", "机器固件", {"容量": size})
                      for label, size in caches.items() if size]
        if not items:
            fields = select_fields(summary, {
                "Architecture": "架构", "Model name": "型号", "CPU(s)": "线程数",
                "Socket(s)": "插槽数", "Core(s) per socket": "每个插槽物理核数",
                "CPU max MHz": "最大频率（MHz）", "L3 cache": "三级缓存",
            })
            if fields:
                items.append(item("处理器概况", "Linux", fields))
            return section("cpu", "处理器", items, "未读取逐插槽的处理器信息")
        populated = [record for record in self.dmi.get(4, [])
                     if "unpopulated" not in record.get("Status", "").lower()]
        missing = []
        for key, label in {"Socket Designation": "插槽", "Version": "型号", "Core Count": "物理核数",
                           "Thread Count": "线程数", "Current Speed": "当前频率"}.items():
            if any(not record.get(key) for record in populated):
                missing.append(label)
        if not summary.get("Architecture"):
            missing.append("架构")
        if not caches and not any(summary.get(f"L{n} cache") for n in (2, 3)):
            missing.append("缓存")
        return section("cpu", "处理器", items,
                       "部分处理器字段未读取：" + "、".join(missing) if missing else "")

    def memory(self):
        items = []
        missing = False
        records = self.dmi.get(17, [])
        for record in records:
            label = record.get("Locator", "内存插槽")
            if record.get("Size") == "No Module Installed":
                items.append(item(label, "机器固件", {"状态": "未安装"}))
                continue
            fields = select_fields(record, {
                "Size": "容量", "Type": "类型", "Speed": "额定速率",
                "Configured Memory Speed": "当前速率", "Manufacturer": "厂商",
                "Part Number": "料号", "Serial Number": "序列号", "Bank Locator": "通道",
            })
            missing |= any(not fields.get(key) for key in
                           ("容量", "类型", "额定速率", "当前速率", "厂商", "料号", "序列号"))
            items.append(item(label, "机器固件", fields))
        meminfo = properties("\n".join(self.lines("proc/meminfo")))
        if meminfo.get("MemTotal"):
            items.append(item("内存总量", "Linux", {"可用总量": meminfo["MemTotal"]}))
        reason = ""
        if not records:
            reason = "机器固件未提供内存插槽清单"
        elif missing:
            reason = "机器固件未提供部分内存条字段"
        if records and all(record.get("Size") == "No Module Installed" for record in records):
            reason = "固件没有报告任何已安装的内存条"
        return section("memory", "内存", items, reason)

    def pci_fields(self, dev):
        address = dev.name
        driver = dev / "driver"
        return {
            "插槽地址": address, "设备": self.pci_descriptions.get(address),
            "厂商编号": self.read(dev / "vendor"), "设备编号": self.read(dev / "device"),
            "物理插槽": self.read(dev / "physical_slot"),
            "固件": self.read(dev / "fw_version") or self.read(dev / "firmware_version"),
            "驱动": driver.resolve().name if driver.is_symlink() else "",
        }

    def storage(self):
        items, reason = [], []
        adapters = [dev for dev in self.pci if self.read(dev / "class").startswith("0x01")]
        raid = [dev for dev in adapters if self.read(dev / "class").startswith("0x0104")]
        for dev in adapters:
            label = "阵列控制器" if dev in raid else "存储控制器"
            fields = self.pci_fields(dev)
            if dev not in raid and not fields.get("固件"):
                reason.append("部分存储控制器固件未读取")
            items.append(item(f"{label} {dev.name}", "Linux", fields))
        disks = object_output(self.run("lsblk", "--json", "--bytes", "--nodeps", "--output",
                                       "NAME,TYPE,SIZE,MODEL,SERIAL,TRAN,ROTA,REV,HCTL"))
        names = set()
        for disk in (disks.get("blockdevices") or [])[:MAX_ITEMS]:
            name = disk.get("name", "")
            if disk.get("type") != "disk" or not re.fullmatch(r"[a-zA-Z0-9_-]+", name):
                continue
            names.add(name)
            fields = select_fields(disk, {
                "size": "容量（字节）", "model": "型号", "serial": "序列号", "tran": "接口",
                "rev": "固件", "hctl": "SCSI 路径",
            })
            fields["类型"] = "系统可见磁盘" if raid else "磁盘"
            if disk.get("rota") is not None:
                fields["介质"] = "机械盘" if disk["rota"] else "非旋转介质"
            # smartctl 在磁盘健康异常时会返回非 0，但仍会返回完整报告。
            smart = object_output(self.run("smartctl", "-j", "-i", "-H", f"/dev/{name}",
                                           output_on_failure=True))
            for smart_key, report_key in {"model_name": "型号", "serial_number": "序列号",
                                          "firmware_version": "固件", "protocol": "接口"}.items():
                if not fields.get(report_key) and smart.get(smart_key):
                    fields[report_key] = smart[smart_key]
            status = (smart.get("smart_status") or {}).get("passed")
            if isinstance(status, bool):
                fields["自检健康状态"] = "正常" if status else "异常"
            elif not raid:
                reason.append("部分磁盘健康状态未读取")
            if not raid:
                missing = [key for key in ("型号", "序列号", "容量（字节）", "接口", "固件")
                           if not fields.get(key)]
                if missing:
                    reason.append("部分磁盘字段未读取：" + "、".join(missing))
            position = self.disk_bay(name)
            if position:
                fields["盘位"] = position
            elif not raid and not name.startswith(("vd", "xvd")):
                reason.append("部分磁盘盘位未读取")
            items.append(item(name, "Linux 和磁盘自检", fields))
        if raid:
            matched, complete = self.storcli()
            items.extend(matched)
            if not complete or len(raid) > 1:
                reason.append("阵列控制器已识别，部分物理盘、盘位或阵列关系未读取")
        if not names:
            reason.append("Linux 未读到磁盘，可能没有磁盘或缺少存储驱动")
        return section("storage", "磁盘和阵列", items, "；".join(dict.fromkeys(reason)))

    def disk_bay(self, name):
        enclosure = self.root / "sys/class/enclosure"
        for entry in sorted(enclosure.glob("*/*")):
            if (entry / "device/block" / name).exists():
                return self.read(entry / "slot") or entry.name
        return ""

    def storcli(self):
        value = object_output(self.run("storcli", "/call", "show", "all", "J", timeout=8))
        items = []
        controller_count = 0
        for result in value.get("Controllers") or []:
            status = result.get("Command Status") or {}
            if str(status.get("Status", "")).lower() != "success":
                continue
            controller_count += 1
            controller = status.get("Controller", "?")
            data = result.get("Response Data") or {}
            basics = data.get("Basics") or {}
            version = data.get("Version") or {}
            items.append(item(f"阵列控制器 {controller}", "本机 StorCLI", {
                "型号": basics.get("Model"), "序列号": basics.get("Serial Number"),
                "固件": version.get("FW Package Build"),
            }))
            for row in data.get("VD LIST") or []:
                items.append(item(f"逻辑盘 {controller}/{row.get('DG/VD', '?')}", "本机 StorCLI",
                                  select_fields(row, {"DG/VD": "阵列组/逻辑盘", "TYPE": "阵列级别",
                                                      "State": "状态", "Size": "容量", "Name": "名称"})))
        detail = object_output(self.run("storcli", "/call", "/eall", "/sall", "show", "all", "J",
                                        timeout=8)) if controller_count else {}
        disk_count = 0
        complete = True
        for result in detail.get("Controllers") or []:
            status = result.get("Command Status") or {}
            if str(status.get("Status", "")).lower() != "success":
                complete = False
                continue
            for key, rows in (result.get("Response Data") or {}).items():
                if not key.startswith("Drive ") or not isinstance(rows, list):
                    continue
                data = result["Response Data"].get(key + " - Detailed Information") or {}
                attributes = next((entry for name, entry in data.items() if name.endswith("Device attributes")), {})
                for row in rows:
                    disk_count += 1
                    fields = select_fields(row, {
                        "EID:Slt": "机箱:盘位", "DG": "阵列组", "Model": "型号", "Intf": "接口",
                        "Med": "介质", "Size": "容量", "State": "状态",
                    })
                    fields.update(select_fields(attributes, {"SN": "序列号", "Firmware Revision": "固件"}))
                    complete &= all(fields.get(field) for field in
                                    ("机箱:盘位", "型号", "序列号", "容量", "接口", "固件", "状态"))
                    items.append(item(f"物理盘 {status.get('Controller', '?')}/{row.get('EID:Slt', '?')}",
                                      "本机 StorCLI", fields))
        return items, complete and disk_count > 0

    def network(self):
        items, seen, missing = [], set(), False
        for port in sorted((self.root / "sys/class/net").glob("*")):
            device = port / "device"
            if not device.exists():
                continue
            device = device.resolve()
            seen.add(device)
            fields = self.pci_fields(device) if self.read(device / "class") else {}
            fields.update(select_fields(properties(self.run("ethtool", "-i", port.name)),
                                        {"driver": "驱动", "firmware-version": "固件"}))
            mac = self.read(port / "address")
            carrier = self.read(port / "carrier")
            speed = self.read(port / "speed")
            fields.update({"硬件地址": mac, "连接": {"0": "未接通", "1": "接通"}.get(carrier),
                           "当前速率": f"{speed} Mb/s" if speed.isdigit() else "",
                           "启动网口": "是" if mac and mac.lower() == self.boot_mac else ""})
            missing |= not all((mac, fields.get("固件"), fields.get("设备"), fields.get("驱动"),
                                carrier, speed.isdigit() or carrier == "0"))
            items.append(item(port.name, "Linux 和网卡驱动", fields))
        for dev in self.pci:
            if self.read(dev / "class").startswith("0x02") and dev.resolve() not in seen:
                fields = self.pci_fields(dev)
                fields["状态"] = "网口未读取"
                items.append(item(f"未启用网卡 {dev.name}", "Linux PCI 设备列表", fields))
                missing = True
        reason = ""
        if not items:
            reason = "未读到物理网卡"
        elif missing or not any(port["fields"].get("启动网口") for port in items):
            reason = "部分网口、固件、连接状态或启动网口未读取"
        return section("network", "网卡和网口", items, reason)

    def expansion(self):
        items = []
        for dev in self.pci:
            device_class = self.read(dev / "class")
            if not device_class.startswith(("0x01", "0x02", "0x06")):
                fields = self.pci_fields(dev)
                fields["类别编号"] = device_class
                items.append(item(dev.name, "Linux PCI 设备列表", fields))
        reason = ""
        if not items:
            reason = "未读取扩展卡或加速卡"
        elif any(not all(part["fields"].get(key) for key in ("设备", "驱动", "固件", "物理插槽"))
                 for part in items):
            reason = "部分扩展设备型号、驱动、固件或物理插槽未读取"
        return section("expansion", "扩展卡和加速卡", items, reason)

    def ipmi(self, name):
        output = self.run("ipmitool", "-I", "open", "sdr", "type", name, timeout=6)
        return [item(parts[0], "本机管理芯片", {"传感器记录": " | ".join(parts[1:])})
                for line in output.splitlines() if "|" in line
                for parts in [[text(value) for value in line.split("|")]]]

    def power(self):
        items = []
        records = self.dmi.get(39, [])
        for record in records:
            fields = select_fields(record, {
                "Location": "位置", "Manufacturer": "厂商", "Name": "名称",
                "Model Part Number": "料号", "Serial Number": "序列号",
                "Max Power Capacity": "额定功率", "Status": "状态",
                "Power Unit Group": "电源组", "Revision": "版本",
            })
            items.append(item(record.get("Location", "电源"), "机器固件", fields))
        sensors = self.ipmi("Power Supply")
        redundancy = self.ipmi("Power Unit")
        items.extend(sensors)
        items.extend(redundancy)
        reason = ""
        if not items:
            reason = "机器固件和本机管理接口未提供电源数据"
        elif (not records or not sensors or not redundancy or
              any(not all(record.get(key) for key in ("Location", "Model Part Number",
                                                      "Max Power Capacity", "Status")) for record in records)):
            reason = "电源清单、实时状态或冗余情况未完整读取"
        return section("power", "电源", items, reason)

    def cooling(self):
        items, kinds = [], set()
        for chip in sorted((self.root / "sys/class/hwmon").glob("*")):
            name = self.read(chip / "name") or chip.name
            for sensor in sorted(chip.glob("*_input")):
                kind = sensor.name.split("_", 1)[0]
                label = self.read(chip / f"{kind}_label") or f"{name} {kind}"
                value = self.read(sensor)
                if kind.startswith("fan") and value.isdigit():
                    items.append(item(label, "Linux 传感器", {"风扇转速": f"{value} 转/分"}))
                    kinds.add("Fan")
                elif kind.startswith("temp") and re.fullmatch(r"-?\d+", value):
                    items.append(item(label, "Linux 传感器", {"温度": f"{int(value) / 1000:g} °C"}))
                    kinds.add("Temperature")
        for kind in ("Fan", "Temperature"):
            sensors = self.ipmi(kind)
            if sensors:
                kinds.add(kind)
                items.extend(sensors)
        reason = "风扇或温度传感器未完整读取" if len(kinds) < 2 else ""
        return section("cooling", "风扇和温度", items, reason)


def encode(report):
    def to_json():
        return json.dumps(report, ensure_ascii=False, separators=(",", ":")).encode()

    payload = to_json()
    while len(payload) > MAX_REPORT:
        entries = max(report["sections"], key=lambda entry: len(entry["items"]))
        entries["items"] = entries["items"][:max(1, len(entries["items"]) // 2)]
        entries.update(status="partial", reason="完整报告超过大小上限，此类部件清单已截断")
        payload = to_json()
    return payload


def uploaded(url, token, report, work):
    body = work / "report.json"
    header = work / "headers"
    body.write_bytes(report)
    fd = os.open(header, os.O_CREAT | os.O_WRONLY | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as stream:
        stream.write(f"Authorization: Bearer {token}\n")
    try:
        for attempt in range(3):
            if attempt:
                time.sleep(2 ** attempt)
            try:
                done = subprocess.run(
                    ["curl", "--proto", "=http,https", "--silent", "--connect-timeout", "4",
                     "--max-time", "20", "--output", "/dev/null", "--write-out", "%{http_code}",
                     "--header", f"@{header}", "--header", "Content-Type: application/json",
                     "--data-binary", f"@{body}", "--url", url],
                    capture_output=True, encoding="utf-8", timeout=22, check=False,
                )
            except (OSError, subprocess.TimeoutExpired):
                continue
            status = done.stdout.strip()
            if done.returncode == 0 and status.startswith("2"):
                return True
            print(f"Lober: upload attempt {attempt + 1} failed ({status if status.isdigit() else 'network'})",
                  flush=True)
            if done.returncode == 0 and status.startswith("4") and status not in {"408", "429"}:
                break
    finally:
        header.unlink()
    return False


def main():
    args = dict(part.split("=", 1) for part in Path("/proc/cmdline").read_text().split() if "=" in part)
    url, token = (args.get(f"lober.discovery_{name}", "") for name in ("url", "token"))
    if not url.startswith(("http://", "https://")) or not re.fullmatch(r"[A-Za-z0-9_-]+", token):
        print("Lober: missing or invalid report destination or upload token", flush=True)
        return 1
    deadline = time.monotonic() + 110

    def local_command(*arguments, timeout=4, **options):
        remaining = deadline - time.monotonic()
        return command(*arguments, timeout=min(remaining, timeout), **options) if remaining > 0 else ""

    print("Lober: reading hardware inventory", flush=True)
    report = Collector(Path("/"), args.get("lober.discovery_mac", ""), local_command).collect()
    for entry in report["sections"]:
        print(f"  {entry['key']}: {entry['status']} ({len(entry['items'])} items)", flush=True)
    return 0 if uploaded(url, token, encode(report), Path("/run")) else 1


if __name__ == "__main__":
    raise SystemExit(main())
