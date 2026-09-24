// Package apierr 实现人面（控制台）响应包裹与错误码映射。对应实现计划
// 任务 14，依据 SPEC §8.2、§12.5；gin 适配层见 gin.go，依据
// docs/superpowers/specs/2026-09-24-gin-migration-contract.md。
//
// 本包存在的理由：HTTP 状态码与 Envelope.Code 是两个消费者各取所需，
// 不是冗余字段。状态码给 curl、网关、监控、gin 的日志中间件识别粗粒度
// 的成功/失败；Envelope.Code 给前端区分同一状态码下的不同业务分支
// （例如 409 下 1005/1006/1007/1008 是四种不同的 UI 提示）。删掉任何
// 一边都会丢信息，这是两个字段必须并存的原因。
//
// 前端曾经只读 Envelope.Code、完全忽略 HTTP 状态码（vben 上游的
// defaultResponseInterceptor 按 codeField:'code'/successCode:0 解包），
// 这一度让状态码显得多余。迁移后前端改用自写拦截器：HTTP 2xx 判成功，
// 非 2xx 再读 body.code 细分同状态码下的具体原因——状态码与 code 从此
// 各司其职，不再是「为了迁就 vben 才双写一份」的冗余设计。
//
// 机器面不适用本包：那边是 iPXE 与 busybox curl，响应保持纯文本裸响应
// （SPEC §6.2），不走本包的 Envelope。
package apierr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/lober/lober/internal/core/contract"
)

// Code 是业务错误码，只给程序判断用；message 才是给人看的文案。
type Code int

// 十个错误码，与计划任务 14 逐一对应。0 是唯一的成功码，其余九个都是
// 失败码——这条划分本身就是 vben 拦截器 successCode:0 的直接要求。
const (
	CodeOK               Code = 0    // 成功
	CodeInternal         Code = 1000 // 内部错误：未识别的错误统一落在这里
	CodeBadRequest       Code = 1001 // 请求错误：参数格式或语义不合法
	CodeUnauthenticated  Code = 1002 // 未认证
	CodeForbidden        Code = 1003 // 无权限
	CodeNotFound         Code = 1004 // 不存在
	CodeConflict         Code = 1005 // 状态冲突（CAS 失败）
	CodeActiveTaskExists Code = 1006 // 该主机已有进行中任务
	CodeStillReferenced  Code = 1007 // 仍被引用，不能删除
	CodeReinstallDenied  Code = 1008 // 拒绝重装
	CodeValidationFailed Code = 1009 // 校验失败
)

// Envelope 是人面 API 统一的响应体形态（SPEC §8.2、§12.5）：
// {"code":0,"data":...,"message":""}。字段名与大小写由 vben 拦截器的
// codeField/dataField 硬编码决定，不能按 Go 习惯改成别的名字。
type Envelope struct {
	Code    Code   `json:"code"`
	Data    any    `json:"data"`
	Message string `json:"message"`
}

// Error 是供 handler 主动构造的「业务错误」类型：携带 HTTP 状态码、
// 业务码与面向运维的文案。它与下面的 sentinel 映射走同一个输出函数
// WriteError，handler 不需要关心响应体是怎么序列化的（计划任务 14
// 「设计要点」第三条）。
type Error struct {
	HTTPStatus int
	Code       Code
	Message    string
}

// New 构造一个 *Error，供 handler 在 sentinel 映射覆盖不到的场景下
// 主动抛出（例如请求参数校验失败、鉴权失败等 handler 自己发现的错误）。
func New(httpStatus int, code Code, message string) *Error {
	return &Error{HTTPStatus: httpStatus, Code: code, Message: message}
}

// Error 实现标准 error 接口，内容包含 message 本身，便于服务端日志排查。
func (e *Error) Error() string {
	return fmt.Sprintf("apierr: code=%d http=%d message=%s", e.Code, e.HTTPStatus, e.Message)
}

// sentinelMapping 描述一个 sentinel error 映射到的 HTTP 状态码、业务码
// 与面向运维的中文文案。
type sentinelMapping struct {
	httpStatus int
	code       Code
	message    string
}

// sentinelMappings 是 sentinel error 到人面响应的映射表。
//
// 五项，对应计划任务 14 错误码表里 1004-1008 五个码，而不是逐字对应
// store.go 里那五个同名为「sentinel error」的变量——ErrDuplicateSeq 被
// ErrNotReinstallable 顶替，理由见 apierr_test.go 里
// TestWriteError_SentinelMapping 的注释（本任务返回值 contractDeviations
// 里也记了一份）。
//
// 用一个切片而非 map[error]sentinelMapping：error 值不能直接用作 map
// 键做可靠比较（wrap 之后的 errors.Is 语义要求逐一遍历判断，不能靠 == 或
// map 查找），所以查找函数必须线性扫描，用 errors.Is 逐一命中。
var sentinelMappings = []struct {
	sentinel error
	mapping  sentinelMapping
}{
	{
		sentinel: contract.ErrNotFound,
		mapping: sentinelMapping{
			httpStatus: http.StatusNotFound,
			code:       CodeNotFound,
			message:    "请求的对象不存在，可能已被删除或 ID 有误，请刷新页面后重试",
		},
	},
	{
		sentinel: contract.ErrConflict,
		mapping: sentinelMapping{
			httpStatus: http.StatusConflict,
			code:       CodeConflict,
			message:    "任务状态已被其他操作改变，请刷新页面查看最新状态后再重试本次操作",
		},
	},
	{
		sentinel: contract.ErrActiveTaskExists,
		mapping: sentinelMapping{
			httpStatus: http.StatusConflict,
			code:       CodeActiveTaskExists,
			message:    "该主机已存在一个未结束的任务，请先在任务列表中处理或取消该任务，再创建新任务",
		},
	},
	{
		sentinel: contract.ErrStillReferenced,
		mapping: sentinelMapping{
			httpStatus: http.StatusConflict,
			code:       CodeStillReferenced,
			message:    "该对象仍被启用中的模板引用，无法删除；请先停用引用它的模板，或改用停用而非删除",
		},
	},
	{
		sentinel: contract.ErrNotReinstallable,
		mapping: sentinelMapping{
			httpStatus: http.StatusConflict,
			code:       CodeReinstallDenied,
			message:    "该机器处于已完成安装但重启或初始化失败的状态：机器已完成安装，磁盘上可能已有数据，重装会擦除磁盘数据；如确需重装，请人工核实后从主机清单重新创建任务",
		},
	},
}

// lookupSentinel 用 errors.Is 逐一判定 err 是否命中已知的 sentinel
// error（可能经过任意层 fmt.Errorf %w 包裹）。命中则返回对应映射，
// ok=true；否则 ok=false，调用方应回退到通用的内部错误响应。
func lookupSentinel(err error) (sentinelMapping, bool) {
	for _, entry := range sentinelMappings {
		if errors.Is(err, entry.sentinel) {
			return entry.mapping, true
		}
	}
	return sentinelMapping{}, false
}

// WriteOK 写出成功响应：HTTP 200，Envelope.Code=CodeOK，Message 空串，
// Data 是调用方传入的业务数据。
func WriteOK(w http.ResponseWriter, data any) {
	writeEnvelope(w, http.StatusOK, Envelope{Code: CodeOK, Data: data, Message: ""})
}

// WriteError 是 sentinel 映射与 *Error 业务错误共用的唯一输出函数
// （计划任务 14「设计要点」第三条：两条路径走同一个输出函数，不是两套
// 响应写法）。判定顺序：
//
//  1. err 是 *Error（handler 主动构造）：直接按其携带的状态码/业务码/
//     文案输出，不做任何改写。
//  2. err 命中已知 sentinel（可能经过包裹）：按映射表输出。
//  3. 其余情况——未识别错误：完整错误信息只记入 log/slog，HTTP 响应体
//     只包含通用文案与 CodeInternal，绝不把 err.Error() 的原文写进
//     响应体。数据库错误里可能带连接串与用户名，这类细节一旦进了 HTTP
//     响应就相当于把内部拓扑暴露给了浏览器端（计划任务 14「设计要点」
//     第二条，也是本包最要紧的一条安全边界）。
func WriteError(w http.ResponseWriter, err error) {
	if err == nil {
		// 调用方误用（不应该用 nil error 调用本函数），仍要给出确定性的
		// 兜底响应，而不是让 nil 解引用panic 到更上层。
		writeEnvelope(w, http.StatusInternalServerError, Envelope{
			Code:    CodeInternal,
			Data:    nil,
			Message: "服务器内部错误，请联系管理员并提供操作时间以便查询日志",
		})
		return
	}

	var businessErr *Error
	if errors.As(err, &businessErr) {
		writeEnvelope(w, businessErr.HTTPStatus, Envelope{
			Code:    businessErr.Code,
			Data:    nil,
			Message: businessErr.Message,
		})
		return
	}

	if mapping, ok := lookupSentinel(err); ok {
		writeEnvelope(w, mapping.httpStatus, Envelope{
			Code:    mapping.code,
			Data:    nil,
			Message: mapping.message,
		})
		return
	}

	// 未识别错误：细节只进日志，不进响应体。
	slog.Default().Error("未识别的内部错误", slog.Any("error", err))
	writeEnvelope(w, http.StatusInternalServerError, Envelope{
		Code:    CodeInternal,
		Data:    nil,
		Message: "服务器内部错误，请联系管理员并提供操作时间以便查询日志",
	})
}

// writeEnvelope 序列化并写出响应体，统一设置 Content-Type 与 HTTP
// 状态码。所有导出函数最终都汇合到这一处，保证 Content-Type 与序列化
// 方式不会在不同路径间产生差异。
func writeEnvelope(w http.ResponseWriter, httpStatus int, env Envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(httpStatus)
	if err := json.NewEncoder(w).Encode(env); err != nil {
		// 序列化本身失败（几乎不可能：Envelope 字段都是可序列化类型）时，
		// 响应头已经发出去，无法再改状态码，只能记日志。context.Background
		// 用于给 slog 一个显式的 ctx 参数位置，本函数不持有请求级 ctx。
		slog.ErrorContext(context.Background(), "写出响应体失败", slog.Any("error", err))
	}
}
