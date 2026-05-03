FROM golang:1.22-alpine AS backend-builder
WORKDIR /src/backend
RUN apk add --no-cache build-base sqlite-dev
COPY backend/go.mod backend/go.sum* ./
RUN go mod download
COPY backend ./
RUN go build -o /out/smatrix ./cmd/smatrix

FROM alpine:3.20
RUN apk add --no-cache ca-certificates sqlite-libs
WORKDIR /app
COPY --from=backend-builder /out/smatrix /app/smatrix
EXPOSE 18088
CMD ["/app/smatrix"]
