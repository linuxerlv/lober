package media

import (
	"github.com/lober/lober/internal/domain/catalog"
	"strings"
)

func Arch(value string) catalog.Arch {
	switch value {
	case "x86_64", "amd64":
		return "x86_64"
	case "aarch64", "arm64":
		return "aarch64"
	}
	return ""
}

func General(text string) map[string]string {
	general := map[string]string{}
	section := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		if section == "general" {
			if key, value, ok := strings.Cut(line, "="); ok {
				general[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
			}
		}
	}
	return general
}
