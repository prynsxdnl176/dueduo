package buffer

import (
	"math"
	"sync"
)

var defaultBytesPool = NewBytesPool(32)

// MallocBytes 从字节池分配字节
func MallocBytes(cap int) *Bytes {
	return defaultBytesPool.Get(cap)
}

type BytesPool struct {
	pools []*sync.Pool
}

// syncPoolReleaser 将 *sync.Pool 适配为 bytesReleaser，保持 BytesPool 原有
// “无丢弃、分级复用”语义。*Bytes.Release 经接口分发到此处，最终落到对应分级池。
type syncPoolReleaser struct{ Pool *sync.Pool }

func (r *syncPoolReleaser) Put(b *Bytes) { r.Pool.Put(b) }

// NewBytesPool 分级创建字节池
func NewBytesPool(grade int) *BytesPool {
	p := &BytesPool{}
	p.pools = make([]*sync.Pool, grade+1)

	for i := range grade + 1 {
		cap := 1 << i
		pool := &sync.Pool{}
		pool.New = func() any { return &Bytes{buf: make([]byte, cap), off: cap, pool: &syncPoolReleaser{Pool: pool}} }
		p.pools[i] = pool
	}

	return p
}

// NewBytesPoolWithCapacity 以指定容量创建字节池
func NewBytesPoolWithCapacity(cap int) *BytesPool {
	return NewBytesPool(int(math.Ceil(math.Log2(float64(cap)))))
}

// Get 获取
func (p *BytesPool) Get(cap int) *Bytes {
	pool := p.getPool(cap)

	if pool == nil {
		return nil
	}

	b := pool.Get().(*Bytes)
	b.off = cap
	b.released.Store(false)

	return b
}

// Put 放回
func (p *BytesPool) Put(b *Bytes) {
	pool := p.getPool(b.Cap())

	if pool == nil {
		return
	}

	pool.Put(b)
}

// 获取对象池
func (p *BytesPool) getPool(cap int) *sync.Pool {
	if len(p.pools) == 0 {
		return nil
	}

	i := min(int(math.Ceil(math.Log2(float64(cap)))), len(p.pools)-1)

	return p.pools[i]
}
