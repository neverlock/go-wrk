package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/influxdata/influxdb/client/v2"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

func StartClient(url_, heads, meth string, dka bool, responseChan chan *Response, waitGroup *sync.WaitGroup, tc int) {
	defer waitGroup.Done()

	var tr *http.Transport

	u, err := url.Parse(url_)

	if err == nil && u.Scheme == "https" {
		var tlsConfig *tls.Config
		if *insecure {
			tlsConfig = &tls.Config{
				InsecureSkipVerify: true,
			}
		} else {
			// Load client cert
			cert, err := tls.LoadX509KeyPair(*certFile, *keyFile)
			if err != nil {
				log.Fatal(err)
			}

			// Load CA cert
			caCert, err := ioutil.ReadFile(*caFile)
			if err != nil {
				log.Fatal(err)
			}
			caCertPool := x509.NewCertPool()
			caCertPool.AppendCertsFromPEM(caCert)

			// Setup HTTPS client
			tlsConfig = &tls.Config{
				Certificates: []tls.Certificate{cert},
				RootCAs:      caCertPool,
			}
			tlsConfig.BuildNameToCertificate()
		}

		tr = &http.Transport{TLSClientConfig: tlsConfig, DisableKeepAlives: dka}
	} else {
		tr = &http.Transport{DisableKeepAlives: dka}
	}

	req, _ := http.NewRequest(meth, url_, nil)
	sets := strings.Split(heads, "\n")

	//Split incoming header string by \n and build header pairs
	for i := range sets {
		split := strings.SplitN(sets[i], ":", 2)
		if len(split) == 2 {
			req.Header.Set(split[0], split[1])
		}
	}

	timer := NewTimer()
	for {
		timer.Reset()

		resp, err := tr.RoundTrip(req)

		respObj := &Response{}

		if err != nil {
			respObj.Error = true
		} else {
			if resp.ContentLength < 0 { // -1 if the length is unknown
				data, err := ioutil.ReadAll(resp.Body)
				if err == nil {
					respObj.Size = int64(len(data))
				}
			} else {
				respObj.Size = resp.ContentLength
			}
			respObj.StatusCode = resp.StatusCode
			resp.Body.Close()
		}

		respObj.Duration = timer.Duration()

		if len(responseChan) >= tc {
			break
		}
		fmt.Printf("resp = %d %d %d %v\n", respObj.Size, respObj.Duration, respObj.StatusCode, respObj.Error)
		fmt.Println("Add data to database ", *influxDB)
		clnt, err := client.NewHTTPClient(client.HTTPConfig{
			Addr: *influxHost,
		})

		if err != nil {
			log.Fatalln("Error: ", err)
		}
		defer clnt.Close()

		bp, _ := client.NewBatchPoints(client.BatchPointsConfig{
			Database:  *influxDB,
			Precision: "us",
		})
		tags := map[string]string{
			"link":    target,
			"queueno": "123456",
		}
		fields := map[string]interface{}{
			"size":       respObj.Size,
			"resptime":   respObj.Duration,
			"statuscode": respObj.StatusCode,
		}
		pt, err := client.NewPoint("loadtest", tags, fields, time.Now())

		if err != nil {
			log.Fatalln("Error: ", err)
		}

		bp.AddPoint(pt)

		// Write the batch
		clnt.Write(bp)

		responseChan <- respObj
	}
}
