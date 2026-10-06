FROM golang:1.27-alpine AS builder
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w -X main.version=${VERSION}" -o gubexplorer .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /build/gubexplorer /gubexplorer
EXPOSE 8080
ENTRYPOINT ["/gubexplorer"]
