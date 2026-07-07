FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN go build -o /out/webhook-api .

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/webhook-api ./webhook-api
EXPOSE 8080
ENTRYPOINT ["./webhook-api"]
