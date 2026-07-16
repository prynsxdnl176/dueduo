/**
 * @Author: fuxiao
 * @Email: 576101059@qq.com
 * @Date: 2022/7/7 1:31 上午
 * @Desc: TODO
 */

package gate

import (
	"context"
	"maps"
	"time"

	"github.com/dobyte/due/v2/cluster"
	"github.com/dobyte/due/v2/etc"
	"github.com/dobyte/due/v2/locate"
	"github.com/dobyte/due/v2/log"
	"github.com/dobyte/due/v2/network"
	"github.com/dobyte/due/v2/packet"
	"github.com/dobyte/due/v2/registry"
	"github.com/dobyte/due/v2/utils/xconv"
	"github.com/dobyte/due/v2/utils/xuuid"
)

// ReceiveMiddleware 网关入站消息中间件（doomscamp 边缘限流 patch）。
//
// 在 gate.proxy.deliver 单次解包（packet.UnpackMessage）后、转发 Node 前调用。
// 返回 allow=true 放行转发；返回 allow=false 则不再 nodeLinker.Deliver，由中间件
// 自行回发错误包（如 SCError）并可在违例达阈值时踢人。
//
// 透传 conn 供中间件取 RemoteIP（pre-auth 按 IP 限流）与回发/断连；
// cid/uid 单独传入（uid 在 post-login 经 Bind 填充，pre-auth 为 0）；
// msg 为已解包的 packet.Message（含 Seq/Route/Buffer）。
//
// nil（默认）= 不拦截，行为与未 patch 完全一致（向后兼容，不影响共享本 fork 的其它项目）。
type ReceiveMiddleware func(ctx context.Context, conn network.Conn, cid, uid int64, msg *packet.Message) (allow bool)

// DisconnectHook 网关断连钩子（doomscamp 边缘限流 patch）。
//
// 在 gate.handleDisconnect 既有逻辑（unbindGate + trigger Disconnect）之后调用，
// 供应用层释放 per-uid/per-cid 限流桶等资源。nil（默认）= 不调用，向后兼容。
type DisconnectHook func(conn network.Conn)

const (
	defaultName              = "gate"         // 默认名称
	defaultDispatch          = cluster.Random // 默认的无状态路由分发策略
	defaultAddr              = ":0"           // 连接器监听地址
	defaultConnNum           = 5              // 默认连接数
	defaultCallTimeout       = "3s"           // 默认调用超时时间
	defaultDialTimeout       = "3s"           // 默认拨号超时时间
	defaultDialRetryTimes    = 3              // 默认拨号重试次数
	defaultWriteTimeout      = "0s"           // 默认写入超时时间
	defaultWriteQueueSize    = 2048           // 默认写入队列大小
	defaultFaultRecoveryTime = "5s"           // 默认故障恢复时间
)

const (
	defaultIDKey                = "etc.cluster.gate.id"
	defaultNameKey              = "etc.cluster.gate.name"
	defaultDispatchKey          = "etc.cluster.gate.dispatch"
	defaultMetadataKey          = "etc.cluster.gate.metadata"
	defaultAddrKey              = "etc.cluster.gate.addr"
	defaultExposeKey            = "etc.cluster.gate.expose"
	defaultConnNumKey           = "etc.cluster.gate.connNum"
	defaultCallTimeoutKey       = "etc.cluster.gate.callTimeout"
	defaultDialTimeoutKey       = "etc.cluster.gate.dialTimeout"
	defaultDialRetryTimesKey    = "etc.cluster.gate.dialRetryTimes"
	defaultWriteTimeoutKey      = "etc.cluster.gate.writeTimeout"
	defaultWriteQueueSizeKey    = "etc.cluster.gate.writeQueueSize"
	defaultFaultRecoveryTimeKey = "etc.cluster.gate.faultRecoveryTime"
)

type Option func(o *options)

type options struct {
	ctx               context.Context   // 上下文
	id                string            // 实例ID
	name              string            // 实例名称
	server            network.Server    // 网关服务器
	locator           locate.Locator    // 用户定位器
	registry          registry.Registry // 服务注册器
	dispatch          cluster.Dispatch  // 无状态路由消息分发策略
	metadata          map[string]string // 元数据
	addr              string            // 内部RPC监听地址
	expose            bool              // 内部RPC是否暴露到公网
	connNum           int               // 内部RPC拨号连接数
	callTimeout       time.Duration     // 内部RPC调用超时时间
	dialTimeout       time.Duration     // 内部RPC拨号超时时间
	dialRetryTimes    int               // 内部RPC拨号重试次数
	writeTimeout      time.Duration     // 内部RPC写入超时时间
	writeQueueSize    int32             // 内部RPC写入队列大小
	faultRecoveryTime time.Duration     // 内部RPC故障恢复时间
	receiveMiddleware ReceiveMiddleware  // 入站消息中间件（边缘限流 patch），nil=不拦截
	disconnectHook    DisconnectHook    // 断连钩子（边缘限流 patch），nil=不调用
}

func defaultOptions() *options {
	opts := &options{}
	opts.ctx = context.Background()
	opts.expose = etc.Get(defaultExposeKey).Bool()
	opts.metadata = make(map[string]string)

	if id := etc.Get(defaultIDKey).String(); id != "" {
		opts.id = id
	} else {
		opts.id = xuuid.UUID()
	}

	if name := etc.Get(defaultNameKey, defaultName).String(); name != "" {
		opts.name = name
	} else {
		opts.name = defaultName
	}

	if addr := etc.Get(defaultAddrKey, defaultAddr).String(); addr != "" {
		opts.addr = addr
	} else {
		opts.addr = defaultAddr
	}

	if strategy := etc.Get(defaultDispatchKey).String(); strategy != "" {
		opts.dispatch = cluster.Dispatch(strategy)
	} else {
		opts.dispatch = defaultDispatch
	}

	if connNum := etc.Get(defaultConnNumKey, defaultConnNum).Int(); connNum > 0 {
		opts.connNum = connNum
	} else {
		opts.connNum = defaultConnNum
	}

	if callTimeout := etc.Get(defaultCallTimeoutKey, defaultCallTimeout).Duration(); callTimeout >= 0 {
		opts.callTimeout = callTimeout
	} else {
		opts.callTimeout = xconv.Duration(defaultCallTimeout)
	}

	if dialTimeout := etc.Get(defaultDialTimeoutKey, defaultDialTimeout).Duration(); dialTimeout >= 0 {
		opts.dialTimeout = dialTimeout
	} else {
		opts.dialTimeout = xconv.Duration(defaultDialTimeout)
	}

	if dialRetryTimes := etc.Get(defaultDialRetryTimesKey, defaultDialRetryTimes).Int(); dialRetryTimes >= 0 {
		opts.dialRetryTimes = dialRetryTimes
	} else {
		opts.dialRetryTimes = defaultDialRetryTimes
	}

	if writeTimeout := etc.Get(defaultWriteTimeoutKey, defaultWriteTimeout).Duration(); writeTimeout >= 0 {
		opts.writeTimeout = writeTimeout
	} else {
		opts.writeTimeout = xconv.Duration(defaultWriteTimeout)
	}

	if writeQueueSize := etc.Get(defaultWriteQueueSizeKey, defaultWriteQueueSize).Int32(); writeQueueSize > 0 {
		opts.writeQueueSize = writeQueueSize
	} else {
		opts.writeQueueSize = defaultWriteQueueSize
	}

	if faultRecoveryTime := etc.Get(defaultFaultRecoveryTimeKey, defaultFaultRecoveryTime).Duration(); faultRecoveryTime >= 0 {
		opts.faultRecoveryTime = faultRecoveryTime
	} else {
		opts.faultRecoveryTime = xconv.Duration(defaultFaultRecoveryTime)
	}

	if err := etc.Get(defaultMetadataKey).Scan(&opts.metadata); err != nil {
		log.Warnf("scan metadata failed: %v", err)
	}

	return opts
}

// WithID 设置实例ID
func WithID(id string) Option {
	return func(o *options) {
		if id != "" {
			o.id = id
		} else {
			log.Warnf("the specified id is empty and will be automatically ignored")
		}
	}
}

// WithName 设置实例名称
func WithName(name string) Option {
	return func(o *options) {
		if name != "" {
			o.name = name
		} else {
			log.Warnf("the specified name is empty and will be ignored")
		}
	}
}

// WithContext 设置上下文
func WithContext(ctx context.Context) Option {
	return func(o *options) {
		if ctx != nil {
			o.ctx = ctx
		} else {
			log.Warnf("the specified ctx is nil and will be ignored")
		}
	}
}

// WithServer 设置服务器
func WithServer(server network.Server) Option {
	return func(o *options) {
		if server != nil {
			o.server = server
		} else {
			log.Warnf("the specified server is nil and will be ignored")
		}
	}
}

// WithLocator 设置用户定位器
func WithLocator(locator locate.Locator) Option {
	return func(o *options) {
		if locator != nil {
			o.locator = locator
		} else {
			log.Warnf("the specified locator is nil and will be ignored")
		}
	}
}

// WithRegistry 设置服务注册器
func WithRegistry(r registry.Registry) Option {
	return func(o *options) {
		if r != nil {
			o.registry = r
		} else {
			log.Warnf("the specified registry is nil and will be ignored")
		}
	}
}

// WithDispatch 设置无状态路由消息分发策略
func WithDispatch(dispatch cluster.Dispatch) Option {
	return func(o *options) {
		if dispatch != "" {
			o.dispatch = dispatch
		} else {
			log.Warnf("the specified dispatch is empty and will be ignored")
		}
	}
}

// WithAddr 设置监听地址
func WithAddr(addr string) Option {
	return func(o *options) {
		if addr != "" {
			o.addr = addr
		} else {
			log.Warnf("the specified addr is empty and will be ignored")
		}
	}
}

// WithExpose 设置是否将内部通信地址暴露到公网
func WithExpose(expose bool) Option {
	return func(o *options) { o.expose = expose }
}

// WithConnNum 设置连接数
func WithConnNum(connNum int) Option {
	return func(o *options) {
		if connNum > 0 {
			o.connNum = connNum
		} else {
			log.Warnf("the specified connNum is less than zero and will be ignored")
		}
	}
}

// WithCallTimeout 设置调用超时时间
func WithCallTimeout(callTimeout time.Duration) Option {
	return func(o *options) {
		if callTimeout >= 0 {
			o.callTimeout = callTimeout
		} else {
			log.Warnf("the specified callTimeout is less than zero and will be ignored")
		}
	}
}

// WithDialTimeout 设置拨号超时时间
func WithDialTimeout(dialTimeout time.Duration) Option {
	return func(o *options) {
		if dialTimeout >= 0 {
			o.dialTimeout = dialTimeout
		} else {
			log.Warnf("the specified dialTimeout is less than zero and will be ignored")
		}
	}
}

// WithDialRetryTimes 设置拨号重试次数
func WithDialRetryTimes(dialRetryTimes int) Option {
	return func(o *options) {
		if dialRetryTimes >= 0 {
			o.dialRetryTimes = dialRetryTimes
		} else {
			log.Warnf("the specified dialRetryTimes is less than zero and will be ignored")
		}
	}
}

// WithWriteTimeout 设置写入超时时间
func WithWriteTimeout(writeTimeout time.Duration) Option {
	return func(o *options) {
		if writeTimeout >= 0 {
			o.writeTimeout = writeTimeout
		} else {
			log.Warnf("the specified writeTimeout is less than zero and will be ignored")
		}
	}
}

// WithWriteQueueSize 设置写入队列大小
func WithWriteQueueSize(writeQueueSize int32) Option {
	return func(o *options) {
		if writeQueueSize > 0 {
			o.writeQueueSize = writeQueueSize
		} else {
			log.Warnf("the specified writeQueueSize is less than zero and will be ignored")
		}
	}
}

// WithFaultRecoveryTime 设置故障恢复时间
func WithFaultRecoveryTime(faultRecoveryTime time.Duration) Option {
	return func(o *options) {
		if faultRecoveryTime >= 0 {
			o.faultRecoveryTime = faultRecoveryTime
		} else {
			log.Warnf("the specified faultRecoveryTime is less than zero and will be ignored")
		}
	}
}

// WithMetadata 设置元数据
func WithMetadata(metadata map[string]string) Option {
	return func(o *options) {
		if len(metadata) != 0 {
			if len(o.metadata) == 0 {
				o.metadata = make(map[string]string)
			}

			maps.Copy(o.metadata, metadata)
		} else {
			log.Warnf("the specified metadata is empty and will be ignored")
		}
	}
}

// WithReceiveMiddleware 设置入站消息中间件（边缘限流 patch）。
//
// 中间件在 deliver 单次解包后、转发 Node 前调用；返回 false 则不转发，由中间件
// 自行回发错误包。nil 视为不设置（向后兼容）。
func WithReceiveMiddleware(fn ReceiveMiddleware) Option {
	return func(o *options) {
		if fn != nil {
			o.receiveMiddleware = fn
		} else {
			log.Warnf("the specified receiveMiddleware is nil and will be ignored")
		}
	}
}

// WithDisconnectHook 设置断连钩子（边缘限流 patch）。
//
// 钩子在 handleDisconnect 既有逻辑之后调用，供应用层释放 per-uid/per-cid 限流桶。
// nil 视为不设置（向后兼容）。
func WithDisconnectHook(fn DisconnectHook) Option {
	return func(o *options) {
		if fn != nil {
			o.disconnectHook = fn
		} else {
			log.Warnf("the specified disconnectHook is nil and will be ignored")
		}
	}
}
