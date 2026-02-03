FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY unscrambler /usr/local/bin/unscrambler

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/unscrambler"]
