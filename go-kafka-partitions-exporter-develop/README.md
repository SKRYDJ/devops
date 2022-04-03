# go-kafka-partitions-exporter
Expose kafka topics partitions metrics to prometheus.

### Prometheus metrics
```
kafka_broker_leader_partition_topic{broker="10.50.83.23:9200",partition="0",topic="test_topic"} 0  
kafka_topic_partitions{brokers="10.50.83.23:9200",partitions="0,1,2",topic="craml-customer-state"} 1
kafka_topics_count{brokers="10.50.83.23:9200"} 100
```

### env:
Use the following ENV vars to change the default options:
* KAFKA_BROKERS=comma separated list of brokers (default kafka:9092)
* PROMETHEUS_ADDR=address and port for Prometheus to bind to (default :7979)
* REFRESH_INTERVAL=how long in seconds between each refresh (default 300)
* SASL_USER=SASL username if required (default "")
* SASL_PASS=SASL password if required (default "")
* DEBUG=true or false (default false)
* ALGORITHM=The SASL algorithm sha256 or sha512 as mechanism (default "")
