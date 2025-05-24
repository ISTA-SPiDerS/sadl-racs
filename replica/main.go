package main

import (
	"flag"
	"fmt"
	"mandator-sporades/configuration"
	"mandator-sporades/replica/src"
	"os"
)

func main() {
	name := flag.Int64("name", 1, "name of the replica as specified in the configuration.yml")
	configFile := flag.String("config", "configuration/local/configuration.yml", "configuration file")
	consAlgo := flag.String("consAlgo", "async", "consensus algo [async, paxos]")
	logFilePath := flag.String("logFilePath", "logs/", "log file path")
	batchSize := flag.Int("batchSize", 50, "batch size")
	batchTime := flag.Int("batchTime", 5000, "maximum time to wait for collecting a batch of requests in micro seconds")
	debugOn := flag.Bool("debugOn", false, "false or true")
	isAsync := flag.Bool("isAsync", false, "false or true for simulating asynchrony")
	mode := flag.Int("mode", 1, "1 for all to all broadcast, and 2 for selective broadcast")
	debugLevel := flag.Int("debugLevel", 0, "debug level")
	viewTimeout := flag.Int("viewTimeout", 20000000, "view timeout in micro seconds")
	window := flag.Int("window", 10, "window for abortable broadcast")
	networkBatchTime := flag.Int("networkBatchTime", 3, "network batch time in ms")
	keyLen := flag.Int("keyLen", 8, "key length")
	valLen := flag.Int("valLen", 8, "value length")
	benchmarkMode := flag.Int("benchmarkMode", 0, "0: resident store, 1: redis")
	asyncSimTime := flag.Int("asyncSimTime", 0, "in ms")
	timeEpochSize := flag.Int("timeEpochSize", 500, "in ms the length of a time epoch for attacks")

	flag.Parse()

	cfg, err := configuration.NewInstanceConfig(*configFile, *name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		panic(err)
	}

	rp := src.New(int32(*name), cfg, *logFilePath, *batchSize, *batchTime, *debugOn, *mode, *debugLevel, *viewTimeout, *window, *networkBatchTime, *consAlgo, *benchmarkMode, *keyLen, *valLen, *isAsync, *asyncSimTime, *timeEpochSize)

	rp.WaitForConnections()
	rp.StartOutgoingLinks()
	rp.Run() // this is run in main thread

}
