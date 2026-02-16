# Deployment Guide

This guide shows how to deploy PicoClaw with your own configuration.

## Configuration Options

PicoClaw uses the `PICOCLAW_CONFIG` environment variable to locate your config file. If not set, it defaults to `./config/config.example.json` (generic placeholders).

### Option 1: Environment Variable (Recommended)

```bash
# Set config path
export PICOCLAW_CONFIG=~/.picoclaw/config.json

# Deploy
docker compose --profile gateway up -d
```

### Option 2: Inline Environment Variable

```bash
# One-liner (no need to export)
PICOCLAW_CONFIG=~/.picoclaw/config.json docker compose --profile gateway up -d
```

### Option 3: Default Config (Testing Only)

```bash
# Uses ./config/config.example.json (placeholders, won't actually work)
docker compose --profile gateway up -d
```

## Quick Start

### 1. Create Your Config

```bash
# Create config directory
mkdir -p ~/.picoclaw

# Copy example and edit with your credentials
cp ~/code/picoclaw/config/config.example.json ~/.picoclaw/config.json

# Edit with your real credentials
nano ~/.picoclaw/config.json
```

Example config with Matrix:

```json
{
  "agents": {
    "defaults": {
      "workspace": "/root/.picoclaw/workspace",
      "provider": "openai",
      "model": "gpt-4",
      "max_tokens": 8192
    }
  },
  "channels": {
    "matrix": {
      "enabled": true,
      "homeserver": "https://matrix.org",
      "user_id": "@mybot:matrix.org",
      "access_token": "syt_YOUR_REAL_TOKEN_HERE",
      "join_on_invite": true
    }
  }
}
```

### 2. Deploy

```bash
cd ~/code/picoclaw

# Build the image
docker compose build

# Deploy with your config
PICOCLAW_CONFIG=~/.picoclaw/config.json docker compose --profile gateway up -d

# Check logs
docker compose logs -f picoclaw-gateway
```

## Multiple Bot Deployments

Deploy multiple bots with different configs using environment variables:

```bash
# Bot 1
PICOCLAW_CONFIG=~/.picoclaw/bot1-config.json \
  docker compose -p bot1 --profile gateway up -d

# Bot 2
PICOCLAW_CONFIG=~/.picoclaw/bot2-config.json \
  docker compose -p bot2 --profile gateway up -d

# Check both
docker ps | grep picoclaw
```

Or create a wrapper script:

```bash
#!/bin/bash
# deploy-bot.sh

BOT_NAME=$1
CONFIG_PATH=~/.picoclaw/${BOT_NAME}-config.json

if [ ! -f "$CONFIG_PATH" ]; then
  echo "Config not found: $CONFIG_PATH"
  exit 1
fi

PICOCLAW_CONFIG=$CONFIG_PATH \
  docker compose -p "picoclaw-$BOT_NAME" --profile gateway up -d

echo "✓ Deployed $BOT_NAME"
```

Usage:
```bash
./deploy-bot.sh bot1
./deploy-bot.sh bot2
```

## Using .env File

Create a `.env` file in the repo root (gitignored):

```bash
# .env
PICOCLAW_CONFIG=/home/user/.picoclaw/mybot-config.json
```

Then just run:
```bash
docker compose --profile gateway up -d
```

Docker Compose automatically loads `.env` files.

## Verification

### Check which config is being used

```bash
# Show resolved docker-compose config
docker compose config | grep -A 2 "PICOCLAW_CONFIG"
```

### Verify config inside container

```bash
# Check what config the container sees
docker compose --profile gateway run --rm picoclaw-gateway cat /root/.picoclaw/config.json | head -20
```

### Test config without deploying

```bash
# Dry run to see if config mounts correctly
docker compose --profile gateway config
```

## Security Best Practices

### Protect Your Config

```bash
# Restrict permissions
chmod 600 ~/.picoclaw/config.json

# Verify it's not in the repo
cd ~/code/picoclaw
git status  # Should NOT show your config
```

### Never Commit Secrets

- ✅ Use `~/.picoclaw/` for configs (outside repo)
- ✅ Use `.env` file (gitignored)
- ✅ Keep example configs generic
- ❌ Never commit real tokens/API keys

### Container Hardening

Add to `docker-compose.override.yml` (optional):

```yaml
services:
  picoclaw-gateway:
    read_only: true
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL
    tmpfs:
      - /tmp:noexec,nosuid,size=100M
```

## Kubernetes Deployment

For K8s, use ConfigMaps and Secrets:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: picoclaw-matrix-token
type: Opaque
stringData:
  access_token: syt_YOUR_TOKEN_HERE
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: picoclaw-config
data:
  config.json: |
    {
      "channels": {
        "matrix": {
          "enabled": true,
          "homeserver": "https://matrix.org",
          "user_id": "@mybot:matrix.org"
        }
      }
    }
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: picoclaw-bot
spec:
  replicas: 1
  selector:
    matchLabels:
      app: picoclaw-bot
  template:
    spec:
      containers:
      - name: picoclaw
        image: your-registry/picoclaw:latest
        command: ["picoclaw", "gateway"]
        volumeMounts:
        - name: config
          mountPath: /root/.picoclaw/config.json
          subPath: config.json
        env:
        - name: MATRIX_ACCESS_TOKEN
          valueFrom:
            secretKeyRef:
              name: picoclaw-matrix-token
              key: access_token
      volumes:
      - name: config
        configMap:
          name: picoclaw-config
```

## Troubleshooting

### Config file not found

```bash
# Check if env var is set
echo $PICOCLAW_CONFIG

# Check if file exists
ls -la $PICOCLAW_CONFIG

# If missing, copy from example
cp config/config.example.json ~/.picoclaw/config.json
```

### Permission denied

```bash
# Docker needs read access
chmod 644 ~/.picoclaw/config.json
```

### Wrong config being used

```bash
# Verify environment variable
docker compose config | grep PICOCLAW_CONFIG

# Force specific config
unset PICOCLAW_CONFIG
PICOCLAW_CONFIG=~/.picoclaw/config.json docker compose --profile gateway up -d
```

### Config changes not applied

```bash
# Restart container to reload config
docker compose restart picoclaw-gateway

# Or recreate
docker compose --profile gateway down
PICOCLAW_CONFIG=~/.picoclaw/config.json docker compose --profile gateway up -d
```

---

**Summary:**
- Repo = generic code + example config
- Your config = anywhere you want (recommended: `~/.picoclaw/`)
- Use `PICOCLAW_CONFIG` env var to point to your config
- Default = `./config/config.example.json` (placeholders only)
