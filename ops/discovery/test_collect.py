import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from collect import Collector, command, dmi_records, encode, section, uploaded
from firmware import Firmware
from hardware import MISSING, port_fields, secure_boot


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
