package buffer

import (
	"sync/atomic"
)

// bytesReleaser 是 *Bytes 归池的抽象接口。
//
// 历史上 *Bytes.pool 为 *sync.Pool，Release 直接调用 pool.Put，无丢弃钩子。
// 改为接口后，BoundedBytesPool 可注入“cap 超限即丢弃交 GC”的语义，
// 而 BytesPool 经 syncPoolReleaser 包装保持原“无丢弃、分级复用”语义不变。
type bytesReleaser interface {
	Put(*Bytes)
}

type Bytes struct {
	buf      []byte
	off      int
	pool     bytesReleaser
	released atomic.Bool
}

var _ Buffer = (*Bytes)(nil)

// NewBytes 以指定buf创建字节
func NewBytes(buf []byte) *Bytes {
	return &Bytes{buf: buf, off: len(buf)}
}

// NewBytesWithCapacity 以指定容量创建字节
func NewBytesWithCapacity(cap int) *Bytes {
	return &Bytes{buf: make([]byte, cap), off: cap}
}

// Len 返回数据长度
func (b *Bytes) Len() int {
	if b == nil {
		return 0
	} else {
		return b.off
	}
}

// Cap 返回容量
func (b *Bytes) Cap() int {
	if b == nil {
		return 0
	} else {
		return cap(b.buf)
	}
}

// Available 返回可用空间
func (b *Bytes) Available() int {
	if b == nil {
		return 0
	} else {
		return cap(b.buf) - b.off
	}
}

// Bytes 获取字节数据
func (b *Bytes) Bytes() []byte {
	if b == nil {
		return nil
	} else {
		return b.buf[:b.off]
	}
}

// Writable 返回可写切片 buf[:0:cap]，供调用方追加数据（如 proto.MarshalAppend）。
//
// 与 Bytes()（只读视图 buf[:off]）相对：Writable 始终从底层数组起始处开始，
// 长度 0、容量为 buf 的全容量，给追加操作完整空间。追加完成后须调用 SetLen
// 提交实际写入长度，使后续 Bytes() 返回写入的内容。
//
// 约定：由 BoundedBytesPool.Get 返回的 *Bytes 其 off=0（即可写状态）；
// 由 BytesPool.Get 返回的 *Bytes 其 off=cap（即只读满载状态），不适用于 Writable。
func (b *Bytes) Writable() []byte {
	if b == nil {
		return nil
	}
	return b.buf[:0:cap(b.buf)]
}

// SetLen 提交写入长度，将 off 置为 n。配合 Writable 使用：
//
//	out, _ := proto.MarshalAppend(b.Writable(), msg) // out 与 b.buf 同底层数组（cap 足够时不扩容）
//	b.SetLen(len(out))
//
// n 须满足 0 <= n <= cap(b.buf)，否则 panic（越界切片）。
func (b *Bytes) SetLen(n int) {
	b.off = n
}

// Release 释放
func (b *Bytes) Release() {
	b.off = 0

	if b.pool != nil {
		b.pool.Put(b)
	}
}
