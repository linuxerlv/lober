// 本文件实现四个认证端点：登录、取用户信息、取权限码、登出。对应实现
// 计划任务 1，依据 docs/contracts/http-console.md §1、SPEC §12.5。
package console

import (
	"encoding/json"
	"net/http"

	"golang.org/x/crypto/bcrypt"

	"github.com/lober/lober/internal/apierr"
)

// loginRequest 是 POST /auth/login 的入参。
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// loginResponse 是登录成功的返回体。字段名 accessToken 与契约逐字对应
// （vben 默认拦截器 + 计划要求「端点名与框架默认约定对齐，不要改名」）。
type loginResponse struct {
	AccessToken string `json:"accessToken"`
}

// handleLogin 处理 POST /auth/login。
//
// 失败响应不区分「用户不存在」与「密码错误」（都回同一个错误）——避免
// 帮攻击者做用户名枚举，这是计划任务 1 明确要求锁住的行为。
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeBadRequest, "请求体不是合法的 JSON"))
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
		apierr.WriteError(w, unauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(matched.BcryptHash), []byte(req.Password)); err != nil {
		apierr.WriteError(w, unauthorized)
		return
	}

	now := s.clock.Now()
	tok, err := s.sessions.create(sessionUser{
		Username: matched.Name,
		RealName: matched.Name,
		Roles:    matched.Roles,
	}, now+int64(s.sessionTTL.Seconds()))
	if err != nil {
		writeInternal(w, err)
		return
	}

	apierr.WriteOK(w, loginResponse{AccessToken: tok})
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
func (s *Server) handleUserInfo(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		apierr.WriteError(w, apierr.New(http.StatusUnauthorized, apierr.CodeUnauthenticated,
			"未登录或登录状态已过期，请重新登录"))
		return
	}
	apierr.WriteOK(w, userInfoResponse{
		Username: user.Username,
		RealName: user.RealName,
		Roles:    user.Roles,
	})
}

// handleAuthCodes 处理 GET /auth/codes（已登录即可）。
func (s *Server) handleAuthCodes(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		apierr.WriteError(w, apierr.New(http.StatusUnauthorized, apierr.CodeUnauthenticated,
			"未登录或登录状态已过期，请重新登录"))
		return
	}
	apierr.WriteOK(w, codesForRoles(user.Roles))
}

// handleLogout 处理 POST /auth/logout：使当前 token 立即失效。
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.sessions.revoke(bearerToken(r))
	apierr.WriteOK(w, nil)
}
