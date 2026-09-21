// 本文件实现 syslog UDP 接收器。对应实现计划任务 8，依据
// docs/contracts/http-machine.md UDP :514 一节、SPEC §6.5。
package gateway

// 任务 8 实现：接收函数接收源地址与原始字节，按源 IP 归档到对应任务，
// 匹配不到的行落一个公共文件。
