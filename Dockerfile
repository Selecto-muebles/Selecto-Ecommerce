FROM golang:1.26.11-alpine@sha256:ae52b5bc6ed045a9ed26ab03dc19327a802a9cc9aa8b27ed57f5c6b60579b0bb AS build
WORKDIR /src
COPY go.mod go.sum ./
ARG GOPROXY=https://proxy.golang.org,direct
ARG GOSUMDB=sum.golang.org
RUN GOPROXY="$GOPROXY" GOSUMDB="$GOSUMDB" go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 go test ./... && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o /out/selecto-ecommerce ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot@sha256:aef9602f8710ec12bde19d593fed1f76c708531bb7aba205110f1029786ead7b
COPY --from=build /out/selecto-ecommerce /app/selecto-ecommerce
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/selecto-ecommerce"]
