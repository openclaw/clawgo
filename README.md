# Clawgo (Go Node)

![Clawgo banner](docs/assets/readme-banner.jpg)

Minimal headless node client for Raspberry Pi / Linux. Connects to the gateway bridge, handles pairing, streams `voice.transcript` events (stdin/FIFO), subscribes to chat, and can speak responses via local TTS.

## Build

Requires Go 1.26 or newer. The preferred toolchain in `go.mod` is Go 1.27.0, which Go downloads automatically when toolchain switching is enabled; CI uses the same version.

```bash
cd clawgo
go build ./cmd/clawgo
```

Cross-compile for Pi:

```bash
GOOS=linux GOARCH=arm64 go build -o /tmp/clawgo-linux-arm64 ./cmd/clawgo
```

## Key flags

| Flag | Description |
| --- | --- |
| `-session-key` | Session for outgoing `voice.transcript` events (default `main`). |
| `-chat-session-key` | Session to subscribe for chat replies (default mirrors `-session-key`). |
| `-chat-subscribe` | Enable chat stream+TTS (default `true`). |
| `-tts-engine` | `system`, `piper`, `elevenlabs`, or `none` (system = `espeak-ng`). |
| `-tts-system-voice` | espeak voice id (default `en-us`). |
| `-tts-system-command` | espeak-compatible executable; receives voice/rate options, `--`, then the utterance. |
| `-tts-system-rate` | Speech rate (wpm). |
| `-mdns-service` | Bonjour service type (default `_clawdbot-node._tcp`). |
| `-stdin` | Read transcripts from stdin (pipe/FIFO). |
| `-stdin-file` | Read transcripts from a FIFO/file instead of stdin. |
| `-agent-request` | Send transcripts as `agent.request` (uses agent + deliver). |
| `-deliver` | Deliver agent responses to a provider (requires channel + to). |
| `-deliver-channel` | Delivery provider (telegram/whatsapp/signal/imessage). |
| `-deliver-to` | Delivery destination id. |
| `-quick-actions` | Enable built-in quick actions (default true). |
| `-ping-message` | Message used for telegram ping quick action. |
| `-router` | Routing plugin name (default `default`). |

## Pair

```bash
./clawgo pair \
  -bridge 100.88.46.29:18790 \
  -display-name "Razor Pi"
```

Approve via `clawdbot nodes approve <requestId>`.

## Run (FIFO + TTS example)

```bash
mkfifo /tmp/voice.fifo
# in one terminal
tail -f /tmp/voice.fifo | ./clawgo run \
  -bridge 100.88.46.29:18790 \
  -stdin \
  -chat-subscribe \
  -tts-engine system
# elsewhere
printf hey computer turn on the lights
 > /tmp/voice.fifo
```

Each line on the FIFO becomes a `voice.transcript`; chat responses from the `main` session are spoken via `espeak-ng`.

A bridge disconnect or node shutdown cancels pending and active speech. Custom TTS wrappers should use `exec` to run their speech command so cancellation reaches it.

With `-stt-engine brabble`, only final utterances are routed or sent to the bridge; interim hypotheses do not trigger requests or quick actions.

SIGINT and SIGTERM interrupt reconnect backoff immediately, including when the bridge is unavailable.
They also cancel bridge dialing and pairing/hello waits in `run` and `pair`, closing the connection before exit.

Spoken text is passed after `--` so leading dashes are treated as words. Custom `-tts-system-command` wrappers must preserve the option separator when forwarding arguments to espeak-ng.

## systemd example

Minimal steps:

1. Install the binary as `/home/pi/clawgo`.
2. Create a wrapper script that keeps a FIFO (`/home/pi/.cache/clawdbot/voice.fifo`) open and pipes it into `clawgo run -stdin`.
3. Create `/etc/systemd/system/clawgo.service` pointing to that wrapper.

## mDNS advertising

The node advertises `_clawdbot-node._tcp` by default.

```bash
dns-sd -B _clawdbot-node._tcp local.
```

Override to `_clawdbot-bridge._tcp` if you intentionally want it to show up as a gateway beacon:

```bash
./clawgo run -mdns-service _clawdbot-bridge._tcp
```

## Notes
- Node state (`nodeId` + token) lives in `~/.clawdbot/clawgo.json`.
- Caps default to `voiceWake`; override via `-caps` if you expose more commands.
- Set `bridge.bind: "tailnet"` on the gateway to restrict the bridge to Tailscale.
- Brabble output is drained before its transcript stream closes; stderr diagnostics cannot block recognition when logging is disabled or a diagnostic line is oversized. Nonzero process exits are logged.
