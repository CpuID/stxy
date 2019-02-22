package main

import (
	"encoding/csv"
	"fmt"
	"github.com/cactus/go-statsd-client/statsd"
	"github.com/fatih/color"
	"gopkg.in/urfave/cli.v1"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	app := cli.NewApp()
	app.Name = "stxy"
	app.Version = "0.0.4"
	app.Usage = "haproxy stats to statsd"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "haproxy-url",
			Value: "http://localhost:22002/;csv",
			Usage: "URL for haproxy server",
		},
		cli.StringFlag{
			Name:  "haproxy-user",
			Value: "username",
			Usage: "HTTP auth username for haproxy server",
		},
		cli.StringFlag{
			Name:  "haproxy-pass",
			Value: "password",
			Usage: "HTTP auth password for haproxy server",
		},
		cli.StringFlag{
			Name:  "statsd-url, s",
			Value: "localhost:8125",
			Usage: "host:port of statsd server",
		},
		cli.StringFlag{
			Name:  "prefix, p",
			Usage: "statsd namespace prefix",
			Value: "haproxy",
		},
		cli.IntFlag{
			Name:  "interval, i",
			Usage: "time in milliseconds between retrievals",
			Value: 10000,
		},
		cli.IntFlag{
			Name:  "failures, f",
			Usage: "exit if this qty of contiguous failures are reached",
			Value: 10,
		},
		cli.BoolFlag{
			Name:  "no-stdout, o",
			Usage: "don't send to stdout as well as statsd",
		},
		cli.BoolFlag{
			Name:  "debug,d",
			Usage: "debug mode",
		},
	}
	app.Action = func(c *cli.Context) {
		interval := c.Int("interval")
		if interval < 100 {
			log.Printf("ERROR: interval should be at least 100ms, exiting.")
			os.Exit(1)
		}
		failures := 0
		client, err := statsd.NewClient(c.String("s"), c.String("p"))
		// handle any errors
		if err != nil {
			log.Fatal(err)
		}
		// make sure to clean up
		defer client.Close()
		for {
			// start of iteration: retrieve some stats
			initial_stats, err := get_stats(c.String("haproxy-url"), c.String("haproxy-user"), c.String("haproxy-pass"))
			if err != nil {
				failures += 1
				log.Printf("Error retrieving haproxy stats: %s\n", err.Error())
				if failures > c.Int("failures") {
					log.Printf("Max of %d sequential failures reached, exiting.\n", c.Int("failures"))
					os.Exit(1)
				} else {
					log.Printf("Sleeping %d ms before retrying...\n", interval)
					time.Sleep(time.Duration(interval) * time.Millisecond)
					log.Printf("Retrying...\n")
					continue
				}
			} else {
				// Reset failures if there's success.
				failures = 0
			}
			// populate the "before" values, aka previous
			previous := map[string]int64{}
			if c.Bool("debug") == true {
				fmt.Printf("INITIAL:\n")
			}
			if c.Bool("debug") == true {
				fmt.Printf("%+v\n------------\n", initial_stats)
			}
			for k1, v := range initial_stats {
				if c.Bool("debug") == true {
					fmt.Printf("%d :: %+v\n", k1, v)
				}
				if v[1] == "BACKEND" {
					previous[fmt.Sprint("1xx_", v[0])] = get_value(v, "hrsp_1xx", 39)
					previous[fmt.Sprint("2xx_", v[0])] = get_value(v, "hrsp_2xx", 40)
					previous[fmt.Sprint("3xx_", v[0])] = get_value(v, "hrsp_3xx", 41)
					previous[fmt.Sprint("4xx_", v[0])] = get_value(v, "hrsp_4xx", 42)
					previous[fmt.Sprint("5xx_", v[0])] = get_value(v, "hrsp_5xx", 43)
				}
			}
			if c.Bool("debug") {
				fmt.Printf("END INITIAL\n")
			}
			// wait the interval
			time.Sleep(time.Duration(interval) * time.Millisecond)
			// retrieve the stats again
			records, err := get_stats(c.String("haproxy-url"), c.String("haproxy-user"), c.String("haproxy-pass"))
			if err != nil {
				failures += 1
				log.Printf("Error retrieving haproxy stats: %s\n", err.Error())
				if failures > c.Int("failures") {
					log.Printf("Max of %d sequential failures reached, exiting.\n", c.Int("failures"))
					os.Exit(1)
				} else {
					log.Printf("Sleeping %d ms before retrying...\n", interval)
					time.Sleep(time.Duration(interval) * time.Millisecond)
					log.Printf("Retrying...\n")
					continue
				}
			}
			if c.Bool("debug") {
				fmt.Printf("RECORDS:\n")
			}
			// send metrics along
			for k2, record := range records {
				if c.Bool("debug") {
					fmt.Printf("%s :: %+v\n", k2, record)
				}
				if record[1] == "BACKEND" {
					go send_gauge(client, c.Bool("no-stdout"), c.String("prefix"), record, "scur", 4)
					go send_gauge(client, c.Bool("no-stdout"), c.String("prefix"), record, "smax", 5)
					go send_gauge(client, c.Bool("no-stdout"), c.String("prefix"), record, "ereq", 12)
					go send_gauge(client, c.Bool("no-stdout"), c.String("prefix"), record, "econ", 13)
					go send_gauge(client, c.Bool("no-stdout"), c.String("prefix"), record, "rate", 33)
					go send_gauge(client, c.Bool("no-stdout"), c.String("prefix"), record, "bin", 8)
					go send_gauge(client, c.Bool("no-stdout"), c.String("prefix"), record, "bout", 9)
					// for the counters, a delta is used between previous and current
					go send_counter(previous[fmt.Sprint("1xx_", record[0])], client, c.Bool("no-stdout"), c.String("prefix"), record, "hrsp_1xx", 39)
					go send_counter(previous[fmt.Sprint("2xx_", record[0])], client, c.Bool("no-stdout"), c.String("prefix"), record, "hrsp_2xx", 40)
					go send_counter(previous[fmt.Sprint("3xx_", record[0])], client, c.Bool("no-stdout"), c.String("prefix"), record, "hrsp_3xx", 41)
					go send_counter(previous[fmt.Sprint("4xx_", record[0])], client, c.Bool("no-stdout"), c.String("prefix"), record, "hrsp_4xx", 42)
					go send_counter(previous[fmt.Sprint("5xx_", record[0])], client, c.Bool("no-stdout"), c.String("prefix"), record, "hrsp_5xx", 43)
					go send_gauge(client, c.Bool("no-stdout"), c.String("prefix"), record, "qtime", 58)
					go send_gauge(client, c.Bool("no-stdout"), c.String("prefix"), record, "ctime", 59)
					go send_gauge(client, c.Bool("no-stdout"), c.String("prefix"), record, "rtime", 60)
					go send_gauge(client, c.Bool("no-stdout"), c.String("prefix"), record, "ttime", 61)
				}
			}
			if c.Bool("debug") {
				fmt.Printf("END RECORDS\n")
			}
			log.Printf("Metrics sent to statsd.\n")
			color.White("-------------------")
		}
	}
	app.Run(os.Args)
}

func non_empty_prefix_with_dot(prefix string) string {
	use_prefix := ""
	if len(prefix) > 0 {
		use_prefix = fmt.Sprintf("%s.", prefix)
	}
	return use_prefix
}

func send_gauge(client statsd.Statter, no_stdout bool, prefix string, v []string, name string, position int64) {
	stat := fmt.Sprintf("%s%s.%s", non_empty_prefix_with_dot(prefix), v[0], name)
	value, _ := strconv.ParseInt(v[position], 10, 64)
	if no_stdout == false {
		fmt.Println(fmt.Sprint(stat, ":", value, "|g"))
	}
	err := client.Gauge(stat, value, 1.0)
	if err != nil {
		log.Printf("Error sending metric: %+v", err)
	}
}

func send_counter(previous int64, client statsd.Statter, no_stdout bool, prefix string, v []string, name string, position int64) {
	stat := fmt.Sprintf("%s%s.%s", non_empty_prefix_with_dot(prefix), v[0], name)
	value_at_interval, _ := strconv.ParseInt(v[position], 10, 64)
	value := value_at_interval - previous
	if no_stdout == false {
		fmt.Println(fmt.Sprint(stat, ":", value, "|c"))
	}
	err := client.Inc(stat, value, 1)
	if err != nil {
		log.Printf("Error sending metric: %+v", err)
	}
}

func get_value(v []string, name string, position int64) int64 {
	value, _ := strconv.ParseInt(v[position], 10, 64)
	return value
}

// Substitute dots for underscores in the proxy names, first field of each record
// So that we get nicely structured statsd namespaces (not multiple level)
func substitute_proxy_names(records [][]string) [][]string {
	for k1, v1 := range records {
		for k2, v2 := range v1 {
			records[k1][k2] = strings.Replace(v2, ".", "_", -1)
		}
	}
	return records
}

func get_stats(url string, user string, pass string) ([][]string, error) {
	// TODO: validate url, make sure its fully qualified.

	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if len(user) > 0 && len(pass) > 0 {
		req.SetBasicAuth(user, pass)
	}
	resp, err := client.Do(req)
	if err != nil {
		return [][]string{}, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)

	r := csv.NewReader(strings.NewReader(string(body)))
	records, err := r.ReadAll()
	if err != nil {
		log.Fatal(err)
	}

	// Substitute proxy names
	records = substitute_proxy_names(records)

	// Return the results
	return records, nil
}
