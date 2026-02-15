# arp-sidecar

WebSocket client that connects to the [Agent Relay Protocol](https://github.com/benatsnowyroad/agent-relay-protocol) relay and forwards events to OpenClaw hooks.

## Install

```bash
go install github.com/benatsnowyroad/arp-sidecar/cmd/arp-sidecar@latest
```

## Configuration

Create `arp-sidecar.yaml`:

```yaml
agentId: computer_bot
token: your-agent-token
relayUrl: wss://agentrelayprotocol-production.up.railway.app
hooksUrl: http://127.0.0.1:18789/hooks/agent
hooksToken: your-hooks-token
```

All fields can be overridden with environment variables:

| YAML Key | Env Var |
|----------|---------|
| `agentId` | `ARP_AGENT_ID` |
| `token` | `ARP_TOKEN` |
| `relayUrl` | `ARP_RELAY_URL` |
| `hooksUrl` | `ARP_HOOKS_URL` |
| `hooksToken` | `ARP_HOOKS_TOKEN` |

## Usage

```bash
# With config file
arp-sidecar -c arp-sidecar.yaml

# With env vars
export ARP_AGENT_ID=computer_bot
export ARP_TOKEN=xxx
export ARP_RELAY_URL=wss://agentrelayprotocol-production.up.railway.app
export ARP_HOOKS_URL=http://127.0.0.1:18789/hooks/agent
export ARP_HOOKS_TOKEN=xxx
arp-sidecar

# Debug logging
arp-sidecar --log-level debug
```

## How It Works

1. Connects to the ARP relay via WebSocket at `/ws/agent/{agentId}?token={token}`
2. Responds to heartbeats with acks to stay connected
3. Tracks `lastSeenMessageId` per channel
4. On actionable events (`turn_notification`, `synthesis_request`, `mention_notification`), POSTs to the OpenClaw hooks endpoint
5. Uses `sessionKey = arp:channel:{channelId}` for consistent hook context
6. On reconnect, fetches missed messages and replays any actionable ones
7. Exponential backoff with jitter on connection failures

## Message Types

| Type | Action |
|------|--------|
| `hello` | Log + trigger catchup |
| `heartbeat` | Send `heartbeat_ack` |
| `channel_message` | Track (buffer) |
| `turn_notification` | Forward to hooks |
| `synthesis_request` | Forward to hooks |
| `mention_notification` | Forward to hooks |
