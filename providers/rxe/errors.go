//go:build linux

package rxe

import "errors"

var (
	errProviderInUse  = errors.New("rxe: provider already serves a device")
	errProviderClosed = errors.New("rxe: device closed")

	errForeignCQ = errors.New("rxe: CQ has no rxe state")
	errCQClosed  = errors.New("rxe: CQ was destroyed or its device closed")

	errForeignQP = errors.New("rxe: QP has no rxe state")
	errQPClosed  = errors.New("rxe: QP was destroyed or its device closed")
	errQPReset   = errors.New("rxe: post receive: QP is in the RESET state")

	errQueueFull       = errors.New("rxe: work queue full")
	errAtomicAlignment = errors.New("atomic operations need an 8-byte, 8-byte aligned buffer")
)
