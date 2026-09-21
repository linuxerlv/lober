package contract

import "testing"

// TestFailurePolicyFor_MatchesSPEC74Table 逐条锁住 SPEC §7.4 的失败语义表。
//
// 为什么这条重要：统一成「失败即中止」会让一次 DNS 抖动把装好的机器标成
// 失败；统一成「失败即忽略」会让 IP 没申请到的机器带着空配置装上去。
// 这张表是七个扩展点唯一的行为依据，任何一行改错都会让某类偶发故障
// 产生完全相反的后果——阻塞变成忽略，或忽略变成阻塞。
func TestFailurePolicyFor_MatchesSPEC74Table(t *testing.T) {
	cases := []struct {
		kind ProviderKind
		want FailurePolicy
		why  string
	}{
		{
			ProviderKindIPAM, FailurePolicyBlock,
			"IP 没申请到不能装（SPEC §7.4）：放行会让机器带着空的业务网络配置装上去",
		},
		{
			ProviderKindTemplateResolver, FailurePolicyBlock,
			"决策出不来无法继续（SPEC §7.4）：选不出模板却继续装机，装的是什么无法定义",
		},
		{
			ProviderKindDiskPlanner, FailurePolicyBlock,
			"决策出不来无法继续（SPEC §7.4）：选不出盘却继续装机，可能擦错盘或装到随机盘上",
		},
		{
			ProviderKindInventory, FailurePolicyBlock,
			"阻塞建任务但允许手工录入兜底（SPEC §7.4）：查不到清单数据仍需要阻塞，" +
				"“允许兜底”是调用方的补救路径，不改变本次调用失败即阻塞的语义",
		},
		{
			ProviderKindDNS, FailurePolicyWarnOnly,
			"只告警，不影响任务状态（SPEC §7.4）：一次 DNS 抖动不该把已经装好的机器标成失败",
		},
		{
			ProviderKindNotify, FailurePolicyWarnOnly,
			"只告警，不影响任务状态（SPEC §7.4）：通知系统不可用不该拖累装机结果",
		},
		{
			ProviderKindHeartbeat, FailurePolicyRecordOnly,
			"记录探测失败，不改任务状态（SPEC §7.4、§6.6）：心跳是辅助通道非主通道，" +
				"探测不到不等于机器真的出问题",
		},
	}

	for _, c := range cases {
		t.Run(string(c.kind), func(t *testing.T) {
			got, ok := FailurePolicyFor(c.kind)
			if !ok {
				t.Fatalf("FailurePolicyFor(%q) 返回 ok=false：want 已声明策略 %q。%s",
					c.kind, c.want, c.why)
			}
			if got != c.want {
				t.Fatalf("FailurePolicyFor(%q) = %q，want %q。%s",
					c.kind, got, c.want, c.why)
			}
		})
	}
}

// TestFailurePolicyFor_UnknownKindReturnsNotOK 锁住「查不到时返回未声明
// 而不是默认值」这一条设计要点。
//
// 为什么这条重要（计划任务 12 设计要点、SPEC §7.4）：加新 provider 类型时
// 必须显式声明失败语义。如果查不到会静默给一个默认策略（比如默认
// warn_only），有人加了新扩展点却忘了想清楚「这个失败该不该拦住装机」，
// 代码依然能跑，只是在某天生产环境里因为策略选错而放行了一次不该放行的
// 失败——且没有任何编译期或测试期信号提示这个疏漏。
func TestFailurePolicyFor_UnknownKindReturnsNotOK(t *testing.T) {
	const unknownKind ProviderKind = "some_future_provider_nobody_declared_yet"

	policy, ok := FailurePolicyFor(unknownKind)
	if ok {
		t.Fatalf("FailurePolicyFor(%q) 返回 ok=true, policy=%q：want ok=false。"+
			"未声明的 provider 类型必须显式返回“查不到”，不能悄悄给出任何默认策略"+
			"（否则新增扩展点时忘了声明失败语义也不会被发现）", unknownKind, policy)
	}
}

// TestFailurePolicyFor_AllSevenInterfacesHaveDeclaredKind 锁住 §7.1 清单的
// 七个接口对应的 ProviderKind 常量都在失败语义表中有声明（除两个纯决策
// 接口外，全部走查表；TemplateResolver 与 DiskPlanner 同样在表中，因为
// 它们的失败策略也是 block，只是它们本身不带 Ping 探活方法）。
//
// 为什么这条重要：七个接口与失败语义表是分别维护的两份清单（接口定义在
// 本文件靠前部分，表在靠后部分），如果新增第八个接口时只改了接口定义、
// 忘了在表里补一行，FailurePolicyFor 对它会一直返回 ok=false 而没有任何
// 编译错误提示——这条测试把「七个 kind 常量全部有声明」钉成一个会立刻
// 变红的断言，而不是依赖人记住两处要同步改。
func TestFailurePolicyFor_AllSevenInterfacesHaveDeclaredKind(t *testing.T) {
	allKinds := []ProviderKind{
		ProviderKindIPAM,
		ProviderKindDNS,
		ProviderKindInventory,
		ProviderKindNotify,
		ProviderKindHeartbeat,
		ProviderKindTemplateResolver,
		ProviderKindDiskPlanner,
	}

	for _, kind := range allKinds {
		if _, ok := FailurePolicyFor(kind); !ok {
			t.Errorf("ProviderKind %q 在失败语义表中查不到策略：SPEC §7.1 列出的七个"+
				"扩展点接口，每一个都必须在 §7.4 的失败语义表里有对应行，"+
				"缺失说明新增接口时漏掉了同步声明失败语义这一步", kind)
		}
	}
}
