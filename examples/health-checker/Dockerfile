FROM golang:1.22-alpine AS build
WORKDIR /app
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 go build -o /agent .

FROM alpine:3.19
COPY --from=build /agent /agent
COPY hap.yaml /hap.yaml
ENTRYPOINT ["/agent"]
