# syntax=docker/dockerfile:1.7

# Build stage
FROM golang:1.25-alpine AS builder
WORKDIR /app

RUN apk add --no-cache curl
RUN go install github.com/tdewolff/minify/v2/cmd/minify@v2.24.13

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN minify -o ./base_photo_min.css ./base_photo.css && \
    minify -o ./base_min.css ./base.css && \
    curl -sL -o jquery.unveil.js https://raw.githubusercontent.com/luis-almeida/unveil/master/jquery.unveil.js && \
    minify -o ./jquery.unveil.min.js ./jquery.unveil.js

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o toomorephotos .

# Runtime stage
FROM alpine:3.23
WORKDIR /app

RUN apk add --no-cache ca-certificates wget && \
    adduser -D -u 10001 app

COPY --from=builder --chown=app:app /app/toomorephotos \
                                    /app/base.htm \
                                    /app/base_2019.html \
                                    /app/index.htm \
                                    /app/photo.htm \
                                    /app/sitemap.htm \
                                    /app/base_min.css \
                                    /app/base_photo_min.css \
                                    /app/jquery.unveil.min.js \
                                    /app/favicon.ico \
                                    ./

RUN echo -e "User-agent: *\nAllow: /" > robots.txt && \
    echo "photo" > tags.txt && \
    chown app:app robots.txt tags.txt
# tags.txt: provide via volume (./tags.txt) or it will use default

USER app
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
    CMD wget -qO- http://localhost:8080/health || exit 1

CMD ["./toomorephotos", "-p", ":8080"]
