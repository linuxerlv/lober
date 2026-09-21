// 本文件实现 contract.TokenIssuer：签发不可预测的随机字符串。对应实现
// 计划任务 2，依据 SPEC §0.1、§1.5、§4.4、§9.1。
//
// token 是删掉 armed 之后唯一的执行许可。MVP 无 TLS，所以它与「默认拒绝」
// 是唯一的防线，不能降级实现——必须用 crypto/rand，不能用 math/rand、
// 不能从时间戳或任务 id 派生。
package statemachine

import (
	"context"
	"crypto/rand"
	"encoding/base64"
)

// tokenRandomBytes 是每次签发 token 时读取的随机字节数（32 字节 = 256
// 位），符合计划任务 2 明确要求的强度。
const tokenRandomBytes = 32

// IssueToken 生成一个不可预测的随机字符串，base64url 编码、不带填充。
//
// 两种用途（装机 token 与 finalize token）用同一套生成逻辑，用途参数
// 当前不影响生成方式——保留调用方区分用途的能力，是为了将来可能需要
// 区分（例如给 finalize token 加前缀便于日志识别），本方法本身不需要
// 知道用途。
//
// base64url 而非标准 base64：token 走 URL 路径段（SPEC §4.4），标准
// base64 的 `+` 与 `/` 在路径里需要转义，用 URL 安全的字母表可以直接嵌入。
func (m *Machine) IssueToken(ctx context.Context) (string, error) {
	buf := make([]byte, tokenRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
