package media

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// 保留完整构建号，避免同一更新版本的不同镜像共用制品目录。
var esxiBuildPattern = regexp.MustCompile(`^[789]\.[0-9]+\.[0-9]+-[0-9]+(?:\.[0-9]+)+$`)

func ESXiVersion(raw string) string {
	values, ok := esxiConfig(raw)
	if !ok || !esxiBuildPattern.MatchString(values["build"]) || !versionPattern.MatchString(values["build"]) {
		return ""
	}
	return values["build"]
}

// ESXiBootFiles 返回原始安装介质引用的文件；不接受外部地址和越界路径。
func ESXiBootFiles(raw string) ([]string, bool) {
	values, ok := esxiConfig(raw)
	if !ok || values["prefix"] != "" && values["prefix"] != "/" {
		return nil, false
	}
	tokens := append([]string{values["kernel"]}, strings.Split(values["modules"], "---")...)
	files := make([]string, 0, len(tokens))
	for _, token := range tokens {
		fields := strings.Fields(token)
		if len(fields) == 0 {
			return nil, false
		}
		name := strings.TrimPrefix(fields[0], "/")
		if name == "" || strings.ContainsAny(name, "\\:?#") {
			return nil, false
		}
		for _, part := range strings.Split(name, "/") {
			if part == "" || part == "." || part == ".." {
				return nil, false
			}
		}
		files = append(files, name)
	}
	return files, true
}

func esxiConfig(raw string) (map[string]string, bool) {
	if !utf8.ValidString(raw) || strings.ContainsRune(raw, 0) {
		return nil, false
	}
	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimPrefix(raw, "\ufeff"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if _, duplicate := values[key]; !ok || key == "" || duplicate {
			return nil, false
		}
		values[key] = strings.TrimSpace(value)
	}
	return values, true
}
