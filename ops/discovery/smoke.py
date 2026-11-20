"""用无磁盘虚拟机验证 Alpine 镜像的驱动、上报和结果屏。"""

import argparse
import json
import shutil
import subprocess
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path


def smoke(image, direct=False, memory=768, registered=False, report_path=None):
    mac = "52:54:00:12:34:56"
    sections = {}
    checkins = []

    class Receiver(BaseHTTPRequestHandler):
        def do_GET(self):
            if self.path == "/boot.ipxe":
                checkins.append(self.headers.get("User-Agent", ""))
                base = f"http://10.0.2.2:{self.server.server_port}"
                boot = (
                    "#!ipxe\n"
                    f"kernel {base}/vmlinuz initrd=discovery.img {kernel_args}"
                    " lober.discovery_mac=${netX/mac:hexhyp}\n"
                    f"initrd --name discovery.img {base}/initramfs.gz\n"
                    "boot\n"
                ).encode()
                self.send_response(200)
                self.send_header("Content-Length", str(len(boot)))
                self.end_headers()
                self.wfile.write(boot)
                return
            if self.path not in {"/vmlinuz", "/initramfs.gz"}:
                self.send_error(404)
                return
            with (image / self.path[1:]).open("rb") as blob:
                self.send_response(200)
                self.send_header("Content-Length", str((image / self.path[1:]).stat().st_size))
                self.end_headers()
                shutil.copyfileobj(blob, self.wfile)

        def do_POST(self):
            target = "/v1/hardware/LOBER-DISCOVERY-QEMU" if registered else "/report"
            if self.path != target or self.headers.get("Authorization") != "Bearer smoke-only":
                self.send_error(403)
                return
            length = int(self.headers.get("Content-Length", "0"))
            if not 0 < length <= 512 * 1024:
                self.send_error(413)
                return
            payload = self.rfile.read(length)
            report = json.loads(payload)
            sections.update((section["key"], section) for section in report["sections"])
            if report_path:
                report_path.write_bytes(payload)
            print(f"报告 {length} 字节，启动至收到上报 {time.monotonic() - started:.1f} 秒", flush=True)
            self.send_response(204)
            self.end_headers()

    with HTTPServer(("127.0.0.1", 0), Receiver) as server, tempfile.TemporaryDirectory() as tmp:
        logs = Path(tmp) / "serial.log"
        base = f"http://10.0.2.2:{server.server_port}"
        endpoint = (f"lober.gateway={base} lober.sn=LOBER-DISCOVERY-QEMU lober.inventory_token=smoke-only"
                    if registered else f"lober.discovery_url={base}/report lober.discovery_token=smoke-only")
        kernel_args = f"console=ttyS0 quiet {endpoint}"
        options = ["-boot", "order=n"]
        network = f"user,id=discovery,bootfile={base}/boot.ipxe"
        if direct:
            options = [
                "-kernel", str(image / "vmlinuz"), "-initrd", str(image / "initramfs.gz"),
                "-append", f"{kernel_args} lober.discovery_mac={mac}",
            ]
        receiver = threading.Thread(target=server.serve_forever, daemon=True)
        started = time.monotonic()
        receiver.start()
        with subprocess.Popen([
            "qemu-system-x86_64", "-machine", "q35", "-m", str(memory), "-nodefaults",
            "-display", "none", "-serial", f"file:{logs}", "-no-reboot",
            "-netdev", network, "-device", f"virtio-net-pci,netdev=discovery,mac={mac},bootindex=1",
            "-smbios", "type=1,manufacturer=Lober,product=DiscoverySmoke,serial=LOBER-DISCOVERY-QEMU",
            *options,
        ], stdout=subprocess.DEVNULL) as vm:
            try:
                deadline = time.monotonic() + 120
                serial = ""
                while time.monotonic() < deadline and vm.poll() is None:
                    if logs.exists():
                        serial = logs.read_text(errors="replace")
                    if "waiting at this screen" in serial or "Kernel panic" in serial:
                        break
                    time.sleep(0.5)
                print(serial)
                assert "smoke-only" not in serial, "现场日志泄露上传凭据"
                assert direct or any("iPXE" in agent for agent in checkins), "未收到 iPXE 报到"
                assert "hardware report uploaded" in serial, "虚拟机未确认成功上报"
                assert "waiting at this screen" in serial and vm.poll() is None, "虚拟机未留在结果屏"
                assert len(sections) == 8, "硬件类别不全"
                assert sections["system"]["items"][0]["fields"]["采集格式"] == "2", "镜像没有新版字段"
                assert sections["system"]["items"][0]["fields"]["整机序列号"] == "LOBER-DISCOVERY-QEMU"
                assert any(part["fields"].get("硬件地址") == mac and part["fields"].get("启动网口") == "是"
                           for part in sections["network"]["items"]), "缺少识别到的启动网口"
                port = next(part["fields"] for part in sections["network"]["items"]
                            if part["fields"].get("启动网口") == "是")
                assert all(port.get(key, "").isdigit() for key in ("接收校验错误次数", "接收错误次数",
                                                                  "发送错误次数", "接收丢包数", "发送丢包数")), "网口计数未上报"
                chip = next(part["fields"] for part in sections["system"]["items"] if part["name"] == "管理芯片")
                assert "未读取·近期事件" in chip, "未标明虚拟机缺少管理芯片事件的原因"
                assert sections["storage"]["status"] in {"unread", "partial"}, "无磁盘应该显示未读取"
            finally:
                if vm.poll() is None:
                    vm.terminate()
                server.shutdown()
                receiver.join()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("image", type=Path)
    parser.add_argument("--direct", action="store_true", help="直接加载内核，单独排查 Alpine 镜像")
    parser.add_argument("--registered", action="store_true", help="验证已登记主机的上报入口")
    parser.add_argument("--memory", type=int, default=768, help="虚拟机内存，单位 MiB")
    parser.add_argument("--report", type=Path, help="保存实际收到的测试报告")
    args = parser.parse_args()
    smoke(args.image.resolve(), args.direct, args.memory, args.registered, args.report)
