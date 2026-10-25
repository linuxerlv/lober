package media

import (
	"github.com/lober/lober/internal/domain/catalog"
	"regexp"
	"strings"
)

var leapProduct = regexp.MustCompile(`^/ (openSUSE-Leap|Leap) ([0-9]+\.[0-9]+)-[0-9]+$`)

func SUSEHeader(general map[string]string) bool {
	v := general["version"]
	return versionPattern.MatchString(v) && general["family"] == "openSUSE Leap" && general["name"] == "openSUSE Leap "+v
}
func SUSEProduct(text, version string) (string, bool) {
	p := leapProduct.FindStringSubmatch(strings.TrimSpace(text))
	if p == nil || p[2] != version {
		return "", false
	}
	return p[1], true
}
func SUSEFormat(version, repo string, live bool, product string) (catalog.Format, catalog.Series, bool) {
	switch {
	case strings.HasPrefix(version, "15.") && repo == "." && !live && product == "openSUSE-Leap":
		return "autoyast", "leap15", true
	case strings.HasPrefix(version, "16.") && live:
		return "agama", "leap16", true
	}
	return "", "", false
}
