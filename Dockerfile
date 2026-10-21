FROM golang:1.26.10-alpine@sha256:9ce231b28998b32e116927c61aa5c406183061d4f3f2d61b292c88e7f6bdf7ca AS build
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

FROM gcr.io/distroless/static-debian12:nonroot@sha256:b7bb25d9f7c31d2bdd1982feb4dafcaf137703c7075dbe2febb41c24212b946f
COPY --from=build /out/selecto-ecommerce /app/selecto-ecommerce
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/selecto-ecommerce"]
