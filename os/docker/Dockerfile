# The build environment for Buildroot. os/build.sh runs every build step in this container.
FROM debian:trixie-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      bc bison build-essential bzip2 ca-certificates cpio file flex git gzip libncurses-dev \
      patch perl python3 rsync unzip wget xz-utils \
 && rm -rf /var/lib/apt/lists/*
ENV LANG=C.UTF-8
