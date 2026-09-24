//go:build tools

// 本文件是依赖锚点，唯一作用是把 gin 钉成 go.mod 里的直接依赖。
//
// 背景：gin 是为 HTTP 层迁移预先引入的，迁移完成前仓库里还没有任何代码
// import 它。Go 的依赖图完全由 import 推导——没有 import 的模块会被
// go mod tidy 判定为不需要并从 go.mod 删除（即便手写了 require 行也一样，
// tidy 会连 require 一起清掉）。用空白 import 给它一个真实的引用点，
// tidy 才会保留。
//
// build tag 选 tools：这不是产品代码，不该参与 go build ./... 与 go vet 的
// 常规编译，也不该出现在任何二进制里。而 go mod tidy 会按「所有 build tag
// 均启用」的方式加载包，因此打了 tag 依然能被 tidy 看见——锚点生效、编译
// 面干净，两者兼得。
//
// gorm 的锚点已经移除：store 层迁移落地后，internal/store/postgres 下的
// 真实代码直接 import 了 gorm.io/gorm 与 gorm.io/driver/postgres，锚点
// 不再需要，留着会变成第二处依赖声明。
//
// 生命周期：临时的。HTTP 层迁移落地、真实代码 import 了 gin 之后，本文件
// 连同 internal/deps 目录应当一并删除。
package deps

import (
	_ "github.com/gin-gonic/gin"
)
