# Build the desktop service from the repository root:
#   docker build -f desktopd/Dockerfile -t maclaw-desktopd .
# Run it on the machine that has the Docker engine:
#   docker run --rm -p 18081:18081 \
#     -e DESKTOPD_TOKEN -e DESKTOPD_ADVERTISE_HOST \
#     -v /var/run/docker.sock:/var/run/docker.sock maclaw-desktopd
FROM golang:1.25 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/desktopd ./desktopd/cmd/desktopd

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates docker.io \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/desktopd /usr/local/bin/desktopd
EXPOSE 18081
ENTRYPOINT ["desktopd"]
