// Command starcheck 是 Stars 付费 reaction 端到端验证工具（开发用，非生产组件）。
//
// 它以用户身份（开发码 12345）登录本地 telesrv，验证 Stars 本地账本与付费 reaction：
//  1. payments.getStarsStatus —— 打印当前余额（Phase 1：惰性首读授予后应 >0）。
//  2. 自动从 dialogs 找一个广播频道 + 其顶部消息（或用 -channel-id/-access-hash/-msg-id 指定）。
//  3. messages.sendPaidReaction —— 发付费 reaction，打印返回的 Updates 结构
//     （Phase 2 崩溃约束：必须是 *tg.Updates，应含 updateMessageReactions + updateStarsBalance）。
//  4. payments.getStarsStatus —— 再次打印余额，验证已扣 -count。
//
// 用法：
//
//	go run ./cmd/starcheck -phone "+8618800000001" -count 50
package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/gotd/log/logzap"
	"github.com/iamxvbaba/td/exchange"
	"github.com/iamxvbaba/td/mtproxy"
	"github.com/iamxvbaba/td/mtproxy/obfuscator"
	"github.com/iamxvbaba/td/proto/codec"
	"github.com/iamxvbaba/td/telegram"
	"github.com/iamxvbaba/td/telegram/dcs"
	"github.com/iamxvbaba/td/tg"
	"github.com/iamxvbaba/td/transport"

	"telesrv/internal/mtprotoedge"
)

type obfuscatedResolver struct {
	host string
	port int
}

func (r obfuscatedResolver) dial(ctx context.Context, dc int) (transport.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(r.host, strconv.Itoa(r.port)))
	if err != nil {
		return nil, err
	}
	obf := obfuscator.Obfuscated2(rand.Reader, conn)
	if err := obf.Handshake(codec.IntermediateClientStart, dc, mtproxy.Secret{}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("obfuscated2 handshake: %w", err)
	}
	proto := transport.NewProtocol(func() transport.Codec {
		return codec.NoHeader{Codec: codec.Intermediate{}}
	})
	tc, err := proto.Handshake(obf)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("transport handshake: %w", err)
	}
	return tc, nil
}

func (r obfuscatedResolver) Primary(ctx context.Context, dc int, _ dcs.List) (transport.Conn, error) {
	return r.dial(ctx, dc)
}
func (r obfuscatedResolver) MediaOnly(ctx context.Context, dc int, _ dcs.List) (transport.Conn, error) {
	return r.dial(ctx, dc)
}
func (r obfuscatedResolver) CDN(ctx context.Context, dc int, _ dcs.List) (transport.Conn, error) {
	return r.dial(ctx, dc)
}

func main() {
	addr := flag.String("addr", "127.0.0.1:2398", "telesrv MTProto address")
	dcID := flag.Int("dc", 2, "DC id")
	rsaPath := flag.String("rsa", "data/server_rsa.pem", "server RSA key path")
	apiID := flag.Int("api-id", 1, "api_id")
	apiHash := flag.String("api-hash", "hash", "api_hash")
	phone := flag.String("phone", "", "login phone number, e.g. +8618800000001")
	code := flag.String("code", "12345", "开发登录码")
	count := flag.Int("count", 50, "付费 reaction 星数")
	channelID := flag.Int64("channel-id", 0, "指定频道 id（0=自动从 dialogs 找广播频道）")
	accessHash := flag.Int64("access-hash", 0, "指定频道 access_hash")
	msgID := flag.Int("msg-id", 0, "指定消息 id（0=用频道顶部消息）")
	flag.Parse()

	if *phone == "" {
		fmt.Fprintln(os.Stderr, "missing -phone")
		os.Exit(2)
	}

	logger, _ := zap.NewDevelopment()
	defer func() { _ = logger.Sync() }()

	priv, err := mtprotoedge.LoadOrGenerateRSAKey(*rsaPath)
	if err != nil {
		logger.Fatal("failed to load RSA key", zap.Error(err))
	}
	host, portStr, err := net.SplitHostPort(*addr)
	if err != nil {
		logger.Fatal("failed to parse address", zap.Error(err))
	}
	port, _ := strconv.Atoi(portStr)

	client := telegram.NewClient(*apiID, *apiHash, telegram.Options{
		PublicKeys: []exchange.PublicKey{{RSA: &priv.PublicKey}},
		Resolver:   obfuscatedResolver{host: host, port: port},
		DCList: dcs.List{Options: []tg.DCOption{
			{ID: *dcID, IPAddress: host, Port: port, Static: true},
		}},
		Logger: logzap.New(logger.Named("client")),
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := client.Run(ctx, func(ctx context.Context) error {
		raw := tg.NewClient(client)

		// 1. 用户登录（开发码）。
		sent, err := raw.AuthSendCode(ctx, &tg.AuthSendCodeRequest{
			PhoneNumber: *phone, APIID: *apiID, APIHash: *apiHash,
			Settings: tg.CodeSettings{},
		})
		if err != nil {
			return fmt.Errorf("sendCode: %w", err)
		}
		sentCode, ok := sent.(*tg.AuthSentCode)
		if !ok {
			return fmt.Errorf("sentCode 类型 = %T", sent)
		}
		authz, err := raw.AuthSignIn(ctx, &tg.AuthSignInRequest{
			PhoneNumber: *phone, PhoneCodeHash: sentCode.PhoneCodeHash, PhoneCode: *code,
		})
		if err != nil {
			return fmt.Errorf("signIn: %w", err)
		}
		a, ok := authz.(*tg.AuthAuthorization)
		if !ok {
			return fmt.Errorf("authorization 类型 = %T", authz)
		}
		self, _ := a.User.(*tg.User)
		fmt.Printf("==== login successful: id=%d name=%q phone=%s ====\n", self.ID, self.FirstName, *phone)

		// 2. 余额（before）。
		balBefore := printStarsBalance(ctx, raw, "扣费前")

		// 3. 解析目标频道 + 消息。
		var peer *tg.InputPeerChannel
		mid := *msgID
		if *channelID != 0 {
			peer = &tg.InputPeerChannel{ChannelID: *channelID, AccessHash: *accessHash}
		} else {
			ch, topMsg, err := findBroadcastChannel(ctx, raw)
			if err != nil {
				return err
			}
			peer = &tg.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}
			if mid == 0 {
				mid = topMsg
			}
			fmt.Printf("  target channel: id=%d title=%q access_hash=%d top_msg=%d\n", ch.ID, ch.Title, ch.AccessHash, mid)
		}
		if mid == 0 {
			return fmt.Errorf("no available message id (channel empty? use -msg-id to specify)")
		}

		// 4. 发付费 reaction。
		rid, _ := randInt64()
		fmt.Printf(">> sendPaidReaction: channel=%d msg=%d count=%d\n", peer.ChannelID, mid, *count)
		res, err := raw.MessagesSendPaidReaction(ctx, &tg.MessagesSendPaidReactionRequest{
			Peer: peer, MsgID: mid, Count: *count, RandomID: rid,
		})
		if err != nil {
			return fmt.Errorf("sendPaidReaction: %w", err)
		}
		inspectPaidReactionUpdates(res)

		// 5. 余额（after）。
		balAfter := printStarsBalance(ctx, raw, "after charge")
		fmt.Printf("==== charge check: %d -> %d, diff %d (expected -%d) ====\n", balBefore, balAfter, balBefore-balAfter, *count)
		if balBefore-balAfter == int64(*count) {
			fmt.Println("==== ✅ PASS: paid reaction charged correctly ====")
		} else {
			fmt.Println("==== ❌ FAIL: charged amount mismatch ====")
		}
		return nil
	}); err != nil {
		logger.Fatal("run failed", zap.Error(err))
	}
}

func printStarsBalance(ctx context.Context, raw *tg.Client, label string) int64 {
	status, err := raw.PaymentsGetStarsStatus(ctx, &tg.PaymentsGetStarsStatusRequest{Peer: &tg.InputPeerSelf{}})
	if err != nil {
		fmt.Printf("  [%s] getStarsStatus failed: %v\n", label, err)
		return -1
	}
	amount, _ := status.Balance.(*tg.StarsAmount)
	var bal int64
	if amount != nil {
		bal = amount.Amount
	}
	fmt.Printf("  [%s] balance=%d stars (balance type=%T, chats=%d users=%d)\n", label, bal, status.Balance, len(status.Chats), len(status.Users))
	return bal
}

// findBroadcastChannel 从 dialogs 找第一个广播频道 + 其顶部消息 id。
func findBroadcastChannel(ctx context.Context, raw *tg.Client) (*tg.Channel, int, error) {
	dlgs, err := raw.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		OffsetPeer: &tg.InputPeerEmpty{}, Limit: 100,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("getDialogs: %w", err)
	}
	var chats []tg.ChatClass
	var dialogs []tg.DialogClass
	switch v := dlgs.(type) {
	case *tg.MessagesDialogs:
		chats, dialogs = v.Chats, v.Dialogs
	case *tg.MessagesDialogsSlice:
		chats, dialogs = v.Chats, v.Dialogs
	default:
		return nil, 0, fmt.Errorf("dialogs 类型 = %T", dlgs)
	}
	topByChannel := make(map[int64]int)
	for _, d := range dialogs {
		dd, ok := d.(*tg.Dialog)
		if !ok {
			continue
		}
		if pc, ok := dd.Peer.(*tg.PeerChannel); ok {
			topByChannel[pc.ChannelID] = dd.TopMessage
		}
	}
	for _, c := range chats {
		ch, ok := c.(*tg.Channel)
		if !ok || !ch.Broadcast || ch.Megagroup {
			continue
		}
		return ch, topByChannel[ch.ID], nil
	}
	return nil, 0, fmt.Errorf("dialogs 中无广播频道")
}

// inspectPaidReactionUpdates 检查返回的 Updates 是否合法且含 updateMessageReactions/updateStarsBalance。
func inspectPaidReactionUpdates(res tg.UpdatesClass) {
	fmt.Printf("  returned type=%T\n", res)
	var ups []tg.UpdateClass
	switch v := res.(type) {
	case *tg.Updates:
		ups = v.Updates
	case *tg.UpdatesCombined:
		ups = v.Updates
	case *tg.UpdateShort:
		ups = []tg.UpdateClass{v.Update}
	default:
		fmt.Printf("  [warning] returned non-Updates subtype (DrKLO may ClassCastException)\n")
		return
	}
	hasReactions, hasBalance := false, false
	for _, up := range ups {
		switch u := up.(type) {
		case *tg.UpdateMessageReactions:
			hasReactions = true
			paid := 0
			for _, rc := range u.Reactions.Results {
				if _, ok := rc.Reaction.(*tg.ReactionPaid); ok {
					paid = rc.Count
				}
			}
			reactors, _ := u.Reactions.GetTopReactors()
			fmt.Printf("    updateMessageReactions: msg=%d paid_count=%d top_reactors=%d\n", u.MsgID, paid, len(reactors))
		case *tg.UpdateStarsBalance:
			hasBalance = true
			amount, _ := u.Balance.(*tg.StarsAmount)
			var b int64
			if amount != nil {
				b = amount.Amount
			}
			fmt.Printf("    updateStarsBalance: balance=%d\n", b)
		}
	}
	fmt.Printf("  valid Updates=true, has updateMessageReactions=%v, has updateStarsBalance=%v\n", hasReactions, hasBalance)
}

func randInt64() (int64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UnixNano(), err
	}
	v := int64(binary.LittleEndian.Uint64(b[:]))
	if v == 0 {
		v = 1
	}
	return v, nil
}
