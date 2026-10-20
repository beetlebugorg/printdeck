## CamillaDSP from its release. Bump CAMILLADSP_VERSION deliberately.
FROM debian:bookworm-slim
ARG CAMILLADSP_VERSION=v4.1.3
RUN apt-get update \
 && apt-get install -y --no-install-recommends libasound2 ca-certificates curl \
 && curl -fsSL https://github.com/HEnquist/camilladsp/releases/download/${CAMILLADSP_VERSION}/camilladsp-linux-aarch64.tar.gz \
    | tar xz -C /usr/local/bin camilladsp \
 && apt-get purge -y curl && apt-get autoremove -y && rm -rf /var/lib/apt/lists/*
# The websocket is only for the printdeck app on this host.
CMD ["camilladsp", "-p", "1234", "-a", "127.0.0.1", "/config/config.yml"]
