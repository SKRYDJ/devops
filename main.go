package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
)

const (
	name      = "prometheus-network-check"
	namespace = "prometheus_network_check"
	indexPage = `
	<html>
	<head>
		<title>Check network access</title>
		<link rel="stylesheet" href="https://stackpath.bootstrapcdn.com/bootstrap/4.1.3/css/bootstrap.min.css" integrity="sha384-MCw98/SFnGE8fJT3GXwEOngsV7Zt27NXFoaoApmYm81iuXoPkFOJwJ8ERdknLPMO" crossorigin="anonymous">
	</head>
	<body>
	<div class="container">
		<div class="py-5 row">
			<div class="col-md-3"></div>
			<div class="col-md-6">
				<form onsubmit="return false">
					<div class="row">
						<div class="col-md-9 mb-3">
							<label for="target">Target</label>
							<input type="text" class="form-control" name="target" id="target" value="%[2]s" placeholder="%[3]s" autofocus>
						</div>
						<div class="col-md-3 mb-3">
							<label for="timeout">Timeout</label>
							<select class="custom-select" id="timeout" required>
								<option value="1s" selected>1s</option>
								<option value="5s">5s</option>
								<option value="10s">10s</option>
								<option value="30s">30s</option>
							</select>
						</div>
					</div>
					<hr class="mb-4">
					<button id="submit" class="btn btn-primary btn-lg btn-block" type="submit">Check network access</button>
				</form>
				<div id="response"></div>
			</div>
			<div class="col-md-3"></div>
		</div>
	</div>
	<script>
		(function(){
			var submit = document.getElementById('submit');
			function checkAccess() {
				var request = new XMLHttpRequest();
				var target = document.getElementById('target').value;
				var timeout = document.getElementById('timeout').value;
				var div = document.getElementById('response');
				div.innerHTML = '';
				if (target == '') {
					alert('Target must be specified');
					return;
				}
				submit.disabled = true;
				request.open('POST', '%[1]s/api/check', true);
				request.setRequestHeader('Content-Type', 'application/json; charset=utf-8');
				request.send(JSON.stringify({
					target:  target,
					timeout: timeout
				}));
				request.onerror = function() {
					submit.disabled = false;
					alert('Error. Try again later');
				};
				request.onload = function() {
					submit.disabled = false;
					if (request.status >= 200 && request.status < 400) {
						var data = JSON.parse(request.responseText);
						var div = document.getElementById('response');
						data.forEach(function(item, i, arr) {
							state = 'danger';
							if (item.has_access == true) {
								state = 'success';
							}
							tmpl = ` + "`" + `<div class="card text-white bg-${state}"><div class="card-header">${item.node}</div><div class="card-body"><p class="card-text">${item.response_status}</p></div></div><br>` + "`" + `;
							div.innerHTML += tmpl;
						});
					} else {
						alert('Error. Try again later');
					}
				};
			};
			submit.onclick=function() {
				checkAccess();
			};
		})();
	</script>
	</body>
	</html>
	`
)

var (
	listenAddress           = flag.String("web.listen-address", "0.0.0.0:8010", "Listen address")
	webTargetValue          = flag.String("web.target-input-value", "google.ru:80", "Default value for target input in web interface")
	webTargetPlaceholder    = flag.String("web.target-input-placeholder", "localhost:9100", "Placeholder for input in web interface")
	targetScheme            = flag.String("web.target-scheme", "http", "Default scheme for targets if not specified")
	prometheusNodesRaw      = flag.String("prometheus.nodes", "http://127.0.0.1:8010", "Comma-separated list of Prometheus addresses (http://host1:8010,http://host2:8010)")
	prometheusAliasesRaw    = flag.String("prometheus.aliases", "node1", "Comma-separated list of Prometheus aliases (node1,node2)")
	indexRoute              = flag.String("web.path", "/network-check", "Web interface path")
	telemetryRoute          = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics; Empty string to disable")
	showVersion             = flag.Bool("version", false, "Prints version and exit")
	accessRequestTimeoutMin = flag.Duration("timeout.min", time.Second, "Minimal allowed timeout for access requests")
	accessRequestTimeoutMax = flag.Duration("timeout.max", 30*time.Second, "Maximal allowed timeout for access requests")

	httpCli *http.Client

	DefaultTimeout      time.Duration     = 60 * time.Second
	DefaultRoundTripper http.RoundTripper = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		Dial: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 15 * time.Second,
		}).Dial,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	prometheusNodes   = []string{}
	prometheusAliases = []string{}

	SuccessChecksCount = prometheus.NewCounter(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(namespace, "", "success_count"),
		Help: "Number of success checks.",
	})
	FailedChecksCount = prometheus.NewCounter(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(namespace, "", "failure_count"),
		Help: "Number of failed checks.",
	})
)

type CheckRequest struct {
	Target     string        `json:"target"`
	TimeoutRaw string        `json:"timeout"`
	Timeout    time.Duration `json:"-"`
}

type CheckResponse struct {
	Node            string        `json:"node"`
	Request         *CheckRequest `json:"request"`
	HasAccess       bool          `json:"has_access"`
	Status          string        `json:"response_status"`
	StatusCode      int           `json:"response_status_code"`
	RequestDuration string        `json:"request_duration"`
}

func IndexHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, fmt.Sprintf(indexPage, *indexRoute, *webTargetValue, *webTargetPlaceholder))
}

func checkNode(node string, checkReq *CheckRequest) (CheckResponse, error) {
	var checkResp CheckResponse
	reqURL := fmt.Sprintf("%s%s/api/_call", node, *indexRoute)
	log.Printf("Checking '%s' on %s node by %s...", checkReq.Target, node, reqURL)
	bodyBytes, err := json.Marshal(checkReq)
	if err != nil {
		return checkResp, fmt.Errorf("Can't encode request json body: %v", err)
	}
	log.Printf("Sending: %s", string(bodyBytes))
	req, err := http.NewRequest("POST", reqURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return checkResp, fmt.Errorf("Can't create request to Prometheus node %s: %v", node, err)
	}
	resp, err := httpCli.Do(req)
	if err != nil {
		return checkResp, fmt.Errorf("Can't get response from Prometheus node %s: %v", node, err)
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return checkResp, fmt.Errorf("Can't read response body: %v", err)
	}
	log.Printf("Response %s: %s", resp.Status, string(b))
	if resp.StatusCode != http.StatusOK {
		return checkResp, fmt.Errorf("Unexpected error code from Prometheus node %s: %s", node, resp.Status)
	}
	if err = json.Unmarshal(b, &checkResp); err != nil {
		return checkResp, fmt.Errorf("Can't decode response json body: %v", err)
	}
	return checkResp, nil
}

func CheckHandler(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	encoder := json.NewEncoder(w)
	var checkReq *CheckRequest
	if err := decoder.Decode(&checkReq); err != nil {
		log.Println(err)
		http.Error(w, "Can't decode request json body", http.StatusBadRequest)
		return
	}
	log.Printf("Checking '%s' on %v nodes...", checkReq.Target, prometheusNodes)
	var responses []CheckResponse
	for id, node := range prometheusNodes {
		checkResp, err := checkNode(node, checkReq)
		if err != nil {
			log.Println(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		checkResp.Node = prometheusAliases[id]
		responses = append(responses, checkResp)
	}
	if err := encoder.Encode(responses); err != nil {
		log.Println(err)
		http.Error(w, "Can't decode response json body", http.StatusBadRequest)
		return
	}
}

func RootHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "This address used by %s v%s", name, version.Version)
}

func CallHandler(w http.ResponseWriter, r *http.Request) {
	var (
		checkReq *CheckRequest
		link     string
	)
	decoder := json.NewDecoder(r.Body)
	encoder := json.NewEncoder(w)
	if err := decoder.Decode(&checkReq); err != nil {
		log.Println(err)
		http.Error(w, "Can't decode request json body", http.StatusBadRequest)
		return
	}
	d, err := time.ParseDuration(checkReq.TimeoutRaw)
	if err != nil {
		log.Println(err)
		http.Error(w, "Can't parse timeout value", http.StatusBadRequest)
		return
	}
	checkReq.Timeout = d
	httpCli := httpClientWithTimeout(checkReq.Timeout)
	checkResp := &CheckResponse{
		Request:   checkReq,
		HasAccess: false,
	}
	startT := time.Now()
	if strings.Contains(checkReq.Target, "://") {
		link = checkReq.Target
	} else {
		link = fmt.Sprintf("%s://%s", *targetScheme, checkReq.Target)
	}
	resp, err := httpCli.Get(link)
	if err != nil {
		log.Println(err)
		checkResp.Status = err.Error()
		FailedChecksCount.Inc()
	} else {
		checkResp.Status = resp.Status
		checkResp.StatusCode = resp.StatusCode
		if resp.StatusCode == http.StatusOK {
			SuccessChecksCount.Inc()
			checkResp.HasAccess = true
		} else {
			FailedChecksCount.Inc()
		}
	}
	defer resp.Body.Close()
	checkResp.RequestDuration = time.Now().Sub(startT).String()
	if err = encoder.Encode(checkResp); err != nil {
		log.Println(err)
		http.Error(w, "Can't decode response json body", http.StatusBadRequest)
		return
	}
}

func httpClientWithTimeout(timeout time.Duration) *http.Client {
	if timeout < *accessRequestTimeoutMin {
		timeout = *accessRequestTimeoutMin
	}
	if timeout > *accessRequestTimeoutMax {
		timeout = *accessRequestTimeoutMax
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:             http.ProxyFromEnvironment,
			DisableKeepAlives: true,
			Dial: (&net.Dialer{
				Timeout: timeout,
			}).Dial,
			TLSHandshakeTimeout: timeout,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		Timeout: timeout,
	}
}

func registerMetrics() {
	prometheus.MustRegister(SuccessChecksCount)
	prometheus.MustRegister(FailedChecksCount)
}

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Println(version.Print(name))
		return
	}

	httpCli = &http.Client{
		Transport: DefaultRoundTripper,
		Timeout:   DefaultTimeout,
	}

	log.Printf("Starting %s %s...", name, version.Version)
	registerMetrics()

	prometheusNodes = strings.Split(*prometheusNodesRaw, ",")
	prometheusAliases = strings.Split(*prometheusAliasesRaw, ",")

	if len(prometheusNodes) != len(prometheusAliases) {
		log.Fatal("Check Prometheus addresses and aliases")
	}

	log.Printf("Listen address: %s", *listenAddress)
	log.Printf("Prometheus nodes: %v", prometheusNodes)
	log.Printf("Index path: %s", *indexRoute)

	if len(*telemetryRoute) != 0 {
		log.Printf("Telemetry path: %s", *telemetryRoute)
		http.Handle(*telemetryRoute, promhttp.Handler())
	}

	http.HandleFunc("/", RootHandler)
	http.HandleFunc(fmt.Sprintf("%s", *indexRoute), IndexHandler)
	http.HandleFunc(fmt.Sprintf("%s/api/%s", *indexRoute, "check"), CheckHandler)
	http.HandleFunc(fmt.Sprintf("%s/api/%s", *indexRoute, "_call"), CallHandler)

	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
