FROM alpine

COPY thanos-receive-controller /usr/bin/thanos-receive-controller

ENTRYPOINT ["/usr/bin/thanos-receive-controller"]
