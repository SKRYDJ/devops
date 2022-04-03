package main

import (
	"flag"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/Shopify/sarama"
	"github.com/prometheus/client_golang/prometheus"
)

//for test - 10.50.83.23:32057
var (
	kafkaBrokers   = flag.String("kafka-brokers", "10.73.31.11:9092,10.73.31.12:9092,10.73.31.12:9092,10.72.31.11:9092,10.72.31.11:9092,10.72.31.11:9092", "Comma separated list of kafka brokers.")
	prometheusAddr = flag.String("prometheus-addr", ":7979", "Prometheus listen interface and port.")
	refreshInt     = flag.Int("refresh-interval", 300, "Time refreshes in seconds.")
	saslUser       = flag.String("sasl-user", "", "SASL username.")
	saslPass       = flag.String("sasl-pass", "", "SASL password.")
	debug          = flag.Bool("debug", false, "Enable debug output.")
	algorithm      = flag.String("algorithm", "", "The SASL algorithm sha256 or sha512 as mechanism")
)

func init() {
	prometheus.MustRegister(LeaderPartitionTopic)
	prometheus.MustRegister(PartitionsTopic)
	prometheus.MustRegister(TopicsCount)
	prometheus.MustRegister(LookupHist)
}

func main() {
	go func() {
		var cycle uint8
		config := sarama.NewConfig()
		config.ClientID = "kfk-partitions-exporter"
		config.Version = sarama.V0_11_0_2
		if *saslUser != "" {
			config.Net.SASL.Enable = true
			config.Net.SASL.User = *saslUser
			config.Net.SASL.Password = *saslPass
		}
		if *algorithm == "sha512" {
			config.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient { return &XDGSCRAMClient{HashGeneratorFcn: SHA512} }
			config.Net.SASL.Mechanism = sarama.SASLMechanism(sarama.SASLTypeSCRAMSHA512)
		} else if *algorithm == "sha256" {
			config.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient { return &XDGSCRAMClient{HashGeneratorFcn: SHA256} }
			config.Net.SASL.Mechanism = sarama.SASLMechanism(sarama.SASLTypeSCRAMSHA256)
		}
		client, err := sarama.NewClient(strings.Split(*kafkaBrokers, ","), config)
		if err != nil {
			log.Fatal("Unable to connect to brokers.", *kafkaBrokers)
		}
		defer client.Close()
		for {
			topicPartitionsInfo := TopicPartitionsInfo{}
			partitionInfoLeader := PartitionInfoLeader{}
			time.Sleep(time.Duration(*refreshInt) * time.Second)
			timer := prometheus.NewTimer(LookupHist)
			client.RefreshMetadata()
			topics, err := client.Topics()
			if err != nil {
				log.Printf("Error fetching topics: %s", err.Error())
				continue
			}
			for _, topic := range topics {
				// Don't include internal topics
				if strings.HasPrefix(topic, "__") {
					continue
				}
				partitions, err := client.Partitions(topic)
				if err != nil {
					log.Printf("Error fetching partitions: %s", err.Error())
					continue
				}
				if *debug {
					log.Printf("Found topic '%s' with %d partitions", topic, len(partitions))
				}
				partitionStr := stringToStrings(partitions)
				topicPartitionsInfo[topic] = partitionStr
				for _, partition := range partitions {
					brLeader, err := client.Leader(topic, partition)
					if err != nil {
						log.Printf("Error get broker leader of topic '%s' with %d partitions, %s", topic, partition, err.Error())
					} else {
						leaderPartition := brLeader.Addr()
						k := map[int32]string{}
						k[partition] = leaderPartition
						partitionInfoLeader[topic] = k
					}
				}
			}
			if cycle >= 99 {
				PartitionsTopic.Reset()
				LeaderPartitionTopic.Reset()
				TopicsCount.Reset()
				cycle = 0
			}
			cycle++

			var wg sync.WaitGroup

			for _, broker := range client.Brokers() {
				broker.Open(client.Config())
				_, err := broker.Connected()
				if err != nil {
					log.Printf("Could not speak to broker %s. Your advertised.listeners may be incorrect.", broker.Addr())
					continue
				}
				wg.Add(1)
				go func(broker *sarama.Broker) {
					defer wg.Done()
					exportMetrics(partitionInfoLeader, topicPartitionsInfo)
				}(broker)
			}

			wg.Wait()
			timer.ObserveDuration()
		}
	}()
	prometheusListen(*prometheusAddr)
}

func stringToStrings(values []int32) string {
	var valuesText []string
	for i := range values {
		number := values[i]
		text := fmt.Sprint(number)
		valuesText = append(valuesText, text)
	}
	res := strings.Join(valuesText, ",")
	return res
}
