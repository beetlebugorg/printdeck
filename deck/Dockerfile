FROM golang:1.24-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /printdeck ./cmd/printdeck

FROM debian:bookworm-slim
# alsa-utils: amixer sets the volume of the Amp2.
RUN apt-get update \
 && apt-get install -y --no-install-recommends alsa-utils ca-certificates tzdata \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /printdeck /usr/local/bin/printdeck
ENV STATE_DIRECTORY=/var/lib/printdeck
ENTRYPOINT ["/usr/local/bin/printdeck"]
