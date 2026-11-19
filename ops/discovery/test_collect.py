import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from collect import Collector, command, dmi_records, encode, section, uploaded
from firmware import Firmware


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
                "VD LIST": [{"DG/VD": "1/0", "TYPE": "RAID1", "State": "Optl"}],
            }}],
        })
        self.outputs[("storcli", "/call", "/eall", "/sall", "show", "all", "J")] = json.dumps({
            "Controllers": [{**controller, "Response Data": {
                "Drive /c0/e1/s2": [{"EID:Slt": "1:2", "DG": 1, "State": "Onln"}],
                "Drive /c0/e1/s2 - Detailed Information": {
                    "Drive /c0/e1/s2 Device attributes": {"SN": "disk-2", "Firmware Revision": "8"},
                },
            }}],
        })
        items = {entry["name"]: entry["fields"] for entry in Collector(root, "", self.command).storage()["items"]}
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
