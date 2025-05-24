package src

import (
	"fmt"
	"log"
	"mandator-sporades/common"
	"mandator-sporades/proto"
	"math/rand"
	"os"
	"strconv"
	"time"
)

/*
	an instance defines the content of a single Paxos consensus instance
*/

type Instance struct {
	id int32 // instance number

	proposedBallot int32
	promisedBallot int32
	acceptedBallot int32

	acceptedValues []int32
	decided        bool
	decisions      []int32

	proposeResponses []*proto.PaxosConsensus

	highestSeenAcceptedBallot int32
	highestSeenAcceptedValue  []int32
}

/*
	Paxos struct defines the replica wide consensus variables
*/

type Paxos struct {
	view                  int32      // current view number
	currentLeader         int32      // current leader
	lastPromisedBallot    int32      // last promised ballot number, for each next instance created this should be used as the promised ballot
	lastPreparedBallot    int32      // last prepared ballot as the proposer, all future instances should propose for this ballot number
	lastDecidedLogIndex   int32      // the last log position that is decided
	lastCommittedLogIndex int32      // the last log position that is committed
	replicatedLog         []Instance // the replicated log of commands
	viewTimer             *common.TimerWithCancel
	lastCommittedRounds   []int                             // for each replica, the index of the mempool block that was last committed
	startTime             time.Time                         // time when the consensus was started
	lastCommittedTime     time.Time                         // time when the last consensus instance was committed
	nextFreeInstance      int                               // log position that needs to be created next in the replicated log
	state                 string                            // can be A (acceptor), L (leader), C (contestant)
	promiseResponses      map[int32][]*proto.PaxosConsensus // for each view the set of received promise messages
}

/*
	Init Paxos Consensus data structs
*/

func InitPaxosConsensus(numReplicas int) *Paxos {

	lastCommittedRounds := make([]int, numReplicas)
	for i := 0; i < numReplicas; i++ {
		lastCommittedRounds = append(lastCommittedRounds, 0)
	}

	replicatedLog := make([]Instance, 0)
	// create the genesis slot

	replicatedLog = append(replicatedLog, Instance{
		id:                        0,
		proposedBallot:            -1,
		promisedBallot:            -1,
		acceptedBallot:            -1,
		acceptedValues:            nil,
		decided:                   true,
		decisions:                 nil,
		proposeResponses:          nil,
		highestSeenAcceptedBallot: -1,
		highestSeenAcceptedValue:  nil,
	})

	// create initial slots
	for i := 1; i < 100; i++ {
		replicatedLog = append(replicatedLog, Instance{
			id:                        int32(i),
			proposedBallot:            -1,
			promisedBallot:            -1,
			acceptedBallot:            -1,
			acceptedValues:            nil,
			decided:                   false,
			decisions:                 nil,
			proposeResponses:          make([]*proto.PaxosConsensus, 0),
			highestSeenAcceptedBallot: -1,
			highestSeenAcceptedValue:  nil,
		})
	}

	return &Paxos{
		view:                  0,
		currentLeader:         -1,
		lastPromisedBallot:    -1,
		lastPreparedBallot:    -1,
		lastDecidedLogIndex:   0, // the log positions start with index 1
		lastCommittedLogIndex: 0,
		replicatedLog:         replicatedLog,
		viewTimer:             nil,
		lastCommittedRounds:   lastCommittedRounds,
		startTime:             time.Time{},
		lastCommittedTime:     time.Time{},
		nextFreeInstance:      100,
		state:                 "A",
		promiseResponses:      make(map[int32][]*proto.PaxosConsensus),
	}
}

/*
	append N new instances to the log from here
*/

func (rp *Replica) createNInstances(number int) {

	for i := 0; i < number; i++ {

		rp.paxosConsensus.replicatedLog = append(rp.paxosConsensus.replicatedLog, Instance{
			id:                        int32(rp.paxosConsensus.nextFreeInstance),
			proposedBallot:            -1,
			promisedBallot:            rp.paxosConsensus.lastPromisedBallot,
			acceptedBallot:            -1,
			acceptedValues:            nil,
			decided:                   false,
			decisions:                 nil,
			proposeResponses:          make([]*proto.PaxosConsensus, 0),
			highestSeenAcceptedBallot: -1,
			highestSeenAcceptedValue:  nil,
		})

		rp.paxosConsensus.nextFreeInstance++
	}
}

/*
	check if the instance number instance is already there, if not create 10 new instances
*/

func (rp *Replica) createInstanceIfMissing(instanceNum int) {

	numMissingEntries := instanceNum - rp.paxosConsensus.nextFreeInstance + 1

	if numMissingEntries > 0 {
		rp.createNInstances(numMissingEntries)
	}
}

/*
	handler for generic Paxos messages
*/

func (rp *Replica) handlePaxosConsensus(message *proto.PaxosConsensus) {
	debugLevel := 0

	if message.Type == 1 {
		if rp.debugOn {
			common.Debug("Received a prepare message from "+strconv.Itoa(int(message.Sender))+
				" for view "+strconv.Itoa(int(message.View))+" for prepare ballot "+strconv.Itoa(int(message.PrepareBallot))+" for initial instance "+strconv.Itoa(int(message.InstanceNumber))+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.paxosConsensus.startTime)), debugLevel, rp.debugLevel, rp.debugOn)
		}
		rp.handlePrepare(message)
	}

	if message.Type == 2 {
		if rp.debugOn {
			common.Debug("Received a promise message from "+strconv.Itoa(int(message.Sender))+
				" for view "+strconv.Itoa(int(message.View))+" for instance "+strconv.Itoa(int(message.InstanceNumber))+" for promise ballot "+strconv.Itoa(int(message.PromiseBallot))+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.paxosConsensus.startTime)), debugLevel, rp.debugLevel, rp.debugOn)
		}
		rp.handlePromise(message)
	}

	if message.Type == 3 {
		if rp.debugOn {
			common.Debug("Received a propose message from "+strconv.Itoa(int(message.Sender))+
				" for view "+strconv.Itoa(int(message.View))+" for instance "+strconv.Itoa(int(message.InstanceNumber))+" for propose ballot "+strconv.Itoa(int(message.ProposeBallot))+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.paxosConsensus.startTime)), debugLevel, rp.debugLevel, rp.debugOn)
		}
		rp.handlePropose(message)
	}

	if message.Type == 4 {
		if rp.debugOn {
			common.Debug("Received a accept message from "+strconv.Itoa(int(message.Sender))+
				" for view "+strconv.Itoa(int(message.View))+" for instance "+strconv.Itoa(int(message.InstanceNumber))+" for accept ballot "+strconv.Itoa(int(message.AcceptBalllot))+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.paxosConsensus.startTime)), debugLevel, rp.debugLevel, rp.debugOn)
		}
		rp.handleAccept(message)
	}

	if message.Type == 5 {
		if rp.debugOn {
			common.Debug("Received an internal timeout message from "+strconv.Itoa(int(message.Sender))+
				" for view "+strconv.Itoa(int(message.View))+" for instance "+strconv.Itoa(int(message.InstanceNumber))+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.paxosConsensus.startTime)), debugLevel, rp.debugLevel, rp.debugOn)
		}
		rp.handlePaxosInternalTimeout(message)
	}
}

/*
	set a timer, which once timeout will send an internal notification for a prepare message after another random wait to break the ties
*/

func (rp *Replica) setPaxosViewTimer(view int32, lastDecidedIndex int32) {
	rand.Seed(time.Now().UnixMilli() + int64(rp.name))
	rp.paxosConsensus.viewTimer = common.NewTimerWithCancel(time.Duration(rp.viewTimeout+rand.Intn(rp.viewTimeout/2)) * time.Microsecond)

	rp.paxosConsensus.viewTimer.SetTimeoutFunction(func() {

		// this function runs in a separate thread, hence we do not send prepare message in this function, instead send a timeout-internal signal
		internalTimeoutNotification := proto.PaxosConsensus{
			Sender:         rp.name,
			Receiver:       rp.name,
			UniqueId:       "",
			Type:           5,
			Note:           "",
			InstanceNumber: lastDecidedIndex,
			PrepareBallot:  -1,
			PromiseBallot:  -1,
			ProposeBallot:  -1,
			AcceptBalllot:  -1,
			View:           view,
			PromiseReply:   nil,
			ProposeValue:   nil,
			DecidedValue:   nil,
		}

		rpcPair := common.RPCPair{
			Code: rp.messageCodes.PaxosConsensus,
			Obj:  &internalTimeoutNotification,
		}
		rp.sendMessage(rp.name, rpcPair)
		if rp.debugOn {
			common.Debug("Sent an internal timeout notification for view "+strconv.Itoa(int(rp.paxosConsensus.view))+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.paxosConsensus.startTime)), 4, rp.debugLevel, rp.debugOn)
		}

	})
	rp.paxosConsensus.viewTimer.Start()
}

/*
	print the replicated log to check for log consistency
*/

func (rp *Replica) printPaxosLogConsensus() {
	f, err := os.Create(rp.logFilePath + strconv.Itoa(int(rp.name)) + "-consensus.txt")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	lastCommittedMemPoolIndexes := make([]int, rp.numReplicas)
	for i := 0; i < rp.numReplicas; i++ {
		lastCommittedMemPoolIndexes[i] = 0
	}

	for i := int32(1); i <= rp.paxosConsensus.lastCommittedLogIndex; i++ {
		nextBlockToCommit := rp.paxosConsensus.replicatedLog[i]
		nextMemBlockLogPositionsToCommit := nextBlockToCommit.decisions

		// for each log position in nextMemBlockLogPositionsToCommit that corresponds to different replicas, check if the index is --
		// greater than the lastCommittedMemPoolIndexes
		for j := 0; j < rp.numReplicas; j++ {
			if int(nextMemBlockLogPositionsToCommit[j]) > lastCommittedMemPoolIndexes[j] {
				// there are new entries to commit for this index
				startMemPoolCounter := lastCommittedMemPoolIndexes[j] + 1
				lastMemPoolCounter := int(nextMemBlockLogPositionsToCommit[j])

				for k := startMemPoolCounter; k <= lastMemPoolCounter; k++ {
					memPoolName := strconv.Itoa(j+1) + "." + strconv.Itoa(k)
					memBlock, _ := rp.memPool.blockMap.Get(memPoolName)
					for clientBatchIndex := 0; clientBatchIndex < len(memBlock.ClientBatches); clientBatchIndex++ {
						clientBatch := memBlock.ClientBatches[clientBatchIndex]
						clientBatchCommands := clientBatch.Requests
						for clientRequestIndex := 0; clientRequestIndex < len(clientBatchCommands); clientRequestIndex++ {
							clientRequest := clientBatchCommands[clientRequestIndex].Command
							_, _ = f.WriteString(strconv.Itoa(int(i)) + "-" + memPoolName + "-" + strconv.Itoa(int(clientBatchIndex)) + "-" + strconv.Itoa(int(clientRequestIndex)) + ":" + clientRequest + "\n")
						}
					}

				}
				lastCommittedMemPoolIndexes[j] = lastMemPoolCounter
			}
		}
	}
}
