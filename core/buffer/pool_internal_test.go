package buffer

import (
	"sync"
	"sync/atomic"
	"testing"
)

// TestNocopyBufferPool_ReusesInstances 验证 *NocopyBuffer 与 *NocopyNode 经
// sync.Pool 复用：稳态下 New 闭包不再被调用（New 计数增量近 0）。
//
// 这是池化生效的回归守卫。注意：Mount([]byte) 仍含 []byte→any 装箱分配
// （固有于 any 类型 block 存储，非池化范畴），本测试只断言对象实例复用，
// 不断言零分配。
func TestNocopyBufferPool_ReusesInstances(t *testing.T) {
	var bufNew, nodeNew int64

	origBufPool := nocopyBufferPool
	origNodePool := nocopyNodePool
	t.Cleanup(func() {
		nocopyBufferPool = origBufPool
		nocopyNodePool = origNodePool
	})

	nocopyBufferPool = sync.Pool{
		New: func() any { atomic.AddInt64(&bufNew, 1); return &NocopyBuffer{} },
	}
	nocopyNodePool = sync.Pool{
		New: func() any { atomic.AddInt64(&nodeNew, 1); return &NocopyNode{} },
	}

	data := []byte("reuse-guard")

	// 预热：首次 Get 触发 New
	b := NewNocopyBuffer()
	b.Mount(data)
	b.Release()

	warmBuf := atomic.LoadInt64(&bufNew)
	warmNode := atomic.LoadInt64(&nodeNew)

	// 稳态 1000 次：New 增量应为 0（池复用），允许 GC 偶发清池的少量回退
	for range 1000 {
		bb := NewNocopyBuffer()
		bb.Mount(data)
		bb.Release()
	}

	if inc := atomic.LoadInt64(&bufNew) - warmBuf; inc > 2 {
		t.Errorf("*NocopyBuffer New 增量 %d > 2，池未复用实例", inc)
	}
	if inc := atomic.LoadInt64(&nodeNew) - warmNode; inc > 2 {
		t.Errorf("*NocopyNode New 增量 %d > 2，池未复用实例", inc)
	}
}
