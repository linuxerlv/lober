// Package model 是 Lober 的领域类型集合：纯数据，无行为逻辑，无项目内依赖。
//
// 契约约束：本包不得 import 任何 github.com/lober/lober/... 包。
// 它是依赖图的根，所有模块都可以安全引用它 —— 一旦它引用了别的包，
// 那个包就会污染全部模块。internal/archtest 在 CI 里强制这条。
package model
