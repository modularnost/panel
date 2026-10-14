# The frontend is built once, on the runner's architecture: its output is the
# same for every target.
FROM --platform=$BUILDPLATFORM oven/bun:1.4.3-alpine AS web
WORKDIR /src/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
# The API client is generated from the spec, which lives outside web/.
COPY api/ ../api/
RUN bun run build

# Build on the runner's own architecture and cross-compile, instead of emulating
# the target one: Go does this natively and it keeps multi-arch builds fast.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.* ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -ldflags="-s -w -X main.version=${VERSION}" -o /panel .

FROM alpine:3.22
# The CLI is what implements `docker stack deploy`: compose is a client-side
# format the daemon API knows nothing about.
RUN apk add --no-cache docker-cli
COPY --from=build /panel /panel
ENV PANEL_DB=/data/modularnost.db
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/panel"]
