# syntax=docker/dockerfile:1

# Build stage. SQLite is the pure-Go modernc driver, so CGO stays off and the
# binary needs no C library at run time.
FROM golang:1.26-alpine AS build
WORKDIR /src

# Modules first, so a source change does not download every dependency again.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# The *_templ.go files are committed, so the build needs no templ step.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tasker ./cmd/tasker \
 && mkdir -p /out/data

# Run stage. The binary embeds its stylesheet, migrations and time-zone data, so
# it is the only file the image needs, and it runs as a non-root user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tasker /tasker
# /data is created owned by the non-root user so that a named volume mounted over
# it starts out writable: Docker copies an image directory's ownership into an
# empty named volume the first time it is mounted.
COPY --from=build --chown=65532:65532 /out/data /data
ENV TASKER_ADDR=:1222 \
    TASKER_DB=/data/tasker.db
EXPOSE 1222
VOLUME ["/data"]
USER nonroot:nonroot
ENTRYPOINT ["/tasker"]
