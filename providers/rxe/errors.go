//go:build linux

package rxe

import "errors"

var (
	errProviderInUse = errors.New("rxe: provider already serves a device")

	errForeignCQ = errors.New("rxe: CQ has no rxe state")
	errForeignQP = errors.New("rxe: QP has no rxe state")
	errQPReset   = errors.New("rxe: post receive: QP is in the RESET state")

	errQueueFull       = errors.New("work queue full")
	errAtomicAlignment = errors.New("atomic operations need an 8-byte, 8-byte aligned buffer")
)
