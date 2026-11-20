import json
import os
import subprocess
import tempfile
import threading
import time
import unittest
from pathlib import Path
from unittest.mock import patch

from collect import Collector, QueryFailure, command, dmi_records, encode, section, uploaded
from firmware import Firmware
from hardware import MISSING, port_fields, secure_boot

SEL = ("ipmitool", "-I", "open", "sel", "list", "last", "20")
DMI = ("dmidecode", "--type", "0,1,2,3,4,7,17,39")


class CollectorTest(unittest.TestCase):
    def setUp(self):
        root = tempfile.TemporaryDirectory()
        self.addCleanup(root.cleanup)
        self.root = Path(root.name)
        self.outputs = {}

    def file(self, name, value):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(value)

    def command(self, *arguments, **options):
        return self.outputs.get(arguments, "")

    def report(self):
        output = Collector(self.root, "52:54:00:12:34:56", self.command).collect()
        return {section["key"]: section for section in output["sections"]}

    def test_no_disk_or_tools_still_reports_other_hardware(self):
        self.file("sys/class/dmi/id/product_name", "Server A\n")
        self.file("sys/class/dmi/id/product_serial", "SERIAL-1\n")
        self.file("proc/meminfo", "MemTotal:       2048192 kB\n")
        report = self.report()
        self.assertEqual(len(report), 8)
        self.assertEqual(report["system"]["items"][0]["fields"]["型号"], "Server A")
        self.assertEqual(report["storage"]["status"], "unread")
        self.assertEqual(report["memory"]["status"], "partial")
        self.assertIn("插槽", report["memory"]["reason"])

    def test_memory_slots_keep_empty_and_serial_numbers(self):
        self.outputs[("dmidecode", "--type", "0,1,2,3,4,7,17,39")] = (
            "Handle 0x20, DMI type 17, 84 bytes\nMemory Device\n"
            "\tSize: 32 GB\n\tLocator: A1\n\tSerial Number: 42\n"
            "Handle 0x21, DMI type 17, 84 bytes\nMemory Device\n"
            "\tSize: No Module Installed\n\tLocator: A2\n"
        )
        memory = self.report()["memory"]
        self.assertEqual(memory["items"][0]["fields"]["序列号"], "42")
        self.assertEqual(memory["items"][1]["fields"]["状态"], "未安装")
        self.assertEqual(memory["status"], "partial")

    def test_placeholder_is_reported_as_missing(self):
        self.file("sys/class/dmi/id/product_name", "Server A\n")
        self.file("sys/class/dmi/id/product_serial", "To Be Filled By O.E.M.\n")
        system = self.report()["system"]
        self.assertNotIn("整机序列号", system["items"][0]["fields"])
        self.assertEqual(system["status"], "partial")
        self.assertIn("整机序列号", system["reason"])

    def test_boot_port_and_raid_visible_when_other_queries_fail(self):
        self.file("sys/class/net/eno1/address", "52:54:00:12:34:56\n")
        self.file("sys/class/net/eno1/carrier", "1\n")
        self.file("sys/class/net/eno1/speed", "10000\n")
        self.file("sys/class/net/eno1/device/class", "0x020000\n")
        self.file("sys/bus/pci/devices/raid/class", "0x010400\n")
        self.outputs[("lsblk", "--json", "--bytes", "--nodeps", "--output",
                      "NAME,TYPE,SIZE,MODEL,SERIAL,TRAN,ROTA,REV,HCTL")] = (
            '{"blockdevices":[{"name":"sda","type":"disk","size":123,"hctl":"1:0:0:0"}]}'
        )
        report = self.report()
        port = report["network"]["items"][0]["fields"]
        self.assertEqual(port["启动网口"], "是")
        self.assertEqual(port["当前速率"], "10000 Mb/s")
        storage = report["storage"]
        self.assertIn("阵列", storage["reason"])
        self.assertEqual(storage["items"][1]["fields"]["类型"], "系统可见磁盘")

    def test_dmi_ignores_nested_properties(self):
        records = dmi_records(
            "Handle 0x01, DMI type 4, 40 bytes\nProcessor Information\n"
            "\tVersion: Processor A\n\tFlags:\n\t\tBad: wrong\n"
        )
        self.assertEqual(records, {4: [{"Version": "Processor A"}]})

    def test_disk_endurance_and_bay_preserve_zero(self):
        name = "nvme0n1"
        self.file(f"sys/class/enclosure/cage/bay/device/block/{name}/present", "1")
        self.file("sys/class/enclosure/cage/bay/slot", "0")
        self.outputs[("lsblk", "--json", "--bytes", "--nodeps", "--output",
                      "NAME,TYPE,SIZE,MODEL,SERIAL,TRAN,ROTA,REV,HCTL")] = json.dumps({
            "blockdevices": [{"name": name, "type": "disk", "size": 1024, "rota": False}]})
        cmd = ("smartctl", "-j", "-i", "-H", "-A", "/dev/" + name)
        self.outputs[cmd] = json.dumps({"nvme_smart_health_information_log": {
            "percentage_used": 0, "power_on_hours": 120, "temperature": 36, "media_errors": 0}})
        disk = self.report()["storage"]["items"][0]["fields"]
        for key, value in {"盘位": "0", "已用寿命（%）": "0", "通电小时": "120",
                           "温度（°C）": "36", "介质错误次数": "0"}.items():
            self.assertEqual(disk[key], value)
        self.outputs[cmd] = "truncated {"
        disk = self.report()["storage"]["items"][0]["fields"]
        self.assertNotIn("介质错误次数", disk)
        self.assertIn(MISSING + "介质错误次数", disk)
        self.outputs[cmd] = json.dumps({"smart_status": [], "power_on_time": "unknown",
                                       "endurance_used": {"current_percent": 6}})
        disk = self.report()["storage"]["items"][0]["fields"]
        self.assertEqual(disk["已用寿命（%）"], "6")
        self.assertIn(MISSING + "通电小时", disk)

    def test_optics_and_offline_fibre_port(self):
        links = "Supported link modes: 1000baseT/Full\n       25000baseCR/Full\nAdvertised link modes: 100000baseCR4/Full"
        fields = port_fields(links, "Vendor PN: OPT-1\nVendor SN: SN-2")
        self.assertEqual(fields["最高支持速率"], "25000 Mb/s")
        self.assertEqual(fields["模块型号（料号）"], "OPT-1")
        self.assertEqual(fields["模块序列号"], "SN-2")
        self.assertIn(MISSING + "最高支持速率", port_fields("Supported link modes:\nAdvertised link modes: 100000baseCR4/Full", ""))
        self.file("sys/class/fc_host/host3/port_name", "0x2000000000000001")
        self.file("sys/class/fc_host/host3/port_state", "Linkdown")
        self.file("sys/class/fc_host/host3/speed", "16 Gbit")
        fc = self.report()["storage"]["items"][0]["fields"]
        self.assertEqual(fc["端口标识"], "0x2000000000000001")
        self.assertIn("未接通", fc[MISSING + "当前速率"])

    def test_disconnected_port_does_not_report_last_negotiated_speed(self):
        self.file("sys/class/net/eth0/device/class", "0x020000")
        self.file("sys/class/net/eth0/carrier", "0")
        self.file("sys/class/net/eth0/speed", "25000")
        port = self.report()["network"]["items"][0]["fields"]
        self.assertNotIn("当前速率", port)
        self.assertIn("未接通", port[MISSING + "当前速率"])

    def test_management_tpm_and_secure_boot(self):
        self.file("sys/class/tpm/tpm0/tpm_version_major", "2")
        self.outputs[("ipmitool", "-I", "open", "mc", "info")] = (
            "Product ID : 42 (board A)\nFirmware Revision : 7.1")
        self.assertIn(MISSING + "安全启动", secure_boot(self.root))
        efivar = "sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"
        self.file(efivar, "")
        self.assertIn("格式", secure_boot(self.root)[MISSING + "安全启动"])
        for bit, expected in [(0, "已关闭"), (1, "已开启")]:
            (self.root / efivar).write_bytes(bytes([7, 0, 0, 0, bit]))
            system = {i["name"]: i["fields"] for i in self.report()["system"]["items"]}
            self.assertEqual(system["整机"]["安全启动"], expected)
            self.assertEqual(system["管理芯片"]["产品型号或编号"], "42 (board A)")
            self.assertEqual(system["管理芯片"]["固件版本"], "7.1")
            self.assertEqual(system["安全芯片 tpm0"]["规范版本"], "2.0")

    def test_gpu_properties_stay_with_their_device(self):
        for name, vendor in [("gpu0", "0x10de"), ("gpu1", "0x1002")]:
            self.file(f"sys/bus/pci/devices/{name}/class", "0x030200")
            self.file(f"sys/bus/pci/devices/{name}/vendor", vendor)
        self.file("sys/bus/pci/devices/gpu0/physical_slot", "3")
        self.file("sys/bus/pci/devices/gpu0/current_link_width", "8")
        self.file("sys/bus/pci/devices/gpu1/mem_info_vram_total", "16384")
        self.outputs[("nvidia-smi", "-i", "gpu0", "--query-gpu=name,memory.total,serial",
                      "--format=csv,noheader,nounits")] = "Card A, 8192, SN-1"
        gpu, amd = [i["fields"] for i in self.report()["expansion"]["items"]]
        self.assertEqual((gpu["型号"], gpu["序列号"], gpu["显存（字节）"]), ("Card A", "SN-1", str(8192 << 20)))
        self.assertEqual((gpu["物理插槽"], gpu["当前通道数"]), ("3", "8"))
        self.assertEqual(amd["显存（字节）"], "16384")
        self.assertIn(MISSING + "序列号", amd)

    def test_management_events_keep_latest_and_preserve_uncertain_time(self):
        entries = [f"{index:x} | 11/20/2026 | 12:00:00 | Power Supply #0x02 | Input lost | Asserted"
                   for index in range(1, 21)]
        entries.append("  15 | Pre-Init | 0000000007 | Memory #0x01 | Correctable ECC | Deasserted")
        self.outputs[SEL] = "\n".join(entries)
        parts = self.report()["system"]["items"]
        events = [part["fields"] for part in parts if part["name"] == "管理芯片事件"]
        self.assertEqual(len(events), 20)
        self.assertEqual(events[0]["事件编号"], "15")
        self.assertEqual(events[0]["设备上报时间"], "Pre-Init 0000000007")
        self.assertIn("无法确认", events[0]["时间说明"])
        self.assertEqual(events[0]["部件"], "Memory #0x01")
        self.assertIn("Correctable ECC", events[0]["事件"])
        self.assertEqual(events[1]["设备上报时间"], "11/20/2026 12:00:00")
        self.assertEqual(events[-1]["事件编号"], "2")

    def test_management_events_distinguish_empty_failed_and_incomplete_queries(self):
        for raw, label, expected in [
            ("SEL has no entries\n", "近期事件", "无记录"),
            (QueryFailure("ipmitool 本机管理接口不可用"), MISSING + "近期事件", "不可用"),
            (QueryFailure("ipmitool 查询超时"), MISSING + "近期事件", "超时"),
            ("Get SEL Entry failed\n", MISSING + "近期事件", "输出"),
            ("", MISSING + "近期事件", "输出"),
        ]:
            with self.subTest(expected=expected):
                self.outputs[SEL] = raw
                report = self.report()
                self.assertEqual(len(report), 8)
                self.assertIn(expected, report["system"]["items"][1]["fields"].get(label, ""))
                self.assertFalse(any(part["name"] == "管理芯片事件" for part in report["system"]["items"]))

    def test_management_event_without_timestamp_omits_undecoded_bytes(self):
        self.outputs[SEL] = "1 | OEM record e0 | 001122334455\n"
        event = self.report()["system"]["items"][-1]["fields"]
        self.assertIn("未提供", event[MISSING + "设备上报时间"])
        self.assertIn("e0", event["事件"])
        self.assertNotIn("001122334455", str(event))

    def test_management_event_keeps_ipmitool_short_date_and_timezone(self):
        self.outputs[SEL] = "1 | 11/20/26 | 12:00:00 UTC | Power Supply | Input lost | Asserted"
        event = self.report()["system"]["items"][-1]["fields"]
        self.assertEqual(event["设备上报时间"], "11/20/26 12:00:00 UTC")
        self.assertNotIn("无法确认设备时间", event["时间说明"])
        self.assertIn("时区", event["时间说明"])

    def test_event_query_runs_after_basic_inventory(self):
        queries = []

        def run(*arguments, **options):
            queries.append(arguments)
            return self.command(*arguments, **options)

        Collector(self.root, "", run).collect()
        self.assertEqual(queries[-1], SEL)
        self.assertIn(("ipmitool", "-I", "open", "sdr", "type", "Temperature"), queries[:-1])

    def test_unresponsive_counter_does_not_delay_basic_report_past_deadline(self):
        self.file("sys/class/net/eth0/device/class", "0x020000\n")
        release = threading.Event()
        self.addCleanup(release.set)

        class SlowCounter(Collector):
            def read(self, path):
                if "statistics" in Path(path).parts:
                    release.wait(2)
                return super().read(path)

        started = time.monotonic()
        report = SlowCounter(self.root, "", self.command).collect(deadline=started + 0.1)
        self.assertLess(time.monotonic() - started, 1)
        network = report["sections"][4]
        self.assertEqual(network["items"][0]["name"], "eth0")
        self.assertIn("采集时限", network["reason"])

    def test_network_counters_belong_to_each_physical_port(self):
        for port, crc in [("eth0", "0"), ("eth1", "9")]:
            self.file(f"sys/class/net/{port}/device/class", "0x020000\n")
            for key, value in {"rx_crc_errors": crc, "rx_errors": "9", "tx_errors": "1",
                               "rx_dropped": "12", "tx_dropped": "-1"}.items():
                self.file(f"sys/class/net/{port}/statistics/{key}", value)
        self.file("sys/class/net/lo/statistics/rx_errors", "99")
        ports = {part["name"]: part["fields"] for part in self.report()["network"]["items"]}
        self.assertEqual(set(ports), {"eth0", "eth1"})
        self.assertEqual(ports["eth0"]["接收校验错误次数"], "0")
        self.assertEqual(ports["eth1"]["接收校验错误次数"], "9")
        self.assertEqual(ports["eth1"]["接收错误次数"], "9")
        self.assertEqual(ports["eth1"]["发送错误次数"], "1")
        self.assertEqual(ports["eth1"]["接收丢包数"], "12")
        self.assertIn(MISSING + "发送丢包数", ports["eth1"])
        self.assertIn("累计", ports["eth1"]["错误计数来源"])

    def test_memory_counters_only_attach_to_unique_installed_slots(self):
        records = [("A1", "32 GB"), ("A2", "No Module Installed"), ("B1", "32 GB"), ("B1", "32 GB")]
        self.outputs[DMI] = "".join(f"Handle 0x{index:02x}, DMI type 17, 84 bytes\n"
                                    f"\tLocator: {slot}\n\tSize: {size}\n"
                                    for index, (slot, size) in enumerate(records))
        for controller in ("mc0", "mc1"):
            path = f"sys/devices/system/edac/mc/{controller}"
            self.file(path + "/ce_count", "19")
            self.file(path + "/ue_count", "3")
            for index, label in enumerate(("a1", "A2", "B1", "C1") if controller == "mc0" else ("C1",)):
                dimm = f"{path}/dimm{index}"
                self.file(dimm + "/dimm_label", label)
                self.file(dimm + "/dimm_ce_count", "0")
                self.file(dimm + "/dimm_ue_count", "2")
        parts = self.report()["memory"]["items"]
        first = parts[0]["fields"]
        self.assertEqual(first["可纠正错误次数"], "0")
        self.assertEqual(first["不可纠正错误次数"], "2")
        self.assertIn("内存纠错", first["错误计数来源"])
        self.assertRegex(first["错误计数采集时间"], r"^\d{4}-\d\d-\d\dT.+Z$")
        self.assertEqual(parts[1]["fields"]["状态"], "未安装")
        for part in parts[1:4]:
            self.assertNotIn("可纠正错误次数", part["fields"])
        for controller in parts[4:]:
            self.assertEqual(controller["fields"]["可纠正错误次数"], "19")
            self.assertIn("内存控制器", controller["name"])

    def test_memory_without_error_driver_reports_missing_counts(self):
        self.file("proc/meminfo", "MemTotal: 2048 kB\n")
        memory = self.report()["memory"]
        self.assertIn("未提供内存错误计数", memory["reason"])
        self.assertEqual(memory["items"][0]["fields"]["可用总量"], "2048 kB")

    def test_expansion_counts_use_reported_totals_and_do_not_invent_zero(self):
        for name in ("gpu0", "gpu1"):
            self.file(f"sys/bus/pci/devices/{name}/class", "0x030200")
        path = "sys/bus/pci/devices/gpu0"
        self.file(path + "/aer_dev_correctable", "Receiver Error 9\nTOTAL_ERR_COR 0\n")
        self.file(path + "/aer_dev_nonfatal", "TOTAL_ERR_NONFATAL 2\n")
        gpu, other = [part["fields"] for part in self.report()["expansion"]["items"]]
        self.assertEqual(gpu["可纠正链路错误次数"], "0")
        self.assertIn(MISSING + "不可纠正链路错误次数", gpu)
        self.assertIn(MISSING + "可纠正链路错误次数", other)
        self.file(path + "/aer_dev_fatal", "TOTAL_ERR_FATAL 3\n")
        gpu = self.report()["expansion"]["items"][0]["fields"]
        self.assertEqual(gpu["不可纠正链路错误次数"], "5")
        self.assertRegex(gpu["错误计数采集时间"], r"^\d{4}-\d\d-\d\dT.+Z$")

    def test_report_size_drops_history_before_inventory_and_preserves_visible_disk(self):
        self.outputs[SEL] = "1 | 11/20/2026 | 12:00:00 | Power Supply #0x02 | Input lost | Asserted"
        report = {"sections": list(self.report().values())}
        disk = {"name": "sda", "fields": {"类型": "系统可见磁盘", "容量（字节）": "1024"}}
        members = [{"name": f"物理盘 0/1:{index}", "fields": {
            "类型": "阵列成员盘", **{f"字段{n}": "读" * 80 for n in range(10)}}} for index in range(240)]
        report["sections"][3] = section("storage", "磁盘与阵列", [disk, *members], "部分字段未读取")
        payload = encode(report)
        self.assertLessEqual(len(payload), 512 * 1024)
        result = json.loads(payload)["sections"]
        self.assertIn("大小上限", result[0]["items"][1]["fields"].get(MISSING + "近期事件", ""))
        self.assertIn(disk, result[3]["items"])
        self.assertNotIn("截断", result[3]["reason"])
        self.assertTrue(any(part["fields"].get(MISSING + "附加部件") for part in result[3]["items"]))

    def test_storage_item_limit_keeps_exactly_256_visible_disks(self):
        controller = {"name": "存储控制器 0000:01:00.0", "fields": {"来源": "Linux"}}
        for count in (255, 256):
            with self.subTest(disks=count):
                disks = [{"name": f"sd{index}", "fields": {"类型": "磁盘", "容量（字节）": "1024"}}
                         for index in range(count)]
                storage = section("storage", "磁盘", [controller, *disks, controller])
                self.assertEqual([part["name"] for part in storage["items"] if part["fields"].get("类型") == "磁盘"],
                                 [disk["name"] for disk in disks])
                self.assertNotIn("截断", storage["reason"])
                self.assertTrue(any(MISSING + "附加部件" in part["fields"] for part in storage["items"]))

    def test_storage_counts_only_disks_toward_the_install_disk_limit(self):
        self.outputs[("lsblk", "--json", "--bytes", "--nodeps", "--output",
                      "NAME,TYPE,SIZE,MODEL,SERIAL,TRAN,ROTA,REV,HCTL")] = json.dumps({
            "blockdevices": [{"name": "sr0", "type": "rom"}] + [
                {"name": f"nvme{index}n1", "type": "disk", "size": 1024} for index in range(256)
            ],
        })
        storage = Collector(self.root, "", self.command).storage()
        self.assertEqual(len(storage["items"]), 256)
        self.assertEqual(storage["items"][-1]["name"], "nvme255n1")
        self.assertNotIn("截断", storage["reason"])

    def test_virtio_port_reports_its_pci_card_only_once(self):
        pci = "sys/bus/pci/devices"
        address = "0000:00:02.0"
        unused = "0000:00:03.0"
        self.file(f"{pci}/{address}/class", "0x020000\n")
        self.file(f"{pci}/{unused}/class", "0x020000\n")
        child = self.root / pci / address / "virtio0"
        child.mkdir()
        self.file("sys/class/net/eth0/address", "52:54:00:12:34:56\n")
        try:
            (self.root / "sys/class/net/eth0/device").symlink_to(child, target_is_directory=True)
        except OSError as err:
            if getattr(err, "winerror", None) == 1314:
                self.skipTest("当前 Windows 账号不能创建模拟网口所需的符号链接")
            raise
        self.outputs[("lspci", "-Dnn")] = f"{address} Virtio network device\n"
        self.outputs[("ethtool", "-i", "eth0")] = "driver: virtio_net\n"

        ports = self.report()["network"]["items"]
        self.assertEqual([p["name"] for p in ports], ["eth0", f"未启用网卡 {unused}"])
        self.assertEqual(ports[0]["fields"]["插槽地址"], address)
        self.assertEqual(ports[0]["fields"]["设备"], "Virtio network device")
        self.assertEqual(ports[0]["fields"]["驱动"], "virtio_net")
        self.assertEqual(ports[0]["fields"]["启动网口"], "是")

    def test_reads_raid_members_and_volume_groups_from_local_tool(self):
        root = self.root
        self.file("sys/bus/pci/devices/raid/class", "0x010400\n")
        controller = {"Command Status": {"Controller": 0, "Status": "Success"}}
        self.outputs[("storcli", "/call", "show", "all", "J")] = json.dumps({
            "Controllers": [{**controller, "Response Data": {
                "Basics": {"Model": "Controller A"}, "Version": {"FW Package Build": "7"},
                "HwCfg": {"On Board Memory Size": "2048 MB"},
                "Cachevault_Info": [{"State": "Optimal"}],
                "VD LIST": [{"DG/VD": "1/0", "TYPE": "RAID1", "State": "Optl"}],
            }}],
        })
        self.outputs[("storcli", "/call/eall/sall", "show", "all", "J")] = json.dumps({
            "Controllers": [{**controller, "Response Data": {
                "Drive /c0/e1/s2": [{"EID:Slt": "1:2", "DG": 1, "State": "Rbld"}],
                "Drive /c0/e1/s3": [{"EID:Slt": "1:3", "DG": "-", "State": "GHS"}],
                "Drive /c0/e1/s2 - Detailed Information": {
                    "Drive /c0/e1/s2 Device attributes": {"SN": "disk-2", "Firmware Revision": "8"},
                },
            }}],
        })
        self.outputs[("storcli", "/call/eall/sall", "show", "rebuild", "J")] = json.dumps({
            "Controllers": [{**controller, "Response Data": {"Drive /c0/e1/s2": [
                {"Drive-ID": "/c0/e1/s2", "Progress%": 0, "Status": "In progress"}]}}]})
        items = {entry["name"]: entry["fields"] for entry in Collector(root, "", self.command).storage()["items"]}
        self.assertEqual(items["阵列控制器 0"]["缓存容量"], "2048 MB")
        self.assertIn("Optimal", items["阵列控制器 0"]["缓存保护状态"])
        self.assertEqual(items["物理盘 0/1:2"]["重建进度"], "0%")
        self.assertEqual(items["物理盘 0/1:3"]["热备角色"], "全局热备")
        self.assertEqual(items["逻辑盘 0/1/0"]["阵列级别"], "RAID1")
        self.assertEqual(items["物理盘 0/1:2"]["序列号"], "disk-2")
        self.assertEqual(items["物理盘 0/1:2"]["阵列组"], "1")

    def test_bound_report_marks_truncated_category_as_partial(self):
        entries = [{"name": "disk", "fields": {f"字段{index}": "读" * 240 for index in range(10)}}] * 250
        report = {"sections": [section("storage", "磁盘", entries)]}
        result = json.loads(encode(report))
        self.assertIn("截断", result["sections"][0]["reason"])
        self.assertEqual(result["sections"][0]["status"], "partial")

    @patch("collect.subprocess.run")
    def test_timed_out_query_remains_unknown(self, run):
        run.side_effect = subprocess.TimeoutExpired("test", 1)
        self.assertEqual(command("test", timeout=1), "")

    @patch("collect.subprocess.run")
    def test_retry_keeps_token_out_of_arguments_and_resends_same_bytes(self, run):
        run.side_effect = [subprocess.CompletedProcess([], 1, "000"),
                           subprocess.CompletedProcess([], 0, "204")]
        with tempfile.TemporaryDirectory() as work, patch("collect.time.sleep"):
            result = uploaded("https://gateway/report", "secret-token", b"same-data", Path(work))
        self.assertTrue(result)
        self.assertEqual(run.call_count, 2)
        for call in run.call_args_list:
            self.assertNotIn("secret-token", str(call.args))

    def test_firmware_prune_keeps_compressed_matches_and_symlink_targets(self):
        self.file("intel/ice/real.pkg.zst", "match")
        self.file("intel/wifi.pkg.zst", "unrelated")
        self.file("rtl_nic/rtl8168-a.fw.zst", "match")
        firmware = Firmware(self.root)
        if os.name != "nt":
            (self.root / "intel/ice/linked.pkg.zst").symlink_to("real.pkg.zst")
            firmware.match("intel/ice/linked.pkg", required=True)
        else:
            firmware.match("intel/ice/real.pkg", required=True)
        firmware.match("rtl_nic/rtl8168-*.fw", required=True)
        firmware.prune()
        self.assertFalse((self.root / "intel/wifi.pkg.zst").exists())
        self.assertTrue((self.root / "intel/ice/real.pkg.zst").is_file())
        self.assertTrue((self.root / "rtl_nic/rtl8168-a.fw.zst").is_file())
        with self.assertRaises(ValueError):
            firmware.match("missing.pkg", required=True)


if __name__ == "__main__":
    unittest.main()
