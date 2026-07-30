package telemetry

func safeInt32FromPID(pid int) int32 {
	const maxInt32 = int(^uint32(0) >> 1)
	if pid > maxInt32 {
		return int32(maxInt32) //nolint:gosec // clamped to int32 max
	}
	if pid < -maxInt32-1 {
		return int32(-maxInt32 - 1) //nolint:gosec // clamped to int32 min
	}
	return int32(pid) //nolint:gosec // bounded to int32 range
}

func safeUint32FromInt(n int) uint32 {
	if n < 0 {
		return 0
	}
	const maxUint32 = ^uint32(0)
	if n > int(maxUint32) {
		return maxUint32 //nolint:gosec // clamped to uint32 max
	}
	return uint32(n) //nolint:gosec // bounded to uint32 range
}
