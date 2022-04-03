package main

import (
	"log"
	"math"
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	TopicsCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_topics_count",
			Help: "Count topics on cluster.",
		},
		[]string{
			"brokers",
		},
	)

	PartitionsTopic = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_topic_partitions",
			Help: "Kafka topic partitions on cluster.",
		},
		[]string{
			"topic",
			"brokers",
			"partitions",
		},
	)

	LeaderPartitionTopic = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_broker_leader_partition_topic",
			Help: "Kafka broker leader of partition topics.",
		},
		[]string{
			"topic",
			"broker",
			"partition",
		},
	)

	LookupHist = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "kafka_partition_exporter_lookup_duration_seconds",
			Help:    "Histogram for the runtime of the partitions exporter.",
			Buckets: []float64{.1, .25, .5, 1, 2.5, 5, 10, 15, 30, 60, 120},
		},
	)
)

type PartitionInfoLeader map[string]map[int32]string
type TopicPartitionsInfo map[string]string

func prometheusListen(addr string) {
	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(addr, nil))
}

func exportMetrics(leaderInfo PartitionInfoLeader, topicInfo TopicPartitionsInfo) {
	for topic, partitions := range leaderInfo {
		for partition, leaderBroker := range partitions {
			LeaderPartitionTopic.With(prometheus.Labels{
				"topic": topic, "broker": leaderBroker,
				"partition": strconv.FormatInt(int64(partition), 10),
			}).Set(math.Max(float64(partition), 0))
		}
	}
	//1 set for get sum of topics for dynatrace on cluster
	for topic := range topicInfo {
		PartitionsTopic.With(prometheus.Labels{
			"topic": topic, "brokers": *kafkaBrokers,
			"partitions": topicInfo[topic],
		}).Set(1)
	}

	TopicsCount.With(prometheus.Labels{
		"brokers": *kafkaBrokers,
	}).Set(math.Max(float64(len(topicInfo)), 0))
}
