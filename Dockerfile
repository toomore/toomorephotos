# Build stage
FROM golang:1.21-alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go install github.com/tdewolff/minify/v2/cmd/minify@v2.20.11
RUN minify -o ./base_photo_min.css ./base_photo.css
RUN minify -o ./base_min.css ./base.css
RUN apk add --no-cache curl && \
    curl -sL -o jquery.unveil.js https://raw.githubusercontent.com/luis-almeida/unveil/master/jquery.unveil.js && \
    minify -o ./jquery.unveil.min.js ./jquery.unveil.js

RUN CGO_ENABLED=0 go build -o toomorephotos .

# Runtime stage
FROM alpine:3.19
WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/toomorephotos .
COPY --from=builder /app/base.htm .
COPY --from=builder /app/base_2019.html .
COPY --from=builder /app/index.htm .
COPY --from=builder /app/photo.htm .
COPY --from=builder /app/sitemap.htm .
COPY --from=builder /app/base_min.css .
COPY --from=builder /app/base_photo_min.css .
COPY --from=builder /app/jquery.unveil.min.js .
COPY --from=builder /app/favicon.ico .

RUN echo "User-agent: *" > robots.txt && echo "Allow: /" >> robots.txt
# tags.txt: provide via volume (./tags.txt) or it will use default
RUN echo "photo" > tags.txt

EXPOSE 8080

CMD ["./toomorephotos", "-p", ":8080"]
