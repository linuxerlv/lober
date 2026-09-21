// 本文件实现客户端 IP 提取：只在直连 peer 是 loopback 时才信任 X-Real-IP
// 头部。对应实现计划任务 2，依据 SPEC §1.6、§9.2。
package gateway

// 任务 2 实现。
