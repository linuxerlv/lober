package pgstore

import (
	"strings"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
	"gorm.io/gorm"
)

// templateSelectionQuery is the SQL equivalent of admission.TemplateUsable and
// the console's currentBuiltinSelection. Filter before counting and pagination;
// referenced versions stay pinned even if a newer hook version is approved.
func templateSelectionQuery(db *gorm.DB, selection *store.TemplateSelection) (*gorm.DB, error) {
	series, err := encodeJSON([]string{string(selection.ISO.Series)})
	if err != nil {
		return nil, err
	}
	db = db.Where("template_versions.state = ?", string(model.VersionApproved)).Where("template_versions.series @> CAST(? AS jsonb)", series)
	templateKeys := make([]versionPair, 0, len(selection.BuiltinTemplates))
	for _, key := range selection.BuiltinTemplates {
		templateKeys = append(templateKeys, versionPair{key.ID, key.Version})
	}
	allowedTemplates, templateArgs := allowedVersionPairs("template_versions.template_id", "template_versions.version", templateKeys)
	db = db.Where("(NOT template_versions.builtin OR ("+allowedTemplates+"))", templateArgs...)
	hookKeys := make([]versionPair, 0, len(selection.BuiltinHooks))
	for _, key := range selection.BuiltinHooks {
		hookKeys = append(hookKeys, versionPair{key.ID, key.Version})
	}
	allowedHooks, hookArgs := allowedVersionPairs("h.hook_id", "h.version", hookKeys)
	refs := `FROM template_hook_refs r JOIN hook_versions h ON h.hook_id = r.hook_id AND h.version = r.hook_version `
	matches := `r.template_id = template_versions.template_id AND r.template_version = template_versions.version`
	db = db.Where("NOT EXISTS (SELECT 1 "+refs+"WHERE "+matches+" AND (h.state = 'deprecated' OR (h.builtin AND NOT ("+allowedHooks+"))))", hookArgs...)
	if selection.ISO.Usable() {
		available := map[string]bool{}
		for key, options := range selection.ISO.Offers {
			if len(options) > 0 && storable(key) {
				available[key] = true
			}
		}
		offers, err := encodeJSON(available)
		if err != nil {
			return nil, err
		}
		missing := `p->>'Type' = 'enum' AND COALESCE(p->>'Offer', '') <> '' AND NOT jsonb_exists(CAST(? AS jsonb), p->>'Offer')`
		db = db.Where("NOT EXISTS (SELECT 1 FROM "+parameterElements("template_versions.params")+" AS p WHERE "+missing+")", offers)
		db = db.Where("NOT EXISTS (SELECT 1 "+refs+"CROSS JOIN LATERAL "+parameterElements("h.params")+" AS p WHERE "+matches+" AND "+missing+")", offers)
	}
	return db, nil
}

type versionPair struct {
	id      string
	version int
}

func allowedVersionPairs(idColumn, versionColumn string, keys []versionPair) (string, []any) {
	parts := make([]string, 0, len(keys))
	args := make([]any, 0, len(keys)*2)
	for _, key := range keys {
		if !model.ValidID(key.id) || key.version < 1 {
			continue
		}
		parts = append(parts, "("+idColumn+" = ? AND "+versionColumn+" = ?)")
		args = append(args, key.id, key.version)
	}
	if len(parts) == 0 {
		return "FALSE", nil
	}
	return strings.Join(parts, " OR "), args
}

func parameterElements(column string) string {
	return "jsonb_array_elements(CASE WHEN jsonb_typeof(" + column + ") = 'array' THEN " + column + " ELSE '[]'::jsonb END)"
}
