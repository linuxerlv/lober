package model

import "github.com/lober/lober/internal/domain/catalog"

// SeriesSupported 只检查声明的发行版是否仍受支持，允许草稿暂不选择发行版。
func (t Template) SeriesSupported() bool { return seriesSupported(t.Series) }

// SeriesSupported 检查全部实现，不能借混合声明继续复制已停止支持的发行版。
func (h Hook) SeriesSupported() bool {
	for _, impl := range h.Impls {
		if !seriesSupported(impl.Series) {
			return false
		}
	}
	return true
}

func seriesSupported(series []catalog.Series) bool {
	for _, s := range series {
		if !s.Valid() {
			return false
		}
	}
	return true
}

// SystemSupported 只用于新执行入口；读取历史快照时不调用它。
func (s Snapshot) SystemSupported() bool {
	return s.Series.Valid() && s.Family == s.Series.Family() && s.Format == s.Series.Format()
}
