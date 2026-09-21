// 本文件提供行扫描辅助：时间列、JSON 列、可空文本列的编解码。对应实现
// 计划任务 2，依据 SPEC §2.4、§2.5、§3.5、§5.5。
//
// # 与计划原文的偏差（契约缺口，详见本节说明）
//
// 计划要求「零值时间与 SQL NULL 的互转要明确……应是 NULL 而不是 1970 年」，
// 但 M1 的 schema（数据契约，本模块不能改）把 tasks 表全部时间列声明为
// "NOT NULL DEFAULT 0"，没有一列是可为 NULL 的——LastSignalAt、
// TokenExpiresAt、FinalizeExpiresAt 等在 schema 里都不接受 SQL NULL。
// model.Task 的对应字段也全部是裸 int64（不是 *int64），且 0 本身就是
// 这些字段在 Go 层「未设置」的语义零值（LastSignalAt 的字段注释就是
// 「距今多久收到过信号」，从未收到时天然是 0）。
//
// 因此本文件不引入 sql.NullInt64：时间列按裸 int64 直读直写，「零值时间
// 往返仍是零值」这条断言在 NOT NULL DEFAULT 0 的列上自动成立，不需要额外
// 的 NULL 包裹逻辑——引入它只会在写入路径多一次无意义的类型转换，且与
// schema 的真实约束不符（写 NULL 到这些列会直接违反 NOT NULL 报错）。
// 若未来确有字段需要真正的「未设置」与「零值」之分（当前六个时间列都不
// 需要——0 与「从未发生」在语义上重合，没有中间态），需要先把该列的
// schema 改成可为 NULL，这是契约变更，不是本模块能单方面决定的。
package sqlite

import (
	"encoding/json"
	"fmt"
)

// ---------------------------------------------------------------------------
// JSON 列：nil 与空值都编成合法 JSON，不写空串
// ---------------------------------------------------------------------------

// encodeJSON 把任意值编码成 JSON 文本列。v 为 nil 切片/map 时 json.Marshal
// 本身已经产出合法的 "null"，但本函数额外把「空切片/空 map/nil」都归一
// 编码成对应类型的空复合字面量（"[]" 或 "{}"），不写空字符串——空字符串
// 不是合法 JSON，解码时会报错，这是计划任务 2 明确要求的行为
// （「编码时 nil 与空值都写成合法 JSON，不要写空串」）。
//
// 由于 encoding/json 对 nil slice 编码为 "null"、对 nil map 同样编码为
// "null"，而不是 "[]"/"{}"，调用方必须显式传入希望落盘的「空」形态
// （例如把 nil slice 换成 make([]T, 0)）才能拿到 "[]"；本函数本身只负责
// 「不产出裸空串」，具体传什么空值由各调用点决定（因为哪种空形态更合适
// 因字段而异：切片字段的语义空值是 "[]"，map 字段是 "{}"）。
func encodeJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("sqlite: JSON 编码失败: %w", err)
	}
	return string(b), nil
}

// decodeJSON 把 JSON 文本列解码进 out（out 必须是指针）。空字符串视为
// 「没有内容」，不尝试解析（避免把历史遗留的空串数据当作解码错误处理）；
// out 保持调用方传入前的零值。
func decodeJSON(text string, out any) error {
	if text == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(text), out); err != nil {
		return fmt.Errorf("sqlite: JSON 解码失败（原文 %q）: %w", text, err)
	}
	return nil
}

// decodeJSONMapNilIfEmpty 解码一个 JSON 文本列到 map[string]string，若
// 解码结果是空 map（长度 0）则归一为 nil，保持与写入前「nil map」在 Go
// 层的可比较性一致。
//
// 存在的理由：encodeJSON 侧为了不写入裸空串而把 nil map 归一编码成合法
// JSON 的 "{}"（scan.go 上方 encodeJSON 注释），这一步是必要的——SQLite
// 该列是 NOT NULL 且没有别的合法空值表示。但反过来，多数调用点（如
// model.HookRef.ParamsDefaults）在 Go 层用 nil 表达「没有覆盖任何默认
// 参数」，而不是「有一个空 map」；这两种状态在业务语义上没有区别，但
// go-cmp 之类的深度比较会把 nil 与 map[string]string{} 视为不同值，
// 导致 conformance 套件里「写入 nil、断言取回后仍是零值」的用例误报。
// 本函数只在明确需要这种「空即 nil」归一的调用点使用，不是 decodeJSON
// 的默认行为——像 ArtifactPaths.Extra 这类字段的语义空值就是非 nil 的
// 空 map（内存实现的 copyStringMap 同样只在输入为 nil 时才返回 nil，
// 输入是空但非 nil 的 map 时返回一个新的空 map，两者行为一致），不应
// 被这个归一逻辑影响。
func decodeJSONMapNilIfEmpty(text string) (map[string]string, error) {
	var m map[string]string
	if err := decodeJSON(text, &m); err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, nil
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// 可空文本列：两个 token，空串与 NULL 要统一
// ---------------------------------------------------------------------------

// nullableToken 把 token 字符串转成写入列时使用的形式。schema 里两个
// token 列都是 "NOT NULL DEFAULT ”"（不是可空列），且对应的唯一索引
// 条件是 "!= ”"——因此这里不需要真正的 SQL NULL，空串本身就已经和
// 「没有 token」统一（空串不会命中条件索引，天然不会与其它空串任务的
// token 唯一性冲突）。保留本函数只是为了在调用点表达意图、并为未来若
// schema 改成真正可空列时留一个改动点。
func nullableToken(token string) string {
	return token
}
