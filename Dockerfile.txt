FROM golang:1.19-bullseye AS dev

WORKDIR /app

# pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them in subsequent builds if they change
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

RUN CGO_ENABLED=1 go build \
    -ldflags="-linkmode external -extldflags -static" \
    -tags netgo \
    -o myhttpgo \
    ./...

RUN ls -l

#FROM scratch
FROM debian:bullseye

WORKDIR /

COPY --from=dev /app/myhttpgo /myhttpgo

EXPOSE 4221

CMD [ "/myhttpgo" ]
