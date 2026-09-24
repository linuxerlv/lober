// 本文件是 apierr 包的 gin 适配层：OK / Fail / Abort 三个函数把已有的
// sentinel 映射与 *Error 判定逻辑接到 gin.Context 上，供 42 个 handler
// 使用。三者都不重写 apierr.go 里的错误码映射表，只是换一层响应写出的
// 载体（gin.Context 而非 http.ResponseWriter），保证映射规则永远只有
// 一份真源。
//
// Fail 比标准库版本 WriteError 多做一件事：识别
// validator.ValidationErrors 并翻译成可展示中文（见 gin 迁移契约
// docs/superpowers/specs/2026-09-24-gin-migration-contract.md §3）。
package apierr

import (
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
)

// init 把 gin binding 用的全局 validator 引擎的字段命名规则改成读
// json 标签，而不是默认的 Go 字段名。这是 Fail 翻译
// validator.ValidationErrors 时能拿到 json 字段名的前提条件——
// go-playground/validator 的 FieldError.Field() 文档明确写着
// "RegisterTagNameFunc 必须先注册才能拿到 tag 值"，不注册就只能拿到
// Go 字段名（如 RequiredField），而用户看到的、也应该在错误提示里看到
// 的是 JSON 字段名（如 required_field）。
//
// 只注册命名函数，不碰 gin.New/gin.Default 的选择、不碰 gin.SetMode——
// 那部分是骨架 agent 的职责（契约 §5），这里只动 validator 引擎本身的
// 一个可选配置项，与骨架配置是两件不冲突的事。
func init() {
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		v.RegisterTagNameFunc(func(fld reflect.StructField) string {
			name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
			if name == "-" {
				return ""
			}
			return name
		})
	}
}

// OK 是 WriteOK 的 gin 版本：HTTP 200，Envelope.Code=CodeOK，Message
// 空串，Data 是调用方传入的业务数据。字段语义与 WriteOK 完全一致，
// 只是换成 gin.Context 写出。
func OK(c *gin.Context, data any) {
	c.JSON(200, Envelope{Code: CodeOK, Data: data, Message: ""})
}

// Fail 是 WriteError 的 gin 版本，判定顺序在 WriteError 原有三层之前
// 多插入一层 validator.ValidationErrors 识别：
//
//  1. err 是 validator.ValidationErrors：翻译成中文可展示文案，422 +
//     CodeValidationFailed。
//  2. err 是 *Error（handler 主动构造）：按其携带的状态码/业务码/文案
//     原样输出。
//  3. err 命中已知 sentinel（可能经过包裹）：按 lookupSentinel 的映射
//     输出。
//  4. 其余情况——未识别错误：完整信息只记入 log/slog，响应体只给
//     CodeInternal + 通用文案，绝不把 err.Error() 原文写进响应体
//     （契约 §2 的安全边界，与 WriteError 完全一致）。
//
// 第 2、3、4 层的判定与文案直接复用 apierr.go 里 errors.As / lookupSentinel
// 与对应的映射表，不重新实现一遍，保证两条响应路径（标准库版、gin 版）
// 的判定结果永远一致。
func Fail(c *gin.Context, err error) {
	httpStatus, env := resolveFailure(err)
	c.JSON(httpStatus, env)
}

// Abort 与 Fail 写出完全相同的响应体，额外调用 c.Abort()，供中间件
// （如 requireAuth、requireCode）拒绝请求时使用：调用方需要的不只是
// 写出错误响应，还要阻止 gin 继续执行链上后续的 handler。
func Abort(c *gin.Context, err error) {
	httpStatus, env := resolveFailure(err)
	c.AbortWithStatusJSON(httpStatus, env)
}

// resolveFailure 是 Fail 与 Abort 共用的判定与取值逻辑，保证两者永远
// 给出一致的响应体，不会出现「Fail 认得这个错误但 Abort 不认得」的分裂。
func resolveFailure(err error) (httpStatus int, env Envelope) {
	if err == nil {
		return 500, Envelope{
			Code:    CodeInternal,
			Data:    nil,
			Message: "服务器内部错误，请联系管理员并提供操作时间以便查询日志",
		}
	}

	var validationErrs validator.ValidationErrors
	if errors.As(err, &validationErrs) {
		return 422, Envelope{
			Code:    CodeValidationFailed,
			Data:    nil,
			Message: translateValidationErrors(validationErrs),
		}
	}

	var businessErr *Error
	if errors.As(err, &businessErr) {
		return businessErr.HTTPStatus, Envelope{
			Code:    businessErr.Code,
			Data:    nil,
			Message: businessErr.Message,
		}
	}

	if mapping, ok := lookupSentinel(err); ok {
		return mapping.httpStatus, Envelope{
			Code:    mapping.code,
			Data:    nil,
			Message: mapping.message,
		}
	}

	// 未识别错误：细节只进日志，不进响应体——与 WriteError 相同的安全
	// 边界（apierr.go「本包最要紧的一条安全边界」）。
	slog.Default().Error("未识别的内部错误", slog.Any("error", err))
	return 500, Envelope{
		Code:    CodeInternal,
		Data:    nil,
		Message: "服务器内部错误，请联系管理员并提供操作时间以便查询日志",
	}
}

// validationTagMessages 把 go-playground/validator 的 tag 名映射到一段
// 不含字段名的中文短语模板；translateValidationErrors 再给每条短语前面
// 拼上字段名，拼成完整一句。只覆盖任务要求的八个 tag：required、min、
// max、oneof、email、ip、mac、hostname——标签表达不了的校验条件本来就
// 不会以 validator.ValidationErrors 的形式到达这里（它们留在 handler
// 里用 Go 代码判断，走的是 sentinel 或 *Error 路径），所以这张表不需要
// 覆盖 validator 支持的全部 tag。
var validationTagMessages = map[string]func(fe validator.FieldError) string{
	"required": func(fe validator.FieldError) string {
		return "不能为空"
	},
	"min": func(fe validator.FieldError) string {
		return "不能小于 " + fe.Param()
	},
	"max": func(fe validator.FieldError) string {
		return "不能大于 " + fe.Param()
	},
	"oneof": func(fe validator.FieldError) string {
		return "取值必须是以下之一：" + fe.Param()
	},
	"email": func(fe validator.FieldError) string {
		return "不是合法的邮箱地址"
	},
	"ip": func(fe validator.FieldError) string {
		return "不是合法的 IP 地址"
	},
	"mac": func(fe validator.FieldError) string {
		return "不是合法的 MAC 地址"
	},
	"hostname": func(fe validator.FieldError) string {
		return "不是合法的主机名"
	},
}

// translateValidationErrors 把 validator.ValidationErrors 翻译成一句
// 可直接展示给用户的中文，每个字段一条、用「；」分隔，每条都带上是
// 哪个字段（用 Field() 拿到的 json 标签名，见本文件 init 注册的
// RegisterTagNameFunc）。
//
// 未覆盖的 tag（理论上不应该出现，因为任务范围内的手写标签只用这八个）
// 退化为「校验失败」这一通用短语而不是 panic 或吞掉这条错误——防御性
// 兜底，避免将来有人给结构体加了标签表没覆盖的 tag 时，用户看到的是
// 空白响应或程序崩溃。
func translateValidationErrors(ve validator.ValidationErrors) string {
	parts := make([]string, 0, len(ve))
	for _, fe := range ve {
		field := fe.Field()
		phrase, ok := validationTagMessages[fe.Tag()]
		var reason string
		if ok {
			reason = phrase(fe)
		} else {
			reason = "校验失败"
		}
		parts = append(parts, fmt.Sprintf("%s：%s", field, reason))
	}
	return strings.Join(parts, "；")
}
