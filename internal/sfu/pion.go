package sfu

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/pion/datachannel"
	"github.com/pion/dtls/v3"
	dtlsnet "github.com/pion/dtls/v3/pkg/net"
	"github.com/pion/ice/v4"
	"github.com/pion/logging"
	"github.com/pion/rtcp"
	"github.com/pion/sctp"
	"github.com/pion/srtp/v3"
	"go.uber.org/zap"
)

// PionConfig 是内嵌 SFU 的运行参数。
type PionConfig struct {
	// UDPPort 是单 UDP 监听端口（pion ICE UDPMux，全部 endpoint 复用）。
	UDPPort int
	// AdvertiseIP 是写进下行 candidate 的客户端可达地址。⚠ 127.0.0.1 会让
	// 真机 ICE 永远连不上且无任何 RPC 错误（纯媒体面静默失败）。
	AdvertiseIP string
	Logger      *zap.Logger
	// Touch 是媒体面活性回报钩子：对仍存活（近期 SRTP 收包/ICE 连通）的
	// endpoint 周期性刷新 group_call_participants.last_check_date，使 sweeper 的
	// 单一水位同时承载「心跳 ∨ 媒体活性」（P0-2 双过期判据的实现方式）。
	Touch func(callID, userID int64)
	// LivenessInterval 是活性回报周期（默认 15s，远小于 sweeper 的 45s TTL）。
	LivenessInterval time.Duration
	// ActivityWindow 判定 endpoint 媒体面存活的最近收包窗口（默认 25s）。
	ActivityWindow time.Duration
}

// pionSFU 是 Service 的 pion 实现：full-ICE CONTROLLING + DTLS 主动握手（client
// 角色，对应下行 setup:"active"）+ SRTP 房间内原样转发（不重写 SSRC、保留
// audio-level 等 RTP 扩展，speaking 指示由客户端渲染）。
type pionSFU struct {
	cfg         PionConfig
	log         *zap.Logger
	udpConn     net.PacketConn
	mux         *ice.UDPMuxDefault
	cert        tls.Certificate
	fingerprint string

	mu    sync.Mutex
	rooms map[int64]*room

	cancel context.CancelFunc
}

type epKey struct {
	userID int64
	kind   EndpointKind
}

type room struct {
	callID    int64
	endpoints map[epKey]*endpoint
}

// mediaPlan 是一个发布 endpoint 的 ssrc 转发计划（Join 时从 ssrc-groups 算定）：
// 订阅端只为 SIM[0]（最低层）建解码 sink——只转发该层与其 FID RTX 伙伴，
// 其余 simulcast 层在 SFU 侧丢弃（客户端反正会丢，转发纯浪费下行）。
type mediaPlan struct {
	audioSSRC uint32
	forward   map[uint32]bool // 转发的视频 ssrc（SIM[0] 层 + 其 RTX）
	drop      map[uint32]bool // 其余已声明视频 ssrc（高层及其 RTX）
}

func buildMediaPlan(offer ClientOffer) mediaPlan {
	plan := mediaPlan{
		audioSSRC: offer.AudioSSRC,
		forward:   make(map[uint32]bool),
		drop:      make(map[uint32]bool),
	}
	var primary uint32
	for _, g := range offer.SsrcGroups {
		if g.Semantics == "SIM" && len(g.Sources) > 0 {
			primary = g.Sources[0]
			break
		}
	}
	// 单层发布（conference 模式）没有 SIM 组：唯一组的第一个 ssrc 即主流。
	if primary == 0 && len(offer.SsrcGroups) == 1 && len(offer.SsrcGroups[0].Sources) > 0 {
		primary = offer.SsrcGroups[0].Sources[0]
	}
	for _, g := range offer.SsrcGroups {
		for i, ssrc := range g.Sources {
			keep := ssrc == primary ||
				(g.Semantics == "FID" && i == 1 && len(g.Sources) == 2 && g.Sources[0] == primary)
			if keep {
				plan.forward[ssrc] = true
			} else if !plan.forward[ssrc] {
				plan.drop[ssrc] = true
			}
		}
	}
	for ssrc := range plan.forward {
		delete(plan.drop, ssrc)
	}
	return plan
}

// shouldForward 报告该 ssrc 的流是否进入扇出（true）或在 SFU 终结（false）。
// 未声明的 ssrc 一律转发：音频、未来的探测流——订阅端对无 sink 的包安全丢弃。
func (p mediaPlan) shouldForward(ssrc uint32) bool {
	return !p.drop[ssrc]
}

type endpoint struct {
	callID int64
	userID int64
	kind   EndpointKind
	plan   mediaPlan
	sfu    *pionSFU

	agent  *ice.Agent
	demux  *demuxer
	closed chan struct{}

	mu           sync.Mutex
	writeStream  *srtp.WriteStreamSRTP
	rtcpWriter   *srtp.WriteStreamSRTCP
	lastActivity time.Time
	connected    bool
	closeOnce    sync.Once
}

// NewPion 启动内嵌 SFU：绑定 UDP 端口、生成进程级 DTLS 证书。
func NewPion(cfg PionConfig) (Service, error) {
	if cfg.UDPPort <= 0 {
		return nil, fmt.Errorf("sfu: invalid udp port %d", cfg.UDPPort)
	}
	if cfg.AdvertiseIP == "" {
		cfg.AdvertiseIP = "127.0.0.1"
	}
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}
	if cfg.LivenessInterval <= 0 {
		cfg.LivenessInterval = 15 * time.Second
	}
	if cfg.ActivityWindow <= 0 {
		cfg.ActivityWindow = 25 * time.Second
	}
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: cfg.UDPPort})
	if err != nil {
		return nil, fmt.Errorf("sfu: listen udp %d: %w", cfg.UDPPort, err)
	}
	cert, fingerprint, err := newDTLSCertificate()
	if err != nil {
		_ = udpConn.Close()
		return nil, err
	}
	s := &pionSFU{
		cfg:         cfg,
		log:         cfg.Logger,
		udpConn:     udpConn,
		mux:         ice.NewUDPMuxDefault(ice.UDPMuxParams{UDPConn: udpConn}),
		cert:        cert,
		fingerprint: fingerprint,
		rooms:       make(map[int64]*room),
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go s.livenessLoop(ctx)
	s.log.Info("sfu listening",
		zap.Int("udp_port", cfg.UDPPort),
		zap.String("advertise_ip", cfg.AdvertiseIP))
	if cfg.AdvertiseIP == "127.0.0.1" {
		s.log.Warn("TELESRV_SFU_ADVERTISE_IP is 127.0.0.1: real devices' ICE will not be reachable (media plane will silently fail); set to host LAN IP for multi-device testing")
	}
	return s, nil
}

func (s *pionSFU) Enabled() bool { return true }

// Join 为参与者建立媒体 endpoint：本端生成独立 ufrag/pwd，ICE CONTROLLING 等待
// 客户端 Binding Request（上行 JSON 无 candidates，对端地址靠 peer-reflexive 学得），
// 连通后以 DTLS client 主动握手并建 SRTP 会话。
func (s *pionSFU) Join(ctx context.Context, callID, userID int64, kind EndpointKind, offer ClientOffer) (ServerAnswer, error) {
	if offer.Ufrag == "" || offer.Pwd == "" {
		return ServerAnswer{}, fmt.Errorf("sfu: empty remote ice credentials")
	}
	localUfrag, err := randomICEString(8)
	if err != nil {
		return ServerAnswer{}, err
	}
	localPwd, err := randomICEString(24)
	if err != nil {
		return ServerAnswer{}, err
	}
	agent, err := ice.NewAgent(&ice.AgentConfig{
		NetworkTypes:    []ice.NetworkType{ice.NetworkTypeUDP4},
		CandidateTypes:  []ice.CandidateType{ice.CandidateTypeHost},
		UDPMux:          s.mux,
		LocalUfrag:      localUfrag,
		LocalPwd:        localPwd,
		IncludeLoopback: true,
		LoggerFactory:   &pionLoggerFactory{log: s.log.Named("ice")},
	})
	if err != nil {
		return ServerAnswer{}, fmt.Errorf("sfu: new ice agent: %w", err)
	}
	// pion 要求注册 OnCandidate 才能收集；候选地址由我们直接以
	// AdvertiseIP:UDPPort 写进下行 JSON，回调本身无需消费。
	if err := agent.OnCandidate(func(ice.Candidate) {}); err != nil {
		_ = agent.Close()
		return ServerAnswer{}, fmt.Errorf("sfu: on candidate: %w", err)
	}
	if err := agent.GatherCandidates(); err != nil {
		_ = agent.Close()
		return ServerAnswer{}, fmt.Errorf("sfu: gather candidates: %w", err)
	}
	ep := &endpoint{
		callID: callID,
		userID: userID,
		kind:   kind,
		plan:   buildMediaPlan(offer),
		sfu:    s,
		agent:  agent,
		closed: make(chan struct{}),
	}
	s.attachEndpoint(ep)
	go ep.run(offer)
	return ServerAnswer{
		Ufrag:             localUfrag,
		Pwd:               localPwd,
		FingerprintSHA256: s.fingerprint,
		Candidates: []Candidate{{
			IP:       s.cfg.AdvertiseIP,
			Port:     s.cfg.UDPPort,
			Protocol: "udp",
			Type:     "host",
		}},
	}, nil
}

func (s *pionSFU) attachEndpoint(ep *endpoint) {
	key := epKey{userID: ep.userID, kind: ep.kind}
	s.mu.Lock()
	rm, ok := s.rooms[ep.callID]
	if !ok {
		rm = &room{callID: ep.callID, endpoints: make(map[epKey]*endpoint)}
		s.rooms[ep.callID] = rm
	}
	old := rm.endpoints[key]
	rm.endpoints[key] = ep
	var oldPresentation *endpoint
	if ep.kind == EndpointMain {
		// 主连接 rejoin：旧 presentation 登记一并作废（信令侧同样清 presentation_json，
		// 客户端随后重发 joinGroupCallPresentation）。
		pk := epKey{userID: ep.userID, kind: EndpointPresentation}
		oldPresentation = rm.endpoints[pk]
		delete(rm.endpoints, pk)
	}
	s.mu.Unlock()
	if old != nil {
		old.close() // rejoin：替换旧 endpoint
	}
	if oldPresentation != nil {
		oldPresentation.close()
	}
}

func (s *pionSFU) Leave(_ context.Context, callID, userID int64, kind EndpointKind) error {
	var victims []*endpoint
	s.mu.Lock()
	if rm, ok := s.rooms[callID]; ok {
		keys := []epKey{{userID: userID, kind: kind}}
		if kind == EndpointMain {
			// 整体离会从不补发 leaveGroupCallPresentation：主 endpoint 离开联动拆屏幕。
			keys = append(keys, epKey{userID: userID, kind: EndpointPresentation})
		}
		for _, key := range keys {
			if ep := rm.endpoints[key]; ep != nil {
				victims = append(victims, ep)
				delete(rm.endpoints, key)
			}
		}
		if len(rm.endpoints) == 0 {
			delete(s.rooms, callID)
		}
	}
	s.mu.Unlock()
	for _, ep := range victims {
		ep.close()
	}
	return nil
}

func (s *pionSFU) CloseRoom(_ context.Context, callID int64) error {
	s.mu.Lock()
	rm := s.rooms[callID]
	delete(s.rooms, callID)
	s.mu.Unlock()
	if rm != nil {
		for _, ep := range rm.endpoints {
			ep.close()
		}
	}
	return nil
}

func (s *pionSFU) AliveUserIDs(callID int64) []int64 {
	cutoff := time.Now().Add(-s.cfg.ActivityWindow)
	s.mu.Lock()
	defer s.mu.Unlock()
	rm, ok := s.rooms[callID]
	if !ok {
		return nil
	}
	seen := make(map[int64]bool)
	var out []int64
	for key, ep := range rm.endpoints {
		// 任一连接（主/屏幕）存活即视为该参与者媒体面存活。
		if !seen[key.userID] && ep.aliveSince(cutoff) {
			seen[key.userID] = true
			out = append(out, key.userID)
		}
	}
	return out
}

// livenessLoop 周期性把媒体面存活回报给信令侧（刷新保活水位）。
func (s *pionSFU) livenessLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.LivenessInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.cfg.Touch == nil {
				continue
			}
			cutoff := time.Now().Add(-s.cfg.ActivityWindow)
			type key struct{ callID, userID int64 }
			seen := make(map[key]bool)
			var alive []key
			s.mu.Lock()
			for callID, rm := range s.rooms {
				for ek, ep := range rm.endpoints {
					k := key{callID, ek.userID}
					if !seen[k] && ep.aliveSince(cutoff) {
						seen[k] = true
						alive = append(alive, k)
					}
				}
			}
			s.mu.Unlock()
			for _, k := range alive {
				s.cfg.Touch(k.callID, k.userID)
			}
		}
	}
}

// forwardRTP 把一条解密后的 RTP 包原样转发给房间内其他参与者的**主** endpoint
// （不重写 SSRC，扩展头随包透传——audio-level 即 speaking 指示数据源；
// 观看屏幕共享也走观看者的主连接，presentation 连接不收任何远端流）。
func (s *pionSFU) forwardRTP(from *endpoint, packet []byte) {
	for _, ep := range s.mainTargets(from) {
		ep.writeRTP(packet)
	}
}

// mainTargets 返回房间内除 from 所属参与者外的全部主 endpoint。
func (s *pionSFU) mainTargets(from *endpoint) []*endpoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	rm, ok := s.rooms[from.callID]
	if !ok {
		return nil
	}
	targets := make([]*endpoint, 0, len(rm.endpoints))
	for key, ep := range rm.endpoints {
		if key.userID != from.userID && key.kind == EndpointMain {
			targets = append(targets, ep)
		}
	}
	return targets
}

// publisherBySSRC 找到房间内声明了该 ssrc（音频/任一视频层/RTX）的发布 endpoint，
// 供 RTCP 反馈（PLI/NACK）按 media ssrc 路由回发布端。房间规模小，直接遍历。
func (s *pionSFU) publisherBySSRC(callID int64, ssrc uint32) *endpoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	rm, ok := s.rooms[callID]
	if !ok {
		return nil
	}
	for _, ep := range rm.endpoints {
		if ep.plan.audioSSRC == ssrc || ep.plan.forward[ssrc] || ep.plan.drop[ssrc] {
			return ep
		}
	}
	return nil
}

// ---- endpoint ----

func (ep *endpoint) run(offer ClientOffer) {
	s := ep.sfu
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// CONTROLLING：主动发起连通性检查；对端候选靠带 ufrag/pwd 认证的
	// Binding Request 学得（peer-reflexive）。
	conn, err := ep.agent.Dial(ctx, offer.Ufrag, offer.Pwd)
	if err != nil {
		s.log.Debug("sfu ice dial", zap.Int64("call_id", ep.callID), zap.Int64("user_id", ep.userID), zap.Error(err))
		ep.close()
		return
	}
	ep.markActivity()
	ep.demux = newDemuxer(conn)

	dtlsConfig := &dtls.Config{
		Certificates:         []tls.Certificate{s.cert},
		InsecureSkipVerify:   true, // 身份校验走指纹（信令面 commit），非 CA 链
		ExtendedMasterSecret: dtls.RequireExtendedMasterSecret,
		SRTPProtectionProfiles: []dtls.SRTPProtectionProfile{
			dtls.SRTP_AEAD_AES_128_GCM,
			dtls.SRTP_AES128_CM_HMAC_SHA1_80,
		},
	}
	dtlsRaw := ep.demux.dtlsConn()
	dtlsConn, err := dtls.Client(dtlsnet.PacketConnFromConn(dtlsRaw), dtlsRaw.RemoteAddr(), dtlsConfig)
	if err != nil {
		s.log.Debug("sfu dtls client", zap.Int64("user_id", ep.userID), zap.Error(err))
		ep.close()
		return
	}
	// pion/dtls v3 构造器是惰性握手（首次读写才握手）：必须显式 HandshakeContext
	// 完成并设硬上限，否则 ConnectionState 拿不到状态、停滞的握手会挂死 goroutine。
	hsCtx, hsCancel := context.WithTimeout(context.Background(), 20*time.Second)
	err = dtlsConn.HandshakeContext(hsCtx)
	hsCancel()
	if err != nil {
		s.log.Debug("sfu dtls handshake", zap.Int64("user_id", ep.userID), zap.Error(err))
		ep.close()
		return
	}
	// 指纹核验：DTLS 证书必须与 join JSON 承诺的 sha-256 指纹一致。
	state, ok := dtlsConn.ConnectionState()
	if !ok || len(state.PeerCertificates) == 0 {
		ep.close()
		return
	}
	gotFP, err := certificateFingerprint(state.PeerCertificates[0])
	if err != nil || normalizeFingerprint(gotFP) != normalizeFingerprint(offer.FingerprintSHA256) {
		s.log.Warn("sfu dtls fingerprint mismatch", zap.Int64("user_id", ep.userID))
		ep.close()
		return
	}
	profile, ok := dtlsConn.SelectedSRTPProtectionProfile()
	if !ok {
		ep.close()
		return
	}
	srtpConfig := &srtp.Config{}
	switch profile {
	case dtls.SRTP_AEAD_AES_128_GCM:
		srtpConfig.Profile = srtp.ProtectionProfileAeadAes128Gcm
	case dtls.SRTP_AES128_CM_HMAC_SHA1_80:
		srtpConfig.Profile = srtp.ProtectionProfileAes128CmHmacSha1_80
	default:
		ep.close()
		return
	}
	// SFU 是 DTLS client：isClient=true 决定 SRTP 读写密钥方向。
	if err := srtpConfig.ExtractSessionKeysFromDTLS(&state, true); err != nil {
		s.log.Debug("sfu srtp keys", zap.Error(err))
		ep.close()
		return
	}
	session, err := srtp.NewSessionSRTP(ep.demux.srtpConn(), srtpConfig)
	if err != nil {
		s.log.Debug("sfu srtp session", zap.Error(err))
		ep.close()
		return
	}
	writeStream, err := session.OpenWriteStream()
	if err != nil {
		ep.close()
		return
	}
	// RTCP 升级为 SRTCP session（同一份 srtpConfig 复用）：读侧做 PLI/NACK 路由，
	// 写侧供把反馈转投给发布端。注意：建了 SessionSRTCP 后绝不能再裸读
	// srtcpConn（双读互偷包）。
	rtcpSession, err := srtp.NewSessionSRTCP(ep.demux.srtcpConn(), srtpConfig)
	if err != nil {
		s.log.Debug("sfu srtcp session", zap.Error(err))
		ep.close()
		return
	}
	rtcpWriter, err := rtcpSession.OpenWriteStream()
	if err != nil {
		ep.close()
		return
	}
	ep.mu.Lock()
	ep.writeStream = writeStream
	ep.rtcpWriter = rtcpWriter
	ep.connected = true
	ep.mu.Unlock()
	ep.markActivity()
	s.log.Info("sfu endpoint connected",
		zap.Int64("call_id", ep.callID),
		zap.Int64("user_id", ep.userID),
		zap.Int("kind", int(ep.kind)))

	go ep.serveRTCP(rtcpSession)
	// SCTP 数据通道骑在同一条 DTLS 连接上（握手后的 application data 即 SCTP 包）。
	go ep.serveData(dtlsConn)

	// 入站 SRTP：按 SSRC 接收流。转发/终结决策在流粒度一次性做出
	//（AcceptStream 即按 SSRC 分流，selective forwarding 零每包开销）。
	for {
		stream, ssrc, err := session.AcceptStream()
		if err != nil {
			ep.close()
			return
		}
		if ep.plan.shouldForward(ssrc) {
			go ep.readStream(stream, ssrc)
		} else {
			// 非转发层（高层 simulcast 及其 RTX）：必须持续排空防 buffer 堆积。
			go ep.drainStream(stream)
		}
	}
}

func (ep *endpoint) readStream(stream *srtp.ReadStreamSRTP, ssrc uint32) {
	buf := make([]byte, 1500)
	for {
		n, err := stream.Read(buf)
		if err != nil {
			return
		}
		ep.markActivity()
		packet := make([]byte, n)
		copy(packet, buf[:n])
		ep.sfu.forwardRTP(ep, packet)
	}
}

func (ep *endpoint) drainStream(stream *srtp.ReadStreamSRTP) {
	buf := make([]byte, 1500)
	for {
		if _, err := stream.Read(buf); err != nil {
			return
		}
		ep.markActivity()
	}
}

// serveRTCP 消费本 endpoint 的入站 RTCP 并路由：
//   - PLI / FIR / NACK（订阅端反馈）→ 按 media ssrc 找到发布 endpoint 转投——
//     恒等转发下订阅端反馈里的 ssrc 就是发布端的原始 ssrc，无需改写；
//     发布端自带 RTX 重传与 300ms keyframe 限流；
//   - SR（发布端报告）→ 转发给其他主 endpoint（A/V 同步的 NTP↔RTP 映射来源）；
//   - RR / REMB / transport-cc：丢弃（v1 不做带宽自适应；layer0 所需码率
//     远低于发布端 400kbps 起始值，不会饿死）。
func (ep *endpoint) serveRTCP(session *srtp.SessionSRTCP) {
	for {
		stream, _, err := session.AcceptStream()
		if err != nil {
			return
		}
		go func() {
			buf := make([]byte, 1500)
			for {
				n, err := stream.Read(buf)
				if err != nil {
					return
				}
				ep.markActivity()
				pkts, err := rtcp.Unmarshal(buf[:n])
				if err != nil {
					continue
				}
				ep.routeRTCP(pkts)
			}
		}()
	}
}

func (ep *endpoint) routeRTCP(pkts []rtcp.Packet) {
	for _, pkt := range pkts {
		switch fb := pkt.(type) {
		case *rtcp.PictureLossIndication:
			ep.sfu.writeRTCPToPublisher(ep.callID, fb.MediaSSRC, pkt)
		case *rtcp.FullIntraRequest:
			ep.sfu.writeRTCPToPublisher(ep.callID, fb.MediaSSRC, pkt)
		case *rtcp.TransportLayerNack:
			ep.sfu.writeRTCPToPublisher(ep.callID, fb.MediaSSRC, pkt)
		case *rtcp.SenderReport:
			for _, target := range ep.sfu.mainTargets(ep) {
				target.writeRTCP(pkt)
			}
		}
	}
}

func (s *pionSFU) writeRTCPToPublisher(callID int64, mediaSSRC uint32, pkt rtcp.Packet) {
	publisher := s.publisherBySSRC(callID, mediaSSRC)
	if publisher != nil {
		publisher.writeRTCP(pkt)
	}
}

func (ep *endpoint) writeRTCP(pkt rtcp.Packet) {
	ep.mu.Lock()
	w := ep.rtcpWriter
	ep.mu.Unlock()
	if w == nil {
		return
	}
	raw, err := rtcp.Marshal([]rtcp.Packet{pkt})
	if err != nil {
		return
	}
	_, _ = w.Write(raw)
}

// serveData 在 DTLS 连接上被动承接 SCTP 关联与 DCEP data channel：
//   - 客户端总是 SCTP INIT 与 DATA_CHANNEL_OPEN 的发起方（sid=0, label="data"），
//     SFU 绝不主动 OPEN（客户端没有 accept 入站 OPEN 的逻辑）；
//   - 关联被客户端重建（restartDataChannel）时在同一 DTLS 上重新 accept；
//   - 数据通道上没有任何消息是音视频工作的硬前置：v1 只消费（记录）
//     ReceiverVideoConstraints，其余忽略。
func (ep *endpoint) serveData(conn *dtls.Conn) {
	logf := logging.NewDefaultLoggerFactory()
	logf.DefaultLogLevel = logging.LogLevelError
	for {
		select {
		case <-ep.closed:
			return
		default:
		}
		assoc, err := sctp.Server(sctp.Config{
			NetConn:       conn,
			LoggerFactory: logf,
		})
		if err != nil {
			return // DTLS 关闭/endpoint 拆除
		}
		for {
			dc, err := datachannel.Accept(assoc, &datachannel.Config{LoggerFactory: logf})
			if err != nil {
				break // 关联结束：外层重试承接客户端重建的新关联
			}
			go ep.serveDataChannel(dc)
		}
		_ = assoc.Close()
	}
}

func (ep *endpoint) serveDataChannel(dc *datachannel.DataChannel) {
	defer func() { _ = dc.Close() }()
	buf := make([]byte, 65536)
	for {
		n, isString, err := dc.ReadDataChannel(buf)
		if err != nil {
			return
		}
		if !isString || n == 0 {
			continue
		}
		ep.markActivity()
		var msg struct {
			ColibriClass string `json:"colibriClass"`
		}
		if err := json.Unmarshal(buf[:n], &msg); err != nil {
			continue
		}
		// v1：ReceiverVideoConstraints 仅记录（转发策略固定为 SIM[0] 层广播，
		// 客户端对未订阅 endpoint 的包安全丢弃）；选层/按订阅裁剪留下一轮。
		ep.sfu.log.Debug("sfu data channel message",
			zap.Int64("call_id", ep.callID),
			zap.Int64("user_id", ep.userID),
			zap.String("colibri_class", msg.ColibriClass))
	}
}

func (ep *endpoint) writeRTP(packet []byte) {
	ep.mu.Lock()
	ws := ep.writeStream
	ep.mu.Unlock()
	if ws == nil {
		return
	}
	_, _ = ws.Write(packet)
}

func (ep *endpoint) markActivity() {
	ep.mu.Lock()
	ep.lastActivity = time.Now()
	ep.mu.Unlock()
}

func (ep *endpoint) aliveSince(cutoff time.Time) bool {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	return ep.connected && ep.lastActivity.After(cutoff)
}

func (ep *endpoint) close() {
	ep.closeOnce.Do(func() {
		close(ep.closed)
		if ep.demux != nil {
			ep.demux.Close()
		}
		_ = ep.agent.Close()
		ep.mu.Lock()
		ep.connected = false
		ep.mu.Unlock()
	})
}

// pionLoggerFactory 把 pion 日志降为静默（按需可接 zap）。
type pionLoggerFactory struct{ log *zap.Logger }

func (f *pionLoggerFactory) NewLogger(scope string) logging.LeveledLogger {
	return logging.NewDefaultLoggerFactory().NewLogger(scope)
}
