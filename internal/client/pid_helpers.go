package client

func safeInt32FromInt(n int) int32 {
	const maxInt32 = int(^uint32(0) >> 1)
	if n > maxInt32 {
		return int32(maxInt32) //nolint:gosec // clamped to int32 max
	}
	if n < -maxInt32-1 {
		return int32(-maxInt32 - 1) //nolint:gosec // clamped to int32 min
	}
	return int32(n) //nolint:gosec // bounded to int32 range
}

func safeInt64FromUint64(n uint64) int64 {
	const maxInt64 = int64(^uint64(0) >> 1)
	if n > uint64(maxInt64) {
		return maxInt64 //nolint:gosec // clamped to int64 max
	}
	return int64(n) //nolint:gosec // bounded to int64 range
}
