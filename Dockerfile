FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/feedsync ./cmd/feedsync

FROM alpine:3.21
RUN adduser -D -u 10001 feedsync
COPY --from=build /out/feedsync /usr/local/bin/feedsync
USER feedsync
WORKDIR /work
ENTRYPOINT ["/usr/local/bin/feedsync"]
