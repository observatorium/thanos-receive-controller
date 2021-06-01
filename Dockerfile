FROM golang:1.16-alpine3.12 as builder

WORKDIR /workspace

COPY . .

RUN apk update && apk upgrade && apk add --no-cache alpine-sdk

RUN  make thanos-receive-controller

FROM gcr.io/distroless/static:latest

COPY --from=builder /workspace/thanos-receive-controller /usr/bin/thanos-receive-controller

USER 65534

ENTRYPOINT ["/usr/bin/thanos-receive-controller"]
