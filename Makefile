# Lober 构建脚本（对应实现计划任务 19）。
#
# 环境：Windows + GNU Make 4.4.1（cygwin）+ bash。规则体一律用 go 自带命令
# （go build/go vet/gofmt/go test），不依赖 rm -rf、find 等 POSIX 专属工具——
# 这些命令在 cygwin 的 bash 下能跑，但换成纯 Windows shell 或未装 cygwin 的
# 机器上就会报「命令未找到」，go 命令本身是跨平台的，不会有这个问题。
#
# race 独立于 test（而不是 test 目标内部顺带跑 -race）：状态机的 CAS 与
# 扫描循环是并发的，两者验证的是不同性质——test 验证「行为对不对」，race
# 验证「并发安全」，后者耗时更长（-race 通常慢 2-10 倍）且不是每次改动都
# 需要跑。把它们拆开，日常开发可以只跑 test，提交前再跑一次 race。
#
# check 目标串起格式化检查、vet、两轮测试——每个任务的最后一步都应该跑它，
# 是 M1 收口验收清单里「make check 全绿」这一条的唯一入口。

.PHONY: build test race vet fmt check

# build 编译全部包，只为验证「能编译」，不产出可执行文件路径（cmd/lober
# 还未创建；等 M9 装配完成后可以在这里加 -o 指定输出路径，但那是 M9 的
# 改动，不是本任务该预先猜的）。
build:
	go build ./...

# test 跑全部包的测试（不含 -race）。
test:
	go test ./...

# race 独立跑一遍 -race，检测数据竞争。之所以不是 `test` 目标里加一个
# flag 分支，是让「跑 race」成为一个显式动作，避免日常开发者忘记跑它就
# 以为「测试通过」等于「并发安全」。
race:
	go test ./... -race

# vet 跑 go vet，抓 go build 不检查但很可能是 bug 的模式（如格式化字符串
# 参数不匹配、锁复制等）。
vet:
	go vet ./...

# fmt 检查格式化是否干净，不是「格式化并覆盖文件」——CI 场景下开发者应该
# 在本机跑 `gofmt -w .` 后再提交，本目标只负责「发现没跑」，不负责代劳
# 修改（代劳修改容易在 CI 环境里悄悄改动源码却没人注意到）。
#
# gofmt -l 对「已经是干净格式」的输入不产生任何输出；一旦有输出（哪怕
# 一行），说明至少有一个文件没格式化，此时用 exit 1 让本目标失败，不能
# 只打印文件名就当作命令成功退出。
fmt:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "$$unformatted"; \
		echo "以上文件未跑 gofmt，请执行 gofmt -w . 后重新提交"; \
		exit 1; \
	fi

# check 是收口目标：格式化检查 → vet → 测试 → race，四步全部通过才算
# 绿。用 Make 的目标依赖串联（而不是写成一行 shell `&&`），任何一步失败
# Make 会立即停止并保留清晰的「哪个目标失败」信息，比一整行命令失败后
# 只看到最后一条错误更容易定位。
check: fmt vet test race
