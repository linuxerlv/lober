// Package pgstore 是仓储的 PostgreSQL 实现（spec §4.2），也是唯一的一份实现。它用 GORM 访问数据库，契约测试在同一个包
// 的 contract_*_test.go 里。
//
// # 建表与表结构断言
//
// Open 对本层的全部行类型跑 AutoMigrate，再做表结构断言：查当前 schema 里本层每张表的主键、索引、外键与 CHECK，逐项
// 比对 schema.go 里的期望值，多出来的也算不一致；不一致就拒绝启动，逐条写出差在哪里、该怎么改。AutoMigrate 有两个
// 不报错的盲区：复合索引漏标一个列的 tag，会静默建成单列索引；同名的索引或约束已经存在，就永远不会按新定义更新。
// 两者在编译、测试、启动时都不报错，断言是唯一的防线。所以要改已有的索引或约束，得先写一步删掉旧定义的升级动作，
// 再改期望值。
//
// # 数据怎么存
//
// 列名由 GORM 按字段名转成 snake_case；属性、制品、提供的集合这类嵌套的 model 值存成 jsonb，nil 存成 JSON 的
// null：写进去什么就读出什么，nil 与空的 map、切片读回来也不同。列表一律按字节序排（COLLATE "C"），不随库的排序
// 规则变。PostgreSQL 的 text 与 jsonb 不收 NUL 和非法的 UTF-8，所以写入前先查文本存不存得下；
// 存不下的键不可能登记过，按它查找、过滤都当作没有。
//
// 每个写操作是一个事务：先对要改的行加写锁（SELECT ... FOR UPDATE）、比对 Rev，再写动作本身，最后写审计。数据库的
// 约束是并发下的最后一道防线，撞上时由 errors.go 按约束名翻成契约的错误。
//
// 写事务的隔离级别固定为 READ COMMITTED，由 write 开事务时指定，不取库、角色或 DSN 上的默认值（spec §6.4）。并发下
// 的写法依赖这一级的两条性质：FOR UPDATE 与唯一约束的检查会等对方提交，之后读到的是对方写下的最新数据；每条语句都看得
// 到在它之前提交的数据。主机与 ISO 的乐观锁、hook 与模板版本的写入、内置内容的装载、任务推进、事件 Seq 的分配，以及
// 建任务时加锁之后重跑的准入都靠它；默认值被改成 repeatable read 或 serializable 时，这些地方等到的会是 SQLSTATE 40001。
//
// # 测试库怎么准备
//
// 测试连一个专用的库：建一个不是超级用户的角色，以及归它所有的库（本机用 lober_test 角色与 lober_test 库）。测试
// 只在这个库里建删 schema，降权是为了让写错的 DROP 碰不到别的库。
//
//	CREATE ROLE lober_test LOGIN PASSWORD '<口令>';
//	CREATE DATABASE lober_test OWNER lober_test;
//
// DSN 先读环境变量 LOBER_TEST_PG_DSN；没设时，从模块根目录逐级往上找 .pgtest.dsn 文件，内容是一行 DSN，如
// postgres://lober_test:<口令>@127.0.0.1:5432/lober_test?sslmode=disable。这个文件带口令，已写进 .gitignore。两处
// 都没有时测试直接失败，不跳过：跳过会让 make check 在存储层零覆盖的情况下变绿。
//
// 每次 Open 新建一个 lober_t_ 加随机串的 schema，DSN 的 search_path 只指向它，用完连同里面的一切删掉，所以几个人
// 同时跑测试也互不干扰。测试进程中途被杀时会留下 schema，可以手工 DROP SCHEMA lober_t_... CASCADE。审计的失败
// 注入（pgstore_test.go 的 faultActor）是测试 schema 里审计表上的一个触发器，Open 建的表上没有这条路。
package pgstore
