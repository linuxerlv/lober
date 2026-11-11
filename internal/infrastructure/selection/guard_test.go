package selection

import "testing"

func Test选择锁读者共用且写者独占(t *testing.T) {
	g := NewGuard().(*guard)
	r := g.ReadLock()
	if g.TryLock() {
		t.Fatal("读锁未阻止写入")
	}
	if !g.TryRLock() {
		t.Fatal("读者不能共享")
	}
	g.RUnlock()
	r()
	w := g.WriteLock()
	if g.TryRLock() {
		t.Fatal("写锁未阻止读取")
	}
	if g.TryLock() {
		t.Fatal("写锁未阻止另一写入")
	}
	w()
	if !g.TryLock() {
		t.Fatal("释放后不能写入")
	}
	g.Unlock()
}
