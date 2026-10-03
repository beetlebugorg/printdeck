## Builds go-librespot from source. Pinned to a tag for reproducible builds;
## bump GO_LIBRESPOT_REF deliberately, don't float on a branch.
FROM alpine:3.23 AS build

RUN apk update && apk -U --no-cache add git go alsa-lib-dev libogg-dev libvorbis-dev flac-dev mpg123-dev gcc musl-dev

ARG GO_LIBRESPOT_REF=master
RUN git clone --depth 1 --branch ${GO_LIBRESPOT_REF} https://github.com/devgianlu/go-librespot.git /src
WORKDIR /src
RUN go mod download
RUN CGO_ENABLED=1 go build -v -o ./go-librespot ./cmd/daemon

FROM alpine:3.23

RUN apk update && apk -U --no-cache add libpulse avahi libgcc gcompat alsa-lib

COPY --from=build /src/go-librespot /usr/bin/go-librespot
COPY config.yml /config/config.yml

CMD ["/usr/bin/go-librespot", "--config_dir", "/config"]
