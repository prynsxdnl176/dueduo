package buffer_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/dobyte/due/v2/core/buffer"
	"github.com/dobyte/due/v2/utils/xrand"
)

type User struct {
	ID  int32
	Age int8
}

func TestNewBuffer(t *testing.T) {
	buff := &bytes.Buffer{}
	buff.Grow(2)

	binary.Write(buff, binary.BigEndian, int16(2))

	fmt.Println(buff.Bytes())

	writer := buffer.NewWriterWithCapacity(2)
	writer.WriteInt16s(binary.BigEndian, int16(2))

	fmt.Println(writer.Bytes())

	writer.Release()
	writer.WriteInt16s(binary.BigEndian, int16(20))
	writer.WriteFloat32s(binary.BigEndian, 5.2)

	fmt.Println(writer.Bytes())

	data := writer.Bytes()

	reader := buffer.NewReader(data)
	v1, _ := reader.ReadInt16(binary.BigEndian)
	fmt.Println(v1)
	v2, _ := reader.ReadFloat32(binary.BigEndian)
	fmt.Println(v2)
}

func BenchmarkBuffer1(b *testing.B) {
	data := []byte(xrand.Letters(1024))

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buff := &bytes.Buffer{}
		buff.Grow(1024)
		binary.Write(buff, binary.BigEndian, data)
		buff.Reset()
	}
}

func BenchmarkBuffer2(b *testing.B) {
	writer := buffer.NewWriterWithCapacity(8)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		writer.WriteInt64s(binary.BigEndian, 2)
		writer.Release()
	}
}

func BenchmarkNocopyBuffer_Malloc(b *testing.B) {
	data := []byte(xrand.Letters(1024))

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := buffer.NewNocopyBuffer()
		buf.Mount(data)
		buf.Release()
	}
}

// BenchmarkNocopyBuffer_MountBytes 对比挂载 *Bytes:
// 指针装箱到 any 不分配，NocopyBuffer/NocopyNode 经 sync.Pool 复用，
// 稳态目标 0 allocs/op。与 BenchmarkNocopyBuffer_Malloc（挂载 []byte，含
// []byte→any 两次装箱）形成对比，说明选用 *Bytes 而非裸 []byte 的理由。
func BenchmarkNocopyBuffer_MountBytes(b *testing.B) {
	bp := buffer.NewBoundedBytesPool(16, 64*1024)
	// 预热池
	warm := buffer.NewNocopyBuffer()
	wp := bp.Get(16)
	wp.SetLen(16)
	warm.Mount(wp)
	warm.Release()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pb := bp.Get(16)
		pb.SetLen(16)
		buf := buffer.NewNocopyBuffer(pb)
		buf.Release()
	}
}

func TestNewBuffer2(t *testing.T) {
	buff := buffer.NewNocopyBuffer()

	writer1 := buff.MallocWriter(8)
	writer1.WriteInt64s(binary.BigEndian, 2)

	writer2 := buff.MallocWriter(8)
	writer2.WriteInt64s(binary.BigEndian, 3)

	t.Log(buff.Len())
	t.Log(buff.Len())

	buff.Visit(func(node *buffer.NocopyNode) bool {
		t.Log(node.Bytes())
		return true
	})

	buff.Release()

	fmt.Println(buff.Bytes())

}

func TestNocopyBuffer_Malloc(t *testing.T) {
	buff := buffer.NewNocopyBuffer()

	buff.MallocWriter(10)

	buff.MallocWriter(250)
}

func TestNocopyBuffer_Mount(t *testing.T) {
	buff1 := buffer.NewNocopyBuffer()

	writer1 := buff1.MallocWriter(8)
	writer1.WriteInt64s(binary.BigEndian, 1)

	writer2 := buff1.MallocWriter(8)
	writer2.WriteInt64s(binary.BigEndian, 2)

	buff2 := buffer.NewNocopyBuffer()

	writer3 := buff2.MallocWriter(8)
	writer3.WriteInt64s(binary.BigEndian, 3)

	writer4 := buff2.MallocWriter(8)
	writer4.WriteInt64s(binary.BigEndian, 4)

	buff1.Mount(buff2, buffer.Head)

	fmt.Println(buff1.Bytes())
}

func TestNocopyBuffer_Release(t *testing.T) {
	buff1 := buffer.NewNocopyBuffer()
	buff1.Delay(2)

	writer1 := buff1.MallocWriter(8)
	writer1.WriteInt64s(binary.BigEndian, 1)

	fmt.Println(buff1.Bytes())

	fmt.Println()

	{
		buff2 := buffer.NewNocopyBuffer()

		writer2 := buff2.MallocWriter(8)
		writer2.WriteInt64s(binary.BigEndian, 2)

		fmt.Println(buff2.Bytes())

		buff2.Mount(buff1, buffer.Head)

		fmt.Println(buff2.Bytes())

		buff2.Release()

		fmt.Println(buff2.Bytes())
	}

	fmt.Println()

	{
		buff3 := buffer.NewNocopyBuffer()

		writer3 := buff3.MallocWriter(8)
		writer3.WriteInt64s(binary.BigEndian, 3)

		fmt.Println(buff3.Bytes())

		buff3.Mount(buff1, buffer.Head)

		fmt.Println(buff3.Bytes())

		buff3.Release()

		fmt.Println(buff3.Bytes())
	}
}
