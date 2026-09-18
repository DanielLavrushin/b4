FROM --platform=$BUILDPLATFORM node:22-alpine AS ui-builder

RUN corepack enable && corepack prepare pnpm@10.18.2 --activate

WORKDIR /app
COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY src/http/ui/package.json ./src/http/ui/
COPY hub/ui/package.json ./hub/ui/
COPY shared/design/package.json ./shared/design/
RUN pnpm install --frozen-lockfile --filter b4hub-ui...

COPY shared/design/ ./shared/design/
COPY hub/ui/ ./hub/ui/
ARG VERSION=dev
ENV VITE_APP_VERSION=${VERSION}
RUN pnpm --filter b4hub-ui build

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS go-builder

WORKDIR /app
COPY src/go.mod src/go.sum ./src/
COPY hub/go.mod hub/go.sum ./hub/
RUN cd hub && go mod download

COPY src/ ./src/
COPY hub/ ./hub/
COPY --from=ui-builder /app/hub/ui/dist ./hub/ui/dist

ARG VERSION=dev
ARG B4_SOURCE=unknown
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go -C hub build \
    -trimpath \
    -ldflags "-s -w -X main.Version=${VERSION} -X main.Source=${B4_SOURCE}" \
    -o /b4hub ./cmd/b4hub

FROM alpine:3.23.3

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 7100 b4hub \
    && adduser -S -D -H -u 7100 -G b4hub -h /var/lib/b4hub b4hub \
    && install -d -o b4hub -g b4hub -m 0750 /var/lib/b4hub

COPY --from=go-builder /b4hub /usr/local/bin/b4hub

ENV B4HUB_DATA=/var/lib/b4hub \
    B4HUB_LISTEN=0.0.0.0:7100

USER b4hub
WORKDIR /var/lib/b4hub
VOLUME /var/lib/b4hub
EXPOSE 7100

ENTRYPOINT ["b4hub"]
CMD ["serve"]
