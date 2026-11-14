# Build a static binary, then ship it alone. Templates and static files are embedded in it.
FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /arena-server .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /arena-server /arena-server
# Accounts live in /data: mount a volume there, or they are lost when the container is replaced.
ENV ARENA_DATA=/data/arena-data.json ARENA_ADDR=:8080
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/arena-server"]
