// Command botcheck 是 bot 客户端验证工具（开发用，非生产组件）。
//
// 它用 BotFather 发的 token 经 auth.importBotAuthorization 登录本地 telesrv，
// 自检 bot 身份 / updates 状态 / userFull.bot_info，并可选以 echo 模式持续回显
// 收到的私聊消息，验证 bot 收发闭环（在线推送 → bot 处理 → bot 回复）。
//
// 用法：
//
//	go run ./cmd/bots/botcheck -token "<bot_id>:<secret>"            # 仅登录自检
//	go run ./cmd/bots/botcheck -token "<bot_id>:<secret>" -echo      # 自检后持续 echo
//
// 连接生产 telesrv（obfuscated TCP）靠 DCOption.TCPObfuscatedOnly=true，
// gotd dcs.Plain 据此自动走 MTProto TCP obfuscation。
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

// obfuscatedResolver 用标准无-secret MTProto TCP obfuscation（obfuscated2）连接，
// 匹配 telesrv 生产 server 的 transport.ObfuscatedListener（obfuscated2.Accept(conn, nil)）。
// gotd 内置 dcs.Plain 的 obfuscated 路径走 MTProxy（强制 secret），不适用这里。
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
	// obfuscated2 握手携带 intermediate codec 的 init tag；空 secret = 标准 obfuscation。
	obf := obfuscator.Obfuscated2(rand.Reader, conn)
	if err := obf.Handshake(codec.IntermediateClientStart, dc, mtproxy.Secret{}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("obfuscated2 handshake: %w", err)
	}
	// NoHeader：tag 已由 obf 握手发出，codec 不再重发。
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
	token := flag.String("token", os.Getenv("TELESRV_BOT_TOKEN"), "bot token <bot_id>:<secret> (defaults to TELESRV_BOT_TOKEN env)")
	rsaPath := flag.String("rsa", "data/server_rsa.pem", "server RSA key path (public key only)")
	apiID := flag.Int("api-id", 1, "api_id")
	apiHash := flag.String("api-hash", "hash", "api_hash")
	echo := flag.Bool("echo", false, "keep echoing received private messages after login")
	sendTo := flag.Int64("send-to", 0, "proactively send to user_id (verify bot can send)")
	sendText := flag.String("send-text", "", "text to proactively send")
	runFor := flag.Duration("for", 0, "echo mode run duration, 0 = until Ctrl+C")
	flag.Parse()

	if *token == "" {
		fmt.Fprintln(os.Stderr, "missing -token (or set TELESRV_BOT_TOKEN env var)")
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
	port, err := strconv.Atoi(portStr)
	if err != nil {
		logger.Fatal("failed to parse port", zap.Error(err))
	}

	var client *telegram.Client
	handler := telegram.UpdateHandlerFunc(func(ctx context.Context, u tg.UpdatesClass) error {
		if !*echo {
			return nil
		}
		return echoUpdates(ctx, tg.NewClient(client), u, logger)
	})

	client = telegram.NewClient(*apiID, *apiHash, telegram.Options{
		PublicKeys: []exchange.PublicKey{{RSA: &priv.PublicKey}},
		Resolver:   obfuscatedResolver{host: host, port: port},
		DCList: dcs.List{Options: []tg.DCOption{
			{ID: *dcID, IPAddress: host, Port: port, Static: true},
		}},
		Logger:        logzap.New(logger.Named("client")),
		UpdateHandler: handler,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := client.Run(ctx, func(ctx context.Context) error {
		raw := tg.NewClient(client)

		authz, err := raw.AuthImportBotAuthorization(ctx, &tg.AuthImportBotAuthorizationRequest{
			APIID: *apiID, APIHash: *apiHash, BotAuthToken: *token,
		})
		if err != nil {
			return fmt.Errorf("importBotAuthorization: %w", err)
		}
		a, ok := authz.(*tg.AuthAuthorization)
		if !ok {
			return fmt.Errorf("authorization 类型 = %T，want *tg.AuthAuthorization", authz)
		}
		self, ok := a.User.(*tg.User)
		if !ok {
			return fmt.Errorf("self 类型 = %T，want *tg.User", a.User)
		}
		ver, hasVer := self.GetBotInfoVersion()
		fmt.Println("==== login successful ====")
		fmt.Printf("  id=%d  username=@%s  name=%q  bot=%v  self=%v  bot_info_version=%d(present=%v)\n",
			self.ID, self.Username, self.FirstName, self.Bot, self.Self, ver, hasVer)
		if !self.Bot || !hasVer {
			fmt.Println("  [warning] self missing bot flag or bot_info_version — TDesktop will not treat as bot")
		}
		if _, hasStatus := self.GetStatus(); hasStatus {
			fmt.Println("  [warning] bot has status — official bots should not have presence")
		}
		if self.Phone != "" {
			fmt.Printf("  [warning] bot carries phone=%q — official bots should not have phone numbers\n", self.Phone)
		}

		state, err := raw.UpdatesGetState(ctx)
		if err != nil {
			return fmt.Errorf("getState: %w", err)
		}
		fmt.Printf("  getState: pts=%d qts=%d date=%d seq=%d\n", state.Pts, state.Qts, state.Date, state.Seq)

		full, err := raw.UsersGetFullUser(ctx, &tg.InputUserSelf{})
		if err != nil {
			return fmt.Errorf("getFullUser: %w", err)
		}
		if bi, ok := full.FullUser.GetBotInfo(); ok {
			uid, _ := bi.GetUserID()
			desc, _ := bi.GetDescription()
			cmds, _ := bi.GetCommands()
			fmt.Printf("  bot_info: user_id=%d  description=%q  commands=%d\n", uid, desc, len(cmds))
			if uid != self.ID {
				fmt.Printf("  [warning] bot_info.user_id=%d != self.id=%d — TDesktop will ignore bot_info\n", uid, self.ID)
			}
			for _, c := range cmds {
				fmt.Printf("    /%s - %s\n", c.Command, c.Description)
			}
		} else {
			fmt.Println("  [warning] userFull missing bot_info — TDesktop will repeatedly re-fetch getFullUser")
		}

		fmt.Println("==== self-check passed ====")

		// 主动发起：bot 主动给指定用户发消息（用户须先与 bot 交互过）。
		// access_hash 经 getUsers 解析（bot 重新登录时手里没有该用户的 update）。
		if *sendTo != 0 && *sendText != "" {
			target := &tg.InputPeerUser{UserID: *sendTo}
			if list, err := raw.UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUser{UserID: *sendTo}}); err == nil {
				for _, uc := range list {
					if u, ok := uc.(*tg.User); ok && u.ID == *sendTo {
						target.AccessHash = u.AccessHash
						fmt.Printf("  resolved target user: id=%d username=@%s name=%q\n", u.ID, u.Username, u.FirstName)
					}
				}
			} else {
				fmt.Printf("  [warning] getUsers(%d) failed: %v (still trying access_hash=0 send)\n", *sendTo, err)
			}
			rid, _ := randInt64()
			if _, err := raw.MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
				Peer:     target,
				Message:  *sendText,
				RandomID: rid,
			}); err != nil {
			return fmt.Errorf("proactive send: %w", err)
			}
			fmt.Printf(">> proactively sent to %d: %q\n", *sendTo, *sendText)
		}

		if !*echo {
			return nil
		}
		fmt.Println(">> echo mode: send a private message from TDesktop/Android to this bot; bot will reply \"echo: <text>\"")
		if *runFor > 0 {
			fmt.Printf(">> will run for %s then exit (or Ctrl+C)\n", *runFor)
			t := time.NewTimer(*runFor)
			defer t.Stop()
			select {
			case <-ctx.Done():
			case <-t.C:
			}
			return nil
		}
		fmt.Println(">> Ctrl+C to exit")
		<-ctx.Done()
		return nil
	}); err != nil {
		logger.Fatal("run failed", zap.Error(err))
	}
	fmt.Println("exited")
}

// echoUpdates 解析推送的 updates，对收到的 incoming 私聊消息回 "echo: <原文>"。
func echoUpdates(ctx context.Context, raw *tg.Client, u tg.UpdatesClass, logger *zap.Logger) error {
	var ups []tg.UpdateClass
	var users []tg.UserClass
	switch v := u.(type) {
	case *tg.Updates:
		ups, users = v.Updates, v.Users
	case *tg.UpdatesCombined:
		ups, users = v.Updates, v.Users
	case *tg.UpdateShort:
		ups = []tg.UpdateClass{v.Update}
	default:
		return nil
	}
	hashByID := make(map[int64]int64, len(users))
	for _, uc := range users {
		if usr, ok := uc.(*tg.User); ok {
			hashByID[usr.ID] = usr.AccessHash
		}
	}
	for _, up := range ups {
		nm, ok := up.(*tg.UpdateNewMessage)
		if !ok {
			continue
		}
		msg, ok := nm.Message.(*tg.Message)
		if !ok || msg.Out {
			continue
		}
		peer, ok := msg.PeerID.(*tg.PeerUser)
		if !ok {
			continue
		}
		from := peer.UserID
		fmt.Printf("<< received from %d: %q\n", from, msg.Message)
		rid, _ := randInt64()
		if _, err := raw.MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
			Peer:     &tg.InputPeerUser{UserID: from, AccessHash: hashByID[from]},
			Message:  "echo: " + msg.Message,
			RandomID: rid,
		}); err != nil {
			logger.Warn("echo reply failed", zap.Int64("to", from), zap.Error(err))
			continue
		}
		fmt.Printf(">> replied to %d: %q\n", from, "echo: "+msg.Message)
	}
	return nil
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
