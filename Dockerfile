#Build Stage
FROM golang:1.22.1-alpine as builder
RUN mkdir /app
WORKDIR /app
RUN mkdir /app/bin

ENV GOPATH=/app
ENV GOBIN=/app/bin

RUN apk update && \
    apk add --no-cache git



COPY go.mod ./
COPY go.sum ./

RUN go mod download

COPY . ./

RUN CGO_ENABLED=0 GOOS=linux go build -v -o binaryFile .

#Production Stage
FROM alpine:latest as production

RUN mkdir /prod
WORKDIR /prod
RUN mkdir /prod/bin

COPY --from=builder /app/binaryFile /prod/
COPY --from=builder /app/bin /bin

RUN chmod +x /prod/binaryFile

CMD ["/prod/binaryFile"]