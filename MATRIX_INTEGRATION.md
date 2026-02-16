# Matrix Integration for PicoClaw

## ✅ Implementation Complete

Matrix protocol support has been added to PicoClaw using the `mautrix-go` SDK.

### Features Implemented

- ✅ **Text Messaging**: Send and receive text messages
- ✅ **HTML Formatting**: Automatic markdown-to-HTML conversion
- ✅ **Room Invites**: Auto-join rooms on invite (configurable)
- ✅ **Message Chunking**: Split long messages (60KB chunks)
- ✅ **Allowlist**: Filter authorized users
- ✅ **Session Management**: Proper sync lifecycle handling
- ✅ **Event Metadata**: Track event IDs in message metadata

### Files Changed

- `pkg/config/config.go` - Added `MatrixConfig` struct
- `pkg/channels/matrix.go` - Full Matrix channel implementation (384 lines)
- `pkg/channels/manager.go` - Registered Matrix in channel manager
- `config/config.example.json` - Added Matrix configuration example
- `go.mod` / `go.sum` - Added `maunium.net/go/mautrix v0.26.3`

### Architecture

```
MatrixChannel
├── *BaseChannel (allowlist, bus integration)
├── *mautrix.Client (SDK client)
├── Event Handlers
│   ├── handleMessage() - Process incoming messages
│   └── handleMemberEvent() - Handle room invites
├── Send() - Outbound message handler
└── Start/Stop() - Lifecycle management with sync
```

### Configuration

Add to your `config.json`:

```json
{
  "channels": {
    "matrix": {
      "enabled": true,
      "homeserver": "https://matrix.medher.online",
      "user_id": "@myka:matrix.medher.online",
      "access_token": "YOUR_TOKEN_HERE",
      "device_id": "PICOCLAW",
      "allow_from": ["@lophie:matrix.medher.online"],
      "join_on_invite": true
    }
  }
}
```

### Getting Your Matrix Credentials

#### Option 1: Via Element/Cinny Web Client

1. **Login to Element/Cinny**
2. **Open Settings → Help & About**
3. **Scroll down to "Advanced"**
4. **Click "Access Token"** (you'll see a long string)
5. **Copy the token** - This is your `access_token`

Your `user_id` format: `@username:homeserver.domain`

#### Option 2: Via matrix-commander (Recommended for bots)

```bash
# Install matrix-commander
pip install matrix-commander

# First login (interactive)
matrix-commander --login password --homeserver https://matrix.medher.online

# Credentials are stored in ~/.config/matrix-commander/credentials.json
cat ~/.config/matrix-commander/credentials.json
```

#### Option 3: Via curl (manual)

```bash
# Login
curl -X POST https://matrix.medher.online/_matrix/client/r0/login \
  -H "Content-Type: application/json" \
  -d '{
    "type": "m.login.password",
    "user": "myka",
    "password": "YOUR_PASSWORD"
  }'

# Response contains access_token and device_id
```

### Testing

#### 1. Configure Matrix

Edit `~/code/picoclaw/config/config.json`:

```json
{
  "channels": {
    "matrix": {
      "enabled": true,
      "homeserver": "https://matrix.medher.online",
      "user_id": "@myka:matrix.medher.online",
      "access_token": "...",
      "device_id": "PICOCLAW",
      "allow_from": ["@lophie:matrix.medher.online"],
      "join_on_invite": true
    }
  }
}
```

#### 2. Rebuild Docker Image

```bash
cd ~/code/picoclaw
docker compose --profile gateway build
```

#### 3. Start Gateway

```bash
docker compose --profile gateway up
```

#### 4. Test from Matrix

1. **From your Matrix client** (Element/Cinny), send a DM to `@myka:matrix.medher.online`
2. **Or invite the bot to a room**
3. **Send a message**: "Hello, Myka!"
4. **Bot should respond** via your configured LLM

#### 5. Check Logs

```bash
docker compose logs -f picoclaw-gateway | grep matrix
```

Expected output:
```
[INFO] channels: Matrix channel enabled successfully
[INFO] matrix: Matrix client connected {"user_id":"@myka:matrix.medher.online","homeserver":"https://matrix.medher.online"}
[DEBUG] matrix: Received message {"sender":"@lophie:matrix.medher.online","room":"!abc123:matrix.medher.online","text":"Hello, Myka!"}
```

### Troubleshooting

#### Bot doesn't respond
- Check `allow_from` includes your Matrix user ID
- Verify `access_token` is valid (try logging in with it)
- Check logs for authorization errors

#### Can't join rooms
- Set `join_on_invite: true`
- Ensure inviter is in `allow_from` list
- Check room permissions

#### Messages not sending
- Verify room ID format: `!roomid:homeserver.domain`
- Check bot has permission to send in room
- Look for rate limiting errors in logs

### Next Steps

Potential enhancements:
- [ ] E2E encryption support (mautrix-go supports it!)
- [ ] Reactions to messages
- [ ] File/image attachments
- [ ] Read receipts
- [ ] Typing indicators
- [ ] Better markdown/HTML conversion (use a library)
- [ ] Room management commands (/list, /leave, etc.)

### Dependencies Added

- `maunium.net/go/mautrix v0.26.3` - Matrix Go SDK
- `go.mau.fi/util v0.9.6` - Utilities
- `github.com/rs/zerolog v1.34.0` - Logging (mautrix dependency)
- `github.com/tidwall/gjson v1.18.0` - JSON parsing
- `filippo.io/edwards25519 v1.1.0` - Cryptography

Total size impact: ~2-3MB added to Docker image

### Comparison to Other Channels

| Feature | Telegram | Discord | Matrix |
|---------|----------|---------|--------|
| SDK | telego | discordgo | mautrix-go |
| Auth | Bot Token | Bot Token | Access Token |
| User ID | Numeric | Snowflake | @user:server |
| Room ID | Numeric | Snowflake | !room:server |
| Formatting | Markdown | Markdown | HTML |
| E2E Crypto | ❌ | ❌ | ✅ (future) |

### License

Matrix integration code follows PicoClaw's MIT license.
mautrix-go is Apache 2.0 licensed.
