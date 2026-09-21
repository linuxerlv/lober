// 本文件实现 HookRequires.ContractVersion 约束表达式的解析与校验，供
// hook 注册期校验第三项使用（对应实现计划任务 4「contract 版本要求能被
// 满足」）。
//
// model 包只定义了 FactContractVersion 这个整数常量，没有提供约束表达式
// 的解析器（ContractVersion 字段本身只是一个 string，语义留给调用方
// 解释）——这是本模块必须自己写的一处小工具，不是契约缺口：字段类型
// 已经足够表达意图，只是解析逻辑没有归属到 model 层（版本约束语法是
// 控制台的注册期校验细节，不是跨模块都要用的公共语义）。
package console

import (
	"fmt"
	"strconv"
	"strings"
)

// checkContractVersion 校验形如 ">=1"、"=1"、"==1"、">1"、"<1"、"<=1" 的
// 约束表达式是否被 current（当前 fact 契约版本）满足。
//
// 空字符串视为「无约束」，总是满足——不强制每个 hook 都声明版本要求。
func checkContractVersion(constraint string, current int) error {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return nil
	}

	op, numStr := splitVersionConstraint(constraint)
	num, err := strconv.Atoi(strings.TrimSpace(numStr))
	if err != nil {
		return fmt.Errorf("无法解析版本约束 %q 中的数字部分: %w", constraint, err)
	}

	var ok bool
	switch op {
	case ">=":
		ok = current >= num
	case "<=":
		ok = current <= num
	case ">":
		ok = current > num
	case "<":
		ok = current < num
	case "==", "=":
		ok = current == num
	default:
		return fmt.Errorf("无法识别版本约束 %q 的比较符（支持 >=/<=/>/</=/==）", constraint)
	}
	if !ok {
		return fmt.Errorf("当前 fact 契约版本 %d 不满足约束 %q", current, constraint)
	}
	return nil
}

// splitVersionConstraint 把约束表达式拆成比较符与数字部分。按最长比较符
// 优先匹配（">=" 必须先于 ">" 判断，否则 ">=1" 会被误判成比较符 ">" 加
// 数字部分 "=1"）。
func splitVersionConstraint(constraint string) (op, rest string) {
	for _, candidate := range []string{">=", "<=", "==", ">", "<", "="} {
		if strings.HasPrefix(constraint, candidate) {
			return candidate, constraint[len(candidate):]
		}
	}
	return "", constraint
}
