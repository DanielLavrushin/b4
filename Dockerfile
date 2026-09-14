# Stage 1: Generate UI defaults from Go config
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS defaults-gen

WORKDIR /app
COPY src/go.mod src/go.sum ./src/
RUN cd src && go mod download
COPY src/ ./src/
RUN cd src && go run tools/gendefaults.go

# Stage 2: Build the web UI
FROM --platform=$BUILDPLATFORM node:22-alpine AS ui-builder

RUN corepack enable && corepack prepare pnpm@10.18.2 --activate

WORKDIR /app
COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY src/http/ui/package.json ./src/http/ui/
COPY hub/ui/package.json ./hub/ui/
COPY shared/design/package.json ./shared/design/
RUN pnpm install --frozen-lockfile --filter b4-ui...

COPY shared/design/ ./shared/design/
COPY src/http/ui/ ./src/http/ui/
COPY --from=defaults-gen /app/src/http/ui/src/models/defaults.json ./src/http/ui/src/models/defaults.json
ARG VERSION=dev
ENV VITE_APP_VERSION=${VERSION}
RUN pnpm --filter b4-ui build

# Stage 3: Build the Go binary
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS go-builder

WORKDIR /app

COPY src/go.mod src/go.sum ./src/
RUN cd src && go mod download

COPY src/ ./src/
COPY --from=ui-builder /app/src/http/ui/dist ./src/http/ui/dist
COPY Makefile ./

ARG VERSION=dev
ARG TARGETARCH
ARG TARGETVARIANT

RUN COMMIT=$(echo "docker" ) && \
    DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) && \
    GOARM=${TARGETVARIANT#v} \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go -C src build \
    -trimpath \
    -ldflags "-s -w -X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.Date=${DATE}" \
    -o /b4

# Stage 4: Runtime image
FROM alpine:3.23.3

RUN apk add --no-cache \
    iptables \
    ip6tables \
    nftables \
    kmod \
    iproute2 \
    tzdata

COPY --from=go-builder /b4 /usr/local/bin/b4

VOLUME /etc/b4
EXPOSE 7000

ENTRYPOINT ["b4"]
