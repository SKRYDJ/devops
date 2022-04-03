FROM golang:1.15.2 AS srv-dependencies
ENV NAME "srv"
WORKDIR /opt/go-kafka-partitions-exporter
COPY go.mod .
COPY go.sum .
RUN go mod download

FROM srv-dependencies AS build
ENV NAME "srv"
WORKDIR /opt/go-kafka-partitions-exporter
COPY . .
RUN go build


FROM debian:buster-slim
WORKDIR /opt/go-kafka-partitions-exporter
COPY --from=build /opt/go-kafka-partitions-exporter/go-kafka-partitions-exporter ./go-kafka-partitions-exporter
CMD ./go-kafka-partitions-exporter
