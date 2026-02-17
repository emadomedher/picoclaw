# ============================================================
# Stage 1: Build the picoclaw binary
# ============================================================
FROM golang:1.26.0-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN make build

# ============================================================
# Stage 2: Minimal runtime image
# ============================================================
FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata curl

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q --spider http://localhost:18790/health || exit 1

# Copy binary
COPY --from=builder /src/build/picoclaw /usr/local/bin/picoclaw

# Create picoclaw home directory and install codex
RUN /usr/local/bin/picoclaw onboard

# Fix: Go's exec.LookPath fails to execute the .js symlink that npm creates
# for the codex binary on Alpine (musl). Replace it with a proper shell wrapper
# so exec.Command("codex") works correctly in all providers.
RUN rm -f /usr/local/bin/codex && \
    printf '#!/bin/sh\nexec node /usr/local/lib/node_modules/@openai/codex/bin/codex.js "$@"\n' \
    > /usr/local/bin/codex && \
    chmod +x /usr/local/bin/codex

ENTRYPOINT ["picoclaw"]
CMD ["gateway"]
