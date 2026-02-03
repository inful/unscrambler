FROM gcr.io/distroless/static:nonroot

WORKDIR /app

COPY unscrambler /app/unscrambler

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/app/unscrambler"]
