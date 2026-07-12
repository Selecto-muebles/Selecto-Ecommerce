FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 go test ./... && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o /out/selecto-ecommerce ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/selecto-ecommerce /app/selecto-ecommerce
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/selecto-ecommerce"]
