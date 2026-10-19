FROM golang:1.26-bookworm AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# VERSION is stamped into the binary and reported on every log line, so a
# running container can be tied back to a build.
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/garages-gateway ./cmd

# Verify the tests pass against the exact tree being shipped. Kept as a
# separate stage so `docker build --target test` runs them and a normal build
# does not pay for them.
FROM builder AS test
RUN CGO_ENABLED=1 go test -race ./...

# :nonroot runs as uid/gid 65532 instead of root. A gateway that terminates
# external traffic should never hold root in its container.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/garages-gateway /usr/local/bin/garages-gateway

ENV GRPC_LISTEN_ADDR=:50057
ENV GATEWAY_LISTEN_ADDR=:8083
ENV LOG_LEVEL=info
ENV LOG_FORMAT=json

EXPOSE 50057
EXPOSE 8083

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/garages-gateway"]
