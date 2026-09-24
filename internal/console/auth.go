// 本文件实现四个认证端点：登录、取用户信息、取权限码、登出。对应实现
// 计划任务 1，依据 docs/contracts/http-console.md §1、SPEC §12.5；gin 迁移
// 见 docs/superpowers/specs/2026-09-24-gin-migration-contract.md §4/§7。
package console

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"golang.org/x/crypto/bcrypt"

	"github.com/lober/lober/internal/apierr"
)

// loginRequest 是 POST /auth/login 的入参。用户名与密码非空这条校验原本是
// 0 个手写检查点（迁移契约 §7 的表：auth.go 手写校验点数为 0），迁移前的
// handler 完全没做这一步——空用户名/空密码会直接进 bcrypt 比对、大概率查不到
// 用户或比对失败，最终仍落到「用户名或密码错误」这一条统一响应，只是绕了
// 一圈才到达同一个结果。改用 binding:"required" 标签把这条检查提到传输层，
// 效果不变（空值依然不可能登录成功）但失败原因更明确（422 校验失败而不是
// 401 认证失败），且省掉一次无意义的 bcrypt 比对。
type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// loginResponse 是登录成功的返回体。字段名 accessToken 与契约逐字对应
// （vben 默认拦截器 + 计划要求「端点名与框架默认约定对齐，不要改名」）。
type loginResponse struct {
	AccessToken string `json:"accessToken"`
}

// handleLogin 处理 POST /auth/login。
//
// 失败响应不区分「用户不存在」与「密码错误」（都回同一个错误）——避免
// 帮攻击者做用户名枚举，这是计划任务 1 明确要求锁住的行为，本次迁移不改
// 这条判断逻辑，只换传输层写法。
//
// 本文件不属于骨架 agent 的四个法定交付文件，也不是六个资源域文件之一
// ——契约 §4/§5 冻结的四个认证端点直接在 routes()（骨架 agent 拥有）里
// 注册，没有归到任何一个 routesXxx()，因此没有资源域 agent 会碰它。
//
// 改用 c.ShouldBindJSON 而非手写 json.NewDecoder：binding:"required" 标签
// （见 loginRequest）让 gin 在进入本函数体之前就完成「JSON 语法是否合法」
// 与「用户名/密码是否非空」两项检查，ShouldBindJSON 把两者统一包装成
// error 返回，但两种失败原因必须映射到不同的响应，不能都丢给
// apierr.Fail 一把处理：
//
//   - 标签校验失败（如空用户名）：err 是 validator.ValidationErrors，
//     apierr.Fail 能识别，走 422+CodeValidationFailed 分支，直接转发即可。
//   - JSON 语法本身不合法（如请求体不是合法 JSON、类型不匹配）：err 是
//     *json.SyntaxError 一类，不是 validator.ValidationErrors、不是
//     *apierr.Error、也命中不了任何 sentinel，若直接转发给 apierr.Fail 会
//     落进「未识别错误」分支变成 500+CodeInternal——这会把迁移前
//     CodeBadRequest(1001) 的业务码悄悄改成 CodeInternal(1000)，属于响应体
//     内容变化，违反「不改业务语义」的约束。因此这一支仍需本地判断，构造
//     与迁移前完全一致的 *apierr.Error（CodeBadRequest），只是 HTTP 状态码
//     按契约 §2 冻结的表改为真实的 400（迁移前固定传 http.StatusOK，属于
//     契约 §0 指出的过期认知，与 rbac.go 把 403 改真是同一类修正）。
func (s *Server) handleLogin(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		var validationErrs validator.ValidationErrors
		if errors.As(err, &validationErrs) {
			apierr.Fail(c, err)
			return
		}
		apierr.Fail(c, apierr.New(http.StatusBadRequest, apierr.CodeBadRequest, "请求体不是合法的 JSON"))
		return
	}

	unauthorized := apierr.New(http.StatusUnauthorized, apierr.CodeUnauthenticated, "用户名或密码错误")

	var matched *AuthUser
	for i := range s.deps.Users {
		if s.deps.Users[i].Name == req.Username {
			matched = &s.deps.Users[i]
			break
		}
	}
	if matched == nil {
		// 用户不存在时仍然跑一次 bcrypt 比对（对着一个固定的占位哈希），
		// 让「用户不存在」与「密码错误」两条路径耗时接近，缓解基于响应
		// 时间的用户名枚举（bcrypt 本身耗时是毫秒级，占位比对的额外成本
		// 可忽略）。
		_ = bcrypt.CompareHashAndPassword([]byte(placeholderBcryptHash), []byte(req.Password))
		apierr.Fail(c, unauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(matched.BcryptHash), []byte(req.Password)); err != nil {
		apierr.Fail(c, unauthorized)
		return
	}

	now := s.clock.Now()
	tok, err := s.sessions.create(sessionUser{
		Username: matched.Name,
		RealName: matched.Name,
		Roles:    matched.Roles,
	}, now+int64(s.sessionTTL.Seconds()))
	if err != nil {
		apierr.Fail(c, err)
		return
	}

	apierr.OK(c, loginResponse{AccessToken: tok})
}

// placeholderBcryptHash 是一个固定的合法 bcrypt 哈希（对应任意密码都不会
// 匹配的占位值），仅用于「用户不存在」路径的耗时掩护比对，不对应任何真实
// 账号。
const placeholderBcryptHash = "$2a$10$7EqJtq98hPqEX7fNZaFWoOhi6vjuLpk3wJ0Qe0KcJvJ8G7ZbtG9Ia"

// userInfoResponse 是 GET /user/info 的返回体，字段名与契约逐字对应。
type userInfoResponse struct {
	Username string   `json:"username"`
	RealName string   `json:"realName"`
	Roles    []string `json:"roles"`
}

// handleUserInfo 处理 GET /user/info（已登录即可，无需特定权限码）。
//
// 理论上 requireAuth 已经在本 handler 之前跑过，userFromContext 这里应该
// 总能取到值；仍然判空是防御性写法，与迁移前的行为一致，不因为「中间件
// 应该已经保证过」就省掉这层判断——两者不冲突时保留判断的代价接近零。
func (s *Server) handleUserInfo(c *gin.Context) {
	user, ok := userFromContext(c.Request.Context())
	if !ok {
		apierr.Fail(c, apierr.New(http.StatusUnauthorized, apierr.CodeUnauthenticated,
			"未登录或登录状态已过期，请重新登录"))
		return
	}
	apierr.OK(c, userInfoResponse{
		Username: user.Username,
		RealName: user.RealName,
		Roles:    user.Roles,
	})
}

// handleAuthCodes 处理 GET /auth/codes（已登录即可）。
func (s *Server) handleAuthCodes(c *gin.Context) {
	user, ok := userFromContext(c.Request.Context())
	if !ok {
		apierr.Fail(c, apierr.New(http.StatusUnauthorized, apierr.CodeUnauthenticated,
			"未登录或登录状态已过期，请重新登录"))
		return
	}
	apierr.OK(c, codesForRoles(user.Roles))
}

// handleLogout 处理 POST /auth/logout：使当前 token 立即失效。
func (s *Server) handleLogout(c *gin.Context) {
	s.sessions.revoke(bearerToken(c.Request))
	apierr.OK(c, nil)
}
