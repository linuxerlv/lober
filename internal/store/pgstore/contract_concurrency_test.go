package pgstore_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/store/pgstore"
	"github.com/lober/lober/internal/store/pgstore/pgtest"
)

// Test契约并发 是并发这一组（C30、C31、C33，spec §6.2 第 4 条）：多个 goroutine 同时调用，只按最终结果断言（恰好一个成功、
// Seq 连续不重不缺），不依赖调度的先后。这一组考验的是加锁与约束：用例失败时，先查是不是没加锁、加锁的顺序不一致，
// 或者在事务外读了数据，不要靠重试或加大超时让它变绿。
func Test契约并发(t *testing.T) {
	runCases(t, []testCase{
		{"同一台主机的几批任务同时提交，恰好一批成功，其余报 host_has_open_task", concurrentBatches},
		{"不同主机的几批任务同时提交，全都成功", concurrentDisjointBatches},
		{"当前选择：反序主机整批竞争，只有一批成功且审计不多写", concurrentCurrentBatches},
		{"同一个任务用同一个期望值同时推进，恰好一次成功，只有它带的事件落库", concurrentTransitions},
		{"同一个任务同时追加事件，Seq 连续、不重、不缺，一批的几条相邻", concurrentAppends},
		{"同一个 Key 同时追加，只落一条", concurrentSameKey},
	})
}

// workers 是每个并发用例同时调用的 goroutine 数。
const workers = 8

// together 让 n 个 goroutine 一起放行、同时跑 fn(i)，等它们全部返回：尽量让调用撞在一起。fn 里不能调用 t.Fatal 这类只能在
// 测试本身的 goroutine 里调用的方法，结果由调用方收集之后再断言。
func together(n int, fn func(i int)) {
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			fn(i)
		})
	}
	close(start)
	wg.Wait()
}

// 几批都是这三台主机，顺序各不相同：拿锁的顺序要是跟着入参走，两批就会互相等死。恰好一批成功（C31）；其余的报
// BatchError，里面只有 host_has_open_task。在事务里重跑准入时每台都报，靠部分唯一索引兜底时可能只报撞上的那一台，所以
// 不要求每台都报。
func concurrentBatches(t *testing.T, s *pgstore.Store) {
	concurrentBatchesUsing(t, s, s.CreateTasks)
}
func concurrentCurrentBatches(t *testing.T, s *pgstore.Store) {
	concurrentBatchesUsing(t, s, s.CreateCurrentTasks)
}
func concurrentBatchesUsing(t *testing.T, s *pgstore.Store, create func(context.Context, []model.Task, repository.Audit) ([]model.Task, error)) {
	f := newFixture(t, s)
	hs := mustTaskHosts(t, s, "SN-1", "SN-2", "SN-3")
	batches := make([][]model.Task, workers)
	for i := range batches {
		order := append(slices.Clone(hs[i%len(hs):]), hs[:i%len(hs)]...)
		if i%2 == 1 {
			slices.Reverse(order)
		}
		batches[i] = f.newTasks(t, order...)
	}
	audits := auditEntries(t, s)
	results := make([][]model.Task, workers)
	errs := make([]error, workers)
	together(workers, func(i int) { results[i], errs[i] = create(t.Context(), batches[i], anAudit("task.create")) })
	winner := -1
	for i, err := range errs {
		if err == nil {
			if winner >= 0 {
				t.Fatalf("第 %d 批与第 %d 批都成功了：同一台主机有了两个未结束的任务（C31）", winner+1, i+1)
			}
			winner = i
			continue
		}
		var be *repository.BatchError
		if !errors.As(err, &be) || !errors.Is(err, repository.ErrRejected) {
			t.Fatalf("第 %d 批应报 BatchError(ErrRejected)，得到 %v", i+1, err)
		}
		ps := batchProblems(be)
		if len(ps) == 0 || slices.ContainsFunc(ps, func(p model.Problem) bool { return p.Code != "host_has_open_task" }) {
			t.Fatalf("第 %d 批的问题应当只有 host_has_open_task，至少一条，得到 %v", i+1, ps)
		}
	}
	if winner < 0 {
		t.Fatal("一批都没成功")
	}
	wantItems(t, slices.Concat(taskPages(t, s, repository.TaskQuery{Limit: 500})...), sortedTasks(results[winner]), "建出的任务只有成功的那一批")
	for _, task := range results[winner] {
		got, found, err := s.FindOpenTask(t.Context(), task.HostSN)
		must(t, err, "找 "+task.HostSN+" 未结束的任务")
		if !found || got.ID != task.ID {
			t.Fatalf("%s 未结束的任务应是成功那一批里的 %s，得到 %v、%s", task.HostSN, task.ID, found, got.ID)
		}
	}
	if n := len(auditEntries(t, s)) - len(audits); n != len(hs) {
		t.Fatalf("应只为成功的那一批写 %d 条审计，写了 %d 条", len(hs), n)
	}
}

// 加锁只挡同一台主机：几批的主机互不相同时，同时提交都要成功，不能把别的主机的批也挡下来。
func concurrentDisjointBatches(t *testing.T, s *pgstore.Store) {
	f := newFixture(t, s)
	batches := make([][]model.Task, workers)
	for i := range batches {
		batches[i] = f.newTasks(t, mustTaskHosts(t, s, fmt.Sprintf("SN-%d", i))...)
	}
	errs := make([]error, workers)
	together(workers, func(i int) { _, errs[i] = s.CreateTasks(t.Context(), batches[i], anAudit("task.create")) })
	for i, err := range errs {
		must(t, err, fmt.Sprintf("第 %d 批", i+1))
	}
	wantEqual(t, len(slices.Concat(taskPages(t, s, repository.TaskQuery{Limit: 500})...)), workers, "建出的任务数")
}

// 条件推进（C33）：同一个期望值只有一次生效，其余报 ErrConflict。事件与状态在同一个事务里写，所以只有成功那一次带的事件落库。
func concurrentTransitions(t *testing.T, s *pgstore.Store) {
	task := mustTask(t, s)
	expect := repository.TaskExpect{State: task.State, Rev: task.Rev}
	errs := make([]error, workers)
	together(workers, func(i int) {
		events := []repository.NewEvent{{At: at0, Kind: "task.boot", Key: "boot-" + strconv.Itoa(i)}}
		_, _, errs[i] = s.TransitionTask(t.Context(), task.ID, expect, model.TaskBooting, events)
	})
	winner := -1
	for i, err := range errs {
		if err == nil {
			if winner >= 0 {
				t.Fatalf("第 %d 次与第 %d 次推进都成功了：同一个期望值只能生效一次", winner+1, i+1)
			}
			winner = i
			continue
		}
		wantIs(t, err, repository.ErrConflict, fmt.Sprintf("第 %d 次推进", i+1))
	}
	if winner < 0 {
		t.Fatal("一次都没成功")
	}
	want := task
	want.State, want.Rev = model.TaskBooting, task.Rev+1
	wantEqual(t, mustGetTask(t, s, task.ID), want, "推进之后")
	wantEqual(t, allEvents(t, s, task.ID), []repository.Event{{TaskID: task.ID, Seq: 1,
		NewEvent: repository.NewEvent{At: at0, Kind: "task.boot", Key: "boot-" + strconv.Itoa(winner)}}}, "落库的事件")
}

// 同一个任务同时追加：每个 goroutine 追加 rounds 批、每批两条。全部 Seq 从 1 起连续、不重、不缺，一批的两条相邻（一批在
// 一个事务里分配），并且每个 Seq 落库的正是追加结果里报的那一条。
func concurrentAppends(t *testing.T, s *pgstore.Store) {
	const rounds = 20
	id := mustTask(t, s).ID
	got := make([][]repository.Appended, workers)
	errs := make([]error, workers)
	together(workers, func(i int) {
		for r := range rounds {
			es := []repository.NewEvent{
				{At: at0, Kind: "slot.log", Fields: map[string]string{"by": strconv.Itoa(i), "n": strconv.Itoa(2 * r)}},
				{At: at0, Kind: "slot.log", Fields: map[string]string{"by": strconv.Itoa(i), "n": strconv.Itoa(2*r + 1)}},
			}
			a, err := s.AppendEvents(t.Context(), id, es)
			if err != nil {
				errs[i] = err
				return
			}
			got[i] = append(got[i], a...)
		}
	})
	owner := map[int64]string{} // Seq → 追加它的 goroutine 与序号
	for i, as := range got {
		must(t, errs[i], fmt.Sprintf("第 %d 个 goroutine 追加", i+1))
		if len(as) != 2*rounds {
			t.Fatalf("第 %d 个 goroutine 追加了 %d 条，结果有 %d 条", i+1, 2*rounds, len(as))
		}
		for j, a := range as {
			switch _, dup := owner[a.Seq]; {
			case a.Duplicate || dup:
				t.Fatalf("第 %d 个 goroutine 的第 %d 条得到 %+v：没有 Key 的事件不去重，Seq 也不能发两次", i+1, j+1, a)
			case j%2 == 1 && a.Seq != as[j-1].Seq+1:
				t.Fatalf("第 %d 个 goroutine 的一批两条得到 Seq %d 与 %d，应当相邻", i+1, as[j-1].Seq, a.Seq)
			}
			owner[a.Seq] = fmt.Sprintf("%d/%d", i, j)
		}
	}
	evs := allEvents(t, s, id)
	if len(evs) != workers*2*rounds {
		t.Fatalf("应落库 %d 条事件，得到 %d 条", workers*2*rounds, len(evs))
	}
	for k, e := range evs {
		if e.Seq != int64(k+1) {
			t.Fatalf("Seq 应从 1 起连续，第 %d 条的 Seq 是 %d", k+1, e.Seq)
		}
		if who := e.Fields["by"] + "/" + e.Fields["n"]; who != owner[e.Seq] {
			t.Fatalf("Seq %d 落库的是 %s 追加的，追加的结果说是 %s", e.Seq, who, owner[e.Seq])
		}
	}
}

// 同一个信号同时重发了几次：只落一条，恰好一次报告为新写的，其余报告 Duplicate，Seq 都是那一条的。
func concurrentSameKey(t *testing.T, s *pgstore.Store) {
	id := mustTask(t, s).ID
	got := make([][]repository.Appended, workers)
	errs := make([]error, workers)
	together(workers, func(i int) {
		es := []repository.NewEvent{{At: at0, Kind: "signal.recv", Key: "sig-1", Fields: map[string]string{"by": strconv.Itoa(i)}}}
		got[i], errs[i] = s.AppendEvents(t.Context(), id, es)
	})
	winner := -1
	for i, as := range got {
		must(t, errs[i], fmt.Sprintf("第 %d 次追加", i+1))
		if len(as) != 1 || as[0].Seq != 1 {
			t.Fatalf("第 %d 次追加得到 %+v，应只有一条，Seq 都是那一条的 1", i+1, as)
		}
		if !as[0].Duplicate {
			if winner >= 0 {
				t.Fatalf("第 %d 次与第 %d 次都报告为新写的：同一个 Key 只能落一条", winner+1, i+1)
			}
			winner = i
		}
	}
	if winner < 0 {
		t.Fatal("没有一次报告为新写的")
	}
	wantEqual(t, allEvents(t, s, id), []repository.Event{{TaskID: id, Seq: 1, NewEvent: repository.NewEvent{
		At: at0, Kind: "signal.recv", Key: "sig-1", Fields: map[string]string{"by": strconv.Itoa(winner)},
	}}}, "落库的事件")
}

// 下面几个测试要另开一个连接、让对方停在没提交的事务里（writeWhileHeld），要 schema 的 DSN，用的是 pgtest.Schema 与
// pgtest.OpenAt，不走 runCases：测锁等待的用例保持串行。

// 写事务的隔离级别由 pgstore 开事务时指定为 READ COMMITTED，不取连接的默认值（spec §6.4）：加锁之后重读最新的数据靠的
// 就是这一级。连接的默认值可以由 DSN、ALTER DATABASE/ROLE … SET default_transaction_isolation 或 postgresql.conf 改掉；
// 运维为了「更安全」设成 repeatable read 或 serializable 时，FOR UPDATE 等到对方提交之后不再重读，而是报 SQLSTATE
// 40001，契约里没有这一类，第 5、7 层只能当内部错误。这里在 DSN 上把连接的默认值设成 repeatable read，跑一次要等对方
// 提交的写入：照样报 ErrConflict。
func Test写事务不随连接默认的隔离级别变(t *testing.T) {
	dsn, err := withRuntimeParam(pgtest.Schema(t), "default_transaction_isolation", "repeatable read")
	if err != nil {
		t.Fatal(err)
	}
	s := pgtest.OpenAt(t, dsn)
	ctx := t.Context()
	var level string
	if err := s.DBForTest().WithContext(ctx).Raw(`SHOW default_transaction_isolation`).Row().Scan(&level); err != nil {
		t.Fatalf("查连接默认的隔离级别：%v", err)
	}
	if level != "repeatable read" {
		t.Fatalf("DSN 上的 default_transaction_isolation 没生效，连接默认的是 %q：这样就什么都没测到", level)
	}
	h, err := s.CreateHost(ctx, model.Host{SN: "SN-1", Arch: "x86_64", Firmware: "uefi"}, anAudit("host.create"))
	must(t, err, "登记")
	err = writeWhileHeld(t, dsn, `UPDATE hosts SET arch = 'aarch64', rev = rev + 1 WHERE sn = $1`, h.SN, func() error {
		in := h
		in.Firmware = "bios"
		_, err := s.UpdateHost(ctx, in, anAudit("host.update"))
		return err
	})
	wantIs(t, err, repository.ErrConflict, fmt.Sprintf("对方提交之后 Rev 已是 %d，带着 %d 修改", h.Rev+1, h.Rev))
	want := h
	want.Arch, want.Rev = "aarch64", h.Rev+1
	got, err := s.GetHost(ctx, h.SN)
	must(t, err, "读回")
	wantEqual(t, got, want, "库里应留着对方写下的")
}

// withRuntimeParam 给 DSN 加一个连接参数，写法同 pgtest 的 withSearchPath：URL 形式加在查询参数里，key=value 形式接在末尾、值加
// 单引号。出错时不带 DSN 原文，免得把口令打进测试输出。
func withRuntimeParam(dsn, key, value string) (string, error) {
	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		return dsn + " " + key + "='" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value) + "'", nil
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "", errors.New("测试库的 DSN 不是合法的 URL（为免泄露口令，不打出原文）")
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// 两路同时装载同一份内置内容（spec §6.2 第 11 条）：先到的一路还没提交时，后到的一路要等它提交，再看到它写下的版本，
// 内容相同就什么都不做、返回 nil（repository.Hooks 的 EnsureBuiltinHook）。所以 EnsureBuiltin* 先确定身份（INSERT … ON
// CONFLICT DO NOTHING 会等先到的一路提交），再锁住身份，锁住之后才查版本。要是先不加锁地查版本、查不到再锁身份，
// 后到的一路看不到对方还没提交的版本：身份也是新的，就去建身份，撞上 hooks_pkey；身份已在，就等到锁之后直接写版本，
// 撞上 hook_versions_pkey。两处都是没翻译的 23505，装配点只能拒绝启动。另开一个连接当先到的一路，照仓储的写法在没提交
// 的事务里写下身份与版本；契约里的装载都是串行的，钉不住这一点。新的写法只在 READ COMMITTED 下成立：ON CONFLICT 与
// FOR UPDATE 等到对方提交之后，在 repeatable read 下报的是 40001（见 Test写事务不随连接默认的隔离级别变）。
//
// writeWhileHeld 让先到的一路写下身份之后，后到的一路才开始；两路都在对方插入身份之前查过冲突的交错，见
// Test同时首次装载同一个新身份两路都没看到对方。
func Test同时装载同一份内置内容后到的等先到的提交之后什么都不做(t *testing.T) {
	const id, name, key = "builtin-base", "lober:Base", "lober:base"
	// 装载的内置版本都是最小的内容：版本行的各列好在 SQL 里照写
	hook := func(version int) model.Hook {
		return model.Hook{ID: id, Version: version, Name: name, State: model.VersionApproved, Author: "lober", Builtin: true, Rev: 1}
	}
	template := func(version int) model.Template {
		return model.Template{ID: id, Version: version, Name: name, State: model.VersionApproved, Author: "lober", Builtin: true, Rev: 1}
	}
	kinds := []struct {
		name   string
		idents string
		// insert 是版本表与列，values 是身份 ID 与版本号之后各列的值，与 hook、template 给出的内容相同
		insert, values string
		ensure         func(ctx context.Context, s *pgstore.Store, version int) error
		listed         func(ctx context.Context, s *pgstore.Store) (any, error) // 身份 id 的全部版本
		want           func(versions ...int) any                                // 只有这几个版本时 listed 应得到的
	}{
		{"hook", "hooks",
			"hook_versions (hook_id, version, description, state, author, approver, builtin, params, timeout_sec, required, impls, rev)",
			"'', 'approved', 'lober', '', true, 'null', 0, false, 'null', 1",
			func(ctx context.Context, s *pgstore.Store, version int) error {
				return s.EnsureBuiltinHook(ctx, hook(version))
			},
			func(ctx context.Context, s *pgstore.Store) (any, error) {
				vs, err := s.ListHookVersions(ctx, id)
				return vs, err
			},
			func(versions ...int) any {
				out := make([]model.Hook, len(versions))
				for i, v := range versions {
					out[i] = hook(v)
				}
				return out
			}},
		{"模板", "templates",
			"template_versions (template_id, version, state, author, approver, builtin, derived_from, family, series, body, params, " +
				"bindings, assembly, rev)",
			"'approved', 'lober', '', true, 'null', '', 'null', '', 'null', 'null', 'null', 1",
			func(ctx context.Context, s *pgstore.Store, version int) error {
				return s.EnsureBuiltinTemplate(ctx, template(version))
			},
			func(ctx context.Context, s *pgstore.Store) (any, error) {
				vs, err := s.ListTemplateVersions(ctx, id)
				return vs, err
			},
			func(versions ...int) any {
				out := make([]model.Template, len(versions))
				for i, v := range versions {
					out[i] = template(v)
				}
				return out
			}},
	}
	for _, c := range []struct {
		name    string
		before  []int  // 先装好的版本
		ident   string // 先到的一路对身份做的事，RETURNING id 给写版本用；%s 是身份表
		version int    // 两路都装载的版本
	}{
		{"新身份", nil, `INSERT INTO %s (id, name, name_key, builtin, last_version) VALUES ($1, '` + name + `', '` + key +
			`', true, 1) RETURNING id`, 1},
		{"已有身份的新版本", []int{1}, `UPDATE %s SET last_version = 2 WHERE id = $1 RETURNING id`, 2},
	} {
		for _, k := range kinds {
			t.Run(c.name+"："+k.name, func(t *testing.T) {
				dsn := pgtest.Schema(t)
				s := pgtest.OpenAt(t, dsn)
				ctx := t.Context()
				for _, v := range c.before {
					must(t, k.ensure(ctx, s, v), fmt.Sprintf("先装好版本 %d", v))
				}
				hold := fmt.Sprintf("WITH w AS ("+c.ident+") INSERT INTO %s SELECT id, %d, %s FROM w", k.idents, k.insert, c.version, k.values)
				err := writeWhileHeld(t, dsn, hold, id, func() error { return k.ensure(ctx, s, c.version) })
				must(t, err, fmt.Sprintf("先到的一路装好版本 %d 并提交之后，后到的一路装载同样的内容", c.version))
				all := append(slices.Clone(c.before), c.version)
				got, err := k.listed(ctx, s)
				must(t, err, "列出版本")
				if want := k.want(all...); !reflect.DeepEqual(got, want) {
					t.Fatalf("库里应只有版本 %v，内容都是装载的：\n得到 %+v\n应为 %+v", all, got, want)
				}
				var idents, last int64
				if err := s.DBForTest().WithContext(ctx).Raw("SELECT count(*), max(last_version) FROM "+k.idents).Row().Scan(&idents, &last); err != nil {
					t.Fatalf("查身份：%v", err)
				}
				if idents != 1 || last != int64(c.version) {
					t.Fatalf("应只有一个身份，分配过的最大版本号是 %d，得到 %d 个身份、%d", c.version, idents, last)
				}
			})
		}
	}
}

// 两路同时首次装载同一个新的内置身份，两条 INSERT 都在对方插入之前查过冲突（spec §6.2 第 11 条）：后到的一路照样要等
// 先到的提交，之后什么都不做、返回 nil。插入身份只拿主键当仲裁（ON CONFLICT (id) DO NOTHING）时，后到的一路在名字的
// 唯一索引 idx_*_name_key 上等先到的提交，之后直接报 23505；在事务外查到的占着名字的身份正是它自己要装载的，报出
// DuplicateError{name, 自己的 ID}，装配点只能拒绝启动。所以插入身份的冲突不带目标，主键与名字的唯一索引都当仲裁。
//
// 这种交错靠两条 INSERT 撞在一起，排不出来，只能靠调度碰：用 together(2) 跑 rounds 轮，每轮一个新的 ID。本机实测旧写法
// 每轮出错的占 15% 到 30%，按 15% 算，200 轮一次都不出错的概率也小于 1e-14。这个测试本身在测并发，保持串行。
//
// 不带目标的 DO NOTHING 不能把该报的错也吞掉：两个不同的内置 ID 同时用同一个名字，恰好一路成功，另一路报
// DuplicateError，Existing 是成功的那一路，它的身份也不建。
func Test同时首次装载同一个新身份两路都没看到对方(t *testing.T) {
	const rounds = 200
	for _, k := range []struct {
		name             string
		idents, versions string
		ensure           func(ctx context.Context, s *pgstore.Store, id, name string) error
	}{
		{"hook", "hooks", "hook_versions", func(ctx context.Context, s *pgstore.Store, id, name string) error {
			return s.EnsureBuiltinHook(ctx, model.Hook{ID: id, Version: 1, Name: name, State: model.VersionApproved,
				Author: "lober", Builtin: true, Rev: 1})
		}},
		{"模板", "templates", "template_versions", func(ctx context.Context, s *pgstore.Store, id, name string) error {
			return s.EnsureBuiltinTemplate(ctx, model.Template{ID: id, Version: 1, Name: name, State: model.VersionApproved,
				Author: "lober", Builtin: true, Rev: 1})
		}},
	} {
		// race 跑 rounds 轮：第 r 轮两路同时装载，各自的 ID 是 ids(r) 里的，名字都是 lober:Race-r。返回每轮两路的结果
		race := func(t *testing.T, s *pgstore.Store, ids func(r int) [2]string) [][2]error {
			ctx := t.Context()
			out := make([][2]error, rounds)
			for r := range out {
				id, name := ids(r), fmt.Sprintf("lober:Race-%d", r)
				together(2, func(i int) { out[r][i] = k.ensure(ctx, s, id[i], name) })
			}
			return out
		}
		// wantRows 要求身份表与版本表各有 n 行：每轮只建出一个身份、一个版本
		wantRows := func(t *testing.T, s *pgstore.Store, n int64) {
			t.Helper()
			var idents, versions int64
			if err := s.DBForTest().WithContext(t.Context()).Raw("SELECT (SELECT count(*) FROM "+k.idents+"), (SELECT count(*) FROM "+
				k.versions+")").Row().Scan(&idents, &versions); err != nil {
				t.Fatalf("数身份与版本：%v", err)
			}
			if idents != n || versions != n {
				t.Fatalf("应有 %d 个身份、%d 个版本，得到 %d 个身份、%d 个版本", n, n, idents, versions)
			}
		}

		t.Run("同一个 ID："+k.name, func(t *testing.T) {
			s := pgtest.OpenAt(t, pgtest.Schema(t))
			failed, first := 0, error(nil)
			for _, errs := range race(t, s, func(r int) [2]string {
				id := fmt.Sprintf("builtin-race-%d", r)
				return [2]string{id, id}
			}) {
				if err := errors.Join(errs[:]...); err != nil {
					failed++
					if first == nil {
						first = err
					}
				}
			}
			if failed > 0 {
				t.Fatalf("%d 轮里有 %d 轮没有两路都返回 nil，第一个错误：%v", rounds, failed, first)
			}
			wantRows(t, s, rounds)
		})

		t.Run("不同 ID 同名："+k.name, func(t *testing.T) {
			s := pgtest.OpenAt(t, pgtest.Schema(t))
			ids := func(r int) [2]string {
				return [2]string{fmt.Sprintf("builtin-a-%d", r), fmt.Sprintf("builtin-b-%d", r)}
			}
			for r, errs := range race(t, s, ids) {
				won := slices.Index(errs[:], nil)
				if won < 0 || errs[1-won] == nil {
					t.Fatalf("第 %d 轮应恰好一路成功，得到 %v；%v", r+1, errs[0], errs[1])
				}
				wantDuplicate(t, errs[1-won], "name", ids(r)[won], fmt.Sprintf("第 %d 轮没成功的那一路", r+1))
			}
			wantRows(t, s, rounds)
		})
	}
}
