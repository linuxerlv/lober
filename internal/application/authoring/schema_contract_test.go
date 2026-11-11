package authoring_test

import (
	"reflect"
	"testing"

	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/domain/catalog"
)

func Test公开目录保持顺序与独立副本(t *testing.T) {
	want := authoring.CatalogEnums{
		Families: catalog.Families(), Formats: catalog.AllFormats(), Series: catalog.AllSeries(),
		Archs: catalog.AllArchs(), Firmwares: catalog.AllFirmwares(),
	}
	got := authoring.PublicCatalog()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("公开目录必须保持领域目录的全部值和顺序：got=%v want=%v", got, want)
	}
	got.Families[0] = "changed"
	got.Formats[0] = "changed"
	got.Series[0] = "changed"
	got.Archs[0] = "changed"
	got.Firmwares[0] = "changed"
	if next := authoring.PublicCatalog(); !reflect.DeepEqual(next, want) {
		t.Fatalf("修改返回结果污染了公开目录：%v", next)
	}
	if !reflect.DeepEqual(catalog.Families(), want.Families) || !reflect.DeepEqual(catalog.AllFormats(), want.Formats) ||
		!reflect.DeepEqual(catalog.AllSeries(), want.Series) || !reflect.DeepEqual(catalog.AllArchs(), want.Archs) ||
		!reflect.DeepEqual(catalog.AllFirmwares(), want.Firmwares) {
		t.Fatal("修改公开结果污染了领域目录")
	}
}
