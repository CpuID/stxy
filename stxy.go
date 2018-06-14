package main

import (
	"encoding/csv"
	"fmt"
	"github.com/cactus/go-statsd-client/statsd"
	"github.com/codegangsta/cli"
	"github.com/fatih/color"
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
			Name:  "prefix,p",
			Usage: "statsd namespace",
			Value: "haproxy",
		},
		cli.StringFlag{
			Name:  "interval,i",
			Usage: "time in milliseconds",
			Value: "10000",
		},
		cli.BoolFlag{
			Name:  "debug,d",
			Usage: "debug mode",
		},
	}
	app.Action = func(c *cli.Context) {
		interval, _ := strconv.ParseInt(c.String("interval"), 10, 64)
		for {
			client, err := statsd.NewClient(c.String("s"), c.String("p"))
			// handle any errors
			if err != nil {
				log.Fatal(err)
			}
			// make sure to clean up
			defer client.Close()
			initial_stats, err := get_stats(c.String("haproxy-url"), c.String("haproxy-user"), c.String("haproxy-pass"))
			// TODO: handle err
			previous := map[string]int64{}
			if c.Bool("debug") {
				fmt.Printf("INITIAL:\n")
			}
			for k1, v := range initial_stats {
				if c.Bool("debug") {
					fmt.Printf("%s :: %+v\n", k1, v)
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
				fmt.Printf("END INITIAL.")
			}
			time.Sleep(time.Duration(interval) * time.Millisecond)
			records, err := get_stats(c.String("haproxy-url"), c.String("haproxy-user"), c.String("haproxy-pass"))
			// TODO: handle err
			if c.Bool("debug") {
				fmt.Printf("RECORDS:")
			}
			for k2, record := range records {
				if c.Bool("debug") {
					fmt.Printf("%s :: %+v\n", k2, record)
				}
				if record[1] == "BACKEND" {
					go send_gauge(client, record, "scur", 4)
					go send_gauge(client, record, "smax", 5)
					go send_gauge(client, record, "ereq", 12)
					go send_gauge(client, record, "econ", 13)
					go send_gauge(client, record, "rate", 33)
					go send_gauge(client, record, "bin", 8)
					go send_gauge(client, record, "bout", 9)
					go send_counter(previous[fmt.Sprint("1xx_", record[0])], client, record, "hrsp_1xx", 39)
					go send_counter(previous[fmt.Sprint("2xx_", record[0])], client, record, "hrsp_2xx", 40)
					go send_counter(previous[fmt.Sprint("3xx_", record[0])], client, record, "hrsp_3xx", 41)
					go send_counter(previous[fmt.Sprint("4xx_", record[0])], client, record, "hrsp_4xx", 42)
					go send_counter(previous[fmt.Sprint("5xx_", record[0])], client, record, "hrsp_5xx", 43)
					go send_gauge(client, record, "qtime", 58)
					go send_gauge(client, record, "ctime", 59)
					go send_gauge(client, record, "rtime", 60)
					go send_gauge(client, record, "ttime", 61)
				}
			}
			if c.Bool("debug") {
				fmt.Printf("END RECORDS.")
			}
			color.White("-------------------")
		}
	}
	app.Run(os.Args)
}

func send_gauge(client statsd.Statter, v []string, name string, position int64) {
	stat := fmt.Sprint(v[0], ".", name)
	value, _ := strconv.ParseInt(v[position], 10, 64)
	fmt.Println(fmt.Sprint(stat, ":", value, "|g"))
	err := client.Gauge(stat, value, 1.0)
	if err != nil {
		log.Printf("Error sending metric: %+v", err)
	}
}

func send_counter(previous int64, client statsd.Statter, v []string, name string, position int64) {
	stat := fmt.Sprint(v[0], ".", name)
	value_at_interval, _ := strconv.ParseInt(v[position], 10, 64)
	value := value_at_interval - previous
	fmt.Println(fmt.Sprint(stat, ":", value, "|c"))
	err := client.Inc(stat, value, 1)
	if err != nil {
		log.Printf("Error sending metric: %+v", err)
	}
}

func get_value(v []string, name string, position int64) int64 {
	value, _ := strconv.ParseInt(v[position], 10, 64)
	return value
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
	return records, nil
}
