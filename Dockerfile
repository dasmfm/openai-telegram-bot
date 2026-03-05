FROM golang:1.26.0-alpine AS build
WORKDIR /app
COPY go.mod ./
COPY . ./
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/bot ./cmd/bot

FROM alpine:3.23
RUN adduser -D -g '' bot
USER bot
COPY --from=build /bin/bot /bin/bot
ENTRYPOINT ["/bin/bot"]
