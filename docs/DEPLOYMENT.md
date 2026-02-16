# Deployment Guide

This guide shows how to deploy PicoClaw with real credentials while keeping the repo clean and generic.

## Simple Deployment Pattern

**Repository (committed to git):**
- `config/config.example.json` → Generic placeholder config for reference
- `docker-compose.yml` → Mounts `~/.picoclaw/config.json` (outside repo)

**Your Deployment (NOT in git):**
- `~/.picoclaw/config.json` → Your real config with credentials

The docker-compose.yml **always mounts from outside the repo**. Just create your config at `~/.picoclaw/config.json` and you're good to go.

## Quick Start

### 1. Create Your Config

```bash
# Create config directory
mkdir -p ~/.picoclaw

# Copy example and edit with your credentials
cat > ~/.picoclaw/config.json <<'EOF'
{
  "agents": {
    "defaults": {
      "workspace": "/root/.picoclaw/workspace",
      "restrict_to_workspace": true,
      "provider": "codex-cli",
      "model": "gpt-5.2",
      "max_tokens": 8192,
      "temperature": 0.7,
      "max_tool_iterations": 20
    }
  },
  "channels": {
    "matrix": {
      "enabled": true,
      "homeserver": "https://matrix.medher.online",
      "user_id": "@wanda:matrix.medher.online",
      "access_token": "syt_YOUR_REAL_TOKEN_HERE",
      "device_id": "",
      "allow_from": [],
      "join_on_invite": true
    }
  },
  "providers": {
    "openai": {
      "api_key": "sk-YOUR_KEY_HERE",
      "api_base": ""
    }
  },
  "tools": {
    "web": {
      "duckduckgo": {
        "enabled": true,
        "max_results": 5
      }
    }
  }
}
EOF

# Protect your config
chmod 600 ~/.picoclaw/config.json
```

### 2. Deploy

```bash
cd ~/code/picoclaw

# Build the image
docker compose build

# Start the gateway
docker compose --profile gateway up -d

# Check logs
docker compose logs -f picoclaw-gateway
```

That's it! The compose file automatically mounts `~/.picoclaw/config.json`.

## Multiple Bot Deployments

To run multiple bots (e.g., Wanda, Aria) with different configs:

### Option 1: Multiple Config Files + Container Names

```bash
# Wanda's config
~/.picoclaw/wanda-config.json

# Aria's config
~/.picoclaw/aria-config.json
```

Create separate compose files:

```yaml
# docker-compose.wanda.yml
services:
  wanda:
    extends:
      file: docker-compose.yml
      service: picoclaw-gateway
    container_name: picoclaw-wanda
    volumes:
      - ~/.picoclaw/wanda-config.json:/root/.picoclaw/config.json:ro
      - ~/.codex:/root/.codex:ro
      - picoclaw-wanda-workspace:/root/.picoclaw/workspace

volumes:
  picoclaw-wanda-workspace:
```

Deploy each bot:

```bash
docker compose -f docker-compose.wanda.yml up -d
docker compose -f docker-compose.aria.yml up -d
```

### Option 2: Environment Variable Override

If you need to switch configs temporarily:

```bash
# Override the mount path
docker compose run --rm \
  -v ~/.picoclaw/aria-config.json:/root/.picoclaw/config.json:ro \
  picoclaw-agent -m "Hello"
```

## Security Best Practices

### Protect Your Config

```bash
# Only you can read/write
chmod 600 ~/.picoclaw/config.json

# Verify it's not in the repo
cd ~/code/picoclaw
git status  # Should NOT show ~/.picoclaw/config.json
```

### Never Commit Secrets

The `.gitignore` already blocks common config locations, but **never** add configs with secrets to the repo:

```bash
# ❌ WRONG - DO NOT DO THIS
git add ~/.picoclaw/config.json

# ✅ RIGHT - Config stays outside repo
ls ~/.picoclaw/config.json  # Exists outside git
```

### Container Security Hardening

```yaml
# docker-compose.override.yml (optional hardening)
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

For K8s, use Secrets and ConfigMaps:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: wanda-matrix-token
  namespace: agents
type: Opaque
stringData:
  access_token: syt_d2FuZGE_vjlXtHrcGFLicqafLMdF_1Mfb0K
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: wanda-config
  namespace: agents
data:
  config.json: |
    {
      "channels": {
        "matrix": {
          "enabled": true,
          "homeserver": "https://matrix.medher.online",
          "user_id": "@wanda:matrix.medher.online",
          "join_on_invite": true
        }
      }
    }
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: picoclaw-wanda
  namespace: agents
spec:
  replicas: 1
  selector:
    matchLabels:
      app: picoclaw-wanda
  template:
    metadata:
      labels:
        app: picoclaw-wanda
    spec:
      containers:
      - name: picoclaw
        image: your-registry/picoclaw:latest
        command: ["picoclaw", "gateway"]
        volumeMounts:
        - name: config
          mountPath: /root/.picoclaw/config.json
          subPath: config.json
          readOnly: true
        - name: workspace
          mountPath: /root/.picoclaw/workspace
        env:
        - name: MATRIX_ACCESS_TOKEN
          valueFrom:
            secretKeyRef:
              name: wanda-matrix-token
              key: access_token
      volumes:
      - name: config
        configMap:
          name: wanda-config
      - name: workspace
        persistentVolumeClaim:
          claimName: picoclaw-wanda-workspace
```

Then inject the secret at runtime or use init containers to merge config + secret.

## Troubleshooting

### Config file not found

```bash
# Check if config exists
ls -la ~/.picoclaw/config.json

# If missing, copy from example
cp ~/code/picoclaw/config/config.example.json ~/.picoclaw/config.json
# Then edit with your credentials
```

### Permission denied

```bash
# Docker needs read access
chmod 644 ~/.picoclaw/config.json

# Or more restrictive (owner only)
chmod 600 ~/.picoclaw/config.json
```

### Verify config is mounted

```bash
# Check what config Docker sees
docker compose --profile gateway run --rm picoclaw-gateway cat /root/.picoclaw/config.json
```

### Secrets visible in logs

```bash
# Never log full config!
# If you see secrets in logs, file a bug report
docker compose logs picoclaw-gateway | grep -i "token\|password\|secret"
```

---

**Remember:** 
- Repo = generic code + example config
- Deployment = your config at `~/.picoclaw/config.json`
- docker-compose.yml automatically mounts it
