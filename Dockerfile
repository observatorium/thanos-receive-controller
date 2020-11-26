FROM gcr.io/distroless/static:latest

COPY thanos-receive-controller /usr/bin/thanos-receive-controller

USER 65534

ENTRYPOINT ["/usr/bin/thanos-receive-controller"]
