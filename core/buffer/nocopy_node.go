package buffer

import "sync"

// nocopyNodePool 复用 *NocopyNode，消除 Mount/addToHead/addToTail 中的
// &NocopyNode{} 字面量堆分配。Release 清空 prev/next/block 后归池，
// acquireNocopyNode 取出时再次置 nil（双保险）。
var nocopyNodePool = sync.Pool{
	New: func() any { return &NocopyNode{} },
}

// acquireNocopyNode 从池取一个干净节点。字段已由上次 Release 清零，此处再置 nil。
func acquireNocopyNode() *NocopyNode {
	n := nocopyNodePool.Get().(*NocopyNode)
	n.prev = nil
	n.next = nil
	n.block = nil
	return n
}

type NocopyNode struct {
	prev  any
	next  any
	block any
}

var _ Buffer = (*NocopyNode)(nil)

// Len 获取字节长度
func (n *NocopyNode) Len() int {
	if n == nil {
		return 0
	}

	switch b := n.block.(type) {
	case []byte:
		return len(b)
	case *Bytes:
		return b.Len()
	case *Writer:
		return b.Len()
	default:
		return 0
	}
}

// Bytes 获取该节点的字节数据
func (n *NocopyNode) Bytes() []byte {
	if n == nil {
		return nil
	}

	switch b := n.block.(type) {
	case []byte:
		return b
	case *Bytes:
		return b.Bytes()
	case *Writer:
		return b.Bytes()
	default:
		return nil
	}
}

// Release 释放
func (n *NocopyNode) Release() {
	if n == nil {
		return
	}

	switch b := n.block.(type) {
	case []byte:
		// ignore
	case *Bytes:
		b.Release()
	case *Writer:
		b.Release()
	}

	n.prev = nil
	n.next = nil
	n.block = nil

	nocopyNodePool.Put(n)
}
