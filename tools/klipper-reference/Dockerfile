FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends gcc-avr avr-libc binutils-avr gcc-arm-none-eabi libnewlib-arm-none-eabi make gcc libc6-dev \
    python3 python3-cffi python3-greenlet python3-jinja2 python3-serial python3-msgspec && rm -rf /var/lib/apt/lists/*
