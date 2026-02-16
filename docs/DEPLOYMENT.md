# Deployment Guide

This guide shows how to deploy PicoClaw with real credentials while keeping the repo clean and generic.

## Repository vs Deployment

**Repository (committed to git):**
- `config/config.json` → Generic placeholders, no secrets
- `docker-compose.yml` → Generic setup, uses `PICOCLAW_CONFIG` env var
- `docker-compose.override.yml.example` → Example deployment config

**Deployment (NOT committed to git):**
- `~/.picoclaw/config.json` → Real credentials (Matrix tokens, API keys, etc.)
- `docker-compose.override.yml` → Your specific volume mounts and env vars
- `.gitignore` → Blocks override file from being committed

## Quick Start

### 1. Create Your Deployment Config

Copy the example and customize with real credentials:

```bash
# Create config directory outside repo
mkdir -p ~/.picoclaw

# Create your config with real credentials
cat > ~/.picoclaw/config.json <<EOF
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

### 2. Create Docker Compose Override

```bash
cd ~/code/picoclaw

# Copy the example
cp docker-compose.override.yml.example docker-compose.override.yml

# Edit it to point to your config
cat > docker-compose.override.yml <<EOF
services:
  picoclaw-gateway:
    volumes:
      - ~/.picoclaw/config.json:/root/.picoclaw/config.json:ro

  picoclaw-agent:
    volumes:
      - ~/.picoclaw/config.json:/root/.picoclaw/config.json:ro
EOF
```

### 3. Deploy

```bash
# Build the image
docker compose build

# Start the gateway
docker compose --profile gateway up -d

# Check logs
docker compose logs -f picoclaw-gateway
```

## Using Environment Variable

Alternatively, set `PICOCLAW_CONFIG` environment variable:

```bash
export PICOCLAW_CONFIG=~/.picoclaw/config.json
docker compose --profile gateway up -d
```

## Multiple Deployments (Multiple Bots)

Deploy multiple bot instances with different configs:

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
```

```bash
docker compose -f docker-compose.wanda.yml up -d
docker compose -f docker-compose.aria.yml up -d
```

## Security Best Practices

### Config File Protection

```bash
# Restrict permissions (owner read/write only)
chmod 600 ~/.picoclaw/config.json

# Never commit configs with secrets
git status  # Should NOT show your deployment config
```

### Secrets Management

For production, use secrets management:

```yaml
# docker-compose.override.yml
services:
  picoclaw-gateway:
    environment:
      - MATRIX_ACCESS_TOKEN_FILE=/run/secrets/matrix_token
    secrets:
      - matrix_token

secrets:
  matrix_token:
    file: ~/.picoclaw/secrets/matrix_token
```

### Container Security

```yaml
# docker-compose.override.yml
services:
  picoclaw-gateway:
    read_only: true
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL
```

## Kubernetes Deployment

For K8s deployments, use ConfigMaps and Secrets:

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
          "homeserver": "https://matrix.medher.online",
          "user_id": "@wanda:matrix.medher.online",
          "access_token": "",  # Mounted from secret
          "join_on_invite": true
        }
      }
    }
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: picoclaw-wanda
spec:
  template:
    spec:
      containers:
      - name: picoclaw
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

### Config not loading

```bash
# Check if override is applied
docker compose config | grep config.json

# Verify mount
docker compose --profile gateway run --rm picoclaw-gateway cat /root/.picoclaw/config.json
```

### Secrets visible in logs

```bash
# Check for accidentally logged secrets
docker compose logs picoclaw-gateway | grep -i "token\|password\|key"
```

### Permission denied

```bash
# Fix permissions on mounted config
chmod 644 ~/.picoclaw/config.json  # Docker needs read access
```

---

**Remember:** The repo stays generic. Your deployment config lives outside git.
