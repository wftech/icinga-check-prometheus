package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
)

const (
	// Exit status
	ExitOK       = 0
	ExitWarning  = 1
	ExitCritical = 2
	// Query type
	QueryCheck    = 1
	QueryDuration = 2
	QuerySamples  = 3
)

type PrometheusRequest struct {
	URL             string
	Instance        string
	Tags            string
	Query           map[string]string
	TimeoutWarning  float64
	TimeoutCritical float64
}

type PrometheusApiResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric struct {
				Name     string `json:"__name__"`
				Instance string `json:"instance"`
				Job      string `json:"job"`
				Role     string `json:"role"`
			} `json:"metric"`
			Value []interface{} `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func (r PrometheusRequest) PrepareQuery(query int) {
	q := make(map[string]string)
	switch query {
	case QueryCheck:
		if len(r.Tags) == 0 {
			q["query"] = fmt.Sprintf(`up{instance="%s"}`, r.Instance)
		} else {
			q["query"] = fmt.Sprintf(`up{instance="%s","%s"}`, r.Instance, r.Tags)
		}
	case QueryDuration:
		if len(r.Tags) == 0 {
			q["query"] = fmt.Sprintf(`scrape_duration_seconds{instance="%s"}`, r.Instance)
		} else {
			q["query"] = fmt.Sprintf(`scrape_duration_seconds{instance="%s","%s"}`, r.Instance, r.Tags)
		}
	case QuerySamples:
		if len(r.Tags) == 0 {
			q["query"] = fmt.Sprintf(`scrape_samples_scraped{instance="%s"}`, r.Instance)
		} else {
			q["query"] = fmt.Sprintf(`scrape_samples_scraped{instance="%s","%s"}`, r.Instance, r.Tags)
		}
	}
	r.Query = q
}

func (r PrometheusRequest) Call() (int, string) {
	req, err := http.NewRequest("GET", r.URL, nil)
	if err != nil {
		return ExitCritical, fmt.Sprintf("CRITICAL - API request error: %s", err.Error())
	}

	if len(r.Query) > 0 {
		q := req.URL.Query()
		for k, v := range r.Query {
			q.Add(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return ExitCritical, fmt.Sprintf("CRITICAL - API request error: %s", err.Error())
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return ExitCritical, fmt.Sprintf("CRITICAL - API request error:  %s", resp.Status)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return ExitCritical, fmt.Sprintf("CRITICAL - API response read error: %s", err.Error())
	}

	var parsed PrometheusApiResponse
	err = json.Unmarshal(body, &parsed)
	if err != nil {
		return ExitCritical, fmt.Sprintf("CRITICAL - API response parse error %s", err.Error())
	}

	if len(parsed.Data.Result) == 0 {
		return ExitCritical, "CRITICAL - API response error - no data"
	}

	result := parsed.Data.Result[0].Value[1]
	return ExitOK, fmt.Sprintf("%v", result)
}

func main() {
	instance := flag.String("instance", "default", "Instance name")
	tags := flag.String("tags", "", "Tags")
	timeoutWarning := flag.Float64("timeout-warning", 5.0, "Timeout warning")
	timeoutCritical := flag.Float64("timeout-critical", 30.0, "Timeout critical")
	flag.Parse()

	msg := ""
	status := ExitOK

	r := PrometheusRequest{
		URL:             "http://127.0.0.1:9090/api/v1/query",
		Instance:        *instance,
		Tags:            *tags,
		TimeoutWarning:  *timeoutWarning,
		TimeoutCritical: *timeoutCritical,
	}

	// Check
	r.PrepareQuery(QueryCheck)
	statusCheck, msgCheck := r.Call()
	if statusCheck > ExitOK {
		fmt.Println(msgCheck)
		os.Exit(statusCheck)
	}
	msgCheckInt, err := strconv.Atoi(msgCheck)
	if err != nil {
		fmt.Printf("CRITICAL - %s", err.Error())
		os.Exit(ExitCritical)
	}
	if msgCheckInt == 0 {
		fmt.Printf("CRITICAL - unable to scrape instance %s", r.Instance)
		os.Exit(ExitCritical)
	}

	// Duration
	r.PrepareQuery(QueryDuration)
	statusDuration, msgDuration := r.Call()
	if statusDuration > ExitOK {
		fmt.Println(msgDuration)
		os.Exit(statusDuration)
	}
	msgDurationFloat, err := strconv.ParseFloat(msgDuration, 32)
	if err != nil {
		fmt.Printf("CRITICAL - %s", err.Error())
		os.Exit(ExitCritical)
	}
	if msgDurationFloat > r.TimeoutCritical {
		msg = "CRITICAL - "
		status = ExitCritical
	} else if msgDurationFloat > r.TimeoutWarning {
		msg = "WARNING  - "
		status = ExitWarning
	}

	// Samples
	r.PrepareQuery(QuerySamples)
	statusSamples, msgSamples := r.Call()
	if statusSamples > ExitOK {
		fmt.Println(msgSamples)
		os.Exit(statusSamples)
	}

	// Final message
	msg = msg + fmt.Sprintf("scraping took %.1fs", msgDurationFloat)
	msg = msg + fmt.Sprintf("|duration=%fs;%.0f;%.0f;0;", msgDurationFloat, r.TimeoutWarning, r.TimeoutCritical)
	msg = msg + fmt.Sprintf(" samples=%s", msgSamples)

	fmt.Println(msg)
	os.Exit(status)
}
