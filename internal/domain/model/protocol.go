package model

// SignalPhase 是机器侧上报的阶段名。它会进 HTTP 路径、事件键和数据库，发布后不能改名。
//
// 这些词原来在领域执行、渲染、应用执行和 HTTP 入口各写一遍；现在只在这里定义一次，
// 各层都引用同一份，拼错时编译期就能发现。
type SignalPhase string

const (
	PhaseBooted     SignalPhase = "booted"      // 新系统已启动
	PhaseInstalled  SignalPhase = "installed"   // 安装器已完成，准备重启
	PhaseInitStart  SignalPhase = "init-start"  // 首次开机脚本开始
	PhaseInitDone   SignalPhase = "init-done"   // 首次开机脚本完成
	PhaseReady      SignalPhase = "ready"       // 任务完成
	PhaseSlotStart  SignalPhase = "slot-start"  // 执行插槽开始
	PhaseSlotEnd    SignalPhase = "slot-end"    // 执行插槽结束
	PhaseHookStart  SignalPhase = "hook-start"  // 插槽里的一个脚本开始
	PhaseHookReport SignalPhase = "hook-report" // 插槽里的一个脚本报结果
)

// Stage 是执行记录里的阶段名，只用于日志、诊断和超时预算。它也会存进数据库，发布后不能改名。
type Stage string

const (
	StageBooting      Stage = "booting"      // 等安装器引导
	StagePre          Stage = "pre"          // 装前脚本
	StagePackages     Stage = "packages"     // 安装软件包
	StagePost         Stage = "post"         // 装后脚本
	StageRebooting    Stage = "rebooting"    // 重启进新系统
	StageInitializing Stage = "initializing" // 首次开机初始化
)
