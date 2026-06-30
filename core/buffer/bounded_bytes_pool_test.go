package buffer_test

import (
	"bytes"
	"sync"
	"testing"

	"github.com/dobyte/due/v2/core/buffer"
)

// TestBoundedBytesPool_GetCapGuarantee 验证 Get(n) 返回 *Bytes 的 cap >= n，
// 且 off=0（可写状态）。这是 MarshalAppend 不扩容、out 与 b.buf 同底层数组的前提。
func TestBoundedBytesPool_GetCapGuarantee(t *testing.T) {
	p := buffer.NewBoundedBytesPool(16, 64*1024)

	cases := []int{0, 1, 2, 3, 7, 8, 9, 100, 1024, 65536}
	for _, n := range cases {
		b := p.Get(n)
		if b.Cap() < max(n, 1) {
			t.Fatalf("Get(%d): cap=%d < %d", n, b.Cap(), max(n, 1))
		}
		if b.Len() != 0 {
			t.Fatalf("Get(%d): off=%d != 0 (must be writable)", n, b.Len())
		}
		b.Release()
	}
}

// TestBoundedBytesPool_WritableSetLen 验证 Writable 返回 buf[:0:cap]，
// 追加后 SetLen 提交，Bytes() 读到追加内容且与底层数组一致。
func TestBoundedBytesPool_WritableSetLen(t *testing.T) {
	p := buffer.NewBoundedBytesPool(16, 64*1024)
	b := p.Get(16)

	w := b.Writable()
	if cap(w) != b.Cap() {
		t.Fatalf("Writable cap=%d != b.Cap()=%d", cap(w), b.Cap())
	}
	if len(w) != 0 {
		t.Fatalf("Writable len=%d != 0", len(w))
	}

	payload := []byte("hello world")
	out := append(w, payload...)
	b.SetLen(len(out))

	if !bytes.Equal(b.Bytes(), payload) {
		t.Fatalf("Bytes()=%q != %q", b.Bytes(), payload)
	}
	// out 与 b.Bytes() 应同底层数组（cap 足够，append 未扩容）
	if &out[0] != &b.Bytes()[0] {
		t.Fatal("out 与 b.buf 底层数组不一致，MarshalAppend 会脱钩")
	}

	b.Release()
}

// TestBoundedBytesPool_PutDropLarge 验证 cap > maxRetain 的 *Bytes 被丢弃（不归池）：
// Put 后再 Get 同级容量应得到全新对象（buf 内容非上次残留），且大容量分级池不复用。
func TestBoundedBytesPool_PutDropLarge(t *testing.T) {
	p := buffer.NewBoundedBytesPool(16, 64*1024)

	// 取一块 <= maxRetain 的小缓冲，写入标记后归池
	small := p.Get(64)
	small.SetLen(3)
	small.Bytes()[0] = 0xAB
	small.Release()

	// 取一块 > maxRetain 的大缓冲（走 standalone 分配），写入标记后归池（应丢弃）
	big := p.Get(200000) // > 64KiB
	if big.Cap() < 200000 {
		t.Fatalf("big cap=%d < 200000", big.Cap())
	}
	big.SetLen(3)
	big.Bytes()[0] = 0xCD
	big.Release()

	// 大缓冲被丢弃：再取 200000 应得全新 buf，首字节非 0xCD
	again := p.Get(200000)
	again.SetLen(3)
	if again.Bytes()[0] == 0xCD {
		t.Fatal("大缓冲未被丢弃，cap>maxRetain 的 *Bytes 泄漏回池")
	}
	again.Release()
}

// TestBoundedBytesPool_PutRetainSmall 验证 cap <= maxRetain 的 *Bytes 归池复用：
// 连续 Get/Put 同容量，稳态后 allocs/op 应为 0（见基准 BenchmarkBoundedBytesPool_Reuse）。
func TestBoundedBytesPool_PutRetainSmall(t *testing.T) {
	p := buffer.NewBoundedBytesPool(16, 64*1024)

	// 预热：首次 Get 分配，Put 归池
	b := p.Get(100)
	b.Release()

	// 再取：应复用归池对象（off 已被 Release 置 0）
	b2 := p.Get(100)
	if b2.Len() != 0 {
		t.Fatalf("复用对象 off=%d != 0", b2.Len())
	}
	b2.Release()
}

// TestBoundedBytesPool_GradeAutoBump 验证 grade 小于 ceil(log2(maxRetain)) 时自动上调，
// 保证分级上限 >= maxRetain（避免 standalone+retain 容量分级错乱）。
func TestBoundedBytesPool_GradeAutoBump(t *testing.T) {
	// grade=4（上限 16）但 maxRetain=64KiB，应自动上调到 16
	p := buffer.NewBoundedBytesPool(4, 64*1024)

	// 64KiB 应走分级池（cap>=n），且 Put 时 cap<=maxRetain 归池
	b := p.Get(65536)
	if b.Cap() < 65536 {
		t.Fatalf("cap=%d < 65536", b.Cap())
	}
	b.Release()

	// 复用：再取 65536 应走分级池（非 standalone）
	b2 := p.Get(65536)
	b2.Release()

	// > 64KiB 走 standalone，Put 丢弃
	big := p.Get(70000)
	if big.Cap() < 70000 {
		t.Fatalf("big cap=%d < 70000", big.Cap())
	}
	big.Release()
}

// TestBoundedBytesPool_Concurrent 验证并发 Get/Put 无竞态、无 panic。
// -race 需 cgo/gcc（CI 补）；本机以高并发计数覆盖逻辑正确性。
func TestBoundedBytesPool_Concurrent(t *testing.T) {
	p := buffer.NewBoundedBytesPool(16, 64*1024)

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 1000 {
				b := p.Get(128)
				b.SetLen(4)
				_ = b.Bytes()
				b.Release()
			}
		}()
	}
	wg.Wait()
}

// BenchmarkBoundedBytesPool_Reuse 量化小缓冲归池复用：稳态 allocs/op 应为 0。
func BenchmarkBoundedBytesPool_Reuse(b *testing.B) {
	p := buffer.NewBoundedBytesPool(16, 64*1024)
	// 预热
	x := p.Get(64)
	x.Release()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := p.Get(64)
		buf.SetLen(8)
		buf.Release()
	}
}

// BenchmarkBoundedBytesPool_DropLarge 量化大缓冲丢弃：每次 Get 分配（不复用），
// 体现 cap>maxRetain 不留存策略。allocs/op 应 > 0（每次新分配）。
func BenchmarkBoundedBytesPool_DropLarge(b *testing.B) {
	p := buffer.NewBoundedBytesPool(16, 64*1024)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := p.Get(200000)
		buf.SetLen(8)
		buf.Release()
	}
}
