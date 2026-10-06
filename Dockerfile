FROM golang:1.22-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o gubexplorer .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /build/gubexplorer /gubexplorer
EXPOSE 8080
ENTRYPOINT ["/gubexplorer"]
