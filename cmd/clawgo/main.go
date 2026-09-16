package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/clawdbot/clawgo/internal/routing"
	_ "github.com/clawdbot/clawgo/internal/routing/policy/default"
	"github.com/clawdbot/clawgo/modules/audio"
	"github.com/clawdbot/clawgo/modules/stt"

	"github.com/grandcat/zeroconf"
)

type NodeState struct {
	NodeID      string `json:"nodeId"`
	Token       string `json:"token,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

type BridgeClient struct {
	conn         net.Conn
	mu           sync.Mutex
	logf         func(string, ...any)
	done         chan struct{}
	errs         chan error
	frames       chan map[string]any
	eventMu      sync.RWMutex
	eventHandler func(string, string)
	closeOnce    sync.Once
}

type NodeConfig struct {
	BridgeAddr       string
	StatePath        string
	NodeID           string
	DisplayName      string
	Platform         string
	Version          string
	DeviceFamily     string
	ModelIdentifier  string
	Caps             []string
	Commands         []string
	Permissions      map[string]bool
	PairSilent       bool
	SessionKey       string
	PingInterval     time.Duration
	StdinMode        bool
	MDNSEnabled      bool
	MDNSService      string
	MDNSDomain       string
	MDNSName         string
	ChatSessionKey   string
	ChatSubscribe    bool
	AgentRequest     bool
	Deliver          bool
	DeliverChannel   string
	DeliverTo        string
	TTSEngine        string
	TTSSystemVoice   string
	TTSSystemRate    int
	TTSSystemCommand string
	StdinPath        string
	QuickActions     bool
	QuickPingMessage string
	RoutingPlugin    string
	STTEngine        string
	STTCommand       string
	STTArgs          string
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	cmd := os.Args[1]
	switch cmd {
	case "pair":
		cfg := parseFlags(cmd, os.Args[2:])
		if err := runPair(cfg); err != nil {
			fatal(err)
		}
	case "run":
		cfg := parseFlags(cmd, os.Args[2:])
		if err := runNode(cfg); err != nil {
			fatal(err)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: clawgo <pair|run> [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Common flags:")
	fmt.Fprintln(os.Stderr, "  -bridge          Bridge host:port (default 127.0.0.1:18790)")
	fmt.Fprintln(os.Stderr, "  -state           Path to node state JSON (default ~/.clawdbot/clawgo.json)")
	fmt.Fprintln(os.Stderr, "  -node-id         Override node id (default derived from hostname)")
	fmt.Fprintln(os.Stderr, "  -display-name    Friendly display name (default hostname)")
	fmt.Fprintln(os.Stderr, "  -platform        Platform label (default linux)")
	fmt.Fprintln(os.Stderr, "  -version         Client version string (default dev)")
	fmt.Fprintln(os.Stderr, "  -device-family   Device family (default raspi or linux)")
	fmt.Fprintln(os.Stderr, "  -model-identifier Model identifier override")
	fmt.Fprintln(os.Stderr, "  -caps            Comma-separated caps (default voiceWake)")
	fmt.Fprintln(os.Stderr, "  -commands        Comma-separated commands (default none)")
	fmt.Fprintln(os.Stderr, "  -permissions     Comma-separated permission keys to advertise (true)")
	fmt.Fprintln(os.Stderr, "  -pair-silent     Request silent pairing (if supported by gateway)")
	fmt.Fprintln(os.Stderr, "  -session-key     Session key for voice.transcript (default main)")
	fmt.Fprintln(os.Stderr, "  -chat-session-key Session key for chat.subscribe (default mirrors -session-key)")
	fmt.Fprintln(os.Stderr, "  -chat-subscribe  Subscribe to chat stream for TTS (default true)")
	fmt.Fprintln(os.Stderr, "  -agent-request   Send agent.request instead of voice.transcript")
	fmt.Fprintln(os.Stderr, "  -deliver         Deliver agent response to a channel")
	fmt.Fprintln(os.Stderr, "  -deliver-channel Channel for delivery (telegram|whatsapp|signal|imessage)")
	fmt.Fprintln(os.Stderr, "  -deliver-to      Destination id (e.g. telegram chat id)")
	fmt.Fprintln(os.Stderr, "  -tts-engine      TTS engine (system, piper, elevenlabs, none)")
	fmt.Fprintln(os.Stderr, "  -tts-system-voice Voice id for system TTS (default en-us)")
	fmt.Fprintln(os.Stderr, "  -tts-system-rate Speech rate for system TTS (default 180)")
	fmt.Fprintln(os.Stderr, "  -tts-system-command Binary for system TTS (default espeak-ng)")
	fmt.Fprintln(os.Stderr, "  -stdin           Read stdin lines and send voice.transcript events")
	fmt.Fprintln(os.Stderr, "  -stdin-file      Read lines from a file/FIFO instead of stdin")
	fmt.Fprintln(os.Stderr, "  -ping-interval   Ping interval (default 30s)")
	fmt.Fprintln(os.Stderr, "  -quick-actions   Enable built-in quick actions (default true)")
	fmt.Fprintln(os.Stderr, "  -router          Routing plugin name (default default)")
	fmt.Fprintln(os.Stderr, "  -stt-engine      STT engine (line, brabble)")
	fmt.Fprintln(os.Stderr, "  -stt-command     STT command for brabble (default brabble)")
	fmt.Fprintln(os.Stderr, "  -stt-args        STT args for brabble (space-separated)")
	fmt.Fprintln(os.Stderr, "  -ping-message    Message used for telegram ping quick action (default \"Ping.\")")
	fmt.Fprintln(os.Stderr, "  -mdns            Advertise mDNS presence (default true)")
	fmt.Fprintln(os.Stderr, "  -mdns-service    mDNS service type (default _clawdbot-node._tcp)")
	fmt.Fprintln(os.Stderr, "  -mdns-domain     mDNS domain (default local.)")
	fmt.Fprintln(os.Stderr, "  -mdns-name       mDNS instance name override")
}

func parseFlags(cmd string, args []string) NodeConfig {
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	bridge := fs.String("bridge", "127.0.0.1:18790", "bridge host:port")
	state := fs.String("state", defaultStatePath(), "path to node state JSON")
	nodeID := fs.String("node-id", "", "override node id")
	displayName := fs.String("display-name", "", "display name")
	platform := fs.String("platform", "linux", "platform label")
	version := fs.String("version", "dev", "client version string")
	deviceFamily := fs.String("device-family", "", "device family")
	modelIdentifier := fs.String("model-identifier", "", "model identifier")
	caps := fs.String("caps", "voiceWake", "comma-separated caps")
	commands := fs.String("commands", "", "comma-separated commands")
	permissions := fs.String("permissions", "", "comma-separated permission keys")
	pairSilent := fs.Bool("pair-silent", false, "request silent pairing")
	sessionKey := fs.String("session-key", "main", "session key for voice.transcript")
	chatSessionKey := fs.String("chat-session-key", "", "session key for chat.subscribe (default mirrors -session-key)")
	chatSubscribe := fs.Bool("chat-subscribe", true, "subscribe to chat events for TTS")
	agentRequest := fs.Bool("agent-request", false, "send agent.request instead of voice.transcript")
	deliver := fs.Bool("deliver", false, "deliver agent response to channel")
	deliverChannel := fs.String("deliver-channel", "", "deliver channel (telegram|whatsapp|signal|imessage)")
	deliverTo := fs.String("deliver-to", "", "deliver destination id")
	ttsEngine := fs.String("tts-engine", "system", "TTS engine (system, piper, elevenlabs, none)")
	ttsSystemVoice := fs.String("tts-system-voice", "en-us", "voice id for system TTS")
	ttsSystemRate := fs.Int("tts-system-rate", 180, "speech rate for system TTS")
	ttsSystemCommand := fs.String("tts-system-command", "espeak-ng", "binary for system TTS")
	stdinMode := fs.Bool("stdin", false, "read stdin lines for voice.transcript")
	stdinFile := fs.String("stdin-file", "", "read input lines from file/FIFO")
	pingInterval := fs.Duration("ping-interval", 30*time.Second, "ping interval")
	quickActions := fs.Bool("quick-actions", true, "enable built-in quick actions")
	quickPingMessage := fs.String("ping-message", "Ping.", "message used for telegram ping quick action")
	router := fs.String("router", "default", "routing plugin name")
	sttEngine := fs.String("stt-engine", "line", "STT engine (line, brabble)")
	sttCommand := fs.String("stt-command", "brabble", "STT command for brabble")
	sttArgs := fs.String("stt-args", "", "STT args for brabble (space-separated)")
	mdnsEnabled := fs.Bool("mdns", true, "advertise mDNS presence")
	mdnsService := fs.String("mdns-service", "_clawdbot-node._tcp", "mDNS service type")
	mdnsDomain := fs.String("mdns-domain", "local.", "mDNS domain")
	mdnsName := fs.String("mdns-name", "", "mDNS instance name override")
	_ = fs.Parse(args)

	family := strings.TrimSpace(*deviceFamily)
	if family == "" {
		if detected := detectDeviceFamily(); detected != "" {
			family = detected
		}
	}
	if family == "" {
		family = "linux"
	}

	cfg := NodeConfig{
		BridgeAddr:       strings.TrimSpace(*bridge),
		StatePath:        strings.TrimSpace(*state),
		NodeID:           strings.TrimSpace(*nodeID),
		DisplayName:      strings.TrimSpace(*displayName),
		Platform:         strings.TrimSpace(*platform),
		Version:          strings.TrimSpace(*version),
		DeviceFamily:     family,
		ModelIdentifier:  strings.TrimSpace(*modelIdentifier),
		Caps:             splitCSV(*caps),
		Commands:         splitCSV(*commands),
		Permissions:      permissionsFromString(*permissions),
		PairSilent:       *pairSilent,
		SessionKey:       strings.TrimSpace(*sessionKey),
		StdinMode:        *stdinMode,
		PingInterval:     *pingInterval,
		MDNSEnabled:      *mdnsEnabled,
		MDNSService:      strings.TrimSpace(*mdnsService),
		MDNSDomain:       strings.TrimSpace(*mdnsDomain),
		MDNSName:         strings.TrimSpace(*mdnsName),
		ChatSessionKey:   strings.TrimSpace(*chatSessionKey),
		ChatSubscribe:    *chatSubscribe,
		AgentRequest:     *agentRequest,
		Deliver:          *deliver,
		DeliverChannel:   strings.TrimSpace(*deliverChannel),
		DeliverTo:        strings.TrimSpace(*deliverTo),
		TTSEngine:        strings.TrimSpace(*ttsEngine),
		TTSSystemVoice:   strings.TrimSpace(*ttsSystemVoice),
		TTSSystemRate:    *ttsSystemRate,
		TTSSystemCommand: strings.TrimSpace(*ttsSystemCommand),
		StdinPath:        strings.TrimSpace(*stdinFile),
		QuickActions:     *quickActions,
		QuickPingMessage: strings.TrimSpace(*quickPingMessage),
		RoutingPlugin:    strings.TrimSpace(*router),
		STTEngine:        strings.TrimSpace(*sttEngine),
		STTCommand:       strings.TrimSpace(*sttCommand),
		STTArgs:          strings.TrimSpace(*sttArgs),
	}
	if cfg.ChatSessionKey == "" {
		cfg.ChatSessionKey = cfg.SessionKey
	}
	return cfg
}

func runPair(cfg NodeConfig) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	state, err := loadOrInitState(cfg)
	if err != nil {
		return err
	}
	if cfg.NodeID != "" {
		state.NodeID = cfg.NodeID
	}
	if cfg.DisplayName != "" {
		state.DisplayName = cfg.DisplayName
	}

	client, err := connectBridge(ctx, cfg.BridgeAddr)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	defer client.Close()

	client.logf("connected to bridge %s", cfg.BridgeAddr)
	if err := sendPairRequest(client, cfg, state); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}

	token, err := waitForPair(ctx, client)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	state.Token = token
	if err := saveState(cfg.StatePath, state); err != nil {
		return err
	}
	client.logf("paired ok; token saved to %s", cfg.StatePath)
	return nil
}

func runNode(cfg NodeConfig) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logf := func(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) }

	state, err := loadOrInitState(cfg)
	if err != nil {
		return err
	}
	if cfg.NodeID != "" {
		state.NodeID = cfg.NodeID
	}
	if cfg.DisplayName != "" {
		state.DisplayName = cfg.DisplayName
	}

	var transcriptCh <-chan stt.Transcript
	engineName := strings.TrimSpace(cfg.STTEngine)
	if engineName == "" {
		engineName = "line"
	}
	switch engineName {
	case "line":
		if cfg.StdinMode || cfg.StdinPath != "" {
			var capture audio.Capture
			if cfg.StdinPath != "" {
				capture = audio.NewLineCaptureFromPath(cfg.StdinPath, logf)
			} else {
				capture = audio.NewLineCapture("stdin", os.Stdin, logf)
			}
			frames, err := capture.Start(ctx)
			if err != nil {
				return err
			}
			engine := stt.NewLineEngine()
			transcriptCh, err = engine.Transcribe(ctx, frames, stt.Options{})
			if err != nil {
				return err
			}
			logf("stt: %s capture=%s", engine.Name(), capture.Name())
		}
	case "brabble":
		cmd := strings.TrimSpace(cfg.STTCommand)
		args := splitArgs(cfg.STTArgs)
		engine := stt.NewBrabbleEngine(stt.BrabbleConfig{Command: cmd, Args: args}, logf)
		var err error
		transcriptCh, err = engine.Transcribe(ctx, nil, stt.Options{})
		if err != nil {
			return err
		}
		logf("stt: %s cmd=%s", engine.Name(), formatCommand(cmd, args))
	default:
		return fmt.Errorf("unknown stt engine: %s", engineName)
	}

	var mdnsCleanup func()
	mdnsStarted := false
	backoff := time.Second
	var ttsQueue *TTSQueue
	defer func() {
		if ttsQueue != nil {
			ttsQueue.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			if mdnsCleanup != nil {
				mdnsCleanup()
			}
			return nil
		default:
		}

		client, err := connectBridge(ctx, cfg.BridgeAddr)
		if err != nil {
			logf("bridge connect failed: %v", err)
			if err := sleepContext(ctx, backoff); err != nil {
				if mdnsCleanup != nil {
					mdnsCleanup()
				}
				return nil
			}
			if backoff < 15*time.Second {
				backoff *= 2
				if backoff > 15*time.Second {
					backoff = 15 * time.Second
				}
			}
			continue
		}
		backoff = time.Second
		client.logf("connected to bridge %s", cfg.BridgeAddr)

		if strings.TrimSpace(state.Token) == "" {
			client.logf("no token found; requesting pairing")
			if err := sendPairRequest(client, cfg, state); err != nil {
				client.Close()
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			token, err := waitForPair(ctx, client)
			if err != nil {
				client.Close()
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			state.Token = token
			if err := saveState(cfg.StatePath, state); err != nil {
				client.Close()
				return err
			}
			client.logf("paired ok; token saved to %s", cfg.StatePath)
		}

		if err := sendHello(client, cfg, state); err != nil {
			client.Close()
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if err := waitForHello(ctx, client); err != nil {
			client.Close()
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if cfg.MDNSEnabled && !mdnsStarted {
			mdnsCleanup = startMDNS(cfg, state, client.logf)
			mdnsStarted = true
		}

		var chatHandler *ChatSubscriber
		if cfg.ChatSubscribe {
			sessionKey := strings.TrimSpace(cfg.ChatSessionKey)
			engine, err := newNodeTTSEngine(cfg)
			if err != nil {
				client.logf("tts disabled: %v", err)
				ttsQueue = replaceTTSQueue(ttsQueue, nil, client.logf)
			} else if engine != nil && sessionKey != "" {
				ttsQueue = replaceTTSQueue(ttsQueue, engine, client.logf)
				chatHandler = newChatSubscriber(sessionKey, ttsQueue, client.logf)
				if err := subscribeChat(client, sessionKey); err != nil {
					client.logf("chat.subscribe failed: %v", err)
				} else {
					client.logf("chat.subscribe sessionKey=%s", sessionKey)
				}
			} else {
				ttsQueue = replaceTTSQueue(ttsQueue, nil, client.logf)
			}
		}
		if chatHandler != nil {
			client.setEventHandler(func(evt, payload string) {
				if evt == "chat" {
					chatHandler.Handle(payload)
				}
			})
		}

		var router routing.Router
		if transcriptCh != nil {
			routerCfg := routing.Config{
				SessionKey:       cfg.SessionKey,
				AgentRequest:     cfg.AgentRequest,
				Deliver:          cfg.Deliver,
				DeliverChannel:   cfg.DeliverChannel,
				DeliverTo:        cfg.DeliverTo,
				QuickActions:     cfg.QuickActions,
				QuickPingMessage: cfg.QuickPingMessage,
			}
			r, err := routing.New(cfg.RoutingPlugin, routerCfg, bridgeTransport{client: client}, client.logf)
			if err != nil {
				client.logf("router init failed: %v", err)
			} else {
				router = r
			}
		}
		connCtx, connCancel := context.WithCancel(ctx)
		if transcriptCh != nil {
			go forwardTranscripts(connCtx, client, cfg, transcriptCh, router)
		}
		if cfg.PingInterval > 0 {
			go pingLoop(connCtx, client, cfg.PingInterval)
		}

		for {
			select {
			case <-ctx.Done():
				connCancel()
				client.Close()
				if mdnsCleanup != nil {
					mdnsCleanup()
				}
				return nil
			case err := <-client.errs:
				if err != nil {
					client.logf("bridge error: %v", err)
				}
				connCancel()
				client.Close()
				goto reconnect
			case frame := <-client.frames:
				if frame == nil {
					continue
				}
				if err := handleFrame(client, frame); err != nil {
					client.logf("frame error: %v", err)
					connCancel()
					client.Close()
					goto reconnect
				}
			}
		}

	reconnect:
		ttsQueue.Stop()
		ttsQueue = nil
		if ctx.Err() != nil {
			if mdnsCleanup != nil {
				mdnsCleanup()
			}
			return nil
		}
		if err := sleepContext(ctx, backoff); err != nil {
			if mdnsCleanup != nil {
				mdnsCleanup()
			}
			return nil
		}
		if backoff < 15*time.Second {
			backoff *= 2
			if backoff > 15*time.Second {
				backoff = 15 * time.Second
			}
		}
	}
}

func connectBridge(ctx context.Context, addr string) (*BridgeClient, error) {
	if addr == "" {
		return nil, errors.New("bridge address required")
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	client := &BridgeClient{
		conn:   conn,
		logf:   func(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) },
		done:   make(chan struct{}),
		errs:   make(chan error, 1),
		frames: make(chan map[string]any, 16),
	}
	go client.readLoop()
	go func() {
		select {
		case <-ctx.Done():
			client.Close()
		case <-client.done:
		}
	}()
	return client, nil
}

func (c *BridgeClient) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

func (c *BridgeClient) sendFrame(frame any) error {
	payload, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.conn.Write(append(payload, 10))
	return err
}

func (c *BridgeClient) setEventHandler(fn func(string, string)) {
	c.eventMu.Lock()
	c.eventHandler = fn
	c.eventMu.Unlock()
}

func (c *BridgeClient) dispatchEvent(evt, payload string) {
	c.eventMu.RLock()
	handler := c.eventHandler
	c.eventMu.RUnlock()
	if handler != nil {
		handler(evt, payload)
	}
}

func (c *BridgeClient) readLoop() {
	scanner := bufio.NewScanner(c.conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			c.logf("invalid frame: %v", err)
			continue
		}
		select {
		case c.frames <- frame:
		case <-c.done:
			return
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		c.errs <- err
		return
	}
	c.errs <- io.EOF
}

func sendPairRequest(c *BridgeClient, cfg NodeConfig, state *NodeState) error {
	frame := map[string]any{
		"type":            "pair-request",
		"nodeId":          state.NodeID,
		"displayName":     state.DisplayName,
		"platform":        cfg.Platform,
		"version":         cfg.Version,
		"deviceFamily":    cfg.DeviceFamily,
		"modelIdentifier": cfg.ModelIdentifier,
		"caps":            cfg.Caps,
		"commands":        cfg.Commands,
		"permissions":     permissionsOrEmpty(cfg.Permissions),
	}
	if cfg.PairSilent {
		frame["silent"] = true
	}
	return c.sendFrame(frame)
}

func sendHello(c *BridgeClient, cfg NodeConfig, state *NodeState) error {
	frame := map[string]any{
		"type":            "hello",
		"nodeId":          state.NodeID,
		"displayName":     state.DisplayName,
		"token":           state.Token,
		"platform":        cfg.Platform,
		"version":         cfg.Version,
		"deviceFamily":    cfg.DeviceFamily,
		"modelIdentifier": cfg.ModelIdentifier,
		"caps":            cfg.Caps,
		"commands":        cfg.Commands,
		"permissions":     permissionsOrEmpty(cfg.Permissions),
	}
	return c.sendFrame(frame)
}

func subscribeChat(c *BridgeClient, sessionKey string) error {
	if strings.TrimSpace(sessionKey) == "" {
		return nil
	}
	payload, err := json.Marshal(map[string]string{"sessionKey": sessionKey})
	if err != nil {
		return err
	}
	return c.sendFrame(map[string]any{
		"type":        "event",
		"event":       "chat.subscribe",
		"payloadJSON": string(payload),
	})
}

func waitForPair(ctx context.Context, c *BridgeClient) (string, error) {
	deadline := time.After(6 * time.Minute)
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline:
			return "", errors.New("pairing timeout")
		case err := <-c.errs:
			return "", err
		case frame := <-c.frames:
			if frame == nil {
				continue
			}
			if err := handleFrame(c, frame); err != nil {
				return "", err
			}
			if frameType(frame) == "pair-ok" {
				token := frameString(frame, "token")
				if token == "" {
					return "", errors.New("pair-ok missing token")
				}
				return token, nil
			}
		}
	}
}

func waitForHello(ctx context.Context, c *BridgeClient) error {
	deadline := time.After(30 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return errors.New("hello timeout")
		case err := <-c.errs:
			return err
		case frame := <-c.frames:
			if frame == nil {
				continue
			}
			if err := handleFrame(c, frame); err != nil {
				return err
			}
			if frameType(frame) == "hello-ok" {
				serverName := frameString(frame, "serverName")
				canvasHost := frameString(frame, "canvasHostUrl")
				if serverName != "" {
					c.logf("hello ok (server=%s)", serverName)
				}
				if canvasHost != "" {
					c.logf("canvas host: %s", canvasHost)
				}
				return nil
			}
		}
	}
}

func handleFrame(c *BridgeClient, frame map[string]any) error {
	switch frameType(frame) {
	case "ping":
		id := frameString(frame, "id")
		if id != "" {
			_ = c.sendFrame(map[string]any{"type": "pong", "id": id})
		}
	case "error":
		code := frameString(frame, "code")
		msg := frameString(frame, "message")
		if code != "" || msg != "" {
			return fmt.Errorf("bridge error: %s %s", code, msg)
		}
	case "invoke":
		id := frameString(frame, "id")
		if id != "" {
			_ = c.sendFrame(map[string]any{
				"type": "invoke-res",
				"id":   id,
				"ok":   false,
				"error": map[string]any{
					"code":    "UNAVAILABLE",
					"message": "headless node has no commands",
				},
			})
		}
	case "req":
		id := frameString(frame, "id")
		if id != "" {
			_ = c.sendFrame(map[string]any{
				"type": "res",
				"id":   id,
				"ok":   false,
				"error": map[string]any{
					"code":    "UNAVAILABLE",
					"message": "headless node has no RPC",
				},
			})
		}
	case "event":
		evt := frameString(frame, "event")
		if evt != "" {
			payload := frameString(frame, "payloadJSON")
			c.logf("event: %s", evt)
			c.dispatchEvent(evt, payload)
		}
	}
	return nil
}

func forwardTranscripts(ctx context.Context, c *BridgeClient, cfg NodeConfig, in <-chan stt.Transcript, router routing.Router) {
	for {
		select {
		case <-ctx.Done():
			return
		case tr, ok := <-in:
			if !ok {
				return
			}
			text := strings.TrimSpace(tr.Text)
			// The bridge treats every transcript as a complete utterance.
			if !tr.Final || text == "" {
				continue
			}
			if router != nil {
				handled, err := router.HandleTranscript(ctx, text)
				if err != nil {
					c.logf("router error: %v", err)
				}
				if handled {
					continue
				}
			}
			if cfg.AgentRequest {
				if err := sendAgentRequest(c, cfg, text); err != nil {
					c.logf("agent request error: %v", err)
				}
				continue
			}
			if err := sendVoiceTranscript(c, cfg, text); err != nil {
				c.logf("voice transcript error: %v", err)
			}
		}
	}
}

// bridgeTransport adapts the BridgeClient to the routing.Transport interface.
type bridgeTransport struct {
	client *BridgeClient
}

func (t bridgeTransport) SendVoiceTranscript(sessionKey, text string) error {
	return sendVoiceTranscript(t.client, NodeConfig{SessionKey: sessionKey}, text)
}

func (t bridgeTransport) SendAgentRequest(sessionKey, text string, deliver bool, channel, to string) error {
	cfg := NodeConfig{SessionKey: sessionKey, Deliver: deliver, DeliverChannel: channel, DeliverTo: to}
	return sendAgentRequest(t.client, cfg, text)
}

func (t bridgeTransport) SendProviderMessage(provider, to, message string) error {
	return sendProviderMessage(t.client, provider, to, message)
}

func sendVoiceTranscript(c *BridgeClient, cfg NodeConfig, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	payload := map[string]any{"text": text}
	if cfg.SessionKey != "" {
		payload["sessionKey"] = cfg.SessionKey
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		c.logf("voice payload error: %v", err)
		return err
	}
	return c.sendFrame(map[string]any{
		"type":        "event",
		"event":       "voice.transcript",
		"payloadJSON": string(payloadJSON),
	})
}

func sendAgentRequest(c *BridgeClient, cfg NodeConfig, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	payload := map[string]any{"message": text}
	if cfg.SessionKey != "" {
		payload["sessionKey"] = cfg.SessionKey
	}
	if cfg.Deliver && cfg.DeliverChannel != "" {
		payload["deliver"] = true
		payload["channel"] = cfg.DeliverChannel
		if cfg.DeliverTo != "" {
			payload["to"] = cfg.DeliverTo
		}
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		c.logf("agent payload error: %v", err)
		return err
	}
	return c.sendFrame(map[string]any{
		"type":        "event",
		"event":       "agent.request",
		"payloadJSON": string(payloadJSON),
	})
}

func sendProviderMessage(c *BridgeClient, provider, to, message string) error {
	idem := randomID(12)
	payload := map[string]any{
		"to":             to,
		"message":        message,
		"provider":       provider,
		"idempotencyKey": idem,
	}
	paramsJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.sendFrame(map[string]any{
		"type":       "req",
		"id":         randomID(8),
		"method":     "send",
		"paramsJSON": string(paramsJSON),
	})
}

func pingLoop(ctx context.Context, c *BridgeClient, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			id := randomID(8)
			_ = c.sendFrame(map[string]any{"type": "ping", "id": id})
		}
	}
}

type TTSEngine interface {
	Speak(context.Context, string) error
}

type TTSQueue struct {
	engine TTSEngine
	queue  chan string
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	logf   func(string, ...any)
}

func newTTSQueue(engine TTSEngine, logf func(string, ...any)) *TTSQueue {
	if engine == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	q := &TTSQueue{
		engine: engine,
		queue:  make(chan string, 16),
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
		logf:   logf,
	}
	go q.loop()
	return q
}

func replaceTTSQueue(prev *TTSQueue, engine TTSEngine, logf func(string, ...any)) *TTSQueue {
	if prev != nil {
		prev.Stop()
	}
	return newTTSQueue(engine, logf)
}

func (q *TTSQueue) loop() {
	defer close(q.done)
	for {
		select {
		case <-q.ctx.Done():
			return
		case text := <-q.queue:
			select {
			case <-q.ctx.Done():
				return
			default:
			}
			if err := q.engine.Speak(q.ctx, text); err != nil {
				q.logf("tts error: %v", err)
			}
		}
	}
}

func (q *TTSQueue) Stop() {
	if q == nil {
		return
	}
	q.cancel()
	<-q.done
}

func (q *TTSQueue) Speak(text string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	select {
	case <-q.ctx.Done():
		return
	case q.queue <- trimmed:
	default:
		q.logf("tts queue full; dropping text")
	}
}

type systemTTSEngine struct {
	command string
	voice   string
	rate    int
}

func newSystemTTSEngine(cmd, voice string, rate int) (*systemTTSEngine, error) {
	if cmd == "" {
		cmd = "espeak-ng"
	}
	resolved, err := exec.LookPath(cmd)
	if err != nil {
		return nil, err
	}
	return &systemTTSEngine{command: resolved, voice: voice, rate: rate}, nil
}

func (s *systemTTSEngine) Speak(ctx context.Context, text string) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	args := []string{}
	if s.voice != "" {
		args = append(args, "-v", s.voice)
	}
	if s.rate > 0 {
		args = append(args, "-s", strconv.Itoa(s.rate))
	}
	args = append(args, "--", trimmed)
	cmd := exec.CommandContext(ctx, s.command, args...)
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func newNodeTTSEngine(cfg NodeConfig) (TTSEngine, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.TTSEngine)) {
	case "", "none":
		return nil, nil
	case "system":
		return newSystemTTSEngine(cfg.TTSSystemCommand, cfg.TTSSystemVoice, cfg.TTSSystemRate)
	default:
		return nil, fmt.Errorf("unsupported tts engine: %s", cfg.TTSEngine)
	}
}

type ChatSubscriber struct {
	sessionKey string
	tts        *TTSQueue
	logf       func(string, ...any)
	mu         sync.Mutex
	buffers    map[string]*strings.Builder
}

func newChatSubscriber(sessionKey string, tts *TTSQueue, logf func(string, ...any)) *ChatSubscriber {
	return &ChatSubscriber{
		sessionKey: strings.TrimSpace(sessionKey),
		tts:        tts,
		logf:       logf,
		buffers:    make(map[string]*strings.Builder),
	}
}

func (c *ChatSubscriber) Handle(payloadJSON string) {
	if strings.TrimSpace(payloadJSON) == "" {
		return
	}
	var payload chatPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		c.logf("chat payload decode failed: %v", err)
		return
	}
	if c.sessionKey != "" && payload.SessionKey != "" && payload.SessionKey != c.sessionKey {
		return
	}
	switch payload.State {
	case "delta":
		if text := payload.text(); text != "" {
			c.append(payload.RunID, text)
		}
	case "final":
		text := payload.text()
		if text == "" {
			text = c.consume(payload.RunID)
		} else {
			c.clear(payload.RunID)
		}
		c.speak(text)
	case "error":
		c.clear(payload.RunID)
	default:
		if text := payload.text(); text != "" {
			c.speak(text)
		}
	}
}

func (c *ChatSubscriber) append(runID, text string) {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	buf := c.buffers[runID]
	if buf == nil {
		buf = &strings.Builder{}
		c.buffers[runID] = buf
	}
	if buf.Len() > 0 {
		buf.WriteString(" ")
	}
	buf.WriteString(clean)
}

func (c *ChatSubscriber) consume(runID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	buf := c.buffers[runID]
	if buf == nil {
		return ""
	}
	text := buf.String()
	delete(c.buffers, runID)
	return strings.TrimSpace(text)
}

func (c *ChatSubscriber) clear(runID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.buffers, runID)
}

func (c *ChatSubscriber) speak(text string) {
	text = strings.TrimSpace(text)
	if text == "" || c.tts == nil {
		return
	}
	c.tts.Speak(text)
}

type chatPayload struct {
	RunID      string       `json:"runId"`
	SessionKey string       `json:"sessionKey"`
	State      string       `json:"state"`
	Message    *chatMessage `json:"message"`
}

type chatMessage struct {
	Role    string        `json:"role"`
	Content []chatContent `json:"content"`
}

type chatContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (p *chatPayload) text() string {
	if p.Message == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range p.Message.Content {
		if strings.TrimSpace(part.Text) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		b.WriteString(strings.TrimSpace(part.Text))
	}
	return strings.TrimSpace(b.String())
}

func startMDNS(cfg NodeConfig, state *NodeState, logf func(string, ...any)) func() {
	if !cfg.MDNSEnabled {
		return nil
	}
	service := strings.TrimSpace(cfg.MDNSService)
	if service == "" {
		service = "_clawdbot-node._tcp"
	}
	domain := strings.TrimSpace(cfg.MDNSDomain)
	if domain == "" {
		domain = "local."
	}
	name := strings.TrimSpace(cfg.MDNSName)
	if name == "" {
		name = strings.TrimSpace(state.DisplayName)
	}
	if name == "" {
		name = defaultDisplayName()
	}
	if !strings.Contains(strings.ToLower(name), "clawdbot") {
		name = fmt.Sprintf("%s (Clawdbot)", name)
	}
	displayName := prettifyInstanceName(name)
	hostLabel := strings.TrimSpace(defaultDisplayName())
	hostLabel = strings.TrimSuffix(hostLabel, ".local")
	if strings.Contains(hostLabel, ".") {
		parts := strings.Split(hostLabel, ".")
		if len(parts) > 0 {
			hostLabel = parts[0]
		}
	}
	if hostLabel == "" {
		hostLabel = "clawgo"
	}
	bridgeHost, bridgePort := parseBridgeAddr(cfg.BridgeAddr)
	txt := []string{fmt.Sprintf("role=%s", "node"), fmt.Sprintf("displayName=%s", displayName), fmt.Sprintf("lanHost=%s.local", hostLabel), fmt.Sprintf("nodeId=%s", state.NodeID), "transport=node"}
	if bridgeHost != "" {
		txt = append(txt, fmt.Sprintf("bridgeHost=%s", bridgeHost))
	}
	if bridgePort > 0 {
		txt = append(txt, fmt.Sprintf("bridgePort=%d", bridgePort))
	}
	if cfg.Platform != "" {
		txt = append(txt, fmt.Sprintf("platform=%s", cfg.Platform))
	}
	if cfg.Version != "" {
		txt = append(txt, fmt.Sprintf("version=%s", cfg.Version))
	}
	if cfg.DeviceFamily != "" {
		txt = append(txt, fmt.Sprintf("deviceFamily=%s", cfg.DeviceFamily))
	}
	if cfg.ModelIdentifier != "" {
		txt = append(txt, fmt.Sprintf("modelIdentifier=%s", cfg.ModelIdentifier))
	}

	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		logf("mdns listen failed: %v", err)
		return nil
	}
	port := listener.Addr().(*net.TCPAddr).Port
	server, err := zeroconf.Register(name, service, domain, port, txt, nil)
	if err != nil {
		_ = listener.Close()
		logf("mdns register failed: %v", err)
		return nil
	}
	logf("mdns: advertised %s on %s (%s) port=%d", name, service, domain, port)
	return func() {
		server.Shutdown()
		_ = listener.Close()
	}
}

func parseBridgeAddr(addr string) (string, int) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", 0
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, 0
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return host, 0
	}
	return host, port
}

func defaultStatePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "./clawgo.json"
	}
	return filepath.Join(home, ".clawdbot", "clawgo.json")
}

func loadOrInitState(cfg NodeConfig) (*NodeState, error) {
	path := cfg.StatePath
	if path == "" {
		path = defaultStatePath()
	}
	data, err := os.ReadFile(path)
	if err == nil {
		var st NodeState
		if err := json.Unmarshal(data, &st); err != nil {
			return nil, err
		}
		if st.NodeID == "" {
			st.NodeID = deriveNodeID()
		}
		if st.DisplayName == "" {
			st.DisplayName = defaultDisplayName()
		}
		return &st, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	st := &NodeState{
		NodeID:      deriveNodeID(),
		DisplayName: defaultDisplayName(),
	}
	if err := saveState(path, st); err != nil {
		return nil, err
	}
	return st, nil
}

func saveState(path string, st *NodeState) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o600)
}

func deriveNodeID() string {
	host := defaultDisplayName()
	base := sanitizeID(host)
	if mid := machineID(); mid != "" {
		return fmt.Sprintf("%s-%s", base, mid[:8])
	}
	return fmt.Sprintf("%s-%s", base, randomID(6))
}

func defaultDisplayName() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return "clawgo"
	}
	return host
}

func machineID() string {
	data, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return ""
	}
	id := strings.TrimSpace(string(data))
	id = strings.ReplaceAll(id, "-", "")
	if len(id) < 8 {
		return ""
	}
	return id
}

func sanitizeID(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return "node"
	}
	var b strings.Builder
	for _, r := range input {
		if (r >= 97 && r <= 122) || (r >= 48 && r <= 57) {
			b.WriteRune(r)
			continue
		}
		if r == 45 || r == 95 {
			b.WriteRune(45)
			continue
		}
		b.WriteRune(45)
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "node"
	}
	return out
}

func randomID(n int) string {
	if n <= 0 {
		n = 6
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func splitArgs(input string) []string {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func formatCommand(cmd string, args []string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		cmd = "brabble"
	}
	if len(args) == 0 {
		return cmd
	}
	return fmt.Sprintf("%s %s", cmd, strings.Join(args, " "))
}

func splitCSV(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func permissionsOrEmpty(perms map[string]bool) map[string]bool {
	if perms == nil {
		return map[string]bool{}
	}
	return perms
}

func permissionsFromString(value string) map[string]bool {
	items := splitCSV(value)
	if len(items) == 0 {
		return map[string]bool{}
	}
	out := make(map[string]bool, len(items))
	for _, item := range items {
		out[item] = true
	}
	return out
}

func prettifyInstanceName(name string) string {
	normalized := strings.TrimSpace(strings.Join(strings.Fields(name), " "))
	if normalized == "" {
		return name
	}
	lower := strings.ToLower(normalized)
	suffix := " (clawdbot)"
	if strings.HasSuffix(lower, suffix) {
		normalized = strings.TrimSpace(normalized[:len(normalized)-len(suffix)])
	}
	return normalized
}

func frameType(frame map[string]any) string {
	if frame == nil {
		return ""
	}
	if v, ok := frame["type"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprint(v)
	}
	return ""
}

func frameString(frame map[string]any, key string) string {
	if frame == nil {
		return ""
	}
	if v, ok := frame[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		if v != nil {
			return fmt.Sprint(v)
		}
	}
	return ""
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}

func detectDeviceFamily() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	if strings.Contains(strings.ToLower(string(data)), "raspberry pi") {
		return "raspi"
	}
	return ""
}
