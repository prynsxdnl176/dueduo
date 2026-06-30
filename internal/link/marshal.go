package link

import (
	"github.com/dobyte/due/v2/core/buffer"
	"google.golang.org/protobuf/proto"
)

// boundedBytesPool 是序列化层专用有界字节池。
//
// 专供 marshalPooled 将 proto.Marshal 的产物追加到池化 *Bytes，消除
// encoding/proto.Codec.Marshal 内部 proto.Marshal 的裸 []byte 分配（该产物
// 旧路径经 NocopyNode{block: []byte} 挂载，Release=ignore 全交 GC）。
//
// maxRetain=64KiB：超过此容量的序列化缓冲不归池（交 GC），避免大 repeated
// （如列表 resp）产物常驻池导致内存放大。阈值可按真实消息尺寸分布调优。
// grade=16 使分级上限=64KiB=maxRetain，
// 超过 64KiB 的 n 走 standalone 分配后必被 Put 丢弃。
//
// 不复用 core/buffer.defaultBytesPool：后者无丢弃策略，会改变 due 其余 buffer
// 用户语义。
var boundedBytesPool = buffer.NewBoundedBytesPool(16, 64*1024)

// marshalOpts 复用零值 MarshalOptions，语义等价 proto.Marshal（AllowPartial=false）。
var marshalOpts = proto.MarshalOptions{}

// marshalPooled 将 proto 消息序列化追加到 BoundedBytesPool 取出的 *Bytes。
//
// 用 proto.Size 取精确长度 → Get(cap>=n, off=0) → MarshalAppend(Writable)。
// cap>=n 保证 MarshalAppend 不扩容，返回的 out 与 b.buf 同底层数组；
// SetLen(len(out)) 后 b.Bytes() 即序列化产物。产物经 NocopyNode{block: *Bytes}
// 挂载，网络写出后由 NocopyBuffer.Release（delay 计数器控制）级联释放，
// b.Release() 归 BoundedBytesPool（cap 超限丢弃）。
//
// 调用方不应自行 Release 返回的 *Bytes；其生命周期绑定到挂载它的 NocopyBuffer。
func marshalPooled(pm proto.Message) (*buffer.Bytes, error) {
	n := proto.Size(pm)
	b := boundedBytesPool.Get(n)

	out, err := marshalOpts.MarshalAppend(b.Writable(), pm)
	if err != nil {
		b.Release()
		return nil, err
	}

	b.SetLen(len(out))
	return b, nil
}
