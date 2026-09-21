// 本文件实现 RBAC：五个角色到权限码的映射、认证中间件与授权中间件。
// 对应实现计划任务 1，依据 SPEC §9.4、§12.5。
//
// 没有 arm 相关的权限码——armed 状态已删除（§3.2）。
package console

import (
	"context"
	"net/http"

	"github.com/lober/lober/internal/apierr"
)

// 五个角色，取值与 SPEC §9.4、§12.5 的角色表逐一对应。config.User.Roles
// 是自由字符串（config 包不校验角色名，见 config.go 的说明），本模块是
// 角色码的权威定义处。
const (
	roleViewer       = "viewer"
	roleOperator     = "operator"
	roleHookAuthor   = "hook-author"
	roleHookApprover = "hook-approver"
	roleAdmin        = "admin"
)

// 七个权限码，取值与 SPEC §9.4、§12.5、http-console.md「GET /auth/codes」
// 逐一对应。
const (
	codeTaskCreate     = "task:create"
	codeTaskRetry      = "task:retry"
	codeTaskCancel     = "task:cancel"
	codeHookWrite      = "hook:write"
	codeHookApprove    = "hook:approve"
	codeProviderWrite  = "provider:write"
	codeArtifactImport = "artifact:import"
)

// roleCodes 是角色到权限码集合的映射（SPEC §9.4、§12.5 表）。
//
// 建任务的权限不是「只是填个表」（§9.4）：这一个动作就能让目标机器下次
// 网络启动时被擦盘，operator 拿到 task:create/retry/cancel 三个码正是按
// 这个理解分配的，不是随手给的默认权限。
//
// 写 hook 是独立权限，与建任务分开：hook 以 root 身份在分区之前运行，
// 能力等同「擦掉这台机器所有盘」，因此 hook-author 与 operator 是两个
// 互不重叠的角色，不能互相代替。
var roleCodes = map[string][]string{
	roleViewer:       {}, // 只读，无写权限码
	roleOperator:     {codeTaskCreate, codeTaskRetry, codeTaskCancel},
	roleHookAuthor:   {codeHookWrite},
	roleHookApprover: {codeHookApprove},
	// admin 是 provider 配置与制品导入的持有者，且按 SPEC §12.5「以上全部」
	// 的要求聚合其余四个角色的全部权限码——admin 登录后 UI 不应因为缺一个
	// 码而看不到某个按钮。
	roleAdmin: {
		codeProviderWrite, codeArtifactImport,
		codeTaskCreate, codeTaskRetry, codeTaskCancel,
		codeHookWrite, codeHookApprove,
	},
}

// codesForRoles 把一个用户的角色列表展开成去重后的权限码集合。角色名不在
// roleCodes 表中时静默跳过（不认识的角色贡献零权限，而不是报错——配置里
// 允许出现尚未被本模块承认的角色字符串，这不是致命错误）。
func codesForRoles(roles []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, r := range roles {
		for _, c := range roleCodes[r] {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	// 保证返回值不是 nil——JSON 序列化 nil 切片得到 null，而契约要求
	// `string[]`（哪怕是空数组）；viewer 用户调 /auth/codes 应该拿到 []
	// 而不是 null。
	if out == nil {
		out = []string{}
	}
	return out
}

// hasCode 判定权限码集合里是否含有目标码。
func hasCode(codes []string, target string) bool {
	for _, c := range codes {
		if c == target {
			return true
		}
	}
	return false
}

// ctxKeyUser 是把已认证用户身份放进 context 的键类型。用私有类型而非
// 字符串常量做 key，避免与其它包的 context 键发生意外碰撞。
type ctxKeyUser struct{}

// userFromContext 取出当前请求已认证的用户身份；ok 为 false 表示认证
// 中间件未曾运行或未认证——调用方（RBAC 中间件、handler）应据此拒绝而非
// panic。
func userFromContext(ctx context.Context) (sessionUser, bool) {
	u, ok := ctx.Value(ctxKeyUser{}).(sessionUser)
	return u, ok
}

// requireAuth 是认证中间件：校验 Authorization 头里的 token，无 token 或
// 无效/过期 token 一律回未认证（HTTP 401，apierr.CodeUnauthenticated）。
//
// 认证失败与授权失败必须用不同的错误码（计划任务 1 明确要求）：前者说明
// 「该重新登录」，后者说明「登录了但没权限」，UI 的处置不同（前者跳转登录
// 页，后者提示联系管理员）。
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		user, ok := s.sessions.lookup(token, s.clock.Now())
		if !ok {
			apierr.WriteError(w, apierr.New(http.StatusUnauthorized, apierr.CodeUnauthenticated,
				"未登录或登录状态已过期，请重新登录"))
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyUser{}, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// bearerToken 从 Authorization 头取出 Bearer token。头部缺失、格式不是
// "Bearer <token>"、或 token 部分为空，统一返回空串（调用方 sessions.lookup
// 对空串必然返回未命中，走向同一条「未认证」路径，不需要在这里单独报告
// 格式错误——机器面与人面都不应该把「头格式错」和「token 无效」区分给
// 客户端，那没有实际用处还多一种响应形态）。
func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) <= len(prefix) || h[:len(prefix)] != prefix {
		return ""
	}
	return h[len(prefix):]
}

// hasRole 判定用户的角色列表里是否含有目标角色。
func hasRole(roles []string, target string) bool {
	for _, r := range roles {
		if r == target {
			return true
		}
	}
	return false
}

// requireRole 是授权中间件工厂：按角色名而非权限码卡门。少数端点的权限
// 表述是「某个角色才能做」而不是「持有某个权限码」——模板的创建/更新在
// SPEC §8.2/http-console.md 里直接写的是 admin 角色，§9.4 的七个权限码
// 表里并不存在一个 template:write 码，因此这类端点用角色名判断，不强行
// 发明一个权限码只为了让所有端点都走 requireCode 这一条路径。
func (s *Server) requireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := userFromContext(r.Context())
			if !ok {
				apierr.WriteError(w, apierr.New(http.StatusUnauthorized, apierr.CodeUnauthenticated,
					"未登录或登录状态已过期，请重新登录"))
				return
			}
			if !hasRole(user.Roles, role) {
				apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeForbidden,
					"当前账号没有执行该操作所需的权限"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireCode 是授权中间件工厂：返回一个中间件，要求当前用户（必须已经过
// requireAuth）的权限码集合包含 code，否则回 403（apierr.CodeForbidden）。
//
// 必须放在 requireAuth 之后使用——本函数假设 context 里已经有用户身份，
// 取不到时视为编程错误（中间件顺序装反了），同样回 403 而非 panic，因为
// panic 会导致整个进程在一个请求处理上崩掉，这个代价与「装反了中间件顺序」
// 这种本该在开发期用测试发现的错误不成比例。
func (s *Server) requireCode(code string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := userFromContext(r.Context())
			if !ok {
				apierr.WriteError(w, apierr.New(http.StatusUnauthorized, apierr.CodeUnauthenticated,
					"未登录或登录状态已过期，请重新登录"))
				return
			}
			codes := codesForRoles(user.Roles)
			if !hasCode(codes, code) {
				apierr.WriteError(w, apierr.New(http.StatusOK, apierr.CodeForbidden,
					"当前账号没有执行该操作所需的权限"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
