package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/lucas-clemente/quic-go"
)

type lkfqperf3Server struct {
}

func (s *lkfqperf3Server) run() (string, error) {
	out, err := exec.Command("qperf", "-lp", "19999", "-t", "10000").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func createServer() *lkfqperf3Server {
	server := lkfqperf3Server{}
	return &server
}

func main() {
	fmt.Println("Server Up")

	//prepare_sysctl()

	addr, err := net.ResolveUDPAddr("udp", "0.0.0.0:19999")
	if err != nil {
		log.Fatal(err)
	}
	listener, err := quic.ListenAddr(addr, generateConfig(), nil)
	if err != nil {
		log.Fatal(err)
	}

	for {
		session, err := listener.Accept(context.Background())
		if err != nil {
			log.Fatal(err)
		}
		stream, err := session.AcceptStream(context.Background())
		if err != nil {
			log.Fatal(err)
		}

		recv := make([]byte, 1024)
		_, err = stream.Read(recv)
		if err != nil {
			log.Fatal(err)
		}
		var params map[string]string
		if err := json.Unmarshal(recv, &params); err != nil {
			log.Fatal(err)
		}
		congestion := params["congestion"]
		scheduler := params["scheduler"]

		server := createServer()
		for {
			result, err := server.run()
			if err != nil {
				if _, ok := err.(KeyError); ok || _, ok := err.(IndexError); ok {
					fmt.Println(err)
					fmt.Println(result, "?")
				}
				var resultJson map[string]interface{}
				if resultString, ok := result.(string); ok {
					err := json.Unmarshal([]byte(resultString), &resultJson)
					if err != nil {
						// Handle error
					}
				} else {
					resultJson = result.(map[string]interface{})
				}

				fmt.Printf("Test %s finished with %.2f Mbps\n", congestion+" "+scheduler, resultJson["end"].(map[string]interface{})["sum_received"].(map[string]interface{})["bits_per_second"].(float64)/(1024*1024))

				if resultJson["start"].(map[string]interface{})["test_start"].(map[string]interface{})["duration"].(float64) == 3 {
					break
				}
			}
		}
	}
}
