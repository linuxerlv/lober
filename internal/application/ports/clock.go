package ports

// Clock 返回调用方提供的秒数，应用不直接读取系统时间。
type Clock func() int64
