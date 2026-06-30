package buffer

import (
	"sync"
	"sync/atomic"
)

// nocopyBufferPool 复用 *NocopyBuffer，消除 NewNocopyBuffer 中的 &NocopyBuffer{}
// 字面量堆分配。Release 在 delay 计数器归零并清空链表字段后归池；
// acquireNocopyBuffer 取出时重置 delay/released 原子与链表字段。
var nocopyBufferPool = sync.Pool{
	New: func() any { return &NocopyBuffer{} },
}

// acquireNocopyBuffer 从池取一个干净的 NocopyBuffer。
//
// 必须重置：delay（防上个使用者的延迟计数残留导致本次 Release 提前/延后）、
// released（防 CAS 守卫误判）、len/num/链表指针（对齐 NewNocopyBuffer 初值）。
func acquireNocopyBuffer() *NocopyBuffer {
	b := nocopyBufferPool.Get().(*NocopyBuffer)
	b.delay.Store(0)
	b.released.Store(false)
	b.len = -1
	b.num = 0
	b.head = nil
	b.tail = nil
	b.prev = nil
	b.next = nil
	return b
}

type NocopyBuffer struct {
	len      int          // 字节数
	num      int          // 节点数
	head     any          // 头节点
	tail     any          // 尾节点
	prev     any          // 上一个节点
	next     any          // 下一个节点
	delay    atomic.Int32 // 延迟释放点
	released atomic.Bool  // 已释放
}

var _ Buffer = &NocopyBuffer{}

func NewNocopyBuffer(blocks ...any) *NocopyBuffer {
	buf := acquireNocopyBuffer()

	for _, block := range blocks {
		buf.Mount(block)
	}

	return buf
}

// Len 获取字节长度
func (b *NocopyBuffer) Len() int {
	if b.len >= 0 {
		return b.len
	}

	size := 0

	for node := b.head; node != nil; {
		switch n := node.(type) {
		case *NocopyNode:
			size += n.Len()
			node = n.next
		case *NocopyBuffer:
			size += n.Len()
			node = n.next
		default:
			node = nil
		}
	}

	b.len = size

	return size
}

// Mount 挂载块到Buffer上
func (b *NocopyBuffer) Mount(block any, whence ...Whence) {
	switch v := block.(type) {
	case []byte:
		n := acquireNocopyNode()
		n.block = v
		if len(whence) > 0 && whence[0] == Head {
			b.addToHead(n)
		} else {
			b.addToTail(n)
		}
	case *Bytes:
		n := acquireNocopyNode()
		n.block = v
		if len(whence) > 0 && whence[0] == Head {
			b.addToHead(n)
		} else {
			b.addToTail(n)
		}
	case *Writer:
		n := acquireNocopyNode()
		n.block = v
		if len(whence) > 0 && whence[0] == Head {
			b.addToHead(n)
		} else {
			b.addToTail(n)
		}
	default:
		if len(whence) > 0 && whence[0] == Head {
			b.addToHead(v)
		} else {
			b.addToTail(v)
		}
	}
}

// MallocBytes 分配一块内存给Bytes
func (b *NocopyBuffer) MallocBytes(cap int, whence ...Whence) *Bytes {
	block := MallocBytes(cap)

	b.Mount(block)

	return block
}

// MallocWriter 分配一块内存给Writer
func (b *NocopyBuffer) MallocWriter(cap int, whence ...Whence) *Writer {
	block := MallocWriter(cap)

	b.Mount(block)

	return block
}

// Visit 迭代
func (b *NocopyBuffer) Visit(fn func(node *NocopyNode) bool) bool {
	for node := b.head; node != nil; {
		switch n := node.(type) {
		case *NocopyNode:
			next := n.next

			if !fn(n) {
				return false
			}

			node = next
		case *NocopyBuffer:
			next := n.next

			if !n.Visit(fn) {
				return false
			}

			node = next
		default:
			return false
		}
	}

	return true
}

// Bytes 获取字节
func (b *NocopyBuffer) Bytes() []byte {
	if b == nil {
		return nil
	}

	switch b.num {
	case 0:
		return nil
	case 1:
		switch h := b.head.(type) {
		case *NocopyNode:
			return h.Bytes()
		case *NocopyBuffer:
			return h.Bytes()
		default:
			return nil
		}
	default:
		bytes := make([]byte, 0, b.Len())

		for node := b.head; node != nil; {
			switch n := node.(type) {
			case *NocopyNode:
				bytes = append(bytes, n.Bytes()...)
				node = n.next
			case *NocopyBuffer:
				bytes = append(bytes, n.Bytes()...)
				node = n.next
			default:
				return bytes
			}
		}

		return bytes
	}
}

// Delay 设置延迟释放点
func (b *NocopyBuffer) Delay(delay int32) {
	b.delay.Store(delay)
}

// Release 释放
func (b *NocopyBuffer) Release() {
	if b.delay.Add(-1) > 0 {
		return
	}

	if !b.released.CompareAndSwap(false, true) {
		return
	}

	for node := b.head; node != nil; {
		switch n := node.(type) {
		case *NocopyNode:
			next := n.next
			n.Release()
			node = next
		case *NocopyBuffer:
			next := n.next
			n.Release()
			node = next
		}
	}
	b.len = -1
	b.num = 0
	b.head = nil
	b.tail = nil
	b.prev = nil
	b.next = nil

	nocopyBufferPool.Put(b)
}

// 添加到头部
func (b *NocopyBuffer) addToHead(node any) {
	if node == nil {
		return
	}

	switch n := node.(type) {
	case *NocopyNode:
		if b.head == nil {
			b.head = n
			b.tail = n
		} else {
			n.next = b.head

			switch h := b.head.(type) {
			case *NocopyNode:
				h.prev = n
				b.head = n
			case *NocopyBuffer:
				h.prev = n
				b.head = n
			}
		}

		b.len = -1
		b.num++
	case *NocopyBuffer:
		if b.head == nil {
			b.head = n
			b.tail = n
		} else {
			n.next = b.head

			switch h := b.head.(type) {
			case *NocopyNode:
				h.prev = n
				b.head = n
			case *NocopyBuffer:
				h.prev = n
				b.head = n
			}
		}

		b.len = -1
		b.num += n.num
	}
}

// 添加到尾部
func (b *NocopyBuffer) addToTail(node any) {
	if node == nil {
		return
	}

	switch n := node.(type) {
	case *NocopyNode:
		if b.tail == nil {
			b.head = n
			b.tail = n
		} else {
			n.prev = b.tail

			switch t := b.tail.(type) {
			case *NocopyNode:
				t.next = n
				b.tail = n
			case *NocopyBuffer:
				t.next = n
				b.tail = n
			}
		}

		b.len = -1
		b.num++
	case *NocopyBuffer:
		if b.tail == nil {
			b.head = n
			b.tail = n
		} else {
			n.prev = b.tail

			switch t := b.tail.(type) {
			case *NocopyNode:
				t.next = n
				b.tail = n
			case *NocopyBuffer:
				t.next = n
				b.tail = n
			}
		}

		b.len = -1
		b.num += n.num
	}
}
