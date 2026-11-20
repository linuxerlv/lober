"""在采集镜像里只读查询本机硬件。"""

import json
import os
import re
import subprocess
import threading
import time
import unicodedata
from collections import Counter
from copy import deepcopy
from pathlib import Path

from hardware import (COUNTER_FIELDS, COUNTER_SOURCE, MEMORY_COUNTERS, MISSING, NETWORK_COUNTERS,
                      accelerator_fields, counter_fields, data_object, event_fields, explain,
                      pcie_counters, port_fields, protection_state, rebuild_progress, secure_boot,
                      smart_fields, unsigned, valid)

MAX_REPORT = 512 * 1024
MAX_ITEMS = 256
MAX_TEXT = 240
MAX_EVENTS = 20
EVENT_ITEM = "管理芯片事件"
EVENT_STATUS = "近期事件"
MEMORY_CONTROLLER = "内存控制器 "


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


class QueryFailure(str):
    def __new__(cls, reason):
        output = super().__new__(cls, "")
        output.reason = reason
        return output


def command(*arguments, timeout=4, output_on_failure=False, include_stderr=False):
    try:
        done = subprocess.run(arguments, check=not output_on_failure, capture_output=True,
                              encoding="utf-8", errors="replace", timeout=timeout)
        return "\n".join(part for part in (done.stdout, done.stderr) if part) if include_stderr else done.stdout
    except FileNotFoundError:
        return QueryFailure(f"采集镜像未安装 {arguments[0]}")
    except PermissionError:
        return QueryFailure(f"没有权限运行 {arguments[0]}")
    except subprocess.TimeoutExpired:
        return QueryFailure(f"{arguments[0]} 查询超时")
    except subprocess.CalledProcessError as err:
        message = (err.stderr or "").lower()
        detail = ("本机管理接口不可用" if arguments[0] == "ipmitool" and
                  "could not open device" in message and "no such file" in message else
                  "驱动不支持此查询" if "not supported" in message else
                  "系统拒绝读取" if "permitted" in message or "denied" in message else
                  f"查询失败，退出码 {err.returncode}")
        return QueryFailure(f"{arguments[0]} {detail}")
    except OSError:
        return QueryFailure(f"无法运行 {arguments[0]}")


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
    return {label: values.get(key) for key, label in names.items()}


def item(name, source, fields):
    for key, value in list(fields.items()):
        if not key.startswith(MISSING) and not text(value):
            fields.setdefault(MISSING + key, f"{source}未提供此字段")
    return {"name": text(name) or "未命名部件", "fields": {"来源": source, **{
        key: text(value) for key, value in fields.items() if text(value)
    }}}


def add_reason(entry, reason):
    reasons = [entry.get("reason", ""), reason]
    entry.update(status="partial" if entry["items"] else "unread",
                 reason="；".join(filter(None, reasons)).encode("utf-8")[:2048].decode("utf-8", errors="ignore"))


def storage_detail(part):
    fields = part["fields"]
    return fields.get("类型") not in {"磁盘", "系统可见磁盘"} and (
        fields.get("来源") == "本机 StorCLI" or part["name"].startswith(
            ("存储控制器 ", "阵列控制器 ", "物理盘 ", "逻辑盘 ", "光纤通道 "))
    )


def limit_storage_details(items, limit, *, room_for_notice=True):
    kept = []
    removed = 0
    for part in items:
        if part["name"] == "存储报告说明":
            removed += 1
        elif not storage_detail(part):
            kept.append(part)
        elif limit:
            kept.append(part)
            limit -= 1
        else:
            removed += 1
    if removed:
        notice = {MISSING + "附加部件": "报告上限已省略部分存储附加清单"}
        if room_for_notice:
            kept.append(item("存储报告说明", "采集器", notice))
        else:
            first = kept[0]
            kept[0] = {**first, "fields": {**first["fields"], **notice}}
    return kept


def section(key, title, items, reason=""):
    if not reason and any(any(k.startswith(MISSING) for k in part["fields"]) for part in items):
        reason = "部分字段未读取，原因见对应部件。"
    if len(items) > MAX_ITEMS:
        if key == "storage":
            basic = sum(not storage_detail(part) for part in items)
            if basic <= MAX_ITEMS:
                spare = MAX_ITEMS - basic
                items = limit_storage_details(items, max(0, spare - 1), room_for_notice=spare > 0)
                reason = reason or "报告上限已省略部分存储附加清单"
        if len(items) > MAX_ITEMS:
            items = items[:MAX_ITEMS]
            reason = "部件数量超过报告上限，清单已截断"
    status = "read"
    if reason:
        status = "partial" if items else "unread"
    result = {"key": key, "title": title, "status": status, "items": items}
    if reason:
        result["reason"] = reason.encode("utf-8")[:2048].decode("utf-8", errors="ignore")
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

    def collect(self, deadline=None):
        entries = [
            self.system(), self.cpu(), self.memory(), self.storage(), self.network(),
            self.expansion(), self.power(), self.cooling(),
        ]
        by_key = {entry["key"]: entry for entry in entries}
        self.collect_counters(by_key, deadline)
        self.management_events(by_key["system"])
        for entry in entries:
            if entry["status"] == "read" and any(key.startswith(MISSING) for part in entry["items"]
                                                 for key in part["fields"]):
                add_reason(entry, "部分字段未读取，原因见对应部件。")
        return {"sections": entries}

    def collect_counters(self, by_key, deadline):
        queries = {"network": self.network_errors, "memory": self.memory_errors,
                   "expansion": self.expansion_errors}

        def read_counters(sections):
            timestamp = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
            for key, query in queries.items():
                query(sections[key], timestamp)

        if deadline is None:
            read_counters(by_key)
            return
        if time.monotonic() < deadline:
            # 某些驱动可能卡在读取文件。只在限时内合并结果，保留基础清单用于上报。
            extra = {key: deepcopy(by_key[key]) for key in queries}
            worker = threading.Thread(target=read_counters, args=(extra,), daemon=True)
            worker.start()
            worker.join(max(0, deadline - time.monotonic()))
            if not worker.is_alive():
                for key in queries:
                    by_key[key].update(extra[key])
                return
        for key in queries:
            add_reason(by_key[key], "本次采集时限已到，未读取错误计数")

    def management_events(self, entry):
        raw = self.run("ipmitool", "-I", "open", "sel", "list", "last", str(MAX_EVENTS),
                       timeout=8, include_stderr=True)
        status = entry["items"][1]["fields"]
        if str(raw).strip() == "SEL has no entries":
            status[EVENT_STATUS] = "无记录"
            return
        lines = [line for line in raw.splitlines() if line.strip()]
        events = [fields for line in lines if (fields := event_fields(line))]
        available = min(MAX_EVENTS, MAX_ITEMS - len(entry["items"]))
        if events and available:
            status[EVENT_STATUS] = f"读到 {min(len(events), available)} 条"
            entry["items"].extend(item(EVENT_ITEM, "本机管理芯片事件日志", fields)
                                  for fields in reversed(events[-available:]))
        failure = getattr(raw, "reason", "") or (
            "管理芯片事件查询输出不完整或格式无法识别" if not events or len(lines) != len(events) else
            "部件数量超过报告上限，已省略部分管理芯片事件" if available < min(len(events), MAX_EVENTS) else ""
        )
        if failure:
            status[MISSING + EVENT_STATUS] = failure

    def network_errors(self, entry, timestamp):
        for part in entry["items"]:
            port = self.root / "sys/class/net" / part["name"]
            if not (port / "device").exists():
                continue
            counts = {label: unsigned(self.read(port / "statistics" / key))
                      for key, label in NETWORK_COUNTERS.items()}
            part["fields"].update(counter_fields(counts, "Linux 网口累计计数", timestamp,
                                                 "Linux 网口驱动未提供可读的计数"))

    def memory_errors(self, entry, timestamp):
        root = self.root / "sys/devices/system/edac/mc"
        controllers = sorted(dev for dev in root.glob("mc*") if re.fullmatch(r"mc[0-9]+", dev.name))
        if not controllers:
            add_reason(entry, "Linux 内存纠错驱动未提供内存错误计数")
            return
        dimms = [(controller, self.read(dimm / "dimm_label").casefold(), dimm)
                 for controller in controllers for dimm in sorted(controller.glob("*"))
                 if re.fullmatch(r"(dimm|rank)[0-9]+", dimm.name)]
        driver_labels = Counter(label for _, label, _ in dimms if label)
        firmware_labels = Counter(record["Locator"].casefold() for record in self.dmi.get(17, [])
                                  if "Locator" in record)
        installed = {part["name"].casefold(): part["fields"] for part in entry["items"]
                     if part["fields"].get("容量")}
        unmapped = set()
        source = "Linux 内存纠错驱动（本次启动）"
        reason = "Linux 内存纠错驱动未提供可读的计数"
        for controller, label, dimm in dimms:
            if label not in installed or driver_labels[label] != 1 or firmware_labels[label] != 1:
                unmapped.add(controller)
                continue
            counts = {name: unsigned(self.read(dimm / f"dimm_{key}"))
                      for key, name in MEMORY_COUNTERS.items()}
            installed[label].update(counter_fields(counts, source, timestamp, reason))
        for controller in controllers:
            if len(entry["items"]) >= MAX_ITEMS:
                add_reason(entry, "部件数量超过报告上限，已省略部分内存控制器计数")
                break
            counts = {name: unsigned(self.read(controller / key)) for key, name in MEMORY_COUNTERS.items()}
            fields = counter_fields(counts, source, timestamp, reason)
            fields["计数范围"] = "控制器总计，包含同一控制器下已展示的插槽"
            if controller in unmapped:
                fields["插槽对应"] = "部分驱动标签无法确认对应的已安装插槽"
            entry["items"].append(item(MEMORY_CONTROLLER + controller.name, "Linux 内存纠错驱动", fields))
        if entry["status"] == "unread" and entry["items"]:
            entry["status"] = "partial"

    def expansion_errors(self, entry, timestamp):
        for part in entry["items"]:
            device = self.root / "sys/bus/pci/devices" / part["name"]
            counts = pcie_counters(self, device)
            part["fields"].update(counter_fields(counts, "Linux PCIe 错误报告（本次启动）", timestamp,
                                                 "此设备未提供完整的 PCIe 错误计数"))

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
        fields.setdefault("机箱料号", "")
        fields.update(secure_boot(self.root))
        fields["采集格式"] = "2"
        fields["启动方式"] = "UEFI" if (self.root / "sys/firmware/efi").exists() else "非 UEFI"
        required = ("厂商", "型号", "整机序列号", "整机标识", "资产标签", "机箱料号",
                    "主板厂商", "主板型号", "主板序列号", "主板版本", "固件版本")
        missing = [label for label in required if not fields.get(label)]
        info = self.run("ipmitool", "-I", "open", "mc", "info", timeout=6)
        bmc = select_fields(properties(info), {"Product ID": "产品型号或编号", "Manufacturer Name": "厂商",
                                             "Firmware Revision": "固件版本"})
        explain(bmc, list(bmc), getattr(info, "reason", "") or "本机管理接口未返回此字段")
        items = [item("整机", "Linux 固件信息", fields), item("管理芯片", "本机管理接口", bmc)]
        chips = sorted((self.root / "sys/class/tpm").glob("tpm[0-9]*"))
        for chip in chips:
            version = self.read(chip / "tpm_version_major")
            fields = {"规范版本": {"1": "1.x", "2": "2.0"}.get(version)}
            items.append(item(f"安全芯片 {chip.name}", "Linux 安全芯片驱动", fields))
        if not chips:
            items.append(item("安全芯片", "Linux", {MISSING + "规范版本": "系统未列出安全芯片设备"}))
        return section("system", "整机、主板和固件", items,
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
                fields[level] = summary.get(level)
            items.append(item(slot, "机器固件和 Linux", fields))
        if caches:
            items += [item(label or "缓存", "机器固件", {"容量": size})
                      for label, size in caches.items() if size]
        has_slots = bool(self.dmi.get(4))
        fields = select_fields(summary, {
            "Architecture": "架构", "Model name": "型号", "CPU(s)": "线程数",
            "Socket(s)": "插槽数", "Core(s) per socket": "每个插槽物理核数",
            "CPU max MHz": "最大频率（MHz）", "L3 cache": "三级缓存",
        })
        if any(fields.values()):
            items.append(item("处理器概况", "Linux", fields))
        if not has_slots:
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
            "物理插槽": self.read(dev / "physical_slot") or next((
                slot.name for slot in (self.root / "sys/bus/pci/slots").glob("*")
                if self.read(slot / "address") == address.partition(".")[0]), ""),
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
        devices = disks.get("blockdevices")
        if not isinstance(devices, list):
            devices = []
        for disk in devices:
            disk = data_object(disk)
            name = disk.get("name")
            if disk.get("type") != "disk" or not isinstance(name, str) or not re.fullmatch(r"[a-zA-Z0-9_-]+", name):
                continue
            if len(names) >= MAX_ITEMS:
                reason.append("磁盘条目超过报告上限，清单已截断")
                break
            names.add(name)
            fields = select_fields(disk, {
                "size": "容量（字节）", "model": "型号", "serial": "序列号", "tran": "接口",
                "rev": "固件", "hctl": "SCSI 路径",
            })
            fields["类型"] = "系统可见磁盘" if raid else "磁盘"
            if disk.get("rota") is not None:
                fields["介质"] = "机械盘" if disk["rota"] else "非旋转介质"
            # smartctl 在磁盘健康异常时会返回非 0，但仍会返回完整报告。
            raw = self.run("smartctl", "-j", "-i", "-H", "-A", f"/dev/{name}", output_on_failure=True)
            smart = object_output(raw)
            stats = smart_fields(smart)
            explain(stats, list(stats), getattr(raw, "reason", "") or
                    ("磁盘自检未返回数据，系统可见盘可能由阵列提供" if raid else "磁盘自检未返回这项指标"))
            fields.update(stats)
            for smart_key, report_key in {"model_name": "型号", "serial_number": "序列号",
                                          "firmware_version": "固件", "protocol": "接口"}.items():
                if not fields.get(report_key) and smart.get(smart_key):
                    fields[report_key] = smart[smart_key]
            status = data_object(smart.get("smart_status")).get("passed")
            if isinstance(status, bool):
                fields["自检健康状态"] = "正常" if status else "异常"
            elif not raid:
                reason.append("部分磁盘健康状态未读取")
            if not isinstance(status, bool):
                fields[MISSING + "自检健康状态"] = getattr(raw, "reason", "") or "磁盘自检未返回健康状态"
            if not raid:
                missing = [key for key in ("型号", "序列号", "容量（字节）", "接口", "固件")
                           if not fields.get(key)]
                if missing:
                    reason.append("部分磁盘字段未读取：" + "、".join(missing))
            position = self.disk_bay(name)
            fields["盘位"] = position
            explain(fields, ["盘位"], "系统未提供机箱槽位与该设备的对应关系")
            if not position and not raid and not name.startswith(("vd", "xvd")):
                reason.append("部分磁盘盘位未读取")
            items.append(item(name, "Linux 和磁盘自检", fields))
        if raid:
            matched, complete = self.storcli()
            items.extend(matched)
            if not matched:
                fields = {}
                explain(fields, ["缓存容量", "缓存保护状态", "热备盘", "重建进度", "阵列组与盘位"],
                        self.raid_query_reason)
                items.append(item("阵列详细信息", "本机 StorCLI", fields))
            if not complete or len(raid) > 1:
                reason.append("阵列控制器已识别，部分物理盘、盘位或阵列关系未读取")
        fc_ports = sorted((self.root / "sys/class/fc_host").glob("host*"))
        for port in fc_ports:
            fields = {label: valid(self.read(port / key)) for key, label in {
                "port_name": "端口标识", "node_name": "节点标识", "port_state": "端口状态",
                "speed": "当前速率", "supported_speeds": "支持速率",
            }.items()}
            if str(fields["端口状态"]).lower() in ("offline", "linkdown"):
                fields["当前速率"] = ""
                explain(fields, ["当前速率"], "光纤端口未接通")
            items.append(item(f"光纤通道 {port.name}", "Linux 光纤通道驱动", fields))
        if not fc_ports and any(self.read(dev / "class").startswith("0x0c04") for dev in self.pci):
            reason.append("已识别光纤通道控制器，驱动未列出可读端口")
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
        query = self.run("storcli", "/call", "show", "all", "J", timeout=8)
        value = object_output(query)
        self.raid_query_reason = getattr(query, "reason", "") or "StorCLI 未返回兼容控制器的数据"
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
                "缓存容量": (data.get("HwCfg") or {}).get("On Board Memory Size"),
                "缓存保护状态": protection_state(data),
            }))
            for row in data.get("VD LIST") or []:
                items.append(item(f"逻辑盘 {controller}/{row.get('DG/VD', '?')}", "本机 StorCLI",
                                  select_fields(row, {"DG/VD": "阵列组/逻辑盘", "TYPE": "阵列级别",
                                                      "State": "状态", "Size": "容量", "Name": "名称"})))
        detail = object_output(self.run("storcli", "/call/eall/sall", "show", "all", "J",
                                        timeout=8)) if controller_count else {}
        disk_count = 0
        complete = True
        progress = {}
        progress_loaded = False
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
                health = next((entry for name, entry in data.items() if name.endswith("State") and
                               isinstance(entry, dict)), {})
                for row in rows:
                    disk_count += 1
                    fields = select_fields(row, {
                        "EID:Slt": "机箱:盘位", "DG": "阵列组", "Model": "型号", "Intf": "接口",
                        "Med": "介质", "Size": "容量", "State": "状态",
                    })
                    fields.update(select_fields(attributes, {"SN": "序列号", "Firmware Revision": "固件"}))
                    fields.update(select_fields(health, {"Media Error Count": "介质错误次数",
                                                         "Drive Temperature": "温度"}))
                    state = row.get("State")
                    fields["热备角色"] = {"GHS": "全局热备", "DHS": "专用热备", "Onln": "在线成员",
                                        "Rbld": "重建成员"}.get(state, state)
                    if state == "Rbld":
                        if not progress_loaded:
                            raw = self.run("storcli", "/call/eall/sall", "show", "rebuild", "J", timeout=8)
                            progress = rebuild_progress(object_output(raw))
                            progress_loaded = True
                        bay = str(row.get("EID:Slt", "")).split(":")
                        fields["重建进度"] = progress.get((str(status.get("Controller")), *bay))
                    elif state == "Onln":
                        fields["重建进度"] = "未在重建"
                    explain(fields, ["已用寿命（%）", "通电小时"], "控制器未透传物理盘的自检指标")
                    complete &= all(fields.get(field) for field in
                                    ("机箱:盘位", "型号", "序列号", "容量", "接口", "固件", "状态"))
                    items.append(item(f"物理盘 {status.get('Controller', '?')}/{row.get('EID:Slt', '?')}",
                                      "本机 StorCLI", fields))
        return items, complete and disk_count > 0

    def network(self):
        items, seen, missing = [], set(), False
        pci_cards = {dev.resolve(): dev for dev in self.pci if self.read(dev / "class").startswith("0x02")}
        for port in sorted((self.root / "sys/class/net").glob("*")):
            device = port / "device"
            if not device.exists():
                continue
            device = device.resolve()
            # virtio 网口挂在网卡的子设备上，向上找最近的 PCI 网卡。
            for parent in (device, *device.parents):
                if parent in pci_cards:
                    device = pci_cards[parent]
                    break
            seen.add(device)
            fields = self.pci_fields(device) if self.read(device / "class") else {}
            settings = properties(self.run("ethtool", "-i", port.name))
            for label, value in select_fields(settings, {"driver": "驱动", "firmware-version": "固件"}).items():
                fields[label] = valid(value) or fields.get(label)
            mac = self.read(port / "address")
            carrier = self.read(port / "carrier")
            speed = self.read(port / "speed")
            fields.update({"硬件地址": mac, "连接": {"0": "未接通", "1": "接通"}.get(carrier),
                           "当前速率": f"{speed} Mb/s" if speed.isdigit() and int(speed) > 0 and carrier != "0" else "",
                           "启动网口": ("是" if mac.lower() == self.boot_mac else "否")
                           if mac and self.boot_mac else ""})
            fields.update(port_fields(self.run("ethtool", port.name), self.run("ethtool", "-m", port.name)))
            if carrier == "0":
                explain(fields, ["当前速率"], "网口未接通，无法协商速率")
            missing |= not all((mac, fields.get("固件"), fields.get("设备"), fields.get("驱动"),
                                carrier, speed.isdigit() or carrier == "0"))
            items.append(item(port.name, "Linux 和网卡驱动", fields))
        for dev in pci_cards.values():
            if dev not in seen:
                fields = self.pci_fields(dev)
                fields["状态"] = "网口未读取"
                items.append(item(f"未启用网卡 {dev.name}", "Linux PCI 设备列表", fields))
                missing = True
        reason = ""
        if not items:
            reason = "未读到物理网卡"
        elif missing or not any(port["fields"].get("启动网口") == "是" for port in items):
            reason = "部分网口、固件、连接状态或启动网口未读取"
        return section("network", "网卡和网口", items, reason)

    def expansion(self):
        items = []
        for dev in self.pci:
            device_class = self.read(dev / "class")
            if not device_class.startswith(("0x01", "0x02", "0x06")):
                fields = self.pci_fields(dev)
                fields["类别编号"] = device_class
                if device_class.startswith(("0x03", "0x12")):
                    fields.update(accelerator_fields(self, dev))
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
    by_key = {entry["key"]: entry for entry in report["sections"]}
    system = by_key.get("system")
    if len(payload) > MAX_REPORT and system:
        base = [part for part in system["items"] if part["name"] != EVENT_ITEM]
        if len(base) < len(system["items"]):
            system["items"] = base
            chip = next(part for part in base if part["name"] == "管理芯片")
            chip["fields"].pop(EVENT_STATUS, None)
            chip["fields"][MISSING + EVENT_STATUS] = "报告大小上限已省略管理芯片事件"
            if system["status"] == "read":
                add_reason(system, "报告大小上限已省略管理芯片事件")
            payload = to_json()
    if len(payload) > MAX_REPORT:
        for key in ("network", "memory", "expansion"):
            entry = by_key.get(key)
            if not entry:
                continue
            removed = False
            retained = []
            for part in entry["items"]:
                if key == "memory" and part["name"].startswith(MEMORY_CONTROLLER):
                    removed = True
                    continue
                fields = part["fields"]
                for label in list(fields):
                    if label.removeprefix(MISSING) in COUNTER_FIELDS:
                        del fields[label]
                        removed = True
                retained.append(part)
            if removed:
                entry["items"] = retained
                add_reason(entry, "报告大小上限已省略错误计数")
        payload = to_json()
    storage = by_key.get("storage")
    if storage:
        details = sum(storage_detail(part) for part in storage["items"])
        while len(payload) > MAX_REPORT and details:
            details //= 2
            storage["items"] = limit_storage_details(storage["items"], details)
            if storage["status"] == "read":
                add_reason(storage, "部分存储附加清单受报告上限省略")
            payload = to_json()
    while len(payload) > MAX_REPORT:
        entries = max(report["sections"], key=lambda entry: len(entry["items"]))
        entries["items"] = entries["items"][:len(entries["items"]) // 2]
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
    if not url and "lober.inventory_token" in args:
        serial = args.get("lober.sn", "")
        if re.fullmatch(r"[~a-zA-Z0-9_.-]+", serial):
            url = args.get("lober.gateway", "").rstrip("/") + "/v1/hardware/" + serial
            token = args["lober.inventory_token"]
    if not url.startswith(("http://", "https://")) or not re.fullmatch(r"[A-Za-z0-9_-]+", token):
        print("Lober: missing or invalid report destination or upload token", flush=True)
        return 1
    started = time.monotonic()
    deadline = started + 110

    def local_command(*arguments, timeout=4, **options):
        remaining = deadline - time.monotonic()
        return (command(*arguments, timeout=min(remaining, timeout), **options) if remaining > 0
                else QueryFailure("本次采集时限已到"))

    print("Lober: reading hardware inventory", flush=True)
    report = Collector(Path("/"), args.get("lober.discovery_mac", ""), local_command).collect(deadline)
    print(f"Lober: hardware inventory read in {time.monotonic() - started:.1f}s", flush=True)
    for entry in report["sections"]:
        print(f"  {entry['key']}: {entry['status']} ({len(entry['items'])} items)", flush=True)
    return 0 if uploaded(url, token, encode(report), Path("/run")) else 1


if __name__ == "__main__":
    raise SystemExit(main())
