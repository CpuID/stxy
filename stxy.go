package main

import (
	"encoding/csv"
	"fmt"
	"github.com/cactus/go-statsd-client/statsd"
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
		// TODO: implement
		//cli.BoolFlag{
		//	Name:  "per-server",
		//	Usage: "instead of aggregated per backend stats, export per server stats (N servers per backend)",
		//},
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
		// Was used in the past, left in place for ease of troubleshooting later
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
		client, err := statsd.NewClient(c.String("s"), c.String("p"))
		// handle any errors
		if err != nil {
			log.Fatal(err)
		}
		// make sure to clean up
		defer client.Close()

		previous := map[string]int64{}

		// No stats retrieved yet, do an initial retrieval, set them as previous, then sleep
		// Usually only used on first startup

		// Retrieve the stats
		records, err := get_stats_auto_retry(c.String("haproxy-url"), c.String("haproxy-user"), c.String("haproxy-pass"), c.Int("failures"))
		if err != nil {
			log.Printf("Error retrieving initial stats (exiting): %s\n", err.Error())
			os.Exit(1)
		}
		// Set the initial retrieval data as previous
		previous = populate_previous(records)
		// Then sleep for one interval
		time.Sleep(time.Duration(interval) * time.Millisecond)

		no_stdout := c.Bool("no-stdout")
		prefix := c.String("prefix")

		for {
			// Per interval, retrieve the stats, send some metrics, then set previous to the values retrieved here
			records, err = get_stats_auto_retry(c.String("haproxy-url"), c.String("haproxy-user"), c.String("haproxy-pass"), c.Int("failures"))
			if err != nil {
				log.Printf("Error retrieving next stats (exiting): %s\n", err.Error())
				os.Exit(1)
			}
			// send metrics along
			for _, record := range records {
				// Only backend stats are exported
				// TODO: do we want to export others? per server rather than aggregated per backend only?
				// skimming through the list, theres a few that don't return for server but do for backend, so
				// would need to do a hybrid if we move to per server...
				// https://cbonte.github.io/haproxy-dconv/1.7/management.html#9.1 - last arg = position
				if record[1] == "BACKEND" {
					go send_gauge(client, no_stdout, prefix, record, "scur", 4)
					go send_gauge(client, no_stdout, prefix, record, "smax", 5)
					go send_gauge(client, no_stdout, prefix, record, "slim", 6)
					go send_gauge(client, no_stdout, prefix, record, "bin", 8)
					go send_gauge(client, no_stdout, prefix, record, "bout", 9)
					// TODOLATER: ereq is only available for a frontend or listener, not a backend)
					// go send_gauge(client, no_stdout, prefix, record, "ereq", 12)
					go send_gauge(client, no_stdout, prefix, record, "econ", 13)
					go send_gauge(client, no_stdout, prefix, record, "eresp", 14)
					go send_gauge(client, no_stdout, prefix, record, "wretr", 15)
					go send_gauge(client, no_stdout, prefix, record, "wredis", 16)
					// TODOLATER: 17 status, parse the value and set a 0 or 1 gauge for up/down? 18/19 will suffice for now
					go send_gauge(client, no_stdout, prefix, record, "act", 19)
					go send_gauge(client, no_stdout, prefix, record, "bck", 20)
					go send_gauge(client, no_stdout, prefix, record, "lastchg", 23)
					go send_gauge(client, no_stdout, prefix, record, "rate", 33)
					go send_gauge(client, no_stdout, prefix, record, "rate_max", 35)
					// for the counters, a delta is used between previous and current
					go send_counter(previous[fmt.Sprint("1xx_", record[0])], client, no_stdout, prefix, record, "hrsp_1xx", 39)
					go send_counter(previous[fmt.Sprint("2xx_", record[0])], client, no_stdout, prefix, record, "hrsp_2xx", 40)
					go send_counter(previous[fmt.Sprint("3xx_", record[0])], client, no_stdout, prefix, record, "hrsp_3xx", 41)
					go send_counter(previous[fmt.Sprint("4xx_", record[0])], client, no_stdout, prefix, record, "hrsp_4xx", 42)
					go send_counter(previous[fmt.Sprint("5xx_", record[0])], client, no_stdout, prefix, record, "hrsp_5xx", 43)
					go send_gauge(client, no_stdout, prefix, record, "qtime", 58)
					go send_gauge(client, no_stdout, prefix, record, "ctime", 59)
					go send_gauge(client, no_stdout, prefix, record, "rtime", 60)
					go send_gauge(client, no_stdout, prefix, record, "ttime", 61)
				}
			}
			// Now repopulate the previous from the last lot of stats retrieved, used on the next iteration.
			previous = populate_previous(records)
			log.Printf("Metrics sent to statsd.\n")
			// Sleep for the interval
			time.Sleep(time.Duration(interval) * time.Millisecond)
		}
	}
	app.Run(os.Args)
}

func populate_previous(records [][]string) map[string]int64 {
	// New map to return, always starts fresh (no need to retain data past one iteration above this function)
	previous := map[string]int64{}
	for _, v := range records {
		// https://cbonte.github.io/haproxy-dconv/1.7/management.html#9.1 - last arg = position
		if v[1] == "BACKEND" {
			previous[fmt.Sprint("1xx_", v[0])] = get_value(v, "hrsp_1xx", 39)
			previous[fmt.Sprint("2xx_", v[0])] = get_value(v, "hrsp_2xx", 40)
			previous[fmt.Sprint("3xx_", v[0])] = get_value(v, "hrsp_3xx", 41)
			previous[fmt.Sprint("4xx_", v[0])] = get_value(v, "hrsp_4xx", 42)
			previous[fmt.Sprint("5xx_", v[0])] = get_value(v, "hrsp_5xx", 43)
		}
	}
	return previous
}

// Only used for stdout display purposes, the statsd.NewClient() handles actual prefix additions
func non_empty_prefix_with_dot(prefix string) string {
	use_prefix := ""
	if len(prefix) > 0 {
		use_prefix = fmt.Sprintf("%s.", prefix)
	}
	return use_prefix
}

func send_gauge(client statsd.Statter, no_stdout bool, prefix string, v []string, name string, position int64) {
	stat := fmt.Sprintf("%s.%s", v[0], name)
	value, _ := strconv.ParseInt(v[position], 10, 64)
	if no_stdout == false {
		// Prefix addition only used for stdout display purposes, the statsd.NewClient() handles actual prefix additions for sent metrics
		fmt.Printf("%s%s:%d|g\n", non_empty_prefix_with_dot(prefix), stat, value)
	}
	err := client.Gauge(stat, value, 1.0)
	if err != nil {
		log.Printf("Error sending metric: %+v", err)
	}
}

func send_counter(previous int64, client statsd.Statter, no_stdout bool, prefix string, v []string, name string, position int64) {
	stat := fmt.Sprintf("%s.%s", v[0], name)
	value_at_interval, _ := strconv.ParseInt(v[position], 10, 64)
	value := value_at_interval - previous
	if no_stdout == false {
		// Prefix addition only used for stdout display purposes, the statsd.NewClient() handles actual prefix additions for sent metrics
		fmt.Printf("%s%s:%d|c\n", non_empty_prefix_with_dot(prefix), stat, value)
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

	// TODO: generate a single http.client{}, pass it in?
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
	if err != nil {
		return [][]string{}, err
	}

	r := csv.NewReader(strings.NewReader(string(body)))
	records, err := r.ReadAll()
	if err != nil {
		return [][]string{}, err
	}

	// Substitute proxy names
	records = substitute_proxy_names(records)

	// Return the results
	return records, nil
}

// Failures must be contiguous to trigger an exit, so the tally is function local
func get_stats_auto_retry(url string, user string, pass string, max_failures int) ([][]string, error) {
	failures := 0
	// Hard defined 5 seconds between retries
	retry_ms := 5000
	for {
		records, err := get_stats(url, user, pass)
		if err != nil {
			failures += 1
			log.Printf("Error retrieving haproxy stats: %s\n", err.Error())
			if failures > max_failures {
				return [][]string{}, fmt.Errorf("Max of %d sequential failures reached", max_failures)
			} else {
				log.Printf("Sleeping %d ms before retrying... (%d failures so far)\n", retry_ms, failures)
				time.Sleep(time.Duration(retry_ms) * time.Millisecond)
				log.Printf("Retrying...\n")
				continue
			}
		}
		return records, nil
	}
}
