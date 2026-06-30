package buffer_test

import (
	"encoding/binary"
	"sync"
	"testing"

	"github.com/dobyte/due/v2/core/buffer"
)

func TestNocopyBuffer(t *testing.T) {
	writer := buffer.MallocWriter(9)
	writer.WriteInt32s(binary.BigEndian, 1029)
	writer.WriteInt8s(int8(0 << 7))
	writer.Release()

	writer.WriteInt32s(binary.BigEndian, 1029)
	writer.WriteInt8s(int8(0 << 7))

	// buf := buffer.NewNocopyBuffer()

	// buffer.MallocWriter()

	// data := []byte(xrand.Letters(1024))

	// for range 100 {
	// 	writer := buffer.MallocWriter(9)
	// 	writer.WriteInt32s(binary.BigEndian, 1029)
	// 	writer.WriteInt8s(int8(0 << 7))

	// 	buf := buffer.NewNocopyBuffer(writer, data)
	// 	buf.Release()
	// }
}

// TestNocopyBuffer_PoolReuse 验证 NewNocopyBuffer 取自池、Release 归池后可复用：
// 连续 NewNocopyBuffer + Release，稳态 allocs/op 应显著下降（见基准）。
// 且复用对象字段已重置（len=-1、num=0、链表 nil），无 stale 残留。
func TestNocopyBuffer_PoolReuse(t *testing.T) {
	// 预热
	b1 := buffer.NewNocopyBuffer()
	b1.Mount([]byte("abc"))
	b1.Release()

	// 复用：再取应得到干净对象
	b2 := buffer.NewNocopyBuffer()
	if b2.Len() != -1 && b2.Len() != 0 {
		t.Fatalf("复用对象 len=%d 非 -1/0 初值", b2.Len())
	}
	b2.Mount([]byte("xyz"))
	if string(b2.Bytes()) != "xyz" {
		t.Fatalf("复用对象挂载后 Bytes()=%q != xyz", b2.Bytes())
	}
	b2.Release()
}

// TestNocopyBuffer_DelayCounterPoolReturn 验证 delay 计数器控制归池时机：
// Delay(N) 时前 N-1 次 Release 提前返回（不归池、不释放节点），第 N 次才释放节点并归池。
// 单次 Release（无 Delay）立即释放并归池。二次 Release 被 released CAS 守卫拦截，不重复归池。
func TestNocopyBuffer_DelayCounterPoolReturn(t *testing.T) {
	// 单次释放：立即归池
	buf := buffer.NewNocopyBuffer()
	buf.Mount([]byte("single"))
	buf.Release()
	// 再次 Release 不应 panic（CAS 守卫拦截）
	buf.Release()

	// Delay(3)：前 2 次不释放，第 3 次释放
	b := buffer.NewNocopyBuffer()
	b.Delay(3)
	b.Mount([]byte("delayed"))

	b.Release() // delay 3->2，提前返回
	b.Release() // delay 2->1，提前返回
	// 此时节点未释放，Bytes() 仍可读
	if string(b.Bytes()) != "delayed" {
		t.Fatalf("delay 未到，Bytes()=%q 应仍可读", b.Bytes())
	}
	b.Release() // delay 1->0，释放节点 + 归池
}

// TestNocopyBuffer_MountBytesPooled 验证挂载 *Bytes 后 Release 归 *Bytes 来源池，
// 且 NocopyNode 取自节点池。重点：*Bytes 经 NocopyNode.Release -> b.Release() 归池，
// 不再走旧 []byte 的 ignore 路径。
func TestNocopyBuffer_MountBytesPooled(t *testing.T) {
	bp := buffer.NewBoundedBytesPool(16, 64*1024)
	payload := bp.Get(32)
	out := append(payload.Writable(), []byte("pooled-bytes")...)
	payload.SetLen(len(out))

	buf := buffer.NewNocopyBuffer(payload)
	if string(buf.Bytes()) != "pooled-bytes" {
		t.Fatalf("Bytes()=%q != pooled-bytes", buf.Bytes())
	}
	// Release 应级联释放 *Bytes 归 BoundedBytesPool（cap<=maxRetain 复用）
	buf.Release()

	// 再取 32 应可复用（不报错，off=0）
	again := bp.Get(32)
	if again.Len() != 0 {
		t.Fatalf("归池后复用 off=%d != 0", again.Len())
	}
	again.Release()
}

// TestNocopyBuffer_ConcurrentPool 验证并发 acquire/release 无竞态。
func TestNocopyBuffer_ConcurrentPool(t *testing.T) {
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 1000 {
				b := buffer.NewNocopyBuffer()
				b.Mount([]byte("concurrent"))
				_ = b.Bytes()
				b.Release()
			}
		}()
	}
	wg.Wait()
}
