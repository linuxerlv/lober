"""只读解析本机驱动和厂商工具提供的部件属性。"""

import csv
import re
from datetime import datetime


MISSING = "未读取·"
NETWORK_COUNTERS = {
    "rx_crc_errors": "接收校验错误次数", "rx_errors": "接收错误次数", "tx_errors": "发送错误次数",
    "rx_dropped": "接收丢包数", "tx_dropped": "发送丢包数",
}
MEMORY_COUNTERS = {"ce_count": "可纠正错误次数", "ue_count": "不可纠正错误次数"}
PCIE_CORRECTABLE = "可纠正链路错误次数"
PCIE_UNCORRECTABLE = "不可纠正链路错误次数"
COUNTER_SOURCE = "错误计数来源"
COUNTER_TIME = "错误计数采集时间"
COUNTER_FIELDS = frozenset((*NETWORK_COUNTERS.values(), *MEMORY_COUNTERS.values(),
                            PCIE_CORRECTABLE, PCIE_UNCORRECTABLE, COUNTER_SOURCE, COUNTER_TIME))


def unsigned(value):
    return value if re.fullmatch(r"[0-9]{1,20}", value) else ""


def counter_fields(counts, source, timestamp, reason):
    fields = {label: value for label, value in counts.items() if value}
    if fields:
        fields.update({COUNTER_SOURCE: source, COUNTER_TIME: timestamp})
    explain(fields, counts, reason)
    return fields


def pcie_counters(collector, device):
    totals = {}
    for severity, label in (("correctable", "COR"), ("nonfatal", "NONFATAL"), ("fatal", "FATAL")):
        for line in collector.lines(device / f"aer_dev_{severity}"):
            columns = line.split()
            if len(columns) == 2 and columns[0] == f"TOTAL_ERR_{label}":
                totals[severity] = unsigned(columns[1])
                break
    nonfatal, fatal = totals.get("nonfatal"), totals.get("fatal")
    return {PCIE_CORRECTABLE: totals.get("correctable"),
            PCIE_UNCORRECTABLE: str(int(nonfatal) + int(fatal)) if nonfatal and fatal else ""}


def event_fields(line):
    columns = [column.strip() for column in line.split("|")]
    if len(columns) < 2 or not re.fullmatch(r"[0-9a-fA-F]{1,4}", columns[0]):
        return {}
    number, *values = columns
    fields = {"事件编号": number}
    if values[0].startswith(("OEM record ", "Linux kernel panic:")):
        fields[MISSING + "设备上报时间"] = "设备未提供事件时间"
    elif len(values) >= 3:
        date, clock, *values = values
        fields["设备上报时间"] = f"{date} {clock}".strip()
        fields["时间说明"] = "无法确认设备时间，已保留原值"
        for pattern in ("%m/%d/%y %H:%M:%S", "%m/%d/%Y %H:%M:%S"):
            try:
                datetime.strptime(f"{date} {clock.partition(' ')[0]}", pattern)
            except ValueError:
                continue
            fields["时间说明"] = "设备时钟读数，时区未确认"
            break
    else:
        return {}
    if values[0].startswith(("OEM record ", "Linux kernel panic:")):
        # 厂商原始字节没有通用解释，只保留可识别的事件类型。
        fields["事件"] = values[0]
    elif len(values) >= 2 and values[0]:
        fields.update({"部件": values[0], "事件": " | ".join(value for value in values[1:] if value)})
    else:
        return {}
    explain(fields, ("部件", "事件"), "设备未提供可读内容")
    return fields


def explain(fields, labels, reason):
    for label in labels:
        if fields.get(label) in (None, ""):
            fields[MISSING + label] = reason


def valid(value):
    return "" if str(value).strip().lower() in {
        "", "-", "none", "n/a", "[n/a]", "unknown", "unknown speed", "unknown width",
        "not reported", "[not supported]",
    } else value


def data_object(value):
    return value if isinstance(value, dict) else {}


def smart_fields(smart):
    nvme = data_object(smart.get("nvme_smart_health_information_log"))
    endurance = data_object(smart.get("endurance_used")).get("current_percent")
    measurements = {
        "已用寿命（%）": nvme.get("percentage_used", smart.get("scsi_percentage_used_endurance_indicator", endurance)),
        "通电小时": data_object(smart.get("power_on_time")).get("hours", nvme.get("power_on_hours")),
        "温度（°C）": data_object(smart.get("temperature")).get("current", nvme.get("temperature")),
        "介质错误次数": nvme.get("media_errors"),
    }
    return {label: value if re.fullmatch(r"-?\d+(?:\.\d+)?", str(value)) else None
            for label, value in measurements.items()}


def port_fields(query, module):
    # 只读本端支持的模式。对端或当前通告的速率可能较低。
    modes = re.search(r"Supported link modes:[ \t]*([^\n]*(?:\n[ \t]+\d+base[^\n]*)*)",
                      query, re.IGNORECASE)
    speeds = [int(n) for n in re.findall(r"\b(\d+)base", modes[1], re.I)] if modes else []
    optics = {key.strip(): valid(value.strip()) for line in module.splitlines() if ":" in line
              for key, value in [line.split(":", 1)]}
    fields = {
        "最高支持速率": f"{max(speeds)} Mb/s" if speeds else "",
        "模块型号（料号）": optics.get("Vendor PN"), "模块序列号": optics.get("Vendor SN"),
    }
    explain(fields, ["最高支持速率"], getattr(query, "reason", "") or "网卡驱动未列出支持的链路模式")
    explain(fields, ["模块型号（料号）", "模块序列号"],
            getattr(module, "reason", "") or "网口未提供可读的模块资料")
    return fields


def accelerator_fields(collector, dev):
    fields = {
        "型号": collector.pci_descriptions.get(dev.name),
        "显存（字节）": valid(collector.read(dev / "mem_info_vram_total")),
        "序列号": valid(collector.read(dev / "serial_number") or collector.read(dev / "serial")),
        "当前通道数": valid(collector.read(dev / "current_link_width")),
        "当前链路速率": valid(collector.read(dev / "current_link_speed")),
    }
    reason = "设备驱动未提供此属性"
    if collector.read(dev / "vendor") == "0x10de":
        result = collector.run("nvidia-smi", "-i", dev.name, "--query-gpu=name,memory.total,serial",
                               "--format=csv,noheader,nounits")
        row = next(csv.reader(result.splitlines(), skipinitialspace=True), [])
        if len(row) == 3:
            fields["型号"] = valid(row[0]) or fields["型号"]
            if row[1].isdigit():
                fields["显存（字节）"] = int(row[1]) * 1024 * 1024
            fields["序列号"] = valid(row[2]) or fields["序列号"]
        reason = getattr(result, "reason", "") or reason
    explain(fields, ["型号", "显存（字节）", "序列号"], reason)
    explain(fields, ["当前通道数", "当前链路速率"], "系统未公开当前扩展总线链路信息")
    return fields


def secure_boot(root):
    efi = root / "sys/firmware/efi"
    if not efi.is_dir():
        return {MISSING + "安全启动": "本次没有以 UEFI 方式启动"}
    path = efi / "efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"
    try:
        data = path.read_bytes()
    except PermissionError:
        return {MISSING + "安全启动": "系统拒绝读取安全启动变量"}
    except OSError:
        return {MISSING + "安全启动": "系统未提供可读的安全启动变量"}
    if len(data) == 5 and data[4] in (0, 1):
        return {"安全启动": "已开启" if data[4] else "已关闭"}
    return {MISSING + "安全启动": "固件返回的安全启动变量格式无效"}


def protection_state(data):
    for name in ("Cachevault_Info", "CacheVault_Info", "BBU_Info"):
        rows = data.get(name) or []
        if isinstance(rows, list):
            states = [str(row["State"]) for row in rows if isinstance(row, dict) and valid(row.get("State"))]
            if states:
                return f"{'电池' if name == 'BBU_Info' else '电容'} · " + "、".join(states)
    return ""


def rebuild_progress(result):
    progress = {}
    for controller in result.get("Controllers") or []:
        status = controller.get("Command Status") or {}
        if status.get("Status") != "Success":
            continue
        for key, rows in (controller.get("Response Data") or {}).items():
            if not isinstance(rows, list):
                continue
            for row in rows:
                if not isinstance(row, dict):
                    continue
                address = re.search(r"/c(\d+)/e(\d+)/s(\d+)\b", str(row.get("Drive-ID") or key), re.I)
                value = str(row.get("Progress%", "")).strip().removesuffix("%")
                if address and value.isdigit() and 0 <= int(value) <= 100:
                    progress[address.groups()] = value + "%"
    return progress
