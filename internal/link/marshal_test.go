package link

import (
	"bytes"
	"testing"

	"github.com/dobyte/due/v2/packet"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// TestMarshalPooled_EquivProtoMarshal 验证 marshalPooled 产出的字节与
// proto.Marshal 完全一致（MarshalAppend 不扩容、out 与 b.buf 同底层数组），
// 且 *Bytes 的 cap >= 产物长度、off 正确提交。
func TestMarshalPooled_EquivProtoMarshal(t *testing.T) {
	cases := []proto.Message{
		wrapperspb.String("hello world"),
		wrapperspb.String(""),     // 空字符串 → 非空编码（field tag + len 0）
		wrapperspb.Int64(1 << 40), // 变长整数
		&wrapperspb.StringValue{}, // 零值 → 空编码（Size=0）
	}

	for _, msg := range cases {
		want, err := proto.Marshal(msg)
		if err != nil {
			t.Fatalf("proto.Marshal: %v", err)
		}

		b, err := marshalPooled(msg)
		if err != nil {
			t.Fatalf("marshalPooled: %v", err)
		}

		got := b.Bytes()
		if !bytes.Equal(got, want) {
			t.Fatalf("marshalPooled 产出与 proto.Marshal 不一致: got=%v want=%v", got, want)
		}
		if b.Cap() < len(got) {
			t.Fatalf("cap=%d < len=%d", b.Cap(), len(got))
		}
		if b.Len() != len(got) {
			t.Fatalf("off=%d != len=%d", b.Len(), len(got))
		}
		b.Release()
	}
}

// TestMarshalPooled_PoolReuse 验证 *Bytes 归 boundedBytesPool 复用：
// 连续 marshalPooled + Release 小消息，再取应得 off 正确提交的 *Bytes，
// 且产出与 proto.Marshal 一致（复用对象无 stale 残留）。
func TestMarshalPooled_PoolReuse(t *testing.T) {
	// 预热
	b, _ := marshalPooled(wrapperspb.String("warmup"))
	b.Release()

	want, _ := proto.Marshal(wrapperspb.String("reuse"))
	b2, err := marshalPooled(wrapperspb.String("reuse"))
	if err != nil {
		t.Fatal(err)
	}
	if b2.Len() != len(b2.Bytes()) {
		t.Fatalf("复用 off=%d != len=%d", b2.Len(), len(b2.Bytes()))
	}
	if !bytes.Equal(b2.Bytes(), want) {
		t.Fatalf("复用产出与 proto.Marshal 不一致: got=%v want=%v", b2.Bytes(), want)
	}
	b2.Release()
}

// TestMarshalPooled_FullSendPathRoundTrip 验证发送路径完整往返：
// proto msg → marshalPooled(*Bytes) → packet.PackBufferWith → UnpackMessage →
// proto.Unmarshal，还原消息相等。这是 GateLinker/NodeLinker.PackMessage 特化路径
// 的核心不变量（不含网络层，序列化正确性）。
func TestMarshalPooled_FullSendPathRoundTrip(t *testing.T) {
	orig := wrapperspb.String("p2-roundtrip")

	payload, err := marshalPooled(orig)
	if err != nil {
		t.Fatal(err)
	}

	buf, err := packet.PackBufferWith(42, 100, payload)
	if err != nil {
		t.Fatal(err)
	}

	msg, err := packet.UnpackMessage(buf.Bytes())
	if err != nil {
		t.Fatalf("UnpackMessage: %v", err)
	}
	if msg.Seq != 42 || msg.Route != 100 {
		t.Fatalf("seq=%d route=%d", msg.Seq, msg.Route)
	}

	got := &wrapperspb.StringValue{}
	if err := proto.Unmarshal(msg.Buffer, got); err != nil {
		t.Fatalf("proto.Unmarshal: %v", err)
	}
	if got.Value != orig.Value {
		t.Fatalf("round-trip 失败: got=%q want=%q", got.Value, orig.Value)
	}

	buf.Release() // 级联释放 *Bytes 归池
}

// BenchmarkMarshalPooled 量化 marshalPooled 稳态分配：*Bytes 归池复用，
// 目标 allocs/op 显著低于裸 proto.Marshal（每次新 []byte）。
func BenchmarkMarshalPooled(b *testing.B) {
	msg := wrapperspb.String("benchmark-payload-1234567890")
	// 预热
	x, _ := marshalPooled(msg)
	x.Release()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf, err := marshalPooled(msg)
		if err != nil {
			b.Fatal(err)
		}
		buf.Release()
	}
}

// BenchmarkMarshalPooled_NoRelease 对比：不归池（每次新分配），体现池化收益。
func BenchmarkMarshalPooled_NoRelease(b *testing.B) {
	msg := wrapperspb.String("benchmark-payload-1234567890")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := proto.Marshal(msg); err != nil {
			b.Fatal(err)
		}
	}
}
