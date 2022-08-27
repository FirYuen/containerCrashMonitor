FROM golang:1.19.0 as builder
WORKDIR /src
COPY go.sum .
COPY go.mod .
RUN go mod download
COPY . .
#RUN CGO_ENABLED=0 GOOS=linux go build  -o /src/app .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /src/app .

FROM hairyhenderson/upx:latest as upx
COPY --from=builder /src/app /app.org
RUN upx --best --lzma -o /app /app.org

FROM busybox:1.35.0
COPY --from=upx /app .
ENTRYPOINT ["./app"]
