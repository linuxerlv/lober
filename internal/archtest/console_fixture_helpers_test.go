package archtest

import "strings"

// 各模块仍按本例需要注入旧桥；返回的声明文本保持不变。
func legacyConsoleBridgeFixture(body string) string {
	legacy := ""
	if strings.Contains(body, "bridge2") {
		legacy += `func(s *server)bridge2(){s.bridge()};`
	}
	if strings.Contains(body, "bridge") {
		legacy += `func(s *server)bridge(){s.taskStream()};`
	}
	if strings.Contains(body, "alias") || strings.Contains(body, "oldResponse") {
		legacy += `func oldResponse(){(&server{}).taskStream()};var alias=oldResponse;`
	}
	if strings.Contains(body, "RespondError") {
		legacy += `func RespondError(){(&server{}).taskStream()};`
	}
	if legacy != "" || strings.Contains(body, "taskStream") {
		legacy += `func(s *server)taskStream(){s.deps.LegacyTasks.GetTask()};`
	}
	return legacy
}
