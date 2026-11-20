package model

import (
	"reflect"
	"testing"
)

func Test业务用户不带网页或数据库标签(t *testing.T) {
	typ := reflect.TypeOf(User{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Tag != "" {
			t.Errorf("业务字段 %s 带有边界标签 %s", field.Name, field.Tag)
		}
	}
}
