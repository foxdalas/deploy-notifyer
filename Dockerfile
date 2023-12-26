FROM golang:1.21.1-alpine as build

RUN apk add git
RUN apk add alpine-sdk

WORKDIR /app
COPY go.mod go.sum /app/
RUN go mod download
COPY . .
RUN go build .

FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
COPY --from=build /app/deploy-notifyer /bin/
ENTRYPOINT ["/bin/deploy-notifyer"]
