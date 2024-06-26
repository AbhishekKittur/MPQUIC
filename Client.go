package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LK4D4/trylock"
	"github.com/lucas-clemente/quic-go"
	common "github.com/mpquic-test/common"
)

type QperfClient struct {
	duration       int
	serverHostname string
}

const PORT = "6000"

var (
	bandwidth  = []int{-1, -1}
	delay      = []int{0, 0}
	loss       = []int{0, 0}
	intT       = 0
	basePath   = ""
	iterPath   = 0
	lock       sync.Mutex
	try        trylock.Mutex
)

func (c *QperfClient) run() string {
	cmd := exec.Command("qperf", "-c", c.serverHostname, "-Z", "-J", "-t", fmt.Sprintf("%d", c.duration))
	out, _ := cmd.Output()
	return string(out)
}

func createClient(duration int, interfaceNum ...int) *QperfClient {
	var num int
	if len(interfaceNum) > 0 {
		num = interfaceNum[0]
	} else {
		num = 0
	}
	hostgrp, err := common.GetHostGroup()
	if err != nil {
		fmt.Printf("error while getting the host group: %v", err)
	} else {
		basePath = fmt.Sprintf("./result%d/%s", hostgrp, "%s")
	}

	return &QperfClient{
		duration:       duration,
		serverHostname: fmt.Sprintf(common.ServerHostname, num+1),
	}
}

func setupClient() {
	common.DoDeltcAll()
}

func runTest(client *QperfClient, duration int) (string, [][]int) {
	var valueResult [][]int

	p := time.Now()

	evaluationDone := make(chan bool)
	interfereDone := make(chan bool)

	go func() {
		defer func() {
			evaluationDone <- true
		}()
		for {
			elapsed := int(time.Since(p).Seconds())
			if elapsed >= duration {
				break
			}
			cwnd1 := common.GetCwnd(0)
			cwnd2 := common.GetCwnd(1)
			valueResult = append(valueResult, []int{elapsed, cwnd1, cwnd2})
			time.Sleep(200 * time.Millisecond)
			select {
			case <-interfereDone:
				return
			default:
			}
		}
	}()

	go func() {
		defer func() {
			common.DoResettc()
			interfereDone <- true
		}()
		for i := 1; i < len(common.InterruptTiming); i++ {
			select {
			case <-evaluationDone:
				return
			case <-time.After(time.Duration(intT) * time.Millisecond):
				break
			}
			common.DoSettc(delay, common.InterruptLoss[i], common.InterruptChange[i])
		}
		select {
		case <-evaluationDone:
			return
		case <-time.After(time.Duration(math.Max(0, float64(duration-int(time.Since(p).Seconds())))) * time.Second):
			break
		}
	}()

	result := client.run()
	<-interfereDone
	<-evaluationDone

	return result, valueResult
}

func performTest(duration int, name string, intf ...int) {
	var num int
	if len(intf) > 0 {
		num = intf[0]
	} else {
		num = 0
	}
	client := createClient(duration, num)

	for i := 0; i < common.RepeatTimes; i++ {
		result, valueResult := runTest(client, duration)
		var resultJson map[string]interface{}
		if err := json.Unmarshal([]byte(result), &resultJson); err != nil {
			fmt.Printf("Error while unmarshalling: %v", err)
			continue
		}
		if received, ok := resultJson["end"].(map[string]interface{})["sum_received"].(map[string]interface{}); ok {
			mbps := received["bits_per_second"].(float64) / 1000 / 1000
			fmt.Printf("Test #%d %s finished with %f Mbps\n", iterPath, name, mbps)
			if name != "" {
				resultFile, _ := os.Create(fmt.Sprintf(basePath, iterPath) + "/" + name + ".json")
				defer resultFile.Close()
				json.NewEncoder(resultFile).Encode(resultJson)
				valueFile, _ := os.Create(fmt.Sprintf(basePath, iterPath) + "/" + name + "_value.json")
				defer valueFile.Close()
				json.NewEncoder(valueFile).Encode(valueResult)
			}
			return
		} else {
			fmt.Printf("Test %s failed: %s\n", name, result)
			time.Sleep(time.Duration(common.SleepDuration) * time.Second)
			continue
		}
	}
	time.Sleep(time.Duration(common.SleepDuration) * time.Second)
}

func main() {
	fmt.Println("Client Up")

	setupClient()

	if _, err := os.Stat(basePath); os.IsNotExist(err) {
		os.Mkdir(basePath, os.ModePerm)
	}

	for iterPath := 0; iterPath < 5; iterPath++ {
		if _, err := os.Stat(filepath.Join(basePath, strconv.Itoa(iterPath))); err == nil {
			if _, err := os.Stat(filepath.Join(basePath, strconv.Itoa(iterPath+1))); err == nil {
				continue
			}
		}

		os.Mkdir(filepath.Join(basePath, strconv.Itoa(iterPath)), os.ModePerm)

		for _, congestion := range common.Congestions {
			for _, scheduler := range common.Schedulers {
				common.DoResettc()

				for _, delay := range common.Delays {
					for _, loss := range common.Losses {
						for _, intT := range common.InterruptDuration {
							baseName := []string{congestion, scheduler, common.CvrtVldFilename(bandwidth), common.CvrtVldFilename(delay), common.CvrtVldFilename(loss), common.CvrtVldFilename(intT)}
							testName := []string{strings.Join(append(baseName, common.PathMarks[0]), "_"), strings.Join(append(baseName, common.PathMarks[1]), "_"), strings.Join(append(baseName, common.PathMarks[2]), "_")}

							if _, err := os.Stat(filepath.Join(basePath, strconv.Itoa(iterPath), testName[len(testName)-1]+".json")); err == nil {
								fmt.Printf("Test #%d %v already exists\n", iterPath, baseName)
								continue
							}

							common.DoSettc(delay, loss)
							performTest(intT*(len(common.InterruptTiming)+1), testName[0])
						}

						performTest(common.ShortDuration, "")
						fmt.Printf("Part %v %v finished\n", congestion, scheduler)
					}
				}
			}
		}
		common.DoDeltcAll()
	}

	fmt.Println("Client Down")
}
