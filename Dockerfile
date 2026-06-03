FROM golang:1.25 AS build

ENV GOPROXY=https://goproxy.cn,direct

WORKDIR /src
COPY go.mod go.sum ./
COPY datasrv/go.mod ./datasrv/go.mod
COPY gui/internal/systray/go.mod ./gui/internal/systray/go.mod
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o /out/maclawsrv ./MaClawSrv

FROM golang:1.25

COPY --from=build /out/maclawsrv /usr/local/bin/maclawsrv

EXPOSE 18080
ENTRYPOINT ["maclawsrv"]
