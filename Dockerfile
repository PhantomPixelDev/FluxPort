FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/fluxport ./cmd/fluxport

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends libstdc++6 ca-certificates && groupadd --gid 10001 fluxport && useradd --uid 10001 --gid fluxport --no-create-home --shell /usr/sbin/nologin fluxport && mkdir -p /data /state && chown fluxport:fluxport /data /state && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/fluxport /usr/local/bin/fluxport
USER fluxport
ENV FLUXPORT_ADDRESS=0.0.0.0:8080 FLUXPORT_DATA_DIR=/data FLUXPORT_STATE_DIR=/state
VOLUME ["/data", "/state"]
EXPOSE 8080
ENTRYPOINT ["fluxport"]
CMD ["serve"]
