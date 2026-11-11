# make check 依次跑格式、vet、测试、竞态检测、交叉编译和生成漂移检查，六步全过才算绿。
# 各目标只用 go、gofmt 和 shell 内建命令，Git Bash 与 Linux 上都能跑。

.PHONY: check fmt vet archcheck test race cross gen webcheck

check: fmt vet test race cross gen

# 只查 go list 列出的包目录：主目录的 web/node_modules 里混有第三方 .go 文件，
# gofmt -l . 会把它们也算进来。go list 一个包都没列出时，不带参数的 gofmt
# 会改读标准输入而卡住，所以先把这种情况挡掉。
fmt:
	@echo 'gofmt -l $$(go list -f {{.Dir}} ./...)'
	@dirs="$$(go list -f '{{.Dir}}' ./...)" || exit 1; \
	if [ -z "$$dirs" ]; then echo "fmt：go list 没有列出任何包"; exit 1; fi; \
	unformatted="$$(gofmt -l $$dirs)" || exit 1; \
	if [ -n "$$unformatted" ]; then echo "以下文件没有 gofmt："; echo "$$unformatted"; exit 1; fi

vet:
	go vet ./...

# Whole-module package discovery cannot rely on Go's test-result cache: a
# subprocess go list can see new packages outside the tracked source roots.
archcheck:
	go test -count=1 -run '^Test全模块依赖守卫$$' ./internal/archtest

test: archcheck
	go test ./...

race: archcheck
	go test -race -timeout=20m ./...

# 旧系统出过在 Windows 上能编译、却编不出 Linux 版的问题（spec §7.1）。
cross:
	GOOS=linux GOARCH=amd64 go build ./...

# Wire's diff command regenerates in memory, returns nonzero on drift, and
# never changes the worktree or requires a clean staging area.
gen:
	WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober

# Frontend checks reuse the existing Vben workspace. Run after pnpm install.
webcheck:
	pnpm --dir web check:console
