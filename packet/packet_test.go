package packet_test

import (
	"bytes"
	"testing"

	"github.com/dobyte/due/v2/core/buffer"
	derrors "github.com/dobyte/due/v2/errors"
	"github.com/dobyte/due/v2/packet"
	"github.com/dobyte/due/v2/utils/xrand"
)

var packer = packet.NewPacker(
	packet.WithHeartbeatTime(true),
)

func TestDefaultPacker_ReadMessage(t *testing.T) {
	data, err := packer.PackMessage(&packet.Message{
		Seq:    1,
		Route:  1,
		Buffer: []byte("hello world"),
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Log(data)

	reader := bytes.NewReader(data)

	message, err := packer.ReadMessage(reader)
	if err != nil {
		t.Fatal(err)
	}

	t.Log(message)
}

func TestDefaultPacker_PackBuffer(t *testing.T) {
	buf, err := packer.PackBuffer(&packet.Message{
		Seq:    1,
		Route:  1,
		Buffer: []byte("hello world"),
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Log(buf.Bytes())

	message, err := packer.UnpackMessage(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	buf.Release()

	t.Logf("seq: %d", message.Seq)
	t.Logf("route: %d", message.Route)
	t.Logf("buffer: %s", string(message.Buffer))
}

func TestDefaultPacker_PackMessage(t *testing.T) {
	data, err := packer.PackMessage(&packet.Message{
		Seq:    1,
		Route:  1,
		Buffer: []byte("hello world"),
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Log(data)

	message, err := packer.UnpackMessage(data)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("seq: %d", message.Seq)
	t.Logf("route: %d", message.Route)
	t.Logf("buffer: %s", string(message.Buffer))
}

func TestDefaultPacker_PackHeartbeat(t *testing.T) {
	data, err := packer.PackHeartbeat()
	if err != nil {
		t.Fatal(err)
	}

	t.Log(data)

	isHeartbeat, err := packer.CheckHeartbeat(data)
	if err != nil {
		t.Fatal(err)
	}

	t.Log(isHeartbeat)
}

// TestPackBuffer_RouteUnsigned 验证 routeUnsigned=true 时 bit15=1 的路由号（33004）
// 经 PackBuffer（buffer.Writer 路径）编码、UnpackMessage 解码后零扩展读得原值。
func TestPackBuffer_RouteUnsigned(t *testing.T) {
	p := packet.NewPacker(packet.WithRouteUnsigned(true), packet.WithSeqUnsigned(true))

	buf, err := p.PackBuffer(&packet.Message{
		Seq:    33004,
		Route:  33004,
		Buffer: []byte("hello"),
	})
	if err != nil {
		t.Fatal(err)
	}

	msg, err := p.UnpackMessage(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	buf.Release()

	if msg.Route != 33004 {
		t.Fatalf("route: want 33004, got %d", msg.Route)
	}
	if msg.Seq != 33004 {
		t.Fatalf("seq: want 33004, got %d", msg.Seq)
	}
}

// TestPackMessage_RouteUnsigned 验证 routeUnsigned=true 时经 PackMessage（binary.Write 路径）往返正确。
func TestPackMessage_RouteUnsigned(t *testing.T) {
	p := packet.NewPacker(packet.WithRouteUnsigned(true), packet.WithSeqUnsigned(true))

	data, err := p.PackMessage(&packet.Message{
		Seq:    40000,
		Route:  33004,
		Buffer: []byte("hello"),
	})
	if err != nil {
		t.Fatal(err)
	}

	msg, err := p.UnpackMessage(data)
	if err != nil {
		t.Fatal(err)
	}

	if msg.Route != 33004 {
		t.Fatalf("route: want 33004, got %d", msg.Route)
	}
	if msg.Seq != 40000 {
		t.Fatalf("seq: want 40000, got %d", msg.Seq)
	}
}

// TestPackBuffer_RouteSignedOverflow 验证默认有符号语义下 route=33004 被溢出校验拒绝。
func TestPackBuffer_RouteSignedOverflow(t *testing.T) {
	p := packet.NewPacker()

	_, err := p.PackBuffer(&packet.Message{Route: 33004})
	if err != derrors.ErrRouteOverflow {
		t.Fatalf("want ErrRouteOverflow, got %v", err)
	}
}

// TestPackMessage_RouteSignedOverflow 验证 PackMessage 路径有符号溢出校验。
func TestPackMessage_RouteSignedOverflow(t *testing.T) {
	p := packet.NewPacker()

	_, err := p.PackMessage(&packet.Message{Route: 33004})
	if err != derrors.ErrRouteOverflow {
		t.Fatalf("want ErrRouteOverflow, got %v", err)
	}
}

// TestPackBuffer_SeqSignedOverflow 验证默认有符号语义下 seq=40000 被溢出校验拒绝。
func TestPackBuffer_SeqSignedOverflow(t *testing.T) {
	p := packet.NewPacker()

	_, err := p.PackBuffer(&packet.Message{Seq: 40000})
	if err != derrors.ErrSeqOverflow {
		t.Fatalf("want ErrSeqOverflow, got %v", err)
	}
}

// TestPackBuffer_RouteUnsignedBoundaries 验证 uint16 语义边界：0/65535 通过，65536/-1 溢出。
func TestPackBuffer_RouteUnsignedBoundaries(t *testing.T) {
	p := packet.NewPacker(packet.WithRouteUnsigned(true))

	cases := []struct {
		route int32
		ok    bool
	}{
		{0, true},
		{65535, true},
		{65536, false},
		{-1, false},
	}
	for _, c := range cases {
		_, err := p.PackBuffer(&packet.Message{Route: c.route})
		switch {
		case c.ok && err != nil:
			t.Errorf("route=%d: want ok, got %v", c.route, err)
		case !c.ok && err != derrors.ErrRouteOverflow:
			t.Errorf("route=%d: want ErrRouteOverflow, got %v", c.route, err)
		}
	}
}

// TestPackBuffer_RouteUnsignedMaxRoundtrip 验证 uint16 上界 65535 往返。
func TestPackBuffer_RouteUnsignedMaxRoundtrip(t *testing.T) {
	p := packet.NewPacker(packet.WithRouteUnsigned(true))

	buf, err := p.PackBuffer(&packet.Message{Route: 65535})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := p.UnpackMessage(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	buf.Release()

	if msg.Route != 65535 {
		t.Fatalf("route: want 65535, got %d", msg.Route)
	}
}

// TestPackBuffer_RouteUnsignedRegression 验证 ≤32767 的正路由号在 uint16 下读得正值（与 int16 一致）。
func TestPackBuffer_RouteUnsignedRegression(t *testing.T) {
	p := packet.NewPacker(packet.WithRouteUnsigned(true))

	buf, err := p.PackBuffer(&packet.Message{Route: 13597, Buffer: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := p.UnpackMessage(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	buf.Release()

	if msg.Route != 13597 {
		t.Fatalf("route: want 13597, got %d", msg.Route)
	}
}

// TestPackBuffer_RouteSignedNegativeRoundtrip 验证有符号语义下负路由号符号扩展往返（回归保护）。
func TestPackBuffer_RouteSignedNegativeRoundtrip(t *testing.T) {
	p := packet.NewPacker()

	buf, err := p.PackBuffer(&packet.Message{Route: -100, Seq: -1})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := p.UnpackMessage(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	buf.Release()

	if msg.Route != -100 {
		t.Fatalf("route: want -100, got %d", msg.Route)
	}
	if msg.Seq != -1 {
		t.Fatalf("seq: want -1, got %d", msg.Seq)
	}
}

func BenchmarkDefaultPacker_ReadBuffer(b *testing.B) {
	data, err := packer.PackMessage(&packet.Message{
		Seq:    1,
		Route:  1,
		Buffer: []byte(xrand.Letters(2048)),
	})
	if err != nil {
		b.Fatal(err)
	}

	reader := bytes.NewReader(data)

	b.ResetTimer()
	b.SetBytes(int64(len(data)))

	for i := 0; i < b.N; i++ {
		if buf, err := packer.ReadBuffer(reader); err != nil {
			b.Fatal(err)
		} else {
			buf.Release()
		}

		reader.Reset(data)
	}
}

func BenchmarkDefaultPacker_ReadMessage(b *testing.B) {
	data, err := packer.PackMessage(&packet.Message{
		Seq:    1,
		Route:  1,
		Buffer: []byte(xrand.Letters(2048)),
	})
	if err != nil {
		b.Fatal(err)
	}

	reader := bytes.NewReader(data)

	b.ResetTimer()
	b.SetBytes(int64(len(data)))

	for i := 0; i < b.N; i++ {
		if _, err = packer.ReadMessage(reader); err != nil {
			b.Fatal(err)
		}

		reader.Reset(data)
	}
}

// TestPackBuffer_WithBytes 验证 PackBufferWith（载荷为池化 *buffer.Bytes）与
// PackBuffer（载荷为裸 []byte）产出等价字节流，且 *Bytes 经 Release 归池。
func TestPackBuffer_WithBytes(t *testing.T) {
	bp := buffer.NewBoundedBytesPool(16, 64*1024)
	payload := []byte("hello world")

	// 池化 *Bytes 载荷
	pb := bp.Get(len(payload))
	out := append(pb.Writable(), payload...)
	pb.SetLen(len(out))

	got, err := packer.PackBufferWith(1, 1, pb)
	if err != nil {
		t.Fatal(err)
	}

	// 参照：裸 []byte 路径
	want, err := packer.PackBuffer(&packet.Message{Seq: 1, Route: 1, Buffer: payload})
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got.Bytes(), want.Bytes()) {
		t.Fatalf("PackBufferWith 与 PackBuffer 产出不一致:\n got=%v\nwant=%v", got.Bytes(), want.Bytes())
	}

	// 解包验证往返
	msg, err := packer.UnpackMessage(got.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if msg.Seq != 1 || msg.Route != 1 || string(msg.Buffer) != "hello world" {
		t.Fatalf("unpack mismatch: seq=%d route=%d buffer=%q", msg.Seq, msg.Route, msg.Buffer)
	}

	got.Release()  // 级联释放 *Bytes 归 BoundedBytesPool
	want.Release() // 释放 writer 节点
}

// TestPackBuffer_WithBytesNil 验证 payload=nil 时不 panic，产出空载荷包。
func TestPackBuffer_WithBytesNil(t *testing.T) {
	got, err := packer.PackBufferWith(1, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := packer.UnpackMessage(got.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if msg.Seq != 1 || msg.Route != 1 || len(msg.Buffer) != 0 {
		t.Fatalf("nil payload mismatch: seq=%d route=%d buffer=%v", msg.Seq, msg.Route, msg.Buffer)
	}
	got.Release()
}

// TestPackBuffer_WithBytesOverflow 验证 PackBufferWith 的 bufferBytes 上限校验。
func TestPackBuffer_WithBytesOverflow(t *testing.T) {
	p := packet.NewPacker(packet.WithBufferBytes(100))
	bp := buffer.NewBoundedBytesPool(16, 64*1024)
	big := bp.Get(200)
	big.SetLen(200)

	_, err := p.PackBufferWith(1, 1, big)
	if err != derrors.ErrMessageTooLarge {
		t.Fatalf("want ErrMessageTooLarge, got %v", err)
	}
	big.Release()
}

// BenchmarkPackBuffer_WithBytes 对比 PackBufferWith（池化 *Bytes 载荷）与
// PackBuffer（裸 []byte 载荷）。稳态下 PackBufferWith 的 *Bytes 归池复用，
// allocs/op 应低于 PackBuffer 的裸 []byte 节点路径。
func BenchmarkPackBuffer_WithBytes(b *testing.B) {
	bp := buffer.NewBoundedBytesPool(16, 64*1024)
	payload := []byte(xrand.Letters(1024))

	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pb := bp.Get(len(payload))
		out := append(pb.Writable(), payload...)
		pb.SetLen(len(out))

		buf, err := packer.PackBufferWith(1, 1, pb)
		if err != nil {
			b.Fatal(err)
		}
		buf.Release()
	}
}

func BenchmarkDefaultPacker_PackBuffer(b *testing.B) {
	buffer := []byte(xrand.Letters(1024))

	b.ResetTimer()
	b.SetBytes(int64(len(buffer)))

	for i := 0; i < b.N; i++ {
		buf, err := packer.PackBuffer(&packet.Message{
			Seq:    1,
			Route:  1,
			Buffer: buffer,
		})
		if err != nil {
			b.Fatal(err)
		}

		buf.Release()
	}
}

func BenchmarkDefaultPacker_PackMessage(b *testing.B) {
	buffer := []byte(xrand.Letters(1024))

	b.ResetTimer()
	b.SetBytes(int64(len(buffer)))

	for i := 0; i < b.N; i++ {
		_, err := packer.PackMessage(&packet.Message{
			Seq:    1,
			Route:  1,
			Buffer: buffer,
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDefaultPacker_UnpackMessage(b *testing.B) {
	buf, err := packer.PackMessage(&packet.Message{
		Seq:    1,
		Route:  1,
		Buffer: []byte(xrand.Letters(1024)),
	})
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.SetBytes(int64(len(buf)))

	for i := 0; i < b.N; i++ {
		if _, err := packer.UnpackMessage(buf); err != nil {
			b.Fatal(err)
		}
	}
}
