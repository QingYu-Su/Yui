package handlers

import (
	"bytes"
	"context"
	"crypto/rand"     // 用于生成随机数
	"encoding/binary" // 二进制编码/解码
	"errors"
	"fmt"
	"io"
	"log"
	"net"     // 网络相关操作
	"os/exec" // 执行外部命令
	"reflect" // 反射
	"runtime" // 运行时信息
	"strings"
	"sync"        // 同步原语
	"sync/atomic" // 原子操作
	"syscall"     // 系统调用
	"time"

	"unsafe" // 非安全操作

	"github.com/QingYu-Su/Yui/pkg/logger" // 自定义日志包
	"github.com/go-ping/ping"             // ICMP ping工具
	"github.com/inetaf/tcpproxy"          // TCP代理
	"gvisor.dev/gvisor/pkg/buffer"        // 缓冲区处理

	"gvisor.dev/gvisor/pkg/tcpip"                // TCP/IP协议栈
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet" // Go网络适配器
	"gvisor.dev/gvisor/pkg/tcpip/checksum"       // 校验和计算
	"gvisor.dev/gvisor/pkg/tcpip/header"         // 协议头处理
	"gvisor.dev/gvisor/pkg/tcpip/header/parse"   // 协议头解析
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"   // IPv4网络
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"   // IPv6网络
	"gvisor.dev/gvisor/pkg/tcpip/stack"          // 协议栈
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp" // ICMP传输
	"gvisor.dev/gvisor/pkg/tcpip/transport/raw"  // 原始套接字
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"  // TCP传输
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"  // UDP传输
	"gvisor.dev/gvisor/pkg/waiter"               // 等待队列

	"golang.org/x/crypto/ssh" // SSH协议实现
)

// 全局变量
var (
	// 网络接口ID映射表，记录已使用的NICID
	nicIds = map[tcpip.NICID]bool{}
	// 保护nicIds的互斥锁
	nicIdsLck sync.Mutex
)

// 统计结构体，用于记录网络接口的统计信息
type stat struct {
	NICID tcpip.NICID // 网络接口ID

	closed bool // 是否已关闭

	// UDP相关统计
	udp struct {
		active   atomic.Int64 // 活跃的UDP连接数
		failures atomic.Int64 // UDP失败次数
	}

	// TCP相关统计
	tcp struct {
		active   atomic.Int64 // 活跃的TCP流数
		failures atomic.Int64 // TCP失败次数
	}
}

// statsPrinter 定期打印统计信息
func (s *stat) statsPrinter(l logger.Logger) {
	// 初始化上次记录的统计值
	pastTcpActive := s.tcp.active.Load()
	pastTcpFail := s.tcp.failures.Load()

	pastUdpActive := s.udp.active.Load()
	pastUdpFail := s.udp.failures.Load()

	// 循环打印统计信息，直到关闭
	for !s.closed {
		// 获取当前统计值
		currentTcpActive := s.tcp.active.Load()
		currentTcpFail := s.tcp.failures.Load()

		currentUdpActive := s.udp.active.Load()
		currentUdpFail := s.udp.failures.Load()

		// 如果统计值有变化，则打印
		if currentUdpActive != pastUdpActive || currentUdpFail != pastUdpFail ||
			currentTcpActive != pastTcpActive || currentTcpFail != pastTcpFail {
			l.Info("TUN NIC %d Stats: TCP streams: %d, TCP failures: %d, UDP connections: %d, UDP failures: %d",
				uint32(s.NICID), currentTcpActive, currentTcpFail, currentUdpActive, currentUdpFail)

			// 更新上次记录的统计值
			pastTcpActive = currentTcpActive
			pastTcpFail = currentTcpFail

			pastUdpActive = currentUdpActive
			pastUdpFail = currentUdpFail
		}

		// 每秒检查一次
		time.Sleep(1 * time.Second)
	}
}

// Tun 函数处理SSH通道上的TUN设备创建和网络栈初始化
func Tun(newChannel ssh.NewChannel, l logger.Logger) {
	// 使用defer和recover捕获可能的panic
	defer func() {
		if r := recover(); r != nil {
			l.Error("Recovered panic from tun driver %v", r)
		}
	}()

	// 定义TUN设备信息结构
	var tunInfo struct {
		Mode uint32 // TUN模式
		No   uint32 // 设备号
	}

	// 从SSH通道的额外数据中解析TUN设备信息
	err := ssh.Unmarshal(newChannel.ExtraData(), &tunInfo)
	if err != nil {
		newChannel.Reject(ssh.ConnectionFailed, "connection closed")
		l.Warning("Unable to accept new channel %s", err)
		return
	}

	// 检查TUN模式是否有效(1表示点对点模式)
	if tunInfo.Mode != 1 {
		newChannel.Reject(ssh.ConnectionFailed, "connection closed")
		return
	}

	var NICID tcpip.NICID   // 网络接口ID
	allocatedNicId := false // 是否成功分配NICID的标志

	// 尝试最多3次分配唯一的NICID
	for i := 0; i < 3; i++ {
		// 生成随机数作为NICID候选
		buff := make([]byte, 4)
		_, err := rand.Read(buff)
		if err != nil {
			newChannel.Reject(ssh.ResourceShortage, "no resources")
			l.Warning("unable to allocate new nicid %s", err)
			return
		}

		NICID = tcpip.NICID(binary.BigEndian.Uint32(buff))

		// 检查NICID是否已被使用
		nicIdsLck.Lock()
		if _, ok := nicIds[NICID]; ok {
			nicIdsLck.Unlock()
			continue // 如果已被使用，继续尝试
		}

		// 成功分配NICID
		nicIds[NICID] = true
		allocatedNicId = true
		nicIdsLck.Unlock()
		break
	}

	// 检查是否成功分配NICID
	if !allocatedNicId {
		newChannel.Reject(ssh.ResourceShortage, "could not allocate nicid after 3 attempts")
		l.Warning("unable to allocate new nicid after 3 attempts")
		return
	}

	// 确保在函数退出时释放NICID
	defer func() {
		nicIdsLck.Lock()
		defer nicIdsLck.Unlock()
		delete(nicIds, NICID)
	}()

	// 接受SSH通道请求
	tunnel, req, err := newChannel.Accept()
	if err != nil {
		newChannel.Reject(ssh.ConnectionFailed, "connection closed")
		l.Warning("Unable to accept new channel %s", err)
		return
	}
	defer tunnel.Close() // 确保通道最终关闭

	l.Info("New TUN NIC %d created", uint32(NICID))

	// 创建新的用户态网络协议栈
	ns := stack.New(stack.Options{
		NetworkProtocols: []stack.NetworkProtocolFactory{
			ipv4.NewProtocol, // IPv4协议
			ipv6.NewProtocol, // IPv6协议
		},
		TransportProtocols: []stack.TransportProtocolFactory{
			tcp.NewProtocol,   // TCP协议
			udp.NewProtocol,   // UDP协议
			icmp.NewProtocol4, // ICMPv4协议
		},
		HandleLocal: false, // 不处理本地流量
	})
	defer ns.Close() // 确保协议栈最终关闭

	// 创建SSH端点作为链路层端点
	linkEP, err := NewSSHEndpoint(tunnel, l)
	if err != nil {
		l.Error("failed to create new SSH endpoint: %s", err)
		return
	}

	// 在协议栈上创建网络接口卡(NIC)
	if err := ns.CreateNIC(NICID, linkEP); err != nil {
		l.Error("CreateNIC: %v", err)
		return
	}

	// 设置ICMP响应器
	err = icmpResponder(ns)
	if err != nil {
		l.Error("Unable to create icmp responder: %v", err)
		return
	}

	// 初始化统计信息结构
	var tunStat stat
	tunStat.NICID = NICID

	// 启动统计信息打印协程
	go tunStat.statsPrinter(l)
	defer func() {
		tunStat.closed = true // 在函数退出时停止统计
	}()

	// 创建TCP流量转发器(端口范围0-14000)
	tcpHandler := tcp.NewForwarder(ns, 0, 14000, forwardTCP(&tunStat))

	// 创建UDP流量转发器
	udpHandler := udp.NewForwarder(ns, forwardUDP(&tunStat))

	// 注册传输层协议处理器
	ns.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpHandler.HandlePacket)
	ns.SetTransportProtocolHandler(udp.ProtocolNumber, udpHandler.HandlePacket)

	// 设置默认路由表
	ns.SetRouteTable([]tcpip.Route{
		{
			Destination: header.IPv4EmptySubnet, // 所有IPv4流量
			NIC:         NICID,
		},
		{
			Destination: header.IPv6EmptySubnet, // 所有IPv6流量
			NIC:         NICID,
		},
	})

	// 禁用IP转发功能
	ns.SetForwardingDefaultAndAllNICs(ipv4.ProtocolNumber, false)
	ns.SetForwardingDefaultAndAllNICs(ipv6.ProtocolNumber, false)

	// 启用TCP SACK选项
	nsacks := tcpip.TCPSACKEnabled(true)
	ns.SetTransportProtocolOption(tcp.ProtocolNumber, &nsacks)

	// 禁用SYN Cookies(可能影响nmap扫描)
	synCookies := tcpip.TCPAlwaysUseSynCookies(false)
	ns.SetTransportProtocolOption(tcp.ProtocolNumber, &synCookies)

	// 设置混杂模式和欺骗模式(允许所有流量)
	ns.SetPromiscuousMode(NICID, true)
	ns.SetSpoofing(NICID, true)

	// 丢弃所有SSH通道请求
	ssh.DiscardRequests(req)

	l.Info("TUN NIC %d ended", uint32(NICID))
}

// forwardUDP 返回一个处理UDP转发请求的函数
func forwardUDP(tunstats *stat) func(request *udp.ForwarderRequest) {
	return func(request *udp.ForwarderRequest) {
		id := request.ID() // 获取请求ID(包含本地和远程地址/端口)

		// 创建等待队列和端点
		var wq waiter.Queue
		ep, iperr := request.CreateEndpoint(&wq)
		if iperr != nil {
			// 记录失败统计
			tunstats.udp.failures.Add(1)
			log.Println("[+] failed to create endpoint for udp: ", iperr)
			return
		}

		// 创建UDP代理:
		// 1. 使用自动停止的监听器包装UDP连接
		// 2. 提供拨号函数连接到目标地址
		p, _ := NewUDPProxy(&autoStoppingListener{
			underlying: gonet.NewUDPConn(&wq, ep),
		}, func() (net.Conn, error) {
			return net.Dial("udp", net.JoinHostPort(
				id.LocalAddress.String(),
				fmt.Sprintf("%d", id.LocalPort)))
		})

		// 启动代理协程
		go func() {
			// 更新活跃连接统计
			tunstats.udp.active.Add(1)
			defer tunstats.udp.active.Add(-1) // 确保减少计数

			p.Run() // 运行代理

			// 清理资源:
			// 1. 关闭端点(后续到达的包会被丢弃)
			// 2. 关闭代理
			// 注意: 新请求会创建新的处理流程
			ep.Close()
			p.Close()
		}()
	}
}

// forwardTCP 返回一个处理TCP转发请求的函数
func forwardTCP(tunstats *stat) func(request *tcp.ForwarderRequest) {
	return func(request *tcp.ForwarderRequest) {
		id := request.ID() // 获取请求ID

		// 构造目标地址
		fwdDst := net.TCPAddr{
			IP:   net.ParseIP(id.LocalAddress.String()),
			Port: int(id.LocalPort),
		}

		// 建立到目标的连接(5秒超时)
		outbound, err := net.DialTimeout("tcp", fwdDst.String(), 5*time.Second)
		if err != nil {
			// 记录失败统计并完成请求(指示错误)
			tunstats.tcp.failures.Add(1)
			request.Complete(true) // true表示错误
			return
		}

		// 创建TCP端点
		var wq waiter.Queue
		ep, errTcp := request.CreateEndpoint(&wq)

		// 无论成功与否都标记请求完成
		request.Complete(false) // false表示无错误

		if errTcp != nil {
			// 忽略连接拒绝错误(临时错误)
			if _, ok := errTcp.(*tcpip.ErrConnectionRefused); !ok {
				log.Printf("could not create endpoint: %s", errTcp)
			}
			tunstats.tcp.failures.Add(1)
			return
		}

		// 更新活跃连接统计
		tunstats.tcp.active.Add(1)
		defer tunstats.tcp.active.Add(-1)

		// 创建TCP代理
		remote := tcpproxy.DialProxy{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return outbound, nil // 重用已建立的连接
			},
		}
		// 处理连接: 将虚拟端点连接与目标连接桥接
		remote.HandleConn(gonet.NewTCPConn(&wq, ep))
	}
}

// SSHEndpoint 实现了stack.LinkEndpoint接口，通过SSH通道传输网络数据包
type SSHEndpoint struct {
	l logger.Logger // 日志记录器

	dispatcher stack.NetworkDispatcher // 网络协议栈分发器
	tunnel     ssh.Channel             // SSH通道用于数据传输

	channelPtr unsafe.Pointer // 指向底层SSH channel结构的指针(非安全操作)

	pending *sshBuffer // 指向SSH channel内部缓冲区的指针

	lock sync.Mutex // 同步锁
}

// adjustWindow 是链接到ssh.(*channel).adjustWindow的私有函数
// 使用go:linkname实现非导出函数的调用
//
//go:linkname adjustWindow golang.org/x/crypto/ssh.(*channel).adjustWindow
func adjustWindow(c unsafe.Pointer, n uint32) error

// NewSSHEndpoint 创建新的SSH端点
func NewSSHEndpoint(dev ssh.Channel, l logger.Logger) (*SSHEndpoint, error) {
	r := &SSHEndpoint{
		tunnel: dev,
		l:      l,
	}

	const bufferName = "pending" // SSH channel内部缓冲区字段名

	// 使用反射获取channel的内部结构
	val := reflect.ValueOf(dev)
	r.channelPtr = val.UnsafePointer() // 保存原始channel指针

	val = val.Elem() // 获取指针指向的值

	// 验证类型是否为标准channel(不支持扩展channel)
	if val.Type().Name() != "channel" {
		return nil, fmt.Errorf("extended channels are not supported: %s", val.Type().Name())
	}

	// 获取channel内部的pending缓冲区字段
	field := val.FieldByName(bufferName)
	if !field.IsValid() {
		return nil, fmt.Errorf("field %s not found", bufferName)
	}

	// 将缓冲区指针转换为sshBuffer类型
	r.pending = (*sshBuffer)(field.UnsafePointer())
	return r, nil
}

// ReadSSHPacket 从SSH通道读取单个数据包
func (m *SSHEndpoint) ReadSSHPacket() ([]byte, error) {
	// 从pending缓冲区读取数据
	buff, err := m.pending.ReadSingle()
	if err != nil {
		return nil, err
	}

	// 成功读取数据后调整窗口大小
	if len(buff) > 0 {
		err = adjustWindow(m.channelPtr, uint32(len(buff)))
		// 忽略EOF错误(当有数据时)
		if len(buff) > 0 && err == io.EOF {
			err = nil
		}
	}

	return buff, err
}

// Close 关闭SSH通道
func (m *SSHEndpoint) Close() {
	m.tunnel.Close()
}

// SetOnCloseAction 空实现(满足接口要求)
func (m *SSHEndpoint) SetOnCloseAction(func()) {}

// SetLinkAddress 空实现(满足接口要求)
func (m *SSHEndpoint) SetLinkAddress(addr tcpip.LinkAddress) {}

// SetMTU 空实现(满足接口要求)
func (m *SSHEndpoint) SetMTU(uint32) {}

// ParseHeader 空实现(总是返回true)
func (m *SSHEndpoint) ParseHeader(*stack.PacketBuffer) bool {
	return true
}

// MTU 返回默认MTU值(1500)
func (m *SSHEndpoint) MTU() uint32 {
	return 1500
}

// Capabilities 返回端点能力(无特殊能力)
func (m *SSHEndpoint) Capabilities() stack.LinkEndpointCapabilities {
	return stack.CapabilityNone
}

// MaxHeaderLength 返回最大头部长度(0)
func (m *SSHEndpoint) MaxHeaderLength() uint16 {
	return 0
}

// LinkAddress 返回空链路地址
func (m *SSHEndpoint) LinkAddress() tcpip.LinkAddress {
	return ""
}

// Attach 将端点附加到网络协议栈并启动分发循环
func (m *SSHEndpoint) Attach(dispatcher stack.NetworkDispatcher) {
	m.dispatcher = dispatcher
	go m.dispatchLoop() // 启动goroutine处理数据包分发
}

// sshBuffer 是来自golang/crypto/ssh包的缓冲区实现，用于生产者和消费者之间的数据交换
// 理论上容量无限，因为它不自己分配内存
type sshBuffer struct {
	// 保护对head、tail和closed的并发访问
	*sync.Cond

	head *element // 最先被读取的缓冲区
	tail *element // 最后被读取的缓冲区

	closed bool // 缓冲区是否已关闭
}

// ReadSingle 从缓冲区读取单个数据包(适配自golang/crypto/ssh实现)
func (sb *sshBuffer) ReadSingle() ([]byte, error) {
	sb.Cond.L.Lock()
	defer sb.Cond.L.Unlock()

	// 检查缓冲区是否已关闭
	if sb.closed {
		return nil, io.EOF
	}

	// 如果缓冲区为空，等待数据到达
	if len(sb.head.buf) == 0 && sb.head == sb.tail {
		sb.Cond.Wait() // 等待条件变量信号
		if sb.closed { // 再次检查是否关闭
			return nil, io.EOF
		}
	}

	// 复制头部数据(避免外部修改影响内部缓冲区)
	result := make([]byte, len(sb.head.buf))
	n := copy(result, sb.head.buf)

	// 更新缓冲区(消费已读取部分)
	sb.head.buf = sb.head.buf[n:]

	// 如果头部不等于尾部，移动到下一个元素
	if sb.head != sb.tail {
		sb.head = sb.head.next
	}

	return result, nil
}

// element 表示链表中的单个节点
type element struct {
	buf  []byte   // 实际数据
	next *element // 下一个节点
}

// dispatchLoop 是SSHEndpoint的核心分发循环
func (m *SSHEndpoint) dispatchLoop() {
	for {
		// 1. 从SSH通道读取数据包
		packet, err := m.ReadSSHPacket()
		if err != nil {
			if err != io.EOF { // 非正常关闭记录错误
				m.l.Error("failed to read from tunnel: %s", err)
			}
			m.tunnel.Close() // 关闭通道
			return
		}

		// 2. 检查数据包长度是否有效
		if len(packet) < 4 {
			continue
		}

		// 3. 检查是否已附加到协议栈
		if !m.IsAttached() {
			continue
		}

		/*
		   4. 处理TUN/TAP帧格式:
		      SSH客户端以tuntap帧格式提供数据(前4字节是元数据):
		      - 标志 [2字节]
		      - 协议 [2字节]
		      - 原始协议帧(IP、IPv6等)
		      参考: https://kernel.googlesource.com/pub/scm/linux/kernel/git/stable/linux-stable/+/v3.4.85/Documentation/networking/tuntap.txt
		*/
		packet = packet[4:] // 去除帧头

		// 5. 根据IP版本分发数据包
		switch header.IPVersion(packet) {
		case header.IPv4Version:
			// 创建IPv4数据包缓冲区
			pkb := stack.NewPacketBuffer(stack.PacketBufferOptions{
				Payload: buffer.MakeWithData(packet),
			})
			// 分发到IPv4协议处理器
			m.dispatcher.DeliverNetworkPacket(header.IPv4ProtocolNumber, pkb)

		case header.IPv6Version:
			// 创建IPv6数据包缓冲区
			pkb := stack.NewPacketBuffer(stack.PacketBufferOptions{
				Payload: buffer.MakeWithData(packet),
			})
			// 分发到IPv6协议处理器
			m.dispatcher.DeliverNetworkPacket(header.IPv6ProtocolNumber, pkb)

		default:
			// 记录未知协议数据包
			log.Println("received something that wasn't an IPv6 or IPv4 packet: family: ",
				header.IPVersion(packet), "len:", len(packet))
		}
	}
}

// IsAttached 检查端点是否已附加到网络协议栈
// 实现stack.LinkEndpoint接口
func (m *SSHEndpoint) IsAttached() bool {
	return m.dispatcher != nil // 通过dispatcher字段判断是否已附加
}

// WritePackets 批量写入出站数据包
// 实现stack.LinkEndpoint接口
func (m *SSHEndpoint) WritePackets(pkts stack.PacketBufferList) (int, tcpip.Error) {
	n := 0 // 成功写入的包计数

	// 遍历所有数据包
	for _, pkt := range pkts.AsSlice() {
		if err := m.writePacket(pkt); err != nil {
			return n, err // 遇到错误立即返回
		}
		n++
	}
	return n, nil // 返回成功写入的数量
}

// writePacket 写入单个出站数据包
func (m *SSHEndpoint) writePacket(pkt *stack.PacketBuffer) tcpip.Error {
	// 获取数据包内容
	pktBuf := pkt.ToView().AsSlice()

	// 加锁解决SSH通道的并发写入问题
	// (实际原因不明，但实验证明需要此锁)
	m.lock.Lock()
	defer m.lock.Unlock()

	/*
	   构造TUN/TAP帧头(4字节):
	   - 前2字节: 标志(固定为1)
	   - 后2字节: 协议类型(取自数据包)
	   参考Linux内核文档:
	   https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/tree/Documentation/networking/tuntap.rst
	*/
	packet := make([]byte, 4)
	binary.BigEndian.PutUint16(packet, 1)                                     // 标志位
	binary.BigEndian.PutUint16(packet[2:], uint16(pkt.NetworkProtocolNumber)) // 协议类型

	// 添加实际数据包内容
	packet = append(packet, pktBuf...)

	// 通过SSH通道写入数据
	if _, err := m.tunnel.Write(packet); err != nil {
		// 非EOF错误记录日志
		if err != io.EOF {
			m.l.Error("failed to write packet to tunnel: %s", err)
		}
		return &tcpip.ErrInvalidEndpointState{} // 返回端点状态错误
	}

	return nil
}

// Wait 空实现(满足接口要求)
func (m *SSHEndpoint) Wait() {}

// ARPHardwareType 返回ARP硬件类型(无ARP支持)
func (*SSHEndpoint) ARPHardwareType() header.ARPHardwareType {
	return header.ARPHardwareNone // 表示不支持ARP
}

// AddHeader 空实现(无需添加额外头部)
func (*SSHEndpoint) AddHeader(*stack.PacketBuffer) {}

// WriteRawPacket 实现原始数据包写入(明确不支持)
func (*SSHEndpoint) WriteRawPacket(*stack.PacketBuffer) tcpip.Error {
	return &tcpip.ErrNotSupported{} // 返回不支持错误
}

// icmpResponder 创建一个ICMP响应器，用于处理传入的ICMP请求
func icmpResponder(s *stack.Stack) error {
	// 创建等待队列用于事件通知
	var wq waiter.Queue

	// 创建原始套接字端点，用于接收IPv4 ICMP数据包
	rawProto, rawerr := raw.NewEndpoint(s, ipv4.ProtocolNumber, icmp.ProtocolNumber4, &wq)
	if rawerr != nil {
		return errors.New("could not create raw endpoint")
	}

	// 绑定原始套接字(空地址表示接收所有)
	if err := rawProto.Bind(tcpip.FullAddress{}); err != nil {
		return errors.New("could not bind raw endpoint")
	}

	// 启动goroutine处理ICMP请求
	go func() {
		// 创建事件通知通道
		we, ch := waiter.NewChannelEntry(waiter.ReadableEvents)
		wq.EventRegister(&we)

		for {
			var buff bytes.Buffer
			// 尝试读取ICMP数据包
			_, err := rawProto.Read(&buff, tcpip.ReadOptions{})

			// 处理阻塞情况
			if _, ok := err.(*tcpip.ErrWouldBlock); ok {
				// 等待数据可用
				for range ch {
					_, err := rawProto.Read(&buff, tcpip.ReadOptions{})
					if err != nil {
						continue
					}

					// 解析IPv4头部
					iph := header.IPv4(buff.Bytes())
					hlen := int(iph.HeaderLength())

					// 检查数据长度是否有效
					if buff.Len() < hlen {
						return
					}

					/*
					   从字节重建ICMP PacketBuffer:
					   1. 创建包含原始数据的视图
					   2. 创建PacketBuffer并保留头部空间
					   3. 设置协议号
					   4. 消费网络头部
					*/
					view := buffer.MakeWithData(buff.Bytes())
					packetbuff := stack.NewPacketBuffer(stack.PacketBufferOptions{
						Payload:            view,
						ReserveHeaderBytes: hlen,
					})

					packetbuff.NetworkProtocolNumber = ipv4.ProtocolNumber
					packetbuff.TransportProtocolNumber = icmp.ProtocolNumber4
					packetbuff.NetworkHeader().Consume(hlen)

					// 异步处理ICMP请求(避免阻塞接收循环)
					go func() {
						// 检查目标地址是否可解析
						if TryResolve(iph.DestinationAddress().String()) {
							ProcessICMP(s, packetbuff)
						}
					}()
				}
			}
		}
	}()
	return nil
}

// ProcessICMP 处理ICMP回显请求并发送回显应答
// 代码主要来自gvisor的pkg/tcpip/network/ipv4/icmp.go实现
func ProcessICMP(nstack *stack.Stack, pkt *stack.PacketBuffer) {
	// 确保释放数据包引用计数
	defer pkt.DecRef()

	// 获取ICMP头部
	h := header.ICMPv4(pkt.TransportHeader().Slice())
	if len(h) < header.ICMPv4MinimumSize {
		return // 头部长度不足直接返回
	}

	// 校验ICMP校验和(必须正确才继续处理)
	if checksum.Checksum(h, pkt.Data().Checksum()) != 0xffff {
		return
	}

	// 获取IPv4头部
	iph := header.IPv4(pkt.NetworkHeader().Slice())
	var newOptions header.IPv4Options // 新IP选项(当前为空)

	// 根据ICMP类型处理
	switch h.Type() {
	case header.ICMPv4Echo: // 只处理回显请求(类型8)
		// 获取传输层负载数据(ICMP报文内容)
		replyData := stack.PayloadSince(pkt.TransportHeader())
		defer replyData.Release() // 确保释放资源

		ipHdr := header.IPv4(pkt.NetworkHeader().Slice())
		localAddressBroadcast := pkt.NetworkPacketInfo.LocalAddressBroadcast

		// 准备构建应答包，清空原包引用
		pkt = nil

		/*
		   根据RFC 1122第3.2.1.3节:
		   - 主机发送任何数据报时，IP源地址必须是其自身地址之一
		   - 不能是广播或多播地址
		*/
		localAddr := ipHdr.DestinationAddress()
		if localAddressBroadcast || header.IsV4MulticastAddress(localAddr) {
			localAddr = tcpip.Address{} // 无效地址将由路由查找处理
		}

		// 查找返回路由路径
		r, err := nstack.FindRoute(1, localAddr, ipHdr.SourceAddress(),
			ipv4.ProtocolNumber, false /* multicastLoop */)
		if err != nil {
			return // 找不到路由则静默丢弃
		}
		defer r.Release() // 确保释放路由

		// 构建IPv4应答头部
		replyHeaderLength := uint8(header.IPv4MinimumSize + len(newOptions))
		replyIPHdrView := buffer.NewView(int(replyHeaderLength))

		// 复制原始IP头部并更新字段
		replyIPHdrView.Write(iph[:header.IPv4MinimumSize])
		replyIPHdrView.Write(newOptions)
		replyIPHdr := header.IPv4(replyIPHdrView.AsSlice())

		// 设置IP头部字段
		replyIPHdr.SetHeaderLength(replyHeaderLength)
		replyIPHdr.SetSourceAddress(r.LocalAddress())
		replyIPHdr.SetDestinationAddress(r.RemoteAddress())
		replyIPHdr.SetTTL(r.DefaultTTL())
		replyIPHdr.SetTotalLength(uint16(len(replyIPHdr) + len(replyData.AsSlice())))
		replyIPHdr.SetChecksum(0)
		replyIPHdr.SetChecksum(^replyIPHdr.CalculateChecksum()) // 计算校验和

		// 构建ICMP应答头部(将类型改为回显应答/类型0)
		replyICMPHdr := header.ICMPv4(replyData.AsSlice())
		replyICMPHdr.SetType(header.ICMPv4EchoReply)
		replyICMPHdr.SetChecksum(0)
		replyICMPHdr.SetChecksum(^checksum.Checksum(replyData.AsSlice(), 0))

		// 组合IP头部和ICMP负载
		replyBuf := buffer.MakeWithView(replyIPHdrView)
		replyBuf.Append(replyData.Clone())

		// 创建应答数据包缓冲区
		replyPkt := stack.NewPacketBuffer(stack.PacketBufferOptions{
			ReserveHeaderBytes: int(r.MaxHeaderLength()),
			Payload:            replyBuf,
		})
		defer replyPkt.DecRef() // 确保释放

		/*
		   显式解析头部以确保:
		   1. 通过IPTables规则检查
		   2. 协议栈正确处理数据包
		*/
		if ok := parse.IPv4(replyPkt); !ok {
			panic("expected to parse IPv4 header we just created")
		}
		if ok := parse.ICMPv4(replyPkt); !ok {
			panic("expected to parse ICMPv4 header we just created")
		}

		// 设置传输协议号并发送应答包
		replyPkt.TransportProtocolNumber = header.ICMPv4ProtocolNumber
		if err := r.WriteHeaderIncludedPacket(replyPkt); err != nil {
			return // 发送失败则静默丢弃
		}
	}
}

// TryResolve 尝试通过多种方法检测远程主机是否在线
func TryResolve(address string) bool {
	// 定义检测方法列表(按优先级排序)
	methods := []func(string) (bool, error){
		RawPinger,     // 方法1: 原始ICMP套接字(需要权限)
		CommandPinger, // 方法2: 系统ping命令(兼容性更好)
	}

	// 依次尝试每种方法
	for _, method := range methods {
		if result, err := method(address); err == nil {
			return result // 任一方法成功即返回
		}
	}

	// 所有方法均失败
	return false
}

// RawPinger 使用原始ICMP套接字检测主机可达性
// 注意: 在某些系统上需要管理员权限
func RawPinger(target string) (bool, error) {
	// 创建ping实例
	pinger, err := ping.NewPinger(target)
	if err != nil {
		return false, fmt.Errorf("create pinger failed: %w", err)
	}

	// 配置ping参数
	pinger.Count = 1                 // 只发送1个包
	pinger.Timeout = 4 * time.Second // 参考NMAP默认超时

	// Windows系统需要特权模式
	if runtime.GOOS == "windows" {
		pinger.SetPrivileged(true)
	}

	// 执行ping
	err = pinger.Run()
	if err != nil {
		return false, fmt.Errorf("ping run failed: %w", err)
	}

	// 检查是否收到回复
	return pinger.PacketsRecv != 0, nil
}

// CommandPinger 使用系统ping命令检测主机可达性
// 优点: 不需要特殊权限
func CommandPinger(target string) (bool, error) {
	// 根据不同系统设置参数
	countArg := "-c" // Linux/Mac包计数参数
	waitArg := "-W"  // Linux/Mac超时参数
	waitTime := "3"  // 3秒超时(单位秒)

	if runtime.GOOS == "windows" {
		countArg = "/n"   // Windows包计数参数
		waitArg = "/w"    // Windows超时参数
		waitTime = "3000" // 3000毫秒超时
	}

	// 构造ping命令
	cmd := exec.Command("ping", countArg, "1", waitArg, waitTime, target)

	// 执行命令
	if err := cmd.Run(); err != nil {
		// 命令执行失败(非目标不可达)
		if exitErr, ok := err.(*exec.ExitError); ok {
			// 在Linux/Mac上，ping不可达会返回1
			// 在Windows上，返回0表示可达，1表示不可达
			if runtime.GOOS == "windows" {
				return exitErr.ExitCode() == 0, nil
			}
			return false, nil
		}
		return false, fmt.Errorf("execute ping command failed: %w", err)
	}

	// 命令执行成功(目标可达)
	return true, nil
}

// 常量定义
const (
	// UDPConnTrackTimeout UDP连接跟踪超时时间(90秒)
	UDPConnTrackTimeout = 90 * time.Second

	// UDPBufSize UDP代理缓冲区大小(最大UDP数据包大小)
	UDPBufSize = 65507 // 65535 - 8字节UDP头 - 20字节IP头
)

// connTrackKey 将IP地址拆分为两个字段的网络地址结构体，可用作map的键
type connTrackKey struct {
	IPHigh uint64 // IP地址高位(IPv6前64位)
	IPLow  uint64 // IP地址低位(IPv6后64位或IPv4全部32位)
	Port   int    // 端口号
}

// newConnTrackKey 从UDP地址创建连接跟踪键
func newConnTrackKey(addr *net.UDPAddr) *connTrackKey {
	if len(addr.IP) == net.IPv4len {
		// IPv4处理: IPLow存储32位地址，IPHigh为0
		return &connTrackKey{
			IPHigh: 0,
			IPLow:  uint64(binary.BigEndian.Uint32(addr.IP)),
			Port:   addr.Port,
		}
	}
	// IPv6处理: 拆分为两个64位部分
	return &connTrackKey{
		IPHigh: binary.BigEndian.Uint64(addr.IP[:8]),
		IPLow:  binary.BigEndian.Uint64(addr.IP[8:]),
		Port:   addr.Port,
	}
}

// connTrackMap 连接跟踪表类型定义
type connTrackMap map[connTrackKey]net.Conn

// UDPProxy UDP代理结构体，实现前端和后端地址之间的UDP流量转发
type UDPProxy struct {
	listener       udpConn                  // UDP监听器接口
	dialer         func() (net.Conn, error) // 后端连接创建函数
	connTrackTable connTrackMap             // 连接跟踪表
	connTrackLock  sync.Mutex               // 保护连接跟踪表的互斥锁
}

// NewUDPProxy 创建新的UDP代理实例
func NewUDPProxy(listener udpConn, dialer func() (net.Conn, error)) (*UDPProxy, error) {
	return &UDPProxy{
		listener:       listener,           // 设置UDP监听器
		connTrackTable: make(connTrackMap), // 初始化连接跟踪表
		dialer:         dialer,             // 设置后端连接创建函数
	}, nil
}

// replyLoop 处理从后端服务返回的UDP数据并转发回客户端
func (proxy *UDPProxy) replyLoop(proxyConn net.Conn, clientAddr net.Addr, clientKey *connTrackKey) {
	// 确保退出时清理资源
	defer func() {
		proxy.connTrackLock.Lock()
		delete(proxy.connTrackTable, *clientKey) // 从连接跟踪表删除
		proxy.connTrackLock.Unlock()
		proxyConn.Close() // 关闭后端连接
	}()

	readBuf := make([]byte, UDPBufSize) // 创建读取缓冲区
	for {
		// 设置读取超时(连接跟踪超时时间)
		_ = proxyConn.SetReadDeadline(time.Now().Add(UDPConnTrackTimeout))

	again:
		// 从后端连接读取数据
		read, err := proxyConn.Read(readBuf)
		if err != nil {
			// 处理连接拒绝错误(后端服务可能暂时不可用)
			if err, ok := err.(*net.OpError); ok && err.Err == syscall.ECONNREFUSED {
				goto again // 继续重试直到超时
			}
			return // 其他错误直接返回
		}

		// 将数据完整写回客户端(处理分片情况)
		for i := 0; i != read; {
			written, err := proxy.listener.WriteTo(readBuf[i:read], clientAddr)
			if err != nil {
				return // 写入失败则终止循环
			}
			i += written
		}
	}
}

// Run 启动UDP代理转发主循环
func (proxy *UDPProxy) Run() {
	readBuf := make([]byte, UDPBufSize) // 创建接收缓冲区

	for {
		// 从监听器读取客户端数据
		read, from, err := proxy.listener.ReadFrom(readBuf)
		if err != nil {
			// 处理监听器关闭错误(非正常关闭才记录日志)
			if !isClosedError(err) {
				log.Printf("Stopping udp proxy (%s)", err)
			}
			break // 退出主循环
		}

		// 创建连接跟踪键
		fromKey := newConnTrackKey(from.(*net.UDPAddr))

		proxy.connTrackLock.Lock()
		// 检查是否已有对应连接
		proxyConn, hit := proxy.connTrackTable[*fromKey]
		if !hit {
			// 新建后端连接
			proxyConn, err = proxy.dialer()
			if err != nil {
				log.Printf("Can't proxy a datagram to udp: %s\n", err)
				proxy.connTrackLock.Unlock()
				continue // 继续处理下一个包
			}
			// 记录新连接并启动回复循环
			proxy.connTrackTable[*fromKey] = proxyConn
			go proxy.replyLoop(proxyConn, from, fromKey)
		}
		proxy.connTrackLock.Unlock()

		// 转发客户端数据到后端(处理分片情况)
		for i := 0; i != read; {
			// 设置写超时(使用连接跟踪超时时间)
			_ = proxyConn.SetReadDeadline(time.Now().Add(UDPConnTrackTimeout))
			written, err := proxyConn.Write(readBuf[i:read])
			if err != nil {
				log.Printf("Can't proxy a datagram to udp: %s\n", err)
				break
			}
			i += written
		}
	}
}

// Close 停止UDP代理并释放所有资源
func (proxy *UDPProxy) Close() error {
	// 1. 关闭监听器停止接收新连接
	proxy.listener.Close()

	// 2. 清理所有活跃连接
	proxy.connTrackLock.Lock()
	defer proxy.connTrackLock.Unlock()

	for _, conn := range proxy.connTrackTable {
		conn.Close() // 关闭每个后端连接
	}

	return nil
}

// isClosedError 检查错误是否由已关闭的连接引起
func isClosedError(err error) bool {
	/* 此比较方法较粗糙，但由于net包未导出errClosing，
	 * 参考:
	 * http://golang.org/src/pkg/net/net.go
	 * https://code.google.com/p/go/issues/detail?id=4337
	 * https://groups.google.com/forum/#!msg/golang-nuts/0_aaCvBmOcM/SptmDyX1XJMJ
	 */
	return strings.HasSuffix(err.Error(), "use of closed network connection")
}

// udpConn UDP连接接口定义
type udpConn interface {
	ReadFrom(b []byte) (int, net.Addr, error)     // 从连接读取数据
	WriteTo(b []byte, addr net.Addr) (int, error) // 向指定地址写入数据
	SetReadDeadline(t time.Time) error            // 设置读取截止时间
	io.Closer                                     // 关闭连接
}

// autoStoppingListener 自动停止的监听器包装
type autoStoppingListener struct {
	underlying udpConn // 底层UDP连接
}

// ReadFrom 实现带超时的读取操作
func (l *autoStoppingListener) ReadFrom(b []byte) (int, net.Addr, error) {
	// 设置默认UDP连接跟踪超时
	_ = l.underlying.SetReadDeadline(time.Now().Add(UDPConnTrackTimeout))
	return l.underlying.ReadFrom(b)
}

// WriteTo 实现带超时的写入操作
func (l *autoStoppingListener) WriteTo(b []byte, addr net.Addr) (int, error) {
	// 设置默认UDP连接跟踪超时
	_ = l.underlying.SetReadDeadline(time.Now().Add(UDPConnTrackTimeout))
	return l.underlying.WriteTo(b, addr)
}

// SetReadDeadline 设置读取截止时间
func (l *autoStoppingListener) SetReadDeadline(t time.Time) error {
	return l.underlying.SetReadDeadline(t)
}

// Close 关闭底层连接
func (l *autoStoppingListener) Close() error {
	return l.underlying.Close()
}
