package media

import (
	"strings"
	"unicode"

	"github.com/lober/lober/internal/domain/catalog"
)

// RepositoryLayout 的路径都相对于安装树根，根本身保留 .treeinfo。
type RepositoryLayout struct {
	Root      string
	RepoPaths []string
}

// MinimalKickstartRepository 只接受两系明确且一致的 Minimal 声明。
// 没有声明时，调用方仍使用完整盘规则；有声明但不完整时不能回退。
func MinimalKickstartRepository(series catalog.Series, treeinfo string) (RepositoryLayout, bool, error) {
	if series != catalog.SeriesAlmalinux && series != catalog.SeriesRocky {
		return RepositoryLayout{}, false, nil
	}
	sections := map[string]map[string]string{}
	section := ""
	matched, invalid := false, false
	for _, line := range strings.Split(treeinfo, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			declared := strings.TrimSpace(line[1 : len(line)-1])
			section = strings.ToLower(declared)
			if strings.HasPrefix(section, "variant-") && minimalRepositoryToken(strings.TrimPrefix(section, "variant-")) {
				matched = true
				invalid = invalid || declared != "variant-Minimal"
			}
			if _, exists := sections[section]; exists {
				invalid = true
			} else {
				sections[section] = map[string]string{}
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || section == "" {
			invalid = true
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		if _, exists := sections[section][key]; exists {
			invalid = true
		}
		sections[section][key] = value
		// 只有分组字段决定布局。路径中的普通目录名不算分组声明。
		if section == "general" && (key == "variant" || key == "variants") ||
			section == "tree" && key == "variants" ||
			strings.HasPrefix(section, "variant-") && key == "id" {
			matched = matched || minimalRepositoryToken(value)
		}
	}
	if !matched {
		return RepositoryLayout{}, false, nil
	}
	reject := func() (RepositoryLayout, bool, error) {
		return RepositoryLayout{}, true, Problem("iso_repo_incomplete", "artifacts.repo", "Minimal/repodata/repomd.xml")
	}
	if invalid {
		return reject()
	}
	for name := range sections {
		if strings.HasPrefix(name, "variant-") && name != "variant-minimal" {
			return reject()
		}
	}
	for section, expected := range map[string]map[string]string{
		"general":         {"variant": "Minimal", "variants": "Minimal", "repository": "Minimal", "packagedir": "Minimal/Packages"},
		"tree":            {"variants": "Minimal"},
		"variant-minimal": {"id": "Minimal", "name": "Minimal", "uid": "Minimal", "type": "variant", "repository": "Minimal", "packages": "Minimal/Packages"},
	} {
		for key, value := range expected {
			if sections[section][key] != value {
				return reject()
			}
		}
	}
	general, release, tree := sections["general"], sections["release"], sections["tree"]
	if Arch(general["arch"]) == "" || general["arch"] != tree["arch"] ||
		!versionPattern.MatchString(general["version"]) || general["version"] != release["version"] {
		return reject()
	}
	family, short := "AlmaLinux", "AlmaLinux"
	if series == catalog.SeriesRocky {
		family, short = "Rocky Linux Minimal", "Rocky-minimal"
	}
	if general["family"] != family || general["name"] != family+" "+general["version"] || release["name"] != family || release["short"] != short {
		return reject()
	}
	return RepositoryLayout{Root: ".", RepoPaths: []string{"Minimal/repodata/repomd.xml"}}, true, nil
}

func minimalRepositoryToken(value string) bool {
	for _, token := range strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-'
	}) {
		if strings.EqualFold(token, "Minimal") {
			return true
		}
	}
	return false
}
