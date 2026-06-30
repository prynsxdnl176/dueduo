package buffer

import (
	"math"
	"sync"
)

// defaultBoundedMaxRetain 默认归池容量上限 64KiB。超过此容量的序列化缓冲
// 不归池（交 GC），避免大 repeated 产物常驻池导致内存放大
// 可按真实消息尺寸分布调优
const defaultBoundedMaxRetain = 64 * 1024

// BoundedBytesPool 是带“容量上限丢弃”语义的分级字节池。
//
// 设计动机：proto 序列化产物 []byte 经NocopyNode 挂载,
// 旧路径 Release 对 []byte = ignore → 全交 GC。改用 *Bytes +
// MarshalAppend 池化后，若大体积 repeated（如列表 resp）的序列化产物常驻分级池，
// 会造成池内存放大永不收缩。故 Put 时 cap > maxRetain 的 *Bytes 直接丢弃交 GC，
// 不归池；仅 cap <= maxRetain 的小/中缓冲复用。
//
// 不复用全局 defaultBytesPool：后者无丢弃策略，patch 会改变 due 其余 buffer
// 用户语义。BoundedBytesPool 为独立实例，专供序列化层。
//
// 与 BytesPool 的关键差异：
//   - Get(n) 返回 off=0 的 *Bytes（可写状态，配合 Writable/SetLen 做 MarshalAppend）；
//     BytesPool.Get 返回 off=cap 的 *Bytes（只读满载状态）。
//   - Put 时 cap > maxRetain 丢弃；BytesPool.Put 无条件归池。
//
// 不变量（MUST）：
//   - Get(n) 保证返回 *Bytes 的 cap >= n（否则 MarshalAppend 会扩容到新底层数组，
//     与 b.buf 脱钩，SetLen 后 Bytes() 读到旧空数组，产生空包/数据错乱）。
//   - 构造期强制 grade >= ceil(log2(maxRetain))，使分级上限 >= maxRetain；
//     超过分级上限的 n 走 standalone 分配（cap=n），其 cap > maxRetain 必被 Put 丢弃，
//     不会污染分级池的容量分级不变量。
type BoundedBytesPool struct {
	pools     []*sync.Pool
	grade     int
	maxRetain int
}

// NewBoundedBytesPool 创建分级有界池。grade 为分级数（2^0..2^grade），maxRetain
// 为归池容量上限（<=0 时取默认 64KiB）。grade 小于 ceil(log2(maxRetain)) 时自动上调。
func NewBoundedBytesPool(grade int, maxRetain int) *BoundedBytesPool {
	if maxRetain <= 0 {
		maxRetain = defaultBoundedMaxRetain
	}
	minGrade := int(math.Ceil(math.Log2(float64(maxRetain))))
	if grade < minGrade {
		grade = minGrade
	}

	p := &BoundedBytesPool{grade: grade, maxRetain: maxRetain}
	p.pools = make([]*sync.Pool, grade+1)

	for i := range grade + 1 {
		cap := 1 << i
		pool := &sync.Pool{}
		pool.New = func() any { return &Bytes{buf: make([]byte, cap), pool: p} }
		p.pools[i] = pool
	}

	return p
}

// Get 取一个 cap >= n 的 *Bytes，off=0（可写状态）。
//
// n <= 0 视为 1（空 proto 序列化产物 Size=0 的兜底）。n 超过分级上限 2^grade 时
// 直接 make 一块精确容量的 *Bytes（pool 指向本池，Put 时因 cap > maxRetain 丢弃）。
func (p *BoundedBytesPool) Get(n int) *Bytes {
	if n < 1 {
		n = 1
	}

	if ceiling := 1 << p.grade; n <= ceiling {
		b := p.getPool(n).Get().(*Bytes)
		b.off = 0
		b.released.Store(false)
		return b
	}

	return &Bytes{buf: make([]byte, n), off: 0, pool: p}
}

// Put 实现 bytesReleaser：cap > maxRetain 丢弃交 GC，否则归对应分级池。
func (p *BoundedBytesPool) Put(b *Bytes) {
	if b == nil {
		return
	}

	if b.Cap() > p.maxRetain {
		return // 丢弃，交 GC 回收
	}

	p.getPool(b.Cap()).Put(b)
}

// 获取对象池。cap 已保证 <= 2^grade（构造期强制 maxRetain <= 2^grade，
// 且仅 cap <= maxRetain 的 buf 才会到达此处）。
func (p *BoundedBytesPool) getPool(cap int) *sync.Pool {
	i := min(int(math.Ceil(math.Log2(float64(cap)))), p.grade)
	return p.pools[i]
}
